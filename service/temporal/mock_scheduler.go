package temporal

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockScheduler is a mock implementation of Scheduler for testing.
type MockScheduler struct {
	mu        sync.Mutex
	schedules map[string]time.Duration // map[scheduleID]interval
	createErr error
	deleteErr error
}

// NewMockScheduler creates a new MockScheduler.
func NewMockScheduler() *MockScheduler {
	return &MockScheduler{
		schedules: make(map[string]time.Duration),
	}
}

// CreateWalletSchedule records that a schedule was created.
func (m *MockScheduler) CreateWalletSchedule(ctx context.Context, address string, network string, interval time.Duration) error {
	if m.createErr != nil {
		return m.createErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network)
	m.schedules[id] = interval
	return nil
}

// DeleteWalletSchedule records that a schedule was deleted.
func (m *MockScheduler) DeleteWalletSchedule(ctx context.Context, address string, network string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network)
	if _, exists := m.schedules[id]; !exists {
		return fmt.Errorf("schedule %q not found", id)
	}

	delete(m.schedules, id)
	return nil
}

// SetCreateError makes CreateWalletSchedule return an error.
func (m *MockScheduler) SetCreateError(err error) {
	m.createErr = err
}

// SetDeleteError makes DeleteWalletSchedule return an error.
func (m *MockScheduler) SetDeleteError(err error) {
	m.deleteErr = err
}

// ScheduleExists checks if a schedule exists for a wallet.
func (m *MockScheduler) ScheduleExists(address string, network string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network)
	_, exists := m.schedules[id]
	return exists
}

// GetScheduleInterval returns the interval for a wallet's schedule.
func (m *MockScheduler) GetScheduleInterval(address string, network string) (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network)
	interval, exists := m.schedules[id]
	return interval, exists
}

// ScheduleCount returns the number of schedules.
func (m *MockScheduler) ScheduleCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.schedules)
}

// Reset clears all schedules and errors.
func (m *MockScheduler) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedules = make(map[string]time.Duration)
	m.createErr = nil
	m.deleteErr = nil
}
