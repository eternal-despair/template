package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/eternal-despair/template/internal/config"
)

type Telemetry struct {
	Tracer  trace.Tracer
	Meter   metric.Meter
	Metrics *Metrics
	cleanup func(ctx context.Context) error
}

type Metrics struct {
	TripsCreated             metric.Int64Counter
	TripsCompleted           metric.Int64Counter
	RequestDuration          metric.Float64Histogram
	ActiveRequests           metric.Int64UpDownCounter
	PositionsSaved           metric.Int64Counter
	StaleChecks              metric.Int64Counter
	PositionRequests         metric.Int64Counter
	PositionRequestsDuration metric.Float64Histogram
	QueueSizeGauge           metric.Int64Gauge
	ActiveWorkersGauge       metric.Int64Gauge
}

func (m *Metrics) TripCreated(ctx context.Context) {
	if m != nil && m.TripsCreated != nil {
		m.TripsCreated.Add(ctx, 1)
	}
}

func (m *Metrics) TripCompleted(ctx context.Context) {
	if m != nil && m.TripsCompleted != nil {
		m.TripsCompleted.Add(ctx, 1)
	}
}

func (m *Metrics) PositionSaved(ctx context.Context) {
	if m != nil && m.PositionsSaved != nil {
		m.PositionsSaved.Add(ctx, 1)
	}
}

func (m *Metrics) RecordStaleCheck(ctx context.Context, result string) {
	if m != nil && m.StaleChecks != nil {
		m.StaleChecks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	}
}

func (m *Metrics) RecordPositionRequest(ctx context.Context, result string) {
	if m != nil && m.PositionRequests != nil {
		m.PositionRequests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	}
}

func (m *Metrics) RecordPositionRequestDuration(ctx context.Context, result string, durationSec float64) {
	if m != nil && m.PositionRequestsDuration != nil {
		m.PositionRequestsDuration.Record(ctx, durationSec, metric.WithAttributes(attribute.String("result", result)))
	}
}

func (m *Metrics) RecordQueueSize(ctx context.Context, size int64) {
	if m != nil && m.QueueSizeGauge != nil {
		m.QueueSizeGauge.Record(ctx, size)
	}
}

func (m *Metrics) RecordActiveWorkers(ctx context.Context, count int64) {
	if m != nil && m.ActiveWorkersGauge != nil {
		m.ActiveWorkersGauge.Record(ctx, count)
	}
}

func (m *Metrics) RecordRequestDuration(ctx context.Context, method, route string, statusCode int, durationSec float64) {
	if m != nil && m.RequestDuration != nil {
		m.RequestDuration.Record(ctx, durationSec, metric.WithAttributes(
			attribute.String("http.request.method", method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", statusCode),
		))
	}
}

func (m *Metrics) IncActiveRequests(ctx context.Context, method, route string) {
	if m != nil && m.ActiveRequests != nil {
		m.ActiveRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("http.request.method", method),
			attribute.String("http.route", route),
		))
	}
}

func (m *Metrics) DecActiveRequests(ctx context.Context, method, route string) {
	if m != nil && m.ActiveRequests != nil {
		m.ActiveRequests.Add(ctx, -1, metric.WithAttributes(
			attribute.String("http.request.method", method),
			attribute.String("http.route", route),
		))
	}
}

