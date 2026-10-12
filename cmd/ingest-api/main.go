package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alulema/telemetry-ingest-iot/internal/api"
	"github.com/alulema/telemetry-ingest-iot/internal/cloud/azure"
	"github.com/alulema/telemetry-ingest-iot/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (runErr error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	metrics, shutdownTelemetry, err := observability.Initialize(context.Background(), "ingest-api")
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shut down telemetry: %w", err))
		}
	}()

	credential, err := azure.NewDefaultCredential()
	if err != nil {
		return err
	}
	publisher, err := azure.NewEventPublisher(requiredEnv("EVENTHUB_NAMESPACE"), requiredEnv("EVENTHUB_NAME"), credential)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := publisher.Close(closeCtx); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	store, err := azure.NewCosmosStore(requiredEnv("COSMOS_ENDPOINT"), requiredEnv("COSMOS_DATABASE"), requiredEnv("COSMOS_CONTAINER"), credential)
	if err != nil {
		return err
	}
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           otelhttp.NewHandler(api.NewCloudHandler(publisher, store, requiredEnv("INGESTION_TOKEN"), metrics).Routes(), "http.server"),
		ReadHeaderTimeout: 5 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		log.Printf("cloud ingestion API listening on %s", address)
		done <- server.ListenAndServe()
	}()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
	}
	return nil
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatal(fmt.Errorf("required environment variable %s is not set", name))
	}
	return value
}
