package project

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/amada/feishu-adapter/core"
	"github.com/amada/feishu-adapter/db"
)

// CommandHandler 项目管理命令处理器
type CommandHandler struct {
	projectMgr  *Manager
	contextMgr  *ContextManager
	agentPool   *AgentPool
	enginePool  map[int]*core.Engine // projectID -> Engine
	createEngine func(projectID int) (*core.Engine, error) // 动态创建 Engine 的回调
}

// NewCommandHandler 创建命令处理器
func NewCommandHandler(
	projectMgr *Manager,
	contextMgr *ContextManager,
	agentPool *AgentPool,
) *CommandHandler {
	return &CommandHandler{
		projectMgr: projectMgr,
		contextMgr: contextMgr,
		agentPool:  agentPool,
		enginePool: make(map[int]*core.Engine),
	}
}

// SetEngineCreator 设置 Engine 创建回调
func (h *CommandHandler) SetEngineCreator(fn func(projectID int) (*core.Engine, error)) {
	h.createEngine = fn
}

// HandleCommand 处理项目管理命令
func (h *CommandHandler) HandleCommand(ctx context.Context, msg *core.Message, platform core.Platform) (bool, error) {
	text := strings.TrimSpace(msg.Content)

	// 检查是否是项目管理命令
	if !strings.HasPrefix(text, "/project") && !strings.HasPrefix(text, "/agent") {
		return false, nil // 不是项目命令，继续正常流程
	}

	parts := strings.Fields(text)
	if len(parts) < 2 {
		h.sendHelp(ctx, platform, msg)
		return true, nil
	}

	command := parts[0]
	subcommand := parts[1]
	args := parts[2:]

	switch command {
	case "/project":
		return true, h.handleProjectCommand(ctx, platform, msg, subcommand, args)
	case "/agent":
		return true, h.handleAgentCommand(ctx, platform, msg, subcommand, args)
	default:
		return false, nil
	}
}

// handleProjectCommand 处理 /project 命令
func (h *CommandHandler) handleProjectCommand(ctx context.Context, platform core.Platform, msg *core.Message, subcommand string, args []string) error {
	switch subcommand {
	case "new":
		return h.createProject(ctx, platform, msg, args)
	case "list":
		return h.listProjects(ctx, platform, msg)
	case "switch":
		return h.switchProject(ctx, platform, msg, args)
	case "delete":
		return h.deleteProject(ctx, platform, msg, args)
	case "info":
		return h.projectInfo(ctx, platform, msg)
	default:
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 未知的项目命令: %s\n\n使用 /project help 查看帮助", subcommand))
	}
}

// createProject 创建新项目
func (h *CommandHandler) createProject(ctx context.Context, platform core.Platform, msg *core.Message, args []string) error {
	if len(args) < 2 {
		return platform.Reply(ctx, msg.ReplyCtx, "❌ 用法: /project new <name> <path> [agent_type]\n\n示例:\n  /project new backend /workspace/backend\n  /project new frontend /workspace/frontend claudecode")
	}

	name := args[0]
	workDir := args[1]
	agentType := "claudecode" // 默认使用 Claude Code

	if len(args) >= 3 {
		agentType = args[2]
	}

	// 展开相对路径
	absPath, err := filepath.Abs(workDir)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 无效的路径: %v", err))
	}

	// 检查目录是否存在
	if err := h.projectMgr.CreateWorkDir(absPath); err != nil {
		// 询问是否创建
		card := &core.Card{
			Title: "📁 目录不存在",
			Sections: []core.CardSection{
				{Content: fmt.Sprintf("目录 `%s` 不存在，是否创建？", absPath)},
			},
			Actions: []core.CardAction{
				{ID: fmt.Sprintf("create_dir:%s:%s:%s", name, absPath, agentType), Label: "✅ 创建"},
				{ID: "cancel", Label: "❌ 取消"},
			},
		}

		if cs, ok := platform.(core.CardSender); ok {
			return cs.ReplyCard(ctx, msg.ReplyCtx, card)
		}
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 目录不存在: %s\n请先创建目录", absPath))
	}

	// 创建项目
	project, err := h.projectMgr.CreateProject(name, absPath, agentType)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 创建项目失败: %v", err))
	}

	// 设置为当前项目
	if err := h.contextMgr.SetCurrentProject(msg.UserID, project.ID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("⚠️ 项目已创建，但设置当前项目失败: %v", err))
	}

	// 启动 Agent
	if _, err := h.agentPool.GetOrStart(ctx, project.ID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("⚠️ 项目已创建，但启动 Agent 失败: %v\n\n使用 /agent restart 重试", err))
	}

	return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("✅ 项目已创建并激活\n\n名称: %s\n类型: %s\n路径: %s", name, agentType, absPath))
}

