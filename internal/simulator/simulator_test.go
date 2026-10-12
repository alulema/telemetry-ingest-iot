package simulator

import (
	"reflect"
	"testing"
	"time"
)

func TestRunIsReproducible(t *testing.T) {
	config := Config{
		Devices:               20,
		EventsPerDevice:       100,
		Seed:                  1234,
		BufferCapacity:        12,
		DisconnectProbability: 0.08,
		ReconnectProbability:  0.35,
		DuplicateProbability:  0.1,
		ReorderWindow:         17,
		Interval:              time.Minute,
	}
	first, err := Run(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same seed and configuration produced different reports")
	}
	if first.FinalStateCount != config.Devices {
		t.Fatalf("expected one final state per device; got %d", first.FinalStateCount)
	}
	if first.UniqueEventsStored != first.Published-first.DuplicatesInjected {
		t.Fatalf("expected unique stored events to match published minus duplicates: %+v", first)
	}
}

func TestBufferOverflowIsMeasured(t *testing.T) {
	report, err := Run(Config{
		Devices:               1,
		EventsPerDevice:       10,
		Seed:                  7,
		BufferCapacity:        2,
		DisconnectProbability: 1,
		ReconnectProbability:  0,
		ReorderWindow:         3,
		Interval:              time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.BufferOverflowDrops != 8 {
		t.Fatalf("expected 8 oldest events dropped, got %d", report.BufferOverflowDrops)
	}
	if report.FinalStateCount != 0 {
		t.Fatalf("device never reconnected, expected no persisted state; got %d states", report.FinalStateCount)
	}
}
