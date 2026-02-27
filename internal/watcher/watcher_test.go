package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testInterval = 50 * time.Millisecond

// setupWorkspace creates a minimal workspace with AGENT.md and SOUL.md.
func setupWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENT.md"), "agent content")
	writeFile(t, filepath.Join(root, "SOUL.md"), "soul content")
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNew(t *testing.T) {
	w := New("/tmp/ws", 2*time.Second)
	if w.root != "/tmp/ws" {
		t.Errorf("expected root %q, got %q", "/tmp/ws", w.root)
	}
	if w.interval != 2*time.Second {
		t.Errorf("expected interval %v, got %v", 2*time.Second, w.interval)
	}
	if w.mtimes == nil {
		t.Error("expected mtimes map to be initialized")
	}
}

func TestRun_DetectsChange(t *testing.T) {
	root := setupWorkspace(t)
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait for initial snapshot to be taken.
	time.Sleep(testInterval / 2)

	// Modify AGENT.md — ensure mtime changes (sleep 10ms for filesystem granularity).
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(root, "AGENT.md"), "updated agent content")

	select {
	case <-changes:
		// Success.
	case <-time.After(5 * testInterval):
		t.Fatal("expected change event after modifying AGENT.md")
	}
}

func TestRun_NoChangeNoEvent(t *testing.T) {
	root := setupWorkspace(t)
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait several poll cycles — no changes should be detected.
	time.Sleep(5 * testInterval)

	select {
	case <-changes:
		t.Fatal("expected no change event when files are unchanged")
	default:
		// Success.
	}
}

func TestRun_MissingOptionalFiles(t *testing.T) {
	root := setupWorkspace(t)
	// No HEARTBEAT.md, no skills/ directory — should not error.
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait for a few poll cycles — watcher should run without errors.
	time.Sleep(3 * testInterval)

	select {
	case <-changes:
		t.Fatal("expected no change event with only required files present")
	default:
		// Success — watcher runs fine without optional files.
	}
}

func TestRun_NewSkillAppears(t *testing.T) {
	root := setupWorkspace(t)
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait for initial snapshot.
	time.Sleep(testInterval / 2)

	// Create a new skill.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(root, "skills", "greeting", "SKILL.md"), "greeting skill")

	select {
	case <-changes:
		// Success.
	case <-time.After(5 * testInterval):
		t.Fatal("expected change event after adding a new skill")
	}
}

func TestRun_SkillRemoved(t *testing.T) {
	root := setupWorkspace(t)
	skillPath := filepath.Join(root, "skills", "greeting", "SKILL.md")
	writeFile(t, skillPath, "greeting skill")

	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait for initial snapshot.
	time.Sleep(testInterval / 2)

	// Remove the skill directory.
	time.Sleep(10 * time.Millisecond)
	if err := os.RemoveAll(filepath.Join(root, "skills", "greeting")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-changes:
		// Success.
	case <-time.After(5 * testInterval):
		t.Fatal("expected change event after removing a skill")
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	root := setupWorkspace(t)
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx, changes)
		close(done)
	}()

	// Let it run briefly, then cancel.
	time.Sleep(testInterval / 2)
	cancel()

	select {
	case <-done:
		// Success — Run returned.
	case <-time.After(time.Second):
		t.Fatal("expected Run() to return on context cancellation")
	}
}

func TestRun_CoalescesMultipleChanges(t *testing.T) {
	root := setupWorkspace(t)
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait for initial snapshot.
	time.Sleep(testInterval / 2)

	// Modify both files before the next poll cycle.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(root, "AGENT.md"), "updated agent")
	writeFile(t, filepath.Join(root, "SOUL.md"), "updated soul")

	// Should receive exactly one event.
	select {
	case <-changes:
		// Success — first event received.
	case <-time.After(5 * testInterval):
		t.Fatal("expected change event")
	}

	// Wait a poll cycle — no further events since no new changes.
	time.Sleep(3 * testInterval)
	select {
	case <-changes:
		t.Fatal("expected only one coalesced event, got a second")
	default:
		// Success.
	}
}

