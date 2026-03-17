package db

import "time"

// Project 项目模型
type Project struct {
	ID           int       `db:"id"`
	Name         string    `db:"name"`
	WorkDir      string    `db:"work_dir"`
	AgentType    string    `db:"agent_type"`    // claudecode | codex | gemini
	AgentMode    string    `db:"agent_mode"`    // default | yolo | plan
	Model        *string   `db:"model"`         // 可选：模型版本
	CreatedAt    time.Time `db:"created_at"`
	LastActiveAt *time.Time `db:"last_active_at"`
	Status       string    `db:"status"`        // running | stopped | idle
}

// ProjectProvider Agent 提供商配置
type ProjectProvider struct {
	ID           int     `db:"id"`
	ProjectID    int     `db:"project_id"`
	ProviderName string  `db:"provider_name"`  // anthropic | openai | gemini
	BaseURL      *string `db:"base_url"`       // 可选：中转 URL
}

// UserContext 用户当前项目映射
type UserContext struct {
	FeishuUserID     string    `db:"feishu_user_id"`
	CurrentProjectID *int      `db:"current_project_id"`
	UpdatedAt        time.Time `db:"updated_at"`
}

// AgentProcess Agent 进程管理
type AgentProcess struct {
	ProjectID int       `db:"project_id"`
	PID       int       `db:"pid"`
	StartedAt time.Time `db:"started_at"`
}
