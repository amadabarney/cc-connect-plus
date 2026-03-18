# Session Export/Import Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 支持飞书对话与本地 AI 工具之间的会话上下文互换——export 到 JSON 文件，import 回飞书自动注入上下文。

**Architecture:** 新增 SessionStore 持久化所有对话消息；ExportManager 负责序列化/反序列化 JSON；Router 在每次发消息前检查 imported_contexts 并注入。命令接口通过 CommandHandler（`/session`）和 CLI 子命令（`export`/`import`）暴露。

**Tech Stack:** Go 1.22+, `testing` package, SQLite `:memory:`, `encoding/json`, `os`

---

## 背景与关键约束

- **现有迁移系统**：`db/database.go:migrate()` 只执行 `migrations/001_init.sql` 单文件，需升级为按文件名顺序执行所有 `*.sql`。
- **现有 Router**：`project/router.go:Route()` 调用 `instance.Session.Send()` 发消息，`handleAgentEvents()` 处理回复；需在这两处加 hook。
- **现有 CommandHandler**：处理 `/project` 和 `/agent`；需新增 `/session` 分支。
- **导出目录**：默认 `~/.cc-connect/exports/`，文件名 `<project>-<timestamp>.json`。
- **Feishu import**：`/session import <file_path>` 接受服务器本地路径（用户先 scp 或 CLI 导入，再飞书 import）。
- **上下文注入**：Router 每次 `Session.Send` 前检查 `imported_contexts`；注入后删除记录，避免重复。

---

## Task 1：升级 DB 迁移系统，添加新表

**Files:**
- Modify: `db/database.go:51-64`（migrate 函数）
- Create: `db/migrations/002_sessions.sql`
- Modify: `db/models.go`（追加新 struct）
- Modify: `db/database_test.go`（追加测试）

**Step 1: 写失败测试**

在 `db/database_test.go` 末尾追加：

```go
func TestInit_HasSessionTables(t *testing.T) {
    database, err := Init(":memory:")
    if err != nil {
        t.Fatalf("Init error: %v", err)
    }
    defer database.Close()

    tables := []string{"conversations", "imported_contexts", "export_records"}
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
```

**Step 2: 运行，确认失败**

```bash
cd /Users/amada/Documents/feishuAdapter
go test ./db/... -run TestInit_HasSessionTables -v
```
期望：FAIL（表不存在）

**Step 3: 升级 migrate() 为顺序执行所有 SQL 文件**

替换 `db/database.go` 中的 `migrate()` 函数：

```go
func (db *Database) migrate() error {
    entries, err := migrationsFS.ReadDir("migrations")
    if err != nil {
        return fmt.Errorf("failed to read migrations directory: %w", err)
    }

    for _, entry := range entries {
        if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
            continue
        }
        data, err := migrationsFS.ReadFile("migrations/" + entry.Name())
        if err != nil {
            return fmt.Errorf("failed to read migration %s: %w", entry.Name(), err)
        }
        if _, err := db.Exec(string(data)); err != nil {
            return fmt.Errorf("failed to execute migration %s: %w", entry.Name(), err)
        }
    }
    return nil
}
```

同时在 import 块加 `"path/filepath"`（已有则跳过）。

**Step 4: 创建 `db/migrations/002_sessions.sql`**

```sql
-- 对话历史：持久化所有飞书消息
CREATE TABLE IF NOT EXISTS conversations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL,
    role       TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_conversations_project ON conversations(project_id);

-- 待注入的上下文（import 后存入，注入后删除）
CREATE TABLE IF NOT EXISTS imported_contexts (
    project_id    INTEGER PRIMARY KEY,
    messages_json TEXT NOT NULL,
    imported_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- 导出文件记录
CREATE TABLE IF NOT EXISTS export_records (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL,
    file_path  TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);
```

**Step 5: 在 `db/models.go` 追加新 struct**

```go
// ConversationMessage 单条对话消息
type ConversationMessage struct {
    ID        int       `db:"id"`
    ProjectID int       `db:"project_id"`
    Role      string    `db:"role"`    // "user" | "assistant"
    Content   string    `db:"content"`
    CreatedAt time.Time `db:"created_at"`
}

// ImportedContext 待注入的导入上下文
type ImportedContext struct {
    ProjectID    int       `db:"project_id"`
    MessagesJSON string    `db:"messages_json"`
    ImportedAt   time.Time `db:"imported_at"`
}

// ExportRecord 导出文件记录
type ExportRecord struct {
    ID        int       `db:"id"`
    ProjectID int       `db:"project_id"`
    FilePath  string    `db:"file_path"`
    CreatedAt time.Time `db:"created_at"`
}
```

**Step 6: 运行测试，确认通过**

```bash
go test ./db/... -v
```
期望：全部 PASS

**Step 7: 提交**

```bash
git add db/database.go db/migrations/002_sessions.sql db/models.go db/database_test.go
git commit -m "feat: add session export/import tables and upgrade migration system"
```

---

## Task 2：SessionStore（对话历史持久化）

**Files:**
- Create: `project/session_store.go`
- Create: `project/session_store_test.go`

**Step 1: 写失败测试，新建 `project/session_store_test.go`**