func Setup(ctx context.Context, cfg *config.Config) (*Telemetry, error) {
	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	traceExporterOpts := []otlptracehttp.Option{}
	if cfg.OtelExporterOTLPEndpoint != "" {
		traceURL := strings.TrimRight(cfg.OtelExporterOTLPEndpoint, "/")
		if !strings.HasSuffix(traceURL, "/v1/traces") {
			traceURL += "/v1/traces"
		}
		traceExporterOpts = append(traceExporterOpts, otlptracehttp.WithEndpointURL(traceURL))
	}
	traceExporter, err := otlptracehttp.New(ctx, traceExporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	metricExporterOpts := []otlpmetrichttp.Option{}
	if cfg.OtelExporterOTLPEndpoint != "" {
		metricURL := strings.TrimRight(cfg.OtelExporterOTLPEndpoint, "/")
		if !strings.HasSuffix(metricURL, "/v1/metrics") {
			metricURL += "/v1/metrics"
		}
		metricExporterOpts = append(metricExporterOpts, otlpmetrichttp.WithEndpointURL(metricURL))
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricExporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	meter := mp.Meter(cfg.OtelServiceName)
	metricsHelper, err := initMetrics(meter)
	if err != nil {
		return nil, fmt.Errorf("init metrics: %w", err)
	}

	tracer := tp.Tracer(cfg.OtelServiceName)

	cleanup := func(shutdownCtx context.Context) error {
		var errs []error
		if err := tp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown tracer provider: %w", err))
		}
		if err := mp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown meter provider: %w", err))
		}
		return errors.Join(errs...)
	}

	return &Telemetry{
		Tracer:  tracer,
		Meter:   meter,
		Metrics: metricsHelper,
		cleanup: cleanup,
	}, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t.cleanup != nil {
		return t.cleanup(ctx)
	}
	return nil
}

func initMetrics(meter metric.Meter) (*Metrics, error) {
	tripsCreated, err := meter.Int64Counter(
		"tripgo.trips.created",
		metric.WithDescription("Total number of created trips"),
		metric.WithUnit("{trip}"),
	)
	if err != nil {
		return nil, err
	}

	tripsCompleted, err := meter.Int64Counter(
		"tripgo.trips.completed",
		metric.WithDescription("Total number of completed trips"),
		metric.WithUnit("{trip}"),
	)
	if err != nil {
		return nil, err
	}

	reqDuration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	activeReqs, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of active HTTP server requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	positionsSaved, err := meter.Int64Counter(
		"tripgo.positions.saved",
		metric.WithDescription("Total number of saved positions"),
		metric.WithUnit("{position}"),
	)
	if err != nil {
		return nil, err
	}

	staleChecks, err := meter.Int64Counter(
		"tripgo.stale_checks",
		metric.WithDescription("Results of stale trip checks by scanner"),
	)
	if err != nil {
		return nil, err
	}

	positionRequests, err := meter.Int64Counter(
		"tripgo.position_requests",
		metric.WithDescription("Results of position requests to push service"),
	)
	if err != nil {
		return nil, err
	}

	posReqDuration, err := meter.Float64Histogram(
		"tripgo.position_requests.duration",
		metric.WithDescription("Duration of position request calls to push service"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	queueSizeGauge, err := meter.Int64Gauge(
		"tripgo.position_requests.queue.size",
		metric.WithDescription("Current size of the position request queue"),
		metric.WithUnit("{task}"),
	)
	if err != nil {
		return nil, err
	}

	activeWorkersGauge, err := meter.Int64Gauge(
		"tripgo.position_requests.workers.active",
		metric.WithDescription("Current count of active position request workers"),
		metric.WithUnit("{worker}"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		TripsCreated:             tripsCreated,
		TripsCompleted:           tripsCompleted,
		RequestDuration:          reqDuration,
		ActiveRequests:           activeReqs,
		PositionsSaved:           positionsSaved,
		StaleChecks:              staleChecks,
		PositionRequests:         positionRequests,
		PositionRequestsDuration: posReqDuration,
		QueueSizeGauge:           queueSizeGauge,
		ActiveWorkersGauge:       activeWorkersGauge,
	}, nil
}

func buildResource(ctx context.Context, cfg *config.Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.OtelServiceName),
		semconv.ServiceVersionKey.String("0.1.0"),
		attribute.String("service.namespace", "tripgo"),
		attribute.String("deployment.environment", "local"),
	}

	if cfg.OtelResourceAttributes != "" {
		pairs := strings.Split(cfg.OtelResourceAttributes, ",")
		for _, pair := range pairs {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) == 2 {
				attrs = append(attrs, attribute.String(parts[0], parts[1]))
			}
		}
	}

	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithSchemaURL(semconv.SchemaURL),
	)
}
