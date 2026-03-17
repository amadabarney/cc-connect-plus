# feishuAdapter

通过飞书机器人远程控制本地 AI 编程工具（Claude Code、Codex、Gemini CLI），实现随时随地编程工作。

## ✨ 特性

- 🤖 **多 AI 支持**：Claude Code、GitHub Copilot、Gemini CLI
- 📁 **动态项目管理**：在飞书中创建、切换、管理项目
- 💬 **会话保持**：切换项目后 AI 上下文自动保留
- 🔄 **灵活切换**：随时切换不同的 AI 工具
- 🐳 **容器化部署**：Docker 一键部署，轻松迁移
- 🔐 **安全可控**：用户权限管理、路径访问限制

## 🏗️ 架构

基于 [cc-connect](https://github.com/chenhg5/cc-connect) 扩展，添加了动态项目管理能力：

```
┌─────────────────────────────────────────────────┐
│                飞书用户 (手机/电脑)               │
└────────────────┬────────────────────────────────┘
                 │ WebSocket
                 ▼
┌─────────────────────────────────────────────────┐
│           feishuAdapter Gateway                  │
│  ┌───────────────────────────────────────────┐  │
│  │  项目管理层                                │  │
│  │  - SQLite 数据库                          │  │
│  │  - Agent 进程池                           │  │
│  │  - 消息路由                               │  │
│  └───────────────────────────────────────────┘  │
└────────┬──────────────┬─────────────┬───────────┘
         │              │             │
         ▼              ▼             ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ Claude Code  │ │   Codex      │ │ Gemini CLI   │
│  (项目 A)    │ │  (项目 B)    │ │  (项目 C)    │
└──────────────┘ └──────────────┘ └──────────────┘
```

## 🚀 快速开始

### 前置要求

1. **Docker 和 Docker Compose**
2. **飞书企业自建应用**（获取 App ID 和 App Secret）
3. **本地认证的 AI CLI 工具**（至少一个）：
   - Claude Code: `npm install -g @anthropic/claude-code && claude login`
   - GitHub Copilot: `gh auth login`
   - Gemini CLI: `gcloud auth login`

### 部署步骤

```bash
# 1. 克隆仓库
git clone https://github.com/yourusername/feishu-adapter.git
cd feishu-adapter

# 2. 配置环境变量
cp .env.example .env
vim .env  # 填入飞书应用凭证

# 3. 在宿主机完成 AI CLI 工具认证
claude login
gh auth login

# 4. 启动服务
docker-compose up -d

# 5. 查看日志
docker-compose logs -f
```

### 在飞书中使用

```
# 创建第一个项目
/project new backend /workspace/backend

# 开始对话
帮我创建一个 Express.js 的 REST API

# 查看所有项目
/project list

# 切换项目
/project switch frontend

# 切换 AI 工具
/agent switch gemini
```

## 📖 完整文档

- [部署指南](docs/DEPLOYMENT.md) - 详细的部署和配置说明
- [设计文档](docs/plans/2026-03-17-feishu-adapter-design.md) - 完整的系统设计
- [集成指南](docs/INTEGRATION.md) - 如何集成到现有项目

## 🎯 使用场景

- 📱 **移动办公**：在手机上通过飞书远程编程
- 🏠 **远程工作**：离开电脑也能处理紧急代码
- 👥 **团队协作**：团队成员共享 AI 编程助手
- 🔄 **多项目切换**：快速在不同项目间切换

## 🛠️ 项目命令

### 项目管理

```
/project new <name> <path> [agent]  - 创建新项目
/project list                       - 列出所有项目
/project switch <name>              - 切换到指定项目
/project delete <name>              - 删除项目
/project info                       - 显示当前项目信息
```

### Agent 管理

```
/agent switch <type>  - 切换 AI 工具 (claudecode/codex/gemini)
/agent restart        - 重启当前 Agent
/agent status         - 查看 Agent 状态
```

## 🔧 技术栈

- **语言**：Go 1.22+
- **数据库**：SQLite
- **容器**：Docker, Docker Compose
- **AI CLI**：Claude Code, GitHub Copilot, Gemini CLI
- **平台**：飞书开放平台（WebSocket）

## 📊 数据持久化

所有数据存储在 `./data/` 目录：

```
data/
├── feishu-adapter.db     # 项目配置数据库
└── sessions/             # 会话数据（如果使用）
```

备份和迁移时只需复制此目录。

## 🔐 安全性

- ✅ **CLI 工具使用 OAuth 认证**，不存储 API keys
- ✅ **用户白名单**：限制可访问的飞书用户
- ✅ **路径限制**：只允许在 `/workspace` 下创建项目
- ✅ **权限控制**：管理员才能删除项目

## 🐛 故障排查

### Agent 启动失败

```bash
# 检查 CLI 工具认证
docker-compose exec feishu-adapter claude models list

# 重新认证（在宿主机）
claude login
```

### 飞书收不到消息

1. 检查飞书应用配置
2. 确认事件订阅使用 **WebSocket 模式**
3. 查看容器日志：`docker-compose logs -f`

### 数据库错误

```bash
# 查看数据库内容
docker-compose exec feishu-adapter sqlite3 /data/feishu-adapter.db "SELECT * FROM projects;"

# 备份数据库
docker-compose exec feishu-adapter tar czf /data/backup.tar.gz /data/feishu-adapter.db
```

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License

## 🙏 致谢

本项目基于 [cc-connect](https://github.com/chenhg5/cc-connect) 构建，感谢原作者的优秀工作。

---

**注意**：本项目仍在开发中，部分功能可能尚未完全实现。详见 [设计文档](docs/plans/2026-03-17-feishu-adapter-design.md)。