```go
package project

import "testing"

func TestSessionStore_RecordAndGet(t *testing.T) {
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    p, _ := mgr.CreateProject("sp", base+"/sp", "claudecode")
    store := NewSessionStore(database)

    store.RecordMessage(p.ID, "user", "你好")
    store.RecordMessage(p.ID, "assistant", "你好，有什么可以帮你？")

    msgs, err := store.GetConversation(p.ID)
    if err != nil {
        t.Fatalf("GetConversation error: %v", err)
    }
    if len(msgs) != 2 {
        t.Fatalf("expected 2 messages, got %d", len(msgs))
    }
    if msgs[0].Role != "user" || msgs[0].Content != "你好" {
        t.Errorf("unexpected message[0]: %+v", msgs[0])
    }
    if msgs[1].Role != "assistant" {
        t.Errorf("unexpected message[1] role: %s", msgs[1].Role)
    }
}

func TestSessionStore_Clear(t *testing.T) {
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    p, _ := mgr.CreateProject("clr", base+"/clr", "claudecode")
    store := NewSessionStore(database)

    store.RecordMessage(p.ID, "user", "hello")
    store.ClearConversation(p.ID)

    msgs, err := store.GetConversation(p.ID)
    if err != nil {
        t.Fatalf("error: %v", err)
    }
    if len(msgs) != 0 {
        t.Errorf("expected 0 messages after clear, got %d", len(msgs))
    }
}
```

**Step 2: 运行，确认失败**

```bash
go test ./project/... -run "TestSessionStore" -v
```
期望：FAIL（SessionStore 未定义）

**Step 3: 实现 `project/session_store.go`**

```go
package project

import (
    "fmt"
    "time"

    "github.com/amadabarney/cc-connect-plus/db"
)

// SessionStore 管理对话历史持久化
type SessionStore struct {
    db *db.Database
}

// NewSessionStore 创建 SessionStore
func NewSessionStore(database *db.Database) *SessionStore {
    return &SessionStore{db: database}
}

// RecordMessage 记录一条消息（非致命，错误仅打印日志）
func (s *SessionStore) RecordMessage(projectID int, role, content string) {
    _, err := s.db.Exec(`
        INSERT INTO conversations (project_id, role, content, created_at)
        VALUES (?, ?, ?, ?)
    `, projectID, role, content, time.Now())
    if err != nil {
        fmt.Printf("Warning: failed to record message: %v\n", err)
    }
}

// GetConversation 获取项目的所有对话历史（按时间升序）
func (s *SessionStore) GetConversation(projectID int) ([]db.ConversationMessage, error) {
    rows, err := s.db.Query(`
        SELECT id, project_id, role, content, created_at
        FROM conversations
        WHERE project_id = ?
        ORDER BY created_at ASC, id ASC
    `, projectID)
    if err != nil {
        return nil, fmt.Errorf("failed to query conversations: %w", err)
    }
    defer rows.Close()

    var msgs []db.ConversationMessage
    for rows.Next() {
        var m db.ConversationMessage
        if err := rows.Scan(&m.ID, &m.ProjectID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
            return nil, fmt.Errorf("failed to scan message: %w", err)
        }
        msgs = append(msgs, m)
    }
    return msgs, nil
}

// ClearConversation 清除项目对话历史
func (s *SessionStore) ClearConversation(projectID int) error {
    _, err := s.db.Exec(`DELETE FROM conversations WHERE project_id = ?`, projectID)
    if err != nil {
        return fmt.Errorf("failed to clear conversation: %w", err)
    }
    return nil
}
```

**Step 4: 运行测试，确认通过**

```bash
go test ./project/... -run "TestSessionStore" -v
```
期望：PASS

**Step 5: 提交**

```bash
git add project/session_store.go project/session_store_test.go
git commit -m "feat: add SessionStore for conversation history persistence"
```

---

## Task 3：ExportManager（Export 到 JSON）

**Files:**
- Create: `project/export_manager.go`
- Create: `project/export_manager_test.go`

**Step 1: 写失败测试，新建 `project/export_manager_test.go`**

