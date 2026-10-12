package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestInitializeExportsTracesAndMetricsUsingOTLPHTTP(t *testing.T) {
	ctx := context.Background()
	received := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		received <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)

	metrics, shutdown, err := Initialize(ctx, "test-service")
	if err != nil {
		t.Fatalf("initialize telemetry: %v", err)
	}
	tracer := otel.Tracer("test-service")
	_, span := tracer.Start(ctx, "test-operation")
	span.End()
	metrics.RecordIngestionLatency(ctx, 75*time.Millisecond)
	metrics.RecordProcessing(ctx, "duplicate", 125*time.Millisecond)
	metrics.RecordCheckpoint(ctx, 25*time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shut down telemetry: %v", err)
	}

	seen := make(map[string]bool)
	for range 2 {
		select {
		case path := <-received:
			seen[path] = true
		case <-shutdownCtx.Done():
			t.Fatalf("timed out waiting for OTLP export requests: %v", shutdownCtx.Err())
		}
	}
	if !seen["/v1/traces"] || !seen["/v1/metrics"] {
		t.Fatalf("missing OTLP exports: %#v", seen)
	}
}
