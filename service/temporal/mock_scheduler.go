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

// CreateWalletAssetSchedule records that a schedule was created.
func (m *MockScheduler) CreateWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string, ata *string, interval time.Duration) error {
	if m.createErr != nil {
		return m.createErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network, assetType, tokenMint)
	m.schedules[id] = interval
	return nil
}

// UpsertWalletAssetSchedule creates or updates a schedule.
func (m *MockScheduler) UpsertWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string, ata *string, interval time.Duration) error {
	if m.createErr != nil {
		return m.createErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network, assetType, tokenMint)
	m.schedules[id] = interval // Creates or updates
	return nil
}

// DeleteWalletAssetSchedule records that a schedule was deleted.
func (m *MockScheduler) DeleteWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network, assetType, tokenMint)
	if _, exists := m.schedules[id]; !exists {
		return fmt.Errorf("schedule %q not found", id)
	}

	delete(m.schedules, id)
	return nil
}

// SetCreateError makes CreateWalletAssetSchedule return an error.
func (m *MockScheduler) SetCreateError(err error) {
	m.createErr = err
}

// SetDeleteError makes DeleteWalletAssetSchedule return an error.
func (m *MockScheduler) SetDeleteError(err error) {
	m.deleteErr = err
}

// ScheduleExists checks if a schedule exists for a wallet asset.
func (m *MockScheduler) ScheduleExists(address string, network string, assetType string, tokenMint string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network, assetType, tokenMint)
	_, exists := m.schedules[id]
	return exists
}

// GetScheduleInterval returns the interval for a wallet asset's schedule.
func (m *MockScheduler) GetScheduleInterval(address string, network string, assetType string, tokenMint string) (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := scheduleID(address, network, assetType, tokenMint)
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
