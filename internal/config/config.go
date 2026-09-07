// Package config holds APCode's build-time and runtime configuration.
package config

import (
	"os"
	"path/filepath"
)

// Version is the current APCode version.
// Overridden at build time via ldflags: -ldflags "-X apcode/internal/config.Version=x.y.z" (current: 0.1.8)
var Version = "0.1.8"

// AppName is the human-readable application name.
const AppName = "APCode"

// DefaultModelDir returns the default model directory path.
func DefaultModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "models")
	}
	return filepath.Join(home, ".apcode", "models")
}
