-- 初始化数据库架构

-- 项目表
CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    work_dir TEXT NOT NULL,
    agent_type TEXT NOT NULL CHECK(agent_type IN ('claudecode', 'codex', 'gemini', 'qoder', 'opencode', 'iflow')),
    agent_mode TEXT DEFAULT 'default' CHECK(agent_mode IN ('default', 'yolo', 'plan', 'acceptEdits', 'bypassPermissions', 'dontAsk')),
    model TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME,
    status TEXT DEFAULT 'stopped' CHECK(status IN ('running', 'stopped', 'idle'))
);

-- 索引：按名称查询
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);

-- 索引：按状态查询
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);

-- Agent 提供商配置表
CREATE TABLE IF NOT EXISTS project_providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL,
    provider_name TEXT NOT NULL,
    base_url TEXT,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- 索引：按项目 ID 查询
CREATE INDEX IF NOT EXISTS idx_providers_project ON project_providers(project_id);

-- 用户当前项目映射表
CREATE TABLE IF NOT EXISTS user_context (
    feishu_user_id TEXT PRIMARY KEY,
    current_project_id INTEGER,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (current_project_id) REFERENCES projects(id) ON DELETE SET NULL
);

-- Agent 进程管理表
CREATE TABLE IF NOT EXISTS agent_processes (
    project_id INTEGER PRIMARY KEY,
    pid INTEGER NOT NULL,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);
