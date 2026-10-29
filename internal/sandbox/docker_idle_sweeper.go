// Idle reclamation for the docker backend.
//
// Every other backend gets this for free. Cube and E2B are told an idle
// timeout at creation and the provider pauses or kills the sandbox when it
// elapses, which is why SessionBoundManager never reaps anything itself. The
// Docker daemon has no such concept: a container runs until something stops
// it. Without a sweep, one abandoned session pins its memory and CPU share on
// the host forever.
//
// The sweep needs to know when a sandbox was last used, and Docker exposes no
// exec timestamps. Labels cannot help either — they are immutable after
// create. So each exec touches an activity marker file inside the container
// (folded into the exec wrapper, costing no extra round-trip), and the sweep
// reads its mtime with a single HEAD /archive call per container.
//
// Deleting an idle container needs no coordination with the binding store: the
// lifecycle already treats a sandbox the provider no longer has as replaceable
// (see CanReplaceRemoteBinding), which is exactly how an E2B sandbox reaped by
// its own TTL is handled.

package sandbox

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/moby/moby/client"
)

// dockerIdleTTLLabel records the TTL each container was created with, so a
// sweep triggered by one workspace config never applies its own TTL to a
// container belonging to another.
const dockerIdleTTLLabel = "com.weknora.sandbox.idle-ttl-seconds"

// dockerSweepMinInterval bounds how often one daemon is swept, no matter how
// many requests trigger it. Sweeping is a list plus one stat per container;
// cheap, but not free enough to run on every execution.
const dockerSweepMinInterval = time.Minute

// dockerSweepBudget bounds one sweep so a wedged daemon cannot leave a
// goroutine hanging around forever.
const dockerSweepBudget = 2 * time.Minute

// dockerSweepThrottle records the last sweep per daemon endpoint. It is
// process-wide because the adapter is rebuilt per request; per-instance state
// would throttle nothing.
var dockerSweepThrottle = struct {
	mu   sync.Mutex
	last map[string]time.Time
}{last: make(map[string]time.Time)}

// Long commands are active work even when the marker has not been refreshed
// for an entire TTL. Keep the periodic sweeper from reclaiming that container.
var dockerExecActivity = struct {
	sync.Mutex
	refs map[string]int
}{refs: make(map[string]int)}

func holdDockerExecActivity(endpoint, containerID string) func() {
	key := endpoint + "\x00" + containerID
	dockerExecActivity.Lock()
	dockerExecActivity.refs[key]++
	dockerExecActivity.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			dockerExecActivity.Lock()
			defer dockerExecActivity.Unlock()
			dockerExecActivity.refs[key]--
			if dockerExecActivity.refs[key] <= 0 {
				delete(dockerExecActivity.refs, key)
			}
		})
	}
}

// dockerIdleSweeper reclaims containers that have not executed anything for
// longer than their TTL.
type dockerIdleSweeper struct {
	client *DockerRemoteClient
	ttl    time.Duration

	// now is injected by tests.
	now func() time.Time
}

func newDockerIdleSweeper(cli *DockerRemoteClient, ttl time.Duration) *dockerIdleSweeper {
	return &dockerIdleSweeper{client: cli, ttl: ttl, now: time.Now}
}

// trigger runs a sweep in the background unless one ran recently. It detaches
// from the caller's context so finishing the request does not abort the sweep,
// and it never reports failures upward: reclaiming storage must not turn a
// working execution into an error.
func (s *dockerIdleSweeper) trigger(ctx context.Context) {
	if s == nil || s.client == nil {
		return
	}
	s.ensurePeriodicSweep()
	if !s.claimSweep() {
		return
	}
	go func() {
		// Drop the chat-trace parent: Engine API list/stat/delete from a
		// sweep must not appear as siblings of agent.round after the tool
		// that triggered the sweep has already finished its span.
		sweepCtx, cancel := context.WithTimeout(
			logger.CloneContextWithoutTrace(ctx), dockerSweepBudget,
		)
		defer cancel()
		if reclaimed, err := s.sweep(sweepCtx); err != nil {
			log.Printf("[sandbox] docker idle sweep failed: %v", err)
		} else if reclaimed > 0 {
			log.Printf("[sandbox] docker idle sweep reclaimed %d container(s)", reclaimed)
		}
	}()
}

// Actual daemon clients get one recurring worker per endpoint. Request-time
// manager construction must not create one ticker per session. Fake clients
// remain synchronous unless a test explicitly starts a worker.
func (s *dockerIdleSweeper) ensurePeriodicSweep() {
	api := s.client.api
	for {
		wrapped, ok := api.(*dockerRPCTimeoutAPI)
		if !ok {
			break
		}
		api = wrapped.inner
	}
	if _, ok := api.(*client.Client); !ok {
		return
	}
	failures := 0
	dockerPeriodicSweeps.start(context.Background(), s.client.settings.Endpoint.key(), dockerSweepMinInterval, func(ctx context.Context) bool {
		if !DockerBackendEnabled() {
			return false
		}
		sweepCtx, cancel := context.WithTimeout(ctx, dockerSweepBudget)
		defer cancel()
		if reclaimed, err := s.sweep(sweepCtx); err != nil {
			failures++
			log.Printf("[sandbox] periodic docker idle sweep failed: %v", err)
			// A removed daemon endpoint must not keep a worker forever. A new
			// request can register it again after connectivity is restored.
			return failures < 5
		} else if reclaimed > 0 {
			log.Printf("[sandbox] periodic docker idle sweep reclaimed %d container(s)", reclaimed)
		}
		failures = 0
		remaining, err := s.client.List(sweepCtx, RemoteListFilter{})
		return err != nil || len(remaining) != 0
	})
}

