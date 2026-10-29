package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

// One process-wide gate is shared by request-time managers and sessions. The
// limit is read for each acquisition, rather than baked into a channel whose
// capacity could become stale after a config edit.
var configuredSandboxTurns = sandboxTurnCapacity{states: make(map[string]*sandboxTurnCapacityState)}

type sandboxTurnCapacity struct {
	mu     sync.Mutex
	states map[string]*sandboxTurnCapacityState
}

type sandboxTurnCapacityState struct {
	active  int
	waiters int
	changed chan struct{}
}

func (g *sandboxTurnCapacity) hold(ctx context.Context, key string, limit int) (func(), error) {
	noop := func() {}
	if limit <= 0 {
		return noop, ctx.Err()
	}
	g.mu.Lock()
	st := g.states[key]
	if st == nil {
		st = &sandboxTurnCapacityState{changed: make(chan struct{})}
		g.states[key] = st
	}
	st.waiters++
	for {
		if err := ctx.Err(); err != nil {
			st.waiters--
			if st.active == 0 && st.waiters == 0 {
				delete(g.states, key)
			}
			g.mu.Unlock()
			return noop, err
		}
		if st.active < limit {
			break
		}
		changed := st.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		g.mu.Lock()
	}
	st.waiters--
	st.active++
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			st.active--
			close(st.changed)
			st.changed = make(chan struct{})
			if st.active == 0 && st.waiters == 0 {
				delete(g.states, key)
			}
		})
	}, nil
}

func (s *sessionService) holdConfiguredSandboxCapacity(ctx context.Context, sessionID, configID string) (func(), error) {
	noop := func() {}
	if s.sandboxConfigRepo == nil || s.hostDesktop {
		return noop, nil
	}
	tenantID, _ := types.TenantIDFromContext(ctx)
	if tenantID == 0 {
		return noop, nil
	}
	if s.sandboxPinner != nil {
		pin, err := s.sandboxPinner.Read(ctx, sessionID)
		if err != nil {
			return noop, err
		}
		if !pin.IsZero() {
			configID, tenantID = pin.ConfigID, pin.TenantOr(tenantID)
		}
	}
	configID = strings.TrimSpace(configID)
	if configID == "" || configID == types.SandboxConfigIDGlobalDefault {
		return noop, nil
	}
	row, err := s.sandboxConfigRepo.GetByID(ctx, tenantID, configID)
	if err != nil {
		return noop, err
	}
	if row == nil || row.Config == nil || row.Config.SandboxType != "docker" || row.Config.Docker == nil {
		return noop, nil
	}
	return configuredSandboxTurns.hold(ctx, fmt.Sprintf("%d:%s", tenantID, configID), row.Config.Docker.MaxConcurrentTurns)
}
