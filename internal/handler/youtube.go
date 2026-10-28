package handler

import (
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *TenantHandler) getYouTubeTranscriptConfig(c *gin.Context) {
	tenant, ok := types.TenantInfoFromContext(c.Request.Context())
	if !ok || tenant == nil {
		c.Error(errors.NewBadRequestError("工作空间不存在"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": types.YouTubeTranscriptConfigForResponse(tenant.YouTubeTranscriptConfig)})
}
func (h *TenantHandler) updateYouTubeTranscriptConfig(c *gin.Context) {
	ctx := c.Request.Context()
	var request struct {
		types.YouTubeTranscriptConfig
		ClearProxy bool `json:"clear_proxy"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError("配置格式无效"))
		return
	}
	tenant, ok := types.TenantInfoFromContext(ctx)
	if !ok || tenant == nil {
		c.Error(errors.NewBadRequestError("工作空间不存在"))
		return
	}
	cfg := request.YouTubeTranscriptConfig
	previous := ""
	if tenant.YouTubeTranscriptConfig != nil {
		previous = tenant.YouTubeTranscriptConfig.ProxyURL
	}
	proxy, err := types.MergeYouTubeProxy(cfg.ProxyURL, previous, request.ClearProxy)
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	cfg.ProxyURL = proxy
	if cfg.Enabled && cfg.KnowledgeBaseID == "" {
		c.Error(errors.NewBadRequestError("请选择目标知识库"))
		return
	}
	if cfg.KnowledgeBaseID != "" {
		kb, err := h.kbService.GetKnowledgeBaseByID(ctx, cfg.KnowledgeBaseID)
		if err != nil || kb == nil || kb.TenantID != tenant.ID || kb.Type != types.KnowledgeBaseTypeDocument || kb.IsTemporary {
			c.Error(errors.NewBadRequestError("目标必须为当前空间的文档知识库"))
			return
		}
		if err := types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kb.ID); err != nil {
			c.Error(errors.NewForbiddenError("无权配置该知识库"))
			return
		}
	}
	copyOfTenant := *tenant
	copyOfTenant.YouTubeTranscriptConfig = &cfg
	updated, err := h.service.UpdateTenant(ctx, &copyOfTenant)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": types.YouTubeTranscriptConfigForResponse(updated.YouTubeTranscriptConfig)})
}
