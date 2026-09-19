package main

// THE GHILLIE HOME — one place on this machine where a claw's identity lives.
//
// Until v0.1.2 every durable thing this terminal owns (the state file, the
// device key, the encryption key, the abilities tree, the quarantine) defaulted
// to the CURRENT WORKING DIRECTORY. That is a quiet identity bug, not a tidiness
// one: run ghillie from a second directory and it finds no device key, generates
// a fresh one, and the machine is now two claws. Every enrolment made under the
// first key is orphaned, silently, with no message saying so.
//
// The fix is that the default is a PLACE, not a cwd: $GHILLIE_HOME, or
// ~/.ghillie. The flags still win when given — an operator naming a path is an
// operator naming a path — and legacy state already sitting in a cwd is used
// where it is, out loud, rather than being shadowed by a new identity.

import (
	"os"
	"path/filepath"
)

// homeEnv names the environment override for the ghillie home. It exists so a
// machine with an established layout — the estate's own, or an operator who
// keeps state on another volume — keeps working without a flag on every run.
const homeEnv = "GHILLIE_HOME"

// stateMarkers are the files and directories whose presence in a directory
// means "a claw already lives here". The device key is the identity proper;
// the others are named too because a half-populated directory is still a
// claw's directory, and treating it as empty is how the second identity gets
// minted.
var stateMarkers = []string{
	"ghillie-state.json",
	"ghillie-device.key",
	"ghillie-encrypt.key",
	"ghillie-facade.pin",
	"abilities",
	"quarantine",
}

// ghillieHome returns the directory this machine keeps ghillie's durable state
// in: $GHILLIE_HOME when set, otherwise ~/.ghillie. It returns "." only when
// the home directory cannot be determined at all, which is the same fallback
// the pre-v0.1.2 behaviour had and so is never worse than it was.
func ghillieHome() string {
	if p := os.Getenv(homeEnv); p != "" {
		return p
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(h, ".ghillie")
}

// homeIsExplicit reports whether the operator NAMED the home, rather than
// getting the ~/.ghillie default. It is the difference between "this machine
// has no configured home" and "this machine's home is over there", and the
// resolution below turns on it.
func homeIsExplicit() bool { return os.Getenv(homeEnv) != "" }

// homeSub returns a path under the ghillie home.
func homeSub(parts ...string) string {
	return filepath.Join(append([]string{ghillieHome()}, parts...)...)
}

// pathExists reports whether a path exists. It is the production implementation
// of the predicate resolveStateDir takes, so the resolution order can be held
// to a table without touching a disk.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hasState reports whether dir holds any of the markers that make a directory a
// claw's own.
func hasState(dir string, exists func(string) bool) bool {
	for _, m := range stateMarkers {
		if exists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

// resolveStateDir decides where this run's state lives, and returns a one-line
// notice when the answer is worth saying out loud.
//
// The whole policy, in the order it applies:
//
//  0. AN EXPLICITLY NAMED HOME WINS OVER A CWD, always. $GHILLIE_HOME being
//     set is an operator naming a path, exactly as -state is, and the module's
//     own rule is that an operator naming a path is an operator naming a path.
//     This branch exists because TWO of the stateMarkers are generic directory
//     names — `abilities` and `quarantine` — so an unrelated directory that
//     merely contains one of them was silently adopting the claw's identity in
//     preference to the home the operator had just named. Reproduced with a
//     directory holding nothing but an empty `abilities/`; on this machine both
//     ~/ObVault and ~/dev/ghillie trigger it.
//  1. STATE IN THE HOME WINS, always. It is the place the product means, and a
//     claw that has one has an identity there already.
//  2. NO HOME STATE BUT LEGACY STATE IN THE CWD: the legacy state is used where
//     it sits, with a notice. Minting a second identity in the home while a
//     first one lies in the cwd is exactly the defect being fixed — the one
//     thing this function must never do.
//  3. BOTH: the home wins, and the cwd copy is NAMED so nobody wonders why
//     their claw looks different from one directory to the next.
//  4. NEITHER: the home, quietly. A first run has nothing to warn about.
//
// exists is injected so the table can drive every branch without a filesystem.
func resolveStateDir(home, cwd string, homeExplicit bool, exists func(string) bool) (dir string, notice string) {
	homeHas := hasState(home, exists)
	cwdHas := cwd != "" && cwd != home && hasState(cwd, exists)

	switch {
	case cwdHas && !homeHas && homeExplicit:
		return home, "state lies beside you in " + cwd + " and is IGNORED — " + home + " was named explicitly, and a named home wins (name the copy beside you with -state to use it instead)"
	case homeHas && cwdHas:
		return home, "state found in BOTH " + home + " and " + cwd + " — using the home; the copy beside you is left untouched and ignored (name it with -state to use it instead)"
	case homeHas:
		return home, ""
	case cwdHas:
		return cwd, "legacy state found beside you in " + cwd + " — using it where it sits rather than minting a second identity in " + home + " (move it there when convenient, or keep naming it with -state)"
	default:
		return home, ""
	}
}
