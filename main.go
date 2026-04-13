package main

import (
	"context"
	"errors"
	"fmt"
	"gomario/lib/config"
	"gomario/lib/service"
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

	service, err := service.NewService(cfg)
	if err != nil {
		log.Fatalf("Error creating new service: %s", err)
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.HttpHost, cfg.HttpPort),
		Handler:      service.Muxer,
		ErrorLog:     slog.NewLogLogger(service.Logger.Handler(), slog.LevelWarn),
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

		shutdownErrorChan <- httpServer.Shutdown(ctx)
	}()

	service.Logger.Info("starting server", slog.Group("server", "addr", httpServer.Addr))

	if err := httpServer.ListenAndServe(); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			service.Logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}

	err = <-shutdownErrorChan
	if err != nil {
		log.Fatalf("error shutting down server: %v", err)
	}

	service.Logger.Info("stopped server", slog.Group("server", "addr", httpServer.Addr))
}
