package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/order"
	"github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/database"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/outbox"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logger.New("order-service")
	if err := run(ctx, log); err != nil {
		log.Error(context.Background(), "order service stopped", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

func run(ctx context.Context, log *logger.Logger) error {
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable")
	port := getEnv("PORT", "50053")
	metricsPort := getEnv("METRICS_PORT", "9092")
	catalogAddr := getEnv("CATALOG_SERVICE_ADDR", "localhost:50052")
	brokers := kafka.Brokers(getEnv("KAFKA_BROKERS", "localhost:9092"))

	db, err := database.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, order.Migrations, "migrations", "order_schema_migrations"); err != nil {
		return err
	}
	log.Info(ctx, "database ready", nil)

	conn, err := grpc.NewClient(catalogAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("catalog client: %w", err)
	}
	defer conn.Close()

	repo := order.NewRepository(db)
	producer := kafka.NewProducer(brokers, "order-service")
	defer producer.Close()
	consumer := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers, GroupID: "order-service", Topics: order.PaymentTopics, Service: "order-service",
	}, log)
	defer consumer.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); outbox.NewRelay(db, order.OutboxTable, producer, log).Run(ctx) }()
	go func() {
		defer wg.Done()
		if err := consumer.Run(ctx, order.NewPaymentEventHandler(repo, log).Handle); err != nil {
			log.Error(ctx, "payment consumer stopped", map[string]interface{}{"error": err.Error()})
		}
	}()

	srv := server.New("order-service", log, port, metricsPort, db.PingContext)
	pb.RegisterOrderServiceServer(srv.GRPC, order.NewService(repo, catalogpb.NewCatalogServiceClient(conn), log))
	err = srv.Run(ctx)
	wg.Wait()
	return err
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
