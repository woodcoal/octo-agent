package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-octo/octo-agent/internal/datahome"
)

// desktopConnectionMode describes whether the desktop shell owns a local hub or
// only renders a remote Octo service.
type desktopConnectionMode string

const (
	desktopConnectionLocal  desktopConnectionMode = "local"
	desktopConnectionRemote desktopConnectionMode = "remote"
)

// desktopSettings holds the desktop app's per-machine preferences — the ones
// the server itself has no opinion about. Persisted to the profile-scoped Octo
// data root's desktop.json so they survive a relaunch.
type desktopSettings struct {
	// ConnectionMode defaults to local so existing desktop.json files retain the
	// current embedded-hub behavior.
	ConnectionMode desktopConnectionMode `json:"connection_mode,omitempty"`
	// RemoteURL is the root URL of an Octo service when ConnectionMode is remote.
	// Access keys are deliberately excluded and stay in the WebView's origin-scoped
	// storage after the service's normal authentication flow completes.
	RemoteURL string `json:"remote_url,omitempty"`
	// window is closed, hiding to the tray instead of quitting. Default true —
	// closing the window shouldn't drop a VS Code / phone client's backend.
	KeepRunningInBackground bool `json:"keep_running_in_background"`
	// SeededOctoVersion records the version of the octo CLI this app last seeded
	// to ~/.local/bin (macOS + Linux; Windows ships the CLI through its
	// installer). It scopes the refresh-on-upgrade: an octo on ~/.local/bin
	// with no matching record here is the user's own and is never overwritten.
	SeededOctoVersion string `json:"seeded_octo_version,omitempty"`
	// WindowWidth/WindowHeight remember the window's non-maximised size across
	// relaunches. Zero (a fresh install) means "use the built-in default size".
	// They hold the *restore* size — while maximised the size isn't overwritten,
	// so un-maximising after a relaunch returns to the size the user last chose.
	WindowWidth  int `json:"window_width,omitempty"`
	WindowHeight int `json:"window_height,omitempty"`
	// WindowMaximised remembers whether the window was maximised at exit, so a
	// relaunch reopens maximised instead of at the restore size.
	WindowMaximised bool `json:"window_maximised,omitempty"`
}

// defaultDesktopSettings is what a first launch (no file yet) uses.
func defaultDesktopSettings() desktopSettings {
	return desktopSettings{
		ConnectionMode:          desktopConnectionLocal,
		KeepRunningInBackground: true,
	}
}

// normalizedConnection returns validated settings with a canonical remote URL.
// It accepts only root HTTP(S) service URLs because the web client resolves
// /api and /ws from the origin root.
func normalizedConnection(mode desktopConnectionMode, remoteURL string) (desktopConnectionMode, string, error) {
	if mode == "" {
		mode = desktopConnectionLocal
	}
	if mode == desktopConnectionLocal {
		return mode, "", nil
	}
	if mode != desktopConnectionRemote {
		return "", "", fmt.Errorf("未知连接模式 %q", mode)
	}

	u, err := url.Parse(strings.TrimSpace(remoteURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("远程服务地址无效")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("远程服务地址必须使用 HTTP 或 HTTPS")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", "", fmt.Errorf("远程服务地址必须是服务根地址")
	}
	u.Path = ""
	return mode, strings.TrimRight(u.String(), "/"), nil
}

func desktopSettingsPath() (string, error) {
	dir, err := datahome.Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "desktop.json"), nil
}

// loadDesktopSettings reads the profile-scoped desktop.json, falling back to defaults for
// a missing or unreadable file (a fresh install, or a hand-corrupted one — the
// defaults are safe either way).
func loadDesktopSettings() desktopSettings {
	s := defaultDesktopSettings()
	path, err := desktopSettingsPath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s) // partial/corrupt JSON keeps the defaults it couldn't override
	mode, remoteURL, err := normalizedConnection(s.ConnectionMode, s.RemoteURL)
	if err != nil {
		return defaultDesktopSettings()
	}
	s.ConnectionMode = mode
	s.RemoteURL = remoteURL
	return s
}

// saveDesktopSettings writes the settings back to the profile-scoped desktop.json.
func saveDesktopSettings(s desktopSettings) error {
	mode, remoteURL, err := normalizedConnection(s.ConnectionMode, s.RemoteURL)
	if err != nil {
		return err
	}
	s.ConnectionMode = mode
	s.RemoteURL = remoteURL
	path, err := desktopSettingsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
