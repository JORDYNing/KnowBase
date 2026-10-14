package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func joinAuditDetails(t *testing.T, event *types.AuditLog) map[string]any {
	t.Helper()
	var details map[string]any
	require.NoError(t, json.Unmarshal(event.Details, &details))
	return details
}
func countJoinAudit(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&types.AuditLog{}).Where("target_type = ?", "tenant_join_request").Count(&count).Error)
	return count
}
func failJoinAuditWrites(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_join_audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_logs" {
			tx.AddError(errors.New("injected audit failure"))
		}
	}))
	var once sync.Once
	stop := func() { once.Do(func() { require.NoError(t, db.Callback().Create().Remove("fail_join_audit")) }) }
	t.Cleanup(stop)
	return stop
}
func requireNoJoinMember(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ? AND tenant_id = ?", userID, types.TeamTenantID).Count(&count).Error)
	require.Zero(t, count)
}

func TestTenantJoinAuditKeepsRejectedDecisionAfterResubmission(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	rejected, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, false, types.TenantRoleAdmin, "owner", "Please confirm your team")
	require.NoError(t, err)
	require.Empty(t, rejected.Role)
	// Rejection ignores a role consistently, including legacy clients that send it.
	replay, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, false, types.TenantRoleOwner, "owner", "Please confirm your team")
	require.NoError(t, err)
	require.Equal(t, rejected.Version, replay.Version)
	require.EqualValues(t, 1, countJoinAudit(t, db))
	pending, err := repo.Resubmit(ctx, types.TeamTenantID, "alice", rejected.Version)
	require.NoError(t, err)
	require.Empty(t, pending.ReviewMessage)
	require.Nil(t, pending.ReviewedBy)
	require.Nil(t, pending.ReviewedAt)
	// The service's repeated POST sees pending and takes this idempotent path.
	same, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, pending.Version, same.Version)
	events, err := NewAuditLogRepository(db).List(ctx, types.TeamTenantID, &interfaces.AuditLogQuery{Limit: 100})
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, types.AuditAction("tenant_join.resubmitted"), events[0].Action)
	require.Equal(t, "alice", events[0].ActorUserID)
	current := joinAuditDetails(t, events[0])
	require.Equal(t, "pending", current["status"])
	require.Equal(t, "", current["review_message"])
	require.Nil(t, current["reviewed_by"])
	require.Nil(t, current["reviewed_at"])
	previous := current["previous_request"].(map[string]any)
	require.Equal(t, "Please confirm your team", previous["review_message"])
	require.Equal(t, "owner", previous["reviewed_by"])
	require.NotEmpty(t, previous["reviewed_at"])
	require.Equal(t, "rejected", previous["status"])
	require.Equal(t, float64(rejected.Version), previous["version"])
	require.Equal(t, types.TenantJoinSourceFeishu, previous["source"])
	decision := joinAuditDetails(t, events[1])
	require.Equal(t, "Please confirm your team", decision["review_message"])
	require.Equal(t, "owner", decision["reviewed_by"])
	require.NotEmpty(t, decision["reviewed_at"])
	require.Equal(t, "", decision["requested_role"])
	require.Equal(t, types.AuditAction("tenant_join.reviewed"), events[1].Action)
	require.Equal(t, "owner", events[1].ActorUserID)
	require.Equal(t, "owner", events[1].ActorRole)
}

func TestTenantJoinAuditFailureRollsBackApprovalAndRetryIsExactlyOnce(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	stop := failJoinAuditWrites(t, db)
	result, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "owner", "Welcome")
	require.ErrorContains(t, err, "injected audit failure")
	require.Nil(t, result)
	stored, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, types.TenantJoinRequestPending, stored.Status)
	require.Equal(t, request.Version, stored.Version)
	requireNoJoinMember(t, db, "alice")
	assertNullTenantID(t, db, "alice")
	require.Zero(t, countJoinAudit(t, db))
	stop()
	approved, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "owner", "Welcome")
	require.NoError(t, err)
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "owner", "Welcome")
	require.NoError(t, err)
	require.EqualValues(t, 1, countJoinAudit(t, db))
	// Replaying after removal may return the decision but cannot re-add the user.
	require.NoError(t, db.Where("user_id = ?", "alice").Delete(&types.TenantMember{}).Error)
	replay, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "owner", "Welcome")
	require.NoError(t, err)
	require.Equal(t, approved.Version, replay.Version)
	requireNoJoinMember(t, db, "alice")
	require.EqualValues(t, 1, countJoinAudit(t, db))
}

