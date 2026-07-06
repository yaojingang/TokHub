package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tokhub/internal/api"
	"tokhub/internal/auth"
	"tokhub/internal/events"
	"tokhub/internal/gateway"
	"tokhub/internal/observability"
	"tokhub/internal/prober"
	"tokhub/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}

	cfg := api.LoadConfig()
	logger := observability.NewLogger(cfg.Env)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	switch cfg.Role {
	case "migrate":
		if err := store.RunMigrations(ctx, db, cfg.MigrationsDir, logger); err != nil {
			logger.Error("run migrations", "error", err)
			os.Exit(1)
		}
		logger.Info("migrations complete")
		return
	case "seed":
		if err := store.Seed(ctx, db, store.SeedConfig{
			PublicURL:        cfg.PublicURL,
			AdminEmail:       cfg.AdminEmail,
			AdminUsername:    cfg.AdminUsername,
			AdminPassword:    cfg.AdminPassword,
			RegistrationOpen: cfg.RegistrationOpen,
			SeedMode:         cfg.SeedMode,
		}, logger); err != nil {
			logger.Error("seed database", "error", err)
			os.Exit(1)
		}
		logger.Info("seed complete")
		return
	}

	repo := store.NewRepository(db)
	authSvc := auth.NewService(repo, cfg.SecretKey, cfg.SessionSecure, logger)
	probeRunner, err := prober.NewRunnerWithSecretKey(repo, logger, cfg.SeedMode != "prod", cfg.SecretKey)
	if err != nil {
		logger.Error("create probe runner", "error", err)
		os.Exit(1)
	}
	gatewayCache := gateway.NewCache(ctx, cfg.RedisURL, logger)
	if gatewayCache != nil {
		defer gatewayCache.Close()
	}

	var probeRuntime *events.ProbeRuntime
	if cfg.Role == "all" || cfg.Role == "prober" || cfg.Role == "worker" {
		enableScheduler := cfg.Role == "all" || cfg.Role == "prober"
		runtime, err := events.StartProbeRuntime(ctx, cfg.NATSURL, repo, probeRunner, logger, enableScheduler)
		if err != nil {
			logger.Error("start probe runtime", "error", err)
			os.Exit(1)
		}
		probeRuntime = runtime
		defer probeRuntime.Close()
		logger.Info("probe runtime started", "nats_url", cfg.NATSURL, "scheduler", enableScheduler)
	}

	handler := api.NewServer(cfg, repo, authSvc, probeRunner, gatewayCache, logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		logger.Info("tokhub listening", "addr", srv.Addr, "role", cfg.Role)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}

func runHealthcheck() {
	port := os.Getenv("TOKHUB_PORT")
	if port == "" {
		port = "8080"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		os.Exit(1)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		os.Exit(1)
	}
}
