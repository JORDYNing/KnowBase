package interfaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

// The repository owns all application/membership transactions. Network calls
// and cached system settings must never be used as authorization markers.
type TenantJoinRequestRepository interface {
	GetForUser(context.Context, uint64, string) (*types.TenantJoinRequest, error)
	Submit(context.Context, uint64, string) (*types.TenantJoinRequest, error)
	Resubmit(context.Context, uint64, string, uint64) (*types.TenantJoinRequest, error)
	BootstrapOwner(context.Context, uint64, string) (bool, error)
	List(context.Context, uint64, string, int, int) ([]*types.TenantJoinRequest, int64, error)
	Review(context.Context, uint64, uint64, uint64, bool, types.TenantRole, string, string) (*types.TenantJoinRequest, error)
}

type TenantJoinRequestService interface {
	EnsureFeishuApplication(context.Context, *types.User, string) error
	GetState(context.Context, string) (*types.TenantJoinRequestState, error)
	Submit(context.Context, string, uint64) (*types.TenantJoinRequestState, error)
	List(context.Context, uint64, string, int, int) ([]types.TenantJoinRequestResponse, int64, error)
	Review(context.Context, uint64, uint64, uint64, bool, types.TenantRole, string, string) (*types.TenantJoinRequest, error)
}
