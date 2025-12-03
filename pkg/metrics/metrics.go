// Package metrics provides Prometheus instrumentation for microservices.
// It includes pre-configured metrics for gRPC, HTTP, and database operations.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics are intentionally global for registration with the default registry.
//
//nolint:gochecknoglobals // Prometheus metrics must be global variables
var (
	// GRPCRequestsTotal tracks total number of gRPC requests
	GRPCRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_requests_total",
			Help: "Total number of gRPC requests",
		},
		[]string{"service", "method", "status"},
	)

	// GRPCRequestDuration tracks gRPC request duration in seconds
	GRPCRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "method"},
	)

	// HTTPRequestsTotal tracks total number of HTTP requests
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"service", "endpoint", "method", "status"},
	)

	// HTTPRequestDuration tracks HTTP request duration in seconds
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "endpoint", "method"},
	)

	// DBQueryDuration tracks database query duration in seconds
	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "query_type"},
	)

	// CacheHitsTotal tracks total cache hits
	CacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Total cache hits",
		},
		[]string{"service", "cache_key_type"},
	)

	// CacheMissesTotal tracks total cache misses
	CacheMissesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_misses_total",
			Help: "Total cache misses",
		},
		[]string{"service", "cache_key_type"},
	)

	// KafkaMessagesProduced tracks total Kafka messages produced
	KafkaMessagesProduced = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_messages_produced_total",
			Help: "Total Kafka messages produced",
		},
		[]string{"service", "topic"},
	)

	// KafkaMessagesConsumed tracks total Kafka messages consumed
	KafkaMessagesConsumed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_messages_consumed_total",
			Help: "Total Kafka messages consumed",
		},
		[]string{"service", "topic", "status"},
	)
)
