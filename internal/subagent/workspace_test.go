package subagent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/edouard/pureclaw/internal/workspace"
)

func testParentWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	return &workspace.Workspace{
		Root:        t.TempDir(),
		AgentMD:     "# Parent Agent\n\n## Environment\n\n- **OS:** linux",
		SoulMD:      "You are a helpful assistant with a friendly personality.",
		HeartbeatMD: "# Heartbeat\n\n- [ ] Check disk space",
		Skills: []workspace.Skill{
			{Name: "weather", Content: "# Weather Skill\n\nFetch weather data."},
			{Name: "search", Content: "# Search Skill\n\nSearch the web."},
		},
	}
}

func TestCreateWorkspace_Success(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace:  parent,
		TaskID:           "task-001",
		TaskDescription:  "Summarize the document",
		AgentsDir:        agentsDir,
		IncludeHeartbeat: true,
		IncludeSkills:    true,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	expected := filepath.Join(agentsDir, "task-001")
	if wsPath != expected {
		t.Errorf("path = %q, want %q", wsPath, expected)
	}

	// Verify AGENT.md exists.
	data, err := os.ReadFile(filepath.Join(wsPath, "AGENT.md"))
	if err != nil {
		t.Fatalf("read AGENT.md: %v", err)
	}
	if !strings.Contains(string(data), "Summarize the document") {
		t.Error("AGENT.md missing task description")
	}

	// Verify SOUL.md exists.
	data, err = os.ReadFile(filepath.Join(wsPath, "SOUL.md"))
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	if string(data) != parent.SoulMD {
		t.Error("SOUL.md content mismatch")
	}

	// Verify HEARTBEAT.md exists.
	data, err = os.ReadFile(filepath.Join(wsPath, "HEARTBEAT.md"))
	if err != nil {
		t.Fatalf("read HEARTBEAT.md: %v", err)
	}
	if string(data) != parent.HeartbeatMD {
		t.Error("HEARTBEAT.md content mismatch")
	}

	// Verify skills.
	for _, skill := range parent.Skills {
		data, err = os.ReadFile(filepath.Join(wsPath, "skills", skill.Name, "SKILL.md"))
		if err != nil {
			t.Fatalf("read skill %s: %v", skill.Name, err)
		}
		if string(data) != skill.Content {
			t.Errorf("skill %s content mismatch", skill.Name)
		}
	}

	// Verify memory/ directory.
	info, err := os.Stat(filepath.Join(wsPath, "memory"))
	if err != nil {
		t.Fatalf("memory dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("memory is not a directory")
	}
}

func TestCreateWorkspace_AgentMDContent(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "research-42",
		TaskDescription: "Research quantum computing advances",
		AgentsDir:       agentsDir,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wsPath, "AGENT.md"))
	if err != nil {
		t.Fatalf("read AGENT.md: %v", err)
	}
	content := string(data)

	checks := []string{
		"# Sub-Agent: research-42",
		"Research quantum computing advances",
		"depth=1",
		"CANNOT spawn further sub-agents",
		"NO Telegram access",
		"result.md",
		"## Environment",
		"_To be populated by introspection on first run._",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("AGENT.md missing: %q", check)
		}
	}
}

func TestCreateWorkspace_SoulMDInherited(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-soul",
		TaskDescription: "Test soul inheritance",
		AgentsDir:       agentsDir,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wsPath, "SOUL.md"))
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	if string(data) != parent.SoulMD {
		t.Errorf("SOUL.md = %q, want %q", string(data), parent.SoulMD)
	}
}

func TestCreateWorkspace_WithHeartbeat(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace:  parent,
		TaskID:           "task-hb",
		TaskDescription:  "Test heartbeat",
		AgentsDir:        agentsDir,
		IncludeHeartbeat: true,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wsPath, "HEARTBEAT.md"))
	if err != nil {
		t.Fatalf("read HEARTBEAT.md: %v", err)
	}
	if string(data) != parent.HeartbeatMD {
		t.Errorf("HEARTBEAT.md content mismatch")
	}
}

