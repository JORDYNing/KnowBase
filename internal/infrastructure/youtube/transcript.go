// Package youtube fetches public subtitle tracks without downloading video or audio.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MaxOutputBytes = 8 << 20

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var languagePattern = regexp.MustCompile(`^[a-zA-Z]{2,3}(?:-[a-zA-Z0-9]+)*$`)
var tagPattern = regexp.MustCompile(`<[^>]*>`)

func NormalizeURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Port() != "" {
		return "", "", fmt.Errorf("请提供有效的 YouTube 视频链接")
	}
	host := strings.ToLower(u.Hostname())
	id := ""
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "live" || parts[0] == "embed") {
			id = parts[1]
		}
	}
	if !videoIDPattern.MatchString(id) {
		return "", "", fmt.Errorf("仅支持 YouTube 单视频链接")
	}
	return id, "https://www.youtube.com/watch?v=" + id, nil
}

type Segment struct {
	Start    float64
	Text     string
	Duration float64
}
type Transcript struct {
	VideoID, URL, Title, Channel, ChannelURL, UploadDate, Description, Thumbnail, Language string
	Duration                                                                               float64
	Automatic                                                                              bool
	FetchedAt                                                                              time.Time
	Segments                                                                               []Segment
}
type Fetcher interface {
	Fetch(context.Context, string, string, string) (*Transcript, error)
}
type Client struct{ Binary string }

func NewClient() *Client { return &Client{Binary: "yt-dlp"} }

type subtitleFormat struct {
	URL  string `json:"url"`
	Ext  string `json:"ext"`
	Name string `json:"name"`
}
type videoInfo struct {
	ID                string                      `json:"id"`
	Title             string                      `json:"title"`
	Channel           string                      `json:"channel"`
	ChannelURL        string                      `json:"channel_url"`
	UploadDate        string                      `json:"upload_date"`
	Duration          float64                     `json:"duration"`
	Description       string                      `json:"description"`
	Thumbnail         string                      `json:"thumbnail"`
	Language          string                      `json:"language"`
	Subtitles         map[string][]subtitleFormat `json:"subtitles"`
	AutomaticCaptions map[string][]subtitleFormat `json:"automatic_captions"`
}
type track struct {
	Language  string
	Automatic bool
	Format    subtitleFormat
}

func NormalizeLanguage(language string) (string, error) {
	l := strings.ToLower(strings.TrimSpace(language))
	switch l {
	case "中文", "汉语", "chinese":
		l = "zh"
	case "简体中文", "简体", "simplified chinese":
		l = "zh-hans"
	case "繁体中文", "繁体", "traditional chinese":
		l = "zh-hant"
	case "英文", "英语", "english":
		l = "en"
	case "日文", "日语", "japanese":
		l = "ja"
	case "韩文", "韩语", "korean":
		l = "ko"
	case "法语", "french":
		l = "fr"
	case "德语", "german":
		l = "de"
	case "西班牙语", "spanish":
		l = "es"
	}
	if l != "" && !languagePattern.MatchString(l) {
		return "", fmt.Errorf("字幕语言无效，请使用中文、英文或语言代码")
	}
	return l, nil
}
func family(l string) string { return strings.Split(strings.ToLower(l), "-")[0] }
func languageMatches(actual, wanted string) bool {
	actual = strings.TrimSuffix(strings.ToLower(actual), "-orig")
	if actual == wanted {
		return true
	}
	if !strings.Contains(wanted, "-") {
		return family(actual) == wanted
	}
	if wanted == "zh-hans" || wanted == "zh-cn" || wanted == "zh-sg" {
		return actual == "zh-hans" || actual == "zh-cn" || actual == "zh-sg"
	}
	if wanted == "zh-hant" || wanted == "zh-tw" || wanted == "zh-hk" {
		return actual == "zh-hant" || actual == "zh-tw" || actual == "zh-hk"
	}
	return false
}

// yt-dlp keys may append a caption track ID (en-eEY6OEpapPo), so the
// signed caption URL's lang parameter is authoritative for matching and storage.
var captionLanguagePattern = regexp.MustCompile(`(?i)^[a-z]{2,3}(?:-(?:[a-z]{2}|[0-9]{3}|[a-z]{4}))*$`)

