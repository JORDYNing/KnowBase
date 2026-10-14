package types

import (
	"errors"
	"time"
)

const TeamTenantID uint64 = 10000
const TeamBootstrapOwnerEmail = "jordyn.ji@idreamsky.com"

type TenantJoinRequestStatus string

const (
	TenantJoinRequestPending  TenantJoinRequestStatus = "pending"
	TenantJoinRequestApproved TenantJoinRequestStatus = "approved"
	TenantJoinRequestRejected TenantJoinRequestStatus = "rejected"
	TenantJoinSourceFeishu                            = "feishu"
	TenantJoinSourceBootstrap                         = "feishu_bootstrap"
)

var (
	ErrJoinTargetUnavailable = errors.New("team workspace is unavailable")
	ErrJoinRequestNotFound   = errors.New("join request not found")
	ErrJoinRequestConflict   = errors.New("join request changed; refresh and try again")
	ErrJoinRequestForbidden  = errors.New("join request operation is not permitted")
	ErrJoinRoleInvalid       = errors.New("an allowed membership role is required when approving")
)

// TenantJoinRequest keeps one durable application per user and workspace.
// Approved bootstrap rows are permanent, preventing login from restoring a
// subsequently revoked Owner grant. They must never be deleted or resubmitted.
type TenantJoinRequest struct {
	ID            uint64                  `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID      uint64                  `json:"tenant_id" gorm:"not null;index"`
	UserID        string                  `json:"user_id" gorm:"type:varchar(36);not null;index"`
	Source        string                  `json:"source" gorm:"type:varchar(32);not null"`
	Status        TenantJoinRequestStatus `json:"status" gorm:"type:varchar(20);not null"`
	Role          TenantRole              `json:"role,omitempty" gorm:"type:varchar(20);not null;default:''"`
	ReviewMessage string                  `json:"review_message,omitempty" gorm:"type:varchar(500);not null;default:''"`
	ReviewedBy    *string                 `json:"reviewed_by,omitempty" gorm:"type:varchar(36)"`
	ReviewedAt    *time.Time              `json:"reviewed_at,omitempty"`
	Version       uint64                  `json:"version" gorm:"not null;default:1"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

func (TenantJoinRequest) TableName() string { return "tenant_join_requests" }

type TenantJoinRequestState struct {
	Request         *TenantJoinRequest `json:"request"`
	Tenant          *TeamTenantSummary `json:"tenant"`
	Membership      *Membership        `json:"membership"`
	TargetTenantID  uint64             `json:"target_tenant_id"`
	TargetAvailable bool               `json:"target_available"`
	CanResubmit     bool               `json:"can_resubmit"`
	AccessRevoked   bool               `json:"access_revoked"`
}
type TeamTenantSummary struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

type TenantJoinRequestResponse struct {
	TenantJoinRequest
	Username string `json:"username"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar,omitempty"`
}
