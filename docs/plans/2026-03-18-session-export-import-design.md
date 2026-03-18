# Session Export/Import 设计

**日期**: 2026-03-18
**项目**: cc-connect-plus
**功能**: 飞书与本地工具之间的会话上下文互换

## 目标

支持在飞书对话和本地 AI 工具（Claude Code / Codex / Gemini）之间任意互换历史上下文，无需依赖各工具的私有存储格式。

## 核心流程

```
飞书对话 ──export──→ 本地文件（session.json）
本地文件 ──手工──────→ 本地终端（用户自行贴入或 --resume）
本地文件 ──import──→ 飞书对话（cc-connect 注入上下文）
```

## 导出格式

JSON，机器友好，工具无关：

```json
{
  "project": "checkDataPairScene",
  "agent_type": "claudecode",
  "exported_at": "2026-03-18T10:00:00Z",
  "messages": [
    {"role": "user", "content": "帮我重构 auth.go 的错误处理", "timestamp": "2026-03-18T09:00:00Z"},
    {"role": "assistant", "content": "已完成，主要改动是...", "timestamp": "2026-03-18T09:01:00Z"}
  ]
}
```

## 架构

```
┌─────────────────────────────────────────────────────┐
│                  cc-connect-plus                     │
│                                                      │
│  SessionStore（SQLite）                              │
│  └── conversations 表：持久化所有对话历史            │
│                                                      │
│  ExportManager                                       │
│  ├── Export(projectID) → session.json               │
│  └── Import(projectID, file) → 写入 imported_context│
│                                                      │
│  AgentPool.GetOrStart                                │
│  └── 检查 imported_context → 注入为首条消息          │
└──────────────────────────────────────────────────────┘
```

## 数据库表

```sql
-- 对话历史（飞书消息持久化）
CREATE TABLE conversations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL,
    role       TEXT NOT NULL,  -- 'user' | 'assistant'
    content    TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

-- 导入的上下文（等待注入，用完即删）
CREATE TABLE imported_contexts (
    project_id    INTEGER PRIMARY KEY,
    messages_json TEXT NOT NULL,  -- JSON array
    imported_at   DATETIME NOT NULL
);

-- 导出文件记录（供飞书回复路径）
CREATE TABLE export_records (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL,
    file_path  TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
```

## 命令接口

| 方式 | 命令 | 说明 |
|------|------|------|
| 飞书 | `/session export` | 导出当前项目对话，回复文件路径 |
| 飞书 | `/session import`（附件）| 上传 JSON 文件，确认后注入 |
| CLI | `cc-connect export --project <name> --out <path>` | 导出到指定文件 |
| CLI | `cc-connect import --project <name> --file <path>` | 导入并等待下次对话注入 |

## Import 注入机制

用户导入后发第一条消息，AgentPool 检测到 `imported_contexts` 存在，构造注入提示注入至 Agent：

```
[系统背景：以下是之前的对话历史]
用户: ...
AI: ...
[背景结束，请继续]
```

注入完成后删除 `imported_contexts` 记录，避免重复注入。

飞书回复确认：
```
✅ 已导入会话上下文（32 条消息）
发消息继续对话，或发 /context clear 清除
```

## 不在范围内

- 自动检测本地 Claude Code 会话目录（SessionWatcher）
- Codex / Gemini 原生会话格式的读写
- 会话自动同步（需用户手动 export/import）
