package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedRelationCleanupHasSameOrderedTablesAcrossDialects(t *testing.T) {
	root := sqliteRepoRoot(t)
	var expected strings.Builder
	for _, table := range removedSpaceTables {
		expected.WriteString("DROP TABLE IF EXISTS " + table + ";\n")
	}
	for _, file := range []string{"migrations/versioned/000117_remove_shared_spaces.up.sql", "migrations/sqlite/000037_remove_shared_spaces.up.sql"} {
		content, err := os.ReadFile(filepath.Join(root, file))
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(string(content), expected.String()), file)
		require.Contains(t, string(content), "UPDATE tenant_api_keys", file)
		require.Contains(t, string(content), "manage_spaces", file)
		require.NotContains(t, strings.ToUpper(string(content)), "CASCADE", file)
	}
}
