package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gitlab.com/torn-city/go-torn-client/client"
	"gitlab.com/torn-city/torn-exporter/internal/exporter"
)

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	apiKey := os.Getenv("TORN_API_KEY")
	if apiKey == "" {
		logger.Error("TORN_API_KEY is required (set it in .env or the environment)")
		os.Exit(1)
	}

	addr := envOr("TORN_EXPORTER_ADDR", ":9101")
	userAgent := envOr("TORN_USER_AGENT", "gitlab.com/torn-city/torn-city-prometheus-exporter")
	comment := envOr("TORN_COMMENT", "gitlab.com/torn-city/torn-city-prometheus-exporter")
	interval, err := time.ParseDuration(envOr("TORN_EXPORTER_INTERVAL", "60s"))
	if err != nil {
		logger.Error("invalid TORN_EXPORTER_INTERVAL", "err", err)
		os.Exit(1)
	}

	api, err := client.New(apiKey,
		client.WithUserAgent(userAgent),
		client.WithComment(comment),
		client.WithRequestsPerMinute(60),
	)
	if err != nil {
		logger.Error("create torn client", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	collector := exporter.NewCollector(api, interval, logger)
	collector.Start(ctx)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("torn prometheus exporter listening",
		"addr", addr,
		"interval", interval.String(),
		"metrics", "http://"+displayAddr(addr)+"/metrics",
	)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "127.0.0.1" + addr
	}
	return addr
}
