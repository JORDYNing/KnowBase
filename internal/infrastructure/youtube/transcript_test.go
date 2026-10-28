package youtube

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestYouTubeNormalizeURL(t *testing.T) {
	for _, u := range []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLx", "https://youtu.be/dQw4w9WgXcQ?t=1", "https://youtube.com/shorts/dQw4w9WgXcQ", "https://m.youtube.com/live/dQw4w9WgXcQ"} {
		id, canonical, err := NormalizeURL(u)
		require.NoError(t, err)
		require.Equal(t, "dQw4w9WgXcQ", id)
		require.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", canonical)
	}
	for _, u := range []string{"file:///etc/passwd", "https://youtube.com.evil.test/watch?v=dQw4w9WgXcQ", "https://user@youtube.com/watch?v=dQw4w9WgXcQ", "https://youtube.com:443/watch?v=dQw4w9WgXcQ", "https://youtube.com/playlist?list=1", "--exec=bad"} {
		_, _, err := NormalizeURL(u)
		require.Error(t, err)
	}
}
func formats(language string) []subtitleFormat {
	return []subtitleFormat{{URL: "https://www.youtube.com/api/timedtext?lang=" + language, Ext: "json3"}}
}
func TestYouTubeSelectTrack(t *testing.T) {
	info := videoInfo{Language: "ja", Subtitles: map[string][]subtitleFormat{"en": formats("en"), "zh-Hant": formats("zh-Hant")}, AutomaticCaptions: map[string][]subtitleFormat{"zh-Hans": formats("zh-Hans"), "ja": formats("ja"), "fr": {{URL: "https://youtube.com/api/timedtext?lang=en&tlang=fr", Ext: "json3"}}}}
	got, err := selectTrack(info, "")
	require.NoError(t, err)
	require.Equal(t, "zh-Hant", got.Language)
	require.False(t, got.Automatic)
	got, err = selectTrack(info, "en")
	require.NoError(t, err)
	require.Equal(t, "en", got.Language)
	got, err = selectTrack(info, "zh-hans")
	require.NoError(t, err)
	require.Equal(t, "zh-Hans", got.Language)
	require.True(t, got.Automatic)
	_, err = selectTrack(info, "fr")
	require.ErrorContains(t, err, "没有 fr")
	delete(info.Subtitles, "zh-Hant")
	delete(info.AutomaticCaptions, "zh-Hans")
	got, err = selectTrack(info, "")
	require.NoError(t, err)
	require.Equal(t, "ja", got.Language)
	info.Language = ""
	got, err = selectTrack(info, "")
	require.NoError(t, err)
	require.Equal(t, "en", got.Language)
}
func TestYouTubeSubtitleParsingAndMarkdown(t *testing.T) {
	segments, err := parseJSON3([]byte(`{"events":[{"tStartMs":1000,"dDurationMs":3000,"segs":[{"utf8":"你好 &amp; 世界"}]},{"tStartMs":2000,"dDurationMs":3000,"segs":[{"utf8":"你好 &amp; 世界"}]},{"tStartMs":3000,"segs":[{"utf8":"你好 &amp; 世界 第二句"}]}]}`))
	require.NoError(t, err)
	segments = cleanSegments(segments)
	require.Len(t, segments, 2)
	require.Equal(t, "第二句", segments[1].Text)
	vtt, err := parseVTT("WEBVTT\n\n00:01.000 --> 00:02.000\n<c>你好</c>\n\n00:02.000 --> 00:03.000\n世界\n")
	require.NoError(t, err)
	require.Equal(t, "你好", cleanSegments(vtt)[0].Text)
	md := RenderMarkdown(&Transcript{VideoID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Title: "示例", Channel: "频道", ChannelURL: "https://youtube.com/@channel", UploadDate: "20261027", Duration: 60, Description: "原视频简介", Thumbnail: "https://i.ytimg.com/vi/a/hqdefault.jpg", Language: "zh-Hans", Automatic: true, FetchedAt: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), Segments: segments})
	for _, want := range []string{"# 示例", "视频链接：https://www.youtube.com/watch?v=dQw4w9WgXcQ", "频道：频道", "2026-10-27", "00:01:00", "原视频简介", "封面链接", "字幕语言：zh-Hans", "自动生成字幕", "&t=1s", "你好 & 世界", "第二句"} {
		require.Contains(t, md, want)
	}
	require.NotContains(t, RenderMarkdown(&Transcript{VideoID: "x"}), "发布日期")
}
func TestYouTubeFetchCancellationAndSizeLimit(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "extractor")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0700))
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := (&Client{Binary: binary}).Fetch(ctx, "https://youtu.be/dQw4w9WgXcQ", "", " ")
	require.Error(t, err)
	buf := &cappedBuffer{limit: 2}
	_, err = buf.Write([]byte("123"))
	require.Error(t, err)
}
func TestYouTubeSubtitleURLAllowlist(t *testing.T) {
	require.True(t, validSubtitleURL("https://www.youtube.com/api/timedtext?x=1"))
	for _, u := range []string{"http://www.youtube.com/x", "https://youtube.com.evil.test/x", "https://127.0.0.1/x", "https://user@www.youtube.com/x", "https://www.youtube.com:123/x"} {
		require.False(t, validSubtitleURL(u))
	}
}

