// Package config holds APCode's build-time and runtime configuration.
package config

import (
	"os"
	"path/filepath"
)

// Version is the current APCode version.
// Overridden at build time via ldflags: -ldflags "-X apcode/internal/config.Version=x.y.z" (current: 0.1.11)
var Version = "0.1.11"

// AppName is the human-readable application name.
const AppName = "APCode"

// FreeCloudAPIKey returns the API key for the free cloud provider.
// It reads from the APCode_FREE_API_KEY environment variable.
func FreeCloudAPIKey() string {
	return os.Getenv("APCode_FREE_API_KEY")
}

// FreeCloudBaseURL returns the base URL for the free cloud provider.
// It reads from the APCode_FREE_BASE_URL environment variable.
func FreeCloudBaseURL() string {
	return os.Getenv("APCode_FREE_BASE_URL")
}

// FreeCloudEnabled returns whether the free cloud provider is enabled.
// It reads from the APCode_FREE_ENABLED environment variable.
func FreeCloudEnabled() bool {
	return os.Getenv("APCode_FREE_ENABLED") == "true"
}

// DefaultModelDir returns the default model directory path.
func DefaultModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "models")
	}
	return filepath.Join(home, ".apcode", "models")
}