func captionLanguage(key string, u *url.URL) string {
	if language := u.Query().Get("lang"); language != "" {
		if captionLanguagePattern.MatchString(language) {
			return language
		}
		return ""
	}
	key = strings.TrimSuffix(key, "-orig")
	if captionLanguagePattern.MatchString(key) {
		return key
	}
	parts := strings.Split(key, "-")
	for len(parts) > 1 {
		parts = parts[:len(parts)-1]
		language := strings.Join(parts, "-")
		if captionLanguagePattern.MatchString(language) {
			return language
		}
	}
	return ""
}
func selectTrack(info videoInfo, wanted string) (track, error) {
	var candidates []track
	for _, src := range []struct {
		values    map[string][]subtitleFormat
		automatic bool
	}{{info.Subtitles, false}, {info.AutomaticCaptions, true}} {
		keys := make([]string, 0, len(src.values))
		for key := range src.values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "live_chat" {
				continue
			}
			var chosen *subtitleFormat
			chosenLanguage := ""
			for _, format := range src.values[key] {
				f := format
				u, err := url.Parse(f.URL)
				if err != nil || u.Query().Get("tlang") != "" {
					continue
				}
				language := captionLanguage(key, u)
				if language == "" || (wanted != "" && !languageMatches(language, wanted)) {
					continue
				}
				if f.Ext != "json3" && f.Ext != "vtt" {
					continue
				}
				if chosen == nil || f.Ext == "json3" {
					chosen = &f
					chosenLanguage = language
				}
				if f.Ext == "json3" {
					break
				}
			}
			if chosen != nil {
				candidates = append(candidates, track{chosenLanguage, src.automatic, *chosen})
			}
		}
	}
	if len(candidates) == 0 {
		if wanted != "" {
			return track{}, fmt.Errorf("视频没有 %s 字幕", wanted)
		}
		return track{}, fmt.Errorf("视频没有可用字幕")
	}
	rank := func(t track) int {
		if wanted != "" {
			if strings.EqualFold(t.Language, wanted) {
				return 0
			}
			return 1
		}
		if family(t.Language) == "zh" {
			return 0
		}
		if info.Language != "" && family(t.Language) == family(info.Language) {
			return 1
		}
		if family(t.Language) == "en" {
			return 2
		}
		return 3
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Automatic != b.Automatic {
			return !a.Automatic
		}
		return a.Language < b.Language
	})
	return candidates[0], nil
}

type cappedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.exceeded = true
		return 0, fmt.Errorf("字幕响应超过大小限制")
	}
	return b.Buffer.Write(p)
}
func (c *Client) Fetch(ctx context.Context, raw, language, proxy string) (*Transcript, error) {
	id, canonical, err := NormalizeURL(raw)
	if err != nil {
		return nil, err
	}
	wanted, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	binary := c.Binary
	if binary == "" {
		binary = "yt-dlp"
	}
	args := []string{"--ignore-config", "--no-playlist", "--skip-download", "--dump-single-json", "--no-warnings", "--socket-timeout", "20", "--retries", "0", "--extractor-retries", "0"}
	if proxy != "" {
		args = append(args, "--proxy", proxy)
	}
	args = append(args, "--", canonical)
	metadata, err := fetchMetadata(ctx, binary, args, proxy != "")
	if err != nil {
		return nil, err
	}
	var info videoInfo
	if err := json.Unmarshal(metadata, &info); err != nil {
		return nil, fmt.Errorf("视频信息响应格式无效")
	}
	if info.ID != id {
		return nil, fmt.Errorf("视频信息与请求的视频不一致")
	}
	selected, err := selectTrack(info, wanted)
	if err != nil {
		return nil, err
	}
	rawSub, err := downloadSubtitle(ctx, selected.Format.URL, proxy)
	if err != nil {
		return nil, err
	}
	var segments []Segment
	if selected.Format.Ext == "json3" {
		segments, err = parseJSON3(rawSub)
	} else {
		segments, err = parseVTT(string(rawSub))
	}
	if err != nil {
		return nil, err
	}
	segments = cleanSegments(segments)
	if len(segments) == 0 {
		return nil, fmt.Errorf("字幕内容为空")
	}
	return &Transcript{VideoID: id, URL: canonical, Title: info.Title, Channel: info.Channel, ChannelURL: info.ChannelURL, UploadDate: info.UploadDate, Duration: info.Duration, Description: info.Description, Thumbnail: info.Thumbnail, Language: strings.TrimSuffix(selected.Language, "-orig"), Automatic: selected.Automatic, FetchedAt: time.Now().UTC(), Segments: segments}, nil
}

