package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		wantErr   string
		setup     func(t *testing.T, dir string)
		checkFunc func(t *testing.T, w *Workspace)
	}{
		{
			name: "AllFiles",
			files: map[string]string{
				"AGENT.md":     "# Agent\n\nI am an agent.",
				"SOUL.md":      "# Soul\n\nBe helpful.",
				"HEARTBEAT.md": "# Heartbeat\n\n- [ ] Check disk",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if w.AgentMD != "# Agent\n\nI am an agent." {
					t.Errorf("AgentMD = %q", w.AgentMD)
				}
				if w.SoulMD != "# Soul\n\nBe helpful." {
					t.Errorf("SoulMD = %q", w.SoulMD)
				}
				if w.HeartbeatMD != "# Heartbeat\n\n- [ ] Check disk" {
					t.Errorf("HeartbeatMD = %q", w.HeartbeatMD)
				}
				if len(w.Skills) != 0 {
					t.Errorf("Skills = %v, want empty", w.Skills)
				}
			},
		},
		{
			name: "WithSkills",
			files: map[string]string{
				"AGENT.md":                "# Agent",
				"SOUL.md":                 "# Soul",
				"skills/coding/SKILL.md":  "Coding skill content",
				"skills/weather/SKILL.md": "Weather skill content",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if len(w.Skills) != 2 {
					t.Fatalf("Skills count = %d, want 2", len(w.Skills))
				}
				// Skills should be sorted alphabetically
				if w.Skills[0].Name != "coding" {
					t.Errorf("Skills[0].Name = %q, want coding", w.Skills[0].Name)
				}
				if w.Skills[0].Content != "Coding skill content" {
					t.Errorf("Skills[0].Content = %q", w.Skills[0].Content)
				}
				if w.Skills[1].Name != "weather" {
					t.Errorf("Skills[1].Name = %q, want weather", w.Skills[1].Name)
				}
				if w.Skills[1].Content != "Weather skill content" {
					t.Errorf("Skills[1].Content = %q", w.Skills[1].Content)
				}
			},
		},
		{
			name: "MissingAgentMD",
			files: map[string]string{
				"SOUL.md": "# Soul",
			},
			wantErr: "AGENT.md",
		},
		{
			name: "MissingSoulMD",
			files: map[string]string{
				"AGENT.md": "# Agent",
			},
			wantErr: "SOUL.md",
		},
		{
			name: "MissingHeartbeatMD",
			files: map[string]string{
				"AGENT.md": "# Agent",
				"SOUL.md":  "# Soul",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if w.HeartbeatMD != "" {
					t.Errorf("HeartbeatMD = %q, want empty", w.HeartbeatMD)
				}
			},
		},
		{
			name: "MissingSkillsDir",
			files: map[string]string{
				"AGENT.md": "# Agent",
				"SOUL.md":  "# Soul",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if w.Skills != nil {
					t.Errorf("Skills = %v, want nil", w.Skills)
				}
			},
		},
		{
			name: "EmptySkillsDir",
			files: map[string]string{
				"AGENT.md":          "# Agent",
				"SOUL.md":           "# Soul",
				"skills/.gitkeep":   "",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				// .gitkeep is a file, not a dir, so no skills discovered
				if w.Skills != nil {
					t.Errorf("Skills = %v, want nil", w.Skills)
				}
			},
		},
		{
			name: "SkillDirNoSkillMD",
			files: map[string]string{
				"AGENT.md":              "# Agent",
				"SOUL.md":               "# Soul",
				"skills/weather/README": "not a SKILL.md",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if w.Skills != nil {
					t.Errorf("Skills = %v, want nil", w.Skills)
				}
			},
		},
		{
			name: "MultipleSkills",
			files: map[string]string{
				"AGENT.md":                  "# Agent",
				"SOUL.md":                   "# Soul",
				"skills/alpha/SKILL.md":     "Alpha",
				"skills/beta/SKILL.md":      "Beta",
				"skills/gamma/SKILL.md":     "Gamma",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if len(w.Skills) != 3 {
					t.Fatalf("Skills count = %d, want 3", len(w.Skills))
				}
				if w.Skills[0].Name != "alpha" || w.Skills[1].Name != "beta" || w.Skills[2].Name != "gamma" {
					t.Errorf("Skills not sorted: %v", w.Skills)
				}
			},
		},
		{
			name: "NonDirInSkills",
			files: map[string]string{
				"AGENT.md":             "# Agent",
				"SOUL.md":              "# Soul",
				"skills/readme.txt":    "just a file",
				"skills/weather/SKILL.md": "Weather",
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				if len(w.Skills) != 1 {
					t.Fatalf("Skills count = %d, want 1", len(w.Skills))
				}
				if w.Skills[0].Name != "weather" {
					t.Errorf("Skills[0].Name = %q, want weather", w.Skills[0].Name)
				}
			},
		},
		{
			name:    "InvalidRoot",
			files:   map[string]string{},
			wantErr: "AGENT.md",
		},
		{
			name: "HeartbeatUnreadable",
			files: map[string]string{
				"AGENT.md":     "# Agent",
				"SOUL.md":      "# Soul",
				"HEARTBEAT.md": "# Heartbeat",
			},
			setup: func(t *testing.T, dir string) {
				// Make HEARTBEAT.md unreadable to test non-NotExist error path
				if err := os.Chmod(filepath.Join(dir, "HEARTBEAT.md"), 0000); err != nil {
					t.Fatal(err)
				}
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				// Should load successfully but with empty HeartbeatMD
				if w.HeartbeatMD != "" {
					t.Errorf("HeartbeatMD = %q, want empty (file was unreadable)", w.HeartbeatMD)
				}
			},
		},
		{
			name: "SkillFileUnreadable",
			files: map[string]string{
				"AGENT.md":                "# Agent",
				"SOUL.md":                 "# Soul",
				"skills/broken/SKILL.md":  "Broken skill",
				"skills/working/SKILL.md": "Working skill",
			},
			setup: func(t *testing.T, dir string) {
				// Make one SKILL.md unreadable
				if err := os.Chmod(filepath.Join(dir, "skills", "broken", "SKILL.md"), 0000); err != nil {
					t.Fatal(err)
				}
			},
			checkFunc: func(t *testing.T, w *Workspace) {
				// Only the readable skill should be discovered
				if len(w.Skills) != 1 {
					t.Fatalf("Skills count = %d, want 1", len(w.Skills))
				}
				if w.Skills[0].Name != "working" {
					t.Errorf("Skills[0].Name = %q, want working", w.Skills[0].Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dir string
			if tt.name == "InvalidRoot" {
				dir = filepath.Join(t.TempDir(), "nonexistent")
			} else {
				dir = setupTestWorkspace(t, tt.files)
			}

			if tt.setup != nil {
				tt.setup(t, dir)
			}

			w, err := Load(dir)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if w.Root != dir {
				t.Errorf("Root = %q, want %q", w.Root, dir)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, w)
			}
		})
	}
}

