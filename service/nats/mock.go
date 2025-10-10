package nats

import (
	"context"
	"sync"
)

// MockPublisher is a mock implementation of Publisher for testing.
type MockPublisher struct {
	mu                sync.RWMutex
	publishedEvents   []*TransactionEvent
	publishError      error
	publishBatchError error
	closed            bool
}

// NewMockPublisher creates a new mock publisher for testing.
func NewMockPublisher() *MockPublisher {
	return &MockPublisher{
		publishedEvents: make([]*TransactionEvent, 0),
	}
}

// PublishTransaction records the event and returns any configured error.
func (m *MockPublisher) PublishTransaction(ctx context.Context, event *TransactionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishError != nil {
		return m.publishError
	}

	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

// PublishTransactionBatch records the events and returns any configured error.
func (m *MockPublisher) PublishTransactionBatch(ctx context.Context, events []*TransactionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishBatchError != nil {
		return m.publishBatchError
	}

	m.publishedEvents = append(m.publishedEvents, events...)
	return nil
}

// Close marks the publisher as closed.
func (m *MockPublisher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// GetPublishedEvents returns all published events (for testing).
func (m *MockPublisher) GetPublishedEvents() []*TransactionEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	events := make([]*TransactionEvent, len(m.publishedEvents))
	copy(events, m.publishedEvents)
	return events
}

// GetPublishedEventCount returns the number of published events.
func (m *MockPublisher) GetPublishedEventCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.publishedEvents)
}

// GetPublishedEventsForWallet returns events published for a specific wallet.
func (m *MockPublisher) GetPublishedEventsForWallet(address string) []*TransactionEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]*TransactionEvent, 0)
	for _, event := range m.publishedEvents {
		if event.WalletAddress == address {
			events = append(events, event)
		}
	}
	return events
}

// SetPublishError configures the mock to return an error on PublishTransaction.
func (m *MockPublisher) SetPublishError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishError = err
}

// SetPublishBatchError configures the mock to return an error on PublishTransactionBatch.
func (m *MockPublisher) SetPublishBatchError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishBatchError = err
}

// Reset clears all published events and errors.
func (m *MockPublisher) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishedEvents = make([]*TransactionEvent, 0)
	m.publishError = nil
	m.publishBatchError = nil
	m.closed = false
}

// IsClosed returns whether the publisher has been closed.
func (m *MockPublisher) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}