// Both retry loops share Fetch's deadline; cancellation also interrupts backoff.
func waitForRetry(ctx context.Context) error {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func limitedByYouTube(msg string) bool {
	return strings.Contains(msg, "429") || strings.Contains(msg, "sign in") || strings.Contains(msg, "bot")
}
func inaccessibleVideo(msg string) bool {
	return strings.Contains(msg, "unavailable") || strings.Contains(msg, "private") || strings.Contains(msg, "removed")
}
func temporaryMetadataError(msg string) bool {
	if inaccessibleVideo(msg) {
		return false
	}
	if limitedByYouTube(msg) {
		return true
	}
	for _, marker := range []string{"timed out", "timeout", "connection", "network is unreachable", "temporary failure", "remote end closed", "http error 500", "http error 502", "http error 503", "http error 504"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
func restrictionError(proxyConfigured bool) error {
	if proxyConfigured {
		return fmt.Errorf("YouTube 暂时限制当前代理出口的字幕获取，自动重试后仍失败，请稍后重试或更换代理线路")
	}
	return fmt.Errorf("YouTube 暂时限制字幕获取，自动重试后仍失败，请稍后重试或配置网络代理")
}
func fetchMetadata(ctx context.Context, binary string, args []string, proxyConfigured bool) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("字幕获取已取消或超时: %w", err)
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		stdout := &cappedBuffer{limit: MaxOutputBytes}
		stderr := &cappedBuffer{limit: 64 << 10}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		err := cmd.Run()
		if err == nil {
			return stdout.Bytes(), nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("字幕获取已取消或超时: %w", ctx.Err())
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("服务器尚未安装 YouTube 字幕获取组件")
		}
		if stdout.exceeded || stderr.exceeded {
			return nil, fmt.Errorf("字幕响应超过大小限制")
		}
		msg := strings.ToLower(stderr.String())
		if attempt == 0 && temporaryMetadataError(msg) {
			if err := waitForRetry(ctx); err != nil {
				return nil, fmt.Errorf("字幕获取已取消或超时: %w", err)
			}
			continue
		}
		if inaccessibleVideo(msg) {
			return nil, fmt.Errorf("YouTube 视频不可访问")
		}
		if limitedByYouTube(msg) {
			return nil, restrictionError(proxyConfigured)
		}
		return nil, fmt.Errorf("YouTube 视频信息获取失败，请检查网络或代理配置")
	}
	panic("unreachable")
}

func validSubtitleURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "youtube.com" || strings.HasSuffix(h, ".youtube.com") || h == "googlevideo.com" || strings.HasSuffix(h, ".googlevideo.com")
}
func downloadSubtitle(ctx context.Context, raw, proxy string) ([]byte, error) {
	if !validSubtitleURL(raw) {
		return nil, fmt.Errorf("字幕来源地址无效")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("代理地址无效")
		}
		transport.Proxy = http.ProxyURL(u)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 40 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !validSubtitleURL(req.URL.String()) {
			return fmt.Errorf("字幕重定向地址无效")
		}
		return nil
	}}
	return downloadSubtitleWithClient(ctx, raw, proxy != "", client)
}
func downloadSubtitleWithClient(ctx context.Context, raw string, proxyConfigured bool, client *http.Client) ([]byte, error) {
	if !validSubtitleURL(raw) {
		return nil, fmt.Errorf("字幕来源地址无效")
	}
	for attempt := 0; attempt < 2; attempt++ {
		data, retry, err := downloadSubtitleAttempt(ctx, raw, proxyConfigured, client)
		if err == nil || !retry || attempt == 1 {
			return data, err
		}
		if err := waitForRetry(ctx); err != nil {
			return nil, err
		}
	}
	panic("unreachable")
}
func downloadSubtitleAttempt(ctx context.Context, raw string, proxyConfigured bool, client *http.Client) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, false, fmt.Errorf("字幕地址无效")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// Redirect validation is a permanent safety failure, never a retry trigger.
		var urlErr *url.Error
		if errors.As(err, &urlErr) && strings.Contains(urlErr.Err.Error(), "字幕重定向地址无效") {
			return nil, false, fmt.Errorf("字幕重定向地址无效")
		}
		return nil, true, fmt.Errorf("字幕下载失败，自动重试后仍失败，请检查网络或代理线路")
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return nil, true, restrictionError(proxyConfigured)
	}
	if res.StatusCode != 200 {
		return nil, res.StatusCode >= 500 && res.StatusCode <= 599, fmt.Errorf("字幕下载失败（HTTP %d）", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxOutputBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, fmt.Errorf("字幕读取失败，自动重试后仍失败，请稍后重试")
	}
	if len(data) > MaxOutputBytes {
		return nil, false, fmt.Errorf("字幕超过大小限制")
	}
	return data, false, nil
}

