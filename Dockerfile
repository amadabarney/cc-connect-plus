# 多阶段构建 Dockerfile for feishuAdapter

# Stage 1: 构建 Go 二进制文件
FROM golang:1.22-alpine AS builder

WORKDIR /build

# 安装构建依赖
RUN apk add --no-cache git make gcc musl-dev sqlite-dev

# 复制 go 模块文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建二进制文件
RUN CGO_ENABLED=1 GOOS=linux go build -o feishu-adapter ./cmd/cc-connect

# Stage 2: 运行时镜像
FROM node:20-slim

# 安装系统依赖
RUN apt-get update && apt-get install -y \
    git \
    sqlite3 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 安装 AI CLI 工具
RUN npm install -g @anthropic/claude-code@latest && \
    npm install -g github-copilot-cli@latest

# 可选：安装 Gemini CLI（如果需要）
# RUN npm install -g @google/gemini-cli

# 创建应用目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/feishu-adapter /app/feishu-adapter

# 复制配置示例
COPY config.example.toml /app/config.example.toml

# 创建数据和工作目录
RUN mkdir -p /data /workspace /root/.claude

# 创建健康检查脚本
COPY <<'EOF' /app/healthcheck.sh
#!/bin/sh
# 检查进程是否运行
if ! pgrep -f feishu-adapter > /dev/null; then
    exit 1
fi

# 检查数据库文件是否存在
if [ ! -f /data/feishu-adapter.db ]; then
    exit 1
fi

exit 0
EOF

RUN chmod +x /app/healthcheck.sh

# 设置环境变量
ENV DATA_DIR=/data
ENV WORKSPACE=/workspace

# 暴露端口（如果需要 webhook 模式）
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD /app/healthcheck.sh

# Volume 挂载点
VOLUME ["/data", "/workspace", "/root/.claude", "/root/.config"]

# 启动命令
ENTRYPOINT ["/app/feishu-adapter"]
