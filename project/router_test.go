package project

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amada/feishu-adapter/core"
)

type stubAgentSession struct {
	events chan core.Event
	alive  bool
	sent   []string
}

func newStubSession() *stubAgentSession {
	return &stubAgentSession{events: make(chan core.Event, 10), alive: true}
}
func (s *stubAgentSession) Send(prompt string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	s.sent = append(s.sent, prompt)
	s.events <- core.Event{Type: core.EventResult, Done: true}
	return nil
}
func (s *stubAgentSession) RespondPermission(_ string, _ core.PermissionResult) error { return nil }
func (s *stubAgentSession) Events() <-chan core.Event                                  { return s.events }
func (s *stubAgentSession) CurrentSessionID() string                                   { return "stub" }
func (s *stubAgentSession) Alive() bool                                                { return s.alive }
func (s *stubAgentSession) Close() error                                               { s.alive = false; return nil }

type stubAgent struct{ session *stubAgentSession }

func (a *stubAgent) Name() string { return "stub-agent" }
func (a *stubAgent) StartSession(_ context.Context, _ string) (core.AgentSession, error) {
	return a.session, nil
}
func (a *stubAgent) ListSessions(_ context.Context) ([]core.AgentSessionInfo, error) { return nil, nil }
func (a *stubAgent) Stop() error                                                       { return nil }

func makeRouter(t *testing.T) (*Router, *Manager, *ContextManager, string) {
	t.Helper()
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)
	ctxMgr := NewContextManager(database)
	pool := NewAgentPool(database, mgr, 5, time.Hour)
	cmdHandler := NewCommandHandler(mgr, ctxMgr, pool)
	router := NewRouter(cmdHandler, ctxMgr, pool)
	return router, mgr, ctxMgr, base
}

func TestRoute_ProjectCommand(t *testing.T) {
	router, _, _, _ := makeRouter(t)
	p := &stubPlatform{}
	handled, err := router.Route(context.Background(), p, makeMsg("u1", "/project list"))
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if !handled {
		t.Error("expected handled=true for /project list")
	}
}

func TestRoute_NoProject(t *testing.T) {
	router, _, _, _ := makeRouter(t)
	p := &stubPlatform{}
	handled, err := router.Route(context.Background(), p, makeMsg("u1", "普通消息"))
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if !handled {
		t.Error("expected handled=true (with error reply)")
	}
	if !strings.Contains(p.lastReply(), "还没有选择项目") {
		t.Errorf("expected no-project message, got: %q", p.lastReply())
	}
}

func TestRoute_WithStubAgent(t *testing.T) {
	router, mgr, ctxMgr, base := makeRouter(t)

	// 注册 stub agent（使用合法 agentType 名称）
	sess := newStubSession()
	agent := &stubAgent{session: sess}
	core.RegisterAgent("claudecode", func(_ map[string]any) (core.Agent, error) {
		return agent, nil
	})

	// 创建项目并选中
	p, err := mgr.CreateProject("test-proj", base+"/test-proj", "claudecode")
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	ctxMgr.SetCurrentProject("u1", p.ID)

	platform := &stubPlatform{}
	handled, err := router.Route(context.Background(), platform, makeMsg("u1", "你好"))
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if !handled {
		t.Error("expected handled=true")
	}
}
