package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime/multipart"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/infrastructure/youtube"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type youtubeImportService struct {
	tenants         interfaces.TenantService
	knowledgeBases  interfaces.KnowledgeBaseService
	knowledge       interfaces.KnowledgeService
	repo            interfaces.KnowledgeRepository
	db              *gorm.DB
	fetcher         youtube.Fetcher
	frontendBaseURL string
}

func NewYouTubeImportService(tenants interfaces.TenantService, knowledgeBases interfaces.KnowledgeBaseService, knowledge interfaces.KnowledgeService, repo interfaces.KnowledgeRepository, db *gorm.DB, appCfg *config.Config) interfaces.YouTubeImportService {
	s := &youtubeImportService{tenants: tenants, knowledgeBases: knowledgeBases, knowledge: knowledge, repo: repo, db: db, fetcher: youtube.NewClient()}
	if appCfg != nil {
		s.frontendBaseURL = appCfg.FrontendBaseURL
	}
	return s
}
func (s *youtubeImportService) Import(ctx context.Context, raw, language string) (*types.YouTubeImportResult, error) {
	result, err := s.importVideo(ctx, raw, language, false)
	if err != nil {
		return result, err
	}
	return s.waitForIMCompletion(ctx, result, 2*time.Second)
}
func (s *youtubeImportService) ImportFromIM(ctx context.Context, raw, language string) (*types.YouTubeImportResult, error) {
	principal, ok := types.PrincipalFromContext(ctx)
	if !ok || principal.Type != types.PrincipalIMUser {
		return nil, apperrors.NewForbiddenError("自动入库仅允许来自已配置的 IM 频道")
	}
	result, err := s.importVideo(ctx, raw, language, true)
	if err != nil {
		return result, err
	}
	if link := types.ResolveDocumentURL(result.DocumentURL, s.frontendBaseURL, os.Getenv("FRONTEND_BASE_URL"), os.Getenv("APP_EXTERNAL_URL")); link != "" {
		result.DocumentURL = link
	}
	// The fetch/import deadline has ended and its deduplication lease is released.
	// Read the persisted parse and summary states before reporting completion.
	return s.waitForIMCompletion(ctx, result, 2*time.Second)
}
func (s *youtubeImportService) destination(ctx context.Context, im bool) (context.Context, *types.Tenant, *types.KnowledgeBase, error) {
	caller := types.CallerFromContext(ctx)
	if caller.TenantID == 0 {
		return ctx, nil, nil, apperrors.NewUnauthorizedError("工作空间身份无效")
	}
	tenant, err := s.tenants.GetTenantByID(ctx, caller.TenantID)
	if err != nil {
		return ctx, nil, nil, err
	}
	if tenant == nil || tenant.ID != caller.TenantID || tenant.YouTubeTranscriptConfig == nil || !tenant.YouTubeTranscriptConfig.Enabled {
		return ctx, nil, nil, apperrors.NewBadRequestError("YouTube 字幕入库工具尚未启用")
	}
	cfg := tenant.YouTubeTranscriptConfig
	if cfg.KnowledgeBaseID == "" {
		return ctx, nil, nil, apperrors.NewBadRequestError("请先在工具配置中选择目标知识库")
	}
	kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ctx, cfg.KnowledgeBaseID)
	if err != nil {
		return ctx, nil, nil, err
	}
	if kb == nil || kb.TenantID != tenant.ID || kb.Type != types.KnowledgeBaseTypeDocument || kb.IsTemporary {
		return ctx, nil, nil, apperrors.NewForbiddenError("目标必须为当前工作空间的文档知识库")
	}
	if im {
		// The separate IM entrypoint has checked its server-issued principal. Admin-only
		// tool configuration admits exactly this destination, never a model-provided KB.
		ctx, err = access.WithKBTaskWrite(ctx, kb, tenant.ID)
	} else {
		if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
			if !scope.FullAccess && !scope.HasCapability(types.APIKeyCapabilityIngest) {
				return ctx, nil, nil, apperrors.NewForbiddenError("API Key 无入库权限")
			}
		} else if !caller.Role.HasPermission(types.TenantRoleAdmin) && !(caller.Role.HasPermission(types.TenantRoleContributor) && caller.UserID != "" && caller.UserID == kb.CreatorID) {
			return ctx, nil, nil, apperrors.NewForbiddenError("无权向目标知识库写入")
		}
		var admitted *access.KBAccess
		admitted, err = access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleEditor, nil, nil)
		if err == nil {
			ctx = admitted.Context(ctx)
		}
	}
	if err != nil {
		return ctx, nil, nil, err
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	return ctx, tenant, kb, nil
}
func (s *youtubeImportService) importVideo(ctx context.Context, raw, language string, im bool) (*types.YouTubeImportResult, error) {
	if _, _, err := youtube.NormalizeURL(raw); err != nil {
		return nil, err
	}
	if _, err := youtube.NormalizeLanguage(language); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	ctx, tenant, kb, err := s.destination(ctx, im)
	if err != nil {
		return nil, err
	}
	transcript, err := s.fetcher.Fetch(ctx, raw, language, tenant.YouTubeTranscriptConfig.ProxyURL)
	if err != nil {
		return nil, err
	}
	// Recheck live configuration before writing, including disable/retarget during fetching.
	admitted, latest, currentKB, err := s.destination(ctx, im)
	if err != nil {
		return nil, err
	}
	if currentKB.ID != kb.ID {
		return nil, apperrors.NewBadRequestError("目标知识库配置已变更，请重试")
	}
	ctx = admitted
	tenant = latest
	kb = currentKB
	key := transcript.VideoID + ":" + strings.ToLower(transcript.Language)
	release, err := s.acquire(ctx, tenant.ID, kb.ID, key)
	if err != nil {
		return nil, err
	}
	defer release()
	existing, err := s.repo.FindByMetadataKey(ctx, tenant.ID, kb.ID, "youtube_import_key", key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return youtubeResult(transcript, kb, existing, true), nil
	}
	md := youtube.RenderMarkdown(transcript)
	if len(md) > youtube.MaxOutputBytes {
		return nil, fmt.Errorf("字幕 Markdown 超过大小限制")
	}
	filename := youtubeFileName(transcript)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write([]byte(md)); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	reader := multipart.NewReader(&body, writer.Boundary())
	form, err := reader.ReadForm(int64(youtube.MaxOutputBytes + 1024))
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	metadata := map[string]string{"youtube_import_key": key, "youtube_video_id": transcript.VideoID, "source_url": transcript.URL, "subtitle_language": transcript.Language, "youtube_channel": transcript.Channel, "youtube_title": transcript.Title, "youtube_upload_date": transcript.UploadDate}
	channel := "web"
	if im {
		channel = "feishu"
	}
	disabled := false
	summaryEnabled := true
	knowledge, err := s.knowledge.CreateKnowledgeFromFile(ctx, kb.ID, form.File["file"][0], metadata, &disabled, filename, nil, channel, &types.KnowledgeProcessOverrides{SummaryEnabled: &summaryEnabled})
	if err != nil {
		var duplicate *types.DuplicateKnowledgeError
		if knowledge != nil && errors.As(err, &duplicate) {
			return youtubeResult(transcript, kb, knowledge, true), nil
		}
		return nil, err
	}
	if knowledge == nil {
		return nil, fmt.Errorf("字幕文件入库未返回文档")
	}
	return youtubeResult(transcript, kb, knowledge, false), nil
}
func youtubeFileName(t *youtube.Transcript) string {
	var out []rune
	for _, r := range t.Title {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' || r == '-' || r == '_' || r == '(' || r == ')' {
			out = append(out, r)
		}
		if len(out) >= 100 {
			break
		}
	}
	title := strings.TrimSpace(string(out))
	if title == "" {
		title = "YouTube"
	}
	return fmt.Sprintf("%s_%s_%s.md", title, t.VideoID, t.Language)
}
func youtubeResult(t *youtube.Transcript, kb *types.KnowledgeBase, k *types.Knowledge, exists bool) *types.YouTubeImportResult {
	publishedAt := ""
	if date, err := time.Parse("20060102", t.UploadDate); err == nil {
		publishedAt = date.Format("2006-01-02")
	}
	result := &types.YouTubeImportResult{VideoID: t.VideoID, Title: t.Title, Channel: t.Channel, PublishedAt: publishedAt, URL: t.URL, Language: t.Language, KnowledgeID: k.ID, KnowledgeBaseID: kb.ID, KnowledgeBaseName: kb.Name, DocumentURL: "/platform/knowledge-bases/" + url.PathEscape(kb.ID) + "?knowledge_id=" + url.QueryEscape(k.ID), AlreadyExists: exists}
	updateYouTubeProcessingResult(result, k)
	return result
}