```go
package project

import (
    "encoding/json"
    "os"
    "testing"
)

// exportSnapshot 是 Export 写出的 JSON 结构
type exportSnapshot struct {
    Project    string            `json:"project"`
    AgentType  string            `json:"agent_type"`
    ExportedAt string            `json:"exported_at"`
    Messages   []exportMessage   `json:"messages"`
}

type exportMessage struct {
    Role      string `json:"role"`
    Content   string `json:"content"`
    Timestamp string `json:"timestamp"`
}

func TestExportManager_Export(t *testing.T) {
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    p, _ := mgr.CreateProject("ex", base+"/ex", "claudecode")
    store := NewSessionStore(database)
    store.RecordMessage(p.ID, "user", "帮我写测试")
    store.RecordMessage(p.ID, "assistant", "好的，这是测试代码...")

    outPath := base + "/export.json"
    em := NewExportManager(database, mgr, store)
    if err := em.Export(p.ID, outPath); err != nil {
        t.Fatalf("Export error: %v", err)
    }

    data, err := os.ReadFile(outPath)
    if err != nil {
        t.Fatalf("ReadFile error: %v", err)
    }

    var snap exportSnapshot
    if err := json.Unmarshal(data, &snap); err != nil {
        t.Fatalf("Unmarshal error: %v", err)
    }

    if snap.Project != "ex" {
        t.Errorf("Project = %q, want ex", snap.Project)
    }
    if snap.AgentType != "claudecode" {
        t.Errorf("AgentType = %q, want claudecode", snap.AgentType)
    }
    if len(snap.Messages) != 2 {
        t.Errorf("len(Messages) = %d, want 2", len(snap.Messages))
    }
    if snap.Messages[0].Role != "user" || snap.Messages[0].Content != "帮我写测试" {
        t.Errorf("unexpected message[0]: %+v", snap.Messages[0])
    }
}

func TestExportManager_Export_EmptyConversation(t *testing.T) {
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    p, _ := mgr.CreateProject("empty", base+"/empty", "codex")
    store := NewSessionStore(database)
    outPath := base + "/empty.json"
    em := NewExportManager(database, mgr, store)
    if err := em.Export(p.ID, outPath); err != nil {
        t.Fatalf("Export error: %v", err)
    }

    data, _ := os.ReadFile(outPath)
    var snap exportSnapshot
    json.Unmarshal(data, &snap)
    if len(snap.Messages) != 0 {
        t.Errorf("expected 0 messages, got %d", len(snap.Messages))
    }
}
```

**Step 2: 运行，确认失败**

```bash
go test ./project/... -run "TestExportManager_Export" -v
```
期望：FAIL

**Step 3: 实现 `project/export_manager.go`（仅 Export 部分）**

```go
package project

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/amadabarney/cc-connect-plus/db"
)

// ExportMessage JSON 导出格式中的单条消息
type ExportMessage struct {
    Role      string `json:"role"`
    Content   string `json:"content"`
    Timestamp string `json:"timestamp"`
}

// ExportSnapshot 导出文件的完整结构
type ExportSnapshot struct {
    Project    string          `json:"project"`
    AgentType  string          `json:"agent_type"`
    ExportedAt string          `json:"exported_at"`
    Messages   []ExportMessage `json:"messages"`
}

// ExportManager 负责会话导出与导入
type ExportManager struct {
    db           *db.Database
    projectMgr   *Manager
    sessionStore *SessionStore
}

// NewExportManager 创建 ExportManager
func NewExportManager(database *db.Database, mgr *Manager, store *SessionStore) *ExportManager {
    return &ExportManager{db: database, projectMgr: mgr, sessionStore: store}
}

// Export 将项目对话历史导出为 JSON 文件
func (em *ExportManager) Export(projectID int, outPath string) error {
    project, err := em.projectMgr.GetProject(projectID)
    if err != nil {
        return fmt.Errorf("project not found: %w", err)
    }

    msgs, err := em.sessionStore.GetConversation(projectID)
    if err != nil {
        return fmt.Errorf("failed to get conversation: %w", err)
    }

    exportMsgs := make([]ExportMessage, 0, len(msgs))
    for _, m := range msgs {
        exportMsgs = append(exportMsgs, ExportMessage{
            Role:      m.Role,
            Content:   m.Content,
            Timestamp: m.CreatedAt.UTC().Format(time.RFC3339),
        })
    }

    snap := ExportSnapshot{
        Project:    project.Name,
        AgentType:  project.AgentType,
        ExportedAt: time.Now().UTC().Format(time.RFC3339),
        Messages:   exportMsgs,
    }

    data, err := json.MarshalIndent(snap, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal snapshot: %w", err)
    }

    if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
        return fmt.Errorf("failed to create output directory: %w", err)
    }

    if err := os.WriteFile(outPath, data, 0644); err != nil {
        return fmt.Errorf("failed to write export file: %w", err)
    }

    // 记录导出
    em.db.Exec(`
        INSERT INTO export_records (project_id, file_path, created_at)
        VALUES (?, ?, ?)
    `, projectID, outPath, time.Now())

    return nil
}

// DefaultExportPath 生成默认导出路径
func DefaultExportPath(projectName string) string {
    home, _ := os.UserHomeDir()
    ts := time.Now().Format("20060102-150405")
    return filepath.Join(home, ".cc-connect", "exports", projectName+"-"+ts+".json")
}
```

**Step 4: 运行测试，确认通过**

```bash
go test ./project/... -run "TestExportManager_Export" -v
```
期望：PASS

**Step 5: 提交**

```bash
git add project/export_manager.go project/export_manager_test.go
git commit -m "feat: add ExportManager with Export functionality"
```

---

## Task 4：ExportManager（Import 与上下文注入）

**Files:**
- Modify: `project/export_manager.go`（追加 Import、GetImportedContext、ClearImportedContext）
- Modify: `project/export_manager_test.go`（追加测试）

**Step 1: 写失败测试**

在 `project/export_manager_test.go` 追加：

