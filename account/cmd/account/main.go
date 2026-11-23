package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/account"
	"github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/database"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logger.New("account-service")
	if err := run(ctx, log); err != nil {
		log.Error(context.Background(), "account service stopped", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

func run(ctx context.Context, log *logger.Logger) error {
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable")
	port := getEnv("PORT", "50051")
	metricsPort := getEnv("METRICS_PORT", "9090")
	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		return errors.New("JWT_SECRET must be set and at least 32 characters long")
	}

	db, err := database.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, account.Migrations, "migrations", "account_schema_migrations"); err != nil {
		return err
	}
	log.Info(ctx, "database ready", nil)

	service := account.NewService(account.NewRepository(db), account.NewPostgresTokenStore(db), jwtSecret)
	go pruneRefreshTokens(ctx, db, log)
	if email, password := os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD"); email != "" {
		if err := service.EnsureAdmin(ctx, email, password); err != nil {
			return err
		}
		log.Info(ctx, "bootstrap admin ensured", map[string]interface{}{"email": email})
	}

	srv := server.New("account-service", log, port, metricsPort, db.PingContext)
	pb.RegisterAccountServiceServer(srv.GRPC, service)
	return srv.Run(ctx)
}

// pruneRefreshTokens deletes long-expired refresh tokens once an hour.
func pruneRefreshTokens(ctx context.Context, db *sql.DB, log *logger.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := account.PruneExpiredRefreshTokens(ctx, db); err != nil {
				if ctx.Err() == nil {
					log.Error(ctx, "refresh token prune failed", map[string]interface{}{"error": err.Error()})
				}
			} else if n > 0 {
				log.Info(ctx, "expired refresh tokens pruned", map[string]interface{}{"rows": n})
			}
		}
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
