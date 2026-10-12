package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alulema/telemetry-ingest-iot/internal/observability"
	"github.com/alulema/telemetry-ingest-iot/internal/telemetry"
)

const maxEventBytes = 192 << 10

type EventPublisher interface {
	Publish(context.Context, telemetry.Event) error
}

type StateReader interface {
	ReadState(context.Context, string) (telemetry.DeviceState, error)
}

type Handler struct {
	processor   *telemetry.Processor
	publisher   EventPublisher
	stateReader StateReader
	apiToken    string
	metrics     *observability.Metrics
}

func NewHandler(processor *telemetry.Processor, metrics ...*observability.Metrics) *Handler {
	return &Handler{processor: processor, metrics: firstMetrics(metrics)}
}

func NewCloudHandler(publisher EventPublisher, stateReader StateReader, apiToken string, metrics ...*observability.Metrics) *Handler {
	return &Handler{publisher: publisher, stateReader: stateReader, apiToken: apiToken, metrics: firstMetrics(metrics)}
}

func firstMetrics(metrics []*observability.Metrics) *observability.Metrics {
	if len(metrics) == 0 {
		return nil
	}
	return metrics[0]
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/events", h.ingest)
	mux.HandleFunc("GET /v1/devices/{deviceID}/state", h.state)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	if h.publisher != nil && !h.authorized(r) {
		h.metrics.RecordIngestion(r.Context(), "unauthorized")
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBytes)
	decoder := json.NewDecoder(r.Body)
	var event telemetry.Event
	if err := decoder.Decode(&event); err != nil {
		h.metrics.RecordIngestion(r.Context(), "invalid")
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		h.metrics.RecordIngestion(r.Context(), "invalid")
		writeError(w, http.StatusBadRequest, "invalid_json", "request must contain one JSON object")
		return
	}
	receivedAt := time.Now().UTC()
	event.ReceivedAt = &receivedAt
	if err := event.Validate(); err != nil {
		h.metrics.RecordIngestion(r.Context(), "invalid")
		writeError(w, http.StatusUnprocessableEntity, "invalid_event", err.Error())
		return
	}
	response := map[string]any{
		"event_id":    event.EventID,
		"received_at": receivedAt,
	}
	if h.publisher != nil {
		if err := h.publisher.Publish(r.Context(), event); err != nil {
			h.metrics.RecordIngestion(r.Context(), "unavailable")
			writeError(w, http.StatusServiceUnavailable, "ingestion_unavailable", err.Error())
			return
		}
		h.metrics.RecordIngestion(r.Context(), "accepted")
		writeJSON(w, http.StatusAccepted, response)
		return
	}
	result, err := h.processor.Apply(event)
	if err != nil {
		h.metrics.RecordIngestion(r.Context(), "invalid")
		writeError(w, http.StatusUnprocessableEntity, "invalid_event", err.Error())
		return
	}
	h.metrics.RecordIngestion(r.Context(), "accepted")
	outcome := "applied"
	if result.Duplicate {
		outcome = "duplicate"
	} else if result.OutOfOrder {
		outcome = "out_of_order"
	}
	h.metrics.RecordProcessing(r.Context(), outcome, 0)
	response["duplicate"] = result.Duplicate
	response["out_of_order"] = result.OutOfOrder
	response["state_changed"] = result.StateChanged
	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	if h.stateReader != nil && !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return
	}
	deviceID := strings.TrimSpace(r.PathValue("deviceID"))
	if h.stateReader != nil {
		state, err := h.stateReader.ReadState(r.Context(), deviceID)
		if errors.Is(err, telemetry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "state_unavailable", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, state)
		return
	}
	state, ok := h.processor.State(deviceID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "device state not found")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) authorized(r *http.Request) bool {
	if h.apiToken == "" {
		return false
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	return len(provided) == len(h.apiToken) && subtle.ConstantTimeCompare([]byte(provided), []byte(h.apiToken)) == 1
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
