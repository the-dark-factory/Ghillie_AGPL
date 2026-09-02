package locale

import (
	"os"
	"path/filepath"
	"testing"
)

// writePack drops a pack file into a temp locale dir and points the package
// at it, undoing everything on cleanup. The SHIPPED directory is pointed at an
// empty temp dir for the duration: without that these cases would depend on
// whether the test binary happens to sit beside a locales/ tree.
func writePack(t *testing.T, lang, body string) {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, lang+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	oldDir, oldShipped := dir, shippedDir
	dir = func() string { return d }
	shippedDir = func() string { return empty }
	t.Cleanup(func() { dir, shippedDir = oldDir, oldShipped; reset() })
	reset()
}

// pointAt writes the given packs (empty body = no file at all) into two fresh
// temp directories and makes them the home and shipped search entries.
func pointAt(t *testing.T, lang, homeBody, shippedBody string) (homeDir, shipped string) {
	t.Helper()
	homeDir, shipped = t.TempDir(), t.TempDir()
	for _, w := range []struct{ d, body string }{{homeDir, homeBody}, {shipped, shippedBody}} {
		if w.body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(w.d, lang+".json"), []byte(w.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldDir, oldShipped := dir, shippedDir
	dir = func() string { return homeDir }
	shippedDir = func() string { return shipped }
	t.Cleanup(func() { dir, shippedDir = oldDir, oldShipped; reset() })
	reset()
	return homeDir, shipped
}

func TestRefusalsStayEnglishUntilReviewed(t *testing.T) {
	tests := []struct {
		name    string
		lang    string
		pack    string
		key     string
		refusal bool
		want    string
	}{
		{"draft pack localises chrome", "fr",
			`{"lang":"fr","reviewed":false,"strings":{"status.noted":"statut noté pour le prochain rapport"}}`,
			"status.noted", false, "statut noté pour le prochain rapport"},
		{"draft pack NEVER localises a refusal", "fr",
			`{"lang":"fr","reviewed":false,"strings":{"refuse.no-consent":"%s exige un consentement Fresh_Explicit; ici: %s"}}`,
			"refuse.no-consent", true, english["refuse.no-consent"]},
		{"reviewed pack may localise a refusal", "fr",
			`{"lang":"fr","reviewed":true,"strings":{"refuse.no-consent":"%s exige un consentement Fresh_Explicit; ici: %s"}}`,
			"refuse.no-consent", true, "%s exige un consentement Fresh_Explicit; ici: %s"},
		{"missing key falls back to English", "fr",
			`{"lang":"fr","reviewed":true,"strings":{}}`,
			"status.noted", false, english["status.noted"]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writePack(t, tt.lang, tt.pack)
			t.Setenv("GHILLIE_LANG", tt.lang)
			reset()
			got := T(tt.key)
			if tt.refusal {
				got = TRefusal(tt.key)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMalformedPackIsEnglishNotError(t *testing.T) {
	writePack(t, "de", `{not json`)
	t.Setenv("GHILLIE_LANG", "de")
	reset()
	if got := T("status.noted"); got != english["status.noted"] {
		t.Errorf("malformed pack must fall back to English, got %q", got)
	}
	if got := TRefusal("refuse.local-only"); got != english["refuse.local-only"] {
		t.Errorf("malformed pack must never touch refusals, got %q", got)
	}
}

// TestSearchPathOrder holds the v0.1.3 widening to its contract: the pack
// shipped beside the binary is found without anything being copied anywhere,
// and the member's own copy still wins wherever both exist.
func TestSearchPathOrder(t *testing.T) {
	const (
		homePack    = `{"lang":"de","reviewed":false,"strings":{"status.noted":"HOME"}}`
		shippedPack = `{"lang":"de","reviewed":false,"strings":{"status.noted":"SHIPPED"}}`
	)
	tests := []struct {
		name       string
		home       string
		shipped    string
		want       string
		wantSource string // "home", "shipped" or "builtin"
	}{
		{"shipped pack alone is loaded", "", shippedPack, "SHIPPED", "shipped"},
		{"home pack alone is loaded", homePack, "", "HOME", "home"},
		{"the member's own copy wins", homePack, shippedPack, "HOME", "home"},
		{"neither means English", "", "", english["status.noted"], "builtin"},
		{"a broken home pack does NOT fall through to the shipped one",
			`{not json`, shippedPack, english["status.noted"], "builtin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir, shipped := pointAt(t, "de", tt.home, tt.shipped)
			t.Setenv("GHILLIE_LANG", "de")
			reset()
			if got := T("status.noted"); got != tt.want {
				t.Errorf("T: got %q, want %q", got, tt.want)
			}
			wantSrc := map[string]string{
				"home":    filepath.Join(homeDir, "de.json"),
				"shipped": filepath.Join(shipped, "de.json"),
				"builtin": "built in",
			}[tt.wantSource]
			if got := Source(); got != wantSrc {
				t.Errorf("Source: got %q, want %q", got, wantSrc)
			}
		})
	}
}

// TestShippedPackNeverLocalisesARefusal is the one that matters: reaching the
// pack a different way must not reach it under different rules. A draft pack
// found beside the binary is still a draft pack.
func TestShippedPackNeverLocalisesARefusal(t *testing.T) {
	pointAt(t, "de", "",
		`{"lang":"de","reviewed":false,"strings":{"refuse.no-consent":"ENTWURF","status.noted":"SHIPPED"}}`)
	t.Setenv("GHILLIE_LANG", "de")
	reset()
	if got := T("status.noted"); got != "SHIPPED" {
		t.Errorf("chrome from a shipped draft pack must render, got %q", got)
	}
	if got := TRefusal("refuse.no-consent"); got != english["refuse.no-consent"] {
		t.Errorf("a shipped DRAFT pack must not localise a refusal, got %q", got)
	}
}

// TestLangReportsWhatIsInForce: a language asked for but not found is reported
// as English, because English is what will actually be printed.
func TestLangReportsWhatIsInForce(t *testing.T) {
	tests := []struct {
		name string
		pack string
		want string
	}{
		{"pack present", `{"lang":"fr","reviewed":false,"strings":{"status.noted":"x"}}`, "fr"},
		{"no pack anywhere", "", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pointAt(t, "fr", tt.pack, "")
			t.Setenv("GHILLIE_LANG", "fr")
			reset()
			if got := Lang(); got != tt.want {
				t.Errorf("Lang: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNoLangMeansEnglish(t *testing.T) {
	t.Setenv("GHILLIE_LANG", "")
	t.Setenv("LANG", "")
	reset()
	if got := T("status.noted"); got != english["status.noted"] {
		t.Errorf("default must be English, got %q", got)
	}
}
