package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock(year int, month time.Month, day, hour, min int) func() time.Time {
	return func() time.Time {
		return time.Date(year, month, day, hour, min, 0, 0, time.UTC)
	}
}

func TestWrite_NewFile(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })
	timeNow = fixedClock(2026, 3, 15, 14, 23)

	root := t.TempDir()
	m := New(root)

	if err := m.Write(context.Background(), "owner", "Hello agent"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(root, "memory", "2026", "03", "15", "14.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	expected := "---\n**2026-03-15 14:23** — owner\nHello agent\n\n"
	if string(data) != expected {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), expected)
	}
}

func TestWrite_AppendExisting(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })

	root := t.TempDir()
	m := New(root)

	// First write at 14:23.
	timeNow = fixedClock(2026, 3, 15, 14, 23)
	if err := m.Write(context.Background(), "owner", "First"); err != nil {
		t.Fatalf("Write 1: %v", err)
	}

	// Second write at 14:45 (same hour).
	timeNow = fixedClock(2026, 3, 15, 14, 45)
	if err := m.Write(context.Background(), "agent", "Second"); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	path := filepath.Join(root, "memory", "2026", "03", "15", "14.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	want := "---\n**2026-03-15 14:23** — owner\nFirst\n\n" +
		"---\n**2026-03-15 14:45** — agent\nSecond\n\n"
	if string(data) != want {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), want)
	}
}

func TestWrite_CreatesDirectories(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })
	timeNow = fixedClock(2026, 12, 31, 23, 59)

	root := t.TempDir()
	m := New(root)

	if err := m.Write(context.Background(), "owner", "End of year"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	dirPath := filepath.Join(root, "memory", "2026", "12", "31")
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory at %s", dirPath)
	}
}

func TestWrite_HourlyRotation(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })

	root := t.TempDir()
	m := New(root)

	// Write at hour 14.
	timeNow = fixedClock(2026, 3, 15, 14, 30)
	if err := m.Write(context.Background(), "owner", "Hour 14"); err != nil {
		t.Fatalf("Write hour 14: %v", err)
	}

	// Write at hour 15.
	timeNow = fixedClock(2026, 3, 15, 15, 5)
	if err := m.Write(context.Background(), "owner", "Hour 15"); err != nil {
		t.Fatalf("Write hour 15: %v", err)
	}

	// Verify separate files exist.
	file14 := filepath.Join(root, "memory", "2026", "03", "15", "14.md")
	file15 := filepath.Join(root, "memory", "2026", "03", "15", "15.md")

	data14, err := os.ReadFile(file14)
	if err != nil {
		t.Fatalf("ReadFile 14.md: %v", err)
	}
	if !strings.Contains(string(data14), "Hour 14") {
		t.Errorf("14.md missing expected content")
	}

	data15, err := os.ReadFile(file15)
	if err != nil {
		t.Fatalf("ReadFile 15.md: %v", err)
	}
	if !strings.Contains(string(data15), "Hour 15") {
		t.Errorf("15.md missing expected content")
	}
}

func TestWrite_DayRotation(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })

	root := t.TempDir()
	m := New(root)

	timeNow = fixedClock(2026, 3, 15, 10, 0)
	if err := m.Write(context.Background(), "owner", "Day 15"); err != nil {
		t.Fatalf("Write day 15: %v", err)
	}

	timeNow = fixedClock(2026, 3, 16, 10, 0)
	if err := m.Write(context.Background(), "owner", "Day 16"); err != nil {
		t.Fatalf("Write day 16: %v", err)
	}

	file15 := filepath.Join(root, "memory", "2026", "03", "15", "10.md")
	file16 := filepath.Join(root, "memory", "2026", "03", "16", "10.md")

	if _, err := os.Stat(file15); err != nil {
		t.Errorf("expected file at %s", file15)
	}
	if _, err := os.Stat(file16); err != nil {
		t.Errorf("expected file at %s", file16)
	}
}

func TestWrite_EntryFormat(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })
	timeNow = fixedClock(2026, 1, 2, 3, 4)

	root := t.TempDir()
	m := New(root)

	if err := m.Write(context.Background(), "heartbeat", "Disk at 42%"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(root, "memory", "2026", "01", "02", "03.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	expected := "---\n**2026-01-02 03:04** — heartbeat\nDisk at 42%\n\n"
	if string(data) != expected {
		t.Errorf("format mismatch:\ngot:  %q\nwant: %q", string(data), expected)
	}
}

func TestWrite_EmptyContent(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })
	timeNow = fixedClock(2026, 3, 15, 14, 0)

	root := t.TempDir()
	m := New(root)

	if err := m.Write(context.Background(), "owner", ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(root, "memory", "2026", "03", "15", "14.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	expected := "---\n**2026-03-15 14:00** — owner\n\n\n"
	if string(data) != expected {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), expected)
	}
}

func TestWrite_MkdirError(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })
	timeNow = fixedClock(2026, 3, 15, 14, 0)

	// Use a file as root so MkdirAll fails.
	tmpDir := t.TempDir()
	blockerPath := filepath.Join(tmpDir, "blocker")
	if err := os.WriteFile(blockerPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("create blocker: %v", err)
	}

	m := New(blockerPath)
	err := m.Write(context.Background(), "owner", "test")
	if err == nil {
		t.Fatal("expected error from MkdirAll, got nil")
	}
	if !strings.Contains(err.Error(), "memory: mkdir:") {
		t.Errorf("expected 'memory: mkdir:' in error, got %q", err.Error())
	}
}

func TestWrite_AtomicWriteError(t *testing.T) {
	origTimeNow := timeNow
	t.Cleanup(func() { timeNow = origTimeNow })
	timeNow = fixedClock(2026, 3, 15, 14, 0)

	// Create a read-only directory so AtomicWrite (temp file creation) fails.
	root := t.TempDir()
	memDir := filepath.Join(root, "memory", "2026", "03", "15")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Make directory read-only to prevent temp file creation.
	if err := os.Chmod(memDir, 0o444); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(memDir, 0o755) })

	m := New(root)
	err := m.Write(context.Background(), "owner", "test")
	if err == nil {
		t.Fatal("expected error from AtomicWrite, got nil")
	}
	if !strings.Contains(err.Error(), "memory: write:") {
		t.Errorf("expected 'memory: write:' in error, got %q", err.Error())
	}
}

func TestHourlyPath_Format(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{
			name: "standard time",
			time: time.Date(2026, 3, 15, 14, 23, 0, 0, time.UTC),
			want: filepath.Join("root", "memory", "2026", "03", "15", "14.md"),
		},
		{
			name: "midnight",
			time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: filepath.Join("root", "memory", "2026", "01", "01", "00.md"),
		},
		{
			name: "end of day",
			time: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			want: filepath.Join("root", "memory", "2026", "12", "31", "23.md"),
		},
		{
			name: "single digit month and day",
			time: time.Date(2026, 2, 5, 9, 0, 0, 0, time.UTC),
			want: filepath.Join("root", "memory", "2026", "02", "05", "09.md"),
		},
	}

	m := New("root")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hourlyPath(tt.time)
			if got != tt.want {
				t.Errorf("hourlyPath(%v) = %q, want %q", tt.time, got, tt.want)
			}
		})
	}
}
