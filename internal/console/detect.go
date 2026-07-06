package console

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kedwards/awst/v3/internal/paths"
)

// ContainerExtensionID is the stable Firefox add-on id of the awst Containers
// extension (see extension/manifest.json).
const ContainerExtensionID = "awst-containers@kedwards.github.io"

// ContainerExtensionInstalled reports whether the awst Containers Firefox
// extension is installed in any local Firefox profile. Detection is best-effort
// (a missing/unreadable profile just means "not found") and is used to decide
// whether `awst console` opens a container tab by default.
func ContainerExtensionInstalled() bool {
	for _, g := range firefoxExtensionGlobs() {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if extensionInFile(m) {
				return true
			}
		}
	}
	return false
}

// firefoxExtensionGlobs returns the per-OS globs that match every Firefox
// profile's extensions.json.
func firefoxExtensionGlobs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(paths.HomeDir(), "Library", "Application Support", "Firefox", "Profiles", "*", "extensions.json")}
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			return nil
		}
		return []string{filepath.Join(base, "Mozilla", "Firefox", "Profiles", "*", "extensions.json")}
	default:
		return []string{filepath.Join(paths.HomeDir(), ".mozilla", "firefox", "*", "extensions.json")}
	}
}

// extensionInFile reports whether the Firefox extensions.json at path lists an
// active awst Containers add-on, matched by its stable add-on id.
//
// It also matches the upstream Granted Containers extension by name ("granted",
// case-insensitive) as a transitional fallback so existing users aren't broken
// before they install awst's own extension. Drop the Granted branch once
// awst-containers is the documented default.
func extensionInFile(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var f struct {
		Addons []struct {
			ID            string `json:"id"`
			Active        bool   `json:"active"`
			DefaultLocale struct {
				Name string `json:"name"`
			} `json:"defaultLocale"`
		} `json:"addons"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return false
	}
	for _, a := range f.Addons {
		if !a.Active {
			continue
		}
		if a.ID == ContainerExtensionID {
			return true
		}
		if strings.Contains(strings.ToLower(a.DefaultLocale.Name), "granted") {
			return true
		}
	}
	return false
}
