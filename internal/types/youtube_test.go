package types

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestYouTubeProxyRedactionAndMerge(t *testing.T) {
	cfg := &YouTubeTranscriptConfig{Enabled: true, KnowledgeBaseID: "kb", ProxyURL: "http://alice:secret@proxy.example:8080"}
	response := YouTubeTranscriptConfigForResponse(cfg)
	require.NotContains(t, response.ProxyURL, "secret")
	require.Contains(t, response.ProxyURL, "%2A")
	require.Contains(t, cfg.ProxyURL, "secret")
	for _, incoming := range []string{"", response.ProxyURL} {
		merged, err := MergeYouTubeProxy(incoming, cfg.ProxyURL, false)
		require.NoError(t, err)
		require.Equal(t, cfg.ProxyURL, merged)
	}
	cleared, err := MergeYouTubeProxy("", cfg.ProxyURL, true)
	require.NoError(t, err)
	require.Empty(t, cleared)
	_, err = MergeYouTubeProxy("file:///secret", cfg.ProxyURL, false)
	require.Error(t, err)
	_, err = MergeYouTubeProxy("http://alice:********@other.example", cfg.ProxyURL, false)
	require.Error(t, err)
	raw, err := json.Marshal(&Tenant{YouTubeTranscriptConfig: cfg})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
	value, err := cfg.Value()
	require.NoError(t, err)
	var restored YouTubeTranscriptConfig
	require.NoError(t, restored.Scan(value))
	require.Equal(t, *cfg, restored)
}
