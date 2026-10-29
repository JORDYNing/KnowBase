package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestDockerPeriodicSweepRunsWithoutNewRequestsAndDeduplicates(t *testing.T) {
	r := dockerPeriodicSweepRegistry{workers: make(map[string]*dockerPeriodicSweepWorker)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := make(chan struct{}, 10)
	w := r.start(ctx, "daemon", 5*time.Millisecond, func(context.Context) bool { runs <- struct{}{}; return true })
	other := r.start(ctx, "daemon", 5*time.Millisecond, func(context.Context) bool { runs <- struct{}{}; return true })
	require.Same(t, w, other)
	for i := 0; i < 2; i++ {
		select {
		case <-runs:
		case <-time.After(time.Second):
			t.Fatal("periodic sweep did not run")
		}
	}
	cancel()
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	r.mu.Lock()
	require.Empty(t, r.workers)
	r.mu.Unlock()
}

func TestDockerIdleSweeperPreservesAnActiveLongCommand(t *testing.T) {
	now := time.Now().UTC()
	s, engine := newSweeperFixture(t, time.Minute, now, []container.Summary{{
		ID: "running-command", State: "running", Created: now.Add(-time.Hour).Unix(),
		Labels: map[string]string{dockerManagedLabel: "true"},
	}})
	engine.statResult[dockerActivityMarker] = container.PathStat{Mtime: now.Add(-time.Hour)}
	release := holdDockerExecActivity(s.client.settings.Endpoint.key(), "running-command")
	n, err := s.sweep(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
	release()
	release()
	n, err = s.sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestDockerPeriodicSweepRetiresUnusedDaemon(t *testing.T) {
	r := dockerPeriodicSweepRegistry{workers: make(map[string]*dockerPeriodicSweepWorker)}
	w := r.start(context.Background(), "daemon", time.Millisecond, func(context.Context) bool { return false })
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("unused daemon worker leaked")
	}
}

func TestDockerPeriodicSweepRefreshDuringRetirementKeepsWorker(t *testing.T) {
	r := dockerPeriodicSweepRegistry{workers: make(map[string]*dockerPeriodicSweepWorker)}
	started, unblock, refreshed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	w := r.start(context.Background(), "daemon", time.Millisecond, func(context.Context) bool {
		close(started)
		<-unblock
		return false
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old worker never ran")
	}
	require.Same(t, w, r.start(context.Background(), "daemon", time.Millisecond, func(context.Context) bool {
		close(refreshed)
		return false
	}))
	close(unblock)
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("refreshed worker was lost")
	}
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish")
	}
}
