package project

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/amada/feishu-adapter/core"
)

// Router 消息路由器
// 负责拦截项目管理命令并路由消息到对应的 Agent
type Router struct {
	commandHandler *CommandHandler
	contextMgr     *ContextManager
	agentPool      *AgentPool
}

// NewRouter 创建消息路由器
func NewRouter(
	commandHandler *CommandHandler,
	contextMgr *ContextManager,
	agentPool *AgentPool,
) *Router {
	return &Router{
		commandHandler: commandHandler,
		contextMgr:     contextMgr,
		agentPool:      agentPool,
	}
}

// Route 路由消息
// 返回 (handled, error)
// handled=true 表示消息已被项目管理命令处理，不需要继续传递
func (r *Router) Route(ctx context.Context, platform core.Platform, msg *core.Message) (bool, error) {
	// 1. 尝试处理项目管理命令
	handled, err := r.commandHandler.HandleCommand(ctx, msg, platform)
	if handled || err != nil {
		return handled, err
	}

	// 2. 不是项目命令，路由到当前项目的 Agent
	projectID, err := r.contextMgr.GetCurrentProject(msg.UserID)
	if err != nil {
		return false, fmt.Errorf("failed to get current project: %w", err)
	}

	if projectID == nil {
		// 用户还没有选择项目
		platform.Reply(ctx, msg.ReplyCtx, "❌ 还没有选择项目\n\n使用 /project list 查看所有项目\n或使用 /project new 创建新项目")
		return true, nil
	}

	// 3. 获取或启动 Agent
	instance, err := r.agentPool.GetOrStart(ctx, *projectID)
	if err != nil {
		return false, fmt.Errorf("failed to get agent: %w", err)
	}

	// 4. 发送消息到 Agent
	err = instance.Session.Send(msg.Content, msg.Images, msg.Files)
	if err != nil {
		slog.Error("failed to send message to agent",
			"project_id", *projectID,
			"error", err,
		)
		platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 发送消息失败: %v\n\n使用 /agent restart 重启 Agent", err))
		return true, nil
	}

	// 5. 处理 Agent 事件（流式输出）
	go r.handleAgentEvents(ctx, platform, msg, instance)

	return true, nil
}

// handleAgentEvents 处理 Agent 事件
func (r *Router) handleAgentEvents(ctx context.Context, platform core.Platform, msg *core.Message, instance *AgentInstance) {
	for event := range instance.Session.Events() {
		switch ev := event.(type) {
		case *core.TextEvent:
			// Agent 输出文本
			if ev.Text != "" {
				platform.Send(ctx, msg.ReplyCtx, ev.Text)
			}

		case *core.PermissionRequestEvent:
			// Agent 请求权限（工具调用）
			// 这里可以实现交互式审批，或根据 mode 自动批准
			slog.Info("permission request", "tool", ev.Tool, "request_id", ev.RequestID)

			// 简单实现：自动批准（TODO: 实现交互式审批）
			result := core.PermissionResult{
				Behavior:     "allow",
				UpdatedInput: ev.Input,
			}
			instance.Session.RespondPermission(ev.RequestID, result)

		case *core.ErrorEvent:
			// Agent 错误
			slog.Error("agent error", "error", ev.Error)
			platform.Send(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 错误: %v", ev.Error))

		case *core.DoneEvent:
			// 对话结束
			slog.Debug("conversation done")
			return

		default:
			slog.Debug("unknown event type", "type", fmt.Sprintf("%T", event))
		}
	}
}
