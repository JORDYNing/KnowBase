package web_fetch

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Replays a locally captured response without storing third-party article
// content in the repository. Set KNOWBASE_WECHAT_REPLAY_HTML to opt in.
func TestWeChatCapturedLargeArticle(t *testing.T) {
	path := os.Getenv("KNOWBASE_WECHAT_REPLAY_HTML")
	if path == "" {
		t.Skip("set KNOWBASE_WECHAT_REPLAY_HTML to a captured WeChat HTML response")
	}
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, len(source), 2*1024*1024, "capture must reproduce the old download-limit failure")
	cleaned, _, err := readPageBody(strings.NewReader(string(source)), "text/html", maxAgentBodySize)
	require.NoError(t, err)
	t.Logf("raw_bytes=%d retained_html_bytes=%d", len(source), len(cleaned))
	for _, markdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "pipeline", true: "agent"}[markdown], func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(string(source))), Request: req}, nil
			})}
			f := newTestFetcher(client)
			f.markdown = markdown
			content, err := f.Fetch(t.Context(), "https://mp.weixin.qq.com/s/1SuM8lrTtWz_i9UCU9Jukw")
			require.NoError(t, err)
			for _, marker := range []string{"本期榜单基于游戏日报", "原神千星奇域一周年", "UEFN", "李道明", "欢迎更多行业朋友和我们交流"} {
				require.Contains(t, content, marker)
			}
			t.Logf("extracted_characters=%d", len([]rune(content)))
		})
	}
}

func TestWeChatLiveLargeArticle(t *testing.T) {
	if os.Getenv("KNOWBASE_WECHAT_LIVE_TEST") != "1" {
		t.Skip("set KNOWBASE_WECHAT_LIVE_TEST=1 for the live production fetcher check")
	}
	content, err := NewFetcher().Fetch(t.Context(), "https://mp.weixin.qq.com/s/1SuM8lrTtWz_i9UCU9Jukw")
	require.NoError(t, err)
	require.Contains(t, content, "本期榜单基于游戏日报")
	require.Contains(t, content, "欢迎更多行业朋友和我们交流")
	t.Logf("live_extracted_characters=%d", len([]rune(content)))
}
