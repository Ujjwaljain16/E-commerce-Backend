package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog"
	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/cache"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/database"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logger.New("catalog-service")
	if err := run(ctx, log); err != nil {
		log.Error(context.Background(), "catalog service stopped", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

func run(ctx context.Context, log *logger.Logger) error {
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable")
	port := getEnv("PORT", "50052")
	metricsPort := getEnv("METRICS_PORT", "9091")
	redisAddr := os.Getenv("REDIS_ADDR")
	brokers := kafka.Brokers(getEnv("KAFKA_BROKERS", "localhost:9092"))

	db, err := database.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, catalog.Migrations, "migrations", "catalog_schema_migrations"); err != nil {
		return err
	}
	log.Info(ctx, "database ready", nil)

	repo := catalog.NewPostgresRepository(db, log)
	if redisAddr != "" {
		rc := cache.NewRedis(redisAddr, os.Getenv("REDIS_PASSWORD"), 0)
		defer rc.Close()
		if err := rc.Ping(ctx); err != nil {
			log.Warn(ctx, "redis unreachable at startup; cache calls will fall through to postgres", map[string]interface{}{"error": err.Error()})
		}
		repo = catalog.NewCachedRepository(repo, rc, log)
		log.Info(ctx, "product cache enabled", map[string]interface{}{"redis": redisAddr})
	}

	consumer := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers, GroupID: "catalog-service", Topics: []string{events.TopicOrderCancelled}, Service: "catalog-service",
	}, log)
	defer consumer.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := consumer.Run(ctx, catalog.NewOrderCancelledHandler(repo, log).Handle); err != nil {
			log.Error(ctx, "order event consumer stopped", map[string]interface{}{"error": err.Error()})
		}
	}()

	srv := server.New("catalog-service", log, port, metricsPort, db.PingContext)
	pb.RegisterCatalogServiceServer(srv.GRPC, catalog.NewService(repo, log))
	err = srv.Run(ctx)
	<-done
	return err
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
