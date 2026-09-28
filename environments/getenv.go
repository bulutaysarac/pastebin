// Package environments reads the app's settings from environment variables.
package environments

import "os"

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultValue
}