```go
func TestExportManager_ImportAndGet(t *testing.T) {
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    p, _ := mgr.CreateProject("imp", base+"/imp", "claudecode")
    store := NewSessionStore(database)
    em := NewExportManager(database, mgr, store)

    // 先 export 一个文件
    store.RecordMessage(p.ID, "user", "帮我重构")
    store.RecordMessage(p.ID, "assistant", "好的")
    outPath := base + "/imp.json"
    em.Export(p.ID, outPath)

    // 清除历史，模拟新项目 import
    store.ClearConversation(p.ID)

    count, err := em.Import(p.ID, outPath)
    if err != nil {
        t.Fatalf("Import error: %v", err)
    }
    if count != 2 {
        t.Errorf("count = %d, want 2", count)
    }

    ctx, err := em.GetImportedContext(p.ID)
    if err != nil {
        t.Fatalf("GetImportedContext error: %v", err)
    }
    if ctx == "" {
        t.Fatal("expected non-empty context")
    }
    if !containsString(ctx, "帮我重构") {
        t.Errorf("context should contain '帮我重构', got: %s", ctx)
    }
}

func TestExportManager_ClearImportedContext(t *testing.T) {
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    p, _ := mgr.CreateProject("clrimp", base+"/clrimp", "claudecode")
    store := NewSessionStore(database)
    em := NewExportManager(database, mgr, store)

    store.RecordMessage(p.ID, "user", "test")
    outPath := base + "/clrimp.json"
    em.Export(p.ID, outPath)
    em.Import(p.ID, outPath)

    em.ClearImportedContext(p.ID)

    ctx, _ := em.GetImportedContext(p.ID)
    if ctx != "" {
        t.Errorf("expected empty context after clear, got: %s", ctx)
    }
}

func containsString(s, substr string) bool {
    return len(s) >= len(substr) && (s == substr ||
        len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return true
        }
    }
    return false
}
```

**Step 2: 运行，确认失败**

```bash
go test ./project/... -run "TestExportManager_Import|TestExportManager_Clear" -v
```
期望：FAIL

**Step 3: 在 `project/export_manager.go` 追加 Import、GetImportedContext、ClearImportedContext**

```go
// Import 从 JSON 文件导入对话，写入 imported_contexts，返回消息数
func (em *ExportManager) Import(projectID int, filePath string) (int, error) {
    data, err := os.ReadFile(filePath)
    if err != nil {
        return 0, fmt.Errorf("failed to read file: %w", err)
    }

    var snap ExportSnapshot
    if err := json.Unmarshal(data, &snap); err != nil {
        return 0, fmt.Errorf("invalid JSON format: %w", err)
    }

    // 序列化消息数组存入 DB
    msgsJSON, err := json.Marshal(snap.Messages)
    if err != nil {
        return 0, fmt.Errorf("failed to marshal messages: %w", err)
    }

    _, err = em.db.Exec(`
        INSERT INTO imported_contexts (project_id, messages_json, imported_at)
        VALUES (?, ?, ?)
        ON CONFLICT(project_id) DO UPDATE SET
            messages_json = excluded.messages_json,
            imported_at = excluded.imported_at
    `, projectID, string(msgsJSON), time.Now())
    if err != nil {
        return 0, fmt.Errorf("failed to save imported context: %w", err)
    }

    return len(snap.Messages), nil
}

// GetImportedContext 获取已导入的上下文（格式化为注入字符串），无记录返回空字符串
func (em *ExportManager) GetImportedContext(projectID int) (string, error) {
    var msgsJSON string
    err := em.db.QueryRow(`
        SELECT messages_json FROM imported_contexts WHERE project_id = ?
    `, projectID).Scan(&msgsJSON)
    if err != nil {
        // sql.ErrNoRows → 返回空字符串
        return "", nil
    }

    var msgs []ExportMessage
    if err := json.Unmarshal([]byte(msgsJSON), &msgs); err != nil {
        return "", fmt.Errorf("failed to unmarshal messages: %w", err)
    }

    if len(msgs) == 0 {
        return "", nil
    }

    var sb strings.Builder
    sb.WriteString("[系统背景：以下是之前的对话历史]\n")
    for _, m := range msgs {
        role := "用户"
        if m.Role == "assistant" {
            role = "AI"
        }
        sb.WriteString(role + ": " + m.Content + "\n")
    }
    sb.WriteString("[背景结束，请继续]\n\n")
    return sb.String(), nil
}

// ClearImportedContext 清除已导入的上下文
func (em *ExportManager) ClearImportedContext(projectID int) error {
    _, err := em.db.Exec(`DELETE FROM imported_contexts WHERE project_id = ?`, projectID)
    return err
}
```

同时在 `export_manager.go` 的 import 块中加入 `"strings"`。

**Step 4: 运行测试，确认通过**

```bash
go test ./project/... -run "TestExportManager" -v
```
期望：全部 PASS

**Step 5: 提交**

```bash
git add project/export_manager.go project/export_manager_test.go
git commit -m "feat: add Import, GetImportedContext, ClearImportedContext to ExportManager"
```

---

## Task 5：Router 集成（记录消息 + 注入上下文）

**Files:**
- Modify: `project/router.go`
- Modify: `project/router_test.go`（追加测试）

**Step 1: 写失败测试**

在 `project/router_test.go` 追加：

