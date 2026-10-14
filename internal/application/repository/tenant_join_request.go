package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type tenantJoinRequestRepository struct{ db *gorm.DB }

func NewTenantJoinRequestRepository(db *gorm.DB) interfaces.TenantJoinRequestRepository {
	return &tenantJoinRequestRepository{db: db}
}

func (r *tenantJoinRequestRepository) GetForUser(ctx context.Context, tenantID uint64, userID string) (*types.TenantJoinRequest, error) {
	var request types.TenantJoinRequest
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &request, err
}

// Writes lock the workspace first, then actor/request identity and request
// rows, and finally mutable membership rows. User locks keep deactivation
// serialized with authorization; reviewer member locks do the same for roles.
// SQLite has no FOR UPDATE: acquire its writer lock before any reads so a
// read-to-write upgrade cannot race another application transaction.
func lockJoinTarget(tx *gorm.DB, tenantID uint64) error {
	if tx.Dialector.Name() == "sqlite" {
		if err := tx.Model(&types.Tenant{}).Where("id = ?", tenantID).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error; err != nil {
			return err
		}
	}
	var tenant types.Tenant
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", tenantID).First(&tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.ErrJoinTargetUnavailable
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(tenant.Status), "active") {
		return types.ErrJoinTargetUnavailable
	}
	return nil
}
func joinApplicant(tx *gorm.DB, userID string) error {
	var user types.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}
	if !user.IsActive || strings.TrimSpace(user.FeishuOpenID) == "" {
		return types.ErrJoinRequestForbidden
	}
	return nil
}
func findJoinRequest(tx *gorm.DB, tenantID uint64, userID string) (*types.TenantJoinRequest, error) {
	var request types.TenantJoinRequest
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND user_id = ?", tenantID, userID).First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &request, err
}
func findJoinMembership(tx *gorm.DB, tenantID uint64, userID string) (*types.TenantMember, error) {
	var member types.TenantMember
	err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order("CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END, id DESC").First(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &member, err
}

// joinRequestSnapshot includes explicit empty/null decision fields so history
// remains complete after resubmission clears the mutable application row.
func joinRequestSnapshot(request *types.TenantJoinRequest) map[string]any {
	return map[string]any{
		"id": request.ID, "tenant_id": request.TenantID, "user_id": request.UserID,
		"source": request.Source, "status": request.Status, "role": request.Role,
		"review_message": request.ReviewMessage, "reviewed_by": request.ReviewedBy,
		"reviewed_at": request.ReviewedAt, "version": request.Version,
		"created_at": request.CreatedAt, "updated_at": request.UpdatedAt,
	}
}

func writeJoinAudit(tx *gorm.DB, action string, request, previous *types.TenantJoinRequest, actor, actorRole string, requestedRole types.TenantRole) error {
	details := joinRequestSnapshot(request)
	if previous != nil {
		details["previous_request"] = joinRequestSnapshot(previous)
	}
	if action == "tenant_join.reviewed" {
		details["requested_role"] = requestedRole
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return tx.Create(&types.AuditLog{
		TenantID: request.TenantID, ActorUserID: actor, ActorRole: actorRole,
		Action: types.AuditAction(action), TargetType: "tenant_join_request",
		TargetID: strconv.FormatUint(request.ID, 10), TargetUserID: request.UserID,
		Outcome: types.AuditOutcomeSuccess, Details: types.JSON(encoded), CreatedAt: time.Now(),
	}).Error
}

// Exact replay is checked against the committed decision rather than the
// assigned role: an already-active membership may have retained another role.
func sameJoinReview(tx *gorm.DB, request *types.TenantJoinRequest, role types.TenantRole, message string) (bool, error) {
	var event types.AuditLog
	err := tx.Where("tenant_id = ? AND action = ? AND target_type = ? AND target_id = ?",
		request.TenantID, "tenant_join.reviewed", "tenant_join_request", strconv.FormatUint(request.ID, 10)).
		Order("id DESC").First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var decision struct {
		Version       uint64                        `json:"version"`
		Status        types.TenantJoinRequestStatus `json:"status"`
		RequestedRole *types.TenantRole             `json:"requested_role"`
		ReviewMessage string                        `json:"review_message"`
	}
	if err := json.Unmarshal(event.Details, &decision); err != nil {
		return false, err
	}
	return decision.Version == request.Version && decision.Status == request.Status && decision.RequestedRole != nil && *decision.RequestedRole == role && decision.ReviewMessage == message, nil
}

func (r *tenantJoinRequestRepository) Submit(ctx context.Context, tenantID uint64, userID string) (*types.TenantJoinRequest, error) {
	var result *types.TenantJoinRequest
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockJoinTarget(tx, tenantID); err != nil {
			return err
		}
		if err := joinApplicant(tx, userID); err != nil {
			return err
		}
		request, err := findJoinRequest(tx, tenantID, userID)
		if err != nil {
			return err
		}
		if request != nil {
			result = request
			return nil
		}
		// A removed/suspended account must be handled by a member administrator,
		// never implicitly reopened by logging in again.
		member, err := findJoinMembership(tx, tenantID, userID)
		if err != nil {
			return err
		}
		if member != nil {
			return nil
		}
		request = &types.TenantJoinRequest{TenantID: tenantID, UserID: userID, Source: types.TenantJoinSourceFeishu, Status: types.TenantJoinRequestPending, Version: 1}
		if err := tx.Create(request).Error; err != nil {
			return err
		}
		result = request
		return nil
	})
	return result, err
}

