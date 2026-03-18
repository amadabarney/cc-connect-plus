package project

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/amadabarney/cc-connect-plus/core"
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
		switch event.Type {
		case core.EventText:
			// Agent 输出文本
			if event.Content != "" {
				slog.Debug("sending agent text to platform", "content_preview", event.Content[:min(len(event.Content), 50)])
				if err := platform.Send(ctx, msg.ReplyCtx, event.Content); err != nil {
					slog.Error("failed to send agent text", "error", err)
				}
			}

		case core.EventPermissionRequest:
			// Agent 请求权限（工具调用）
			// 这里可以实现交互式审批，或根据 mode 自动批准
			slog.Info("permission request", "tool", event.ToolName, "request_id", event.RequestID)

			// 简单实现：自动批准（TODO: 实现交互式审批）
			result := core.PermissionResult{
				Behavior:     "allow",
				UpdatedInput: event.ToolInputRaw,
			}
			instance.Session.RespondPermission(event.RequestID, result)

		case core.EventError:
			// Agent 错误
			slog.Error("agent error", "error", event.Error)
			platform.Send(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 错误: %v", event.Error))

		case core.EventResult:
			// 对话结束
			if event.Done {
				slog.Debug("conversation done")
				return
			}

		default:
			slog.Debug("unknown event type", "type", string(event.Type))
		}
	}
}
