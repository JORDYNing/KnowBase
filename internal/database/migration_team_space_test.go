package database

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var removedSpaceTables = []string{
	"tenant_disabled_shared_agents", "agent_shares", "kb_shares", "organization_join_requests",
	"organization_tenant_members", "organization_members_pre_plan3", "organization_members", "organizations",
}

func requireTeamSpaceSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range removedSpaceTables {
		require.Falsef(t, sqliteTableExists(t, db, table), "retired relation table %s must be removed", table)
	}
	for _, table := range []string{"tenants", "users", "tenant_members", "knowledge_bases", "custom_agents", "knowledges", "sessions", "messages", "resources", "tenant_join_requests"} {
		require.Truef(t, sqliteTableExists(t, db, table), "normal resource table %s must remain", table)
	}
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, 37, version)
	require.False(t, dirty)
}

func TestSQLiteTeamSpaceMigrationsFreshDatabase(t *testing.T) {
	chdirAndRestore(t, sqliteRepoRoot(t))
	dbPath := filepath.Join(t.TempDir(), "team-fresh.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	requireTeamSpaceSchema(t, db)
	// Re-running deployment migrations remains safe after historical tables are gone.
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	requireTeamSpaceSchema(t, db)
}

func TestSQLiteTeamSpaceUpgradePreservesNormalResources(t *testing.T) {
	root := sqliteRepoRoot(t)
	legacyRoot := copySQLiteMigrationsThrough(t, root, 35)
	chdirAndRestore(t, legacyRoot)
	dbPath := filepath.Join(t.TempDir(), "team-upgrade.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	for _, query := range []string{
		`INSERT INTO tenants (id,name,business) VALUES (10000,'team','test')`,
		`INSERT INTO users (id,username,email,password_hash,tenant_id) VALUES ('user-1','member','member@example.test','unused',10000)`,
		`INSERT INTO tenant_members (user_id,tenant_id,role) VALUES ('user-1',10000,'owner')`,
		`INSERT INTO knowledge_bases (id,name,tenant_id,embedding_model_id,summary_model_id) VALUES ('kb-1','docs',10000,'embed-1','chat-1')`,
		`INSERT INTO custom_agents (id,name,tenant_id) VALUES ('agent-1','assistant',10000)`,
		`INSERT INTO knowledges (id,tenant_id,knowledge_base_id,type,title,source) VALUES ('doc-1',10000,'kb-1','file','document','upload')`,
		`INSERT INTO tenant_api_keys (tenant_id,name,key_hash,capabilities) VALUES (10000,'mixed','mixed-key','["chat","manage_spaces","ingest"]')`,
		`INSERT INTO tenant_api_keys (tenant_id,name,key_hash,capabilities) VALUES (10000,'spaces-only','spaces-only-key','["manage_spaces"]')`,
		`INSERT INTO tenant_api_keys (tenant_id,name,key_hash,capabilities) VALUES (10000,'unchanged','unchanged-key','["chat"]')`,
		`INSERT INTO organizations (id,name,owner_id,owner_tenant_id) VALUES ('org-1','retired','user-1',10000)`,
		`INSERT INTO organization_tenant_members (id,organization_id,tenant_id) VALUES ('otm-1','org-1',10000)`,
		`INSERT INTO kb_shares (id,knowledge_base_id,organization_id,shared_by_user_id,source_tenant_id) VALUES ('kbs-1','kb-1','org-1','user-1',10000)`,
		`INSERT INTO agent_shares (id,agent_id,organization_id,shared_by_user_id,source_tenant_id) VALUES ('as-1','agent-1','org-1','user-1',10000)`,
		`INSERT INTO tenant_disabled_shared_agents (tenant_id,agent_id,source_tenant_id) VALUES (10000,'agent-1',10000)`,
		`INSERT INTO organization_join_requests (id,organization_id,user_id,tenant_id) VALUES ('ojr-1','org-1','user-1',10000)`,
		`CREATE TABLE IF NOT EXISTS organization_members (id TEXT PRIMARY KEY,organization_id TEXT REFERENCES organizations(id))`,
		`CREATE TABLE IF NOT EXISTS organization_members_pre_plan3 (id TEXT PRIMARY KEY,organization_id TEXT REFERENCES organizations(id))`,
	} {
		_, err := db.Exec(query)
		require.NoError(t, err, query)
	}
	require.NoError(t, os.Chdir(root))
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	requireTeamSpaceSchema(t, db)
	for name, want := range map[string][]string{"mixed": {"chat", "ingest"}, "spaces-only": {}, "unchanged": {"chat"}} {
		var raw string
		require.NoError(t, db.QueryRow("SELECT capabilities FROM tenant_api_keys WHERE name=?", name).Scan(&raw))
		var got []string
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		require.Equal(t, want, got, name)
	}

	for _, query := range []string{
		`SELECT COUNT(*) FROM tenants WHERE id=10000`, `SELECT COUNT(*) FROM users WHERE id='user-1'`,
		`SELECT COUNT(*) FROM tenant_members WHERE user_id='user-1' AND tenant_id=10000 AND role='owner'`,
		`SELECT COUNT(*) FROM knowledge_bases WHERE id='kb-1' AND tenant_id=10000`,
		`SELECT COUNT(*) FROM custom_agents WHERE id='agent-1' AND tenant_id=10000`,
		`SELECT COUNT(*) FROM knowledges WHERE id='doc-1' AND knowledge_base_id='kb-1'`,
	} {
		var count int
		require.NoError(t, db.QueryRow(query).Scan(&count))
		require.Equal(t, 1, count, query)
	}
}