func TestRun_BufferedChannelNonBlocking(t *testing.T) {
	root := setupWorkspace(t)
	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-fill the channel buffer.
	changes <- struct{}{}

	go w.Run(ctx, changes)

	// Wait for initial snapshot.
	time.Sleep(testInterval / 2)

	// Modify a file — watcher should not block because channel is already full.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(root, "AGENT.md"), "modified content")

	// Wait several cycles — watcher should continue running without blocking.
	time.Sleep(5 * testInterval)

	// Drain the pre-filled event.
	<-changes

	// Modify again — watcher should detect the new change.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(root, "AGENT.md"), "modified again")

	select {
	case <-changes:
		// Success — watcher still works after channel was full.
	case <-time.After(5 * testInterval):
		t.Fatal("expected watcher to recover and detect subsequent change")
	}
}

func TestRun_SkillDirWithoutSkillMD(t *testing.T) {
	root := setupWorkspace(t)
	// Create a skill directory without SKILL.md inside it.
	emptySkillDir := filepath.Join(root, "skills", "broken-skill")
	if err := os.MkdirAll(emptySkillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait several poll cycles — no change event expected (dir exists but no SKILL.md).
	time.Sleep(3 * testInterval)

	select {
	case <-changes:
		t.Fatal("expected no change event for skill dir without SKILL.md")
	default:
		// Success — missing SKILL.md is silently ignored.
	}
}

func TestRun_SkillsNonDirectoryIgnored(t *testing.T) {
	root := setupWorkspace(t)
	// Create a regular file (not a directory) inside skills/.
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillsDir, "README.md"), "not a skill dir")

	w := New(root, testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx, changes)

	// Wait several poll cycles — no change event expected.
	time.Sleep(3 * testInterval)

	select {
	case <-changes:
		t.Fatal("expected no change event for non-directory entry in skills/")
	default:
		// Success — non-directory entries are silently ignored.
	}
}

func TestSnapshot_StatPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}
	root := setupWorkspace(t)

	// Remove execute permission from root so os.Stat on children returns EACCES.
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755) // Restore for t.TempDir cleanup.

	w := New(root, testInterval)
	snap := w.snapshot()
	if len(snap) != 0 {
		t.Errorf("expected empty snapshot with permission denied, got %d entries", len(snap))
	}
}

func TestSnapshot_SkillsDirPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: test requires non-root user")
	}
	root := setupWorkspace(t)
	skillsDir := filepath.Join(root, "skills")
	writeFile(t, filepath.Join(skillsDir, "greeting", "SKILL.md"), "skill content")

	// Remove read permission from skills/ so os.ReadDir returns EACCES.
	if err := os.Chmod(skillsDir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(skillsDir, 0o755) // Restore for t.TempDir cleanup.

	w := New(root, testInterval)
	snap := w.snapshot()
	// Should still have the fixed workspace files (AGENT.md, SOUL.md) but no skills.
	if len(snap) != 2 {
		t.Errorf("expected 2 entries (AGENT.md + SOUL.md), got %d", len(snap))
	}
}

func TestRun_WorkspaceRootNotFound(t *testing.T) {
	w := New("/nonexistent/workspace/path", testInterval)
	changes := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx, changes)
		close(done)
	}()

	// Wait a few cycles — watcher should run without crashing.
	time.Sleep(3 * testInterval)

	cancel()

	select {
	case <-done:
		// Success — Run returned cleanly.
	case <-time.After(time.Second):
		t.Fatal("expected Run() to return on context cancellation with nonexistent root")
	}

	// No events should have been emitted (initial snapshot is empty, stays empty).
	select {
	case <-changes:
		t.Fatal("expected no events for nonexistent workspace root")
	default:
		// Success.
	}
}