```go
func TestRoute_RecordsUserMessage(t *testing.T) {
    router, mgr, ctxMgr, base := makeRouter(t)
    p, _ := mgr.CreateProject("rec", base+"/rec", "claudecode")
    ctxMgr.SetCurrentProject("u1", p.ID)

    // 注入一个 stub agent 实例到 pool 中
    session := newStubSession()
    router.agentPool.instances[p.ID] = &AgentInstance{
        Agent:      &stubAgent{session: session},
        Session:    session,
        ProjectID:  p.ID,
        LastActive: time.Now(),
    }

    plat := &stubPlatform{}
    router.Route(context.Background(), plat, makeMsg("u1", "你好"))

    // 等待 goroutine 处理 events
    time.Sleep(50 * time.Millisecond)

    msgs, err := router.sessionStore.GetConversation(p.ID)
    if err != nil {
        t.Fatalf("GetConversation error: %v", err)
    }
    if len(msgs) == 0 {
        t.Fatal("expected at least 1 message recorded")
    }
    if msgs[0].Role != "user" || msgs[0].Content != "你好" {
        t.Errorf("unexpected message: %+v", msgs[0])
    }
}

func TestRoute_InjectsImportedContext(t *testing.T) {
    router, mgr, ctxMgr, base := makeRouter(t)
    p, _ := mgr.CreateProject("inj", base+"/inj", "claudecode")
    ctxMgr.SetCurrentProject("u1", p.ID)

    // 模拟已导入的上下文
    router.exportMgr.db.Exec(`
        INSERT INTO imported_contexts (project_id, messages_json, imported_at)
        VALUES (?, ?, datetime('now'))
    `, p.ID, `[{"role":"user","content":"旧消息","timestamp":"2026-01-01T00:00:00Z"}]`)

    session := newStubSession()
    router.agentPool.instances[p.ID] = &AgentInstance{
        Agent:      &stubAgent{session: session},
        Session:    session,
        ProjectID:  p.ID,
        LastActive: time.Now(),
    }

    plat := &stubPlatform{}
    router.Route(context.Background(), plat, makeMsg("u1", "继续"))

    time.Sleep(50 * time.Millisecond)

    // 发送的消息应该包含注入的上下文
    if len(session.sent) == 0 {
        t.Fatal("expected message to be sent")
    }
    if !containsSubstring(session.sent[0], "旧消息") {
        t.Errorf("sent message should contain imported context, got: %s", session.sent[0])
    }

    // 上下文应已清除
    ctx, _ := router.exportMgr.GetImportedContext(p.ID)
    if ctx != "" {
        t.Error("imported context should be cleared after injection")
    }
}
```

**Step 2: 运行，确认失败**

```bash
go test ./project/... -run "TestRoute_Records|TestRoute_Injects" -v
```
期望：FAIL（router 没有 sessionStore / exportMgr 字段）

**Step 3: 修改 `project/router.go`**

更新 Router 结构体和构造函数：

```go
// Router 消息路由器
type Router struct {
    commandHandler *CommandHandler
    contextMgr     *ContextManager
    agentPool      *AgentPool
    sessionStore   *SessionStore
    exportMgr      *ExportManager
}

// NewRouter 创建消息路由器
func NewRouter(
    commandHandler *CommandHandler,
    contextMgr *ContextManager,
    agentPool *AgentPool,
    sessionStore *SessionStore,
    exportMgr *ExportManager,
) *Router {
    return &Router{
        commandHandler: commandHandler,
        contextMgr:     contextMgr,
        agentPool:      agentPool,
        sessionStore:   sessionStore,
        exportMgr:      exportMgr,
    }
}
```

在 `Route()` 方法中，第 4 步发送消息前加上下文注入，并记录用户消息：

```go
// 3. 获取或启动 Agent
instance, err := r.agentPool.GetOrStart(ctx, *projectID)
if err != nil {
    return false, fmt.Errorf("failed to get agent: %w", err)
}

// 4. 记录用户消息
r.sessionStore.RecordMessage(*projectID, "user", msg.Content)

// 5. 检查并注入导入的上下文
content := msg.Content
if importedCtx, err := r.exportMgr.GetImportedContext(*projectID); err == nil && importedCtx != "" {
    content = importedCtx + content
    r.exportMgr.ClearImportedContext(*projectID)
}

// 6. 发送消息到 Agent
err = instance.Session.Send(content, msg.Images, msg.Files)
```

在 `handleAgentEvents()` 的 `EventText` 分支中记录 assistant 回复：

```go
case core.EventText:
    if event.Content != "" {
        r.sessionStore.RecordMessage(/* projectID needed */)
        // ... existing send logic
    }
```

注意：`handleAgentEvents` 需要接收 projectID 参数。修改其签名：

```go
func (r *Router) handleAgentEvents(ctx context.Context, platform core.Platform, msg *core.Message, instance *AgentInstance, projectID int) {
    for event := range instance.Session.Events() {
        switch event.Type {
        case core.EventText:
            if event.Content != "" {
                r.sessionStore.RecordMessage(projectID, "assistant", event.Content)
                slog.Debug("sending agent text to platform", "content_preview", event.Content[:min(len(event.Content), 50)])
                if err := platform.Send(ctx, msg.ReplyCtx, event.Content); err != nil {
                    slog.Error("failed to send agent text", "error", err)
                }
            }
        // ... rest unchanged
        }
    }
}
```

并更新调用处：`go r.handleAgentEvents(ctx, platform, msg, instance, *projectID)`

**Step 4: 修复 `project_integration.go` 中的 NewRouter 调用**