func TestYouTubePreservesRealRepeatedSpeech(t *testing.T) {
	in := []Segment{{Start: 1, Duration: 2, Text: "你好"}, {Start: 10, Duration: 2, Text: "你好"}, {Start: 20, Duration: 2, Text: "你好 世界"}}
	got := cleanSegments(in)
	require.Len(t, got, 3)
	require.Equal(t, "你好 世界", got[2].Text)
}

func TestYouTubeCaptionTrackIDsDoNotBecomeLanguages(t *testing.T) {
	info := videoInfo{Language: "en", Subtitles: map[string][]subtitleFormat{
		"en-eEY6OEpapPo":      formats("en"),
		"zh-Hans-eEY6OEpapPo": formats("zh-Hans"),
	}, AutomaticCaptions: map[string][]subtitleFormat{"zh-Hans-orig": formats("zh-Hans")}}
	got, err := selectTrack(info, "")
	require.NoError(t, err)
	require.Equal(t, "zh-Hans", got.Language)
	require.False(t, got.Automatic)
	got, err = selectTrack(info, "zh-hans")
	require.NoError(t, err)
	require.Equal(t, "zh-Hans", got.Language)
	got, err = selectTrack(info, "en")
	require.NoError(t, err)
	require.Equal(t, "en", got.Language)
	delete(info.Subtitles, "zh-Hans-eEY6OEpapPo")
	delete(info.AutomaticCaptions, "zh-Hans-orig")
	got, err = selectTrack(info, "")
	require.NoError(t, err)
	require.Equal(t, "en", got.Language)
}

func TestYouTubeChineseLanguageAliases(t *testing.T) {
	require.True(t, languageMatches("zh-Hans", "zh-cn"))
	require.True(t, languageMatches("zh-TW", "zh-hant"))
	require.False(t, languageMatches("zh-Hant", "zh-hans"))
}

func TestYouTubeMetadataRetriesTemporaryFailure(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "extractor")
	// The executable fails once, then emits metadata; a fresh process must run.
	script := "#!/bin/sh\nif [ ! -f '" + dir + "/attempt' ]; then touch '" + dir + "/attempt'; echo 'HTTP Error 429' >&2; exit 1; fi\nprintf '{\"id\":\"dQw4w9WgXcQ\"}'\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0700))
	got, err := fetchMetadata(context.Background(), binary, nil, true)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"dQw4w9WgXcQ"}`, string(got))
}

func TestYouTubeMetadataPermanentFailureAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, message, want string
		cancel              bool
	}{
		{"private", "Private video bot", "视频不可访问", false},
		{"unavailable", "Video unavailable", "视频不可访问", false},
		{"cancel_backoff", "HTTP Error 429", "已取消或超时", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "extractor")
			count := filepath.Join(dir, "count")
			require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\necho attempt >> '"+count+"'\necho '"+tc.message+"' >&2\nexit 1\n"), 0700))
			ctx := context.Background()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				go func() {
					for {
						if _, err := os.Stat(count); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(5 * time.Millisecond):
						}
					}
				}()
			}
			_, err := fetchMetadata(ctx, binary, nil, true)
			require.ErrorContains(t, err, tc.want)
			attempts, err := os.ReadFile(count)
			require.NoError(t, err)
			require.Equal(t, "attempt\n", string(attempts))
		})
	}
}

func TestYouTubeMetadataRetryExhaustionDoesNotLeakProxy(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "extractor")
	count := filepath.Join(dir, "count")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\necho attempt >> '"+count+"'\necho 'Sign in to confirm you are not a bot https://secret:password@proxy.test' >&2\nexit 1\n"), 0700))
	_, err := fetchMetadata(context.Background(), binary, nil, true)
	require.ErrorContains(t, err, "当前代理出口")
	require.NotContains(t, err.Error(), "配置网络代理")
	require.NotContains(t, err.Error(), "secret")
	attempts, err := os.ReadFile(count)
	require.NoError(t, err)
	require.Equal(t, "attempt\nattempt\n", string(attempts))
}

type subtitleRoundTripFunc func(*http.Request) (*http.Response, error)

func (f subtitleRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestYouTubeSubtitleRetriesTransientErrors(t *testing.T) {
	for _, first := range []int{0, 429, 503} {
		t.Run(http.StatusText(first), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: subtitleRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if first == 0 {
						return nil, errors.New("temporary connection reset")
					}
					return &http.Response{StatusCode: first, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"events":[]}`)), Header: make(http.Header)}, nil
			})}
			got, err := downloadSubtitleWithClient(context.Background(), "https://www.youtube.com/api/timedtext?lang=en", true, client)
			require.NoError(t, err)
			require.Equal(t, `{"events":[]}`, string(got))
			require.Equal(t, 2, calls)
		})
	}
}

func TestYouTubeSubtitleDoesNotRetryPermanentFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", 403, ""}, {"too_large", 200, strings.Repeat("x", MaxOutputBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: subtitleRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			_, err := downloadSubtitleWithClient(context.Background(), "https://www.youtube.com/api/timedtext?lang=en", true, client)
			require.Error(t, err)
			require.Equal(t, 1, calls)
		})
	}
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("connection reset") })}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := downloadSubtitleWithClient(ctx, "https://www.youtube.com/api/timedtext?lang=en", true, client)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, calls)
	_, err = downloadSubtitleWithClient(context.Background(), "https://evil.test/timedtext", true, client)
	require.ErrorContains(t, err, "来源地址无效")
	require.Equal(t, 1, calls)
}
