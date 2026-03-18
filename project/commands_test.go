package project

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amadabarney/cc-connect-plus/core"
)

type stubPlatform struct{ replies []string }

func (s *stubPlatform) Name() string                                                     { return "stub" }
func (s *stubPlatform) Start(_ core.MessageHandler) error                                { return nil }
func (s *stubPlatform) Reply(_ context.Context, _ any, content string) error {
	s.replies = append(s.replies, content)
	return nil
}
func (s *stubPlatform) Send(_ context.Context, _ any, content string) error {
	s.replies = append(s.replies, content)
	return nil
}
func (s *stubPlatform) Stop() error { return nil }
func (s *stubPlatform) lastReply() string {
	if len(s.replies) == 0 {
		return ""
	}
	return s.replies[len(s.replies)-1]
}

func makeMsg(userID, content string) *core.Message {
	return &core.Message{UserID: userID, Content: content}
}

func makeCommandHandler(t *testing.T) (*CommandHandler, *Manager, *ContextManager, string) {
	t.Helper()
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)
	ctxMgr := NewContextManager(database)
	pool := NewAgentPool(database, mgr, 0, time.Hour)
	handler := NewCommandHandler(mgr, ctxMgr, pool)
	return handler, mgr, ctxMgr, base
}

func TestHandleCommand_NotCommand(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	handled, err := handler.HandleCommand(context.Background(), makeMsg("u1", "hello"), &stubPlatform{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if handled {
		t.Error("expected handled=false")
	}
}

func TestHandleCommand_ProjectList_Empty(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	p := &stubPlatform{}
	handled, err := handler.HandleCommand(context.Background(), makeMsg("u1", "/project list"), p)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !handled {
		t.Error("expected handled=true")
	}
	if !strings.Contains(p.lastReply(), "还没有任何项目") {
		t.Errorf("expected empty list message, got: %q", p.lastReply())
	}
}

func TestHandleCommand_ProjectNew_InvalidArgs(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	p := &stubPlatform{}
	handler.HandleCommand(context.Background(), makeMsg("u1", "/project new"), p)
	if !strings.Contains(p.lastReply(), "用法") {
		t.Errorf("expected usage hint, got: %q", p.lastReply())
	}
}

func TestHandleCommand_ProjectSwitch_NotFound(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	p := &stubPlatform{}
	handler.HandleCommand(context.Background(), makeMsg("u1", "/project switch nonexistent"), p)
	if !strings.Contains(p.lastReply(), "项目不存在") {
		t.Errorf("expected not found message, got: %q", p.lastReply())
	}
}

func TestHandleCommand_ProjectInfo_NoProject(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	p := &stubPlatform{}
	handler.HandleCommand(context.Background(), makeMsg("u1", "/project info"), p)
	if !strings.Contains(p.lastReply(), "还没有选择项目") {
		t.Errorf("expected no-project message, got: %q", p.lastReply())
	}
}

func TestHandleCommand_AgentStatus_NoProject(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	p := &stubPlatform{}
	handler.HandleCommand(context.Background(), makeMsg("u1", "/agent status"), p)
	if !strings.Contains(p.lastReply(), "还没有选择项目") {
		t.Errorf("expected no-project message, got: %q", p.lastReply())
	}
}

func TestHandleCommand_HelpOnBareCommand(t *testing.T) {
	handler, _, _, _ := makeCommandHandler(t)
	p := &stubPlatform{}
	handled, _ := handler.HandleCommand(context.Background(), makeMsg("u1", "/project"), p)
	if !handled {
		t.Error("expected handled=true")
	}
	if !strings.Contains(p.lastReply(), "帮助") {
		t.Errorf("expected help message, got: %q", p.lastReply())
	}
}
