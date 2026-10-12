package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"
)

type DeviceKind string

const (
	DeviceCollar DeviceKind = "collar"
	DeviceGarden DeviceKind = "garden"
)

type Payload struct {
	Latitude       *float64 `json:"latitude,omitempty"`
	Longitude      *float64 `json:"longitude,omitempty"`
	BatteryPercent *float64 `json:"battery_percent,omitempty"`
	TemperatureC   *float64 `json:"temperature_c,omitempty"`
	SoilMoisture   *float64 `json:"soil_moisture_percent,omitempty"`
	Activity       *float64 `json:"activity,omitempty"`
	CueState       string   `json:"cue_state,omitempty"`
}

type Event struct {
	DeviceID      string     `json:"device_id"`
	DeviceKind    DeviceKind `json:"device_kind"`
	BootID        string     `json:"boot_id"`
	Seq           uint64     `json:"seq"`
	EventID       string     `json:"event_id"`
	RecordedAt    time.Time  `json:"recorded_at"`
	ReceivedAt    *time.Time `json:"received_at,omitempty"`
	SchemaVersion uint16     `json:"schema_version"`
	FWVersion     string     `json:"fw_version"`
	Payload       Payload    `json:"payload"`
}

func NewEvent(deviceID string, kind DeviceKind, bootID string, seq uint64, recordedAt time.Time, fwVersion string, payload Payload) Event {
	e := Event{
		DeviceID:      deviceID,
		DeviceKind:    kind,
		BootID:        bootID,
		Seq:           seq,
		RecordedAt:    recordedAt.UTC(),
		SchemaVersion: 1,
		FWVersion:     fwVersion,
		Payload:       payload,
	}
	e.EventID = DeterministicEventID(deviceID, bootID, seq)
	return e
}

func DeterministicEventID(deviceID, bootID string, seq uint64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", deviceID, bootID, seq)))
	return hex.EncodeToString(sum[:])
}

func (e Event) Validate() error {
	if e.DeviceID == "" {
		return errors.New("device_id is required")
	}
	if e.BootID == "" {
		return errors.New("boot_id is required")
	}
	if e.Seq == 0 {
		return errors.New("seq must be greater than zero")
	}
	if e.EventID != DeterministicEventID(e.DeviceID, e.BootID, e.Seq) {
		return errors.New("event_id does not match device_id, boot_id, and seq")
	}
	if e.RecordedAt.IsZero() {
		return errors.New("recorded_at is required")
	}
	if e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", e.SchemaVersion)
	}
	switch e.DeviceKind {
	case DeviceCollar:
		if e.Payload.Latitude == nil || e.Payload.Longitude == nil {
			return errors.New("collar payload requires latitude and longitude")
		}
		if !inRange(*e.Payload.Latitude, -90, 90) || !inRange(*e.Payload.Longitude, -180, 180) {
			return errors.New("collar coordinates are out of range")
		}
	case DeviceGarden:
		if e.Payload.SoilMoisture == nil || e.Payload.TemperatureC == nil {
			return errors.New("garden payload requires soil_moisture_percent and temperature_c")
		}
		if !inRange(*e.Payload.SoilMoisture, 0, 100) {
			return errors.New("soil_moisture_percent must be between 0 and 100")
		}
	default:
		return fmt.Errorf("unsupported device_kind %q", e.DeviceKind)
	}
	return nil
}

func inRange(v, min, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= min && v <= max
}
