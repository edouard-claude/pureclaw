package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edouard/pureclaw/internal/workspace"
)

// --- Test helpers ---

func saveIntrospectVars(t *testing.T) func() {
	t.Helper()
	oldOS := introspectGetOS
	oldArch := introspectGetArch
	oldCPU := introspectGetCPU
	oldLook := introspectLookPath
	oldCmd := introspectRunCmd
	oldRead := introspectReadFile
	oldNow := introspectNow
	return func() {
		introspectGetOS = oldOS
		introspectGetArch = oldArch
		introspectGetCPU = oldCPU
		introspectLookPath = oldLook
		introspectRunCmd = oldCmd
		introspectReadFile = oldRead
		introspectNow = oldNow
	}
}

var fixedTime = time.Date(2026, 3, 15, 14, 23, 0, 0, time.UTC)

// --- formatBytesUint tests ---

func TestFormatBytesUint_GB(t *testing.T) {
	got := formatBytesUint(2 * 1024 * 1024 * 1024)
	if got != "2.0 GB" {
		t.Errorf("formatBytesUint(2GB) = %q, want %q", got, "2.0 GB")
	}
}

func TestFormatBytesUint_MB(t *testing.T) {
	got := formatBytesUint(512 * 1024 * 1024)
	if got != "512.0 MB" {
		t.Errorf("formatBytesUint(512MB) = %q, want %q", got, "512.0 MB")
	}
}

func TestFormatBytesUint_Bytes(t *testing.T) {
	got := formatBytesUint(1023)
	if got != "1023 bytes" {
		t.Errorf("formatBytesUint(1023) = %q, want %q", got, "1023 bytes")
	}
}

func TestFormatBytesUint_Zero(t *testing.T) {
	got := formatBytesUint(0)
	if got != "0 bytes" {
		t.Errorf("formatBytesUint(0) = %q, want %q", got, "0 bytes")
	}
}

// --- formatEnvironmentSection tests ---

func TestFormatEnvironmentSection(t *testing.T) {
	info := SystemInfo{
		OS:            "linux",
		Arch:          "arm64",
		CPUCount:      4,
		TotalRAM:      "1.0 GB",
		DiskTotal:     "32.0 GB",
		DiskAvailable: "28.5 GB",
		AvailableCmds: []string{"curl", "git", "go"},
		DetectedAt:    fixedTime,
	}
	got := formatEnvironmentSection(info)

	want := `## Environment

- **OS:** linux
- **Architecture:** arm64
- **CPU Count:** 4
- **Total RAM:** 1.0 GB
- **Disk Space:** 28.5 GB available / 32.0 GB total
- **Available Commands:** curl, git, go
- **Detected At:** 2026-03-15 14:23 UTC`

	if got != want {
		t.Errorf("formatEnvironmentSection mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatEnvironmentSection_NoCommands(t *testing.T) {
	info := SystemInfo{
		OS:            "linux",
		Arch:          "amd64",
		CPUCount:      1,
		TotalRAM:      "unknown",
		DiskTotal:     "unknown",
		DiskAvailable: "unknown",
		AvailableCmds: nil,
		DetectedAt:    fixedTime,
	}
	got := formatEnvironmentSection(info)
	if !strings.Contains(got, "**Available Commands:** none") {
		t.Errorf("expected 'none' for empty commands, got:\n%s", got)
	}
}

// --- discoverCommands tests ---

func TestDiscoverCommands_SomeFound(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectLookPath = func(file string) (string, error) {
		switch file {
		case "git", "curl":
			return "/usr/bin/" + file, nil
		default:
			return "", errors.New("not found")
		}
	}

	got := discoverCommands()
	if len(got) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(got), got)
	}
	if got[0] != "curl" || got[1] != "git" {
		t.Errorf("expected [curl, git], got %v", got)
	}
}

