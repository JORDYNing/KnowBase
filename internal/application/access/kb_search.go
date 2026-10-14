package access

import (
	"context"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

func AuthorizeKBAccess(
	ctx context.Context,
	kbs []*types.KnowledgeBase,
) error {
	if len(kbs) == 0 {
		return nil
	}

	kbIDs := make([]string, 0, len(kbs))
	for _, kb := range kbs {
		kbIDs = append(kbIDs, kb.ID)
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbIDs...); err != nil {
		return err
	}

	requestTenantID := types.CallerFromContext(ctx).TenantID
	permissions := NewKBPermissions(ctx)

	for _, kb := range kbs {
		hasPermission, permErr := permissions.Check(kb.ID, kb.TenantID, types.KBPermissionRead)
		if permErr != nil {
			logger.ErrorWithFields(ctx, permErr, map[string]interface{}{
				"caller_tenant_id": requestTenantID,
				"kb_tenant_id":     kb.TenantID,
				"kb_id":            kb.ID,
				"reason":           "knowledge base permission check failed",
			})
			return apperrors.NewInternalServerError("failed to verify knowledge base access")
		}
		if !hasPermission {
			logger.WarnWithFields(ctx, logger.Fields{
				"caller_tenant_id": requestTenantID,
				"kb_tenant_id":     kb.TenantID,
				"kb_id":            kb.ID,
				"reason":           "tenant lacks viewer permission for foreign-tenant KB",
			}, "search scope rejected: unauthorized foreign-tenant KB")
			return apperrors.NewNotFoundError("knowledge base not found")
		}
	}
	return nil
}
