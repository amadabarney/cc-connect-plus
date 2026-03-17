package main

import (
	"context"
	"log/slog"

	"github.com/amada/feishu-adapter/core"
)

// WrapFeishuPlatform 包装飞书 Platform，注入项目路由
// 这个包装器拦截飞书消息，先尝试通过项目路由处理（项目管理命令或路由到 Agent）
// 如果未被处理，则传递给原始的 cc-connect Engine
func WrapFeishuPlatform(platform core.Platform, projectMgmt *ProjectManagement) core.Platform {
	if projectMgmt == nil {
		slog.Warn("project management is nil, skipping platform wrapper")
		return platform
	}

	slog.Info("wrapping feishu platform with project management")

	return &wrappedFeishuPlatform{
		Platform:    platform,
		projectMgmt: projectMgmt,
	}
}

// wrappedFeishuPlatform 包装的飞书平台
type wrappedFeishuPlatform struct {
	core.Platform
	projectMgmt *ProjectManagement
}

// Start 启动平台（注入包装后的 MessageHandler）
func (w *wrappedFeishuPlatform) Start(originalHandler core.MessageHandler) error {
	slog.Info("starting wrapped feishu platform with project routing")

	// 创建包装后的 handler
	wrappedHandler := func(p core.Platform, msg *core.Message) {
		ctx := context.Background()

		// 记录收到的消息（用于调试）
		slog.Debug("feishu message received",
			"user_id", msg.UserID,
			"content_preview", truncate(msg.Content, 50),
			"platform", msg.Platform,
		)

		// 尝试通过项目路由处理
		handled, err := w.projectMgmt.router.Route(ctx, p, msg)
		if err != nil {
			slog.Error("project router error",
				"error", err,
				"user_id", msg.UserID,
			)
			// 发生错误时，降级到原始处理器
			originalHandler(p, msg)
			return
		}

		// 如果项目路由已处理（项目命令或已路由到 Agent）
		if handled {
			slog.Debug("message handled by project router",
				"user_id", msg.UserID,
			)
			return
		}

		// 否则，传递给原始处理器（cc-connect 的默认逻辑）
		slog.Debug("message forwarded to original handler",
			"user_id", msg.UserID,
		)
		originalHandler(p, msg)
	}

	// 使用包装后的 handler 启动平台
	return w.Platform.Start(wrappedHandler)
}

// truncate 截断字符串用于日志
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
