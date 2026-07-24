package config

import (
	"log/slog"
	"testing"
	"time"
)

// noEnv is an env lookup that always reports "unset".
func noEnv(string) (string, bool) { return "", false }

// envMap returns a lookup backed by a map.
func envMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, noEnv)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9200" {
		t.Errorf("Listen = %q, want :9200", cfg.Listen)
	}
	if cfg.JetstreamHost != "jetstream1.us-east.bsky.network" {
		t.Errorf("JetstreamHost = %q", cfg.JetstreamHost)
	}
	if len(cfg.JetstreamFailover) != 3 {
		t.Errorf("JetstreamFailover = %v, want 3 hosts", cfg.JetstreamFailover)
	}
	if !cfg.JetstreamZSTD {
		t.Errorf("JetstreamZSTD = false, want true")
	}
	if cfg.TopN != 20 {
		t.Errorf("TopN = %d, want 20", cfg.TopN)
	}
	if cfg.TopNWindow != time.Hour {
		t.Errorf("TopNWindow = %v, want 1h", cfg.TopNWindow)
	}
	if cfg.PLCBaseURL != "https://plc.directory" {
		t.Errorf("PLCBaseURL = %q", cfg.PLCBaseURL)
	}
	if cfg.PLCInterval != 30*time.Second {
		t.Errorf("PLCInterval = %v, want 30s", cfg.PLCInterval)
	}
	if cfg.CursorDir != "./data" {
		t.Errorf("CursorDir = %q", cfg.CursorDir)
	}
	if cfg.CursorRewind != 5*time.Second {
		t.Errorf("CursorRewind = %v, want 5s", cfg.CursorRewind)
	}
	if len(cfg.Collections) == 0 {
		t.Errorf("Collections empty, want defaults")
	}
	if len(cfg.Langs) == 0 {
		t.Errorf("Langs empty, want defaults")
	}
}

func TestLoadEnvFallback(t *testing.T) {
	cfg, err := Load(nil, envMap(map[string]string{
		"ATPROTO_LISTEN":         ":7000",
		"ATPROTO_TOPN":           "5",
		"ATPROTO_PLC_INTERVAL":   "10s",
		"ATPROTO_JETSTREAM_ZSTD": "false",
		"ATPROTO_COLLECTIONS":    "app.bsky.feed.post,app.bsky.feed.like",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":7000" {
		t.Errorf("Listen = %q, want :7000", cfg.Listen)
	}
	if cfg.TopN != 5 {
		t.Errorf("TopN = %d, want 5", cfg.TopN)
	}
	if cfg.PLCInterval != 10*time.Second {
		t.Errorf("PLCInterval = %v, want 10s", cfg.PLCInterval)
	}
	if cfg.JetstreamZSTD {
		t.Errorf("JetstreamZSTD = true, want false")
	}
	if len(cfg.Collections) != 2 {
		t.Errorf("Collections = %v, want 2", cfg.Collections)
	}
}

func TestLoadFlagOverridesEnv(t *testing.T) {
	cfg, err := Load([]string{"-listen", ":8888"}, envMap(map[string]string{
		"ATPROTO_LISTEN": ":7000",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":8888" {
		t.Errorf("Listen = %q, want :8888 (flag beats env)", cfg.Listen)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	_, err := Load(nil, envMap(map[string]string{
		"ATPROTO_PLC_INTERVAL": "not-a-duration",
	}))
	if err == nil {
		t.Fatalf("Load: want error for invalid duration, got nil")
	}
}

func TestLoadInvalidInt(t *testing.T) {
	_, err := Load(nil, envMap(map[string]string{
		"ATPROTO_TOPN": "abc",
	}))
	if err == nil {
		t.Fatalf("Load: want error for invalid int, got nil")
	}
}

func TestSlogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"INFO":  slog.LevelInfo,
	}
	for in, want := range cases {
		cfg := Config{LogLevel: in}
		if got := cfg.SlogLevel(); got != want {
			t.Errorf("SlogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
