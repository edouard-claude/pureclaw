package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRawMemoryFile writes raw content to the expected hourly memory file path.
func writeRawMemoryFile(t *testing.T, root string, ts time.Time, content string) string {
	t.Helper()
	m := New(root)
	path := m.hourlyPath(ts)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestSearch_SingleMatch(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nHello agent\n\n"+
			"---\n**2026-03-15 14:20** — agent\nWeather is sunny\n\n"+
			"---\n**2026-03-15 14:30** — owner\nThanks\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "sunny", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Source != "agent" {
		t.Errorf("expected source 'agent', got %q", results[0].Source)
	}
	if results[0].Content != "Weather is sunny" {
		t.Errorf("expected content 'Weather is sunny', got %q", results[0].Content)
	}
}

func TestSearch_MultipleMatchesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	ts1 := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	ts3 := time.Date(2026, 3, 15, 18, 0, 0, 0, time.UTC)

	writeRawMemoryFile(t, root, ts1,
		"---\n**2026-03-15 10:05** — owner\nDisk check please\n\n")
	writeRawMemoryFile(t, root, ts2,
		"---\n**2026-03-15 14:10** — agent\nNo disk issues found\n\n")
	writeRawMemoryFile(t, root, ts3,
		"---\n**2026-03-15 18:00** — heartbeat\nDisk usage at 42%\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "disk", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Verify chronological order.
	if results[0].Time.After(results[1].Time) || results[1].Time.After(results[2].Time) {
		t.Error("results not in chronological order")
	}
}

func TestSearch_NoMatches(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nHello agent\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "nonexistent", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_DateRangeFiltering(t *testing.T) {
	root := t.TempDir()
	// Create files for 3 days.
	day1 := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	day3 := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)

	writeRawMemoryFile(t, root, day1,
		"---\n**2026-03-14 10:00** — owner\nHello day1\n\n")
	writeRawMemoryFile(t, root, day2,
		"---\n**2026-03-15 10:00** — owner\nHello day2\n\n")
	writeRawMemoryFile(t, root, day3,
		"---\n**2026-03-16 10:00** — owner\nHello day3\n\n")

	m := New(root)
	// Only search day 2.
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "Hello", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (day2 only), got %d", len(results))
	}
	if results[0].Content != "Hello day2" {
		t.Errorf("expected 'Hello day2', got %q", results[0].Content)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nHello World\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "hello world", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for case-insensitive match, got %d", len(results))
	}
}

func TestSearch_EmptyKeyword(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nFirst\n\n"+
			"---\n**2026-03-15 14:20** — agent\nSecond\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for empty keyword, got %d", len(results))
	}
}

