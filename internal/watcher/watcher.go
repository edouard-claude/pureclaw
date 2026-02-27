package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Watcher polls workspace files for mtime changes and signals on a channel.
type Watcher struct {
	root     string
	interval time.Duration
	mtimes   map[string]time.Time
}

// New creates a Watcher that polls files under root at the given interval.
func New(root string, interval time.Duration) *Watcher {
	return &Watcher{
		root:     root,
		interval: interval,
		mtimes:   make(map[string]time.Time),
	}
}

// Run polls workspace files at the configured interval, sending a signal on
// changes whenever any file mtime differs from the last snapshot. It blocks
// until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context, changes chan<- struct{}) {
	slog.Info("watcher started",
		"component", "watcher",
		"operation", "run",
		"root", w.root,
		"interval", w.interval,
	)

	// Initial snapshot — no event emitted.
	w.mtimes = w.snapshot()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("watcher stopped",
				"component", "watcher",
				"operation", "run",
			)
			return
		case <-ticker.C:
			w.poll(changes)
		}
	}
}

// poll compares current mtimes with stored state and sends a single event if
// any file changed, appeared, or disappeared.
func (w *Watcher) poll(changes chan<- struct{}) {
	current := w.snapshot()

	var changedFile string

	// Check for changed or disappeared files.
	for path, oldTime := range w.mtimes {
		newTime, exists := current[path]
		if !exists || !newTime.Equal(oldTime) {
			changedFile = path
			break
		}
	}

	// Check for new files (present in current but not in old).
	if changedFile == "" {
		for path := range current {
			if _, exists := w.mtimes[path]; !exists {
				changedFile = path
				break
			}
		}
	}

	if changedFile != "" {
		slog.Info("workspace file change detected",
			"component", "watcher",
			"operation", "detect_change",
			"file", changedFile,
		)
		// Non-blocking send: skip if channel already has a pending event.
		select {
		case changes <- struct{}{}:
		default:
		}
	}

	w.mtimes = current
}

// snapshot builds a map of watched file paths to their modification times.
// Missing files are silently excluded (HEARTBEAT.md and skills/ are optional).
// Non-existence errors are expected; other errors are logged.
func (w *Watcher) snapshot() map[string]time.Time {
	mtimes := make(map[string]time.Time)

	// Fixed workspace files.
	for _, name := range []string{"AGENT.md", "SOUL.md", "HEARTBEAT.md"} {
		p := filepath.Join(w.root, name)
		info, err := os.Stat(p)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("failed to stat workspace file",
					"component", "watcher",
					"operation", "snapshot",
					"file", p,
					"error", err,
				)
			}
			continue
		}
		mtimes[p] = info.ModTime()
	}

	// Dynamic skill files: skills/*/SKILL.md.
	skillsDir := filepath.Join(w.root, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read skills directory",
				"component", "watcher",
				"operation", "snapshot",
				"path", skillsDir,
				"error", err,
			)
		}
		return mtimes
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		mtimes[p] = info.ModTime()
	}

	return mtimes
}