func TestSystemPrompt(t *testing.T) {
	tests := []struct {
		name      string
		workspace Workspace
		contains  []string
		notContains []string
		checkOrder [][2]string // [before, after] pairs
	}{
		{
			name: "NoSkills",
			workspace: Workspace{
				SoulMD:  "Be helpful.",
				AgentMD: "I am an agent.",
			},
			contains:    []string{"Be helpful.", "I am an agent."},
			notContains: []string{"Available Skills"},
		},
		{
			name: "WithSkills",
			workspace: Workspace{
				SoulMD:  "Be helpful.",
				AgentMD: "I am an agent.",
				Skills: []Skill{
					{Name: "coding", Content: "Coding instructions"},
					{Name: "weather", Content: "Weather instructions"},
				},
			},
			contains: []string{
				"Be helpful.",
				"I am an agent.",
				"## Available Skills",
				"### coding",
				"Coding instructions",
				"### weather",
				"Weather instructions",
			},
		},
		{
			name: "Order_SoulBeforeAgent",
			workspace: Workspace{
				SoulMD:  "SOUL_CONTENT",
				AgentMD: "AGENT_CONTENT",
			},
			checkOrder: [][2]string{
				{"SOUL_CONTENT", "AGENT_CONTENT"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.workspace.SystemPrompt()

			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("SystemPrompt() missing %q\ngot: %s", s, result)
				}
			}

			for _, s := range tt.notContains {
				if strings.Contains(result, s) {
					t.Errorf("SystemPrompt() should not contain %q\ngot: %s", s, result)
				}
			}

			for _, pair := range tt.checkOrder {
				before := strings.Index(result, pair[0])
				after := strings.Index(result, pair[1])
				if before == -1 || after == -1 {
					t.Errorf("missing content for order check: %q or %q", pair[0], pair[1])
				} else if before >= after {
					t.Errorf("%q should appear before %q in SystemPrompt()", pair[0], pair[1])
				}
			}
		})
	}
}
