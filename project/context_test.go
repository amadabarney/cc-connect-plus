package project

import "testing"

func TestGetCurrentProject_NoContext(t *testing.T) {
	database := mustInitDB(t)
	cm := NewContextManager(database)
	pid, err := cm.GetCurrentProject("user1")
	if err != nil {
		t.Fatalf("GetCurrentProject error: %v", err)
	}
	if pid != nil {
		t.Errorf("expected nil, got %d", *pid)
	}
}

func TestSetAndGetCurrentProject(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)
	cm := NewContextManager(database)

	p, _ := mgr.CreateProject("ctx-proj", base+"/ctxp", "claudecode")
	cm.SetCurrentProject("user1", p.ID)

	pid, err := cm.GetCurrentProject("user1")
	if err != nil {
		t.Fatalf("GetCurrentProject error: %v", err)
	}
	if pid == nil || *pid != p.ID {
		t.Errorf("GetCurrentProject = %v, want %d", pid, p.ID)
	}
}

func TestSetCurrentProject_Upsert(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)
	cm := NewContextManager(database)

	p1, _ := mgr.CreateProject("proj1", base+"/p1", "claudecode")
	p2, _ := mgr.CreateProject("proj2", base+"/p2", "codex")

	cm.SetCurrentProject("user1", p1.ID)
	cm.SetCurrentProject("user1", p2.ID) // UPSERT

	pid, _ := cm.GetCurrentProject("user1")
	if pid == nil || *pid != p2.ID {
		t.Errorf("after upsert want %d, got %v", p2.ID, pid)
	}
}

func TestClearCurrentProject(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)
	cm := NewContextManager(database)

	p, _ := mgr.CreateProject("clrp", base+"/clrp", "claudecode")
	cm.SetCurrentProject("user1", p.ID)
	cm.ClearCurrentProject("user1")

	pid, _ := cm.GetCurrentProject("user1")
	if pid != nil {
		t.Errorf("expected nil after clear, got %d", *pid)
	}
}
