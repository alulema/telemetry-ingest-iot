package simulator

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/alulema/telemetry-ingest-iot/internal/telemetry"
)

type Config struct {
	Devices               int
	EventsPerDevice       int
	Seed                  int64
	BufferCapacity        int
	DisconnectProbability float64
	ReconnectProbability  float64
	DuplicateProbability  float64
	ReorderWindow         int
	StartTime             time.Time
	Interval              time.Duration
}

type Report struct {
	Seed                 int64                   `json:"seed"`
	Devices              int                     `json:"devices"`
	Generated            int                     `json:"generated"`
	Published            int                     `json:"published"`
	DuplicatesInjected   int                     `json:"duplicates_injected"`
	DuplicatesDiscarded  int                     `json:"duplicates_discarded"`
	EventsOutOfOrder     int                     `json:"events_out_of_order"`
	BufferOverflowDrops  int                     `json:"buffer_overflow_drops"`
	PendingOfflineEvents int                     `json:"pending_offline_events"`
	FinalStateCount      int                     `json:"final_state_count"`
	UniqueEventsStored   int                     `json:"unique_events_stored"`
	States               []telemetry.DeviceState `json:"states,omitempty"`
}

func (c Config) Validate() error {
	if c.Devices < 1 || c.EventsPerDevice < 1 {
		return errors.New("devices and events-per-device must be positive")
	}
	if c.BufferCapacity < 1 {
		return errors.New("buffer-capacity must be positive")
	}
	if c.ReorderWindow < 1 {
		return errors.New("reorder-window must be positive")
	}
	for name, value := range map[string]float64{
		"disconnect-probability": c.DisconnectProbability,
		"reconnect-probability":  c.ReconnectProbability,
		"duplicate-probability":  c.DuplicateProbability,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("%s must be between 0 and 1", name)
		}
	}
	if c.Interval <= 0 {
		return errors.New("interval must be positive")
	}
	return nil
}

func Run(c Config) (Report, error) {
	if err := c.Validate(); err != nil {
		return Report{}, err
	}
	if c.StartTime.IsZero() {
		c.StartTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	random := rand.New(rand.NewSource(c.Seed))
	truth := make([]telemetry.Event, 0, c.Devices*c.EventsPerDevice)
	published := make([]telemetry.Event, 0, c.Devices*c.EventsPerDevice)
	overflowDrops := 0
	duplicatesInjected := 0
	pendingOfflineEvents := 0

	for device := 0; device < c.Devices; device++ {
		deviceID := fmt.Sprintf("collar-%06d", device+1)
		bootID := "boot-0001"
		seq := uint64(0)
		buffer := make([]telemetry.Event, 0, c.BufferCapacity)
		connected := true

		for tick := 0; tick < c.EventsPerDevice; tick++ {
			if connected && random.Float64() < c.DisconnectProbability {
				connected = false
			} else if !connected && random.Float64() < c.ReconnectProbability {
				connected = true
			}

			seq++
			at := c.StartTime.Add(time.Duration(tick) * c.Interval)
			event := makeCollarEvent(deviceID, bootID, seq, at, random)
			truth = append(truth, event)

			if !connected {
				if len(buffer) == c.BufferCapacity {
					copy(buffer, buffer[1:])
					buffer = buffer[:len(buffer)-1]
					overflowDrops++
				}
				buffer = append(buffer, event)
				continue
			}

			for _, queued := range buffer {
				var duplicates int
				published, duplicates = addRetryDuplicate(published, queued, c.DuplicateProbability, random)
				duplicatesInjected += duplicates
			}
			buffer = buffer[:0]
			var duplicates int
			published, duplicates = addRetryDuplicate(published, event, c.DuplicateProbability, random)
			duplicatesInjected += duplicates
		}
		pendingOfflineEvents += len(buffer)
	}

	reorderInWindows(published, c.ReorderWindow, random)

	processor := telemetry.NewProcessor()
	duplicatesDiscarded := 0
	eventsOutOfOrder := 0
	for _, event := range published {
		result, err := processor.Apply(event)
		if err != nil {
			return Report{}, fmt.Errorf("process %s: %w", event.EventID, err)
		}
		if result.Duplicate {
			duplicatesDiscarded++
		}
		if result.OutOfOrder {
			eventsOutOfOrder++
		}
	}

	states := processor.States()
	sort.Slice(states, func(i, j int) bool { return states[i].DeviceID < states[j].DeviceID })
	return Report{
		Seed:                 c.Seed,
		Devices:              c.Devices,
		Generated:            len(truth),
		Published:            len(published),
		DuplicatesInjected:   duplicatesInjected,
		DuplicatesDiscarded:  duplicatesDiscarded,
		EventsOutOfOrder:     eventsOutOfOrder,
		BufferOverflowDrops:  overflowDrops,
		PendingOfflineEvents: pendingOfflineEvents,
		FinalStateCount:      len(states),
		UniqueEventsStored:   processor.EventCount(),
		States:               states,
	}, nil
}

func makeCollarEvent(deviceID, bootID string, seq uint64, at time.Time, random *rand.Rand) telemetry.Event {
	latitude := 40.0 + random.Float64()
	longitude := -74.0 + random.Float64()
	battery := 50.0 + random.Float64()*50
	temperature := 10.0 + random.Float64()*25
	activity := random.Float64()
	return telemetry.NewEvent(deviceID, telemetry.DeviceCollar, bootID, seq, at, "sim-1", telemetry.Payload{
		Latitude:       &latitude,
		Longitude:      &longitude,
		BatteryPercent: &battery,
		TemperatureC:   &temperature,
		Activity:       &activity,
	})
}

func addRetryDuplicate(published []telemetry.Event, event telemetry.Event, probability float64, random *rand.Rand) ([]telemetry.Event, int) {
	published = append(published, event)
	if random.Float64() < probability {
		published = append(published, event)
		return published, 1
	}
	return published, 0
}

func reorderInWindows(events []telemetry.Event, window int, random *rand.Rand) {
	for start := 0; start < len(events); start += window {
		end := start + window
		if end > len(events) {
			end = len(events)
		}
		random.Shuffle(end-start, func(i, j int) {
			events[start+i], events[start+j] = events[start+j], events[start+i]
		})
	}
}
