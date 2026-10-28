package service

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/infrastructure/youtube"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"io"
	"mime/multipart"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type youtubeTenantStub struct {
	interfaces.TenantService
	tenant *types.Tenant
}

func (s *youtubeTenantStub) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, nil
}

type youtubeKBStub struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s *youtubeKBStub) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type youtubeRepoStub struct {
	interfaces.KnowledgeRepository
	mu   sync.Mutex
	rows map[string]*types.Knowledge
}

func (s *youtubeRepoStub) FindByMetadataKey(_ context.Context, _ uint64, _ string, key, value string) (*types.Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[value], nil
}

func (s *youtubeRepoStub) GetKnowledgeByID(_ context.Context, tenantID uint64, id string) (*types.Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.ID == id && row.TenantID == tenantID {
			copy := *row
			return &copy, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

type youtubeFetcherStub struct {
	language string
	fail     error
	calls    int
}

func (f *youtubeFetcherStub) Fetch(ctx context.Context, raw, language, proxy string) (*youtube.Transcript, error) {
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	actual := language
	if actual == "" {
		actual = f.language
	}
	if actual == "" {
		actual = "zh-Hans"
	}
	return &youtube.Transcript{VideoID: "dQw4w9WgXcQ", Title: "测试视频", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Language: actual, Channel: "原频道", UploadDate: "20261028", Description: "原简介", FetchedAt: time.Now(), Segments: []youtube.Segment{{Start: 1, Text: "完整字幕"}}}, nil
}

type youtubeKnowledgeStub struct {
	interfaces.KnowledgeService
	t         *testing.T
	repo      *youtubeRepoStub
	kb        *types.KnowledgeBase
	contents  []string
	channels  []string
	roles     []types.TenantRole
	completed bool
}

func (s *youtubeKnowledgeStub) CreateKnowledgeFromFile(ctx context.Context, kbID string, file *multipart.FileHeader, metadata map[string]string, _ *bool, filename string, _ []string, channel string, overrides *types.KnowledgeProcessOverrides) (*types.Knowledge, error) {
	require.Equal(s.t, s.kb.ID, kbID)
	require.NotNil(s.t, overrides)
	require.NotNil(s.t, overrides.SummaryEnabled)
	require.True(s.t, *overrides.SummaryEnabled)
	require.NoError(s.t, access.RequireKBWrite(ctx, s.kb))
	require.False(s.t, access.HasKBGrant(ctx, "different-kb", s.kb.TenantID, types.OrgRoleEditor))
	handle, err := file.Open()
	require.NoError(s.t, err)
	defer handle.Close()
	data, err := io.ReadAll(handle)
	require.NoError(s.t, err)
	s.contents = append(s.contents, string(data))
	s.channels = append(s.channels, channel)
	s.roles = append(s.roles, types.CallerFromContext(ctx).Role)
	k := &types.Knowledge{ID: "doc-" + metadata["subtitle_language"], KnowledgeBaseID: kbID, TenantID: s.kb.TenantID, FileName: filename, ParseStatus: "pending"}
	if s.completed {
		k.ParseStatus = types.ParseStatusCompleted
		k.SummaryStatus = types.SummaryStatusCompleted
		k.Description = "根据完整字幕生成的总结"
	}
	s.repo.mu.Lock()
	s.repo.rows[metadata["youtube_import_key"]] = k
	s.repo.mu.Unlock()
	return k, nil
}
func youtubeTestService(t *testing.T) (*youtubeImportService, *youtubeKnowledgeStub, *youtubeFetcherStub, context.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "youtube.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&youtubeImportLock{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	tenant := &types.Tenant{ID: 42, YouTubeTranscriptConfig: &types.YouTubeTranscriptConfig{Enabled: true, KnowledgeBaseID: "kb"}}
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 42, Type: types.KnowledgeBaseTypeDocument, Name: "视频知识库", CreatorID: "owner"}
	repo := &youtubeRepoStub{rows: map[string]*types.Knowledge{}}
	knowledge := &youtubeKnowledgeStub{t: t, repo: repo, kb: kb, completed: true}
	fetcher := &youtubeFetcherStub{}
	svc := &youtubeImportService{tenants: &youtubeTenantStub{tenant: tenant}, knowledgeBases: &youtubeKBStub{kb: kb}, knowledge: knowledge, repo: repo, db: db, fetcher: fetcher}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 42, UserID: "owner", Role: types.TenantRoleAdmin})
	ctx = types.WithExecutionTenant(ctx, 42)
	return svc, knowledge, fetcher, ctx
}
func TestYouTubeImportMarkdownAndLanguageDedup(t *testing.T) {
	svc, knowledge, _, ctx := youtubeTestService(t)
	first, err := svc.Import(ctx, "https://youtu.be/dQw4w9WgXcQ", "")
	require.NoError(t, err)
	require.False(t, first.AlreadyExists)
	require.Equal(t, "zh-Hans", first.Language)
	require.Equal(t, types.ParseStatusCompleted, first.Status)
	require.Equal(t, "根据完整字幕生成的总结", first.Summary)
	require.Contains(t, first.DocumentURL, "knowledge_id=doc-zh-Hans")
	for _, expected := range []string{"视频链接：https://www.youtube.com/watch?v=dQw4w9WgXcQ", "原频道", "原简介", "完整字幕", "字幕语言：zh-Hans"} {
		require.Contains(t, knowledge.contents[0], expected)
	}
	again, err := svc.Import(ctx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=5", "")
	require.NoError(t, err)
	require.True(t, again.AlreadyExists)
	require.Len(t, knowledge.contents, 1)
	english, err := svc.Import(ctx, "https://youtu.be/dQw4w9WgXcQ", "en")
	require.NoError(t, err)
	require.False(t, english.AlreadyExists)
	require.Equal(t, "en", english.Language)
	require.Len(t, knowledge.contents, 2)
}
func TestYouTubeImportAuthorization(t *testing.T) {
	svc, knowledge, fetcher, ctx := youtubeTestService(t)
	viewer := types.WithCaller(ctx, types.Caller{TenantID: 42, UserID: "viewer", Role: types.TenantRoleViewer})
	_, err := svc.Import(viewer, "https://youtu.be/dQw4w9WgXcQ", "")
	require.Error(t, err)
	require.Zero(t, fetcher.calls)
	_, err = svc.ImportFromIM(viewer, "https://youtu.be/dQw4w9WgXcQ", "")
	require.Error(t, err)
	knowledge.completed = true
	im := types.WithPrincipal(viewer, types.Principal{Type: types.PrincipalIMUser, ID: "42:feishu:channel:user"})
	_, err = svc.ImportFromIM(im, "https://youtu.be/dQw4w9WgXcQ", "")
	require.NoError(t, err)
	require.Equal(t, []string{"feishu"}, knowledge.channels)
	require.Equal(t, []types.TenantRole{types.TenantRoleViewer}, knowledge.roles)
	svc.knowledgeBases.(*youtubeKBStub).kb.TenantID = 99
	_, err = svc.ImportFromIM(im, "https://youtu.be/dQw4w9WgXcQ", "")
	require.Error(t, err)
}
func TestYouTubeImportFailureDoesNotWrite(t *testing.T) {
	svc, knowledge, fetcher, ctx := youtubeTestService(t)
	fetcher.fail = errors.New("视频没有字幕")
	_, err := svc.Import(ctx, "https://youtu.be/dQw4w9WgXcQ", "")
	require.ErrorContains(t, err, "没有字幕")
	require.Empty(t, knowledge.contents)
	svc.tenants.(*youtubeTenantStub).tenant.YouTubeTranscriptConfig.Enabled = false
	_, err = svc.Import(ctx, "https://youtu.be/dQw4w9WgXcQ", "")
	require.ErrorContains(t, err, "尚未启用")
	require.Equal(t, 1, fetcher.calls)
}
func TestYouTubeImportLeaseAndScope(t *testing.T) {
	svc, _, _, ctx := youtubeTestService(t)
	release, err := svc.acquire(ctx, 42, "kb", "video:zh")
	require.NoError(t, err)
	_, err = svc.acquire(ctx, 42, "kb", "video:zh")
	require.ErrorContains(t, err, "正在入库")
	otherLanguage, err := svc.acquire(ctx, 42, "kb", "video:en")
	require.NoError(t, err)
	otherLanguage()
	otherKB, err := svc.acquire(ctx, 42, "other-kb", "video:zh")
	require.NoError(t, err)
	otherKB()
	release()
	again, err := svc.acquire(ctx, 42, "kb", "video:zh")
	require.NoError(t, err)
	again()
}

func TestYouTubeIMCompletionAndDuplicateSummary(t *testing.T) {
	svc, knowledge, _, ctx := youtubeTestService(t)
	svc.frontendBaseURL = "https://gamelib.example"
	knowledge.completed = true
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalIMUser, ID: "42:feishu:channel:user"})
	for _, duplicate := range []bool{false, true} {
		result, err := svc.ImportFromIM(ctx, "https://youtu.be/dQw4w9WgXcQ", "")
		require.NoError(t, err)
		require.Equal(t, duplicate, result.AlreadyExists)
		require.Equal(t, types.ParseStatusCompleted, result.Status)
		require.Equal(t, "原频道", result.Channel)
		require.Equal(t, "2026-10-28", result.PublishedAt)
		require.Equal(t, "根据完整字幕生成的总结", result.Summary)
		require.Equal(t, "https://gamelib.example/platform/knowledge-bases/kb?knowledge_id=doc-zh-Hans", result.DocumentURL)
	}
	require.Len(t, knowledge.contents, 1)
}