func TestTenantJoinAuditFailureRollsBackBootstrapMarkerAndOwnership(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	stop := failJoinAuditWrites(t, db)
	granted, err := repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.ErrorContains(t, err, "injected audit failure")
	require.False(t, granted)
	marker, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Nil(t, marker)
	requireNoJoinMember(t, db, "alice")
	assertNullTenantID(t, db, "alice")
	require.Zero(t, countJoinAudit(t, db))
	stop()
	granted, err = repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.True(t, granted)
	granted, err = repo.BootstrapOwner(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.False(t, granted)
	require.EqualValues(t, 1, countJoinAudit(t, db))
	var event types.AuditLog
	require.NoError(t, db.Where("action = ?", "tenant_join.bootstrap_owner").First(&event).Error)
	details := joinAuditDetails(t, &event)
	require.Equal(t, "owner", details["role"])
	require.Equal(t, types.TenantJoinSourceBootstrap, details["source"])
	require.NotEmpty(t, details["reviewed_at"])
}

func TestTenantJoinAuditFailureRollsBackResubmission(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	rejected, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, false, "", "owner", "Manager confirmation required")
	require.NoError(t, err)
	stop := failJoinAuditWrites(t, db)
	result, err := repo.Resubmit(ctx, types.TeamTenantID, "alice", rejected.Version)
	require.ErrorContains(t, err, "injected audit failure")
	require.Nil(t, result)
	stored, err := repo.GetForUser(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.Equal(t, rejected.Status, stored.Status)
	require.Equal(t, rejected.Version, stored.Version)
	require.Equal(t, rejected.ReviewMessage, stored.ReviewMessage)
	require.Equal(t, rejected.ReviewedBy, stored.ReviewedBy)
	require.NotNil(t, stored.ReviewedAt)
	require.True(t, rejected.ReviewedAt.Equal(*stored.ReviewedAt))
	require.EqualValues(t, 1, countJoinAudit(t, db))
	stop()
	pending, err := repo.Resubmit(ctx, types.TeamTenantID, "alice", rejected.Version)
	require.NoError(t, err)
	require.EqualValues(t, rejected.Version+1, pending.Version)
	require.EqualValues(t, 2, countJoinAudit(t, db))
}

func TestTenantJoinReviewReplayUsesChosenRoleWhenExistingRoleIsPreserved(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	require.NoError(t, db.Create(&types.TenantMember{TenantID: types.TeamTenantID, UserID: "alice", Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}).Error)
	approved, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleViewer, "owner", "Already on team")
	require.NoError(t, err)
	require.Equal(t, types.TenantRoleContributor, approved.Role)
	replay, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleViewer, "owner", "Already on team")
	require.NoError(t, err)
	require.Equal(t, approved.Version, replay.Version)
	// Merely matching the actual assigned role is not an identical decision.
	_, err = repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleContributor, "owner", "Already on team")
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	var event types.AuditLog
	require.NoError(t, db.Where("target_id = ?", strconv.FormatUint(request.ID, 10)).First(&event).Error)
	details := joinAuditDetails(t, &event)
	require.Equal(t, "viewer", details["requested_role"])
	require.Equal(t, "contributor", details["role"])
	require.EqualValues(t, 1, countJoinAudit(t, db))
}

// This deterministic driver captures the actual PostgreSQL authorization SQL
// without requiring a PostgreSQL server or an additional test dependency.
var joinLockDriverID atomic.Uint64

type joinLockDriver struct {
	queries           []string
	inactiveApplicant bool
	userReads         int
	rolledBack        bool
	committed         bool
}

func (d *joinLockDriver) Open(string) (driver.Conn, error) { return &joinLockConn{state: d}, nil }

type joinLockConn struct{ state *joinLockDriver }

func (c *joinLockConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (c *joinLockConn) Close() error              { return nil }
func (c *joinLockConn) Begin() (driver.Tx, error) { return &joinLockTx{state: c.state}, nil }
func (c *joinLockConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.queries = append(c.state.queries, query)
	switch {
	case strings.Contains(query, `FROM "tenants"`):
		return &joinLockRows{columns: []string{"id", "status"}, values: []driver.Value{int64(types.TeamTenantID), "active"}}, nil
	case strings.Contains(query, `FROM "users"`):
		c.state.userReads++
		id, active := "owner", true
		if c.state.userReads == 2 {
			id, active = "alice", false
		}
		return &joinLockRows{columns: []string{"id", "is_active", "feishu_open_id"}, values: []driver.Value{id, active, "ou_" + id}}, nil
	case strings.Contains(query, `FROM "tenant_members"`):
		role := string(types.TenantRoleContributor)
		if c.state.inactiveApplicant {
			role = string(types.TenantRoleOwner)
		}
		return &joinLockRows{columns: []string{"user_id", "tenant_id", "role", "status"}, values: []driver.Value{"owner", int64(types.TeamTenantID), role, "active"}}, nil
	case strings.Contains(query, `FROM "tenant_join_requests"`):
		return &joinLockRows{columns: []string{"id", "tenant_id", "user_id", "source", "status", "version"}, values: []driver.Value{int64(1), int64(types.TeamTenantID), "alice", "feishu", "pending", int64(1)}}, nil
	default:
		return nil, errors.New("unexpected authorization query: " + query)
	}
}

type joinLockTx struct{ state *joinLockDriver }

func (tx *joinLockTx) Commit() error   { tx.state.committed = true; return nil }
func (tx *joinLockTx) Rollback() error { tx.state.rolledBack = true; return nil }

type joinLockRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *joinLockRows) Columns() []string { return r.columns }
func (r *joinLockRows) Close() error      { return nil }
func (r *joinLockRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(dest, r.values)
	return nil
}

