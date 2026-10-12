package telemetry

import (
	"errors"
	"sync"
	"time"
)

type DeviceState struct {
	DeviceID   string     `json:"device_id"`
	DeviceKind DeviceKind `json:"device_kind"`
	BootID     string     `json:"boot_id"`
	Seq        uint64     `json:"seq"`
	EventID    string     `json:"event_id"`
	RecordedAt time.Time  `json:"recorded_at"`
	Payload    Payload    `json:"payload"`
}

type ApplyResult struct {
	Duplicate    bool
	OutOfOrder   bool
	StateChanged bool
}

type Processor struct {
	mu     sync.RWMutex
	events map[string]Event
	states map[string]DeviceState
}

func NewProcessor() *Processor {
	return &Processor{
		events: make(map[string]Event),
		states: make(map[string]DeviceState),
	}
}

func (p *Processor) Apply(e Event) (ApplyResult, error) {
	if err := e.Validate(); err != nil {
		return ApplyResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.events[e.EventID]; exists {
		return ApplyResult{Duplicate: true}, nil
	}
	p.events[e.EventID] = e

	current, exists := p.states[e.DeviceID]
	if exists && CompareEvents(e, Event{
		DeviceID:   current.DeviceID,
		DeviceKind: current.DeviceKind,
		BootID:     current.BootID,
		Seq:        current.Seq,
		EventID:    current.EventID,
		RecordedAt: current.RecordedAt,
	}) <= 0 {
		return ApplyResult{OutOfOrder: true}, nil
	}

	p.states[e.DeviceID] = DeviceState{
		DeviceID:   e.DeviceID,
		DeviceKind: e.DeviceKind,
		BootID:     e.BootID,
		Seq:        e.Seq,
		EventID:    e.EventID,
		RecordedAt: e.RecordedAt.UTC(),
		Payload:    e.Payload,
	}
	return ApplyResult{StateChanged: true}, nil
}

func CompareEvents(a, b Event) int {
	if a.RecordedAt.Before(b.RecordedAt) {
		return -1
	}
	if a.RecordedAt.After(b.RecordedAt) {
		return 1
	}
	if a.BootID < b.BootID {
		return -1
	}
	if a.BootID > b.BootID {
		return 1
	}
	if a.Seq < b.Seq {
		return -1
	}
	if a.Seq > b.Seq {
		return 1
	}
	return 0
}

func (p *Processor) State(deviceID string) (DeviceState, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	state, ok := p.states[deviceID]
	return state, ok
}

func (p *Processor) EventCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.events)
}

func (p *Processor) States() []DeviceState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]DeviceState, 0, len(p.states))
	for _, state := range p.states {
		result = append(result, state)
	}
	return result
}

func ProcessAll(events []Event) (*Processor, error) {
	p := NewProcessor()
	for _, e := range events {
		if _, err := p.Apply(e); err != nil {
			return nil, err
		}
	}
	return p, nil
}

var ErrNotFound = errors.New("device state not found")

func (p *Processor) RequireState(deviceID string) (DeviceState, error) {
	state, ok := p.State(deviceID)
	if !ok {
		return DeviceState{}, ErrNotFound
	}
	return state, nil
}
