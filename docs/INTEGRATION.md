# feishuAdapter 集成指南

本文档说明如何将 feishuAdapter 的项目管理功能集成到 cc-connect 的 main.go 中。

## 目录

- [集成概述](#集成概述)
- [方案选择](#方案选择)
- [详细步骤](#详细步骤)
- [测试验证](#测试验证)
- [故障排查](#故障排查)

---

## 集成概述

feishuAdapter 在 cc-connect 基础上添加了动态项目管理功能。集成需要：

1. **初始化数据库和项目管理组件**
2. **包装消息处理流程**，注入项目路由
3. **在退出时清理资源**

我们提供了 `project_integration.go` 来简化集成。

---

## 方案选择

### 方案 A：最小集成（推荐）⭐

**适用场景**：快速验证功能，最小修改

**步骤**：
1. 使用 `project_integration.go` 提供的辅助函数
2. 在 main.go 添加 ~20 行代码
3. 不破坏现有功能

**优点**：
- ✅ 简单安全
- ✅ 易于回滚
- ✅ 保持向后兼容

**缺点**：
- ⚠️ 需要手动修改 main.go

---

### 方案 B：独立运行模式

**适用场景**：暂时不想修改 main.go

**步骤**：
1. 创建独立的配置文件 `config.project.toml`
2. 通过环境变量启用项目管理模式
3. 飞书平台专用于项目管理

**优点**：
- ✅ 完全独立
- ✅ 不修改原有代码

**缺点**：
- ❌ 功能受限
- ❌ 无法与 cc-connect 原有项目共存

---

## 详细步骤（方案 A）

### 步骤 1：确认文件已存在

确保以下文件已在项目中：

```
cmd/cc-connect/
└── project_integration.go  # ✅ 已创建
```

### 步骤 2：修改 main.go

#### 2.1 添加项目管理初始化

在 `main()` 函数中，找到配置加载后的位置：

```go
// 原有代码（约 line 120-125）
config.ConfigPath = configPath
slog.Info("config loaded", "path", configPath)

setupLogger(cfg.Log.Level, logWriter)

// 🆕 添加以下代码
// Initialize project management system
projectMgmt, err := SetupProjectManagement(cfg.DataDir)
if err != nil {
	slog.Error("failed to initialize project management", "error", err)
	// Non-fatal: continue without project management
	projectMgmt = nil
}
// Ensure cleanup on exit
if projectMgmt != nil {
	defer func() {
		if err := projectMgmt.Close(); err != nil {
			slog.Error("failed to close project management", "error", err)
		}
	}()
}

engines := make([]*core.Engine, 0, len(cfg.Projects))
```

#### 2.2 包装 Platform 的 MessageHandler

有两种方式包装 MessageHandler：

**方式 A：在 Engine 创建后包装**

在创建完所有 Engine 后（约 line 450-470），添加：

```go
// 原有代码
engines = append(engines, engine)

// 🆕 添加：为每个 Engine 注入项目路由
if projectMgmt != nil {
	// 获取 Engine 的内部 handler（需要 Engine 提供访问方法）
	// 或者在 Platform.Start() 时包装
}
```

**方式 B：修改 Platform 启动逻辑（推荐）**

找到 Engine.Start() 调用的地方（约 line 484-488）：

```go
// 原有代码
var startErrors []error
for _, e := range engines {
	if err := e.Start(); err != nil {
		slog.Warn("engine start partially failed", "error", err)
		startErrors = append(startErrors, err)
	}
}

// 🆕 修改为：
var startErrors []error
for i, e := range engines {
	// 如果启用了项目管理，包装第一个 Engine（通常是飞书）
	if projectMgmt != nil && i == 0 {
		// 注入项目路由到飞书平台
		// 注意：这需要修改 Engine 的内部逻辑
		// 或使用 Platform 的包装器
	}

	if err := e.Start(); err != nil {
		slog.Warn("engine start partially failed", "error", err)
		startErrors = append(startErrors, err)
	}
}
```

---

### 步骤 3：实现 MessageHandler 包装（简化方案）

由于直接修改 Engine/Platform 的内部逻辑较复杂，这里提供一个**简化方案**：

创建一个新文件 `cmd/cc-connect/feishu_wrapper.go`：

```go
package main

import (
	"context"
	"log/slog"

	"github.com/amada/feishu-adapter/core"
)

// WrapFeishuPlatform 包装飞书 Platform，注入项目路由
func WrapFeishuPlatform(platform core.Platform, projectMgmt *ProjectManagement) core.Platform {
	if projectMgmt == nil {
		return platform
	}

	return &wrappedFeishuPlatform{
		Platform:    platform,
		projectMgmt: projectMgmt,
	}
}

type wrappedFeishuPlatform struct {
	core.Platform
	projectMgmt *ProjectManagement
}

func (w *wrappedFeishuPlatform) Start(handler core.MessageHandler) error {
	// 包装 handler，先经过项目路由
	wrappedHandler := func(p core.Platform, msg *core.Message) {
		ctx := context.Background()

		// 尝试通过项目路由处理
		handled, err := w.projectMgmt.router.Route(ctx, p, msg)
		if err != nil {
			slog.Error("project router error", "error", err)
		}

		// 如果未被路由处理，传递给原始 handler
		if !handled {
			handler(p, msg)
		}
	}

	// 使用包装后的 handler 启动
	return w.Platform.Start(wrappedHandler)
}
```

然后在创建 Platform 时包装：

```go
// 在 main.go 中，创建 Platform 的地方（约 line 153-161）
var platforms []core.Platform
for _, pc := range proj.Platforms {
	p, err := core.CreatePlatform(pc.Type, pc.Options)
	if err != nil {
		slog.Error("failed to create platform", "error", err)
		os.Exit(1)
	}

	// 🆕 如果是飞书平台且启用了项目管理，则包装
	if projectMgmt != nil && pc.Type == "feishu" {
		p = WrapFeishuPlatform(p, projectMgmt)
	}

	platforms = append(platforms, p)
}
```

---

## 测试验证

### 1. 编译测试

```bash
cd cmd/cc-connect
go build -o feishu-adapter
```

### 2. 运行测试

```bash
./feishu-adapter --config ../../config.toml
```

### 3. 功能测试

在飞书中发送：

```
/project list
```

预期响应：
- 如果集成成功：显示项目列表（可能为空）
- 如果集成失败：无响应或错误消息

### 4. 创建项目测试

```
/project new test /workspace/test
```

预期：
- 创建项目成功
- 数据库中有记录：`sqlite3 data/feishu-adapter.db "SELECT * FROM projects;"`

---

## 故障排查

### 问题 1：编译错误

**错误**：`undefined: SetupProjectManagement`

**解决**：
```bash
# 确保 project_integration.go 在同一目录
ls cmd/cc-connect/project_integration.go

# 重新构建
go build -o feishu-adapter ./cmd/cc-connect
```

### 问题 2：数据库初始化失败

**错误**：`failed to initialize database`

**解决**：
```bash
# 检查数据目录权限
ls -la ~/.cc-connect/

# 创建目录
mkdir -p ~/.cc-connect/
chmod 755 ~/.cc-connect/
```

### 问题 3：项目命令无响应

**可能原因**：MessageHandler 未被正确包装

**排查**：
```bash
# 查看日志
tail -f ~/.cc-connect/cc-connect.log

# 检查数据库
sqlite3 ~/.cc-connect/feishu-adapter.db "SELECT * FROM projects;"
```

### 问题 4：Agent 启动失败

**错误**：`failed to start agent`

**解决**：
```bash
# 检查 CLI 工具认证
claude models list
gh copilot explain "test"

# 重新认证
claude login
```

---

## 完整代码示例

### 最小集成版 main.go 修改

```diff
diff --git a/cmd/cc-connect/main.go b/cmd/cc-connect/main.go
index xxx..yyy 100644
--- a/cmd/cc-connect/main.go
+++ b/cmd/cc-connect/main.go
@@ -120,6 +120,19 @@ func main() {
 	config.ConfigPath = configPath
 	slog.Info("config loaded", "path", configPath)

 	setupLogger(cfg.Log.Level, logWriter)
+
+	// Initialize project management system
+	projectMgmt, err := SetupProjectManagement(cfg.DataDir)
+	if err != nil {
+		slog.Error("failed to initialize project management", "error", err)
+		projectMgmt = nil
+	}
+	if projectMgmt != nil {
+		defer func() {
+			if err := projectMgmt.Close(); err != nil {
+				slog.Error("failed to close project management", "error", err)
+			}
+		}()
+	}

 	engines := make([]*core.Engine, 0, len(cfg.Projects))
@@ -158,6 +171,11 @@ func main() {
 			os.Exit(1)
 		}
+
+		// Wrap Feishu platform with project routing
+		if projectMgmt != nil && pc.Type == "feishu" {
+			p = WrapFeishuPlatform(p, projectMgmt)
+		}
+
 		platforms = append(platforms, p)
 	}
```

---

## 下一步

1. ✅ **测试基本功能**：项目创建、列表、切换
2. ✅ **测试 Agent 启动**：确保 Claude Code 等工具正常运行
3. ✅ **压力测试**：多项目并发、长时间运行
4. 📝 **补充单元测试**：为项目管理模块添加测试
5. 📚 **完善文档**：添加更多使用示例

---

## 参考资料

- [设计文档](plans/2026-03-17-feishu-adapter-design.md)
- [部署指南](DEPLOYMENT.md)
- [README](../README.md)
- [cc-connect 文档](https://github.com/chenhg5/cc-connect)

---

**注意**：集成过程中如遇到问题，请查看日志文件或提交 Issue。