type youtubeCompletionRepo struct {
	interfaces.KnowledgeRepository
	states []*types.Knowledge
	calls  int
	err    error
}

func (r *youtubeCompletionRepo) GetKnowledgeByID(_ context.Context, tenantID uint64, id string) (*types.Knowledge, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	i := r.calls - 1
	if i >= len(r.states) {
		i = len(r.states) - 1
	}
	return r.states[i], nil
}

func TestYouTubeIMWaitsForParseAndSummary(t *testing.T) {
	for _, transition := range [][]string{
		{types.ParseStatusPending, types.ParseStatusFinalizing, types.ParseStatusCompleted},
		{types.ParseStatusCompleted, types.ParseStatusCompleted, types.ParseStatusCompleted},
	} {
		r := &youtubeCompletionRepo{}
		for i, status := range transition {
			summaryStatus := types.SummaryStatusPending
			if i == 2 {
				summaryStatus = types.SummaryStatusCompleted
			}
			r.states = append(r.states, &types.Knowledge{TenantID: 42, KnowledgeBaseID: "kb", ParseStatus: status, SummaryStatus: summaryStatus, Description: "最终总结"})
		}
		svc := &youtubeImportService{repo: r}
		ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 42})
		result, err := svc.waitForIMCompletion(ctx, &types.YouTubeImportResult{KnowledgeID: "doc", KnowledgeBaseID: "kb"}, time.Millisecond)
		require.NoError(t, err)
		require.Equal(t, 3, r.calls)
		require.Equal(t, "最终总结", result.Summary)
	}
}

