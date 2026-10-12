package azure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	eventhub "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2/checkpoints"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"github.com/alulema/telemetry-ingest-iot/internal/telemetry"
)

type EventPublisher struct {
	client *eventhub.ProducerClient
}

func NewEventPublisher(namespace, hub string, credential azcore.TokenCredential) (*EventPublisher, error) {
	client, err := eventhub.NewProducerClient(namespace, hub, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("create Event Hubs producer: %w", err)
	}
	return &EventPublisher{client: client}, nil
}

func (p *EventPublisher) Publish(ctx context.Context, event telemetry.Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal telemetry event: %w", err)
	}
	partitionKey := event.DeviceID
	batch, err := p.client.NewEventDataBatch(ctx, &eventhub.EventDataBatchOptions{PartitionKey: &partitionKey})
	if err != nil {
		return fmt.Errorf("create Event Hubs batch: %w", err)
	}
	contentType := "application/json"
	if err := batch.AddEventData(&eventhub.EventData{Body: body, ContentType: &contentType}, nil); err != nil {
		return fmt.Errorf("add event to Event Hubs batch: %w", err)
	}
	if err := p.client.SendEventDataBatch(ctx, batch, nil); err != nil {
		return fmt.Errorf("send event to Event Hubs: %w", err)
	}
	return nil
}

func (p *EventPublisher) Close(ctx context.Context) error {
	if err := p.client.Close(ctx); err != nil {
		return fmt.Errorf("close Event Hubs producer: %w", err)
	}
	return nil
}

type CosmosStore struct {
	container *azcosmos.ContainerClient
}

type record struct {
	ID         string                 `json:"id"`
	DeviceID   string                 `json:"device_id"`
	RecordType string                 `json:"record_type"`
	Event      *telemetry.Event       `json:"event,omitempty"`
	State      *telemetry.DeviceState `json:"state,omitempty"`
}

func NewCosmosStore(endpoint, database, container string, credential azcore.TokenCredential) (*CosmosStore, error) {
	client, err := azcosmos.NewClient(endpoint, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("create Cosmos DB client: %w", err)
	}
	containerClient, err := client.NewContainer(database, container)
	if err != nil {
		return nil, fmt.Errorf("create Cosmos DB container client: %w", err)
	}
	return &CosmosStore{container: containerClient}, nil
}

func (s *CosmosStore) ReadState(ctx context.Context, deviceID string) (telemetry.DeviceState, error) {
	response, err := s.container.ReadItem(ctx, azcosmos.NewPartitionKeyString(deviceID), "state", nil)
	if err != nil {
		if responseStatus(err) == http.StatusNotFound {
			return telemetry.DeviceState{}, telemetry.ErrNotFound
		}
		return telemetry.DeviceState{}, fmt.Errorf("read Cosmos DB device state: %w", err)
	}
	var stored record
	if err := json.Unmarshal(response.Value, &stored); err != nil {
		return telemetry.DeviceState{}, fmt.Errorf("decode Cosmos DB device state: %w", err)
	}
	if stored.State == nil {
		return telemetry.DeviceState{}, errors.New("Cosmos DB state record has no state payload")
	}
	return *stored.State, nil
}

func (s *CosmosStore) Apply(ctx context.Context, event telemetry.Event) (telemetry.ApplyResult, error) {
	if err := event.Validate(); err != nil {
		return telemetry.ApplyResult{}, err
	}
	partitionKey := azcosmos.NewPartitionKeyString(event.DeviceID)
	eventRecord := record{
		ID:         event.EventID,
		DeviceID:   event.DeviceID,
		RecordType: "event",
		Event:      &event,
	}
	eventJSON, err := json.Marshal(eventRecord)
	if err != nil {
		return telemetry.ApplyResult{}, fmt.Errorf("marshal Cosmos DB event record: %w", err)
	}

	for attempt := 0; attempt < 8; attempt++ {
		_, err := s.container.ReadItem(ctx, partitionKey, event.EventID, nil)
		if err == nil {
			return telemetry.ApplyResult{Duplicate: true}, nil
		}
		if responseStatus(err) != http.StatusNotFound {
			return telemetry.ApplyResult{}, fmt.Errorf("check Cosmos DB event deduplication: %w", err)
		}

		current, etag, exists, err := s.readCurrentState(ctx, partitionKey)
		if err != nil {
			return telemetry.ApplyResult{}, err
		}
		batch := s.container.NewTransactionalBatch(partitionKey)
		batch.CreateItem(eventJSON, nil)
		result := telemetry.ApplyResult{}
		if exists && telemetry.CompareEvents(event, stateAsEvent(current)) <= 0 {
			result.OutOfOrder = true
		} else {
			state := telemetry.DeviceState{
				DeviceID:   event.DeviceID,
				DeviceKind: event.DeviceKind,
				BootID:     event.BootID,
				Seq:        event.Seq,
				EventID:    event.EventID,
				RecordedAt: event.RecordedAt.UTC(),
				Payload:    event.Payload,
			}
			stateRecord := record{ID: "state", DeviceID: event.DeviceID, RecordType: "state", State: &state}
			stateJSON, marshalErr := json.Marshal(stateRecord)
			if marshalErr != nil {
				return telemetry.ApplyResult{}, fmt.Errorf("marshal Cosmos DB state record: %w", marshalErr)
			}
			if exists {
				batch.ReplaceItem("state", stateJSON, &azcosmos.TransactionalBatchItemOptions{IfMatchETag: &etag})
			} else {
				batch.CreateItem(stateJSON, nil)
			}
			result.StateChanged = true
		}

		response, err := s.container.ExecuteTransactionalBatch(ctx, batch, nil)
		if err != nil {
			return telemetry.ApplyResult{}, fmt.Errorf("execute Cosmos DB telemetry transaction: %w", err)
		}
		if response.Success {
			return result, nil
		}
		status := batchFailureStatus(response)
		if status != http.StatusConflict && status != http.StatusPreconditionFailed {
			return telemetry.ApplyResult{}, fmt.Errorf("Cosmos DB telemetry transaction failed with status %d", status)
		}
	}
	return telemetry.ApplyResult{}, errors.New("Cosmos DB state changed repeatedly; retry telemetry event")
}

