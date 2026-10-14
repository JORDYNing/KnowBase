package handler

import (
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// registerByInviteRequest is the body for /auth/register-by-invite.
//
// Email is collected from the invitee themselves: share-link rows have
// no specific invitee, so we cannot pre-bind any address. The token
// IS the authorisation; the email is whatever account the invitee
// wants to register.
type registerByInviteRequest struct {
	Token    string `json:"token"    binding:"required"`
	Email    string `json:"email"    binding:"required,email"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

// invitationLookupResponse is the public projection of a share-link
// row returned by /auth/invitations/lookup. Kept narrow on purpose:
// just enough for the registration page to render context ("X invited
// you to Y") without exposing inviter audit fields.
type invitationLookupResponse struct {
	TenantID   uint64           `json:"tenant_id"`
	TenantName string           `json:"tenant_name,omitempty"`
	Role       types.TenantRole `json:"role"`
	ExpiresAt  string           `json:"expires_at"`
}

// invitationLookupRequest carries the token in the request body
// instead of the URL path: GET /auth/invitations/:token would have
// landed the plaintext token in access logs, browser history, and
// tracing spans. POST + body keeps the token out of those surfaces
// at the cost of "this endpoint reads, why is it POST" — worth it
// because the token is the sole authorisation for registration.
type invitationLookupRequest struct {
	Token string `json:"token" binding:"required"`
}

// LookupInvitationByToken always rejects the disabled public invitation flow.
func (h *AuthHandler) LookupInvitationByToken(c *gin.Context) {
	_ = c.Error(apperrors.NewForbiddenError("Only Feishu login is available"))
}

// RegisterByInvite always rejects the disabled invitation registration flow.
func (h *AuthHandler) RegisterByInvite(c *gin.Context) {
	_ = c.Error(apperrors.NewForbiddenError("Only Feishu login is available"))
}
