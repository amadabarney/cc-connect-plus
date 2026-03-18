package db

import "testing"

func TestInit_InMemory(t *testing.T) {
	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("Init(:memory:) error: %v", err)
	}
	defer database.Close()

	tables := []string{"projects", "project_providers", "user_context", "agent_processes"}
	for _, table := range tables {
		var name string
		err := database.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestInit_FileIdempotent(t *testing.T) {
	path := t.TempDir() + "/test.db"
	db1, err := Init(path)
	if err != nil {
		t.Fatalf("first Init error: %v", err)
	}
	db1.Close()

	db2, err := Init(path)
	if err != nil {
		t.Fatalf("second Init error: %v", err)
	}
	defer db2.Close()
}

func TestDatabase_Close(t *testing.T) {
	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}
