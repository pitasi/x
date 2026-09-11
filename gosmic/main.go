package main

import (
	"html/template"
	"net/http"
	"os"
	"time"

	"anto.pt/x/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"anto.pt/x/gosmic/antoph"
	"anto.pt/x/gosmic/antopt"
	"anto.pt/x/gosmic/httpx"
)

var logger = log.Module("gosmic")

func main() {
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = "0.0.0.0:8080"
	}

	mux := http.NewServeMux()

	httpx.RegisterWebsite("anto.pt", &antopt.Website{
		Colors: []template.CSS{
			"#b9b5ff", // periwinkle (default)
			"#ff91bc", // bubblegum
			"#ffe66b", // sunshine
			"#91dfcf", // sea glass
			"#a8d8ff", // sky blue
			"#f5f6f8", // quiet mode
		},
	}, mux)
	httpx.RegisterWebsite("anto.ph", antoph.Website{}, mux)

	handler := httpx.MetricsInc(mux)
	handler = httpx.RewriteHost(handler)

	s := http.Server{
		Addr:        listenAddr,
		Handler:     handler,
		ReadTimeout: 10 * time.Second,
	}
	go ServeMetrics(":9090")

	logger.Info("listening", "addr", s.Addr)
	logger.Error("listening", "err", s.ListenAndServe())
}

func ServeMetrics(addr string) {
	logger := log.Module("metrics_server")
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{
		Addr:         addr,
		Handler:      metricsMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	logger.Info("listening", "addr", addr)
	logger.Error("listening", "err", metricsServer.ListenAndServe())
}