func TestDiscoverCommands_NoneFound(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectLookPath = func(file string) (string, error) {
		return "", errors.New("not found")
	}

	got := discoverCommands()
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// --- discoverRAM tests ---

func TestDiscoverRAM_Linux(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:        1048576 kB\nMemFree:          512000 kB\n"), nil
	}

	got := discoverRAM(context.Background())
	// 1048576 kB = 1048576 * 1024 bytes = 1 GB
	if got != "1.0 GB" {
		t.Errorf("discoverRAM(linux) = %q, want %q", got, "1.0 GB")
	}
}

func TestDiscoverRAM_Linux_ReadError(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(linux read error) = %q, want %q", got, "unknown")
	}
}

func TestDiscoverRAM_Linux_MalformedMeminfo(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("garbage data\nno memtotal here\n"), nil
	}

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(linux malformed) = %q, want %q", got, "unknown")
	}
}

func TestDiscoverRAM_Linux_MalformedValue(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:        notanumber kB\n"), nil
	}

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(linux bad value) = %q, want %q", got, "unknown")
	}
}

func TestDiscoverRAM_Linux_ShortLine(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:\n"), nil
	}

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(linux short line) = %q, want %q", got, "unknown")
	}
}

func TestDiscoverRAM_Darwin(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "darwin" }
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("17179869184\n"), nil // 16 GB
	}

	got := discoverRAM(context.Background())
	if got != "16.0 GB" {
		t.Errorf("discoverRAM(darwin) = %q, want %q", got, "16.0 GB")
	}
}

func TestDiscoverRAM_Darwin_CmdError(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "darwin" }
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("command not found")
	}

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(darwin error) = %q, want %q", got, "unknown")
	}
}

func TestDiscoverRAM_Darwin_ParseError(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "darwin" }
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("not_a_number\n"), nil
	}

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(darwin parse error) = %q, want %q", got, "unknown")
	}
}

func TestDiscoverRAM_UnknownOS(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "freebsd" }

	got := discoverRAM(context.Background())
	if got != "unknown" {
		t.Errorf("discoverRAM(freebsd) = %q, want %q", got, "unknown")
	}
}

// --- discoverDisk tests ---

func TestDiscoverDisk_Success(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       31457280 15728640  15728640  50% /\n"), nil
	}

	total, avail := discoverDisk(context.Background())
	// 31457280 kB = 30 GB
	if total != "30.0 GB" {
		t.Errorf("discoverDisk total = %q, want %q", total, "30.0 GB")
	}
	// 15728640 kB = 15 GB
	if avail != "15.0 GB" {
		t.Errorf("discoverDisk available = %q, want %q", avail, "15.0 GB")
	}
}

func TestDiscoverDisk_CmdError(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("df not found")
	}

	total, avail := discoverDisk(context.Background())
	if total != "unknown" || avail != "unknown" {
		t.Errorf("discoverDisk(error) = %q, %q; want unknown, unknown", total, avail)
	}
}

func TestDiscoverDisk_MalformedOutput(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("garbage\n"), nil
	}

	total, avail := discoverDisk(context.Background())
	if total != "unknown" || avail != "unknown" {
		t.Errorf("discoverDisk(malformed) = %q, %q; want unknown, unknown", total, avail)
	}
}

func TestDiscoverDisk_TooFewFields(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks\n/dev/sda1       31457280\n"), nil
	}

	total, avail := discoverDisk(context.Background())
	if total != "unknown" || avail != "unknown" {
		t.Errorf("discoverDisk(few fields) = %q, %q; want unknown, unknown", total, avail)
	}
}

func TestDiscoverDisk_BadTotalValue(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       notanum 15728640  15728640  50% /\n"), nil
	}

	total, avail := discoverDisk(context.Background())
	if total != "unknown" || avail != "unknown" {
		t.Errorf("discoverDisk(bad total) = %q, %q; want unknown, unknown", total, avail)
	}
}

func TestDiscoverDisk_BadAvailValue(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       31457280 15728640  notanum  50% /\n"), nil
	}

	total, avail := discoverDisk(context.Background())
	if total != "unknown" || avail != "unknown" {
		t.Errorf("discoverDisk(bad avail) = %q, %q; want unknown, unknown", total, avail)
	}
}

