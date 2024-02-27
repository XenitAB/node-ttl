package utils

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestGetEnvOrDefault(t *testing.T) {
	// Set an environment variable for testing
	err := os.Setenv("TEST_KEY", "TEST_VALUE")
	assert.Nil(t, err)

	// Test case when the environment variable exists
	value := GetEnvOrDefault("TEST_KEY", "DEFAULT_VALUE")
	if value != "TEST_VALUE" {
		t.Errorf("Expected TEST_VALUE, but got %s", value)
	}

	// Test case when the environment variable does not exist
	value = GetEnvOrDefault("NON_EXISTENT_KEY", "DEFAULT_VALUE")
	if value != "DEFAULT_VALUE" {
		t.Errorf("Expected DEFAULT_VALUE, but got %s", value)
	}

	// Unset the environment variable after testing
	err = os.Unsetenv("TEST_KEY")
	assert.Nil(t, err)
}
