package memory

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// SearchResult represents a single parsed memory entry.
type SearchResult struct {
	Time     time.Time // Timestamp of the entry
	Source   string    // Who wrote it: "owner", "agent", "heartbeat", "action"
	Content  string    // The entry content (after the source line)
	FilePath string    // Path to the source file (for debugging)
}

// Search reads memory files within [start, end] and returns entries matching keyword.
// Keyword matching is case-insensitive on the full entry text (source + content).
// Returns results in chronological order.
// If keyword is empty, returns all entries in the range (equivalent to ReadRange).
func (m *Memory) Search(ctx context.Context, keyword string, start, end time.Time) ([]SearchResult, error) {
	slog.Info("searching memory",
		"component", "memory",
		"operation", "search",
		"keyword", keyword,
		"start", start.Format(time.RFC3339),
		"end", end.Format(time.RFC3339),
	)

	files := m.listFiles(start, end)

	var results []SearchResult
	lowerKeyword := strings.ToLower(keyword)

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("memory: search: %w", err)
		}

		entries, err := m.parseFile(path)
		if err != nil {
			slog.Warn("failed to parse memory file",
				"component", "memory",
				"operation", "search",
				"path", path,
				"error", err,
			)
			continue
		}

		for _, e := range entries {
			if e.Time.Before(start) || e.Time.After(end) {
				continue
			}
			if keyword == "" || strings.Contains(strings.ToLower(e.Source+" "+e.Content), lowerKeyword) {
				results = append(results, e)
			}
		}
	}

	slog.Info("search complete",
		"component", "memory",
		"operation", "search",
		"files_scanned", len(files),
		"results_found", len(results),
	)

	return results, nil
}

// ReadRange reads all memory entries within [start, end] in chronological order.
// Returns all entries without filtering. Suitable for context reconstruction.
// Delegates to Search with an empty keyword; Search handles logging.
func (m *Memory) ReadRange(ctx context.Context, start, end time.Time) ([]SearchResult, error) {
	return m.Search(ctx, "", start, end)
}

// listFiles enumerates hourly memory files within [start, end].
// Returns paths in chronological order.
// Uses hour-by-hour iteration for predictable performance.
func (m *Memory) listFiles(start, end time.Time) []string {
	// Truncate to the start of the hour.
	t := start.Truncate(time.Hour)
	endTrunc := end.Truncate(time.Hour)

	var files []string
	for !t.After(endTrunc) {
		path := m.hourlyPath(t)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
		t = t.Add(time.Hour)
	}

	return files
}

// parseFile reads a memory file and returns parsed entries.
// Entry format: ---\n**YYYY-MM-DD HH:MM** — source\ncontent\n\n
// Malformed entries are skipped with a warning log.
func (m *Memory) parseFile(path string) ([]SearchResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("memory: parse_file: %w", err)
	}

	content := string(data)
	if content == "" {
		return nil, nil
	}

	// Split on "---\n" separator.
	// Each entry starts with "---\n", so we split and skip the first empty segment.
	segments := strings.Split(content, "---\n")

	var results []SearchResult
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}

		result, ok := parseEntry(seg, path)
		if !ok {
			continue
		}
		results = append(results, result)
	}

	return results, nil
}

// parseEntry parses a single entry segment into a SearchResult.
// Returns false if the entry is malformed.
func parseEntry(seg, filePath string) (SearchResult, bool) {
	// First line: **YYYY-MM-DD HH:MM** — source
	firstNewline := strings.Index(seg, "\n")
	var headerLine, entryContent string
	if firstNewline == -1 {
		headerLine = seg
		entryContent = ""
	} else {
		headerLine = seg[:firstNewline]
		entryContent = strings.TrimSpace(seg[firstNewline+1:])
	}

	// Strip ** markers: "**2026-03-15 14:23** — owner" -> "2026-03-15 14:23 — owner"
	header := strings.ReplaceAll(headerLine, "**", "")
	header = strings.TrimSpace(header)

	// Split on " — " to get timestamp and source.
	parts := strings.SplitN(header, " — ", 2)
	if len(parts) != 2 {
		slog.Warn("malformed memory entry: missing separator",
			"component", "memory",
			"operation", "parse_file",
			"path", filePath,
			"header", headerLine,
		)
		return SearchResult{}, false
	}

	timestampStr := strings.TrimSpace(parts[0])
	source := strings.TrimSpace(parts[1])

	// Parse in UTC explicitly. Memory.Write() formats timestamps using timeNow()
	// (local time). The system assumes UTC deployment (Raspberry Pi default).
	// In non-UTC environments, entry timestamps would drift by the UTC offset.
	t, err := time.ParseInLocation("2006-01-02 15:04", timestampStr, time.UTC)
	if err != nil {
		slog.Warn("malformed memory entry: bad timestamp",
			"component", "memory",
			"operation", "parse_file",
			"path", filePath,
			"timestamp", timestampStr,
			"error", err,
		)
		return SearchResult{}, false
	}

	return SearchResult{
		Time:     t,
		Source:   source,
		Content:  entryContent,
		FilePath: filePath,
	}, true
}
