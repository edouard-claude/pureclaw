package memory

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/edouard/pureclaw/internal/platform"
)

// Replaceable for testing.
var timeNow = time.Now

// Memory handles writing entries to hourly memory files.
type Memory struct {
	root string // workspace root path
}

// New creates a Memory writer rooted at the given workspace path.
func New(root string) *Memory {
	return &Memory{root: root}
}

// Write appends an entry to the current hourly memory file.
// Format: ---\n**YYYY-MM-DD HH:MM** — source\ncontent\n\n
func (m *Memory) Write(ctx context.Context, source, content string) error {
	now := timeNow()
	path := m.hourlyPath(now)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}

	existing, _ := os.ReadFile(path) // ignore error — file may not exist yet

	entry := fmt.Sprintf("---\n**%s** — %s\n%s\n\n",
		now.Format("2006-01-02 15:04"),
		source,
		content,
	)

	data := append(existing, []byte(entry)...)
	if err := platform.AtomicWrite(path, data, 0o644); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}

	slog.Info("memory entry written",
		"component", "memory",
		"operation", "write",
		"source", source,
		"path", path,
	)
	return nil
}

// hourlyPath returns the file path for the hourly memory file at time t.
func (m *Memory) hourlyPath(t time.Time) string {
	return filepath.Join(m.root, "memory",
		t.Format("2006"),
		t.Format("01"),
		t.Format("02"),
		t.Format("15")+".md",
	)
}
