// getEnvOrFile returns the value of the environment variable if set, otherwise if a corresponding _FILE variable is set, reads the value from the file at that path.
// If both are set, the environment variable takes precedence.
package githosts

import (
	"os"
	"strings"
)

// getEnvOrFile returns the value of the environment variable if set, otherwise if a corresponding _FILE variable is set, reads the value from the file at that path.
func getEnvOrFile(envVar string) string {
	val := strings.TrimSpace(os.Getenv(envVar))
	if val != "" {
		return val
	}

	fileEnv := envVar + "_FILE"
	filePath := strings.TrimSpace(os.Getenv(fileEnv))
	if filePath != "" {
		if b, err := os.ReadFile(filePath); err == nil {
			return strings.TrimSpace(string(b))
		}
	}

	return ""
}
