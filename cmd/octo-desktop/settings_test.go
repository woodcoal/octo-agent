package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/datahome"
)

// TestSelectDesktopProfile covers the desktop shell's only CLI option. The
// "unknown argument" case is the interesting one: it must NOT fail, because
// this runs before setupCrashLog and a GUI launch has no stderr to explain
// itself on.
func TestSelectDesktopProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		profile string
		wantErr bool
	}{
		{"profile only", []string{"--profile", "work"}, "work", false},
		{"equals profile", []string{"--profile=work"}, "work", false},
		{"missing profile", []string{"--profile="}, "", true},
		{"invalid profile", []string{"--profile", "../escape"}, "", true},
		{"unknown argument is ignored", []string{"-psn_0_12345"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(datahome.ProfileEnv, "")
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			err := selectDesktopProfile(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("selectDesktopProfile(%q) error = %v, want error %v", tc.args, err, tc.wantErr)
			}
			if got := os.Getenv(datahome.ProfileEnv); got != tc.profile {
				t.Errorf("profile = %q, want %q", got, tc.profile)
			}
		})
	}
}

// TestDesktopSettings_WindowGeometryRoundTrip guards the on-disk contract for
// the remembered window geometry. A typo'd json tag would silently reset the
// window to its default size on every launch — the exact kind of quiet
// regression this test exists to catch.
func TestDesktopSettings_WindowGeometryRoundTrip(t *testing.T) {
	in := desktopSettings{
		WindowWidth:     1600,
		WindowHeight:    1000,
		WindowMaximised: true,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out desktopSettings
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.WindowWidth != in.WindowWidth || out.WindowHeight != in.WindowHeight {
		t.Errorf("size not preserved: got %dx%d, want %dx%d",
			out.WindowWidth, out.WindowHeight, in.WindowWidth, in.WindowHeight)
	}
	if out.WindowMaximised != in.WindowMaximised {
		t.Errorf("maximised not preserved: got %v, want %v", out.WindowMaximised, in.WindowMaximised)
	}
}

// TestDefaultDesktopSettings_NoGeometry documents that a fresh install carries
// zero geometry, which showWindowAt reads as "use the built-in default size".
func TestDefaultDesktopSettings_NoGeometry(t *testing.T) {
	s := defaultDesktopSettings()
	if s.WindowWidth != 0 || s.WindowHeight != 0 || s.WindowMaximised {
		t.Errorf("defaults should carry no saved geometry, got %+v", s)
	}
	if s.ConnectionMode != desktopConnectionLocal || s.RemoteURL != "" {
		t.Errorf("defaults should start locally, got %+v", s)
	}
}

// TestNormalizedConnection restricts remote desktop targets to an Octo service
// origin because the web client resolves its API and WebSocket paths from root.
func TestNormalizedConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    desktopConnectionMode
		remote  string
		wantURL string
		wantErr bool
	}{
		{"old settings default local", "", "", "", false},
		{"local ignores address", desktopConnectionLocal, "https://octo.example.com", "", false},
		{"https remote", desktopConnectionRemote, "https://octo.example.com/", "https://octo.example.com", false},
		{"http remote", desktopConnectionRemote, "http://10.0.0.8:8088", "http://10.0.0.8:8088", false},
		{"missing remote address", desktopConnectionRemote, "", "", true},
		{"unsupported scheme", desktopConnectionRemote, "ws://octo.example.com", "", true},
		{"path is rejected", desktopConnectionRemote, "https://octo.example.com/octo", "", true},
		{"query is rejected", desktopConnectionRemote, "https://octo.example.com?key=x", "", true},
		{"unknown mode", desktopConnectionMode("other"), "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotMode, gotURL, err := normalizedConnection(tc.mode, tc.remote)
			if (err != nil) != tc.wantErr {
				t.Fatalf("normalizedConnection(%q, %q) error = %v, want error %v", tc.mode, tc.remote, err, tc.wantErr)
			}
			wantMode := tc.mode
			if wantMode == "" {
				wantMode = desktopConnectionLocal
			}
			if !tc.wantErr && (gotMode != wantMode || gotURL != tc.wantURL) {
				t.Errorf("normalizedConnection(%q, %q) = (%q, %q), want (%q, %q)", tc.mode, tc.remote, gotMode, gotURL, wantMode, tc.wantURL)
			}
		})
	}
}

func TestDesktopSettingsPathUsesProfileDataHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct {
		name    string
		profile string
		root    string
	}{
		{"default profile", "", ".octo"},
		{"work profile", "work", ".octo-work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(datahome.ProfileEnv, tc.profile)
			got, err := desktopSettingsPath()
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(home, tc.root, "desktop.json")
			if got != want {
				t.Errorf("desktop settings path = %q, want %q", got, want)
			}
		})
	}
}
