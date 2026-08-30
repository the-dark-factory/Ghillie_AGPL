// Package locale gives the terminal's fixed strings a language, under one
// hard rule inherited from the mouth policy: A REFUSAL IS NEVER REPHRASED.
//
// Two classes of string, deliberately unequal:
//
//   - CHROME (confirmations, notices): may render from a locale pack even
//     while the pack is a machine draft awaiting native review.
//   - REFUSALS: render from a pack ONLY when that pack is marked
//     Reviewed=true by a human. Until then a refusal goes out in English —
//     the exact words the design proved its properties over. A refusal
//     machine-translated on the fly is a refusal softened with extra steps.
//
// Packs are plain JSON a member can read, correct and re-hash:
//
//	~/.ghillie/locales/<lang>.json
//	{"lang":"fr","reviewed":false,"strings":{"key":"template with %s"}}
//
// TWO PLACES ARE SEARCHED, IN THIS ORDER (v0.1.3):
//
//  1. the ghillie home — ~/.ghillie/locales, or $GHILLIE_HOME/locales. The
//     member's own copy, the one they edit and review, always wins.
//  2. locales/ BESIDE THE BINARY — the directory the release tarball unpacks
//     alongside ghillie itself.
//
// The second entry is why a stranger who unpacks a tarball and runs
// GHILLIE_LANG=de ./ghillie sees German without first being told, in a table
// cell, to copy files into a dot-directory. Nothing is copied and nothing is
// written: the shipped pack is read where it lies, so an unpacked tarball
// stays exactly as unpacked, and the home copy overrides it the moment one
// exists.
//
// The language is chosen by GHILLIE_LANG (e.g. "fr"), else the LANG
// environment prefix, else English. A missing pack, a missing key, or a
// malformed file falls back to English silently at the STRING level and is
// never an error: language must not be able to break the machine.
package locale

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Pack is one language's string table.
type Pack struct {
	Lang     string            `json:"lang"`
	Reviewed bool              `json:"reviewed"`
	Strings  map[string]string `json:"strings"`
}

// english is the source of truth. Every key used anywhere appears here; a
// pack overrides keys it knows and inherits the rest.
var english = map[string]string{
	// chrome
	"status.noted":      "status noted for the next report",
	"catalogue.offered": "catalogue index offered (ref %#016x, version %d) — display only, nothing installed",
	"delivery.notice":   "delivery notice quarantined at %s — not fetched, not installed, not executed",
	"delivery.nointerv": "; no interviewer configured, so the body was not fetched",
	// chrome — the STATIC commands a newcomer actually types first (v0.1.3).
	// Before these existed the four keys above fired only deep inside a live
	// interview, so every tester who checked localisation the natural way
	// (-version, -h, -abilities-available) concluded the feature did nothing.
	"chrome.version":    "ghillie %s",
	"chrome.usage":      "ghillie — the customer-side terminal. Flags:",
	"chrome.lang":       "interface language: %s (pack from %s; refusals stay English until the pack is reviewed by a human)",
	"catalogue.header":  "%s (published %s) — %d ability(ies)",
	"catalogue.needs":   "    needs: %s",
	"abilities.none":    "no abilities installed here yet",
	"abilities.removed": "removed %s — recorded in the ledger; its tab closes when a display next looks",
	// refusals (review-gated)
	"refuse.local-only":   "%s is local-only by construction — refused at EVERY ceiling and EVERY consent level (ledger 112, NO-REMOTE-REACH-FOR-LOCAL-WORK)",
	"refuse.over-ceiling": "rank %d is above this machine's ceiling of %s (rank %d)",
	"refuse.no-consent":   "%s needs Fresh_Explicit consent from a human at this machine; consent here is %s",
}

var (
	once   sync.Once
	active Pack
	// source records where the active pack was read from, so -h and -version
	// can say which file is speaking rather than leaving the member guessing
	// why their translation did or did not take.
	source string
)

// dir returns the MEMBER'S locale-pack directory — the ghillie home's
// locales/. Overridable for tests. GHILLIE_HOME is honoured here for the same
// reason it is honoured for the state file: a machine that keeps its ghillie
// home somewhere else keeps its language packs with it.
var dir = func() string {
	if h := os.Getenv("GHILLIE_HOME"); h != "" {
		return filepath.Join(h, "locales")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ghillie", "locales")
}

// shippedDir returns the locales/ directory the release tarball unpacks BESIDE
// the binary. Overridable for tests. It resolves symlinks so a ghillie reached
// through one still finds the packs that travelled with it, and returns "" when
// the executable's own path cannot be determined — in which case the search
// simply has one entry instead of two.
var shippedDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "locales")
}

// searchPath is the pack search order: the member's own copy first, the copy
// shipped beside the binary second. Empty entries are dropped so a machine with
// no determinable home or executable path still searches the other one.
func searchPath() []string {
	var out []string
	for _, d := range []string{dir(), shippedDir()} {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// load resolves the active pack once. Absence of everything means English.
func load() {
	lang := os.Getenv("GHILLIE_LANG")
	if lang == "" {
		if l := os.Getenv("LANG"); len(l) >= 2 {
			lang = l[:2]
		}
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" || lang == "en" {
		active, source = Pack{Lang: "en", Reviewed: true}, "built in"
		return
	}
	for _, d := range searchPath() {
		path := filepath.Join(d, lang+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p Pack
		if err := json.Unmarshal(raw, &p); err != nil || p.Strings == nil {
			// A MALFORMED PACK IS NOT A REASON TO KEEP LOOKING. The member
			// named this file by naming the language; falling through to a
			// shipped copy would render a language from a file they did not
			// edit while their own broken one sat there unmentioned.
			break
		}
		p.Lang = lang
		active, source = p, path
		return
	}
	active, source = Pack{Lang: "en", Reviewed: true}, "built in"
}

// Lang returns the interface language actually in force — "en" whenever no
// pack was found, whatever GHILLIE_LANG asked for.
func Lang() string {
	once.Do(load)
	return active.Lang
}

// Source returns where the active pack was read from: a file path, or
// "built in" for the English source of truth. It is what the chrome prints so
// a member can see which file their translation came from.
func Source() string {
	once.Do(load)
	return source
}

// reset is a test hook: forget the resolved pack so load runs again.
func reset() { once = sync.Once{}; active = Pack{}; source = "" }

// T returns the chrome template for key in the active language, falling back
// to English per key. Draft packs are honoured for chrome.
func T(key string) string {
	once.Do(load)
	if s, ok := active.Strings[key]; ok && s != "" {
		return s
	}
	return english[key]
}

// TRefusal returns the refusal template for key. A pack's translation is
// used ONLY when the pack is human-reviewed; otherwise the English stands,
// whatever the interface language. This is the mouth rule at the string
// layer: the exact words ARE the service.
func TRefusal(key string) string {
	once.Do(load)
	if active.Reviewed {
		if s, ok := active.Strings[key]; ok && s != "" {
			return s
		}
	}
	return english[key]
}