在 `cmd/cc-connect/project_integration.go` 中，`NewRouter` 调用需要传入新参数（Task 7 会统一处理，此步先确保编译通过）。

**Step 5: 运行测试，确认通过**

```bash
go build ./...
go test ./project/... -run "TestRoute" -v
```
期望：全部 PASS

**Step 6: 提交**

```bash
git add project/router.go project/router_test.go
git commit -m "feat: router records messages and injects imported context"
```

---

## Task 6：CommandHandler 新增 /session 命令

**Files:**
- Modify: `project/commands.go`
- Modify: `project/commands_test.go`（追加测试）

**Step 1: 写失败测试**

在 `project/commands_test.go` 中，更新 `makeCommandHandler` 函数，并追加测试：

```go
func makeCommandHandlerFull(t *testing.T) (*CommandHandler, *Manager, *ContextManager, *ExportManager, string) {
    t.Helper()
    database := mustInitDB(t)
    base := workspaceDir(t)
    mgr := NewManagerWithBase(database, base)
    ctxMgr := NewContextManager(database)
    pool := NewAgentPool(database, mgr, 0, time.Hour)
    store := NewSessionStore(database)
    exportDir := base + "/exports"
    em := NewExportManager(database, mgr, store)
    handler := NewCommandHandlerFull(mgr, ctxMgr, pool, store, em, exportDir)
    return handler, mgr, ctxMgr, em, base
}

func TestHandleCommand_SessionExport_NoProject(t *testing.T) {
    handler, _, _, _, _ := makeCommandHandlerFull(t)
    p := &stubPlatform{}
    handled, _ := handler.HandleCommand(context.Background(), makeMsg("u1", "/session export"), p)
    if !handled {
        t.Error("expected handled=true")
    }
    if !strings.Contains(p.lastReply(), "还没有选择项目") {
        t.Errorf("expected no-project message, got: %q", p.lastReply())
    }
}

func TestHandleCommand_SessionExport_Success(t *testing.T) {
    handler, mgr, ctxMgr, em, base := makeCommandHandlerFull(t)
    p, _ := mgr.CreateProject("ex2", base+"/ex2", "claudecode")
    ctxMgr.SetCurrentProject("u1", p.ID)
    em.sessionStore.RecordMessage(p.ID, "user", "test message")

    plat := &stubPlatform{}
    handled, err := handler.HandleCommand(context.Background(), makeMsg("u1", "/session export"), plat)
    if err != nil { t.Fatalf("error: %v", err) }
    if !handled { t.Error("expected handled=true") }
    if !strings.Contains(plat.lastReply(), "✅") {
        t.Errorf("expected success reply, got: %q", plat.lastReply())
    }
}

func TestHandleCommand_SessionImport_FileNotFound(t *testing.T) {
    handler, mgr, ctxMgr, _, base := makeCommandHandlerFull(t)
    p, _ := mgr.CreateProject("im2", base+"/im2", "claudecode")
    ctxMgr.SetCurrentProject("u1", p.ID)

    plat := &stubPlatform{}
    handler.HandleCommand(context.Background(), makeMsg("u1", "/session import /nonexistent/file.json"), plat)
    if !strings.Contains(plat.lastReply(), "❌") {
        t.Errorf("expected error reply, got: %q", plat.lastReply())
    }
}
```

**Step 2: 运行，确认失败**

```bash
go test ./project/... -run "TestHandleCommand_Session" -v
```
期望：FAIL

**Step 3: 更新 CommandHandler**

在 `project/commands.go` 中：

1. 更新结构体字段：
```go
type CommandHandler struct {
    projectMgr *Manager
    contextMgr *ContextManager
    agentPool  *AgentPool
    sessionStore *SessionStore
    exportMgr  *ExportManager
    exportDir  string
}
```

2. 添加 `NewCommandHandlerFull` 构造函数（保留原 `NewCommandHandler` 向后兼容）：
```go
func NewCommandHandlerFull(
    projectMgr *Manager,
    contextMgr *ContextManager,
    agentPool *AgentPool,
    sessionStore *SessionStore,
    exportMgr *ExportManager,
    exportDir string,
) *CommandHandler {
    return &CommandHandler{
        projectMgr:   projectMgr,
        contextMgr:   contextMgr,
        agentPool:    agentPool,
        sessionStore: sessionStore,
        exportMgr:    exportMgr,
        exportDir:    exportDir,
    }
}
```

3. 在 `HandleCommand` 的前置检查中加入 `/session`：
```go
if !strings.HasPrefix(text, "/project") && !strings.HasPrefix(text, "/agent") && !strings.HasPrefix(text, "/session") {
    return false, nil
}
```

4. 在 switch 中加入 `/session` 分支：
```go
case "/session":
    return true, h.handleSessionCommand(ctx, platform, msg, subcommand, args)
```