func TestYouTubeIMCompletionTerminalStates(t *testing.T) {
	for _, tc := range []struct {
		name, parse, summary string
		wantError            bool
	}{
		{"parse failed", types.ParseStatusFailed, types.SummaryStatusFailed, true},
		{"cancelled", types.ParseStatusCancelled, types.SummaryStatusPending, true},
		{"deleting", types.ParseStatusDeleting, types.SummaryStatusPending, true},
		{"summary failed", types.ParseStatusCompleted, types.SummaryStatusFailed, true},
		{"summary disabled", types.ParseStatusCompleted, types.SummaryStatusNone, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &youtubeCompletionRepo{states: []*types.Knowledge{{TenantID: 42, KnowledgeBaseID: "kb", ParseStatus: tc.parse, SummaryStatus: tc.summary, Description: "首个字幕块降级正文"}}}
			svc := &youtubeImportService{repo: r}
			ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 42})
			result, err := svc.waitForIMCompletion(ctx, &types.YouTubeImportResult{KnowledgeID: "doc", KnowledgeBaseID: "kb"}, time.Millisecond)
			require.Equal(t, tc.wantError, err != nil)
			require.Empty(t, result.Summary)
			require.Equal(t, 1, r.calls)
		})
	}
}

func TestYouTubeIMCompletionScopeAndReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  *types.Knowledge
		err  error
	}{
		{"tenant mismatch", &types.Knowledge{TenantID: 99, KnowledgeBaseID: "kb"}, nil},
		{"moved", &types.Knowledge{TenantID: 42, KnowledgeBaseID: "other"}, nil},
		{"missing", nil, nil},
		{"read failure", nil, errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &youtubeCompletionRepo{states: []*types.Knowledge{tc.row}, err: tc.err}
			svc := &youtubeImportService{repo: r}
			ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 42})
			_, err := svc.waitForIMCompletion(ctx, &types.YouTubeImportResult{KnowledgeID: "doc", KnowledgeBaseID: "kb"}, time.Millisecond)
			require.Error(t, err)
		})
	}
}

