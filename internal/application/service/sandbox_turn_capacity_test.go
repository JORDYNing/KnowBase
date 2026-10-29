package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSandboxTurnCapacityWaitCancellationAndRelease(t *testing.T) {
	g := sandboxTurnCapacity{states: make(map[string]*sandboxTurnCapacityState)}
	release, err := g.hold(context.Background(), "tenant:config", 1)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		close, err := g.hold(ctx, "tenant:config", 1)
		if err == nil {
			close()
		}
		result <- err
	}()
	select {
	case <-result:
		t.Fatal("second task must wait")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	other, err := g.hold(context.Background(), "tenant:other-config", 1)
	require.NoError(t, err)
	other()
	release()
	release() // completion can run through more than one cleanup path
	require.Empty(t, g.states)
	next, err := g.hold(context.Background(), "tenant:config", 1)
	require.NoError(t, err)
	next()
}

func TestHoldSandboxTurnCapacitySharedBetweenSessionsAndReleasedOnFailure(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(123))
	repo := &fakeConfigRepo{entity: &types.TenantSandboxConfigEntity{Config: &types.TenantSandboxConfig{
		SandboxType: "docker", Docker: &types.DockerSandboxConfig{MaxConcurrentTurns: 1},
	}}}
	svc := &sessionService{sandboxConfigRepo: repo}
	release, err := svc.holdSandboxTurn(ctx, "first-session", t.Name())
	require.NoError(t, err)
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = svc.holdSandboxTurn(waitCtx, "other-session", t.Name())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	release()

	svc.sandboxMgr = &turnLeaseManager{beginErr: sandbox.ErrSessionRewindLocked}
	_, err = svc.holdSandboxTurn(ctx, "first-session", t.Name())
	require.ErrorIs(t, err, sandbox.ErrSessionRewindLocked)
	svc.sandboxMgr = nil
	release, err = svc.holdSandboxTurn(ctx, "other-session", t.Name())
	require.NoError(t, err)
	release()
}

func TestSandboxTurnCapacityConfigEditDoesNotCreateAnotherPool(t *testing.T) {
	g := sandboxTurnCapacity{states: make(map[string]*sandboxTurnCapacityState)}
	first, err := g.hold(context.Background(), "config", 2)
	require.NoError(t, err)
	second, err := g.hold(context.Background(), "config", 2)
	require.NoError(t, err)
	first()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = g.hold(ctx, "config", 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	second()
	next, err := g.hold(context.Background(), "config", 1)
	require.NoError(t, err)
	next()
}
