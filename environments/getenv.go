// Package environments reads the app's settings from environment variables.
package environments

import (
	"fmt"
	"os"
	"strconv"
)

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}

// GetEnvInt reads a whole number of 0 or more. Any other value stops the app
// at start instead of running with a setting nobody chose.
func GetEnvInt(key string, defaultValue int) int {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		panic(fmt.Sprintf("%s must be a whole number of 0 or more, got %q", key, value))
	}

	return n
}