func TestYouTubeIMCompletionTimeoutAndCancellation(t *testing.T) {
	for _, cancelNow := range []bool{false, true} {
		r := &youtubeCompletionRepo{states: []*types.Knowledge{{TenantID: 42, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusPending}}}
		svc := &youtubeImportService{repo: r}
		ctx, cancel := context.WithTimeout(types.WithCaller(context.Background(), types.Caller{TenantID: 42}), 5*time.Millisecond)
		if cancelNow {
			cancel()
		}
		result, err := svc.waitForIMCompletion(ctx, &types.YouTubeImportResult{KnowledgeID: "doc", KnowledgeBaseID: "kb"}, time.Hour)
		cancel()
		require.NotNil(t, result)
		if cancelNow {
			require.ErrorIs(t, err, context.Canceled)
		} else {
			require.ErrorIs(t, err, context.DeadlineExceeded)
		}
	}
}

func TestYouTubeResultDoesNotInventPublicationDate(t *testing.T) {
	for _, date := range []string{"", "20260230", "invalid"} {
		result := youtubeResult(&youtube.Transcript{UploadDate: date}, &types.KnowledgeBase{}, &types.Knowledge{}, false)
		require.Empty(t, result.PublishedAt)
	}
}

func (s *youtubeKnowledgeStub) RegenerateKnowledgeSummary(ctx context.Context, id string) (*types.Knowledge, error) {
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	for _, row := range s.repo.rows {
		if row.ID == id {
			row.SummaryStatus = types.SummaryStatusCompleted
			row.Description = "自动补生成的视频总结"
			copy := *row
			return &copy, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func TestYouTubeImportRegeneratesMissingSummaryAndReportsProgress(t *testing.T) {
	svc, knowledge, _, ctx := youtubeTestService(t)
	knowledge.repo.rows["dQw4w9WgXcQ:zh-hans"] = &types.Knowledge{ID: "existing", TenantID: 42, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusNone}
	calls := 0
	ctx = types.WithYouTubeImportProgress(ctx, func(result *types.YouTubeImportResult) {
		calls++
		require.Equal(t, "existing", result.KnowledgeID)
		require.Empty(t, result.Summary)
	})
	result, err := svc.Import(ctx, "https://youtu.be/dQw4w9WgXcQ", "")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.True(t, result.AlreadyExists)
	require.Equal(t, "自动补生成的视频总结", result.Summary)
	require.Empty(t, knowledge.contents)
}
