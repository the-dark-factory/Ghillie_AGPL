package main

// The state-location policy as a table. The defect being fixed here was
// identity-affecting — a second run from a second directory minted a second
// claw — so the case that matters most is the third one: legacy state in the
// cwd, nothing in the home, and the answer must be "use theirs", never "make a
// new one".

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setOf turns a list of present paths into the predicate resolveStateDir takes.
func setOf(present ...string) func(string) bool {
	m := make(map[string]bool, len(present))
	for _, p := range present {
		m[p] = true
	}
	return func(path string) bool { return m[path] }
}

// TestResolveStateDir holds the resolution order to its four cases.
func TestResolveStateDir(t *testing.T) {
	const home = "/home/someone/.ghillie"
	const cwd = "/work/project"

	tests := []struct {
		name         string
		present      []string
		cwdIsHome    bool // the run happens to be from the home itself
		homeExplicit bool // $GHILLIE_HOME was set: the operator NAMED this home
		wantDir      string
		wantNotice   string // substring, "" = the notice must be empty
	}{
		{
			name:         "cwd legacy but the home was NAMED — the named home wins, out loud",
			present:      []string{filepath.Join(cwd, "abilities")},
			homeExplicit: true,
			wantDir:      home,
			wantNotice:   "is IGNORED",
		},
		{
			name:         "a bare quarantine dir must not capture an explicitly named home",
			present:      []string{filepath.Join(cwd, "quarantine")},
			homeExplicit: true,
			wantDir:      home,
			wantNotice:   "was named explicitly",
		},
		{
			name:         "named home with its OWN state — settled, and still silent",
			present:      []string{filepath.Join(home, "ghillie-device.key")},
			homeExplicit: true,
			wantDir:      home,
			wantNotice:   "",
		},
		{
			name:       "neither — a first run picks the home and says nothing",
			present:    nil,
			wantDir:    home,
			wantNotice: "",
		},
		{
			name:       "home only — the settled case, silent",
			present:    []string{filepath.Join(home, "ghillie-device.key"), filepath.Join(home, "ghillie-state.json")},
			wantDir:    home,
			wantNotice: "",
		},
		{
			name:       "cwd legacy only — used where it sits, out loud",
			present:    []string{filepath.Join(cwd, "ghillie-device.key")},
			wantDir:    cwd,
			wantNotice: "legacy state found beside you",
		},
		{
			name:       "cwd legacy, state file alone — still a claw's directory",
			present:    []string{filepath.Join(cwd, "ghillie-state.json")},
			wantDir:    cwd,
			wantNotice: "legacy state found beside you",
		},
		{
			name:       "cwd legacy, abilities tree alone — still a claw's directory",
			present:    []string{filepath.Join(cwd, "abilities")},
			wantDir:    cwd,
			wantNotice: "legacy state found beside you",
		},
		{
			name:       "cwd legacy, encryption key alone",
			present:    []string{filepath.Join(cwd, "ghillie-encrypt.key")},
			wantDir:    cwd,
			wantNotice: "legacy state found beside you",
		},
		{
			name:       "cwd legacy, pinned facade key alone",
			present:    []string{filepath.Join(cwd, "ghillie-facade.pin")},
			wantDir:    cwd,
			wantNotice: "legacy state found beside you",
		},
		{
			name:       "cwd legacy, quarantine alone",
			present:    []string{filepath.Join(cwd, "quarantine")},
			wantDir:    cwd,
			wantNotice: "legacy state found beside you",
		},
		{
			name: "both — the home wins and the cwd copy is named",
			present: []string{
				filepath.Join(home, "ghillie-device.key"),
				filepath.Join(cwd, "ghillie-device.key"),
			},
			wantDir:    home,
			wantNotice: "state found in BOTH",
		},
		{
			name:       "cwd IS the home — no double-counting, no notice",
			present:    []string{filepath.Join(home, "ghillie-device.key")},
			cwdIsHome:  true,
			wantDir:    home,
			wantNotice: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			here := cwd
			if tt.cwdIsHome {
				here = home
			}
			gotDir, gotNotice := resolveStateDir(home, here, tt.homeExplicit, setOf(tt.present...))
			if gotDir != tt.wantDir {
				t.Errorf("dir = %q, want %q", gotDir, tt.wantDir)
			}
			switch {
			case tt.wantNotice == "" && gotNotice != "":
				t.Errorf("notice = %q, want none", gotNotice)
			case tt.wantNotice != "" && !strings.Contains(gotNotice, tt.wantNotice):
				t.Errorf("notice = %q, want it to contain %q", gotNotice, tt.wantNotice)
			}
		})
	}
}

// TestResolveStateDirNeverMintsBesideLegacy is the defect itself, stated as a
// property rather than a case: whenever a cwd holds state and the home does
// not, the answer is the cwd. Nothing else is an acceptable answer, because
// anything else generates a fresh device key and orphans every enrolment made
// under the old one.
func TestResolveStateDirNeverMintsBesideLegacy(t *testing.T) {
	const home = "/home/someone/.ghillie"
	const cwd = "/somewhere/else"

	for _, marker := range stateMarkers {
		t.Run(marker, func(t *testing.T) {
			dir, notice := resolveStateDir(home, cwd, false, setOf(filepath.Join(cwd, marker)))
			if dir != cwd {
				t.Fatalf("marker %q in the cwd resolved to %q — a second identity would be minted", marker, dir)
			}
			if notice == "" {
				t.Errorf("marker %q in the cwd resolved silently; the fallback must always be said out loud", marker)
			}
		})
	}
}

// TestGhillieHome holds the home override to its order: the environment first,
// then ~/.ghillie.
func TestGhillieHome(t *testing.T) {
	t.Run("env override wins", func(t *testing.T) {
		t.Setenv(homeEnv, "/opt/claws/one")
		if got := ghillieHome(); got != "/opt/claws/one" {
			t.Errorf("ghillieHome() = %q, want the environment's value", got)
		}
	})

	t.Run("no override — under the user home", func(t *testing.T) {
		t.Setenv(homeEnv, "")
		h, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no user home on this machine: %v", err)
		}
		want := filepath.Join(h, ".ghillie")
		if got := ghillieHome(); got != want {
			t.Errorf("ghillieHome() = %q, want %q", got, want)
		}
	})
}

// TestHasState is the marker predicate on its own: an empty directory is not a
// claw's, and any single marker makes it one.
func TestHasState(t *testing.T) {
	const dir = "/d"

	if hasState(dir, setOf()) {
		t.Error("an empty directory was read as holding state")
	}
	for _, m := range stateMarkers {
		if !hasState(dir, setOf(filepath.Join(dir, m))) {
			t.Errorf("marker %q alone did not make the directory a claw's", m)
		}
	}
	if hasState(dir, setOf("/other/ghillie-device.key")) {
		t.Error("a marker in ANOTHER directory was counted")
	}
}
