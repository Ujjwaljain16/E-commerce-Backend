// Package server runs a service's gRPC server alongside an HTTP server for /metrics and /healthz,
// with panic recovery, metrics and graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const shutdownTimeout = 10 * time.Second

// Server bundles the gRPC server, its health service and the HTTP side server.
type Server struct {
	GRPC     *grpc.Server
	name     string
	log      *logger.Logger
	health   *health.Server
	grpcPort string
	httpPort string
	ready    func(ctx context.Context) error
}

// New creates a gRPC server with recovery and metrics interceptors, health checks and reflection.
// ready is called by /healthz and the gRPC health check loop; nil means always ready.
func New(name string, log *logger.Logger, grpcPort, httpPort string, ready func(ctx context.Context) error) *Server {
	s := &Server{
		name:     name,
		log:      log,
		health:   health.NewServer(),
		grpcPort: grpcPort,
		httpPort: httpPort,
		ready:    ready,
	}
	s.GRPC = grpc.NewServer(grpc.ChainUnaryInterceptor(
		s.recoverInterceptor,
		metrics.UnaryServerInterceptor(name),
	))
	grpc_health_v1.RegisterHealthServer(s.GRPC, s.health)
	reflection.Register(s.GRPC)
	return s
}

func (s *Server) recoverInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error(ctx, "panic in handler", map[string]interface{}{
				"method": info.FullMethod, "panic": fmt.Sprint(r), "stack": string(debug.Stack()),
			})
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return h(ctx, req)
}

func (s *Server) checkReady(ctx context.Context) error {
	if s.ready == nil {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.ready(c)
}

// Run serves until ctx is cancelled, then drains in-flight RPCs (bounded by shutdownTimeout).
func (s *Server) Run(ctx context.Context) error {
	lis, err := net.Listen("tcp", ":"+s.grpcPort)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.checkReady(r.Context()); err != nil {
			http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	httpSrv := &http.Server{Addr: ":" + s.httpPort, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errc := make(chan error, 2)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("http server: %w", err)
		}
	}()
	go func() {
		if err := s.GRPC.Serve(lis); err != nil {
			errc <- fmt.Errorf("grpc server: %w", err)
		}
	}()
	go s.watchHealth(ctx)

	s.log.Info(ctx, s.name+" listening", map[string]interface{}{"grpc_port": s.grpcPort, "http_port": s.httpPort})

	select {
	case <-ctx.Done():
	case err = <-errc:
	}

	s.log.Info(context.Background(), "shutting down", nil)
	s.health.Shutdown()
	stopped := make(chan struct{})
	go func() { s.GRPC.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(shutdownTimeout):
		s.GRPC.Stop()
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
	return err
}

// watchHealth keeps the gRPC health status in line with the readiness check so Kubernetes
// probes and clients see a dependency outage.
func (s *Server) watchHealth(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		st := grpc_health_v1.HealthCheckResponse_SERVING
		if err := s.checkReady(ctx); err != nil {
			st = grpc_health_v1.HealthCheckResponse_NOT_SERVING
		}
		s.health.SetServingStatus("", st)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
