package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Database 数据库封装
type Database struct {
	*sql.DB
}

// Init 初始化数据库
func Init(dbPath string) (*Database, error) {
	// 确保目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// 打开数据库
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	database := &Database{db}

	// 执行迁移
	if err := database.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return database, nil
}

// migrate 执行数据库迁移
func (db *Database) migrate() error {
	// 读取迁移文件
	migrationFile, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	// 执行迁移
	if _, err := db.Exec(string(migrationFile)); err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	return nil
}

// Close 关闭数据库连接
func (db *Database) Close() error {
	return db.DB.Close()
}