// SQLite serializes writers globally, so its functional concurrency test
// cannot by itself detect missing PostgreSQL reviewer/applicant row locks.
func TestTenantJoinPostgresLocksReviewerAndApplicantBeforeAuthorization(t *testing.T) {
	for _, inactiveApplicant := range []bool{false, true} {
		t.Run(strconv.FormatBool(inactiveApplicant), func(t *testing.T) {
			state := &joinLockDriver{inactiveApplicant: inactiveApplicant}
			driverName := "join_review_locks_" + strconv.FormatUint(joinLockDriverID.Add(1), 10)
			sql.Register(driverName, state)
			sqlDB, err := sql.Open(driverName, "")
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			_, err = NewTenantJoinRequestRepository(db).Review(context.Background(), types.TeamTenantID, 1, 1, true, types.TenantRoleAdmin, "owner", "")
			require.ErrorIs(t, err, types.ErrJoinRequestForbidden)
			tables := []string{"tenants", "users", "tenant_members"}
			if inactiveApplicant {
				tables = append(tables, "tenant_join_requests", "users")
			}
			require.Len(t, state.queries, len(tables))
			for i, table := range tables {
				require.Contains(t, state.queries[i], `FROM "`+table+`"`)
				require.True(t, strings.HasSuffix(strings.TrimSpace(state.queries[i]), "FOR UPDATE"), state.queries[i])
			}
			require.True(t, state.rolledBack)
			require.False(t, state.committed)
		})
	}
}

func TestTenantJoinConcurrentReviewerRevocationSerializesWithReview(t *testing.T) {
	db, repo := joinRequestDB(t)
	ctx := context.Background()
	request, err := repo.Submit(ctx, types.TeamTenantID, "alice")
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	reached := make(chan struct{})
	resume := make(chan struct{})
	var reachedOnce, resumeOnce sync.Once
	t.Cleanup(func() { resumeOnce.Do(func() { close(resume) }) })
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("pause_join_reviewer", func(tx *gorm.DB) {
		reviewer, ok := tx.Statement.Dest.(*types.TenantMember)
		if ok && tx.Statement.Table == "tenant_members" && reviewer.UserID == "owner" {
			reachedOnce.Do(func() { close(reached); <-resume })
		}
	}))
	reviewDone := make(chan error, 1)
	go func() {
		_, err := repo.Review(ctx, types.TeamTenantID, request.ID, request.Version, true, types.TenantRoleAdmin, "owner", "")
		reviewDone <- err
	}()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("review did not reach the locked reviewer")
	}
	demoteStarted := make(chan struct{})
	demoteDone := make(chan error, 1)
	go func() {
		close(demoteStarted)
		demoteDone <- db.Model(&types.TenantMember{}).Where("user_id = ? AND tenant_id = ?", "owner", types.TeamTenantID).Update("role", types.TenantRoleContributor).Error
	}()
	<-demoteStarted
	select {
	case err := <-demoteDone:
		t.Fatalf("reviewer changed while authorization transaction was open: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	resumeOnce.Do(func() { close(resume) })
	select {
	case err := <-reviewDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("review did not finish")
	}
	select {
	case err := <-demoteDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("revocation did not finish after review")
	}
	var member types.TenantMember
	require.NoError(t, db.Where("user_id = ? AND tenant_id = ?", "alice", types.TeamTenantID).First(&member).Error)
	require.Equal(t, types.TenantRoleAdmin, member.Role)
	var reviewer types.TenantMember
	require.NoError(t, db.Where("user_id = ? AND tenant_id = ?", "owner", types.TeamTenantID).First(&reviewer).Error)
	require.Equal(t, types.TenantRoleContributor, reviewer.Role)
}
