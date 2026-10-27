// Package environments reads the app's settings from environment variables.
package environments

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

// GetEnvInt reads a whole number of at least minValue. Any other value stops
// the app at start instead of running with a setting nobody chose.
func GetEnvInt(key string, defaultValue, minValue int) int {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < minValue {
		panic(fmt.Sprintf("%s must be a whole number of %d or more, got %q", key, minValue, value))
	}

	return n
}

// GetEnvDuration reads a positive Go duration such as "5m" or "90s". Any other
// value stops the app at start.
func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}

	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		panic(fmt.Sprintf("%s must be a positive duration such as 5m, got %q", key, value))
	}

	return d
}
