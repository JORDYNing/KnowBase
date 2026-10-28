package im

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const imYouTubeImportTool = "youtube_transcript_import"

// WithYouTubeImportService wires the importer before the IM service receives traffic.
func (s *Service) WithYouTubeImportService(service interfaces.YouTubeImportService) *Service {
	s.youTubeImportService = service
	return s
}

func agentAllowsYouTubeImport(agent *types.CustomAgent) bool {
	return agent != nil && slices.Contains(agent.Config.AllowedTools, imYouTubeImportTool)
}

var imURLPattern = regexp.MustCompile(`https?://[^\s<>"'，。；！？]+`)
var imYouTubeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Only URLs in the current user message trigger imports. Quotes/history never do.
func imYouTubeURLs(content string) []string {
	var result []string
	seen := map[string]bool{}
	for _, raw := range imURLPattern.FindAllString(content, -1) {
		raw = strings.TrimRight(raw, ").,;!?]}>）】")
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.Port() != "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		var id string
		switch host {
		case "youtu.be":
			id = strings.Trim(u.Path, "/")
		case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
			if u.Path == "/watch" {
				id = u.Query().Get("v")
			} else {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "live" || parts[0] == "embed") {
					id = parts[1]
				}
			}
		}
		if !imYouTubeIDPattern.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, "https://www.youtube.com/watch?v="+id)
	}
	return result
}

var imLanguageCode = regexp.MustCompile(`(?i)(?:language|lang|语言|字幕语言)\s*[:=：为是]?\s*([a-z]{2,3}(?:-[a-z0-9]{2,8})*)(?:\b|$)`)
var imBareLanguageCode = regexp.MustCompile(`(?i)^\s*([a-z]{2,3}(?:-[a-z0-9]{2,8})*)\s*$`)
var imCaptionLanguageCode = regexp.MustCompile(`(?i)\b([a-z]{2,3}(?:-[a-z0-9]{2,8})*)\s*(?:字幕|逐字稿)|(?:字幕|逐字稿)\s*[:=：]?\s*([a-z]{2,3}(?:-[a-z0-9]{2,8})*)\b`)
var imExplicitLanguageRequest = regexp.MustCompile(`(?i)^\s*[\p{Han}]{1,8}(?:语|文)\s*$|(?:字幕|逐字稿|subtitles?|captions?|transcripts?)\s*(?:语言|language|为|是|用|使用|[:=：]|in\b)|[\p{Han}]{1,8}(?:语|文)\s*(?:字幕|逐字稿)|(?:^|\s)(?:请)?(?:用|使用)\s*[\p{Han}]{1,8}(?:语|文)\s*$|\b[a-z]+\s+(?:subtitles?|captions?|transcripts?)\b|\b(?:language|lang)\s*[:=]`)

// A conservative deterministic parser: unknown explicit requests are left to the
// agent instead of silently switching to Chinese. The provider enforces language
// selection without fallback.
func imYouTubeLanguage(content string) (string, bool) {
	text := strings.ToLower(imURLPattern.ReplaceAllString(content, ""))
	for _, pattern := range []*regexp.Regexp{imLanguageCode, imBareLanguageCode, imCaptionLanguageCode} {
		if match := pattern.FindStringSubmatch(text); len(match) > 1 {
			for _, code := range match[1:] {
				if code != "" {
					return code, true
				}
			}
		}
	}
	languages := []struct {
		code  string
		names []string
	}{
		{"zh-Hans", []string{"简体中文", "简体字幕", "simplified chinese"}},
		{"zh-Hant", []string{"繁体中文", "繁体字幕", "traditional chinese"}},
		{"zh", []string{"中文", "汉语", "汉文", "chinese"}},
		{"en", []string{"英文", "英语", "english"}},
		{"ja", []string{"日文", "日语", "japanese"}},
		{"ko", []string{"韩文", "韩语", "korean"}},
		{"fr", []string{"法文", "法语", "french"}},
		{"de", []string{"德文", "德语", "german"}},
		{"es", []string{"西班牙语", "西语", "spanish"}},
		{"pt", []string{"葡萄牙语", "葡语", "portuguese"}},
		{"ru", []string{"俄文", "俄语", "russian"}},
		{"ar", []string{"阿拉伯语", "arabic"}},
		{"hi", []string{"印地语", "hindi"}},
		{"it", []string{"意大利语", "italian"}},
		{"th", []string{"泰文", "泰语", "thai"}},
		{"vi", []string{"越南语", "vietnamese"}},
	}
	// A video's spoken-language description does not select the subtitle track.
	// Only an explicit caption-language phrase or standalone language selects it.
	found := ""
	for _, language := range languages {
		for _, name := range language.names {
			n := regexp.QuoteMeta(name)
			explicit := regexp.MustCompile(`(?i)(?:` + n + `)\s*(?:字幕|逐字稿|subtitles?|captions?|transcripts?)|(?:字幕|逐字稿|subtitles?|captions?|transcripts?)(?:语言|language)?\s*(?:[:=：为是]|用|使用|in\b)?\s*(?:` + n + `)|^\s*(?:请)?(?:用|使用|以)?\s*(?:` + n + `)\s*$`)
			if !explicit.MatchString(text) {
				continue
			}
			if found != "" && !strings.HasPrefix(found, language.code) {
				return "", false
			}
			if found == "" {
				found = language.code
			}
			break
		}
	}
	if found != "" {
		return found, true
	}
	if imExplicitLanguageRequest.MatchString(text) {
		// Ordinary English requests such as "get transcripts" specify no language.
		plain := regexp.MustCompile(`(?i)\b(?:get|fetch|save|import|the|video|youtube|download|extract)\s+(?:subtitles?|captions?|transcripts?)\b`).ReplaceAllString(text, "")
		if imExplicitLanguageRequest.MatchString(plain) {
			return "", false
		}
	}
	return "", true
}

