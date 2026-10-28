package repository

import (
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTenantYouTubeConfigurationPersistsUsingMigrationColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tenants.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Use the actual migration's column name rather than AutoMigrate, which
	// would reproduce a wrong ORM name and hide a production schema mismatch.
	require.NoError(t, db.Exec(`CREATE TABLE tenants (
		id INTEGER PRIMARY KEY,
		youtube_transcript_config TEXT,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec("INSERT INTO tenants (id) VALUES (42), (43)").Error)
	repo := NewTenantRepository(db)
	config := &types.YouTubeTranscriptConfig{Enabled: true, KnowledgeBaseID: "youtube-kb", ProxyURL: "http://proxy.example:8080"}
	require.NoError(t, repo.UpdateTenant(t.Context(), &types.Tenant{ID: 42, YouTubeTranscriptConfig: config}))
	loaded, err := repo.GetTenantByID(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, config, loaded.YouTubeTranscriptConfig)
	other, err := repo.GetTenantByID(t.Context(), 43)
	require.NoError(t, err)
	require.Nil(t, other.YouTubeTranscriptConfig)
	// Disabling and clearing the proxy are also persisted inside the JSON value.
	config = &types.YouTubeTranscriptConfig{KnowledgeBaseID: "youtube-kb"}
	require.NoError(t, repo.UpdateTenant(t.Context(), &types.Tenant{ID: 42, YouTubeTranscriptConfig: config}))
	loaded, err = repo.GetTenantByID(t.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, config, loaded.YouTubeTranscriptConfig)
}
