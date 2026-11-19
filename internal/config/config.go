package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr        string
	LogLevel        string
	ShutdownTimeout time.Duration

	DatabaseURL             string
	DatabaseMaxConns        int32
	DatabaseMinConns        int32
	DatabaseMaxConnLifetime time.Duration
	DatabaseConnectTimeout  time.Duration
	DatabaseQueryTimeout    time.Duration

	OtelServiceName          string
	OtelExporterOTLPEndpoint string
	OtelExporterOTLPProtocol string
	OtelResourceAttributes   string
	OtelTracesSampler        string

	PositionCheckInterval time.Duration
	PositionStaleAfter    time.Duration
	PositionWorkers       int
	PositionQueueSize     int
	PushHTTPURL           string
	PushRequestTimeout    time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	cfg.HTTPAddr = getEnv("HTTP_ADDR", ":8080")
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")

	var err error
	cfg.ShutdownTimeout, err = getEnvDuration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invalid SHUTDOWN_TIMEOUT: %w", err)
	}

	cfg.DatabaseMaxConns, err = getEnvInt32("DATABASE_MAX_CONNS", 10)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_MAX_CONNS: %w", err)
	}

	cfg.DatabaseMinConns, err = getEnvInt32("DATABASE_MIN_CONNS", 2)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_MIN_CONNS: %w", err)
	}

	cfg.DatabaseMaxConnLifetime, err = getEnvDuration("DATABASE_MAX_CONN_LIFETIME", 30*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_MAX_CONN_LIFETIME: %w", err)
	}

	cfg.DatabaseConnectTimeout, err = getEnvDuration("DATABASE_CONNECT_TIMEOUT", 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_CONNECT_TIMEOUT: %w", err)
	}

	cfg.DatabaseQueryTimeout, err = getEnvDuration("DATABASE_QUERY_TIMEOUT", 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_QUERY_TIMEOUT: %w", err)
	}

	cfg.OtelServiceName = getEnv("OTEL_SERVICE_NAME", "trip-service")
	cfg.OtelExporterOTLPEndpoint = getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	cfg.OtelExporterOTLPProtocol = getEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	cfg.OtelResourceAttributes = getEnv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=tripgo,deployment.environment=local")
	cfg.OtelTracesSampler = getEnv("OTEL_TRACES_SAMPLER", "parentbased_always_on")

	cfg.PositionCheckInterval, err = getEnvDuration("POSITION_CHECK_INTERVAL", 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invalid POSITION_CHECK_INTERVAL: %w", err)
	}

	cfg.PositionStaleAfter, err = getEnvDuration("POSITION_STALE_AFTER", 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invalid POSITION_STALE_AFTER: %w", err)
	}

	cfg.PositionWorkers, err = getEnvInt("POSITION_WORKERS", 4)
	if err != nil {
		return nil, fmt.Errorf("invalid POSITION_WORKERS: %w", err)
	}

	cfg.PositionQueueSize, err = getEnvInt("POSITION_QUEUE_SIZE", 100)
	if err != nil {
		return nil, fmt.Errorf("invalid POSITION_QUEUE_SIZE: %w", err)
	}

	cfg.PushHTTPURL = getEnv("PUSH_HTTP_URL", "http://localhost:8090")

	cfg.PushRequestTimeout, err = getEnvDuration("PUSH_REQUEST_TIMEOUT", 1*time.Second)
	if err != nil {
		return nil, fmt.Errorf("invalid PUSH_REQUEST_TIMEOUT: %w", err)
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue, nil
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", key, val, err)
	}
	return d, nil
}

func getEnvInt32(key string, defaultValue int32) (int32, error) {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue, nil
	}
	n, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", key, val, err)
	}
	return int32(n), nil
}

func getEnvInt(key string, defaultValue int) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", key, val, err)
	}
	return n, nil
}
