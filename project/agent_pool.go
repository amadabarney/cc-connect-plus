package project

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/amada/feishu-adapter/core"
	"github.com/amada/feishu-adapter/db"
)

// AgentInstance 包装了 Agent 和其会话
type AgentInstance struct {
	Agent       core.Agent
	Session     core.AgentSession
	ProjectID   int
	LastActive  time.Time
	mu          sync.Mutex
}

// AgentPool Agent 进程池
type AgentPool struct {
	instances      map[int]*AgentInstance // projectID -> AgentInstance
	mu             sync.RWMutex
	maxAgents      int
	idleTimeout    time.Duration
	db             *db.Database
	projectManager *Manager
}

// NewAgentPool 创建 Agent 进程池
func NewAgentPool(database *db.Database, projectMgr *Manager, maxAgents int, idleTimeout time.Duration) *AgentPool {
	pool := &AgentPool{
		instances:      make(map[int]*AgentInstance),
		maxAgents:      maxAgents,
		idleTimeout:    idleTimeout,
		db:             database,
		projectManager: projectMgr,
	}

	// 启动自动清理协程
	go pool.autoCleanup()

	return pool
}

// GetOrStart 获取或启动 Agent 实例
func (p *AgentPool) GetOrStart(ctx context.Context, projectID int) (*AgentInstance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 如果已存在，检查是否存活
	if instance, exists := p.instances[projectID]; exists {
		instance.mu.Lock()
		defer instance.mu.Unlock()

		if instance.Session != nil && instance.Session.Alive() {
			instance.LastActive = time.Now()
			p.projectManager.TouchActivity(projectID)
			return instance, nil
		}

		// 会话已死，需要重启
		delete(p.instances, projectID)
	}

	// 检查是否超过最大数量
	if len(p.instances) >= p.maxAgents {
		// 尝试清理最久未使用的实例
		if err := p.evictOldest(); err != nil {
			return nil, fmt.Errorf("agent pool is full and cannot evict: %w", err)
		}
	}

	// 启动新实例
	instance, err := p.startNewAgent(ctx, projectID)
	if err != nil {
		return nil, err
	}

	p.instances[projectID] = instance
	return instance, nil
}

// startNewAgent 启动新的 Agent 实例
func (p *AgentPool) startNewAgent(ctx context.Context, projectID int) (*AgentInstance, error) {
	// 从数据库获取项目配置
	project, err := p.projectManager.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	// 构建 Agent 选项
	opts := map[string]any{
		"work_dir": project.WorkDir,
		"mode":     project.AgentMode,
	}

	if project.Model != nil {
		opts["model"] = *project.Model
	}

	// 通过 cc-connect 的注册机制创建 Agent
	agent, err := core.CreateAgent(project.AgentType, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// 启动会话（使用项目 ID 作为 session ID）
	sessionID := fmt.Sprintf("project-%d", projectID)
	session, err := agent.StartSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}

	// 更新项目状态
	if err := p.projectManager.UpdateProjectStatus(projectID, "running"); err != nil {
		// 非致命错误，记录但继续
		fmt.Printf("Warning: failed to update project status: %v\n", err)
	}

	instance := &AgentInstance{
		Agent:      agent,
		Session:    session,
		ProjectID:  projectID,
		LastActive: time.Now(),
	}

	return instance, nil
}

// Stop 停止指定项目的 Agent
func (p *AgentPool) Stop(projectID int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	instance, exists := p.instances[projectID]
	if !exists {
		return nil // 已经停止
	}

	// 关闭会话
	if instance.Session != nil {
		instance.Session.Close()
	}

	// 停止 Agent
	if instance.Agent != nil {
		instance.Agent.Stop()
	}

	// 从池中移除
	delete(p.instances, projectID)

	// 更新项目状态
	p.projectManager.UpdateProjectStatus(projectID, "stopped")

	return nil
}

// StopAll 停止所有 Agent
func (p *AgentPool) StopAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for projectID, instance := range p.instances {
		if instance.Session != nil {
			instance.Session.Close()
		}
		if instance.Agent != nil {
			instance.Agent.Stop()
		}
		p.projectManager.UpdateProjectStatus(projectID, "stopped")
	}

	p.instances = make(map[int]*AgentInstance)
	return nil
}

// Restart 重启指定项目的 Agent
func (p *AgentPool) Restart(ctx context.Context, projectID int) error {
	if err := p.Stop(projectID); err != nil {
		return err
	}

	time.Sleep(500 * time.Millisecond) // 短暂延迟确保进程完全退出

	_, err := p.GetOrStart(ctx, projectID)
	return err
}

// autoCleanup 自动清理闲置的 Agent
func (p *AgentPool) autoCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		now := time.Now()
		var toEvict []int

		for projectID, instance := range p.instances {
			instance.mu.Lock()
			idle := now.Sub(instance.LastActive)
			instance.mu.Unlock()

			if idle > p.idleTimeout {
				toEvict = append(toEvict, projectID)
			}
		}
		p.mu.Unlock()

		// 清理闲置实例
		for _, projectID := range toEvict {
			p.Stop(projectID)
			p.projectManager.UpdateProjectStatus(projectID, "idle")
		}
	}
}

// evictOldest 驱逐最久未使用的实例
func (p *AgentPool) evictOldest() error {
	var oldestID int
	var oldestTime time.Time
	first := true

	for projectID, instance := range p.instances {
		instance.mu.Lock()
		lastActive := instance.LastActive
		instance.mu.Unlock()

		if first || lastActive.Before(oldestTime) {
			oldestID = projectID
			oldestTime = lastActive
			first = false
		}
	}

	if first {
		return fmt.Errorf("no instances to evict")
	}

	return p.Stop(oldestID)
}

// Get 获取 Agent 实例（不启动）
func (p *AgentPool) Get(projectID int) (*AgentInstance, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	instance, exists := p.instances[projectID]
	if exists {
		instance.mu.Lock()
		instance.LastActive = time.Now()
		instance.mu.Unlock()
	}

	return instance, exists
}

// Status 获取 Agent 池状态
func (p *AgentPool) Status() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"total_instances": len(p.instances),
		"max_agents":      p.maxAgents,
		"idle_timeout":    p.idleTimeout.String(),
	}
}
