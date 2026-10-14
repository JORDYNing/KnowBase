package compat_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/cli/internal/compat"
)

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	want := &compat.Info{ServerVersion: "1.2.3", ProbedAt: time.Now().UTC().Truncate(time.Second)}
	if err := compat.SaveCache(want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	got, fresh, err := compat.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !fresh {
		t.Error("just-saved cache should be fresh")
	}
	if got == nil || got.ServerVersion != want.ServerVersion {
		t.Errorf("ServerVersion = %v, want %q", got, want.ServerVersion)
	}

	// File should live at $XDG_CACHE_HOME/gamelib/server-info.yaml
	p := filepath.Join(dir, "gamelib", "server-info.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("cache file not written at new path: %v", err)
	}
}

func TestCache_IgnoresOldNamespace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	oldPath := filepath.Join(dir, "weknora", "server-info.yaml")
	oldData := []byte("server_version: 1.2.3\nprobed_at: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, oldData, 0o600); err != nil {
		t.Fatal(err)
	}
	info, fresh, err := compat.LoadCache()
	if err != nil || info != nil || fresh {
		t.Fatalf("old cache must not be loaded: info=%v fresh=%v err=%v", info, fresh, err)
	}
	if err := compat.SaveCache(&compat.Info{ServerVersion: "9.8.7", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(oldPath)
	if err != nil || string(got) != string(oldData) {
		t.Fatalf("old cache must remain untouched: data=%q err=%v", got, err)
	}
}

func TestCache_Stale(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	old := &compat.Info{ServerVersion: "1.2.3", ProbedAt: time.Now().Add(-25 * time.Hour)}
	if err := compat.SaveCache(old); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	_, fresh, err := compat.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if fresh {
		t.Error("25h old cache should be stale")
	}
}

func TestCache_Missing(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	info, fresh, err := compat.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache should not error on missing: %v", err)
	}
	if fresh {
		t.Error("missing cache must not be fresh")
	}
	if info != nil {
		t.Errorf("missing cache info should be nil, got %v", info)
	}
}
