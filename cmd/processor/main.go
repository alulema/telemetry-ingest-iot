package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	eventhub "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"

	"github.com/alulema/telemetry-ingest-iot/internal/cloud/azure"
	"github.com/alulema/telemetry-ingest-iot/internal/observability"
	"github.com/alulema/telemetry-ingest-iot/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	metrics, shutdownTelemetry, err := observability.Initialize(context.Background(), "processor")
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			log.Printf("shut down telemetry: %v", err)
		}
	}()

	credential, err := azure.NewDefaultCredential()
	if err != nil {
		return err
	}
	namespace := requiredEnv("EVENTHUB_NAMESPACE")
	hubName := requiredEnv("EVENTHUB_NAME")
	consumerGroup := requiredEnv("EVENTHUB_CONSUMER_GROUP")
	consumer, err := eventhub.NewConsumerClient(namespace, hubName, consumerGroup, credential, nil)
	if err != nil {
		return fmt.Errorf("create Event Hubs consumer: %w", err)
	}
	defer consumer.Close(context.Background())

	checkpointStore, err := azure.NewCheckpointStore(requiredEnv("CHECKPOINT_STORAGE_ACCOUNT"), requiredEnv("CHECKPOINT_CONTAINER"), credential)
	if err != nil {
		return err
	}
	processor, err := eventhub.NewProcessor(consumer, checkpointStore, &eventhub.ProcessorOptions{
		StartPositions: eventhub.StartPositions{
			Default: eventhub.StartPosition{Earliest: boolPtr(true), Inclusive: true},
		},
	})
	if err != nil {
		return fmt.Errorf("create Event Hubs processor: %w", err)
	}
	store, err := azure.NewCosmosStore(requiredEnv("COSMOS_ENDPOINT"), requiredEnv("COSMOS_DATABASE"), requiredEnv("COSMOS_CONTAINER"), credential)
	if err != nil {
		return err
	}
	quarantine, err := azure.NewQuarantineSender(requiredEnv("SERVICEBUS_NAMESPACE"), requiredEnv("SERVICEBUS_QUEUE"), credential)
	if err != nil {
		return err
	}
	defer func() {
		if err := quarantine.Close(context.Background()); err != nil {
			log.Printf("close Service Bus quarantine sender: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	processorDone := make(chan error, 1)
	go func() {
		processorDone <- processor.Run(runCtx)
		cancel()
	}()

	var workers sync.WaitGroup
	for runCtx.Err() == nil {
		partition := processor.NextPartitionClient(runCtx)
		if partition == nil {
			break
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			processPartition(runCtx, partition, store, quarantine, metrics)
		}()
	}
	cancel()
	workers.Wait()
	if err := <-processorDone; err != nil {
		return fmt.Errorf("run Event Hubs processor: %w", err)
	}
	return nil
}

func processPartition(ctx context.Context, partition *eventhub.ProcessorPartitionClient, store *azure.CosmosStore, quarantine *azure.QuarantineSender, metrics *observability.Metrics) {
	defer func() {
		if err := partition.Close(context.Background()); err != nil {
			log.Printf("close Event Hubs partition %s: %v", partition.PartitionID(), err)
		}
	}()
	for ctx.Err() == nil {
		receiveCtx, cancelReceive := context.WithTimeout(ctx, 5*time.Second)
		events, err := partition.ReceiveEvents(receiveCtx, 32, nil)
		cancelReceive()
		if ctx.Err() != nil {
			return
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("receive Event Hubs partition %s: %v", partition.PartitionID(), err)
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		for _, received := range events {
			if err := processEvent(ctx, received.Body, store, quarantine, metrics); err != nil {
				log.Printf("process Event Hubs event in partition %s: %v", partition.PartitionID(), err)
				if !retry(ctx, func() error { return processEvent(ctx, received.Body, store, quarantine, metrics) }) {
					return
				}
			}
		}
		if len(events) == 0 {
			continue
		}
		checkpointStarted := time.Now()
		if err := partition.UpdateCheckpoint(ctx, events[len(events)-1], nil); err != nil {
			log.Printf("update checkpoint for Event Hubs partition %s: %v", partition.PartitionID(), err)
			if !retry(ctx, func() error { return partition.UpdateCheckpoint(ctx, events[len(events)-1], nil) }) {
				return
			}
		}
		metrics.RecordCheckpoint(ctx, time.Since(checkpointStarted))
	}
}

func processEvent(ctx context.Context, body []byte, store *azure.CosmosStore, quarantine *azure.QuarantineSender, metrics *observability.Metrics) error {
	started := time.Now()
	outcome := "failed"
	ctx, span := otel.Tracer("telemetry-ingest-iot/processor").Start(ctx, "telemetry.event.process")
	defer func() {
		span.SetAttributes(attribute.String("telemetry.outcome", outcome))
		metrics.RecordProcessing(ctx, outcome, time.Since(started))
		span.End()
	}()

	var event telemetry.Event
	if err := json.Unmarshal(body, &event); err != nil {
		if err := quarantine.Send(ctx, body, "invalid JSON: "+err.Error()); err != nil {
			span.RecordError(err)
			return err
		}
		outcome = "quarantined"
		return nil
	}
	if err := event.Validate(); err != nil {
		if err := quarantine.Send(ctx, body, "invalid event: "+err.Error()); err != nil {
			span.RecordError(err)
			return err
		}
		outcome = "quarantined"
		return nil
	}
	if event.ReceivedAt != nil {
		metrics.RecordIngestionLatency(ctx, time.Since(*event.ReceivedAt))
	}
	result, err := store.Apply(ctx, event)
	if err != nil {
		span.RecordError(err)
		return err
	}
	switch {
	case result.Duplicate:
		outcome = "duplicate"
	case result.OutOfOrder:
		outcome = "out_of_order"
	default:
		outcome = "applied"
	}
	return nil
}

func retry(ctx context.Context, operation func() error) bool {
	delay := time.Second
	for ctx.Err() == nil {
		if err := operation(); err == nil {
			return true
		} else {
			log.Printf("retry cloud operation: %v", err)
		}
		if !wait(ctx, delay) {
			return false
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
	return false
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatal(fmt.Errorf("required environment variable %s is not set", name))
	}
	return value
}

func boolPtr(value bool) *bool {
	return &value
}