func TestCreateWorkspace_WithoutHeartbeat(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace:  parent,
		TaskID:           "task-no-hb",
		TaskDescription:  "Test no heartbeat",
		AgentsDir:        agentsDir,
		IncludeHeartbeat: false,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	_, err = os.Stat(filepath.Join(wsPath, "HEARTBEAT.md"))
	if !os.IsNotExist(err) {
		t.Error("HEARTBEAT.md should not exist when IncludeHeartbeat=false")
	}
}

func TestCreateWorkspace_HeartbeatEmptyParent(t *testing.T) {
	parent := testParentWorkspace(t)
	parent.HeartbeatMD = "" // Parent has no heartbeat
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace:  parent,
		TaskID:           "task-empty-hb",
		TaskDescription:  "Test empty heartbeat parent",
		AgentsDir:        agentsDir,
		IncludeHeartbeat: true, // Requested but parent has none
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	_, err = os.Stat(filepath.Join(wsPath, "HEARTBEAT.md"))
	if !os.IsNotExist(err) {
		t.Error("HEARTBEAT.md should not exist when parent HeartbeatMD is empty")
	}
}

func TestCreateWorkspace_WithSkills(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-skills",
		TaskDescription: "Test skills copy",
		AgentsDir:       agentsDir,
		IncludeSkills:   true,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	for _, skill := range parent.Skills {
		path := filepath.Join(wsPath, "skills", skill.Name, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read skill %s: %v", skill.Name, err)
		}
		if string(data) != skill.Content {
			t.Errorf("skill %s content mismatch", skill.Name)
		}
	}
}

func TestCreateWorkspace_WithoutSkills(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-no-skills",
		TaskDescription: "Test no skills",
		AgentsDir:       agentsDir,
		IncludeSkills:   false,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	_, err = os.Stat(filepath.Join(wsPath, "skills"))
	if !os.IsNotExist(err) {
		t.Error("skills/ should not exist when IncludeSkills=false")
	}
}

func TestCreateWorkspace_SkillsEmptyParent(t *testing.T) {
	parent := testParentWorkspace(t)
	parent.Skills = nil // Parent has no skills
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-empty-skills",
		TaskDescription: "Test empty skills parent",
		AgentsDir:       agentsDir,
		IncludeSkills:   true, // Requested but parent has none
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	_, err = os.Stat(filepath.Join(wsPath, "skills"))
	if !os.IsNotExist(err) {
		t.Error("skills/ should not exist when parent has no skills")
	}
}

func TestCreateWorkspace_MemoryDirCreated(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-mem",
		TaskDescription: "Test memory dir",
		AgentsDir:       agentsDir,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(wsPath, "memory"))
	if err != nil {
		t.Fatalf("stat memory: %v", err)
	}
	if !info.IsDir() {
		t.Error("memory is not a directory")
	}
}

func TestCreateWorkspace_EmptyTaskID(t *testing.T) {
	parent := testParentWorkspace(t)

	_, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "",
		TaskDescription: "Some task",
		AgentsDir:       t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for empty TaskID")
	}
	if !strings.Contains(err.Error(), "task ID is required") {
		t.Errorf("error = %q, want 'task ID is required'", err)
	}
}

func TestCreateWorkspace_EmptyTaskDescription(t *testing.T) {
	parent := testParentWorkspace(t)

	_, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-x",
		TaskDescription: "",
		AgentsDir:       t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for empty TaskDescription")
	}
	if !strings.Contains(err.Error(), "task description is required") {
		t.Errorf("error = %q, want 'task description is required'", err)
	}
}

func TestCreateWorkspace_NilParentWorkspace(t *testing.T) {
	_, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: nil,
		TaskID:          "task-x",
		TaskDescription: "Some task",
		AgentsDir:       t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for nil ParentWorkspace")
	}
	if !strings.Contains(err.Error(), "parent workspace is required") {
		t.Errorf("error = %q, want 'parent workspace is required'", err)
	}
}

