package main

import (
	"context"
	"errors"
	"fmt"
	"gomario/lib/config"
	"gomario/lib/service"
	"gomario/lib/telemetry"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultIdleTimeout    = time.Minute
	defaultReadTimeout    = 5 * time.Second
	defaultWriteTimeout   = 10 * time.Second
	defaultShutdownPeriod = 30 * time.Second
)

func main() {

	cfg, err := config.Configuration()
	if err != nil {
		log.Fatalf("Error reading application configuration: %s", err)
	}

	// Initialise OpenTelemetry. The shutdown function flushes all pending
	// spans and metrics — it must be called before the process exits.
	ctx := context.Background()
	telShutdown, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		log.Fatalf("Error initialising telemetry: %s", err)
	}

	svc, err := service.NewService(cfg)
	if err != nil {
		log.Fatalf("Error creating new service: %s", err)
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.HttpHost, cfg.HttpPort),
		Handler:      svc.Handler, // otelhttp-wrapped mux
		ErrorLog:     slog.NewLogLogger(svc.Logger.Handler(), slog.LevelWarn),
		IdleTimeout:  defaultIdleTimeout,
		ReadTimeout:  defaultReadTimeout,
		WriteTimeout: defaultWriteTimeout,
	}

	shutdownErrorChan := make(chan error)

	go func() {
		quitChan := make(chan os.Signal, 1)
		signal.Notify(quitChan, syscall.SIGINT, syscall.SIGTERM)
		<-quitChan

		ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownPeriod)
		defer cancel()

		// Shut down HTTP server first, then flush telemetry.
		if err := httpServer.Shutdown(ctx); err != nil {
			shutdownErrorChan <- err
			return
		}
		shutdownErrorChan <- telShutdown(ctx)
	}()

	svc.Logger.Info("starting server", slog.Group("server", "addr", httpServer.Addr))

	if err := httpServer.ListenAndServe(); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			svc.Logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}

	err = <-shutdownErrorChan
	if err != nil {
		log.Fatalf("error shutting down server: %v", err)
	}

	svc.Logger.Info("stopped server", slog.Group("server", "addr", httpServer.Addr))
}
