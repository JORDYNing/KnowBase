package web_fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchLargeHTMLNoisePreservesCompleteArticle(t *testing.T) {
	// More than the old Agent limit before the article, and more noise after it.
	source := `<html><head><title>公众号文章</title><script>` + strings.Repeat("x", 3*1024*1024) +
		`</script><style>` + strings.Repeat("x", 1024*1024) + `</style></head><body><article id="js_content"><h1>正文</h1><p>FIRST_SECTION</p>` + strings.Repeat(`<p>完整的文章正文与证据。</p>`, 5000) + `<p>FINAL_SECTION</p></article><!--` + strings.Repeat("x", 1024*1024) + `--></body></html>`
	for _, markdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "pipeline", true: "agent"}[markdown], func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(source)), Request: req}, nil
			})}
			f := newTestFetcher(client)
			f.markdown = markdown
			content, err := f.Fetch(context.Background(), "https://mp.weixin.qq.com/s/example")
			require.NoError(t, err)
			require.Contains(t, content, "FIRST")
			require.Contains(t, content, "FINAL")
			require.NotContains(t, content, strings.Repeat("x", 100))
		})
	}
}

func TestReadPageBodyLimits(t *testing.T) {
	for _, tc := range []struct {
		name, kind, body string
		limit            int64
		want             ErrorCode
	}{
		{"html exact", "text/html", "<p>ok</p>", 9, ""},
		{"html over", "text/html", "<p>ok!</p>", 9, ErrorBodyTooLarge},
		{"plain exact", "text/plain", "abc", 3, ""},
		{"plain over", "text/plain", "abcd", 3, ErrorBodyTooLarge},
		{"sniff HTML", "", `<html><body><script>` + strings.Repeat("x", 1024) + `</script><p>kept</p></body></html>`, 128, ""},
		{"raw over", "text/html", "<script>" + strings.Repeat("x", maxDownloadSize) + "</script>", 1024, ErrorBodyTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := readPageBody(strings.NewReader(tc.body), tc.kind, tc.limit)
			code, _, _ := ErrorDetails(err)
			require.Equal(t, tc.want, code)
		})
	}
}

type failingPageReader struct{}

func (failingPageReader) Read([]byte) (int, error) { return 0, errors.New("broken connection") }
func TestReadPageBodyReadError(t *testing.T) {
	_, _, err := readPageBody(io.MultiReader(strings.NewReader("<p>incomplete"), failingPageReader{}), "text/html", 1024)
	code, retryable, _ := ErrorDetails(err)
	require.Equal(t, ErrorRead, code)
	require.True(t, retryable)
}