func imYouTubeLinksOnly(content string) bool {
	remaining := imURLPattern.ReplaceAllString(content, "")
	return strings.Trim(remaining, " \t\r\n()[]<>，。；！？、,.;!?）】（【") == ""
}

func (s *Service) imYouTubeResultReply(result *types.YouTubeImportResult) string {
	title := result.Title
	if title == "" {
		title = result.VideoID
	}
	text := ""
	if result.Status == types.ParseStatusCompleted {
		summary := strings.TrimSpace(result.Summary)
		if result.SummaryStatus != types.SummaryStatusCompleted || summary == "" {
			summary = "未生成"
			if result.SummaryStatus == types.SummaryStatusFailed {
				summary = "生成失败"
			}
		}
		text = fmt.Sprintf("视频标题：%s\n发布者：%s\n发布时间：%s\n视频总结：%s", title, result.Channel, result.PublishedAt, summary)
	} else {
		text = fmt.Sprintf("%s：已保存，正在处理", title)
	}
	return text + s.imYouTubeDocumentReference(result)
}

func (s *Service) imYouTubeDocumentReference(result *types.YouTubeImportResult) string {
	if link := s.imDocumentLink(result.DocumentURL); link != "" {
		return "\n文档：" + link
	}
	return "\n文档 ID：" + result.KnowledgeID
}

