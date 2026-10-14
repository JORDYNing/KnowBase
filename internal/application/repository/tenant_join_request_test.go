package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func joinRequestDB(t *testing.T) (*gorm.DB, interfaces.TenantJoinRequestRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "join.db")+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.TenantMember{}, &types.AuditLog{}))
	migration, err := os.ReadFile("../../../migrations/sqlite/000036_tenant_join_requests.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_users_feishu_open_id ON users(feishu_open_id) WHERE feishu_open_id IS NOT NULL AND feishu_open_id <> ''").Error)
	require.NoError(t, db.Create(&types.Tenant{ID: types.TeamTenantID, Name: "Team", Status: "active"}).Error)
	for _, id := range []string{"alice", "owner", "admin", "bob"} {
		require.NoError(t, db.Omit("tenant_id").Create(&types.User{ID: id, Username: id, Email: id + "@example.com", PasswordHash: "unused", FeishuOpenID: "ou_" + id, IsActive: true}).Error)
	}
	require.NoError(t, db.Create(&types.TenantMember{TenantID: types.TeamTenantID, UserID: "owner", Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}).Error)
	require.NoError(t, db.Create(&types.TenantMember{TenantID: types.TeamTenantID, UserID: "admin", Role: types.TenantRoleAdmin, Status: types.TenantMemberStatusActive}).Error)
	return db, NewTenantJoinRequestRepository(db)
}
func TestTenantJoinConcurrentSubmissionAndVersionedReview(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := repo.Submit(ctx, types.TeamTenantID, "alice"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, db.Model(&types.TenantJoinRequest{}).Where("user_id = ?", "alice").Count(&count).Error)
	require.EqualValues(t, 1, count)
	request, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestPending, request.Status)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, "", "owner", "")
	require.ErrorIs(t, err, types.ErrJoinRoleInvalid)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleAdmin, "admin", "")
	require.ErrorIs(t, err, types.ErrJoinRoleInvalid)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleOwner, "owner", "")
	require.ErrorIs(t, err, types.ErrJoinRoleInvalid)
	approved, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "admin", "")
	require.NoError(t, err)
	require.EqualValues(t, request.Version+1, approved.Version)
	retry, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "admin", "")
	require.NoError(t, err)
	require.Equal(t, approved.Version, retry.Version)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleViewer, "admin", "")
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, false, "", "owner", "stale")
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	var member types.TenantMember
	require.NoError(t, db.Where("user_id = ? AND tenant_id = ?", "alice", types.TeamTenantID).First(&member).Error)
	require.Equal(t, types.TenantRoleContributor, member.Role)
	_, err = repo.Resubmit(ctx, types.TeamTenantID, "alice", approved.Version)
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
}
func TestTenantJoinRejectManualResubmitRequiresCurrentVersion(t *testing.T) {
	_, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	rejected, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, false, "", "owner", "Ask again later")
	require.NoError(t, err)
	same, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestRejected, same.Status)
	_, err = repo.Resubmit(ctx, types.TeamTenantID, "alice", request.Version)
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	pending, err := repo.Resubmit(ctx, types.TeamTenantID, "alice", rejected.Version)
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestPending, pending.Status)
	require.EqualValues(t, 3, pending.Version)
	require.Nil(t, pending.ReviewedAt)
	_, err = repo.Resubmit(ctx, types.TeamTenantID, "alice", rejected.Version)
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
}
func TestTenantJoinApprovalRollsBackIfMembershipInsertFails(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_join_member", func(tx *gorm.DB) {
		if tx.Statement.Table == "tenant_members" {
			tx.AddError(errors.New("injected membership failure"))
		}
	}))
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "owner", "")
	require.ErrorContains(t, err, "injected")
	stored, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestPending, stored.Status)
	require.EqualValues(t, 1, stored.Version)
	assertNullTenantID(t, db, "alice")
}
func TestTenantJoinBootstrapIsOncePerWorkspaceAndNeverRestoresOwner(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	granted, err := repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.True(t, granted)
	request, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinSourceBootstrap, request.Source)
	require.Equal(t, types.TenantRoleOwner, request.Role)
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", "alice").Update("role", types.TenantRoleViewer).Error)
	granted, err = repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.False(t, granted)
	var member types.TenantMember
	require.NoError(t, db.Where("user_id = ?", "alice").First(&member).Error)
	require.Equal(t, types.TenantRoleViewer, member.Role)
	require.NoError(t, db.Where("user_id = ?", "alice").Delete(&types.TenantMember{}).Error)
	granted, err = repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.False(t, granted)
	_, err = repo.Resubmit(ctx, types.TeamTenantID, "alice", request.Version)
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	granted, err = repo.BootstrapOwner(ctx, types.TeamTenantID, "bob")
	require.NoError(t, err)
	require.False(t, granted)
	var count int64
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id IN ?", []string{"alice", "bob"}).Count(&count).Error)
	require.Zero(t, count)
}
func TestTenantJoinBootstrapRollsBackMarkerOnFailure(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_bootstrap_member", func(tx *gorm.DB) {
		if tx.Statement.Table == "tenant_members" {
			tx.AddError(errors.New("injected bootstrap failure"))
		}
	}))
	granted, err := repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.Error(t, err)
	require.False(t, granted)
	row, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Nil(t, row)
	require.NoError(t, db.Callback().Create().Remove("fail_bootstrap_member"))
	granted, err = repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.True(t, granted)
}
func TestTenantJoinUnavailableTargetAndCrossTenantReview(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	_, err := repo.Submit(ctx, 99999, "alice")
	require.ErrorIs(t, err, types.ErrJoinTargetUnavailable)
	require.NoError(t, db.Model(&types.Tenant{}).Where("id = ?", types.TeamTenantID).Update("status", "inactive").Error)
	_, err = repo.Submit(ctx, types.TeamTenantID, "alice")
	require.ErrorIs(t, err, types.ErrJoinTargetUnavailable)
	granted, err := repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.ErrorIs(t, err, types.ErrJoinTargetUnavailable)
	require.False(t, granted)
	require.NoError(t, db.Model(&types.Tenant{}).Where("id = ?", types.TeamTenantID).Update("status", "active").Error)
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "bob", "")
	require.ErrorIs(t, err, types.ErrJoinRequestForbidden)
}
func TestTenantJoinRemovedApprovedMemberDoesNotAutoRejoin(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleAdmin, "owner", "")
	require.NoError(t, err)
	require.NoError(t, db.Where("user_id = ?", "alice").Delete(&types.TenantMember{}).Error)
	same, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestApproved, same.Status)
	retry, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleAdmin, "owner", "")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestApproved, retry.Status)
	_, err = repo.Resubmit(ctx, types.TeamTenantID, "alice", same.Version)
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	var count int64
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", "alice").Count(&count).Error)
	require.Zero(t, count)
}
func TestTeamFeishuIdentityBindingIsConditionalAndAvatarDoesNotOverwriteTenant(t *testing.T) {
	db, _ := joinRequestDB(t)
	ctx := context.Background()
	repo := NewUserRepository(db).(interfaces.FeishuIdentityRepository)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "alice").Update("feishu_open_id", nil).Error)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, openID := range []string{"ou_one", "ou_two"} {
		wg.Add(1)
		go func(openID string) {
			defer wg.Done()
			_, err := repo.BindFeishuIdentity(ctx, "alice", openID)
			results <- err
		}(openID)
	}
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrFeishuIdentityAlreadyBound)
			conflicts++
		}
	}
	require.Equal(t, 1, winners)
	require.Equal(t, 1, conflicts)
	var stored types.User
	require.NoError(t, db.First(&stored, "id = ?", "alice").Error)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "alice").Update("tenant_id", types.TeamTenantID).Error)
	require.NoError(t, repo.UpdateFeishuAvatar(ctx, "alice", stored.FeishuOpenID, "https://example.com/avatar.png"))
	require.NoError(t, db.First(&stored, "id = ?", "alice").Error)
	require.Equal(t, types.TeamTenantID, stored.TenantID)
	// A profile write based on a snapshot taken before the binding cannot
	// clear or replace the new provider identity.
	stale := stored
	stale.FeishuOpenID = ""
	stale.Username = "updated-profile"
	require.NoError(t, NewUserRepository(db).UpdateUser(ctx, &stale))
	reloaded, err := NewUserRepository(db).GetUserByID(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, stored.FeishuOpenID, reloaded.FeishuOpenID)
}
