package configmanagement

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// EventPipelineImpl implements EventPipeline orchestrator
type EventPipelineImpl struct {
	ctx        context.Context
	subscriber Subscriber
	cache      DedupCache
	validator  EventValidator

	running   bool
	mu        sync.RWMutex
	startTime time.Time

	eventOut chan Event
	errChan  chan error

	statsLock sync.RWMutex
	stats     PipelineStats

	doneCh chan struct{}
}

// NewEventPipeline creates a new event pipeline orchestrator
func NewEventPipeline(subscriber Subscriber, cache DedupCache, validator EventValidator) *EventPipelineImpl {
	if subscriber == nil {
		subscriber = &NullSubscriber{} // Fallback
	}
	if cache == nil {
		cache = NewLRUCache(10000) // Default cache
	}
	if validator == nil {
		validator = &NullValidator{} // No-op validator
	}

	return &EventPipelineImpl{
		subscriber: subscriber,
		cache:      cache,
		validator:  validator,
		eventOut:   make(chan Event, 1000),
		errChan:    make(chan error, 100),
		doneCh:     make(chan struct{}),
	}
}

// Start begins event subscription and processing
func (p *EventPipelineImpl) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("pipeline already running")
	}

	p.ctx = ctx
	p.running = true
	p.startTime = time.Now()
	p.stats = PipelineStats{} // Reset stats

	// Subscribe to events from subscriber
	eventCh, err := p.subscriber.Subscribe(ctx, []string{"*"})
	if err != nil {
		p.running = false
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Spawn event processing goroutine
	go p.handleEventLoop(eventCh)

	return nil
}

// Stop gracefully stops the pipeline
func (p *EventPipelineImpl) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return fmt.Errorf("pipeline not running")
	}

	p.running = false

	// Signal goroutine to stop
	close(p.doneCh)

	// Close output channel (allow consumers to drain)
	close(p.eventOut)

	// Close subscriber
	if err := p.subscriber.Close(); err != nil {
		return fmt.Errorf("failed to close subscriber: %w", err)
	}

	return nil
}

// GetEventStream returns read-only channel of processed events
func (p *EventPipelineImpl) GetEventStream() <-chan Event {
	return p.eventOut
}

// Stats returns pipeline statistics
func (p *EventPipelineImpl) Stats() PipelineStats {
	p.statsLock.RLock()
	defer p.statsLock.RUnlock()

	stats := p.stats
	stats.Uptime = time.Since(p.startTime)
	return stats
}

// handleEventLoop processes events: dedup → validate → forward
func (p *EventPipelineImpl) handleEventLoop(eventCh <-chan Event) {
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return // Channel closed
			}

			// Create span for event processing
			ctx, span := startSpan(p.ctx, "CM.ProcessEvent")
			defer span.End()

			// Increment received counter
			p.statsLock.Lock()
			p.stats.EventsReceived++
			p.statsLock.Unlock()
			incrementEventsReceived()

			// Check for duplicates
			if p.cache.CheckAndStore(event.Fingerprint) {
				p.statsLock.Lock()
				p.stats.EventsDuplicated++
				p.statsLock.Unlock()
				incrementEventsDuplicated()
				continue // Skip duplicate
			}

			// Validate event
			if err := p.validator.Validate(&event); err != nil {
				p.statsLock.Lock()
				p.stats.ValidationErrors++
				p.statsLock.Unlock()
				incrementValidationErrors()
				defaultLogger.Error("event validation failed",
					"event_id", event.ID,
					"error", err.Error(),
				)
				continue // Skip invalid event
			}

			p.statsLock.Lock()
			p.stats.EventsValidated++
			p.statsLock.Unlock()

			// Forward event (non-blocking on buffered channel)
			select {
			case p.eventOut <- event:
				p.statsLock.Lock()
				p.stats.EventsForwarded++
				p.statsLock.Unlock()
				incrementEventsForwarded()
			case <-p.doneCh:
				return // Pipeline stopping
			case <-ctx.Done():
				return // Context cancelled
			}

		case <-p.doneCh:
			return // Pipeline stopping
		case <-p.ctx.Done():
			return // Context cancelled
		}
	}
}

// NullSubscriber is a fallback subscriber that returns no events
type NullSubscriber struct{}

func (ns *NullSubscriber) Subscribe(ctx context.Context, topics []string) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch) // Empty channel
	return ch, nil
}

func (ns *NullSubscriber) Close() error {
	return nil
}

// NullValidator is a fallback validator that accepts all events
type NullValidator struct{}

func (nv *NullValidator) Validate(event *Event) error {
	return nil
}
