package locale

import (
	"os"
	"path/filepath"
	"testing"
)

// writePack drops a pack file into a temp locale dir and points the package
// at it, undoing everything on cleanup.
func writePack(t *testing.T, lang, body string) {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, lang+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDir := dir
	dir = func() string { return d }
	t.Cleanup(func() { dir = oldDir; reset() })
	reset()
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

func TestNoLangMeansEnglish(t *testing.T) {
	t.Setenv("GHILLIE_LANG", "")
	t.Setenv("LANG", "")
	reset()
	if got := T("status.noted"); got != english["status.noted"] {
		t.Errorf("default must be English, got %q", got)
	}
}
