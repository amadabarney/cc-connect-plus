package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/amadabarney/cc-connect-plus/core"
	"github.com/amadabarney/cc-connect-plus/db"
	"github.com/amadabarney/cc-connect-plus/project"
)

// ProjectManagement 项目管理集成
type ProjectManagement struct {
	db         *db.Database
	projectMgr *project.Manager
	contextMgr *project.ContextManager
	agentPool  *project.AgentPool
	cmdHandler *project.CommandHandler
	router     *project.Router
}

// SetupProjectManagement 初始化项目管理系统
func SetupProjectManagement(dataDir string) (*ProjectManagement, error) {
	// 确保数据目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// 初始化数据库
	dbPath := filepath.Join(dataDir, "feishu-adapter.db")
	database, err := db.Init(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	slog.Info("project management database initialized", "path", dbPath)

	// 创建项目管理器
	projectMgr := project.NewManager(database)

	// 创建用户上下文管理器
	contextMgr := project.NewContextManager(database)

	// 创建 Agent 进程池
	maxAgents := 5
	if v := os.Getenv("MAX_AGENTS"); v != "" {
		fmt.Sscanf(v, "%d", &maxAgents)
	}

	idleTimeout := 2 * time.Hour
	if v := os.Getenv("IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			idleTimeout = d
		}
	}

	agentPool := project.NewAgentPool(database, projectMgr, maxAgents, idleTimeout)

	// 创建命令处理器
	cmdHandler := project.NewCommandHandler(projectMgr, contextMgr, agentPool)

	// 创建消息路由器
	router := project.NewRouter(cmdHandler, contextMgr, agentPool)

	slog.Info("project management initialized",
		"max_agents", maxAgents,
		"idle_timeout", idleTimeout,
	)

	return &ProjectManagement{
		db:         database,
		projectMgr: projectMgr,
		contextMgr: contextMgr,
		agentPool:  agentPool,
		cmdHandler: cmdHandler,
		router:     router,
	}, nil
}

// WrapMessageHandler 包装原始的 MessageHandler，注入项目路由
func (pm *ProjectManagement) WrapMessageHandler(originalHandler core.MessageHandler) core.MessageHandler {
	return func(p core.Platform, msg *core.Message) {
		ctx := context.Background()

		// 尝试通过项目路由处理
		handled, err := pm.router.Route(ctx, p, msg)
		if err != nil {
			slog.Error("project router error", "error", err)
			// 发生错误时降级到原始处理器
			originalHandler(p, msg)
			return
		}

		// 如果项目路由已处理（项目命令或已路由到 Agent），则完成
		if handled {
			return
		}

		// 否则，传递给原始处理器（cc-connect 的默认逻辑）
		originalHandler(p, msg)
	}
}

// Close 关闭项目管理系统
func (pm *ProjectManagement) Close() error {
	slog.Info("shutting down project management")

	// 停止所有 Agent
	if err := pm.agentPool.StopAll(); err != nil {
		slog.Error("failed to stop agent pool", "error", err)
	}

	// 关闭数据库
	if err := pm.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	return nil
}

// LoadProjectsFromDatabase 从数据库加载项目（可选：用于启动时同步）
func (pm *ProjectManagement) LoadProjectsFromDatabase() ([]*db.Project, error) {
	return pm.projectMgr.ListProjects()
}