func updateYouTubeProcessingResult(result *types.YouTubeImportResult, knowledge *types.Knowledge) {
	result.Status = knowledge.ParseStatus
	result.SummaryStatus = knowledge.SummaryStatus
	result.Summary = ""
	// Description may hold a first-chunk fallback when summary generation fails.
	if knowledge.SummaryStatus == types.SummaryStatusCompleted {
		result.Summary = strings.TrimSpace(knowledge.Description)
	}
}

func (s *youtubeImportService) waitForIMCompletion(ctx context.Context, result *types.YouTubeImportResult, interval time.Duration) (*types.YouTubeImportResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	types.NotifyYouTubeImportProgress(ctx, result)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		knowledge, err := s.repo.GetKnowledgeByID(ctx, types.CallerFromContext(ctx).TenantID, result.KnowledgeID)
		if err != nil {
			return result, fmt.Errorf("字幕已保存，读取文档处理状态失败：%w", err)
		}
		if knowledge == nil || knowledge.TenantID != types.CallerFromContext(ctx).TenantID || knowledge.KnowledgeBaseID != result.KnowledgeBaseID {
			return nil, fmt.Errorf("入库文档已不存在或已移出目标知识库")
		}
		updateYouTubeProcessingResult(result, knowledge)
		switch knowledge.ParseStatus {
		case types.ParseStatusFailed:
			return result, fmt.Errorf("字幕已保存，文档解析失败")
		case types.ParseStatusCancelled, types.ParseStatusDeleting:
			return result, fmt.Errorf("文档处理已取消或文档正在删除")
		case types.ParseStatusCompleted:
			switch knowledge.SummaryStatus {
			case types.SummaryStatusCompleted:
				if result.Summary == "" {
					return result, fmt.Errorf("视频总结已完成但内容为空")
				}
				return result, nil
			case types.SummaryStatusFailed:
				return result, fmt.Errorf("字幕已保存，视频总结生成失败")
			case types.SummaryStatusNone, "":
				if s.knowledge == nil {
					return result, fmt.Errorf("视频总结服务未配置")
				}
				summarized, err := s.knowledge.RegenerateKnowledgeSummary(ctx, result.KnowledgeID)
				if err != nil {
					return result, fmt.Errorf("字幕已保存，视频总结生成失败：%w", err)
				}
				if summarized == nil || summarized.TenantID != knowledge.TenantID || summarized.KnowledgeBaseID != knowledge.KnowledgeBaseID {
					return result, fmt.Errorf("视频总结未返回有效文档")
				}
				updateYouTubeProcessingResult(result, summarized)
				if result.SummaryStatus != types.SummaryStatusCompleted || result.Summary == "" {
					return result, fmt.Errorf("视频总结尚未完成")
				}
				return result, nil
			}
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}

type youtubeImportLock struct {
	ID        string `gorm:"primaryKey"`
	Owner     string
	ExpiresAt int64
}

func (youtubeImportLock) TableName() string { return "youtube_import_locks" }
func (s *youtubeImportService) acquire(ctx context.Context, tenant uint64, kb, key string) (func(), error) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", tenant, kb, key)))
	id := hex.EncodeToString(sum[:])
	owner := uuid.NewString()
	now := time.Now().Unix()
	// Expiry is longer than the entire import deadline. Conditional deletion and an
	// owner token prevent one request from releasing another process's lease.
	if err := s.db.WithContext(ctx).Where("id = ? AND expires_at < ?", id, now).Delete(&youtubeImportLock{}).Error; err != nil {
		return nil, err
	}
	lock := youtubeImportLock{ID: id, Owner: owner, ExpiresAt: now + 600}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&lock)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("该视频的字幕正在入库，请稍后重试")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.db.WithContext(cleanup).Where("id = ? AND owner = ?", id, owner).Delete(&youtubeImportLock{})
	}, nil
}