func TestReadRange_AllEntries(t *testing.T) {
	root := t.TempDir()
	ts1 := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)

	writeRawMemoryFile(t, root, ts1,
		"---\n**2026-03-15 10:05** — owner\nMorning\n\n"+
			"---\n**2026-03-15 10:15** — agent\nGood morning\n\n")
	writeRawMemoryFile(t, root, ts2,
		"---\n**2026-03-15 14:10** — owner\nAfternoon\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)

	results, err := m.ReadRange(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Verify chronological order.
	for i := 1; i < len(results); i++ {
		if results[i].Time.Before(results[i-1].Time) {
			t.Errorf("results not in chronological order at index %d", i)
		}
	}
}

func TestReadRange_AcrossDayBoundary(t *testing.T) {
	root := t.TempDir()
	ts1 := time.Date(2026, 3, 15, 23, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)

	writeRawMemoryFile(t, root, ts1,
		"---\n**2026-03-15 23:30** — owner\nLate night\n\n")
	writeRawMemoryFile(t, root, ts2,
		"---\n**2026-03-16 00:15** — agent\nEarly morning\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 23, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 16, 1, 0, 0, 0, time.UTC)

	results, err := m.ReadRange(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results across day boundary, got %d", len(results))
	}
	if results[0].Content != "Late night" {
		t.Errorf("expected 'Late night', got %q", results[0].Content)
	}
	if results[1].Content != "Early morning" {
		t.Errorf("expected 'Early morning', got %q", results[1].Content)
	}
}

func TestReadRange_AcrossMonthBoundary(t *testing.T) {
	root := t.TempDir()
	ts1 := time.Date(2026, 2, 28, 23, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	writeRawMemoryFile(t, root, ts1,
		"---\n**2026-02-28 23:45** — owner\nEnd of February\n\n")
	writeRawMemoryFile(t, root, ts2,
		"---\n**2026-03-01 00:10** — agent\nStart of March\n\n")

	m := New(root)
	start := time.Date(2026, 2, 28, 23, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC)

	results, err := m.ReadRange(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results across month boundary, got %d", len(results))
	}
	if results[0].Content != "End of February" {
		t.Errorf("expected 'End of February', got %q", results[0].Content)
	}
	if results[1].Content != "Start of March" {
		t.Errorf("expected 'Start of March', got %q", results[1].Content)
	}
}

func TestReadRange_MissingHours(t *testing.T) {
	root := t.TempDir()
	// Only create files for hours 08, 14, 22 — gaps in between.
	writeRawMemoryFile(t, root, time.Date(2026, 3, 15, 8, 0, 0, 0, time.UTC),
		"---\n**2026-03-15 08:00** — owner\nMorning check\n\n")
	writeRawMemoryFile(t, root, time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC),
		"---\n**2026-03-15 14:00** — owner\nAfternoon check\n\n")
	writeRawMemoryFile(t, root, time.Date(2026, 3, 15, 22, 0, 0, 0, time.UTC),
		"---\n**2026-03-15 22:00** — owner\nEvening check\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)

	results, err := m.ReadRange(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results from existing files only, got %d", len(results))
	}
}

func TestReadRange_EmptyRange(t *testing.T) {
	root := t.TempDir()
	// Create file outside the search range.
	writeRawMemoryFile(t, root, time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		"---\n**2026-03-15 10:00** — owner\nEntry\n\n")

	m := New(root)
	// Search a range with no files.
	start := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 20, 23, 59, 0, 0, time.UTC)

	results, err := m.ReadRange(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty range, got %d", len(results))
	}
}

func TestReadRange_EmptyMemoryDir(t *testing.T) {
	root := t.TempDir()
	// No memory directory at all.
	m := New(root)
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)

	results, err := m.ReadRange(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty memory dir, got %d", len(results))
	}
}

func TestListFiles_CorrectRange(t *testing.T) {
	root := t.TempDir()
	// Create files spanning 48 hours.
	base := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 48; h++ {
		ts := base.Add(time.Duration(h) * time.Hour)
		writeRawMemoryFile(t, root, ts, "---\n**"+ts.Format("2006-01-02 15:04")+"** — owner\nEntry\n\n")
	}

	m := New(root)
	// Only request 12 hours.
	start := time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 17, 59, 0, 0, time.UTC)

	files := m.listFiles(start, end)
	if len(files) != 12 {
		t.Fatalf("expected 12 files, got %d", len(files))
	}
}

func TestListFiles_EmptyDir(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)

	files := m.listFiles(start, end)
	if len(files) != 0 {
		t.Fatalf("expected 0 files for empty dir, got %d", len(files))
	}
}

func TestListFiles_SkipsOutOfRange(t *testing.T) {
	root := t.TempDir()
	// Create file at hour 10.
	writeRawMemoryFile(t, root, time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		"---\n**2026-03-15 10:00** — owner\nEntry\n\n")

	m := New(root)
	// Search range that doesn't include hour 10.
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 18, 0, 0, 0, time.UTC)

	files := m.listFiles(start, end)
	if len(files) != 0 {
		t.Fatalf("expected 0 files (out of range), got %d", len(files))
	}
}

func TestParseFile_CorrectFormat(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	path := writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nFirst entry\n\n"+
			"---\n**2026-03-15 14:20** — agent\nSecond entry\n\n"+
			"---\n**2026-03-15 14:30** — heartbeat\nThird entry\n\n")

	m := New(root)
	results, err := m.parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(results))
	}

	// Verify first entry.
	if results[0].Source != "owner" {
		t.Errorf("entry[0] source = %q, want 'owner'", results[0].Source)
	}
	if results[0].Content != "First entry" {
		t.Errorf("entry[0] content = %q, want 'First entry'", results[0].Content)
	}
	expectedTime := time.Date(2026, 3, 15, 14, 10, 0, 0, time.UTC)
	if !results[0].Time.Equal(expectedTime) {
		t.Errorf("entry[0] time = %v, want %v", results[0].Time, expectedTime)
	}

	// Verify second entry.
	if results[1].Source != "agent" {
		t.Errorf("entry[1] source = %q, want 'agent'", results[1].Source)
	}

	// Verify third entry.
	if results[2].Source != "heartbeat" {
		t.Errorf("entry[2] source = %q, want 'heartbeat'", results[2].Source)
	}
}

