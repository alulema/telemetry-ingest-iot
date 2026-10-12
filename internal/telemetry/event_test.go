package telemetry

import (
	"math/rand"
	"testing"
	"time"
)

func ptr(v float64) *float64 { return &v }

func gardenEvent(device, boot string, seq uint64, recorded time.Time, moisture float64) Event {
	return NewEvent(device, DeviceGarden, boot, seq, recorded, "test", Payload{
		SoilMoisture: ptr(moisture),
		TemperatureC: ptr(21.5),
	})
}

func TestEventValidationAndDeterministicID(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	e := gardenEvent("garden-1", "boot-a", 1, now, 42)
	if err := e.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if e.EventID != DeterministicEventID("garden-1", "boot-a", 1) {
		t.Fatal("event ID is not deterministic")
	}
	e.EventID = "wrong"
	if err := e.Validate(); err == nil {
		t.Fatal("expected mismatched event ID to be rejected")
	}
}

func TestProcessorDeduplicatesAndDoesNotRegress(t *testing.T) {
	start := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	newer := gardenEvent("garden-1", "boot-a", 2, start.Add(time.Minute), 30)
	older := gardenEvent("garden-1", "boot-a", 1, start, 70)

	p := NewProcessor()
	if _, err := p.Apply(newer); err != nil {
		t.Fatal(err)
	}
	result, err := p.Apply(newer)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Duplicate {
		t.Fatal("expected duplicate event to be discarded")
	}
	if _, err := p.Apply(older); err != nil {
		t.Fatal(err)
	}
	state, ok := p.State("garden-1")
	if !ok {
		t.Fatal("expected device state")
	}
	if state.EventID != newer.EventID {
		t.Fatalf("stale event regressed state to %s", state.EventID)
	}
	if p.EventCount() != 2 {
		t.Fatalf("expected 2 unique events, got %d", p.EventCount())
	}
}

func TestPropertyDuplicatePermutationOrderInvariance(t *testing.T) {
	const cases = 1000
	random := rand.New(rand.NewSource(0x51A7E))
	start := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	events := make([]Event, 30)
	for i := range events {
		events[i] = gardenEvent("garden-1", "boot-a", uint64(i+1), start.Add(time.Duration(i)*time.Minute), float64(i%100))
	}

	reference, err := ProcessAll(events)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := reference.State("garden-1")

	for run := 0; run < cases; run++ {
		input := append([]Event(nil), events...)
		for i := range events {
			if random.Intn(3) == 0 {
				input = append(input, events[i])
			}
		}
		random.Shuffle(len(input), func(i, j int) { input[i], input[j] = input[j], input[i] })
		gotProcessor, err := ProcessAll(input)
		if err != nil {
			t.Fatalf("case %d: %v", run, err)
		}
		got, _ := gotProcessor.State("garden-1")
		if got.EventID != want.EventID || got.Payload.SoilMoisture == nil || *got.Payload.SoilMoisture != *want.Payload.SoilMoisture {
			t.Fatalf("case %d produced different final state", run)
		}
		if gotProcessor.EventCount() != len(events) {
			t.Fatalf("case %d expected %d unique events, got %d", run, len(events), gotProcessor.EventCount())
		}
	}
}

func TestEqualTimestampUsesStableTieBreakers(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	first := gardenEvent("garden-1", "boot-a", 1, at, 10)
	second := gardenEvent("garden-1", "boot-a", 2, at, 20)
	if CompareEvents(first, second) >= 0 {
		t.Fatal("sequence should break equal-timestamp tie within boot")
	}
	otherBoot := gardenEvent("garden-1", "boot-b", 1, at, 30)
	if CompareEvents(second, otherBoot) >= 0 {
		t.Fatal("boot ID should provide stable tie-break across boots")
	}
}