// listProjects 列出所有项目
func (h *CommandHandler) listProjects(ctx context.Context, platform core.Platform, msg *core.Message) error {
	projects, err := h.projectMgr.ListProjects()
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 获取项目列表失败: %v", err))
	}

	if len(projects) == 0 {
		return platform.Reply(ctx, msg.ReplyCtx, "📁 还没有任何项目\n\n使用 /project new <name> <path> 创建第一个项目")
	}

	// 获取当前项目
	currentProjectID, _ := h.contextMgr.GetCurrentProject(msg.UserID)

	var sections []core.CardSection
	for _, p := range projects {
		isCurrent := currentProjectID != nil && *currentProjectID == p.ID
		statusIcon := "⭕"
		if isCurrent {
			statusIcon = "✅"
		}

		agentName := p.AgentType
		statusText := p.Status

		var lastActive string
		if p.LastActiveAt != nil {
			duration := time.Since(*p.LastActiveAt)
			if duration < time.Minute {
			lastActive = "刚刚"
			} else if duration < time.Hour {
				lastActive = fmt.Sprintf("%d分钟前", int(duration.Minutes()))
			} else if duration < 24*time.Hour {
				lastActive = fmt.Sprintf("%d小时前", int(duration.Hours()))
			} else {
				lastActive = fmt.Sprintf("%d天前", int(duration.Hours()/24))
			}
		} else {
			lastActive = "从未使用"
		}

		section := core.CardSection{
			Title: fmt.Sprintf("%s %s (%s)", statusIcon, p.Name, agentName),
			Content: fmt.Sprintf("路径: `%s`\n状态: %s\n最后活跃: %s", p.WorkDir, statusText, lastActive),
		}

		sections = append(sections, section)
	}

	card := &core.Card{
		Title:    "📁 项目列表",
		Sections: sections,
	}

	if cs, ok := platform.(core.CardSender); ok {
		return cs.ReplyCard(ctx, msg.ReplyCtx, card)
	}

	// 降级为文本
	var text strings.Builder
	text.WriteString("📁 项目列表\n\n")
	for _, section := range sections {
		text.WriteString(fmt.Sprintf("%s\n%s\n\n", section.Title, section.Content))
	}

	return platform.Reply(ctx, msg.ReplyCtx, text.String())
}

// switchProject 切换项目
func (h *CommandHandler) switchProject(ctx context.Context, platform core.Platform, msg *core.Message, args []string) error {
	if len(args) < 1 {
		return platform.Reply(ctx, msg.ReplyCtx, "❌ 用法: /project switch <name>")
	}

	name := args[0]

	// 查找项目
	project, err := h.projectMgr.GetProjectByName(name)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 项目不存在: %s\n\n使用 /project list 查看所有项目", name))
	}

	// 启动 Agent（如果未运行）
	if _, err := h.agentPool.GetOrStart(ctx, project.ID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 启动 Agent 失败: %v", err))
	}

	// 设置当前项目
	if err := h.contextMgr.SetCurrentProject(msg.UserID, project.ID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 切换失败: %v", err))
	}

	return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("✅ 已切换到项目: %s\n\nAgent: %s\n路径: %s", project.Name, project.AgentType, project.WorkDir))
}

// deleteProject 删除项目
func (h *CommandHandler) deleteProject(ctx context.Context, platform core.Platform, msg *core.Message, args []string) error {
	if len(args) < 1 {
		return platform.Reply(ctx, msg.ReplyCtx, "❌ 用法: /project delete <name>")
	}

	name := args[0]

	// 查找项目
	project, err := h.projectMgr.GetProjectByName(name)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 项目不存在: %s", name))
	}

	// 停止 Agent
	h.agentPool.Stop(project.ID)

	// 删除项目
	if err := h.projectMgr.DeleteProject(project.ID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 删除失败: %v", err))
	}

	// 如果这是当前项目，清除上下文
	currentProjectID, _ := h.contextMgr.GetCurrentProject(msg.UserID)
	if currentProjectID != nil && *currentProjectID == project.ID {
		h.contextMgr.ClearCurrentProject(msg.UserID)
	}

	return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("✅ 项目已删除: %s", name))
}