type dockerPeriodicSweepWorker struct {
	run     func(context.Context) bool
	done    chan struct{}
	version uint64
}

type dockerPeriodicSweepRegistry struct {
	mu      sync.Mutex
	workers map[string]*dockerPeriodicSweepWorker
}

var dockerPeriodicSweeps = dockerPeriodicSweepRegistry{workers: make(map[string]*dockerPeriodicSweepWorker)}

func (r *dockerPeriodicSweepRegistry) start(ctx context.Context, key string, interval time.Duration, run func(context.Context) bool) *dockerPeriodicSweepWorker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.workers[key]; existing != nil {
		existing.run = run
		existing.version++
		return existing
	}
	w := &dockerPeriodicSweepWorker{run: run, done: make(chan struct{}), version: 1}
	r.workers[key] = w
	go func() {
		defer func() {
			r.mu.Lock()
			if r.workers[key] == w {
				delete(r.workers, key)
			}
			r.mu.Unlock()
			close(w.done)
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.mu.Lock()
				fn := w.run
				version := w.version
				r.mu.Unlock()
				if !fn(ctx) {
					r.mu.Lock()
					if version == w.version {
						delete(r.workers, key)
						r.mu.Unlock()
						return
					}
					r.mu.Unlock()
				}
			}
		}
	}()
	return w
}

// claimSweep reports whether this caller won the right to sweep now.
func (s *dockerIdleSweeper) claimSweep() bool {
	key := s.client.settings.Endpoint.key()
	now := s.now()
	dockerSweepThrottle.mu.Lock()
	defer dockerSweepThrottle.mu.Unlock()
	if last, ok := dockerSweepThrottle.last[key]; ok &&
		now.Sub(last) < dockerSweepMinInterval {
		return false
	}
	dockerSweepThrottle.last[key] = now
	return true
}

// sweep deletes every managed container idle beyond its own TTL and reports
// how many went away.
func (s *dockerIdleSweeper) sweep(ctx context.Context) (int, error) {
	summaries, err := s.client.List(ctx, RemoteListFilter{})
	if err != nil {
		return 0, err
	}
	reclaimed := 0
	for _, summary := range summaries {
		select {
		case <-ctx.Done():
			return reclaimed, ctx.Err()
		default:
		}
		if !s.isIdle(ctx, summary) {
			continue
		}
		// Re-check immediately before deleting. Listing every container and
		// stat'ing each one takes long enough on a busy daemon that a session
		// can be resumed in between, and deleting it then destroys a sandbox
		// the user is actively working in.
		if !s.isIdle(ctx, summary) {
			continue
		}
		if err := s.client.Delete(ctx, summary.ID); err != nil {
			// One undeletable container must not stop the sweep; the next
			// pass will try again.
			log.Printf("[sandbox] docker idle sweep: delete %s: %v", summary.ID, err)
			continue
		}
		reclaimed++
	}
	return reclaimed, nil
}

// isIdle decides whether one container has gone unused for longer than the
// TTL it was created with.
func (s *dockerIdleSweeper) isIdle(ctx context.Context, summary RemoteSandboxSummary) bool {
	dockerExecActivity.Lock()
	active := dockerExecActivity.refs[s.client.settings.Endpoint.key()+"\x00"+summary.ID] > 0
	dockerExecActivity.Unlock()
	if active {
		return false
	}
	ttl := s.ttlFor(summary)
	if ttl <= 0 {
		return false
	}
	lastUsed := s.lastActivity(ctx, summary)
	if lastUsed.IsZero() {
		// Neither a marker nor a creation timestamp: treating that as idle
		// would risk deleting a sandbox that is merely unreadable right now.
		return false
	}
	return s.now().UTC().Sub(lastUsed) > ttl
}

// ttlFor prefers the TTL stamped on the container at creation over this
// client's own configuration.
func (s *dockerIdleSweeper) ttlFor(summary RemoteSandboxSummary) time.Duration {
	if raw, ok := summary.Metadata[dockerIdleTTLLabel]; ok {
		if seconds, err := strconv.Atoi(raw); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return s.ttl
}

// lastActivity returns when the container last ran a command, falling back to
// when it started for a sandbox that has not executed anything yet.
//
// The marker lives inside the container and has to be writable by the account
// the execs run as, so its mtime is attacker-influenced: a script can
// `touch -d` it. A timestamp in the future is the one form of that which
// would disable reclamation permanently, so it is refused outright and the
// container falls back to its start time. Backdating only makes a sandbox look
// idle sooner, which costs the container that did it and nothing else.
func (s *dockerIdleSweeper) lastActivity(
	ctx context.Context,
	summary RemoteSandboxSummary,
) time.Time {
	stat, err := s.client.api.ContainerStatPath(ctx, summary.ID,
		client.ContainerStatPathOptions{Path: dockerActivityMarker})
	if err == nil && !stat.Stat.Mtime.IsZero() {
		marker := stat.Stat.Mtime.UTC()
		if marker.After(s.now().UTC().Add(dockerActivityClockSkew)) {
			log.Printf(
				"[sandbox] docker idle sweep: container %s reports activity at %s, "+
					"which is in the future; falling back to its start time",
				summary.ID, marker.Format(time.RFC3339),
			)
		} else {
			return marker
		}
	}
	return summary.StartedAt.UTC()
}

// dockerActivityClockSkew is how far ahead of WeKnora the daemon's clock may
// run before a marker is treated as forged. The marker is stamped by the
// daemon host while the comparison happens here, and the two are not
// necessarily the same machine.
const dockerActivityClockSkew = 2 * time.Minute