// --- gatherSystemInfo tests ---

func TestGatherSystemInfo(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectGetArch = func() string { return "arm64" }
	introspectGetCPU = func() int { return 4 }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:        1048576 kB\n"), nil
	}
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       31457280 15728640  15728640  50% /\n"), nil
	}
	introspectLookPath = func(file string) (string, error) {
		if file == "git" || file == "go" {
			return "/usr/bin/" + file, nil
		}
		return "", errors.New("not found")
	}
	introspectNow = func() time.Time { return fixedTime }

	info := gatherSystemInfo(context.Background())

	if info.OS != "linux" {
		t.Errorf("OS = %q, want linux", info.OS)
	}
	if info.Arch != "arm64" {
		t.Errorf("Arch = %q, want arm64", info.Arch)
	}
	if info.CPUCount != 4 {
		t.Errorf("CPUCount = %d, want 4", info.CPUCount)
	}
	if info.TotalRAM != "1.0 GB" {
		t.Errorf("TotalRAM = %q, want 1.0 GB", info.TotalRAM)
	}
	if info.DiskTotal != "30.0 GB" {
		t.Errorf("DiskTotal = %q, want 30.0 GB", info.DiskTotal)
	}
	if info.DiskAvailable != "15.0 GB" {
		t.Errorf("DiskAvailable = %q, want 15.0 GB", info.DiskAvailable)
	}
	if len(info.AvailableCmds) != 2 || info.AvailableCmds[0] != "git" || info.AvailableCmds[1] != "go" {
		t.Errorf("AvailableCmds = %v, want [git, go]", info.AvailableCmds)
	}
	if !info.DetectedAt.Equal(fixedTime) {
		t.Errorf("DetectedAt = %v, want %v", info.DetectedAt, fixedTime)
	}
}

// --- runIntrospectionIfNeeded tests ---

