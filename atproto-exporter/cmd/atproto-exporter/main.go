// Command atproto-exporter serves bounded-cardinality Prometheus metrics derived
// from public AT Protocol data sources (Jetstream + plc.directory).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"anto.pt/x/atproto-exporter/internal/config"
	"anto.pt/x/atproto-exporter/internal/cursor"
	"anto.pt/x/atproto-exporter/internal/jetstream"
	"anto.pt/x/atproto-exporter/internal/metrics"
	"anto.pt/x/atproto-exporter/internal/normalize"
	"anto.pt/x/atproto-exporter/internal/plc"
	"anto.pt/x/atproto-exporter/internal/topn"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const cursorPersistEvery = 100

func main() {
	cfg, err := config.Load(os.Args[1:], os.LookupEnv)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(logger)

	if err := run(cfg, logger); err != nil {
		logger.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	m := metrics.New(reg)

	allow := normalize.New(cfg.Collections, cfg.Langs)
	domains := topn.New(cfg.TopN, cfg.TopNWindow)
	hashtags := topn.New(cfg.TopN, cfg.TopNWindow)

	jsCursor := cursor.NewStore(filepath.Join(cfg.CursorDir, "jetstream.cursor"))
	plcCursor := cursor.NewStore(filepath.Join(cfg.CursorDir, "plc.cursor"))

	pipeline := jetstream.New(m, allow, domains, hashtags, jsCursor, jetstream.Options{
		PersistEvery: cursorPersistEvery,
	})
	consumer, err := jetstream.NewConsumer(jetstream.Config{
		Hosts:       append([]string{cfg.JetstreamHost}, cfg.JetstreamFailover...),
		Collections: cfg.Collections,
		Zstd:        cfg.JetstreamZSTD,
		Rewind:      cfg.CursorRewind,
	}, pipeline, m, jsCursor)
	if err != nil {
		return err
	}
	poller := plc.NewPoller(plc.Config{BaseURL: cfg.PLCBaseURL}, m, plcCursor)

	// The three source loops run independently: each survives the others'
	// failures (graceful degradation). They exit only when ctx is cancelled.
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("jetstream consumer stopped", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		poller.Run(ctx, cfg.PLCInterval)
	}()
	go func() {
		defer wg.Done()
		runTopN(ctx, cfg.TopNRefresh, domains, hashtags, m)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-serverErr:
		runErr = err
		stop() // cancel source loops too
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	wg.Wait()
	return runErr
}

// runTopN refreshes the top-N gauges from the rolling windows on an interval,
// deleting series that fall out of the top set.
func runTopN(ctx context.Context, interval time.Duration, domains, hashtags *topn.Window, m *metrics.Metrics) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			m.TopDomains.Sync(domains.Snapshot(now))
			m.TopHashtags.Sync(hashtags.Snapshot(now))
		}
	}
}
