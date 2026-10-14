package service

import (
	"context"
	"errors"
	"strings"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type tenantJoinRequestService struct {
	repo    interfaces.TenantJoinRequestRepository
	tenants interfaces.TenantService
	members interfaces.TenantMemberService
	users   interfaces.UserRepository
}

func NewTenantJoinRequestService(repo interfaces.TenantJoinRequestRepository, tenants interfaces.TenantService, members interfaces.TenantMemberService, users interfaces.UserRepository) interfaces.TenantJoinRequestService {
	return &tenantJoinRequestService{repo: repo, tenants: tenants, members: members, users: users}
}

// verifiedEmail comes exclusively from the authenticated Feishu user_info.
// It is never inferred from a local profile, a request body or display name.
func (s *tenantJoinRequestService) EnsureFeishuApplication(ctx context.Context, user *types.User, verifiedEmail string) error {
	if user == nil || !user.IsActive || strings.TrimSpace(user.FeishuOpenID) == "" {
		return types.ErrJoinRequestForbidden
	}
	if strings.EqualFold(strings.TrimSpace(verifiedEmail), types.TeamBootstrapOwnerEmail) {
		granted, err := s.repo.BootstrapOwner(ctx, types.TeamTenantID, user.ID)
		if err != nil {
			return err
		}
		if granted {
			return nil
		}
	}
	_, err := s.repo.Submit(ctx, types.TeamTenantID, user.ID)
	return err
}
func (s *tenantJoinRequestService) GetState(ctx context.Context, userID string) (*types.TenantJoinRequestState, error) {
	request, err := s.repo.GetForUser(ctx, types.TeamTenantID, userID)
	if err != nil {
		return nil, err
	}
	state := &types.TenantJoinRequestState{Request: request, TargetTenantID: types.TeamTenantID}
	tenant, err := s.tenants.GetTenantByID(ctx, types.TeamTenantID)
	if err != nil && !errors.Is(err, apprepo.ErrTenantNotFound) {
		return nil, err
	}
	if tenant != nil {
		state.Tenant = &types.TeamTenantSummary{ID: tenant.ID, Name: tenant.Name}
		state.TargetAvailable = strings.EqualFold(strings.TrimSpace(tenant.Status), "active")
	}
	member, err := s.members.GetMembership(ctx, userID, types.TeamTenantID)
	if err != nil {
		return nil, err
	}
	if member != nil && member.Status == types.TenantMemberStatusActive {
		state.Membership = &types.Membership{TenantID: types.TeamTenantID, Role: member.Role}
		if tenant != nil {
			state.Membership.TenantName = tenant.Name
		}
	}
	state.AccessRevoked = request != nil && request.Status == types.TenantJoinRequestApproved && state.Membership == nil
	state.CanResubmit = state.TargetAvailable && member == nil && request != nil && request.Status == types.TenantJoinRequestRejected && request.Source != types.TenantJoinSourceBootstrap
	return state, nil
}
func (s *tenantJoinRequestService) Submit(ctx context.Context, userID string, version uint64) (*types.TenantJoinRequestState, error) {
	request, err := s.repo.GetForUser(ctx, types.TeamTenantID, userID)
	if err != nil {
		return nil, err
	}
	if request != nil && request.Status == types.TenantJoinRequestRejected {
		_, err = s.repo.Resubmit(ctx, types.TeamTenantID, userID, version)
	} else if request != nil && request.Status == types.TenantJoinRequestApproved {
		return nil, types.ErrJoinRequestConflict
	} else {
		_, err = s.repo.Submit(ctx, types.TeamTenantID, userID)
	}
	if err != nil {
		return nil, err
	}
	return s.GetState(ctx, userID)
}
func (s *tenantJoinRequestService) List(ctx context.Context, tenantID uint64, status string, page, pageSize int) ([]types.TenantJoinRequestResponse, int64, error) {
	if tenantID != types.TeamTenantID {
		return nil, 0, types.ErrJoinRequestNotFound
	}
	if status != "" && status != string(types.TenantJoinRequestPending) && status != string(types.TenantJoinRequestApproved) && status != string(types.TenantJoinRequestRejected) {
		return nil, 0, types.ErrJoinRequestConflict
	}
	rows, total, err := s.repo.List(ctx, tenantID, status, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	users, err := s.users.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	result := make([]types.TenantJoinRequestResponse, 0, len(rows))
	for _, row := range rows {
		item := types.TenantJoinRequestResponse{TenantJoinRequest: *row}
		if user := users[row.UserID]; user != nil {
			item.Username = user.Username
			item.Email = user.Email
			item.Avatar = user.Avatar
		}
		result = append(result, item)
	}
	return result, total, nil
}
func (s *tenantJoinRequestService) Review(ctx context.Context, tenantID, id, version uint64, approved bool, role types.TenantRole, reviewerID, message string) (*types.TenantJoinRequest, error) {
	if tenantID != types.TeamTenantID {
		return nil, types.ErrJoinRequestNotFound
	}
	message = strings.TrimSpace(message)
	if len([]rune(message)) > 500 {
		return nil, types.ErrJoinRequestConflict
	}
	request, err := s.repo.Review(ctx, tenantID, id, version, approved, role, reviewerID, message)
	if err != nil {
		return nil, err
	}
	return request, nil
}
