# cc-connect-plus

> 基于 [cc-connect](https://github.com/chenhg5/cc-connect) 扩展，通过飞书机器人远程控制本地 AI 编程工具，实现随时随地编程工作。

## 什么是 cc-connect-plus？

[cc-connect](https://github.com/chenhg5/cc-connect) 是一个将 Claude Code 等 AI 编程工具接入即时通讯平台（飞书、Telegram、Discord 等）的开源网关。你可以在手机飞书上发一条消息，让服务器上的 Claude Code 帮你写代码、修 Bug、查日志。

**cc-connect-plus** 在此基础上增加了**动态项目管理**能力：无需重启服务，在飞书中就能随时创建项目、切换项目、更换 AI 工具。适合需要同时维护多个代码库的开发者。

## 使用场景

| 场景 | 说明 |
|------|------|
| 📱 移动编程 | 开会途中用手机飞书发指令，让服务器上的 Claude Code 自动处理代码 |
| 🏠 远程工作 | 不带电脑也能通过飞书远程操控开发机上的 AI 编程助手 |
| 🔄 多项目切换 | 一个 Bot 管理前端、后端、基础设施等多个项目，随时切换 |
| 👥 团队协作 | 团队成员共享一个 AI 编程助手，各自操作自己的项目 |

## 架构

```
┌──────────────────────────────┐
│   飞书 App（手机 / 电脑）     │
└────────────┬─────────────────┘
             │ WebSocket 长连接
             ▼
┌──────────────────────────────────────────────┐
│              cc-connect-plus                  │
│                                              │
│  消息路由层                                   │
│  ├── /project、/agent 命令 → 项目管理模块     │
│  └── 普通消息 → Agent 进程池                  │
│                                              │
│  项目管理模块（本项目新增）                    │
│  ├── SQLite 持久化项目配置                    │
│  ├── Agent 进程池（最多 5 个并发）             │
│  └── 用户会话上下文                           │
│                                              │
│  cc-connect Engine（原有）                    │
│  ├── 飞书 / Telegram / Discord 平台接入       │
│  ├── Claude Code / Codex / Gemini 支持        │
│  └── 流式回复、权限控制、速率限制等            │
└──────┬───────────┬──────────┬───────────────┘
       ▼           ▼          ▼
  Claude Code    Codex    Gemini CLI
  （项目 A）   （项目 B） （项目 C）
```

## 快速开始

### 前置要求

- **Go 1.22+**
- **飞书企业自建应用**（需要 App ID 和 App Secret）
- **Claude Code**：`npm install -g @anthropic/claude-code && claude login`

### 安装

```bash
git clone https://github.com/amadabarney/cc-connect-plus.git
cd cc-connect-plus

# 编译
go build -o /usr/local/bin/cc-connect ./cmd/cc-connect/...
```

### 配置飞书应用

1. 前往 [飞书开放平台](https://open.feishu.cn) 创建企业自建应用
2. 开启**机器人**能力
3. 在「事件与回调」中添加 `im.message.receive_v1` 事件，选择 **WebSocket 长连接**模式（无需公网 IP）
4. 获取 App ID 和 App Secret

### 创建配置文件

```bash
cp config.example.toml config.toml
```

最小配置示例（`config.toml`）：

```toml
[log]
level = "info"

[[projects]]
name = "my-project"

[projects.agent]
type = "claudecode"

[projects.agent.options]
work_dir = "/path/to/your/project"
mode = "bypassPermissions"   # 全自动模式，无需手动确认每个工具调用

[[projects.platforms]]
type = "feishu"

[projects.platforms.options]
app_id = "cli_xxxxxxxxx"
app_secret = "xxxxxxxxx"
```

更多配置选项见 [config.example.toml](config.example.toml)。

### 启动

```bash
cc-connect -config config.toml
```

看到以下日志说明连接成功：

```
feishu: bot identified  open_id=ou_xxx
engine started          project=my-project agent=claudecode
cc-connect is running   projects=1
connected to wss://msg-frontier.feishu.cn/ws/v2...
```

### macOS 开机自启动

```bash
# 创建 LaunchAgent
cat > ~/Library/LaunchAgents/com.cc-connect.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.cc-connect</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/cc-connect</string>
        <string>-config</string>
        <string>/path/to/config.toml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/path/to/node/bin</string>
    </dict>
    <key>StandardOutPath</key>
    <string>/Users/你的用户名/Library/Logs/cc-connect/cc-connect.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/你的用户名/Library/Logs/cc-connect/cc-connect.error.log</string>
</dict>
</plist>
EOF

mkdir -p ~/Library/Logs/cc-connect
launchctl load ~/Library/LaunchAgents/com.cc-connect.plist
```

## 在飞书中使用

### 直接对话

Bot 启动后，直接在飞书中给它发消息即可开始与 Claude Code 对话：

```
帮我写一个 Go 的 HTTP 中间件，实现请求日志记录
```

```
解释一下这段代码的作用，并找出潜在的 bug
```

### 项目管理命令

当你需要管理多个项目时，使用 `/project` 命令：

```
/project new backend /workspace/backend          # 创建项目并绑定目录
/project new frontend /workspace/frontend codex  # 使用 Codex 作为 AI

/project list      # 查看所有项目
/project switch frontend   # 切换到 frontend 项目
/project info      # 查看当前项目信息
/project delete old-project  # 删除项目
```

### Agent 管理命令

```
/agent switch gemini   # 切换 AI 工具（claudecode / codex / gemini）
/agent restart         # 重启当前 Agent（清空上下文）
/agent status          # 查看 Agent 运行状态
```

### cc-connect 内置命令

本项目继承了 cc-connect 的所有内置命令，常用的有：

```
/mode yolo    # 切换到全自动模式（跳过权限确认）
/mode default # 切换回标准模式（每步确认）
/clear        # 清空当前对话上下文
/help         # 查看所有可用命令
```

## 技术栈

- **语言**：Go 1.22+
- **核心依赖**：[cc-connect](https://github.com/chenhg5/cc-connect)（平台接入 + Agent 管理框架）
- **数据库**：SQLite（`github.com/mattn/go-sqlite3`）
- **AI 工具**：Claude Code、GitHub Copilot (Codex)、Gemini CLI
- **平台**：飞书、Telegram、Discord（cc-connect 支持的所有平台）

## 与原版 cc-connect 的区别

| 功能 | cc-connect（原版） | cc-connect-plus（本项目） |
|------|-------------------|--------------------------|
| 项目配置 | 静态配置文件，需重启生效 | 飞书命令动态创建/切换，无需重启 |
| 多项目管理 | 手动修改配置 | `/project new` / `/project switch` |
| Agent 切换 | 重启服务 | `/agent switch` 实时切换 |
| 项目持久化 | 无 | SQLite 数据库持久化 |
| 消息路由 | 单项目直通 | 按用户上下文路由到对应项目的 Agent |

## 数据存储

项目数据库默认位于 `~/.cc-connect/feishu-adapter.db`，备份此文件即可保存所有项目配置。

## 故障排查

**Agent 启动失败：`'claude' CLI not found in PATH`**

launchd 的 PATH 与 Shell 不同，需要在 plist 的 `EnvironmentVariables` 中显式指定 `claude` 的完整路径：

```bash
which claude  # 找到 claude 的完整路径
# 将路径所在目录添加到 plist 的 PATH 中
```

**飞书收不到消息**

1. 确认飞书应用已开启「机器人」能力
2. 事件订阅选择 **WebSocket 长连接**（非 Webhook）
3. 检查 App ID / App Secret 是否正确

**查看运行日志**

```bash
tail -f ~/Library/Logs/cc-connect/cc-connect.log
```

## 致谢

本项目基于 [cc-connect](https://github.com/chenhg5/cc-connect) 构建，感谢原作者的优秀工作。

## 许可证

MIT License
