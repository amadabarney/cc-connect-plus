# feishuAdapter 部署指南

本文档提供 feishuAdapter 的详细部署说明。

## 目录

- [环境准备](#环境准备)
- [飞书应用配置](#飞书应用配置)
- [本地部署](#本地部署)
- [Docker 部署](#docker-部署)
- [环境变量配置](#环境变量配置)
- [迁移和备份](#迁移和备份)

---

## 环境准备

### 1. 系统要求

- **操作系统**：Linux / macOS / Windows (WSL2)
- **Docker**：20.10+ 和 Docker Compose
- **磁盘空间**：至少 10GB（用于 Docker 镜像和项目文件）

### 2. 安装 AI CLI 工具

在宿主机上安装并认证所需的 AI CLI 工具：

#### Claude Code

```bash
npm install -g @anthropic/claude-code
claude login
```

验证：
```bash
claude models list
```

#### GitHub Copilot

```bash
gh auth login
npm install -g github-copilot-cli
```

验证：
```bash
gh copilot explain "test"
```

#### Gemini CLI（可选）

```bash
gcloud auth login
```

验证：
```bash
gcloud auth list
```

---

## 飞书应用配置

### 1. 创建企业自建应用

1. 访问 [飞书开放平台](https://open.feishu.cn/app)
2. 点击"创建企业自建应用"
3. 填写应用信息（名称、描述、图标等）

### 2. 获取凭证

在应用的"凭证与基础信息"页面获取：
- **App ID**：`cli_xxxxxxxxxx`
- **App Secret**：`xxxxxxxxxxxxxxxxxxxxxx`

### 3. 配置权限

进入"权限管理"，添加以下权限：

| 权限 | 说明 |
|------|------|
| `im:message` | 获取与发送单聊、群组消息 |
| `im:message.group_msg` | 接收群聊中@机器人消息事件 |
| `im:message.p2p_msg` | 接收用户发送的单聊消息事件 |

### 4. 启用事件订阅

1. 进入"事件订阅"
2. 选择 **"使用 WebSocket 模式"**（无需公网 IP）
3. 订阅以下事件：
   - `im.message.receive_v1`

### 5. 配置机器人

1. 进入"机器人"设置
2. 启用"允许发送消息"
3. 记录机器人名称（用于@机器人）

### 6. 发布应用

1. 进入"版本管理与发布"
2. 创建版本并发布
3. 等待审核通过（企业内部应用通常快速通过）

---

## Docker 部署（推荐）

### 1. 克隆仓库

```bash
git clone https://github.com/yourusername/feishu-adapter.git
cd feishu-adapter
```

### 2. 配置环境变量

```bash
cp .env.example .env
vim .env
```

必需配置：
```env
FEISHU_APP_ID=cli_xxxxxxxxxx
FEISHU_APP_SECRET=xxxxxxxxxxxxxxxxxxxxxx
```

可选配置：
```env
MAX_AGENTS=5
IDLE_TIMEOUT=7200
ALLOWED_USERS=ou_xxxxx,ou_yyyyy
ADMIN_USERS=ou_xxxxx
```

### 3. 构建镜像

```bash
docker-compose build
```

### 4. 启动服务

```bash
docker-compose up -d
```

### 5. 查看日志

```bash
docker-compose logs -f
```

成功启动的日志示例：
```
feishu-adapter | 2026-03-17 12:00:00 INFO database initialized path=/data/feishu-adapter.db
feishu-adapter | 2026-03-17 12:00:00 INFO feishu platform started
```

### 6. 验证服务

```bash
# 检查容器状态
docker-compose ps

# 检查健康状态
docker-compose exec feishu-adapter /app/healthcheck.sh && echo "✅ Healthy"
```

---

## 环境变量配置

### 核心配置

| 变量 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `FEISHU_APP_ID` | ✅ | - | 飞书应用 ID |
| `FEISHU_APP_SECRET` | ✅ | - | 飞书应用密钥 |
| `MAX_AGENTS` | ❌ | 5 | 最大并发 Agent 数量 |
| `IDLE_TIMEOUT` | ❌ | 7200s | Agent 闲置超时（秒） |

### 权限控制

| 变量 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `ALLOWED_USERS` | ❌ | 空（所有人） | 允许的飞书用户 ID（逗号分隔） |
| `ADMIN_USERS` | ❌ | 空 | 管理员用户 ID（逗号分隔） |

### 系统配置

| 变量 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `TZ` | ❌ | Asia/Shanghai | 时区 |
| `WORKSPACE_DIR` | ❌ | ~/projects | 项目根目录 |
| `DATA_DIR` | ❌ | /data | 数据目录 |

---

## 迁移和备份

### 备份数据

```bash
# 备份数据库和配置
docker-compose exec feishu-adapter tar czf /data/backup.tar.gz /data

# 复制到宿主机
docker cp feishu-adapter:/data/backup.tar.gz ./backup.tar.gz
```

### 迁移到新机器

```bash
# 1. 在新机器上克隆仓库
git clone https://github.com/yourusername/feishu-adapter.git
cd feishu-adapter

# 2. 恢复数据
tar xzf backup.tar.gz -C ./data

# 3. 在新机器上重新认证 CLI 工具
claude login
gh auth login

# 4. 启动服务
docker-compose up -d
```

---

## 常见问题

### Q: 如何查看所有项目？

```bash
docker-compose exec feishu-adapter sqlite3 /data/feishu-adapter.db \
  "SELECT id, name, agent_type, work_dir, status FROM projects;"
```

### Q: 如何手动重启 Agent？

```bash
# 重启整个服务
docker-compose restart

# 或在飞书中发送
/agent restart
```

### Q: 如何更新到最新版本？

```bash
git pull
docker-compose build
docker-compose up -d
```

### Q: 如何查看 Agent 进程？

```bash
docker-compose exec feishu-adapter ps aux | grep -E "claude|gemini|codex"
```

---

## 性能调优

### 增加 Agent 数量

```env
# .env
MAX_AGENTS=10
```

### 调整闲置超时

```env
# .env
IDLE_TIMEOUT=3600  # 1小时
```

### 资源限制

编辑 `docker-compose.yml`：

```yaml
deploy:
  resources:
    limits:
      cpus: '4'
      memory: 8G
    reservations:
      cpus: '2'
      memory: 4G
```

---

## 监控和日志

### 查看实时日志

```bash
docker-compose logs -f
```

### 查看特定时间段日志

```bash
docker-compose logs --since 1h
```

### 日志轮转

日志自动轮转配置在 `docker-compose.yml`：

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "10m"
    max-file: "3"
```

---

## 安全建议

1. ✅ **使用环境变量**：不要在配置文件中硬编码凭证
2. ✅ **限制用户访问**：配置 `ALLOWED_USERS`
3. ✅ **定期备份**：设置自动备份任务
4. ✅ **更新依赖**：定期更新 Docker 镜像
5. ✅ **监控日志**：检查异常登录和操作

---

## 下一步

- [使用指南](../README.md#使用场景)
- [设计文档](plans/2026-03-17-feishu-adapter-design.md)
- [集成指南](INTEGRATION.md)
