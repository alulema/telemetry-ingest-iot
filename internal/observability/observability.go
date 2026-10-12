package observability

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Metrics struct {
	ingestionEvents   metric.Int64Counter
	ingestionLatency  metric.Float64Histogram
	processingEvents  metric.Int64Counter
	processingSeconds metric.Float64Histogram
	checkpointSeconds metric.Float64Histogram
}

func Initialize(ctx context.Context, serviceName string) (*Metrics, func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		metrics, err := newMetrics()
		if err != nil {
			return nil, nil, err
		}
		return metrics, func(context.Context) error { return nil }, nil
	}
	endpoint = strings.TrimRight(endpoint, "/")

	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"))
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint+"/v1/metrics"))
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("create OTLP metric exporter: %w", err),
			traceExporter.Shutdown(ctx),
		)
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.namespace", "telemetry-ingest-iot"),
	)
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(metricProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	metrics, err := newMetrics()
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("create telemetry metrics: %w", err),
			traceProvider.Shutdown(ctx),
			metricProvider.Shutdown(ctx),
		)
	}
	shutdown := func(shutdownCtx context.Context) error {
		return errors.Join(traceProvider.Shutdown(shutdownCtx), metricProvider.Shutdown(shutdownCtx))
	}
	return metrics, shutdown, nil
}

func newMetrics() (*Metrics, error) {
	meter := otel.Meter("github.com/alulema/telemetry-ingest-iot")
	ingestionEvents, err := meter.Int64Counter(
		"telemetry.ingestion.events",
		metric.WithDescription("Telemetry events accepted, rejected, or unavailable at the ingestion API."),
	)
	if err != nil {
		return nil, fmt.Errorf("create ingestion event counter: %w", err)
	}
	processingEvents, err := meter.Int64Counter(
		"telemetry.processing.events",
		metric.WithDescription("Telemetry events applied, deduplicated, rejected, quarantined, or failed."),
	)
	if err != nil {
		return nil, fmt.Errorf("create processing event counter: %w", err)
	}
	ingestionLatency, err := meter.Float64Histogram(
		"telemetry.ingestion.latency",
		metric.WithDescription("Time from API receipt to event processing."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create ingestion latency histogram: %w", err)
	}
	processingSeconds, err := meter.Float64Histogram(
		"telemetry.processing.duration",
		metric.WithDescription("Time spent processing one telemetry event."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create processing duration histogram: %w", err)
	}
	checkpointSeconds, err := meter.Float64Histogram(
		"telemetry.checkpoint.duration",
		metric.WithDescription("Time spent saving an Event Hubs partition checkpoint."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create checkpoint duration histogram: %w", err)
	}
	return &Metrics{
		ingestionEvents:   ingestionEvents,
		ingestionLatency:  ingestionLatency,
		processingEvents:  processingEvents,
		processingSeconds: processingSeconds,
		checkpointSeconds: checkpointSeconds,
	}, nil
}

func (m *Metrics) RecordIngestionLatency(ctx context.Context, duration time.Duration) {
	if m != nil && duration >= 0 {
		m.ingestionLatency.Record(ctx, duration.Seconds())
	}
}

func (m *Metrics) RecordIngestion(ctx context.Context, outcome string) {
	if m != nil {
		m.ingestionEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}

func (m *Metrics) RecordProcessing(ctx context.Context, outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	m.processingEvents.Add(ctx, 1, attrs)
	if duration > 0 {
		m.processingSeconds.Record(ctx, duration.Seconds(), attrs)
	}
}

func (m *Metrics) RecordCheckpoint(ctx context.Context, duration time.Duration) {
	if m != nil {
		m.checkpointSeconds.Record(ctx, duration.Seconds())
	}
}
