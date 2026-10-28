package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Registration needs the explicit agent allowlist as well as the workspace
// switch. Import rechecks configuration and write permission at execution time.
func (s *agentService) youtubeImportAvailable(ctx context.Context) bool {
	if s.youtubeImportService == nil || s.tenantService == nil {
		return false
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return false
	}
	tenant, err := s.tenantService.GetTenantByID(ctx, tenantID)
	return err == nil && tenant != nil && tenant.YouTubeTranscriptConfig != nil &&
		tenant.YouTubeTranscriptConfig.Enabled && tenant.YouTubeTranscriptConfig.KnowledgeBaseID != ""
}