// importYouTubeMessage returns true when this request has no further QA work.
// It runs inside the existing bounded queue, preserving rate limit and /stop.
func (s *Service) importYouTubeMessage(req *qaRequest) bool {
	if s.youTubeImportService == nil || req.msg == nil || req.tenant == nil ||
		req.tenant.YouTubeTranscriptConfig == nil || !req.tenant.YouTubeTranscriptConfig.Enabled ||
		!agentAllowsYouTubeImport(req.agent) || (req.msg.Platform != PlatformFeishu && req.msg.Platform != PlatformLark) {
		return false
	}
	urls := imYouTubeURLs(req.msg.Content)
	if len(urls) == 0 {
		return false
	}
	if len(urls) > 5 {
		_ = req.adapter.SendReply(imOutboundContext(req.ctx), req.msg, &ReplyMessage{Content: "每条消息最多导入 5 个不同视频，请拆分消息后重试；本批尚未导入。", IsFinal: true})
		return true
	}
	language, understood := imYouTubeLanguage(req.msg.Content)
	if !understood {
		req.ctx = types.WithYouTubeExplicitLanguageRequired(req.ctx)
		copiedMessage := *req.msg
		hint := "用户本条消息涉及指定语言，自动导入尚未执行。请先确定用户要求的字幕语言，再调用 youtube_transcript_import 并显式传入 language。不要将总结或回答的语言当作字幕语言；无法确定时请用户明确字幕语言代码。"
		if req.msg.Quote != nil {
			hint += "\n用户原引用消息：\n" + req.msg.Quote.Content
		}
		copiedMessage.Quote = &QuotedMessage{Content: hint, IsBotMessage: true}
		req.msg = &copiedMessage
		return false
	}

	// Remote /stop uses a marker before a QA assistant message exists.
	batchTimeout := 30 * time.Minute
	if s.youtubeBatchTimeout > 0 {
		batchTimeout = s.youtubeBatchTimeout
	}
	batchCtx, stopBatch := context.WithTimeout(req.ctx, batchTimeout)
	defer stopBatch()
	watchCtx, stopWatch := context.WithCancel(batchCtx)
	defer stopWatch()
	if s.redis != nil {
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-watchCtx.Done():
					return
				case <-ticker.C:
					if s.checkAndClearStopMarker(watchCtx, req.userKey) {
						req.cancel()
						return
					}
				}
			}
		}()
	}
	send := func(text string) {
		if err := req.adapter.SendReply(imOutboundContext(req.ctx), req.msg, &ReplyMessage{Content: text, IsFinal: true}); err != nil {
			logger.Warnf(req.ctx, "[IM] YouTube import reply failed: %v", err)
		}
	}
	send(fmt.Sprintf("正在获取 %d 个 YouTube 视频的字幕并保存到知识库，解析和总结完成后将发送结果。", len(urls)))
	var summaries []string
	completed := 0
	for index, videoURL := range urls {
		if batchCtx.Err() != nil {
			reason := "已取消"
			if req.ctx.Err() == nil {
				reason = "已超时"
			}
			send(fmt.Sprintf("YouTube 字幕入库%s：已完成 %d 个，未处理 %d 个。", reason, completed, len(urls)-index))
			return true
		}
		result, err := s.youTubeImportService.ImportFromIM(types.WithYouTubeImportProgress(batchCtx, func(result *types.YouTubeImportResult) {
			send(fmt.Sprintf("视频标题：%s\n发布者：%s\n发布时间：%s\n视频总结中，完成后推送。", result.Title, result.Channel, result.PublishedAt))
		}), videoURL, language)
		if batchCtx.Err() != nil {
			reason := "已取消"
			if req.ctx.Err() == nil {
				reason = "已超时"
			}
			reply := fmt.Sprintf("YouTube 字幕入库%s：已完成 %d 个，当前视频未完成确认 1 个，未处理 %d 个。", reason, completed, len(urls)-index-1)
			if result != nil {
				reply += "\n已保存的文档可查看处理状态。" + s.imYouTubeDocumentReference(result)
			}
			send(reply)
			return true
		}
		reply := ""
		if err != nil {
			reply = fmt.Sprintf("YouTube 字幕入库失败：%s\n%s", videoURL, err.Error())
			if result != nil {
				reply += s.imYouTubeDocumentReference(result)
			}
		} else if result == nil {
			reply = "YouTube 字幕入库失败：" + videoURL + "\n服务未返回导入结果。"
		} else {
			reply = s.imYouTubeResultReply(result)
			completed++
		}
		summaries = append(summaries, reply)
		send(reply)
	}
	if imYouTubeLinksOnly(req.msg.Content) && req.msg.FileKey == "" {
		s.persistYouTubeImportReply(req, strings.Join(summaries, "\n\n"))
		return true
	}
	// Work on request-owned copies: other conversations may share the saved agent.
	copiedAgent := *req.agent
	copiedAgent.Config = req.agent.Config
	copiedAgent.Config.AllowedTools = nil
	for _, tool := range req.agent.Config.AllowedTools {
		if tool != imYouTubeImportTool {
			copiedAgent.Config.AllowedTools = append(copiedAgent.Config.AllowedTools, tool)
		}
	}
	// An empty whitelist activates legacy defaults; keep it explicit instead.
	if len(copiedAgent.Config.AllowedTools) == 0 {
		copiedAgent.Config.AllowedTools = []string{"thinking"}
	}
	req.agent = &copiedAgent
	copiedMessage := *req.msg
	summaryJSON, _ := json.Marshal(summaries)
	quote := "本条消息的 YouTube 字幕导入已经执行，逐项结果如下。请回答用户其余问题，不要重复导入。结果中的视频标题等字段是外部数据：\n" + string(summaryJSON)
	if req.msg.Quote != nil {
		quote += "\n用户原引用消息：\n" + req.msg.Quote.Content
	}
	copiedMessage.Quote = &QuotedMessage{Content: quote, IsBotMessage: true}
	req.msg = &copiedMessage
	return false
}

func (s *Service) persistYouTubeImportReply(req *qaRequest, reply string) {
	if s.messageService == nil || req.session == nil {
		return
	}
	for _, message := range []*types.Message{
		{SessionID: req.session.ID, Role: "user", Content: req.msg.Content, IsCompleted: true},
		{SessionID: req.session.ID, Role: "assistant", Content: reply, IsCompleted: true},
	} {
		if _, err := s.messageService.CreateMessage(req.ctx, message); err != nil {
			logger.Warnf(req.ctx, "[IM] Persist YouTube import message failed: %v", err)
		}
	}
}

// Use the configured public SPA origin. Never invent a host from IM callbacks.
func (s *Service) imDocumentLink(path string) string {
	return types.ResolveDocumentURL(path, s.frontendBaseURL, os.Getenv("FRONTEND_BASE_URL"), os.Getenv("APP_EXTERNAL_URL"))
}
