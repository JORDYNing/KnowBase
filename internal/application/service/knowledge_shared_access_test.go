package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupKnowledgeSharedAccessDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	return db
}

func newKnowledgeSharedAccessService(t *testing.T) (*knowledgeService, *gorm.DB) {
	t.Helper()

	db := setupKnowledgeSharedAccessDB(t)
	repo := repository.NewKnowledgeRepository(db)
	return &knowledgeService{
		repo: repo,
	}, db
}

func newSharedAccessContext() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "user-1")
	return ctx
}

func seedKnowledge(t *testing.T, db *gorm.DB, knowledge *types.Knowledge) {
	t.Helper()
	require.NoError(t, db.Create(knowledge).Error)
}

func TestAuthorizedKnowledgeBatchRejectsForeignWithoutGrant(t *testing.T) {
	service, db := newKnowledgeSharedAccessService(t)
	seedKnowledge(t, db, &types.Knowledge{ID: "own", TenantID: 1, KnowledgeBaseID: "own-kb", Type: "file"})
	seedKnowledge(t, db, &types.Knowledge{ID: "foreign", TenantID: 2, KnowledgeBaseID: "foreign-kb", Type: "file"})
	rows, err := service.GetAuthorizedKnowledgeBatch(newSharedAccessContext(), 1, []string{"own", "foreign"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "own", rows[0].ID)
}
