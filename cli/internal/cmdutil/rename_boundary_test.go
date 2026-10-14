package cmdutil

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/cli/internal/config"
)

func TestEnvCredentials_IgnoreOldVariables(t *testing.T) {
	t.Setenv("GAMELIB_API_KEY", "")
	t.Setenv("GAMELIB_TOKEN", "")
	t.Setenv("GAMELIB_HOST", "")
	t.Setenv("WEKNORA_API_KEY", "old-key")
	t.Setenv("WEKNORA_TOKEN", "old-token")
	t.Setenv("WEKNORA_HOST", "https://old.example.com")
	f := &Factory{Config: func() (*config.Config, error) { return &config.Config{}, nil }}
	active, kind := EnvCredential()
	assert.False(t, active)
	assert.Empty(t, kind)
	c, handled, err := buildClientFromEnv(f)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, c)

	t.Setenv("GAMELIB_TOKEN", "new-token")
	_, handled, err = buildClientFromEnv(f)
	assert.True(t, handled)
	require.ErrorContains(t, err, "no host is available", "old host must not fill a new credential's host")
	t.Setenv("GAMELIB_HOST", "https://new.example.com")
	c, handled, err = buildClientFromEnv(f)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.NotNil(t, c)
	active, kind = EnvCredential()
	assert.True(t, active)
	assert.Equal(t, "GAMELIB_TOKEN", kind)
}

func TestProfileAndKB_IgnoreOldEnvironment(t *testing.T) {
	t.Setenv("GAMELIB_PROFILE", "")
	t.Setenv("GAMELIB_KB_ID", "")
	t.Setenv("WEKNORA_PROFILE", "old-profile")
	t.Setenv("WEKNORA_KB_ID", "old-kb")
	resolveKBChdir(t, t.TempDir())
	f := &Factory{}
	assert.Empty(t, f.ActiveProfile())
	_, err := f.ResolveKB(makeResolveKBCmd(t, ""))
	var typed *Error
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, CodeKBIDRequired, typed.Code)

	t.Setenv("GAMELIB_PROFILE", "new-profile")
	t.Setenv("GAMELIB_KB_ID", "new-kb")
	assert.Equal(t, "new-profile", f.ActiveProfile())
	kb, err := f.ResolveKB(makeResolveKBCmd(t, ""))
	require.NoError(t, err)
	assert.Equal(t, "new-kb", kb)
}

func TestOutputAndLogging_IgnoreOldEnvironment(t *testing.T) {
	t.Setenv("GAMELIB_FORMAT", "")
	t.Setenv("GAMELIB_LOG_LEVEL", "")
	t.Setenv("WEKNORA_FORMAT", "text")
	t.Setenv("WEKNORA_LOG_LEVEL", "debug")
	opts := &FormatOptions{}
	opts.ResolveDefault(false)
	assert.Equal(t, FormatJSON, opts.Mode)
	cmd := &cobra.Command{}
	AddLogLevelFlag(cmd)
	level, _ := ResolveLogLevel(cmd, &bytes.Buffer{})
	assert.Equal(t, "error", level)

	t.Setenv("GAMELIB_FORMAT", "ndjson")
	t.Setenv("GAMELIB_LOG_LEVEL", "info")
	opts = &FormatOptions{}
	opts.ResolveDefault(false)
	assert.Equal(t, FormatNDJSON, opts.Mode)
	level, _ = ResolveLogLevel(cmd, &bytes.Buffer{})
	assert.Equal(t, "info", level)
}
