// Package config parses the exporter's runtime configuration from command-line
// flags with per-key environment-variable fallbacks (ATPROTO_<UPPER_SNAKE>) and
// sane defaults, so the binary runs with zero flags.
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config holds the fully-resolved runtime configuration. See SPEC.md for the
// authoritative flag/env/default table.
type Config struct {
	Listen string

	JetstreamHost     string
	JetstreamFailover []string
	JetstreamZSTD     bool

	Collections []string // allowlisted NSIDs; everything else buckets to "other"
	Langs       []string // allowlisted ISO-639 codes; everything else buckets to "other"

	TopN        int
	TopNWindow  time.Duration
	TopNRefresh time.Duration

	PLCBaseURL  string
	PLCInterval time.Duration

	CursorDir    string
	CursorRewind time.Duration

	LogLevel string
}

// Defaults, kept as package-level vars so they document the zero-flag behavior.
var (
	defaultFailover = []string{
		"jetstream2.us-east.bsky.network",
		"jetstream1.us-west.bsky.network",
		"jetstream2.us-west.bsky.network",
	}
	// Full NSIDs, matching what Jetstream emits in commit events.
	defaultCollections = []string{
		"app.bsky.feed.post",
		"app.bsky.feed.like",
		"app.bsky.feed.repost",
		"app.bsky.graph.follow",
		"app.bsky.graph.block",
		"app.bsky.actor.profile",
		"app.bsky.graph.list",
		"app.bsky.graph.listitem",
		"app.bsky.graph.listblock",
		"app.bsky.feed.generator",
		"app.bsky.feed.threadgate",
		"app.bsky.feed.postgate",
	}
	// ~40 common ISO-639-1 codes. The lang field is user-supplied and dirty;
	// anything outside this set buckets to "other".
	defaultLangs = []string{
		"en", "ja", "pt", "de", "es", "fr", "ko", "it", "nl", "tr",
		"ru", "zh", "ar", "id", "pl", "th", "uk", "fa", "vi", "sv",
		"fi", "da", "no", "cs", "el", "he", "hu", "ro", "hi", "ca",
		"tl", "eu", "gl", "sk", "hr", "bg", "sr", "lt", "et", "lv",
	}
)

// EnvLookup mirrors os.LookupEnv; injected for testability.
type EnvLookup = func(string) (string, bool)

// Load resolves configuration from args (without the program name) and an env
// lookup. Flags take precedence over env, which takes precedence over defaults.
func Load(args []string, getenv EnvLookup) (Config, error) {
	fs := flag.NewFlagSet("atproto-exporter", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		cfg     Config
		err     error
		errKeep = func(e error) {
			if err == nil {
				err = e
			}
		}
	)

	listen := fs.String("listen", envStr(getenv, "ATPROTO_LISTEN", ":9200"), "listen address for /metrics and /healthz")
	jsHost := fs.String("jetstream-host", envStr(getenv, "ATPROTO_JETSTREAM_HOST", "jetstream1.us-east.bsky.network"), "primary Jetstream host")
	jsFailover := fs.String("jetstream-failover", envStr(getenv, "ATPROTO_JETSTREAM_FAILOVER", strings.Join(defaultFailover, ",")), "comma-separated Jetstream failover hosts")
	jsZSTD := fs.Bool("jetstream-zstd", envBool(getenv, "ATPROTO_JETSTREAM_ZSTD", true, errKeep), "negotiate zstd compression with Jetstream")
	collections := fs.String("collections", envStr(getenv, "ATPROTO_COLLECTIONS", strings.Join(defaultCollections, ",")), "comma-separated NSID allowlist")
	langs := fs.String("langs", envStr(getenv, "ATPROTO_LANGS", strings.Join(defaultLangs, ",")), "comma-separated language allowlist")
	topN := fs.Int("topn", envInt(getenv, "ATPROTO_TOPN", 20, errKeep), "number of top domains/hashtags to export")
	topNWindow := fs.Duration("topn-window", envDur(getenv, "ATPROTO_TOPN_WINDOW", time.Hour, errKeep), "rolling window for top-N")
	topNRefresh := fs.Duration("topn-refresh", envDur(getenv, "ATPROTO_TOPN_REFRESH", 15*time.Second, errKeep), "how often top-N gauges are refreshed")
	plcBase := fs.String("plc-base-url", envStr(getenv, "ATPROTO_PLC_BASE_URL", "https://plc.directory"), "plc.directory base URL")
	plcInterval := fs.Duration("plc-interval", envDur(getenv, "ATPROTO_PLC_INTERVAL", 30*time.Second, errKeep), "PLC export poll interval")
	cursorDir := fs.String("cursor-dir", envStr(getenv, "ATPROTO_CURSOR_DIR", "./data"), "directory for persisted cursors")
	cursorRewind := fs.Duration("cursor-rewind", envDur(getenv, "ATPROTO_CURSOR_REWIND", 5*time.Second, errKeep), "how far to rewind the Jetstream cursor on resume")
	logLevel := fs.String("log-level", envStr(getenv, "ATPROTO_LOG_LEVEL", "info"), "log level: debug|info|warn|error")

	if err != nil {
		return Config{}, err
	}
	if perr := fs.Parse(args); perr != nil {
		return Config{}, perr
	}

	cfg = Config{
		Listen:            *listen,
		JetstreamHost:     *jsHost,
		JetstreamFailover: splitList(*jsFailover),
		JetstreamZSTD:     *jsZSTD,
		Collections:       splitList(*collections),
		Langs:             splitList(*langs),
		TopN:              *topN,
		TopNWindow:        *topNWindow,
		TopNRefresh:       *topNRefresh,
		PLCBaseURL:        *plcBase,
		PLCInterval:       *plcInterval,
		CursorDir:         *cursorDir,
		CursorRewind:      *cursorRewind,
		LogLevel:          *logLevel,
	}
	return cfg, nil
}

// SlogLevel maps the configured log level string to a slog.Level, defaulting to
// info for anything unrecognized.
func (c Config) SlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envStr(getenv EnvLookup, key, def string) string {
	if v, ok := getenv(key); ok {
		return v
	}
	return def
}

func envBool(getenv EnvLookup, key string, def bool, keep func(error)) bool {
	v, ok := getenv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		keep(fmt.Errorf("%s: invalid bool %q: %w", key, v, err))
		return def
	}
	return b
}

func envInt(getenv EnvLookup, key string, def int, keep func(error)) int {
	v, ok := getenv(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		keep(fmt.Errorf("%s: invalid int %q: %w", key, v, err))
		return def
	}
	return n
}

func envDur(getenv EnvLookup, key string, def time.Duration, keep func(error)) time.Duration {
	v, ok := getenv(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		keep(fmt.Errorf("%s: invalid duration %q: %w", key, v, err))
		return def
	}
	return d
}
