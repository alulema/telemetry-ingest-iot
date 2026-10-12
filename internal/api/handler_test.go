package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alulema/telemetry-ingest-iot/internal/telemetry"
)

func TestIngestDeduplicatesAndReturnsLatestState(t *testing.T) {
	processor := telemetry.NewProcessor()
	handler := NewHandler(processor).Routes()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	event := telemetry.NewEvent("garden-1", telemetry.DeviceGarden, "boot-a", 1, at, "test", telemetry.Payload{
		SoilMoisture: floatPtr(40),
		TemperatureC: floatPtr(22),
	})

	first := postEvent(t, handler, event)
	if first.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", first.Code, first.Body.String())
	}
	second := postEvent(t, handler, event)
	if second.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for duplicate, got %d", second.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["duplicate"] != true {
		t.Fatalf("expected duplicate response, got %v", response["duplicate"])
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/devices/garden-1/state", nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK {
		t.Fatalf("expected state 200, got %d: %s", result.Code, result.Body.String())
	}
	if processor.EventCount() != 1 {
		t.Fatalf("expected one unique event, got %d", processor.EventCount())
	}
}

func TestIngestRejectsMalformedAndInvalidEvents(t *testing.T) {
	handler := NewHandler(telemetry.NewProcessor()).Routes()
	for _, body := range []string{`{`, `{"device_id":"bad"}`} {
		request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewBufferString(body))
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, request)
		if result.Code < 400 || result.Code >= 500 {
			t.Fatalf("expected client error for %q, got %d", body, result.Code)
		}
	}
}

func TestIngestRejectsOversizedAndTrailingJSON(t *testing.T) {
	handler := NewHandler(telemetry.NewProcessor()).Routes()
	for name, body := range map[string]string{
		"oversized": strings.Repeat(" ", maxEventBytes+1),
		"trailing":  `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(body))
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, request)
			if result.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", result.Code, result.Body.String())
			}
		})
	}
}

func TestStateReturnsNotFoundForUnknownDevice(t *testing.T) {
	handler := NewHandler(telemetry.NewProcessor()).Routes()
	request := httptest.NewRequest(http.MethodGet, "/v1/devices/missing/state", nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", result.Code, result.Body.String())
	}
}

func TestCloudHandlerPublishesValidatedEvent(t *testing.T) {
	publisher := &testPublisher{}
	handler := NewCloudHandler(publisher, testStateReader{}, "test-token").Routes()
	event := telemetry.NewEvent("garden-1", telemetry.DeviceGarden, "boot-a", 1, time.Now(), "test", telemetry.Payload{
		SoilMoisture: floatPtr(40),
		TemperatureC: floatPtr(22),
	})

	result := postEvent(t, handler, event)
	if result.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", result.Code, result.Body.String())
	}
	if publisher.event.EventID != event.EventID {
		t.Fatalf("expected event %q to be published, got %q", event.EventID, publisher.event.EventID)
	}
	if publisher.event.ReceivedAt == nil {
		t.Fatal("expected the API to stamp the server receive time")
	}
}

func TestCloudHandlerReportsPublishFailure(t *testing.T) {
	publisher := &testPublisher{err: errors.New("unavailable")}
	handler := NewCloudHandler(publisher, testStateReader{}, "test-token").Routes()
	event := telemetry.NewEvent("garden-1", telemetry.DeviceGarden, "boot-a", 1, time.Now(), "test", telemetry.Payload{
		SoilMoisture: floatPtr(40),
		TemperatureC: floatPtr(22),
	})

	result := postEvent(t, handler, event)
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", result.Code, result.Body.String())
	}
}

func TestCloudHandlerRequiresBearerToken(t *testing.T) {
	handler := NewCloudHandler(&testPublisher{}, testStateReader{}, "test-token").Routes()
	request := httptest.NewRequest(http.MethodGet, "/v1/devices/garden-1/state", nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", result.Code, result.Body.String())
	}
}

type testPublisher struct {
	event telemetry.Event
	err   error
}

func (p *testPublisher) Publish(_ context.Context, event telemetry.Event) error {
	p.event = event
	return p.err
}

type testStateReader struct{}

func (testStateReader) ReadState(context.Context, string) (telemetry.DeviceState, error) {
	return telemetry.DeviceState{}, telemetry.ErrNotFound
}

func postEvent(t *testing.T, handler http.Handler, event telemetry.Event) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	return result
}

func floatPtr(value float64) *float64 { return &value }
