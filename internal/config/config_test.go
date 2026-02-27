package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		dur  Duration
		want string
	}{
		{"30 minutes", Duration{30 * time.Minute}, `"30m0s"`},
		{"5 minutes", Duration{5 * time.Minute}, `"5m0s"`},
		{"1h30m", Duration{90 * time.Minute}, `"1h30m0s"`},
		{"zero", Duration{0}, `"0s"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.dur.MarshalJSON()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"30 minutes", `"30m"`, 30 * time.Minute, false},
		{"5 minutes", `"5m"`, 5 * time.Minute, false},
		{"1h30m", `"1h30m"`, 90 * time.Minute, false},
		{"with seconds", `"30m0s"`, 30 * time.Minute, false},
		{"invalid duration", `"not-a-duration"`, 0, true},
		{"invalid json", `123`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Duration != tt.want {
				t.Fatalf("got %v, want %v", d.Duration, tt.want)
			}
		})
	}
}

func TestDuration_JSONRoundTrip(t *testing.T) {
	original := Duration{30 * time.Minute}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Duration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Duration != original.Duration {
		t.Fatalf("round-trip failed: got %v, want %v", decoded.Duration, original.Duration)
	}
}

func TestLoad(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		content := `{
  "workspace": "./workspace",
  "model_text": "mistral-large-latest",
  "model_audio": "voxtral-mini-latest",
  "telegram_allowed_ids": [123456789],
  "heartbeat_interval": "30m",
  "sub_agent_timeout": "5m"
}`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Workspace != "./workspace" {
			t.Fatalf("workspace = %q, want %q", cfg.Workspace, "./workspace")
		}
		if cfg.ModelText != "mistral-large-latest" {
			t.Fatalf("model_text = %q, want %q", cfg.ModelText, "mistral-large-latest")
		}
		if cfg.ModelAudio != "voxtral-mini-latest" {
			t.Fatalf("model_audio = %q, want %q", cfg.ModelAudio, "voxtral-mini-latest")
		}
		if len(cfg.TelegramAllowedIDs) != 1 || cfg.TelegramAllowedIDs[0] != 123456789 {
			t.Fatalf("telegram_allowed_ids = %v, want [123456789]", cfg.TelegramAllowedIDs)
		}
		if cfg.HeartbeatInterval.Duration != 30*time.Minute {
			t.Fatalf("heartbeat_interval = %v, want 30m", cfg.HeartbeatInterval.Duration)
		}
		if cfg.SubAgentTimeout.Duration != 5*time.Minute {
			t.Fatalf("sub_agent_timeout = %v, want 5m", cfg.SubAgentTimeout.Duration)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Load("/nonexistent/config.json")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("{invalid}"), 0644); err != nil {
			t.Fatalf("write test file: %v", err)
		}
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid duration", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		content := `{"heartbeat_interval": "not-a-duration"}`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write test file: %v", err)
		}
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid duration, got nil")
		}
	})
}

func TestSave(t *testing.T) {
	t.Run("valid save", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		cfg := &Config{
			Workspace:          "./workspace",
			ModelText:          "mistral-large-latest",
			ModelAudio:         "voxtral-mini-latest",
			TelegramAllowedIDs: []int64{123456789},
			HeartbeatInterval:  Duration{30 * time.Minute},
			SubAgentTimeout:    Duration{5 * time.Minute},
		}

		if err := Save(cfg, path); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify by loading back
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if loaded.Workspace != cfg.Workspace {
			t.Fatalf("workspace = %q, want %q", loaded.Workspace, cfg.Workspace)
		}
		if loaded.HeartbeatInterval.Duration != cfg.HeartbeatInterval.Duration {
			t.Fatalf("heartbeat = %v, want %v", loaded.HeartbeatInterval.Duration, cfg.HeartbeatInterval.Duration)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		original := jsonMarshalIndent
		defer func() { jsonMarshalIndent = original }()
		jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal failure")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		err := Save(&Config{}, path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("atomic write error", func(t *testing.T) {
		original := atomicWrite
		defer func() { atomicWrite = original }()
		atomicWrite = func(path string, data []byte, perm os.FileMode) error {
			return errors.New("write failure")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		err := Save(&Config{}, path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestSave_Load_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := &Config{
		Workspace:          "./workspace",
		ModelText:          "mistral-large-latest",
		ModelAudio:         "voxtral-mini-latest",
		TelegramAllowedIDs: []int64{111, 222, 333},
		HeartbeatInterval:  Duration{90 * time.Minute},
		SubAgentTimeout:    Duration{10 * time.Minute},
	}

	if err := Save(original, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Workspace != original.Workspace {
		t.Fatalf("workspace: got %q, want %q", loaded.Workspace, original.Workspace)
	}
	if loaded.ModelText != original.ModelText {
		t.Fatalf("model_text: got %q, want %q", loaded.ModelText, original.ModelText)
	}
	if loaded.ModelAudio != original.ModelAudio {
		t.Fatalf("model_audio: got %q, want %q", loaded.ModelAudio, original.ModelAudio)
	}
	if len(loaded.TelegramAllowedIDs) != len(original.TelegramAllowedIDs) {
		t.Fatalf("telegram_allowed_ids length: got %d, want %d", len(loaded.TelegramAllowedIDs), len(original.TelegramAllowedIDs))
	}
	for i, id := range loaded.TelegramAllowedIDs {
		if id != original.TelegramAllowedIDs[i] {
			t.Fatalf("telegram_allowed_ids[%d]: got %d, want %d", i, id, original.TelegramAllowedIDs[i])
		}
	}
	if loaded.HeartbeatInterval.Duration != original.HeartbeatInterval.Duration {
		t.Fatalf("heartbeat: got %v, want %v", loaded.HeartbeatInterval.Duration, original.HeartbeatInterval.Duration)
	}
	if loaded.SubAgentTimeout.Duration != original.SubAgentTimeout.Duration {
		t.Fatalf("sub_agent_timeout: got %v, want %v", loaded.SubAgentTimeout.Duration, original.SubAgentTimeout.Duration)
	}
}