// projectInfo 显示当前项目信息
func (h *CommandHandler) projectInfo(ctx context.Context, platform core.Platform, msg *core.Message) error {
	projectID, err := h.contextMgr.GetCurrentProject(msg.UserID)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 获取当前项目失败: %v", err))
	}

	if projectID == nil {
		return platform.Reply(ctx, msg.ReplyCtx, "❌ 还没有选择项目\n\n使用 /project list 查看所有项目\n或使用 /project new 创建新项目")
	}

	project, err := h.projectMgr.GetProject(*projectID)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 获取项目信息失败: %v", err))
	}

	instance, exists := h.agentPool.Get(*projectID)
	var agentStatus string
	if exists && instance.Session != nil && instance.Session.Alive() {
		agentStatus = "运行中"
	} else {
		agentStatus = "已停止"
	}

	return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("📊 当前项目: %s\n\nAgent: %s\n状态: %s\n路径: %s\n创建时间: %s",
		project.Name,
		project.AgentType,
		agentStatus,
		project.WorkDir,
		project.CreatedAt.Format("2006-01-02 15:04:05"),
	))
}

// handleAgentCommand 处理 /agent 命令
func (h *CommandHandler) handleAgentCommand(ctx context.Context, platform core.Platform, msg *core.Message, subcommand string, args []string) error {
	projectID, err := h.contextMgr.GetCurrentProject(msg.UserID)
	if err != nil || projectID == nil {
		return platform.Reply(ctx, msg.ReplyCtx, "❌ 还没有选择项目\n\n使用 /project list 查看所有项目")
	}

	switch subcommand {
	case "switch":
		return h.switchAgent(ctx, platform, msg, *projectID, args)
	case "restart":
		return h.restartAgent(ctx, platform, msg, *projectID)
	case "status":
		return h.agentStatus(ctx, platform, msg, *projectID)
	default:
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 未知的 Agent 命令: %s", subcommand))
	}
}

// switchAgent 切换 Agent 类型
func (h *CommandHandler) switchAgent(ctx context.Context, platform core.Platform, msg *core.Message, projectID int, args []string) error {
	if len(args) < 1 {
		return platform.Reply(ctx, msg.ReplyCtx, "❌ 用法: /agent switch <type>\n\n支持的类型: claudecode, codex, gemini")
	}

	newAgentType := args[0]

	// 停止当前 Agent
	h.agentPool.Stop(projectID)

	// 更新项目配置
	if err := h.projectMgr.UpdateProjectAgentType(projectID, newAgentType); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 更新失败: %v", err))
	}

	// 启动新 Agent
	if _, err := h.agentPool.GetOrStart(ctx, projectID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("⚠️ Agent 类型已更新，但启动失败: %v\n\n使用 /agent restart 重试", err))
	}

	return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("✅ 已切换到 %s\n\n⚠️ 注意：上下文已重置，这是一个新的对话", newAgentType))
}

// restartAgent 重启 Agent
func (h *CommandHandler) restartAgent(ctx context.Context, platform core.Platform, msg *core.Message, projectID int) error {
	if err := h.agentPool.Restart(ctx, projectID); err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 重启失败: %v", err))
	}

	return platform.Reply(ctx, msg.ReplyCtx, "✅ Agent 已重启")
}

// agentStatus Agent 状态
func (h *CommandHandler) agentStatus(ctx context.Context, platform core.Platform, msg *core.Message, projectID int) error {
	project, err := h.projectMgr.GetProject(projectID)
	if err != nil {
		return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("❌ 获取项目信息失败: %v", err))
	}

	instance, exists := h.agentPool.Get(projectID)
	var status string
	if exists && instance.Session != nil && instance.Session.Alive() {
		status = "✅ 运行中"
	} else {
		status = "❌ 已停止"
	}

	poolStatus := h.agentPool.Status()

	return platform.Reply(ctx, msg.ReplyCtx, fmt.Sprintf("📊 Agent 状态\n\n项目: %s\nAgent: %s\n状态: %s\n\n进程池: %d/%d",
		project.Name,
		project.AgentType,
		status,
		poolStatus["total_instances"],
		poolStatus["max_agents"],
	))
}

// sendHelp 发送帮助信息
func (h *CommandHandler) sendHelp(ctx context.Context, platform core.Platform, msg *core.Message) {
	help := `📖 项目管理命令帮助

**项目管理:**
/project new <name> <path> [agent]  - 创建新项目
/project list                       - 列出所有项目
/project switch <name>              - 切换到指定项目
/project delete <name>              - 删除项目
/project info                       - 显示当前项目信息

**Agent 管理:**
/agent switch <type>  - 切换 AI 工具 (claudecode/codex/gemini)
/agent restart        - 重启当前 Agent
/agent status         - 查看 Agent 状态

**示例:**
/project new backend /workspace/backend claudecode
/project switch backend
/agent switch gemini
`
	platform.Reply(ctx, msg.ReplyCtx, help)
}