func TestRunIntrospectionIfNeeded_AlreadyDone(t *testing.T) {
	ag := &Agent{
		workspace: &workspace.Workspace{
			Root:    t.TempDir(),
			AgentMD: "# Agent\n\n## Environment\n\n- **OS:** linux",
		},
	}

	err := ag.runIntrospectionIfNeeded(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestRunIntrospectionIfNeeded_NilWorkspace(t *testing.T) {
	ag := &Agent{workspace: nil}

	err := ag.runIntrospectionIfNeeded(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestRunIntrospectionIfNeeded_EmptyRoot(t *testing.T) {
	ag := &Agent{
		workspace: &workspace.Workspace{Root: ""},
	}

	err := ag.runIntrospectionIfNeeded(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestRunIntrospectionIfNeeded_FirstRun(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectGetArch = func() string { return "arm64" }
	introspectGetCPU = func() int { return 4 }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:        1048576 kB\n"), nil
	}
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       31457280 15728640  15728640  50% /\n"), nil
	}
	introspectLookPath = func(file string) (string, error) {
		if file == "git" {
			return "/usr/bin/git", nil
		}
		return "", errors.New("not found")
	}
	introspectNow = func() time.Time { return fixedTime }

	tmpDir := t.TempDir()
	mem := &fakeMemoryWriter{}
	ag := &Agent{
		workspace: &workspace.Workspace{
			Root:    tmpDir,
			AgentMD: "# Test Agent",
		},
		memory: mem,
	}

	err := ag.runIntrospectionIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify AGENT.md was written to disk.
	data, err := os.ReadFile(filepath.Join(tmpDir, "AGENT.md"))
	if err != nil {
		t.Fatalf("failed to read AGENT.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Environment") {
		t.Error("AGENT.md missing ## Environment section")
	}
	if !strings.Contains(content, "**OS:** linux") {
		t.Error("AGENT.md missing OS field")
	}

	// Verify in-memory AgentMD was updated.
	if !strings.Contains(ag.workspace.AgentMD, "## Environment") {
		t.Error("in-memory AgentMD not updated")
	}

	// Verify memory was logged.
	if len(mem.entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(mem.entries))
	}
	if mem.entries[0].source != "introspection" {
		t.Errorf("memory source = %q, want introspection", mem.entries[0].source)
	}
	if !strings.Contains(mem.entries[0].content, "## Environment") {
		t.Error("memory content missing environment section")
	}
}

func TestRunIntrospectionIfNeeded_WriteFail(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectGetArch = func() string { return "arm64" }
	introspectGetCPU = func() int { return 1 }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:        1048576 kB\n"), nil
	}
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       31457280 15728640  15728640  50% /\n"), nil
	}
	introspectLookPath = func(file string) (string, error) {
		return "", errors.New("not found")
	}
	introspectNow = func() time.Time { return fixedTime }

	ag := &Agent{
		workspace: &workspace.Workspace{
			Root:    "/nonexistent/path/that/does/not/exist",
			AgentMD: "# Test Agent",
		},
	}

	err := ag.runIntrospectionIfNeeded(context.Background())
	if err == nil {
		t.Fatal("expected error for bad root path, got nil")
	}
	if !strings.Contains(err.Error(), "agent: introspection:") {
		t.Errorf("error should contain 'agent: introspection:', got %q", err.Error())
	}
}

func TestRunIntrospectionIfNeeded_NilMemory(t *testing.T) {
	restore := saveIntrospectVars(t)
	defer restore()

	introspectGetOS = func() string { return "linux" }
	introspectGetArch = func() string { return "arm64" }
	introspectGetCPU = func() int { return 1 }
	introspectReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal:        1048576 kB\n"), nil
	}
	introspectRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/sda1       31457280 15728640  15728640  50% /\n"), nil
	}
	introspectLookPath = func(file string) (string, error) {
		return "", errors.New("not found")
	}
	introspectNow = func() time.Time { return fixedTime }

	tmpDir := t.TempDir()
	ag := &Agent{
		workspace: &workspace.Workspace{
			Root:    tmpDir,
			AgentMD: "# Test Agent",
		},
		memory: nil, // nil memory — should not panic
	}

	err := ag.runIntrospectionIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("unexpected error with nil memory: %v", err)
	}

	// Verify AGENT.md was still written.
	data, err := os.ReadFile(filepath.Join(tmpDir, "AGENT.md"))
	if err != nil {
		t.Fatalf("failed to read AGENT.md: %v", err)
	}
	if !strings.Contains(string(data), "## Environment") {
		t.Error("AGENT.md missing ## Environment section")
	}
}

// --- updateAgentMD tests ---

func TestUpdateAgentMD(t *testing.T) {
	tmpDir := t.TempDir()
	ag := &Agent{
		workspace: &workspace.Workspace{
			Root:    tmpDir,
			AgentMD: "# Test Agent",
		},
	}

	envSection := "## Environment\n\n- **OS:** linux"
	err := ag.updateAgentMD(envSection)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file on disk.
	data, err := os.ReadFile(filepath.Join(tmpDir, "AGENT.md"))
	if err != nil {
		t.Fatalf("failed to read AGENT.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Test Agent") {
		t.Error("file missing original content")
	}
	if !strings.Contains(content, "## Environment") {
		t.Error("file missing environment section")
	}

	// Verify in-memory update.
	if !strings.Contains(ag.workspace.AgentMD, "## Environment") {
		t.Error("in-memory AgentMD not updated")
	}
	if !strings.HasPrefix(ag.workspace.AgentMD, "# Test Agent") {
		t.Error("in-memory AgentMD missing original content")
	}
}

func TestUpdateAgentMD_InMemoryUpdated(t *testing.T) {
	tmpDir := t.TempDir()
	ag := &Agent{
		workspace: &workspace.Workspace{
			Root:    tmpDir,
			AgentMD: "original",
		},
	}

	err := ag.updateAgentMD("## Environment\n\n- test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "original\n\n## Environment\n\n- test"
	if ag.workspace.AgentMD != want {
		t.Errorf("in-memory AgentMD = %q, want %q", ag.workspace.AgentMD, want)
	}
}
