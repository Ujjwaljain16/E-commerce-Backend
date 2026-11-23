// Package server wires the GraphQL gateway's HTTP stack: configuration,
// gRPC clients, middleware and routes.
package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config is the gateway configuration, read from the environment.
type Config struct {
	Port               string
	JWTSecret          string
	AccountAddr        string
	CatalogAddr        string
	OrderAddr          string
	PaymentAddr        string
	NotificationAddr   string
	GRPCTimeout        time.Duration
	RateLimitRPS       float64
	RateLimitBurst     int
	TrustProxy         bool
	ComplexityLimit    int
	CORSAllowedOrigins []string
	PlaygroundEnabled  bool
	// RazorpayKeyID is the public key clients open Razorpay Checkout with. The secret stays in the
	// payment service.
	RazorpayKeyID string
}

// LoadConfig reads the configuration using getenv (normally os.Getenv).
func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		Port:             get("PORT", "8000"),
		JWTSecret:        getenv("JWT_SECRET"),
		AccountAddr:      get("ACCOUNT_SERVICE_ADDR", "localhost:50051"),
		CatalogAddr:      get("CATALOG_SERVICE_ADDR", "localhost:50052"),
		OrderAddr:        get("ORDER_SERVICE_ADDR", "localhost:50053"),
		PaymentAddr:      get("PAYMENT_SERVICE_ADDR", "localhost:50054"),
		NotificationAddr: get("NOTIFICATION_SERVICE_ADDR", "localhost:50055"),
		RazorpayKeyID:    strings.TrimSpace(getenv("RAZORPAY_KEY_ID")),
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}

	var err error
	if cfg.GRPCTimeout, err = time.ParseDuration(get("GRPC_TIMEOUT", "5s")); err != nil || cfg.GRPCTimeout <= 0 {
		return Config{}, fmt.Errorf("GRPC_TIMEOUT must be a positive duration like 5s")
	}
	if cfg.RateLimitRPS, err = strconv.ParseFloat(get("RATE_LIMIT_RPS", "20"), 64); err != nil || cfg.RateLimitRPS <= 0 {
		return Config{}, fmt.Errorf("RATE_LIMIT_RPS must be a positive number")
	}
	if cfg.RateLimitBurst, err = strconv.Atoi(get("RATE_LIMIT_BURST", "40")); err != nil || cfg.RateLimitBurst < 1 {
		return Config{}, fmt.Errorf("RATE_LIMIT_BURST must be a positive integer")
	}
	if cfg.ComplexityLimit, err = strconv.Atoi(get("COMPLEXITY_LIMIT", "200")); err != nil || cfg.ComplexityLimit < 1 {
		return Config{}, fmt.Errorf("COMPLEXITY_LIMIT must be a positive integer")
	}
	if cfg.TrustProxy, err = strconv.ParseBool(get("TRUST_PROXY", "false")); err != nil {
		return Config{}, fmt.Errorf("TRUST_PROXY must be true or false")
	}
	if cfg.PlaygroundEnabled, err = strconv.ParseBool(get("PLAYGROUND_ENABLED", "false")); err != nil {
		return Config{}, fmt.Errorf("PLAYGROUND_ENABLED must be true or false")
	}
	for _, origin := range strings.Split(getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, origin)
		}
	}
	return cfg, nil
}
