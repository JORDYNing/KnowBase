package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type youtubeImportStub struct {
	webCalls int
	imCalls  int
	url      string
	language string
	result   *types.YouTubeImportResult
	err      error
}

func (s *youtubeImportStub) Import(ctx context.Context, url, language string) (*types.YouTubeImportResult, error) {
	s.webCalls++
	if s.result != nil {
		types.NotifyYouTubeImportProgress(ctx, s.result)
	}
	s.url, s.language = url, language
	return s.result, s.err
}

func (s *youtubeImportStub) ImportFromIM(_ context.Context, url, language string) (*types.YouTubeImportResult, error) {
	s.imCalls++
	s.url, s.language = url, language
	return s.result, s.err
}

func TestYouTubeToolRoutesOnlyAuthenticatedIMPrincipal(t *testing.T) {
	for _, im := range []bool{false, true} {
		svc := &youtubeImportStub{result: &types.YouTubeImportResult{Language: "en", Status: "pending"}}
		ctx := t.Context()
		if im {
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalIMUser, ID: "7:channel:feishu:user"})
		}
		result, err := NewYouTubeTranscriptImportTool(svc).Execute(ctx, json.RawMessage(`{"url":" https://youtu.be/dQw4w9WgXcQ ","language":"en"}`))
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Equal(t, "https://youtu.be/dQw4w9WgXcQ", svc.url)
		require.Equal(t, "en", svc.language)
		require.Equal(t, "pending", result.Data["status"])
		require.Equal(t, "en", result.Data["language"])
		if im {
			require.Equal(t, 1, svc.imCalls)
			require.Zero(t, svc.webCalls)
		} else {
			require.Equal(t, 1, svc.webCalls)
			require.Zero(t, svc.imCalls)
		}
	}
}

func TestYouTubeToolDefaultLanguageIsNotOverridden(t *testing.T) {
	svc := &youtubeImportStub{result: &types.YouTubeImportResult{Language: "zh-Hant", Status: "pending"}}
	_, err := NewYouTubeTranscriptImportTool(svc).Execute(t.Context(), json.RawMessage(`{"url":"https://youtu.be/dQw4w9WgXcQ"}`))
	require.NoError(t, err)
	require.Empty(t, svc.language, "empty delegates Chinese-first policy to the shared service")
}

func TestYouTubeToolRejectsDestinationAndMalformedArguments(t *testing.T) {
	for _, args := range []string{
		`{"url":"https://youtu.be/dQw4w9WgXcQ","knowledge_base_id":"other"}`,
		`{"url":"https://youtu.be/dQw4w9WgXcQ","im":true}`,
		`{"url":"https://youtu.be/dQw4w9WgXcQ"} {}`,
		`{"url":" "}`, `null`, `{`,
	} {
		svc := &youtubeImportStub{}
		result, err := NewYouTubeTranscriptImportTool(svc).Execute(t.Context(), json.RawMessage(args))
		require.Error(t, err, args)
		require.False(t, result.Success)
		require.Zero(t, svc.webCalls+svc.imCalls)
	}
}

func TestYouTubeToolFailuresDoNotReportSuccess(t *testing.T) {
	svc := &youtubeImportStub{err: errors.New("指定语言没有可用字幕")}
	result, err := NewYouTubeTranscriptImportTool(svc).Execute(t.Context(), json.RawMessage(`{"url":"https://youtu.be/dQw4w9WgXcQ","language":"ja"}`))
	require.Error(t, err)
	require.False(t, result.Success)
	require.Empty(t, result.Output)
	require.Equal(t, "指定语言没有可用字幕", result.Error)
	require.False(t, CanRunConcurrently(ToolYouTubeTranscriptImport), "writes must remain execution barriers")
	require.NotContains(t, DefaultAllowedTools(), ToolYouTubeTranscriptImport, "existing agents must not silently gain write tools")
}

func TestYouTubeToolExplicitLanguageRequestCannotSilentlyUseDefault(t *testing.T) {
	svc := &youtubeImportStub{}
	ctx := types.WithYouTubeExplicitLanguageRequired(t.Context())
	result, err := NewYouTubeTranscriptImportTool(svc).Execute(ctx, json.RawMessage(`{"url":"https://youtu.be/dQw4w9WgXcQ"}`))
	require.Error(t, err)
	require.False(t, result.Success)
	require.Zero(t, svc.webCalls+svc.imCalls)
}

func TestYouTubeToolPreservesSavedDocumentOnProcessingFailure(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled, errors.New("文档解析失败")} {
		svc := &youtubeImportStub{result: &types.YouTubeImportResult{KnowledgeID: "doc", DocumentURL: "/platform/knowledge-bases/kb?knowledge_id=doc", Status: types.ParseStatusPending}, err: failure}
		ctx := types.WithPrincipal(t.Context(), types.Principal{Type: types.PrincipalIMUser, ID: "7:channel:feishu:user"})
		result, err := NewYouTubeTranscriptImportTool(svc).Execute(ctx, json.RawMessage(`{"url":"https://youtu.be/dQw4w9WgXcQ"}`))
		require.NoError(t, err, "execution errors cause the agent wrapper to discard the saved document")
		require.False(t, result.Success)
		require.Equal(t, failure.Error(), result.Error)
		require.Equal(t, "doc", result.Data["knowledge_id"])
		require.Contains(t, result.Output, "knowledge_id=doc")
	}
}

func TestYouTubeToolReportsProgressAndReturnsCompletedSummary(t *testing.T) {
	bus := event.NewEventBus()
	var notifications []event.AgentFinalAnswerData
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		require.Equal(t, "current-session", evt.SessionID)
		notifications = append(notifications, evt.Data.(event.AgentFinalAnswerData))
		return nil
	})
	ctx := WithToolExecContext(t.Context(), &ToolExecContext{SessionID: "current-session", EventBus: bus})
	svc := &youtubeImportStub{result: &types.YouTubeImportResult{Title: "Video", Channel: "Creator", PublishedAt: "2026-10-28", Status: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted, Summary: "Completed summary"}}
	result, err := NewYouTubeTranscriptImportTool(svc).Execute(ctx, json.RawMessage(`{"url":"https://youtu.be/dQw4w9WgXcQ"}`))
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Contains(t, notifications[0].Content, "视频总结中，完成后推送。")
	require.False(t, notifications[0].Done)
	require.Contains(t, result.Output, "Completed summary")
}