5. 实现 `handleSessionCommand`：
```go
func (h *CommandHandler) handleSessionCommand(ctx context.Context, platform core.Platform, msg *core.Message, subcommand string, args []string) error {
    projectID, err := h.contextMgr.GetCurrentProject(msg.UserID)
    if err != nil || projectID == nil {
        return platform.Reply(ctx, msg.ReplyCtx, "❌ 还没有选择项目\n\n使用 /project list 查看所有项目")
    }

    switch subcommand {
    case "export":
        return h.sessionExport(ctx, platform, msg, *projectID)
    case "import":
        return h.sessionImport(ctx, platform, msg, *projectID, args)
    case "clear":
        return h.sessionClear(ctx, platform, msg, *projectID)
    default:
        return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 未知的 session 命令: %s\n\n可用: export, import <path>, clear", subcommand))
    }
}

func (h *CommandHandler) sessionExport(ctx context.Context, platform core.Platform, msg *core.Message, projectID int) error {
    project, err := h.projectMgr.GetProject(projectID)
    if err != nil {
        return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 获取项目失败: %v", err))
    }

    outPath := DefaultExportPath(project.Name)
    if h.exportDir != "" {
        ts := time.Now().Format("20060102-150405")
        outPath = filepath.Join(h.exportDir, project.Name+"-"+ts+".json")
    }

    if err := h.exportMgr.Export(projectID, outPath); err != nil {
        return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 导出失败: %v", err))
    }

    msgs, _ := h.sessionStore.GetConversation(projectID)
    return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("✅ 已导出 %d 条消息\n\n文件路径: %s", len(msgs), outPath))
}

func (h *CommandHandler) sessionImport(ctx context.Context, platform core.Platform, msg *core.Message, projectID int, args []string) error {
    if len(args) < 1 {
        return platform.Reply(ctx, msg.ReplyCtx, "❌ 用法: /session import <file_path>")
    }

    count, err := h.exportMgr.Import(projectID, args[0])
    if err != nil {
        return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 导入失败: %v", err))
    }

    return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("✅ 已导入会话上下文（%d 条消息）\n发消息继续对话，或发 /session clear 清除", count))
}

func (h *CommandHandler) sessionClear(ctx context.Context, platform core.Platform, msg *core.Message, projectID int) error {
    h.exportMgr.ClearImportedContext(projectID)
    return platform.Reply(ctx, msg.ReplyCtx, "✅ 已清除导入的上下文")
}
```

同时在 import 中加 `"path/filepath"` 和 `"time"`（如未引入）。

**Step 4: 运行测试，确认通过**

```bash
go test ./project/... -run "TestHandleCommand_Session" -v
```
期望：PASS

**Step 5: 提交**

```bash
git add project/commands.go project/commands_test.go
git commit -m "feat: add /session export|import|clear commands to CommandHandler"
```

---

## Task 7：组装 ExportManager + 修复 project_integration.go

**Files:**
- Modify: `cmd/cc-connect/project_integration.go`

**Step 1: 更新 `SetupProjectManagement`**

在 `ProjectManagement` 结构体中加字段：

```go
type ProjectManagement struct {
    db           *db.Database
    projectMgr   *project.Manager
    contextMgr   *project.ContextManager
    agentPool    *project.AgentPool
    sessionStore *project.SessionStore
    exportMgr    *project.ExportManager
    cmdHandler   *project.CommandHandler
    router       *project.Router
}
```

在 `SetupProjectManagement` 函数的 `cmdHandler` 创建之前插入：

```go
// 创建对话历史存储
sessionStore := project.NewSessionStore(database)

// 创建导出管理器
exportDir := filepath.Join(dataDir, "exports")
exportMgr := project.NewExportManager(database, projectMgr, sessionStore)

// 创建命令处理器
cmdHandler := project.NewCommandHandlerFull(projectMgr, contextMgr, agentPool, sessionStore, exportMgr, exportDir)

// 创建消息路由器
router := project.NewRouter(cmdHandler, contextMgr, agentPool, sessionStore, exportMgr)
```

同时更新 `ProjectManagement` 的初始化字面量加入新字段。

**Step 2: 编译确认**

```bash
go build ./...
```
期望：无错误

**Step 3: 提交**

```bash
git add cmd/cc-connect/project_integration.go
git commit -m "feat: wire up SessionStore and ExportManager in project integration"
```

---

## Task 8：CLI 子命令 export / import

**Files:**
- Create: `cmd/cc-connect/session_cmd.go`
- Modify: `cmd/cc-connect/main.go`（在 switch 中加 export/import）

**Step 1: 创建 `cmd/cc-connect/session_cmd.go`**

