package project

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/amadabarney/cc-connect-plus/db"
)

// ContextManager 用户上下文管理器
type ContextManager struct {
	db *db.Database
}

// NewContextManager 创建上下文管理器
func NewContextManager(database *db.Database) *ContextManager {
	return &ContextManager{db: database}
}

// GetCurrentProject 获取用户当前项目 ID
func (cm *ContextManager) GetCurrentProject(userID string) (*int, error) {
	var projectID *int
	err := cm.db.QueryRow(`
		SELECT current_project_id
		FROM user_context
		WHERE feishu_user_id = ?
	`, userID).Scan(&projectID)

	if err == sql.ErrNoRows {
		// 用户还没有设置当前项目
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get current project: %w", err)
	}

	return projectID, nil
}

// SetCurrentProject 设置用户当前项目
func (cm *ContextManager) SetCurrentProject(userID string, projectID int) error {
	// 使用 UPSERT（INSERT OR REPLACE）
	_, err := cm.db.Exec(`
		INSERT INTO user_context (feishu_user_id, current_project_id, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(feishu_user_id) DO UPDATE SET
			current_project_id = excluded.current_project_id,
			updated_at = excluded.updated_at
	`, userID, projectID, time.Now())

	if err != nil {
		return fmt.Errorf("failed to set current project: %w", err)
	}

	return nil
}

// ClearCurrentProject 清除用户当前项目（项目被删除时使用）
func (cm *ContextManager) ClearCurrentProject(userID string) error {
	_, err := cm.db.Exec(`
		UPDATE user_context
		SET current_project_id = NULL, updated_at = ?
		WHERE feishu_user_id = ?
	`, time.Now(), userID)

	if err != nil {
		return fmt.Errorf("failed to clear current project: %w", err)
	}

	return nil
}

// GetUserContext 获取用户完整上下文
func (cm *ContextManager) GetUserContext(userID string) (*db.UserContext, error) {
	ctx := &db.UserContext{}
	err := cm.db.QueryRow(`
		SELECT feishu_user_id, current_project_id, updated_at
		FROM user_context
		WHERE feishu_user_id = ?
	`, userID).Scan(&ctx.FeishuUserID, &ctx.CurrentProjectID, &ctx.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user context: %w", err)
	}

	return ctx, nil
}
