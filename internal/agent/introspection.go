package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/edouard/pureclaw/internal/platform"
)

const envSectionHeader = "## Environment"

// SystemInfo holds discovered system information.
type SystemInfo struct {
	OS            string
	Arch          string
	CPUCount      int
	TotalRAM      string
	DiskTotal     string
	DiskAvailable string
	AvailableCmds []string
	DetectedAt    time.Time
}

// Replaceable for testing.
var (
	introspectGetOS  = func() string { return runtime.GOOS }
	introspectGetArch = func() string { return runtime.GOARCH }
	introspectGetCPU  = func() int { return runtime.NumCPU() }
	introspectLookPath = exec.LookPath
	introspectRunCmd   = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
	introspectReadFile = os.ReadFile
	introspectNow      = time.Now
)

var defaultCommands = []string{
	"git", "curl", "wget", "ssh", "scp", "rsync",
	"python3", "python", "node", "npm",
	"docker", "make", "gcc", "go",
	"tar", "gzip", "unzip",
	"df", "free", "top", "htop",
	"systemctl", "journalctl",
}

// runIntrospectionIfNeeded discovers the host system on first run and records results.
// It skips silently if workspace is nil, Root is empty, or the environment section already exists.
// Failure is non-fatal: returns an error but the caller should log and continue.
func (a *Agent) runIntrospectionIfNeeded(ctx context.Context) error {
	if a.workspace == nil {
		slog.Debug("introspection skipped: no workspace",
			"component", "agent",
			"operation", "introspection",
		)
		return nil
	}
	if a.workspace.Root == "" {
		slog.Debug("introspection skipped: empty root",
			"component", "agent",
			"operation", "introspection",
		)
		return nil
	}
	if strings.Contains(a.workspace.AgentMD, envSectionHeader) {
		slog.Debug("introspection skipped: environment section already present",
			"component", "agent",
			"operation", "introspection",
		)
		return nil
	}

	slog.Info("running system introspection",
		"component", "agent",
		"operation", "introspection",
	)

	info := gatherSystemInfo(ctx)
	envSection := formatEnvironmentSection(info)

	if err := a.updateAgentMD(envSection); err != nil {
		return fmt.Errorf("agent: introspection: %w", err)
	}

	a.logMemory(ctx, "introspection", envSection)

	slog.Info("introspection complete",
		"component", "agent",
		"operation", "introspection",
		"os", info.OS,
		"arch", info.Arch,
		"cpus", info.CPUCount,
		"ram", info.TotalRAM,
		"disk_total", info.DiskTotal,
		"disk_available", info.DiskAvailable,
		"commands", len(info.AvailableCmds),
	)

	return nil
}

// gatherSystemInfo orchestrates all discovery functions. Never returns error; uses "unknown" fallback.
func gatherSystemInfo(ctx context.Context) SystemInfo {
	diskTotal, diskAvailable := discoverDisk(ctx)
	return SystemInfo{
		OS:            introspectGetOS(),
		Arch:          introspectGetArch(),
		CPUCount:      introspectGetCPU(),
		TotalRAM:      discoverRAM(ctx),
		DiskTotal:     diskTotal,
		DiskAvailable: diskAvailable,
		AvailableCmds: discoverCommands(),
		DetectedAt:    introspectNow(),
	}
}

// discoverRAM returns human-readable total RAM. Linux: /proc/meminfo; macOS: sysctl; other: "unknown".
func discoverRAM(ctx context.Context) string {
	switch introspectGetOS() {
	case "linux":
		return discoverRAMLinux()
	case "darwin":
		return discoverRAMDarwin(ctx)
	default:
		return "unknown"
	}
}

func discoverRAMLinux() string {
	data, err := introspectReadFile("/proc/meminfo")
	if err != nil {
		slog.Warn("failed to read /proc/meminfo",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
		return "unknown"
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				slog.Debug("meminfo MemTotal line has too few fields",
					"component", "agent",
					"operation", "introspection",
				)
				return "unknown"
			}
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				slog.Debug("failed to parse MemTotal value",
					"component", "agent",
					"operation", "introspection",
					"error", err,
				)
				return "unknown"
			}
			return formatBytesUint(kb * 1024)
		}
	}
	return "unknown"
}

func discoverRAMDarwin(ctx context.Context) string {
	out, err := introspectRunCmd(ctx, "sysctl", "-n", "hw.memsize")
	if err != nil {
		slog.Warn("sysctl failed",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
		return "unknown"
	}
	b, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		slog.Debug("failed to parse sysctl hw.memsize value",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
		return "unknown"
	}
	return formatBytesUint(b)
}

// discoverDisk runs `df -k /` and returns (total, available) as human-readable strings.
func discoverDisk(ctx context.Context) (string, string) {
	out, err := introspectRunCmd(ctx, "df", "-k", "/")
	if err != nil {
		slog.Warn("df command failed",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
		return "unknown", "unknown"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "unknown", "unknown"
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return "unknown", "unknown"
	}
	totalKB, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		slog.Debug("failed to parse df total value",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
		return "unknown", "unknown"
	}
	availKB, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		slog.Debug("failed to parse df available value",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
		return "unknown", "unknown"
	}
	return formatBytesUint(totalKB * 1024), formatBytesUint(availKB * 1024)
}

// discoverCommands checks which commands from defaultCommands are available on PATH.
func discoverCommands() []string {
	var found []string
	for _, cmd := range defaultCommands {
		if _, err := introspectLookPath(cmd); err == nil {
			found = append(found, cmd)
		}
	}
	sort.Strings(found)
	return found
}

// formatEnvironmentSection renders SystemInfo as a Markdown section.
func formatEnvironmentSection(info SystemInfo) string {
	cmds := "none"
	if len(info.AvailableCmds) > 0 {
		cmds = strings.Join(info.AvailableCmds, ", ")
	}
	return fmt.Sprintf(`## Environment

- **OS:** %s
- **Architecture:** %s
- **CPU Count:** %d
- **Total RAM:** %s
- **Disk Space:** %s available / %s total
- **Available Commands:** %s
- **Detected At:** %s`,
		info.OS,
		info.Arch,
		info.CPUCount,
		info.TotalRAM,
		info.DiskAvailable,
		info.DiskTotal,
		cmds,
		info.DetectedAt.UTC().Format("2006-01-02 15:04 UTC"),
	)
}

// formatBytesUint formats a byte count as human-readable (GB, MB, or bytes).
func formatBytesUint(n uint64) string {
	const (
		gb = 1024 * 1024 * 1024
		mb = 1024 * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// updateAgentMD appends the environment section to AGENT.md and updates the in-memory copy.
func (a *Agent) updateAgentMD(envSection string) error {
	newContent := a.workspace.AgentMD + "\n\n" + envSection
	path := filepath.Join(a.workspace.Root, "AGENT.md")
	if err := platform.AtomicWrite(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write AGENT.md: %w", err)
	}
	a.workspace.AgentMD = newContent
	slog.Info("AGENT.md updated with environment section",
		"component", "agent",
		"operation", "introspection",
		"path", path,
	)
	return nil
}