func TestParseFile_MalformedEntry(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	// One good entry + one malformed (no " — " separator).
	path := writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nGood entry\n\n"+
			"---\nBad header without separator\n\n")

	m := New(root)
	results, err := m.parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry (malformed skipped), got %d", len(results))
	}
	if results[0].Content != "Good entry" {
		t.Errorf("expected 'Good entry', got %q", results[0].Content)
	}
}

func TestParseFile_MalformedTimestamp(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	// Entry with bad timestamp format.
	path := writeRawMemoryFile(t, root, ts,
		"---\n**not-a-date** — owner\nContent\n\n"+
			"---\n**2026-03-15 14:10** — agent\nGood entry\n\n")

	m := New(root)
	results, err := m.parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry (bad timestamp skipped), got %d", len(results))
	}
	if results[0].Source != "agent" {
		t.Errorf("expected source 'agent', got %q", results[0].Source)
	}
}

func TestParseFile_EmptyFile(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	path := writeRawMemoryFile(t, root, ts, "")

	m := New(root)
	results, err := m.parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 entries for empty file, got %d", len(results))
	}
}

func TestParseFile_FileNotFound(t *testing.T) {
	root := t.TempDir()
	m := New(root)
	_, err := m.parseFile(filepath.Join(root, "nonexistent.md"))
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestSearch_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	// Create multiple files so the loop iterates.
	for h := 0; h < 5; h++ {
		ts := time.Date(2026, 3, 15, h, 0, 0, 0, time.UTC)
		writeRawMemoryFile(t, root, ts,
			"---\n**"+ts.Format("2006-01-02 15:04")+"** — owner\nEntry\n\n")
	}

	m := New(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	start := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 4, 59, 0, 0, time.UTC)

	_, err := m.Search(ctx, "Entry", start, end)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected 'context canceled' in error, got %q", err.Error())
	}
}

func TestSearch_LargeRange(t *testing.T) {
	root := t.TempDir()
	// Create files across 7 days (one per day at noon).
	for d := 0; d < 7; d++ {
		ts := time.Date(2026, 3, 10+d, 12, 0, 0, 0, time.UTC)
		writeRawMemoryFile(t, root, ts,
			"---\n**"+ts.Format("2006-01-02 15:04")+"** — owner\nDay "+
				time.Date(2026, 3, 10+d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")+"\n\n")
	}

	m := New(root)
	start := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 16, 23, 59, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "Day", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 7 {
		t.Fatalf("expected 7 results across 7 days, got %d", len(results))
	}
}

func TestSearch_MatchesOnSource(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — heartbeat\nDisk at 42%\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	// Search for source name.
	results, err := m.Search(context.Background(), "heartbeat", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result matching source, got %d", len(results))
	}
}

func TestSearch_UnreadableFile(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	path := writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nEntry\n\n")

	// Make the file unreadable so parseFile returns an error.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	// Search should skip the unreadable file gracefully (slog.Warn, no error).
	results, err := m.Search(context.Background(), "Entry", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results (file unreadable), got %d", len(results))
	}
}

func TestSearch_EntryOutsideDateRange(t *testing.T) {
	root := t.TempDir()
	// File is at hour 14, but entry timestamp is for a different time that's outside the search range.
	// The file is included by listFiles (hour match), but entry filtered by time check.
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nIn range\n\n"+
			"---\n**2026-03-15 14:55** — owner\nAlso in range\n\n")

	m := New(root)
	// Narrow search range: only 14:05 to 14:20.
	start := time.Date(2026, 3, 15, 14, 5, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 14, 20, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (14:55 outside range), got %d", len(results))
	}
	if results[0].Content != "In range" {
		t.Errorf("expected 'In range', got %q", results[0].Content)
	}
}

func TestSearch_MultilineContent(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nLine one\nLine two\nLine three\n\n")

	m := New(root)
	start := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	results, err := m.Search(context.Background(), "Line two", start, end)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "Line one\nLine two\nLine three") {
		t.Errorf("expected multiline content, got %q", results[0].Content)
	}
}

func TestParseFile_FilePathSet(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	path := writeRawMemoryFile(t, root, ts,
		"---\n**2026-03-15 14:10** — owner\nEntry\n\n")

	m := New(root)
	results, err := m.parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(results))
	}
	if results[0].FilePath != path {
		t.Errorf("FilePath = %q, want %q", results[0].FilePath, path)
	}
}

