package utils

import "os"

// GetEnvOrDefault returns the value of the environment variable with the given key, or the given default value if the env is not set.
func GetEnvOrDefault(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
