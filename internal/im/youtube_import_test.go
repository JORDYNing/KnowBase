package im

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestIMYouTubeURLs(t *testing.T) {
	got := imYouTubeURLs("https://youtu.be/abcdefghijk?t=5 https://www.youtube.com/watch?v=abcdefghijk&list=x https://youtube.com/shorts/lmnopqrstuv https://youtube.com/live/zyxwvutsrqp https://youtube.com.evil/watch?v=abcdefghijk https://youtube.com/@abc https://youtu.be/invalid")
	want := []string{"https://www.youtube.com/watch?v=abcdefghijk", "https://www.youtube.com/watch?v=lmnopqrstuv", "https://www.youtube.com/watch?v=zyxwvutsrqp"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestIMYouTubeLanguage(t *testing.T) {
	for _, tc := range []struct {
		text, language string
		understood     bool
	}{
		{"https://youtu.be/abcdefghijk", "", true},
		{"https://youtu.be/abcdefghijk 获取英文字幕", "en", true},
		{"请用繁体中文 https://youtu.be/abcdefghijk", "zh-Hant", true},
		{"字幕语言：pt-BR https://youtu.be/abcdefghijk", "pt-br", true},
		{"language=sv https://youtu.be/abcdefghijk", "sv", true},
		{"https://youtu.be/abcdefghijk en", "en", true},
		{"https://youtu.be/abcdefghijk 获取en字幕", "en", true},
		{"https://youtu.be/abcdefghijk 获取字幕 pt-BR", "pt-br", true},
		{"https://youtu.be/abcdefghijk 用中文总结英文视频", "", true},
		{"https://youtu.be/abcdefghijk 用中文总结视频", "", true},
		{"https://youtu.be/abcdefghijk 获取瑞典语字幕", "", false},
		{"https://youtu.be/abcdefghijk 用瑞典语", "", false},
		{"https://youtu.be/abcdefghijk get swedish subtitles", "", false},
		{"https://youtu.be/abcdefghijk 中文字幕和英文字幕", "", false},
		{"https://youtu.be/abcdefghijk 请分析视频内容", "", true},
		{"把这个英文视频保存到知识库 https://youtu.be/abcdefghijk", "", true},
		{"把这个日语视频保存到知识库 https://youtu.be/abcdefghijk", "", true},
	} {
		got, ok := imYouTubeLanguage(tc.text)
		if got != tc.language || ok != tc.understood {
			t.Errorf("%q = (%q,%v), want (%q,%v)", tc.text, got, ok, tc.language, tc.understood)
		}
	}
}

type youtubeImportFake struct {
	calls     []string
	languages []string
	failures  map[string]bool
	cancel    context.CancelFunc
	viewer    bool
}

func (f *youtubeImportFake) Import(ctx context.Context, url, language string) (*types.YouTubeImportResult, error) {
	panic("IM must use ImportFromIM")
}
func (f *youtubeImportFake) ImportFromIM(ctx context.Context, url, language string) (*types.YouTubeImportResult, error) {
	f.calls = append(f.calls, url)
	f.languages = append(f.languages, language)
	f.viewer = types.TenantRoleFromContext(ctx) == types.TenantRoleViewer
	if f.cancel != nil {
		f.cancel()
		return nil, context.Canceled
	}
	if f.failures[url] {
		return nil, errors.New("所选字幕语言不可用")
	}
	return &types.YouTubeImportResult{Title: "视频", VideoID: "abcdefghijk", Language: language, KnowledgeBaseName: "目标库", Status: "pending", KnowledgeID: "doc", KnowledgeBaseID: "kb"}, nil
}

type youtubeReplyAdapter struct {
	lifecycleTestAdapter
	replies []string
}

func (a *youtubeReplyAdapter) SendReply(_ context.Context, _ *IncomingMessage, r *ReplyMessage) error {
	a.replies = append(a.replies, r.Content)
	return nil
}
func youtubeRequest(content string) (*qaRequest, *youtubeReplyAdapter) {
	a := &youtubeReplyAdapter{}
	ch := &IMChannel{ID: "channel", TenantID: 42}
	msg := &IncomingMessage{Content: content, Platform: PlatformFeishu, ChatType: ChatTypeDirect}
	ctx, cancel := context.WithCancel(withIMIdentity(context.Background(), ch, msg))
	return &qaRequest{ctx: ctx, cancel: cancel, msg: msg, adapter: a, channel: ch, tenant: &types.Tenant{ID: 42, YouTubeTranscriptConfig: &types.YouTubeTranscriptConfig{Enabled: true}}, agent: &types.CustomAgent{Config: types.CustomAgentConfig{AllowedTools: []string{imYouTubeImportTool, "thinking"}}}}, a
}
func TestIMYouTubeImportsUniqueURLsAndContinuesAfterFailure(t *testing.T) {
	req, a := youtubeRequest("https://youtu.be/abcdefghijk https://youtube.com/watch?v=abcdefghijk https://youtu.be/lmnopqrstuv")
	defer req.cancel()
	fake := &youtubeImportFake{failures: map[string]bool{"https://www.youtube.com/watch?v=abcdefghijk": true}}
	s := (&Service{}).WithYouTubeImportService(fake)
	if !s.importYouTubeMessage(req) {
		t.Fatal("pure links should finish without QA")
	}
	if len(fake.calls) != 2 || len(a.replies) != 3 || !strings.Contains(a.replies[1], "失败") || !strings.Contains(a.replies[2], "已保存，正在处理") {
		t.Fatalf("calls=%v replies=%v", fake.calls, a.replies)
	}
	if !fake.viewer {
		t.Fatal("must preserve Viewer identity")
	}
}
func TestIMYouTubeAdditionalQuestionPreservesOriginalAndRemovesTool(t *testing.T) {
	req, _ := youtubeRequest("https://youtu.be/abcdefghijk 帮我分析这个视频")
	defer req.cancel()
	originalAgent, originalMessage := req.agent, req.msg
	s := (&Service{}).WithYouTubeImportService(&youtubeImportFake{})
	if s.importYouTubeMessage(req) {
		t.Fatal("question needs QA")
	}
	if !agentAllowsYouTubeImport(originalAgent) || agentAllowsYouTubeImport(req.agent) {
		t.Fatal("must disable only request copy")
	}
	if req.msg == originalMessage || originalMessage.Quote != nil || req.msg.Quote == nil || req.msg.Content != originalMessage.Content {
		t.Fatal("original message mutated or results missing")
	}
}
func TestIMYouTubeDisabledAndUnrecognizedLanguage(t *testing.T) {
	for _, scenario := range []string{"disabled", "not-allowed", "unknown-language", "no-link"} {
		req, a := youtubeRequest("https://youtu.be/abcdefghijk")
		switch scenario {
		case "disabled":
			req.tenant.YouTubeTranscriptConfig.Enabled = false
		case "not-allowed":
			req.agent.Config.AllowedTools = nil
		case "unknown-language":
			req.msg.Content += " 获取瑞典语字幕"
		case "no-link":
			req.msg.Content = "请总结一下"
		}
		f := &youtubeImportFake{}
		if (&Service{}).WithYouTubeImportService(f).importYouTubeMessage(req) || len(f.calls) != 0 || len(a.replies) != 0 {
			t.Fatalf("unexpected import in %s", scenario)
		}
		req.cancel()
	}
}
func TestIMYouTubeCancellationStopsRemainingVideos(t *testing.T) {
	req, a := youtubeRequest("https://youtu.be/abcdefghijk https://youtu.be/lmnopqrstuv")
	defer req.cancel()
	f := &youtubeImportFake{cancel: req.cancel}
	if !(&Service{}).WithYouTubeImportService(f).importYouTubeMessage(req) || len(f.calls) != 1 || !strings.Contains(a.replies[len(a.replies)-1], "取消") {
		t.Fatalf("calls=%v replies=%v", f.calls, a.replies)
	}
}
func TestIMYouTubeGroupRequiresMention(t *testing.T) {
	agent := &types.CustomAgent{ID: "custom", Config: types.CustomAgentConfig{AllowedTools: []string{imYouTubeImportTool}}}
	s := &Service{agentService: &stubIMAgentService{agent: agent}, tenantService: &youtubeTenantService{enabled: true}}
	for _, mentioned := range []bool{true, false} {
		ignore, err := s.shouldIgnoreUnmentionedGroupMessage(context.Background(), &IncomingMessage{Platform: PlatformFeishu, ChatType: ChatTypeGroup, Content: "https://youtu.be/abcdefghijk"}, &IMChannel{AgentID: agent.ID}, &mentionTestAdapter{mentioned: mentioned})
		if err != nil || ignore == mentioned {
			t.Fatalf("mentioned=%v ignore=%v err=%v", mentioned, ignore, err)
		}
	}
}

func TestIMYouTubeDocumentLinks(t *testing.T) {
	t.Setenv("FRONTEND_BASE_URL", "")
	t.Setenv("APP_EXTERNAL_URL", "")
	path := "/platform/knowledge-bases/kb?knowledge_id=doc"
	s := &Service{frontendBaseURL: "https://gamelib.example"}
	if got := s.imDocumentLink(path); got != "https://gamelib.example"+path {
		t.Fatalf("got %s", got)
	}
	s.frontendBaseURL = ""
	if got := s.imDocumentLink(path); got != "" {
		t.Fatalf("invented host: %s", got)
	}
	reply := s.imYouTubeResultReply(&types.YouTubeImportResult{KnowledgeID: "doc", DocumentURL: path})
	if !strings.Contains(reply, "文档 ID：doc") || strings.Contains(reply, "文档：/platform") {
		t.Fatal(reply)
	}
	t.Setenv("APP_EXTERNAL_URL", "https://public.example")
	if got := s.imDocumentLink(path); got != "https://public.example"+path {
		t.Fatalf("got %s", got)
	}
	for _, bad := range []string{"//evil.example/path", "javascript:alert(1)", "https://user:secret@example/path"} {
		if got := s.imDocumentLink(bad); got != "" {
			t.Fatalf("unsafe link: %s", got)
		}
	}
}
func TestIMYouTubeUnknownLanguageRequiresExplicitToolLanguage(t *testing.T) {
	req, _ := youtubeRequest("https://youtu.be/abcdefghijk 获取瑞典语字幕")
	defer req.cancel()
	if (&Service{}).WithYouTubeImportService(&youtubeImportFake{}).importYouTubeMessage(req) {
		t.Fatal("should continue through Agent")
	}
	if !types.YouTubeExplicitLanguageRequired(req.ctx) || req.msg.Quote == nil {
		t.Fatal("missing language guard and guidance")
	}
	if types.TenantRoleFromContext(req.ctx) != types.TenantRoleViewer {
		t.Fatal("identity changed")
	}
}

func TestIMYouTubeExplicitLanguageReachesImporter(t *testing.T) {
	req, _ := youtubeRequest("https://youtu.be/abcdefghijk 获取英文字幕")
	defer req.cancel()
	f := &youtubeImportFake{}
	(&Service{}).WithYouTubeImportService(f).importYouTubeMessage(req)
	if len(f.languages) != 1 || f.languages[0] != "en" {
		t.Fatalf("languages=%v", f.languages)
	}
}
func TestIMYouTubeDefaultLanguageLeavesProviderChoice(t *testing.T) {
	req, _ := youtubeRequest("https://youtu.be/abcdefghijk")
	defer req.cancel()
	f := &youtubeImportFake{}
	(&Service{}).WithYouTubeImportService(f).importYouTubeMessage(req)
	if len(f.languages) != 1 || f.languages[0] != "" {
		t.Fatalf("languages=%v", f.languages)
	}
}

type youtubeTenantService struct {
	interfaces.TenantService
	enabled bool
}

func (s *youtubeTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{YouTubeTranscriptConfig: &types.YouTubeTranscriptConfig{Enabled: s.enabled}}, nil
}
func TestIMYouTubeDisabledDoesNotChangeQuickAnswerMentionBehavior(t *testing.T) {
	agent := &types.CustomAgent{ID: "custom", Config: types.CustomAgentConfig{AllowedTools: []string{imYouTubeImportTool}}}
	s := &Service{agentService: &stubIMAgentService{agent: agent}, tenantService: &youtubeTenantService{}}
	ignore, err := s.shouldIgnoreUnmentionedGroupMessage(context.Background(), &IncomingMessage{Platform: PlatformFeishu, ChatType: ChatTypeGroup, Content: "https://youtu.be/abcdefghijk"}, &IMChannel{AgentID: agent.ID}, &mentionTestAdapter{})
	if err != nil || ignore {
		t.Fatalf("ignore=%v err=%v", ignore, err)
	}
}

func TestIMYouTubeQueueWorkerCompletesWithoutCallingLLM(t *testing.T) {
	req, a := youtubeRequest("https://youtu.be/abcdefghijk")
	req.userKey = "youtube-worker"
	f := &youtubeImportFake{}
	s := (&Service{}).WithYouTubeImportService(f)
	// No SessionService: entering QA would panic. Import is a deterministic worker step.
	s.executeQARequest(req)
	if len(f.calls) != 1 || len(a.replies) != 2 {
		t.Fatalf("calls=%v replies=%v", f.calls, a.replies)
	}
	if _, active := s.inflight.Load(req.userKey); active {
		t.Fatal("inflight not cleaned up")
	}
}

func TestIMYouTubeRejectsMoreThanFiveVideosBeforeImport(t *testing.T) {
	req, a := youtubeRequest("https://youtu.be/abcdefghijk https://youtu.be/lmnopqrstuv https://youtu.be/zyxwvutsrqp https://youtu.be/12345678901 https://youtu.be/12345678902 https://youtu.be/12345678903")
	defer req.cancel()
	f := &youtubeImportFake{}
	if !(&Service{}).WithYouTubeImportService(f).importYouTubeMessage(req) || len(f.calls) != 0 || len(a.replies) != 1 || !strings.Contains(a.replies[0], "最多导入 5") {
		t.Fatalf("calls=%v replies=%v", f.calls, a.replies)
	}
}

type youtubeBlockingImport struct{ youtubeImportFake }

func (f *youtubeBlockingImport) ImportFromIM(ctx context.Context, url, language string) (*types.YouTubeImportResult, error) {
	if len(f.calls) == 0 {
		return f.youtubeImportFake.ImportFromIM(ctx, url, language)
	}
	f.calls = append(f.calls, url)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestIMYouTubeBatchTimeoutReportsCompletedAndUnprocessed(t *testing.T) {
	req, a := youtubeRequest("https://youtu.be/abcdefghijk https://youtu.be/lmnopqrstuv https://youtu.be/zyxwvutsrqp")
	defer req.cancel()
	f := &youtubeBlockingImport{}
	s := (&Service{youtubeBatchTimeout: 20 * time.Millisecond}).WithYouTubeImportService(f)
	if !s.importYouTubeMessage(req) || len(f.calls) != 2 {
		t.Fatalf("calls=%v", f.calls)
	}
	last := a.replies[len(a.replies)-1]
	if !strings.Contains(last, "已超时") || !strings.Contains(last, "已完成 1 个") || !strings.Contains(last, "未处理 1 个") {
		t.Fatal(last)
	}
	if req.ctx.Err() != nil {
		t.Fatal("batch timeout must not mutate original caller context")
	}
}