func (r *tenantJoinRequestRepository) Resubmit(ctx context.Context, tenantID uint64, userID string, version uint64) (*types.TenantJoinRequest, error) {
	var result *types.TenantJoinRequest
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockJoinTarget(tx, tenantID); err != nil {
			return err
		}
		if err := joinApplicant(tx, userID); err != nil {
			return err
		}
		request, err := findJoinRequest(tx, tenantID, userID)
		if err != nil {
			return err
		}
		if request == nil {
			return types.ErrJoinRequestNotFound
		}
		if request.Source == types.TenantJoinSourceBootstrap || request.Status != types.TenantJoinRequestRejected || version == 0 || request.Version != version {
			return types.ErrJoinRequestConflict
		}
		member, err := findJoinMembership(tx, tenantID, userID)
		if err != nil {
			return err
		}
		if member != nil && !member.DeletedAt.Valid {
			return types.ErrJoinRequestForbidden
		}
		previous := *request
		result = request
		now := time.Now()
		res := tx.Model(&types.TenantJoinRequest{}).Where("id = ? AND version = ? AND status = ?", request.ID, version, types.TenantJoinRequestRejected).
			Updates(map[string]any{"status": types.TenantJoinRequestPending, "role": "", "review_message": "", "reviewed_by": nil, "reviewed_at": nil, "version": version + 1, "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return types.ErrJoinRequestConflict
		}
		result.Status = types.TenantJoinRequestPending
		result.Role = ""
		result.ReviewMessage = ""
		result.ReviewedBy = nil
		result.ReviewedAt = nil
		result.Version++
		result.UpdatedAt = now
		return writeJoinAudit(tx, "tenant_join.resubmitted", result, &previous, userID, "applicant", "")
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// writeJoinMembership is used only after an explicit administrator approval
// or the one-off trusted Feishu bootstrap. Preserve an existing active role
// on ordinary approvals; bootstrap explicitly promotes to Owner once.
func writeJoinMembership(tx *gorm.DB, tenantID uint64, userID string, role types.TenantRole, reviewer *string, bootstrap bool) (types.TenantRole, error) {
	if !bootstrap {
		// This path does not mutate an existing active member. Avoid locking a
		// second Owner while holding the reviewer lock: Owner demotion locks the
		// other Owners first and would otherwise form a lock cycle.
		var active types.TenantMember
		err := tx.Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, types.TenantMemberStatusActive).First(&active).Error
		if err == nil {
			return active.Role, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
	}
	member, err := findJoinMembership(tx, tenantID, userID)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if member == nil {
		member = &types.TenantMember{TenantID: tenantID, UserID: userID, Role: role, Status: types.TenantMemberStatusActive, InvitedBy: reviewer, JoinedAt: now}
		if err := tx.Create(member).Error; err != nil {
			return "", err
		}
	} else {
		if err := tx.Unscoped().Model(&types.TenantMember{}).Where("id = ?", member.ID).
			Updates(map[string]any{"role": role, "status": types.TenantMemberStatusActive, "invited_by": reviewer, "joined_at": now, "deleted_at": nil, "updated_at": now}).Error; err != nil {
			return "", err
		}
	}
	if err := tx.Model(&types.User{}).Where("id = ? AND (tenant_id IS NULL OR tenant_id = 0)", userID).UpdateColumn("tenant_id", tenantID).Error; err != nil {
		return "", err
	}
	return role, nil
}

func (r *tenantJoinRequestRepository) BootstrapOwner(ctx context.Context, tenantID uint64, userID string) (bool, error) {
	granted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockJoinTarget(tx, tenantID); err != nil {
			return err
		}
		if err := joinApplicant(tx, userID); err != nil {
			return err
		}
		var prior types.TenantJoinRequest
		err := tx.Where("tenant_id = ? AND source = ?", tenantID, types.TenantJoinSourceBootstrap).First(&prior).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		request, err := findJoinRequest(tx, tenantID, userID)
		if err != nil {
			return err
		}
		var previous *types.TenantJoinRequest
		if request != nil {
			copy := *request
			previous = &copy
		}
		// Claim the permanent marker before granting membership; both writes roll
		// back together if anything fails. The partial unique index also guards
		// distinct local accounts that resolve the same bootstrap email.
		now := time.Now()
		if request == nil {
			request = &types.TenantJoinRequest{TenantID: tenantID, UserID: userID, Source: types.TenantJoinSourceBootstrap, Status: types.TenantJoinRequestApproved, Role: types.TenantRoleOwner, ReviewedAt: &now, Version: 1}
			if err := tx.Create(request).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(request).Updates(map[string]any{"source": types.TenantJoinSourceBootstrap, "status": types.TenantJoinRequestApproved, "role": types.TenantRoleOwner, "review_message": "", "reviewed_by": nil, "reviewed_at": now, "version": request.Version + 1}).Error; err != nil {
				return err
			}
		}
		if _, err := writeJoinMembership(tx, tenantID, userID, types.TenantRoleOwner, nil, true); err != nil {
			return err
		}
		if err := tx.Where("id = ?", request.ID).First(request).Error; err != nil {
			return err
		}
		if err := writeJoinAudit(tx, "tenant_join.bootstrap_owner", request, previous, userID, "system", ""); err != nil {
			return err
		}
		granted = true
		return nil
	})
	return granted && err == nil, err
}