func parseJSON3(data []byte) ([]Segment, error) {
	var doc struct {
		Events []struct {
			Start    float64 `json:"tStartMs"`
			Duration float64 `json:"dDurationMs"`
			Segs     []struct {
				Text string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("字幕格式无效")
	}
	out := []Segment{}
	for _, e := range doc.Events {
		var text strings.Builder
		for _, s := range e.Segs {
			text.WriteString(s.Text)
		}
		out = append(out, Segment{Start: e.Start / 1000, Text: text.String(), Duration: e.Duration / 1000})
	}
	return out, nil
}
func parseTimestamp(s string) (float64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid timestamp")
	}
	result := float64(0)
	for _, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid timestamp")
		}
		result = result*60 + n
	}
	return result, nil
}
func parseVTT(text string) ([]Segment, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var out []Segment
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], "-->") {
			continue
		}
		linesAtTime := lines[i]
		start, err := parseTimestamp(strings.Split(lines[i], "-->")[0])
		if err != nil {
			return nil, fmt.Errorf("字幕时间戳无效")
		}
		var body []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
			body = append(body, lines[i])
		}
		endText := strings.Fields(strings.Split(linesAtTime, "-->")[1])
		end := start
		if len(endText) > 0 {
			if parsed, e := parseTimestamp(endText[0]); e == nil {
				end = parsed
			}
		}
		out = append(out, Segment{Start: start, Text: strings.Join(body, " "), Duration: end - start})
	}
	return out, nil
}
func cleanSegments(in []Segment) []Segment {
	sort.SliceStable(in, func(i, j int) bool { return in[i].Start < in[j].Start })
	var out []Segment
	previous := ""
	previousEnd := float64(-1)
	previousStart := float64(-1)
	for _, s := range in {
		text := strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(s.Text, ""))), " ")
		if text == "" {
			continue
		}
		full := text
		// Only overlapping cues can be rolling-caption repeats. A repeated sentence
		// later in the video is real transcript content and must survive.
		overlaps := s.Start < previousEnd || s.Start == previousStart
		if overlaps && text == previous {
			previousEnd = s.Start + s.Duration
			previousStart = s.Start
			continue
		}
		if overlaps && previous != "" && strings.HasPrefix(text, previous+" ") {
			text = strings.TrimSpace(strings.TrimPrefix(text, previous))
		}
		previous = full
		previousStart = s.Start
		previousEnd = s.Start + s.Duration
		s.Text = text
		out = append(out, s)
	}
	return out
}
func escapeText(s string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "*", "\\*", "_", "\\_", "`", "\\`").Replace(s)
}
func timestamp(seconds float64) string {
	n := int(seconds)
	if n < 0 {
		n = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", n/3600, (n%3600)/60, n%60)
}
func RenderMarkdown(t *Transcript) string {
	var b strings.Builder
	title := t.Title
	if title == "" {
		title = t.VideoID
	}
	fmt.Fprintf(&b, "# %s\n\n## 原视频信息\n\n", escapeText(strings.Join(strings.Fields(title), " ")))
	fmt.Fprintf(&b, "- 视频链接：%s\n- 视频 ID：%s\n", t.URL, t.VideoID)
	for _, v := range [][2]string{{"频道", t.Channel}, {"频道链接", safePublicURL(t.ChannelURL)}, {"发布日期", formatDate(t.UploadDate)}, {"封面链接", safePublicURL(t.Thumbnail)}} {
		if v[1] != "" {
			if strings.HasSuffix(v[0], "链接") {
				fmt.Fprintf(&b, "- %s：<%s>\n", v[0], strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(v[1]))
			} else {
				fmt.Fprintf(&b, "- %s：%s\n", v[0], escapeText(v[1]))
			}
		}
	}
	if t.Duration > 0 {
		fmt.Fprintf(&b, "- 时长：%s\n", timestamp(t.Duration))
	}
	if t.Description != "" {
		fmt.Fprintf(&b, "\n### 视频简介\n\n%s\n", escapeText(t.Description))
	}
	kind := "人工字幕"
	if t.Automatic {
		kind = "自动生成字幕"
	}
	fmt.Fprintf(&b, "\n## 字幕信息\n\n- 字幕语言：%s\n- 字幕类型：%s\n- 获取时间：%s\n\n## 完整字幕\n\n", t.Language, kind, t.FetchedAt.UTC().Format(time.RFC3339))
	for _, s := range t.Segments {
		fmt.Fprintf(&b, "[%s](%s&t=%ds) %s\n\n", timestamp(s.Start), t.URL, int(s.Start), escapeText(s.Text))
	}
	return b.String()
}
func safePublicURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Hostname() == "" {
		return ""
	}
	return u.String()
}
func formatDate(raw string) string {
	if t, err := time.Parse("20060102", raw); err == nil {
		return t.Format("2006-01-02")
	}
	return ""
}