func TestCreateWorkspace_AlreadyExists(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	// Pre-create the workspace directory.
	wsPath := filepath.Join(agentsDir, "existing-task")
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "existing-task",
		TaskDescription: "Should fail",
		AgentsDir:       agentsDir,
	})
	if err == nil {
		t.Fatal("expected error for existing workspace")
	}
	if !strings.Contains(err.Error(), "workspace already exists") {
		t.Errorf("error = %q, want 'workspace already exists'", err)
	}
}

func TestCreateWorkspace_NoSymlinks(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	wsPath, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace:  parent,
		TaskID:           "task-nosym",
		TaskDescription:  "Test no symlinks",
		AgentsDir:        agentsDir,
		IncludeHeartbeat: true,
		IncludeSkills:    true,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	// Check all created files are regular files, not symlinks.
	filesToCheck := []string{
		filepath.Join(wsPath, "AGENT.md"),
		filepath.Join(wsPath, "SOUL.md"),
		filepath.Join(wsPath, "HEARTBEAT.md"),
	}
	for _, skill := range parent.Skills {
		filesToCheck = append(filesToCheck, filepath.Join(wsPath, "skills", skill.Name, "SKILL.md"))
	}

	for _, f := range filesToCheck {
		info, err := os.Lstat(f)
		if err != nil {
			t.Fatalf("Lstat %s: %v", f, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is a symlink", f)
		}
	}
}

func TestCreateWorkspace_PathTraversal(t *testing.T) {
	parent := testParentWorkspace(t)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	traversalIDs := []string{"../escape", "../../etc", "foo/../../escape", "../"}
	for _, id := range traversalIDs {
		_, err := CreateWorkspace(WorkspaceConfig{
			ParentWorkspace: parent,
			TaskID:          id,
			TaskDescription: "Malicious task",
			AgentsDir:       agentsDir,
		})
		if err == nil {
			t.Errorf("expected error for traversal TaskID %q", id)
		}
		if !strings.Contains(err.Error(), "invalid task ID") {
			t.Errorf("TaskID %q: error = %q, want 'invalid task ID'", id, err)
		}
	}
}

func TestCreateWorkspace_StatError(t *testing.T) {
	origStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return nil, errors.New("injected stat error")
	}
	t.Cleanup(func() { osStat = origStat })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "check workspace path") {
		t.Fatalf("expected 'check workspace path' error, got: %v", err)
	}
}

func TestCreateWorkspace_EmptyAgentsDir(t *testing.T) {
	parent := testParentWorkspace(t)

	_, err := CreateWorkspace(WorkspaceConfig{
		ParentWorkspace: parent,
		TaskID:          "task-x",
		TaskDescription: "Some task",
		AgentsDir:       "",
	})
	if err == nil {
		t.Fatal("expected error for empty AgentsDir")
	}
	if !strings.Contains(err.Error(), "agents directory is required") {
		t.Errorf("error = %q, want 'agents directory is required'", err)
	}
}

// validConfig returns a WorkspaceConfig that will succeed, for use in error injection tests.
func validConfig(t *testing.T) WorkspaceConfig {
	t.Helper()
	return WorkspaceConfig{
		ParentWorkspace:  testParentWorkspace(t),
		TaskID:           "task-err",
		TaskDescription:  "Error test",
		AgentsDir:        filepath.Join(t.TempDir(), "agents"),
		IncludeHeartbeat: true,
		IncludeSkills:    true,
	}
}

func TestCreateWorkspace_AgentsDirError(t *testing.T) {
	origMkdir := mkdirAll
	mkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("injected mkdir error")
	}
	t.Cleanup(func() { mkdirAll = origMkdir })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "create agents dir") {
		t.Fatalf("expected 'create agents dir' error, got: %v", err)
	}
}

