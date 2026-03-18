package main

import (
	"context"
	"log/slog"

	"github.com/amadabarney/cc-connect-plus/core"
)

// WrapPlatform 包装任意 Platform，注入项目路由。
// 拦截所有消息，先尝试通过项目路由处理（项目管理命令或路由到 Agent），
// 若未被处理则传递给原始 cc-connect Engine。
func WrapPlatform(platform core.Platform, projectMgmt *ProjectManagement) core.Platform {
	if projectMgmt == nil {
		return platform
	}
	return &wrappedFeishuPlatform{
		Platform:    platform,
		projectMgmt: projectMgmt,
	}
}

// WrapFeishuPlatform 为向后兼容保留的别名，等同于 WrapPlatform。
func WrapFeishuPlatform(platform core.Platform, projectMgmt *ProjectManagement) core.Platform {
	return WrapPlatform(platform, projectMgmt)
}

// wrappedFeishuPlatform 包装任意平台，注入项目路由逻辑。
type wrappedFeishuPlatform struct {
	core.Platform
	projectMgmt *ProjectManagement
}

// Start 启动平台（注入包装后的 MessageHandler）
func (w *wrappedFeishuPlatform) Start(originalHandler core.MessageHandler) error {
	slog.Info("starting platform with project routing", "platform", w.Platform.Name())

	wrappedHandler := func(p core.Platform, msg *core.Message) {
		ctx := context.Background()

		slog.Debug("message received",
			"user_id", msg.UserID,
			"content_preview", truncate(msg.Content, 50),
			"platform", msg.Platform,
		)

		handled, err := w.projectMgmt.router.Route(ctx, p, msg)
		if err != nil {
			slog.Error("project router error", "error", err, "user_id", msg.UserID)
			originalHandler(p, msg)
			return
		}

		if handled {
			slog.Debug("message handled by project router", "user_id", msg.UserID)
			return
		}

		// 未被项目路由处理，传递给 Engine
		slog.Debug("message forwarded to original handler", "user_id", msg.UserID)
		originalHandler(p, msg)
	}

	return w.Platform.Start(wrappedHandler)
}
