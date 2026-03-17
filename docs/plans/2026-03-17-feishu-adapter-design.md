# feishuAdapter 设计文档

**项目名称**: feishuAdapter
**设计日期**: 2026-03-17
**设计目标**: 构建一个网关系统，通过飞书机器人远程控制本地 AI 编程工具（Claude Code、Codex、Gemini CLI）

---

## 一、项目背景与目标

### 1.1 问题陈述

作为开发者，我们希望能够：
- 在离开电脑时，仍能通过手机远程控制本地的 AI 编程工具
- 灵活选择项目目录和 AI 工具（Claude Code / Codex / Gemini）
- 在飞书中与 AI 对话，执行编程任务
- 实现"随时随地工作"的能力

### 1.2 解决方案

基于开源项目 [cc-connect](https://github.com/chenhg5/cc-connect) 进行扩展，构建一个支持动态项目管理的飞书网关：

- **复用 cc-connect 的核心能力**：AI Agent 管理、飞书集成、消息格式转换
- **扩展项目管理功能**：动态创建/切换项目、多 Agent 进程池、会话状态保持
- **支持 Docker 部署**：便于迁移和快速部署

### 1.3 核心需求

- ✅ 支持从飞书创建新项目目录
- ✅ 支持动态切换项目和 AI 工具
- ✅ 切换项目时保持 AI 对话上下文
- ✅ Docker 容器化部署
- ❌ 不需要多人协作功能

---

## 二、整体架构设计

### 2.1 系统架构

```
┌─────────────────────────────────────────────────┐
│                    飞书用户                      │
│            (手机/电脑访问飞书)                   │
└────────────────┬────────────────────────────────┘
                 │ WebSocket
                 ▼
┌─────────────────────────────────────────────────┐
│           feishuAdapter Gateway                  │
│  ┌───────────────────────────────────────────┐  │
│  │  项目管理层 (新增)                         │  │
│  │  - 动态项目配置 (SQLite)                  │  │
│  │  - 用户上下文映射                         │  │
│  │  - Agent 进程池管理                       │  │
│  └───────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────┐  │
│  │  cc-connect 核心层 (复用)                 │  │
│  │  - 消息路由和分发                         │  │
│  │  - 飞书 WebSocket 连接                    │  │
│  │  - 命令解析和权限控制                     │  │
│  └───────────────────────────────────────────┘  │
└────────┬──────────────┬─────────────┬───────────┘
         │              │             │
         ▼              ▼             ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ Claude Code  │ │   Codex      │ │ Gemini CLI   │
│  进程 #1     │ │  进程 #2     │ │  进程 #3     │
│ (项目 A)     │ │ (项目 B)     │ │ (项目 C)     │
└──────┬───────┘ └──────┬───────┘ └──────┬───────┘
       │                │                │
       ▼                ▼                ▼
┌──────────────────────────────────────────────┐
│         本地文件系统 (项目目录)               │
│  /workspace/backend-api                      │
│  /workspace/frontend-app                     │
│  /workspace/mobile-app                       │
└──────────────────────────────────────────────┘
```

### 2.2 技术架构（基于 cc-connect）

```
feishu-adapter/  (Fork from cc-connect)
├── agent/                    # ✅ 复用：AI Agent 实现
├── platform/                 # 🔧 修改：飞书集成 + 项目命令
├── core/                     # 🔧 修改：路由支持用户上下文
├── project/                  # 🆕 新增：项目管理模块
│   ├── manager.go           # 项目 CRUD
│   ├── database.go          # SQLite 数据访问
│   ├── agent_pool.go        # 多 Agent 进程池
│   └── context.go           # 用户-项目映射
├── db/                       # 🆕 新增：数据库层
│   ├── migrations/          # SQL 迁移脚本
│   └── models.go            # 数据模型
├── cmd/cc-connect/
│   └── main.go              # 🔧 修改：数据库初始化
├── Dockerfile                # 🆕 新增：容器化
└── docker-compose.yml        # 🆕 新增：一键部署
```

### 2.3 关键技术决策

**多进程策略**：
- 每个项目维护独立的 Agent 进程（Claude Code / Codex / Gemini CLI）
- 切换项目 = 切换消息路由目标，而非杀死进程
- 保持进程运行以保留 AI 对话上下文
- 闲置项目（超过 2 小时无操作）自动休眠
- 资源控制：最多同时保持 5 个活跃 Agent（可配置）

**状态保持机制**：
- Claude Code 自己维护 conversation context（存在 `~/.claude/`）
- 我们的系统只需确保：
  1. Agent 进程不被意外杀死
  2. 消息路由到正确的进程
  3. 进程的 working directory 正确

---

## 三、数据模型设计

### 3.1 数据库表结构（SQLite）

```sql
-- 项目表
CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,           -- 项目名称
    work_dir TEXT NOT NULL,              -- 工作目录绝对路径
    agent_type TEXT NOT NULL,            -- claudecode | codex | gemini
    agent_mode TEXT DEFAULT 'default',   -- default | yolo | plan
    model TEXT,                          -- 可选：模型版本
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME,             -- 最后活跃时间
    status TEXT DEFAULT 'stopped'        -- running | stopped | idle
);

-- Agent 提供商配置（支持多个 API 端点）
CREATE TABLE project_providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL,
    provider_name TEXT NOT NULL,         -- anthropic | openai | gemini
    base_url TEXT,                       -- 可选：中转 URL
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- 用户当前项目映射
CREATE TABLE user_context (
    feishu_user_id TEXT PRIMARY KEY,     -- 飞书用户 ID
    current_project_id INTEGER,          -- 当前活跃的项目
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (current_project_id) REFERENCES projects(id)
);

-- Agent 进程管理
CREATE TABLE agent_processes (
    project_id INTEGER PRIMARY KEY,
    pid INTEGER NOT NULL,                -- 进程 ID
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);
```

### 3.2 设计说明

- `projects` 表存储所有项目配置，支持动态增删
- `project_providers` 支持配置多个 API 端点（主备切换）
- `user_context` 记住每个用户当前在哪个项目
- `agent_processes` 跟踪运行中的进程，用于健康检查和资源回收

---

## 四、核心功能流程

### 4.1 创建新项目

```
用户发送：/project new backend /workspace/backend

1. 解析命令参数（项目名、路径）
2. 验证路径：
   - 如果路径不存在 → 询问"目录不存在，是否创建？"
   - 用户确认 → mkdir -p 创建目录
3. 弹出飞书交互卡片：选择 AI 工具
   ┌─────────────────────────┐
   │ 选择 AI 编程助手         │
   ├─────────────────────────┤
   │ ○ Claude Code (推荐)    │
   │ ○ GitHub Codex          │
   │ ○ Google Gemini CLI     │
   │                         │
   │ [确定] [取消]           │
   └─────────────────────────┘
4. 用户选择后 → 插入 projects 表
5. 启动对应的 Agent 进程：
   - 设置 working directory = work_dir
   - 记录 PID 到 agent_processes 表
6. 设置 user_context.current_project_id = 新项目 ID
7. 回复：✅ 项目 backend 已创建并激活
```

### 4.2 切换项目

```
用户发送：/project switch backend-api

1. 查询 projects 表，找到名为 backend-api 的项目
2. 检查该项目的 Agent 进程状态：
   - 如果进程正在运行 → 直接切换路由
   - 如果进程已停止 → 重新启动 Agent 进程
3. 更新 user_context：current_project_id = backend-api 的 ID
4. 回复：✅ 已切换到项目 backend-api (Claude Code)
   工作目录：/workspace/backend-api
5. 后续消息自动路由到该项目的 Agent 进程
```

### 4.3 动态切换 Agent

```
用户在项目中发送：/agent switch gemini

1. 获取当前项目 ID（从 user_context）
2. 停止当前 Agent 进程（优雅关闭）
3. 更新 projects 表：agent_type = 'gemini'
4. 启动新的 Gemini CLI 进程（work_dir 不变）
5. 更新 agent_processes 表（新 PID）
6. 回复：✅ 已切换到 Gemini CLI
   ⚠️ 注意：上下文已重置，这是一个新的对话
```

---

## 五、飞书交互设计

### 5.1 命令系统

**项目管理命令：**
```
/project new <name> <path>     - 创建新项目
/project list                  - 列出所有项目
/project switch <name>         - 切换到指定项目
/project delete <name>         - 删除项目（需确认）
/project info                  - 显示当前项目信息
```

**Agent 管理命令：**
```
/agent switch <type>           - 切换 AI 工具
/agent restart                 - 重启当前 Agent 进程
/agent status                  - 查看 Agent 状态
```

**会话管理（复用 cc-connect）：**
```
/new                          - 在当前项目下新建会话
/list                         - 列出当前项目的所有会话
/switch <session>             - 切换会话
/mode yolo                    - 自动批准所有操作
/mode default                 - 每次操作需确认
```

### 5.2 交互式卡片

**项目列表卡片：**
```
┌────────────────────────────────────┐
│ 📁 您的项目列表                     │
├────────────────────────────────────┤
│ ✅ backend-api (Claude Code)       │
│    /workspace/backend              │
│    最后活跃：2分钟前                │
│    [切换] [详情]                    │
├────────────────────────────────────┤
│ frontend-app (Gemini CLI)          │
│    /workspace/frontend             │
│    最后活跃：1小时前                │
│    [切换] [详情]                    │
├────────────────────────────────────┤
│                                    │
│ [+ 创建新项目]                     │
└────────────────────────────────────┘
```

**项目详情卡片：**
```
┌────────────────────────────────────┐
│ 📊 项目：backend-api               │
├────────────────────────────────────┤
│ 目录：/workspace/backend           │
│ AI 工具：Claude Code               │
│ 模型：claude-sonnet-4-20250514     │
│ 状态：运行中 (PID: 12345)          │
│ 创建时间：2026-03-17 10:00         │
│                                    │
│ [切换 Agent] [重启] [删除项目]     │
└────────────────────────────────────┘
```

---

## 六、技术实现细节

### 6.1 关键模块扩展点

**1. 修改 `cmd/cc-connect/main.go`**
```go
func main() {
    // 原有逻辑：加载 TOML 配置
    cfg := config.Load()

    // 🆕 新增：初始化数据库
    db := database.Init("~/.feishu-adapter/data.db")

    // 🆕 新增：创建项目管理器
    projectMgr := project.NewManager(db)

    // 🆕 新增：创建 Agent 进程池
    agentPool := project.NewAgentPool(cfg, projectMgr)

    // 原有逻辑：启动平台连接器
    // 🔧 修改：传入 projectMgr 和 agentPool
    platform.Start(cfg, projectMgr, agentPool)
}
```

**2. 扩展 `platform/feishu/handler.go`**
```go
type Handler struct {
    // 原有字段
    client *Client

    // 🆕 新增字段
    projectMgr *project.Manager
    agentPool  *project.AgentPool
}

func (h *Handler) HandleMessage(msg *Message) error {
    // 🆕 新增：处理项目管理命令
    if strings.HasPrefix(msg.Text, "/project") {
        return h.handleProjectCommand(msg)
    }

    // 🆕 新增：根据用户上下文路由
    projectID := h.projectMgr.GetCurrentProject(msg.UserID)
    agent := h.agentPool.Get(projectID)

    // 原有逻辑：发送消息到 Agent
    return agent.SendMessage(msg.Text)
}
```

**3. 新增 `project/manager.go`**
```go
type Manager struct {
    db *sql.DB
}

func (m *Manager) CreateProject(name, workDir, agentType string) (*Project, error)
func (m *Manager) SwitchProject(userID string, projectName string) error
func (m *Manager) GetCurrentProject(userID string) (int, error)
func (m *Manager) ListProjects(userID string) ([]*Project, error)
```

**4. 新增 `project/agent_pool.go`**
```go
type AgentPool struct {
    agents map[int]*AgentProcess  // projectID -> Agent进程
    mu     sync.RWMutex
    maxAgents int
}

func (p *AgentPool) GetOrStart(projectID int, config ProjectConfig) (*AgentProcess, error)
func (p *AgentPool) AutoCleanup(idleTimeout time.Duration)
```

### 6.2 代码复用比例

- ✅ **85% 复用** cc-connect 的代码（agent/、platform/ 大部分逻辑）
- 🔧 **10% 修改** 现有代码（handler、router、main）
- 🆕 **5% 新增** 项目管理模块（project/、db/）

---

## 七、安全性和错误处理

### 7.1 安全性设计

**1. CLI 工具认证管理**

Claude Code、Codex、Gemini CLI 都使用 OAuth 认证，凭证存储在：
- Claude Code: `~/.claude/`
- GitHub CLI: `~/.config/gh/`
- Gemini: `~/.config/gcloud/`

**部署前准备：**
```bash
# 在宿主机完成所有 CLI 工具的登录
claude login
gh auth login
gcloud auth login
```

**Docker 挂载认证目录：**
```yaml
volumes:
  - ~/.claude:/root/.claude:ro
  - ~/.config/gh:/root/.config/gh:ro
  - ~/.config/gcloud:/root/.config/gcloud:ro
```

**2. 飞书机器人凭证管理**
```go
// 从环境变量加载，避免硬编码
func loadFeishuConfig() *FeishuConfig {
    return &FeishuConfig{
        AppID:     os.Getenv("FEISHU_APP_ID"),
        AppSecret: os.Getenv("FEISHU_APP_SECRET"),
    }
}
```

**3. 用户权限控制**
```go
type ACL struct {
    AllowedUsers []string  // 允许使用的飞书用户ID
    AdminUsers   []string  // 管理员
}

func (h *Handler) checkPermission(userID string, action string) error {
    if action == "project.delete" && !isAdmin(userID) {
        return errors.New("需要管理员权限")
    }
    if !isAllowedUser(userID) {
        return errors.New("无权访问此服务")
    }
    return nil
}
```

**4. 目录访问限制**
```go
// 防止路径遍历攻击
func validateWorkDir(path string) error {
    allowedBase := "/workspace"
    absPath, _ := filepath.Abs(path)

    if !strings.HasPrefix(absPath, allowedBase) {
        return errors.New("不允许在此路径创建项目")
    }
    return nil
}
```

### 7.2 错误处理策略

**1. Agent 进程异常自动重启**
```go
func (p *AgentPool) monitorProcess(projectID int) {
    for {
        if !p.isProcessAlive(projectID) {
            log.Warn("Agent 进程已停止，尝试重启", projectID)
            if err := p.restartAgent(projectID); err != nil {
                p.notifyUser(projectID, "⚠️ AI 工具异常，请稍后重试")
            }
        }
        time.Sleep(30 * time.Second)
    }
}
```

**2. 优雅降级**
```go
func (h *Handler) HandleMessage(msg *Message) error {
    agent, err := h.agentPool.Get(projectID)
    if err != nil {
        return h.sendCard(msg.UserID, &Card{
            Title: "⚠️ 项目服务暂时不可用",
            Actions: []Action{
                {Label: "重启", Command: "/agent restart"},
                {Label: "查看其他项目", Command: "/project list"},
            },
        })
    }
    return agent.SendMessage(msg.Text)
}
```

**3. 数据库事务回滚**
```go
func (m *Manager) CreateProject(name, workDir, agentType string) error {
    tx, _ := m.db.Begin()
    defer tx.Rollback()

    if err := tx.Exec("INSERT INTO projects ..."); err != nil {
        return fmt.Errorf("创建项目失败: %w", err)
    }

    if err := os.MkdirAll(workDir, 0755); err != nil {
        return fmt.Errorf("创建目录失败: %w", err)
    }

    return tx.Commit()
}
```

---

## 八、Docker 容器化部署

### 8.1 Dockerfile

```dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY . .
RUN go build -o feishu-adapter ./cmd/cc-connect

FROM node:20-slim

# 安装 Claude Code CLI
RUN npm install -g @anthropic/claude-code

# 安装 GitHub Copilot CLI
RUN npm install -g github-copilot-cli

# 可选：安装 Gemini CLI
# RUN npm install -g @google/gemini-cli

# 复制二进制文件
COPY --from=builder /build/feishu-adapter /app/feishu-adapter

# 挂载点
VOLUME ["/workspace", "/data", "/root/.claude"]

WORKDIR /app
ENTRYPOINT ["/app/feishu-adapter"]
```

### 8.2 docker-compose.yml

```yaml
version: '3.8'

services:
  feishu-adapter:
    build: .
    container_name: feishu-adapter
    restart: unless-stopped

    volumes:
      # 项目工作空间
      - ~/projects:/workspace

      # 持久化数据
      - ./data:/data

      # CLI 工具认证凭证（只读）
      - ~/.claude:/root/.claude:ro
      - ~/.config/gh:/root/.config/gh:ro
      - ~/.config/gcloud:/root/.config/gcloud:ro

    environment:
      - FEISHU_APP_ID=${FEISHU_APP_ID}
      - FEISHU_APP_SECRET=${FEISHU_APP_SECRET}
      - ALLOWED_USERS=${ALLOWED_USERS}
      - ADMIN_USERS=${ADMIN_USERS}
      - MAX_AGENTS=${MAX_AGENTS:-5}
      - IDLE_TIMEOUT=${IDLE_TIMEOUT:-7200}
      - TZ=Asia/Shanghai

    healthcheck:
      test: ["CMD", "/app/healthcheck.sh"]
      interval: 30s
      timeout: 10s
      retries: 3

    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

### 8.3 快速部署流程

```bash
# 1. 准备工作（宿主机）
npm install -g @anthropic/claude-code
claude login
gh auth login

# 2. 克隆仓库
git clone https://github.com/yourusername/feishu-adapter.git
cd feishu-adapter

# 3. 配置环境变量
cp .env.example .env
vim .env  # 填入飞书应用凭证

# 4. 一键启动
docker-compose up -d

# 5. 查看日志
docker-compose logs -f
```

### 8.4 迁移到新机器

```bash
# 在新机器上：
# 1. 完成 CLI 工具认证
claude login && gh auth login

# 2. 拷贝数据
scp -r old-machine:~/feishu-adapter/data ./data

# 3. 启动服务
docker-compose up -d
```

---

## 九、部署和使用指南

### 9.1 飞书机器人配置

1. 访问 [飞书开放平台](https://open.feishu.cn)
2. 创建企业自建应用
3. 配置权限：
   - 机器人（im:message）
   - 接收消息（im:message:receive_v1）
4. 启用事件订阅（WebSocket 模式，无需公网 IP）
5. 获取 App ID 和 App Secret

### 9.2 使用流程

```
📱 飞书操作：

1. 创建第一个项目：
   /project new backend /workspace/backend

2. 开始对话：
   "帮我创建一个 Express.js 的 REST API 项目"

3. 查看所有项目：
   /project list

4. 切换项目：
   /project switch frontend

5. 临时切换 AI 工具：
   /agent switch gemini
```

### 9.3 常见问题

**Q: Agent 进程启动失败**
```bash
# 检查 CLI 工具认证
docker-compose exec feishu-adapter claude models list
```

**Q: 飞书收不到消息**
- 检查飞书应用的事件订阅配置
- 确认使用 WebSocket 模式

**Q: 备份数据**
```bash
docker-compose exec feishu-adapter \
  tar czf /data/backup.tar.gz /data/data.db
```

---

## 十、开发计划

### 10.1 开发阶段

**Phase 1: Fork 和基础扩展（1-2 天）**
- Fork cc-connect 仓库
- 添加数据库模块和迁移脚本
- 实现项目管理基础 CRUD

**Phase 2: 核心功能实现（3-4 天）**
- 实现 Agent 进程池管理
- 修改飞书 Handler 支持项目命令
- 实现用户上下文路由

**Phase 3: Docker 和部署（1-2 天）**
- 编写 Dockerfile 和 docker-compose.yml
- 测试容器化部署
- 编写部署文档

**Phase 4: 测试和优化（2-3 天）**
- 功能测试（创建、切换、删除项目）
- 压力测试（多项目并发）
- 错误处理和边界情况

### 10.2 成功标准

- ✅ 能在飞书中创建新项目
- ✅ 能在飞书中切换项目和 AI 工具
- ✅ 切换项目后 AI 上下文保持
- ✅ Docker 一键部署成功
- ✅ 数据能在机器间迁移

---

## 十一、参考资料

- [cc-connect GitHub](https://github.com/chenhg5/cc-connect)
- [cc-connect 配置示例](https://github.com/chenhg5/cc-connect/blob/main/config.example.toml)
- [飞书开放平台文档](https://open.feishu.cn/document/)
- [Claude Code 文档](https://docs.anthropic.com/claude-code)
- [LiteLLM AI Gateway](https://www.litellm.ai/)

---

**文档版本**: 1.0
**最后更新**: 2026-03-17