func (r *tenantJoinRequestRepository) List(ctx context.Context, tenantID uint64, status string, offset, limit int) ([]*types.TenantJoinRequest, int64, error) {
	q := r.db.WithContext(ctx).Model(&types.TenantJoinRequest{}).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*types.TenantJoinRequest
	err := q.Order("updated_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func (r *tenantJoinRequestRepository) Review(ctx context.Context, tenantID, id, version uint64, approved bool, role types.TenantRole, reviewerID, message string) (*types.TenantJoinRequest, error) {
	// Roles are irrelevant to rejection. Normalize both the initial decision
	// and every retry before comparing their canonical audit snapshots.
	if !approved {
		role = ""
	}
	requestedRole := role
	var result *types.TenantJoinRequest
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockJoinTarget(tx, tenantID); err != nil {
			return err
		}
		// Both user deactivation and role revocation must serialize with review.
		if err := joinApplicant(tx, reviewerID); err != nil {
			return err
		}
		var reviewer types.TenantMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, reviewerID, types.TenantMemberStatusActive).First(&reviewer).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return types.ErrJoinRequestForbidden
			}
			return err
		}
		if reviewer.Role != types.TenantRoleOwner && reviewer.Role != types.TenantRoleAdmin {
			return types.ErrJoinRequestForbidden
		}
		if approved && (!role.IsValid() || role == types.TenantRoleOwner || (reviewer.Role == types.TenantRoleAdmin && role == types.TenantRoleAdmin)) {
			return types.ErrJoinRoleInvalid
		}
		var request types.TenantJoinRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&request).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return types.ErrJoinRequestNotFound
			}
			return err
		}
		if request.Source == types.TenantJoinSourceBootstrap {
			return types.ErrJoinRequestConflict
		}
		if request.Status != types.TenantJoinRequestPending {
			expectedStatus := types.TenantJoinRequestRejected
			if approved {
				expectedStatus = types.TenantJoinRequestApproved
			}
			// A transport retry of the exact completed decision is a read-only
			// success. Never recreate membership if it was subsequently revoked.
			if version > 0 && request.Version == version+1 && request.Status == expectedStatus {
				same, err := sameJoinReview(tx, &request, requestedRole, message)
				if err != nil {
					return err
				}
				if same {
					result = &request
					return nil
				}
			}
			return types.ErrJoinRequestConflict
		}
		if version == 0 || request.Version != version {
			return types.ErrJoinRequestConflict
		}
		if err := joinApplicant(tx, request.UserID); err != nil {
			return err
		}
		previous := request
		status := types.TenantJoinRequestRejected
		if approved {
			status = types.TenantJoinRequestApproved
			actualRole, err := writeJoinMembership(tx, tenantID, request.UserID, role, &reviewerID, false)
			if err != nil {
				return err
			}
			role = actualRole
		} else {
			role = ""
		}
		now := time.Now()
		res := tx.Model(&types.TenantJoinRequest{}).Where("id = ? AND tenant_id = ? AND version = ? AND status = ?", id, tenantID, version, types.TenantJoinRequestPending).
			Updates(map[string]any{"status": status, "role": role, "reviewed_by": reviewerID, "review_message": message, "reviewed_at": now, "version": version + 1, "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return types.ErrJoinRequestConflict
		}
		request.Status = status
		request.Role = role
		request.ReviewedBy = &reviewerID
		request.ReviewMessage = message
		request.ReviewedAt = &now
		request.Version++
		request.UpdatedAt = now
		result = &request
		return writeJoinAudit(tx, "tenant_join.reviewed", &request, &previous, reviewerID, string(reviewer.Role), requestedRole)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
