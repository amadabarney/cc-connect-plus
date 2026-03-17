package project

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amada/feishu-adapter/db"
)

// Manager 项目管理器
type Manager struct {
	db *db.Database
}

// NewManager 创建项目管理器
func NewManager(database *db.Database) *Manager {
	return &Manager{db: database}
}

// CreateProject 创建新项目
func (m *Manager) CreateProject(name, workDir, agentType string) (*db.Project, error) {
	// 验证参数
	if name == "" || workDir == "" || agentType == "" {
		return nil, fmt.Errorf("name, workDir and agentType are required")
	}

	// 验证 agent type
	validTypes := map[string]bool{
		"claudecode": true,
		"codex":      true,
		"gemini":     true,
		"qoder":      true,
		"opencode":   true,
		"iflow":      true,
	}
	if !validTypes[agentType] {
		return nil, fmt.Errorf("invalid agent type: %s", agentType)
	}

	// 验证目录路径（安全检查）
	absPath, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("invalid work directory path: %w", err)
	}

	// 检查目录是否在允许的工作空间内
	// TODO: 从配置中读取允许的基础路径
	allowedBase := "/workspace"
	if !strings.HasPrefix(absPath, allowedBase) {
		return nil, fmt.Errorf("work directory must be under %s", allowedBase)
	}

	// 开始事务
	tx, err := m.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 插入项目记录
	result, err := tx.Exec(`
		INSERT INTO projects (name, work_dir, agent_type, agent_mode, status)
		VALUES (?, ?, ?, 'default', 'stopped')
	`, name, absPath, agentType)
	if err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}

	projectID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get project ID: %w", err)
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 返回创建的项目
	return m.GetProject(int(projectID))
}

// GetProject 获取项目
func (m *Manager) GetProject(id int) (*db.Project, error) {
	project := &db.Project{}
	err := m.db.QueryRow(`
		SELECT id, name, work_dir, agent_type, agent_mode, model,
		       created_at, last_active_at, status
		FROM projects
		WHERE id = ?
	`, id).Scan(
		&project.ID,
		&project.Name,
		&project.WorkDir,
		&project.AgentType,
		&project.AgentMode,
		&project.Model,
		&project.CreatedAt,
		&project.LastActiveAt,
		&project.Status,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("project not found: %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	return project, nil
}

// GetProjectByName 根据名称获取项目
func (m *Manager) GetProjectByName(name string) (*db.Project, error) {
	project := &db.Project{}
	err := m.db.QueryRow(`
		SELECT id, name, work_dir, agent_type, agent_mode, model,
		       created_at, last_active_at, status
		FROM projects
		WHERE name = ?
	`, name).Scan(
		&project.ID,
		&project.Name,
		&project.WorkDir,
		&project.AgentType,
		&project.AgentMode,
		&project.Model,
		&project.CreatedAt,
		&project.LastActiveAt,
		&project.Status,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("project not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	return project, nil
}

// ListProjects 列出所有项目
func (m *Manager) ListProjects() ([]*db.Project, error) {
	rows, err := m.db.Query(`
		SELECT id, name, work_dir, agent_type, agent_mode, model,
		       created_at, last_active_at, status
		FROM projects
		ORDER BY last_active_at DESC NULLS LAST, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []*db.Project
	for rows.Next() {
		project := &db.Project{}
		err := rows.Scan(
			&project.ID,
			&project.Name,
			&project.WorkDir,
			&project.AgentType,
			&project.AgentMode,
			&project.Model,
			&project.CreatedAt,
			&project.LastActiveAt,
			&project.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// UpdateProjectStatus 更新项目状态
func (m *Manager) UpdateProjectStatus(id int, status string) error {
	_, err := m.db.Exec(`
		UPDATE projects
		SET status = ?, last_active_at = ?
		WHERE id = ?
	`, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update project status: %w", err)
	}
	return nil
}

// UpdateProjectAgentType 更新项目的 Agent 类型
func (m *Manager) UpdateProjectAgentType(id int, agentType string) error {
	_, err := m.db.Exec(`
		UPDATE projects
		SET agent_type = ?
		WHERE id = ?
	`, agentType, id)
	if err != nil {
		return fmt.Errorf("failed to update agent type: %w", err)
	}
	return nil
}

// DeleteProject 删除项目
func (m *Manager) DeleteProject(id int) error {
	result, err := m.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("project not found: %d", id)
	}

	return nil
}

// CreateWorkDir 创建项目工作目录
func (m *Manager) CreateWorkDir(workDir string) error {
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	return nil
}

// TouchActivity 更新项目最后活跃时间
func (m *Manager) TouchActivity(id int) error {
	_, err := m.db.Exec(`
		UPDATE projects
		SET last_active_at = ?
		WHERE id = ?
	`, time.Now(), id)
	return err
}
