package project

import (
	"testing"

	"github.com/amada/feishu-adapter/db"
)

func mustInitDB(t *testing.T) *db.Database {
	t.Helper()
	database, err := db.Init(":memory:")
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func workspaceDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}
