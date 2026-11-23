package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/graphql/graph"
	notificationpb "github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	orderpb "github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// Backends holds the gRPC connections to every service the gateway fronts.
type Backends struct {
	conns map[string]*grpc.ClientConn
}

// DialBackends creates a client connection per backend service. Connections
// are lazy, so this succeeds even when the services are not up yet.
func DialBackends(cfg Config) (*Backends, error) {
	addrs := map[string]string{
		"account":      cfg.AccountAddr,
		"catalog":      cfg.CatalogAddr,
		"order":        cfg.OrderAddr,
		"payment":      cfg.PaymentAddr,
		"notification": cfg.NotificationAddr,
	}
	b := &Backends{conns: make(map[string]*grpc.ClientConn, len(addrs))}
	for name, addr := range addrs {
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(timeoutInterceptor(cfg.GRPCTimeout)),
			// While a backend restarts, wait (up to the call timeout) for the connection instead of
			// failing immediately, so rolling deploys don't surface as UNAVAILABLE errors.
			grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		)
		if err != nil {
			b.Close()
			return nil, fmt.Errorf("create %s client for %s: %w", name, addr, err)
		}
		// Start connecting now so /readyz reflects reality before the first request.
		conn.Connect()
		b.conns[name] = conn
	}
	return b, nil
}

func timeoutInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// Resolver returns a resolver backed by these connections.
func (b *Backends) Resolver(tokens graph.TokenValidator) *graph.Resolver {
	return &graph.Resolver{
		AccountSvc:      accountpb.NewAccountServiceClient(b.conns["account"]),
		CatalogSvc:      catalogpb.NewCatalogServiceClient(b.conns["catalog"]),
		OrderSvc:        orderpb.NewOrderServiceClient(b.conns["order"]),
		PaymentSvc:      paymentpb.NewPaymentServiceClient(b.conns["payment"]),
		NotificationSvc: notificationpb.NewNotificationServiceClient(b.conns["notification"]),
		Tokens:          tokens,
	}
}

// Ready returns an error naming the first backend that is failing or closed.
func (b *Backends) Ready() error {
	for name, conn := range b.conns {
		switch conn.GetState() {
		case connectivity.TransientFailure, connectivity.Shutdown:
			return fmt.Errorf("%s service unreachable", name)
		}
	}
	return nil
}

// Close tears down every connection.
func (b *Backends) Close() error {
	var errs []error
	for _, conn := range b.conns {
		errs = append(errs, conn.Close())
	}
	return errors.Join(errs...)
}

// RouterOptions are the pieces the HTTP router needs.
type RouterOptions struct {
	Config   Config
	Resolver *graph.Resolver
	Ready    func() error
	Logger   *logger.Logger
}

// NewRouter builds the gateway's HTTP routes.
func NewRouter(opts RouterOptions) http.Handler {
	cfg := opts.Config
	gql := graph.NewHandler(opts.Resolver, graph.HandlerOptions{
		ComplexityLimit: cfg.ComplexityLimit,
		Introspection:   cfg.PlaygroundEnabled,
		Logger:          opts.Logger,
	})
	limiter := NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst, cfg.TrustProxy)

	var api http.Handler = gql
	api = Authenticate(opts.Resolver.Tokens)(api)
	api = LimitBody(api)
	api = limiter.Middleware(api)
	// CORS sits outside the limiter so browsers can read 429 responses.
	api = CORS(cfg.CORSAllowedOrigins)(api)

	mux := http.NewServeMux()
	mux.Handle("/graphql", Instrument("/graphql", api))
	// Razorpay calls this from a handful of IPs and proves itself with a signature, so it skips
	// the per-IP limiter and bearer auth.
	mux.Handle("POST /webhooks/razorpay", Instrument("/webhooks/razorpay", RazorpayWebhook(opts.Resolver.PaymentSvc, opts.Logger)))
	if cfg.PlaygroundEnabled {
		mux.Handle("GET /playground", Instrument("/playground", playground.Handler("GraphQL playground", "/graphql")))
	}
	mux.Handle("GET /healthz", Instrument("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})))
	mux.Handle("GET /readyz", Instrument("/readyz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := opts.Ready(); err != nil {
			writeStatus(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeStatus(w, http.StatusOK, "ready")
	})))
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
}

func writeStatus(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg + "\n"))
}

// NewHTTPServer returns an http.Server with timeouts suited to a public API.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}
