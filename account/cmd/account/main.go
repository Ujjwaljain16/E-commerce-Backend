package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ujjwaljain16/E-commerce-Backend/account"
	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	ctx := context.Background()

	// Initialize logger
	log := logger.New("account-service")
	log.Info(ctx, "Starting Account Service", nil)

	// Get configuration from environment
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable")
	jwtSecret := getEnv("JWT_SECRET", "your-secret-key-change-in-production")
	port := getEnv("PORT", "50051")

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
	repo := account.NewRepository(db)
	service := account.NewService(repo, jwtSecret)

	// Create gRPC server
	grpcServer := grpc.NewServer()
	pb.RegisterAccountServiceServer(grpcServer, service)

	// Enable reflection for grpcurl/grpcui
	reflection.Register(grpcServer)

	// Start server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Error(ctx, "Failed to listen", map[string]interface{}{
			"error": err.Error(),
			"port":  port,
		})
		os.Exit(1)
	}

	log.Info(ctx, "Account Service listening", map[string]interface{}{
		"port": port,
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
