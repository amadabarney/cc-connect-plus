package project

import (
	"strings"
	"testing"
)

func TestCreateProject_Success(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)

	p, err := mgr.CreateProject("myapp", base+"/myapp", "claudecode")
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if p.Name != "myapp" {
		t.Errorf("Name = %q, want myapp", p.Name)
	}
	if p.AgentType != "claudecode" {
		t.Errorf("AgentType = %q, want claudecode", p.AgentType)
	}
}

func TestCreateProject_ValidationErrors(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)

	tests := []struct {
		name, projName, workDir, agentType, wantErr string
	}{
		{"empty name", "", base + "/x", "claudecode", "required"},
		{"empty workDir", "p", "", "claudecode", "required"},
		{"invalid agentType", "p", base + "/x", "invalid", "invalid agent type"},
		{"path outside base", "p", "/tmp/outside", "claudecode", "must be under"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mgr.CreateProject(tc.projName, tc.workDir, tc.agentType)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestCreateProject_DuplicateName(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)

	mgr.CreateProject("dup", base+"/dup1", "claudecode")
	_, err := mgr.CreateProject("dup", base+"/dup2", "claudecode")
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestGetProject_NotFound(t *testing.T) {
	database := mustInitDB(t)
	mgr := NewManager(database)
	_, err := mgr.GetProject(9999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListProjects(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)

	projects, _ := mgr.ListProjects()
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}

	mgr.CreateProject("alpha", base+"/alpha", "codex")
	mgr.CreateProject("beta", base+"/beta", "codex")

	projects, err := mgr.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects error: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}
}

func TestDeleteProject(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)

	p, _ := mgr.CreateProject("todel", base+"/todel", "gemini")
	if err := mgr.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject error: %v", err)
	}
	if err := mgr.DeleteProject(p.ID); err == nil {
		t.Fatal("expected error for second delete")
	}
}

func TestUpdateProjectStatus(t *testing.T) {
	database := mustInitDB(t)
	base := workspaceDir(t)
	mgr := NewManagerWithBase(database, base)

	p, _ := mgr.CreateProject("sp", base+"/sp", "claudecode")
	mgr.UpdateProjectStatus(p.ID, "running")

	updated, _ := mgr.GetProject(p.ID)
	if updated.Status != "running" {
		t.Errorf("Status = %q, want running", updated.Status)
	}
}