func (s *CosmosStore) readCurrentState(ctx context.Context, partitionKey azcosmos.PartitionKey) (telemetry.DeviceState, azcore.ETag, bool, error) {
	response, err := s.container.ReadItem(ctx, partitionKey, "state", nil)
	if err != nil {
		if responseStatus(err) == http.StatusNotFound {
			return telemetry.DeviceState{}, "", false, nil
		}
		return telemetry.DeviceState{}, "", false, fmt.Errorf("read Cosmos DB state for update: %w", err)
	}
	var stored record
	if err := json.Unmarshal(response.Value, &stored); err != nil {
		return telemetry.DeviceState{}, "", false, fmt.Errorf("decode Cosmos DB state for update: %w", err)
	}
	if stored.State == nil {
		return telemetry.DeviceState{}, "", false, errors.New("Cosmos DB state record has no state payload")
	}
	return *stored.State, response.ETag, true, nil
}

func stateAsEvent(state telemetry.DeviceState) telemetry.Event {
	return telemetry.Event{
		DeviceID:   state.DeviceID,
		DeviceKind: state.DeviceKind,
		BootID:     state.BootID,
		Seq:        state.Seq,
		EventID:    state.EventID,
		RecordedAt: state.RecordedAt,
	}
}

func responseStatus(err error) int {
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		return responseError.StatusCode
	}
	return 0
}

func batchFailureStatus(response azcosmos.TransactionalBatchResponse) int {
	for _, result := range response.OperationResults {
		if result.StatusCode != http.StatusFailedDependency {
			return int(result.StatusCode)
		}
	}
	return 0
}

type QuarantineSender struct {
	client *azservicebus.Client
	sender *azservicebus.Sender
}

func NewQuarantineSender(namespace, queue string, credential azcore.TokenCredential) (*QuarantineSender, error) {
	client, err := azservicebus.NewClient(namespace, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("create Service Bus client: %w", err)
	}
	sender, err := client.NewSender(queue, nil)
	if err != nil {
		_ = client.Close(context.Background())
		return nil, fmt.Errorf("create Service Bus quarantine sender: %w", err)
	}
	return &QuarantineSender{client: client, sender: sender}, nil
}

func (s *QuarantineSender) Send(ctx context.Context, body []byte, reason string) error {
	contentType := "application/json"
	hashInput := append(append(append([]byte(nil), body...), 0), []byte(reason)...)
	sum := sha256.Sum256(hashInput)
	messageID := hex.EncodeToString(sum[:])
	message := &azservicebus.Message{
		Body:        body,
		ContentType: &contentType,
		MessageID:   &messageID,
		Subject:     &reason,
		ApplicationProperties: map[string]any{
			"failure_reason": reason,
			"received_at":    time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if err := s.sender.SendMessage(ctx, message, nil); err != nil {
		return fmt.Errorf("send invalid event to Service Bus quarantine: %w", err)
	}
	return nil
}

func (s *QuarantineSender) Close(ctx context.Context) error {
	if err := s.sender.Close(ctx); err != nil {
		return fmt.Errorf("close Service Bus sender: %w", err)
	}
	if err := s.client.Close(ctx); err != nil {
		return fmt.Errorf("close Service Bus client: %w", err)
	}
	return nil
}

func NewCheckpointStore(storageAccount, container string, credential azcore.TokenCredential) (*checkpoints.BlobStore, error) {
	client, err := azblob.NewClient("https://"+storageAccount+".blob.core.windows.net/", credential, nil)
	if err != nil {
		return nil, fmt.Errorf("create checkpoint Blob client: %w", err)
	}
	containerClient := client.ServiceClient().NewContainerClient(container)
	store, err := checkpoints.NewBlobStore(containerClient, nil)
	if err != nil {
		return nil, fmt.Errorf("create Event Hubs checkpoint store: %w", err)
	}
	return store, nil
}

func NewDefaultCredential() (*azidentity.DefaultAzureCredential, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create Azure default credential: %w", err)
	}
	return credential, nil
}
