package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/graphql/server"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/auth"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logger.New("graphql-gateway")

	cfg, err := server.LoadConfig(os.Getenv)
	if err != nil {
		log.Error(ctx, "Invalid configuration", map[string]interface{}{"error": err.Error()})
		return err
	}

	backends, err := server.DialBackends(cfg)
	if err != nil {
		log.Error(ctx, "Failed to set up gRPC clients", map[string]interface{}{"error": err.Error()})
		return err
	}
	defer func() {
		if err := backends.Close(); err != nil {
			log.Warn(context.Background(), "Closing gRPC connections failed", map[string]interface{}{"error": err.Error()})
		}
	}()

	// Durations only matter for issuing tokens; the gateway just validates them.
	tokens := auth.NewTokenService(cfg.JWTSecret, 15*time.Minute, 7*24*time.Hour)
	router := server.NewRouter(server.RouterOptions{
		Config:   cfg,
		Resolver: backends.Resolver(tokens),
		Ready:    backends.Ready,
		Logger:   log,
	})
	srv := server.NewHTTPServer(net.JoinHostPort("", cfg.Port), router)

	serveErr := make(chan error, 1)
	go func() {
		log.Info(ctx, "GraphQL gateway listening", map[string]interface{}{
			"port":       cfg.Port,
			"playground": cfg.PlaygroundEnabled,
		})
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error(ctx, "HTTP server failed", map[string]interface{}{"error": err.Error()})
			return err
		}
		return nil
	case <-ctx.Done():
	}

	log.Info(context.Background(), "Shutting down gracefully", nil)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(shutdownCtx, "Graceful shutdown failed", map[string]interface{}{"error": err.Error()})
		return err
	}
	return nil
}
