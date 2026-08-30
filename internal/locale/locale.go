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
	// refusals (review-gated)
	"refuse.local-only":   "%s is local-only by construction — refused at EVERY ceiling and EVERY consent level (ledger 112, NO-REMOTE-REACH-FOR-LOCAL-WORK)",
	"refuse.over-ceiling": "rank %d is above this machine's ceiling of %s (rank %d)",
	"refuse.no-consent":   "%s needs Fresh_Explicit consent from a human at this machine; consent here is %s",
}

var (
	once   sync.Once
	active Pack
)

// dir returns the locale-pack directory, overridable for tests.
var dir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ghillie", "locales")
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
		active = Pack{Lang: "en", Reviewed: true}
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir(), lang+".json"))
	if err != nil {
		active = Pack{Lang: "en", Reviewed: true}
		return
	}
	var p Pack
	if err := json.Unmarshal(raw, &p); err != nil || p.Strings == nil {
		active = Pack{Lang: "en", Reviewed: true}
		return
	}
	p.Lang = lang
	active = p
}

// reset is a test hook: forget the resolved pack so load runs again.
func reset() { once = sync.Once{}; active = Pack{} }

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