```go
package main

import (
    "flag"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/amadabarney/cc-connect-plus/db"
    "github.com/amadabarney/cc-connect-plus/project"
)

func runSessionExport(args []string) {
    fs := flag.NewFlagSet("export", flag.ExitOnError)
    projectName := fs.String("project", "", "项目名称 (必填)")
    outPath := fs.String("out", "", "输出文件路径 (默认: ~/.cc-connect/exports/<project>-<timestamp>.json)")
    fs.Parse(args)

    if *projectName == "" {
        fmt.Fprintln(os.Stderr, "错误: 必须指定 --project")
        fs.Usage()
        os.Exit(1)
    }

    pm, database, err := setupCLIProjectManagement()
    if err != nil {
        fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
        os.Exit(1)
    }
    defer database.Close()

    mgr := project.NewManager(database)
    proj, err := mgr.GetProjectByName(*projectName)
    if err != nil {
        fmt.Fprintf(os.Stderr, "项目不存在: %s\n", *projectName)
        os.Exit(1)
    }

    if *outPath == "" {
        *outPath = project.DefaultExportPath(*projectName)
    }

    if err := pm.exportMgr.Export(proj.ID, *outPath); err != nil {
        fmt.Fprintf(os.Stderr, "导出失败: %v\n", err)
        os.Exit(1)
    }

    fmt.Printf("✅ 已导出到: %s\n", *outPath)
}

func runSessionImport(args []string) {
    fs := flag.NewFlagSet("import", flag.ExitOnError)
    projectName := fs.String("project", "", "项目名称 (必填)")
    filePath := fs.String("file", "", "JSON 文件路径 (必填)")
    fs.Parse(args)

    if *projectName == "" || *filePath == "" {
        fmt.Fprintln(os.Stderr, "错误: 必须指定 --project 和 --file")
        fs.Usage()
        os.Exit(1)
    }

    pm, database, err := setupCLIProjectManagement()
    if err != nil {
        fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
        os.Exit(1)
    }
    defer database.Close()

    mgr := project.NewManager(database)
    proj, err := mgr.GetProjectByName(*projectName)
    if err != nil {
        fmt.Fprintf(os.Stderr, "项目不存在: %s\n", *projectName)
        os.Exit(1)
    }

    count, err := pm.exportMgr.Import(proj.ID, *filePath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "导入失败: %v\n", err)
        os.Exit(1)
    }

    fmt.Printf("✅ 已导入 %d 条消息，下次发消息时将自动注入上下文\n", count)
}

// setupCLIProjectManagement 为 CLI 子命令初始化最小化的项目管理
func setupCLIProjectManagement() (*ProjectManagement, *db.Database, error) {
    home, _ := os.UserHomeDir()
    dataDir := filepath.Join(home, ".cc-connect")
    dbPath := filepath.Join(dataDir, "feishu-adapter.db")

    database, err := db.Init(dbPath)
    if err != nil {
        return nil, nil, err
    }

    projectMgr := project.NewManager(database)
    store := project.NewSessionStore(database)
    exportMgr := project.NewExportManager(database, projectMgr, store)
    exportDir := filepath.Join(dataDir, "exports")

    pm := &ProjectManagement{
        db:           database,
        projectMgr:   projectMgr,
        sessionStore: store,
        exportMgr:    exportMgr,
    }
    _ = exportDir
    return pm, database, nil
}
```

**Step 2: 在 `cmd/cc-connect/main.go` 的 switch 中加入 export/import**

在现有 `case "sessions":` 附近加入：

```go
case "export":
    runSessionExport(os.Args[2:])
    return
case "import":
    runSessionImport(os.Args[2:])
    return
```

**Step 3: 编译确认**

```bash
go build ./...
```
期望：无错误

**Step 4: 手动验证**

```bash
# 编译
go build -o /tmp/cc-connect-test ./cmd/cc-connect/...

# 查看帮助
/tmp/cc-connect-test export --help
/tmp/cc-connect-test import --help
```

**Step 5: 提交**

```bash
git add cmd/cc-connect/session_cmd.go cmd/cc-connect/main.go
git commit -m "feat: add CLI export/import subcommands for session management"
```

---

## Task 9：全量测试

```bash
# 全量运行
go test ./db/... ./project/... -v -count=1

# Race detector
go test -race ./db/... ./project/...

# Vet
go vet ./...

# 编译
go build ./...
```

**Step 1: 修复发现的任何问题**

**Step 2: 最终提交**

```bash
git commit -m "test: all session export/import tests passing"
```

---

## 验证检查清单

- [ ] `go build ./...` 无错误
- [ ] `go test ./db/...` 全部 PASS（含 `TestInit_HasSessionTables`）
- [ ] `go test ./project/... -run TestSessionStore` PASS
- [ ] `go test ./project/... -run TestExportManager` PASS
- [ ] `go test ./project/... -run TestRoute_Records|TestRoute_Injects` PASS
- [ ] `go test ./project/... -run TestHandleCommand_Session` PASS
- [ ] `go test -race ./db/... ./project/...` 无 race
- [ ] `go vet ./...` 无警告

## 关键文件清单

| 文件 | 操作 |
|------|------|
| `db/database.go` | 修改：migrate() 改为顺序执行所有 SQL |
| `db/migrations/002_sessions.sql` | 新建：conversations / imported_contexts / export_records 表 |
| `db/models.go` | 修改：追加 3 个新 struct |
| `db/database_test.go` | 修改：追加 TestInit_HasSessionTables |
| `project/session_store.go` | 新建 |
| `project/session_store_test.go` | 新建 |
| `project/export_manager.go` | 新建 |
| `project/export_manager_test.go` | 新建 |
| `project/router.go` | 修改：加 sessionStore/exportMgr 字段，记录消息，注入上下文 |
| `project/router_test.go` | 修改：追加 2 个测试 |
| `project/commands.go` | 修改：加 /session 命令，NewCommandHandlerFull |
| `project/commands_test.go` | 修改：追加 session 命令测试 |
| `cmd/cc-connect/project_integration.go` | 修改：组装新组件 |
| `cmd/cc-connect/session_cmd.go` | 新建：CLI export/import 子命令 |
| `cmd/cc-connect/main.go` | 修改：加 export/import case |
