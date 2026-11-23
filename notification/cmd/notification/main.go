package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/notification"
	"github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/database"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const serviceName = "notification-service"

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logger.New(serviceName)
	fail := func(msg string, err error) error {
		log.Error(ctx, msg, map[string]interface{}{"error": err.Error()})
		return err
	}

	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable")
	brokers := kafka.Brokers(getEnv("KAFKA_BROKERS", "localhost:9092"))
	accountAddr := getEnv("ACCOUNT_SERVICE_ADDR", "localhost:50051")
	port := getEnv("PORT", "50055")
	metricsPort := getEnv("METRICS_PORT", "9094")

	sender, err := newSender(log)
	if err != nil {
		return fail("Invalid SMTP configuration", err)
	}

	db, err := database.Open(ctx, dbURL)
	if err != nil {
		return fail("Failed to connect to database", err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db, notification.Migrations, "migrations", "notification_schema_migrations"); err != nil {
		return fail("Failed to run migrations", err)
	}

	accountConn, err := grpc.NewClient(accountAddr, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)))
	if err != nil {
		return fail("Failed to create account service client", err)
	}
	defer accountConn.Close()

	consumer := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers,
		GroupID: "notification-service",
		Topics:  notification.ConsumedTopics,
		Service: serviceName,
	}, log)
	defer consumer.Close()

	service := notification.NewService(
		notification.NewRepository(db), accountpb.NewAccountServiceClient(accountConn), sender, log)

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		metrics.UnaryServerInterceptor(serviceName),
		recoverPanics(log),
	))
	pb.RegisterNotificationServiceServer(grpcServer, service)
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("notification.NotificationService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	reflection.Register(grpcServer)

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fail("Failed to listen", err)
	}

	httpServer := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           httpHandler(db),
		ReadHeaderTimeout: 5 * time.Second,
	}

	var wg sync.WaitGroup
	serveErr := make(chan error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := consumer.Run(ctx, service.HandleMessage); err != nil {
			log.Error(ctx, "Kafka consumer stopped", map[string]interface{}{"error": err.Error()})
		}
	}()
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("metrics server: %w", err)
		}
	}()
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			serveErr <- fmt.Errorf("grpc server: %w", err)
		}
	}()

	log.Info(ctx, "Notification Service listening", map[string]interface{}{"port": port, "metrics_port": metricsPort})

	var runErr error
	select {
	case <-ctx.Done():
		log.Info(context.Background(), "Shutting down gracefully", nil)
	case runErr = <-serveErr:
		log.Error(context.Background(), "Server failed", map[string]interface{}{"error": runErr.Error()})
		stop()
	}

	healthServer.Shutdown()
	shutdown(grpcServer, httpServer)
	wg.Wait()
	return runErr
}

// newSender delivers through SMTP when SMTP_HOST is set and only logs emails otherwise.
func newSender(log *logger.Logger) (notification.Sender, error) {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return notification.NewLogSender(log), nil
	}
	port, err := strconv.Atoi(getEnv("SMTP_PORT", "587"))
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP_PORT: %w", err)
	}
	return notification.NewSMTPSender(notification.SMTPConfig{
		Host:     host,
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	})
}

// shutdown lets in-flight RPCs finish, but never waits more than 10s for slow clients.
func shutdown(grpcServer *grpc.Server, httpServer *http.Server) {
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		grpcServer.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func httpHandler(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func recoverPanics(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error(ctx, "Panic in gRPC handler", map[string]interface{}{
					"method": info.FullMethod, "panic": fmt.Sprint(r), "stack": string(debug.Stack()),
				})
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
