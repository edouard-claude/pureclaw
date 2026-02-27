package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Workspace holds the loaded contents of a pureclaw workspace directory.
type Workspace struct {
	Root        string
	AgentMD     string
	SoulMD      string
	HeartbeatMD string
	Skills      []Skill
}

// Skill represents a single skill definition loaded from skills/*/SKILL.md.
type Skill struct {
	Name    string
	Content string
}

// Load reads a workspace directory and returns a populated Workspace.
// AGENT.md and SOUL.md are required; HEARTBEAT.md and skills/ are optional.
func Load(root string) (*Workspace, error) {
	slog.Info("loading workspace",
		"component", "workspace",
		"operation", "load",
		"root", root)

	w := &Workspace{Root: root}

	// Required files — error if missing
	agentData, err := os.ReadFile(filepath.Join(root, "AGENT.md"))
	if err != nil {
		return nil, fmt.Errorf("workspace: load AGENT.md: %w", err)
	}
	w.AgentMD = string(agentData)

	soulData, err := os.ReadFile(filepath.Join(root, "SOUL.md"))
	if err != nil {
		return nil, fmt.Errorf("workspace: load SOUL.md: %w", err)
	}
	w.SoulMD = string(soulData)

	// Optional files — skip if missing, warn if unreadable
	heartbeatData, err := os.ReadFile(filepath.Join(root, "HEARTBEAT.md"))
	if err == nil {
		w.HeartbeatMD = string(heartbeatData)
		slog.Debug("heartbeat file loaded",
			"component", "workspace",
			"operation", "load")
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("failed to read HEARTBEAT.md",
			"component", "workspace",
			"operation", "load",
			"error", err)
	}

	// Discover skills
	w.Skills, err = discoverSkills(filepath.Join(root, "skills"))
	if err != nil {
		slog.Warn("skills discovery skipped",
			"component", "workspace",
			"operation", "load",
			"error", err)
	}

	slog.Info("workspace loaded",
		"component", "workspace",
		"operation", "load",
		"root", root,
		"skills", len(w.Skills))

	return w, nil
}

// discoverSkills finds skill definitions in skillsDir/*/SKILL.md.
func discoverSkills(skillsDir string) ([]Skill, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}

	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			slog.Debug("skipping non-directory in skills",
				"component", "workspace",
				"operation", "discover_skills",
				"entry", entry.Name())
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("failed to read skill file",
					"component", "workspace",
					"operation", "discover_skills",
					"path", skillPath,
					"error", err)
			}
			continue
		}
		skills = append(skills, Skill{
			Name:    entry.Name(),
			Content: string(data),
		})
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	return skills, nil
}

// SystemPrompt assembles the system prompt from loaded workspace files.
// Order: soul → agent → skills.
func (w *Workspace) SystemPrompt() string {
	var b strings.Builder

	b.WriteString(w.SoulMD)
	b.WriteString("\n\n")

	b.WriteString(w.AgentMD)

	if len(w.Skills) > 0 {
		b.WriteString("\n\n## Available Skills\n\n")
		for _, s := range w.Skills {
			b.WriteString("### ")
			b.WriteString(s.Name)
			b.WriteString("\n\n")
			b.WriteString(s.Content)
			b.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(b.String())
}
