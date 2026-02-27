package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/edouard/pureclaw/internal/platform"
)

// configFilePerm is the file permission for config.json (owner rw, group/others read).
const configFilePerm = 0644

// Replaceable for testing error paths.
var (
	atomicWrite      = platform.AtomicWrite
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) { return json.MarshalIndent(v, prefix, indent) }
)

// Duration wraps time.Duration with custom JSON marshal/unmarshal for string durations.
type Duration struct {
	time.Duration
}

// MarshalJSON encodes the duration as a JSON string (e.g., "30m0s").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// UnmarshalJSON decodes a JSON string into a Duration.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

// Config holds the application configuration.
type Config struct {
	Workspace          string   `json:"workspace"`
	ModelText          string   `json:"model_text"`
	ModelAudio         string   `json:"model_audio"`
	TelegramAllowedIDs []int64  `json:"telegram_allowed_ids"`
	HeartbeatInterval  Duration `json:"heartbeat_interval"`
	SubAgentTimeout    Duration `json:"sub_agent_timeout"`
}

// Load reads and parses a config.json file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: load: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: load: unmarshal: %w", err)
	}
	slog.Info("config loaded", "component", "config", "operation", "load", "path", path)
	return &cfg, nil
}

// Save writes the config struct to the given path atomically with JSON formatting.
func Save(cfg *Config, path string) error {
	data, err := jsonMarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: save: marshal: %w", err)
	}
	data = append(data, '\n')
	if err := atomicWrite(path, data, configFilePerm); err != nil {
		return fmt.Errorf("config: save: %w", err)
	}
	slog.Info("config saved", "component", "config", "operation", "save", "path", path)
	return nil
}
