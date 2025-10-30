package temporal

// Test helper functions shared across temporal test files
// Note: contains() and findSubstring() are already defined in activities.go

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}
