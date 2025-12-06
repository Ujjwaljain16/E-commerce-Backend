package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog"
	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/metrics"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func main() {
	ctx := context.Background()

	// Initialize logger
	log := logger.New("catalog-service")
	log.Info(ctx, "Starting Catalog Service", nil)

	// Get configuration from environment
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/ecommerce?sslmode=disable")
	port := getEnv("PORT", "50052")
	metricsPort := getEnv("METRICS_PORT", "9091")

	// Connect to database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Error(ctx, "Failed to connect to database", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Error(ctx, "Failed to ping database", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}
	log.Info(ctx, "Connected to database", nil)

	// Create repository and service
	repo := catalog.NewPostgresRepository(db, log)
	service := catalog.NewService(repo, log)

	// Create gRPC server with metrics interceptor
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(metrics.UnaryServerInterceptor("catalog-service")),
	)
	pb.RegisterCatalogServiceServer(grpcServer, service)

	// Register health check service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("catalog.CatalogService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for grpcurl/grpcui
	reflection.Register(grpcServer)

	// Start Prometheus metrics HTTP server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		metricsAddr := fmt.Sprintf(":%s", metricsPort)
		log.Info(ctx, "Metrics server listening", map[string]interface{}{
			"port": metricsPort,
		})
		if err := http.ListenAndServe(metricsAddr, nil); err != nil {
			log.Error(ctx, "Metrics server failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Start gRPC server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Error(ctx, "Failed to listen", map[string]interface{}{
			"error": err.Error(),
			"port":  port,
		})
		os.Exit(1)
	}

	log.Info(ctx, "Catalog Service listening", map[string]interface{}{
		"port":         port,
		"metrics_port": metricsPort,
	})

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Info(ctx, "Shutting down gracefully", nil)
		grpcServer.GracefulStop()
		repo.Close()
	}()

	// Start serving
	if err := grpcServer.Serve(listener); err != nil {
		log.Error(ctx, "Failed to serve", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
