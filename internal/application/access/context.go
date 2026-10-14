package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Context grants contain values, not mutable KB objects. Deriving a context
// copies the grant slice so parallel search branches cannot widen each other.
type kbGrant struct {
	caller     types.Caller
	kbID       string
	tenantID   uint64
	permission types.KBPermission
	task       bool
}

// HasKBGrant checks only previously resolved resource access. It never infers
// ownership from the execution tenant, and always reapplies API-key scope.
func HasKBGrant(ctx context.Context, kbID string, tenantID uint64, required types.KBPermission) bool {
	if !required.IsValid() || types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID) != nil {
		return false
	}
	caller := types.CallerFromContext(ctx)
	if kbID == "" || tenantID == 0 {
		return false
	}
	grants, _ := ctx.Value(types.KBGrantsContextKey).([]kbGrant)
	for _, grant := range grants {
		if (caller.TenantID != 0 || grant.task) && grant.caller == caller && grant.kbID == kbID &&
			grant.tenantID == tenantID &&
			grant.permission.HasPermission(required) {
			return true
		}
	}
	return false
}

// KBPermissions combines tenant ownership and exact operation grants.
type KBPermissions struct {
	ctx    context.Context
	caller types.Caller
}

func NewKBPermissions(ctx context.Context) *KBPermissions {
	return &KBPermissions{ctx: ctx, caller: types.CallerFromContext(ctx)}
}
func (p *KBPermissions) Check(kbID string, ownerTenantID uint64, required types.KBPermission) (bool, error) {
	if kbID == "" || ownerTenantID == 0 || !required.IsValid() {
		return false, nil
	}
	if err := types.AuthorizeTenantAPIKeyKnowledgeBases(p.ctx, kbID); err != nil {
		return false, err
	}
	if HasKBGrant(p.ctx, kbID, ownerTenantID, required) {
		return true, nil
	}
	return p.caller.TenantID != 0 && p.caller.TenantID == ownerTenantID && required == types.KBPermissionRead, nil
}