func TestCreateWorkspace_WorkspaceDirError(t *testing.T) {
	var callCount int64
	origMkdir := mkdirAll
	mkdirAll = func(path string, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		if n == 2 { // Second call is workspace dir creation
			return errors.New("injected workspace dir error")
		}
		return origMkdir(path, perm)
	}
	t.Cleanup(func() { mkdirAll = origMkdir })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "create workspace dir") {
		t.Fatalf("expected 'create workspace dir' error, got: %v", err)
	}
}

func TestCreateWorkspace_WriteAgentMDError(t *testing.T) {
	var callCount int64
	origWrite := atomicWrite
	atomicWrite = func(path string, data []byte, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		if n == 1 { // First write is AGENT.md
			return errors.New("injected write error")
		}
		return origWrite(path, data, perm)
	}
	t.Cleanup(func() { atomicWrite = origWrite })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "write AGENT.md") {
		t.Fatalf("expected 'write AGENT.md' error, got: %v", err)
	}
}

func TestCreateWorkspace_WriteSoulMDError(t *testing.T) {
	var callCount int64
	origWrite := atomicWrite
	atomicWrite = func(path string, data []byte, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		if n == 2 { // Second write is SOUL.md
			return errors.New("injected write error")
		}
		return origWrite(path, data, perm)
	}
	t.Cleanup(func() { atomicWrite = origWrite })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "write SOUL.md") {
		t.Fatalf("expected 'write SOUL.md' error, got: %v", err)
	}
}

func TestCreateWorkspace_WriteHeartbeatMDError(t *testing.T) {
	var callCount int64
	origWrite := atomicWrite
	atomicWrite = func(path string, data []byte, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		if n == 3 { // Third write is HEARTBEAT.md
			return errors.New("injected write error")
		}
		return origWrite(path, data, perm)
	}
	t.Cleanup(func() { atomicWrite = origWrite })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "write HEARTBEAT.md") {
		t.Fatalf("expected 'write HEARTBEAT.md' error, got: %v", err)
	}
}

func TestCreateWorkspace_SkillDirError(t *testing.T) {
	var callCount int64
	origMkdir := mkdirAll
	mkdirAll = func(path string, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		if n == 3 { // Third mkdir is first skill dir (after agents dir + workspace dir)
			return errors.New("injected skill dir error")
		}
		return origMkdir(path, perm)
	}
	t.Cleanup(func() { mkdirAll = origMkdir })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "create skill dir") {
		t.Fatalf("expected 'create skill dir' error, got: %v", err)
	}
}

func TestCreateWorkspace_WriteSkillError(t *testing.T) {
	var callCount int64
	origWrite := atomicWrite
	atomicWrite = func(path string, data []byte, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		if n == 4 { // Fourth write is first skill file (AGENT.md, SOUL.md, HEARTBEAT.md, then skill)
			return errors.New("injected skill write error")
		}
		return origWrite(path, data, perm)
	}
	t.Cleanup(func() { atomicWrite = origWrite })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "write skill") {
		t.Fatalf("expected 'write skill' error, got: %v", err)
	}
}

func TestCreateWorkspace_MemoryDirError(t *testing.T) {
	var callCount int64
	origMkdir := mkdirAll
	mkdirAll = func(path string, perm os.FileMode) error {
		n := atomic.AddInt64(&callCount, 1)
		// agents dir (1) + workspace dir (2) + 2 skill dirs (3, 4) + memory (5)
		if n == 5 {
			return errors.New("injected memory dir error")
		}
		return origMkdir(path, perm)
	}
	t.Cleanup(func() { mkdirAll = origMkdir })

	cfg := validConfig(t)
	_, err := CreateWorkspace(cfg)
	if err == nil || !strings.Contains(err.Error(), "create memory dir") {
		t.Fatalf("expected 'create memory dir' error, got: %v", err)
	}
}
