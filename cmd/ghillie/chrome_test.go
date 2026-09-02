package main

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"
)

// The locale package resolves its pack ONCE per process and offers no exported
// reset, by design — the interface language is not a thing that changes under a
// running terminal. These tests therefore hold the chrome to its English
// behaviour, which is the behaviour every un-packed machine gets; that a pack
// is actually picked up from either search directory is held by the table in
// internal/locale, and the end-to-end render is checked against a built binary
// on the release path (make locale-demo).

func TestVersionLineCarriesTheVersionVerbatim(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a release tag", "v0.1.3", "ghillie v0.1.3"},
		{"a dirty tree", "v0.1.3-2-gdeadbee-dirty", "ghillie v0.1.3-2-gdeadbee-dirty"},
		{"an unstamped build", "dev", "ghillie dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionLine(tt.in); got != tt.want {
				t.Errorf("versionLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A machine with no pack must say nothing about language at all: a line
// announcing "interface language: en" on every -h is noise on the one path
// where noise is least welcome.
func TestLangLineSilentInEnglish(t *testing.T) {
	t.Setenv("GHILLIE_LANG", "")
	if got := langLine(); got != "" {
		t.Errorf("langLine in English must be empty, got %q", got)
	}
}

// -h must print the version banner and then the flags. The regression this
// guards is a usage function that replaces flag.PrintDefaults rather than
// preceding it — which would silently delete the flag reference.
func TestUsagePrintsBannerThenFlags(t *testing.T) {
	oldCmdLine, oldUsage := flag.CommandLine, flag.Usage
	t.Cleanup(func() { flag.CommandLine, flag.Usage = oldCmdLine, oldUsage })

	flag.CommandLine = flag.NewFlagSet("ghillie", flag.ContinueOnError)
	flag.String("facade", "", "facade base URL")
	var buf bytes.Buffer
	usageTo(&buf)

	out := buf.String()
	for _, want := range []string{"ghillie ", "Flags:", "-facade"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage output missing %q; got:\n%s", want, out)
		}
	}
	if i, j := strings.Index(out, "Flags:"), strings.Index(out, "-facade"); i < 0 || j < 0 || i > j {
		t.Errorf("banner must come before the flag defaults; got:\n%s", out)
	}
}

// usageTo borrows the flag set's output while it prints and must give it back:
// leaving it pointed at a caller's buffer would send every later parse error
// somewhere nobody is reading.
func TestUsageRestoresTheFlagSetOutput(t *testing.T) {
	oldCmdLine := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = oldCmdLine })

	flag.CommandLine = flag.NewFlagSet("ghillie", flag.ContinueOnError)
	var errs, help bytes.Buffer
	flag.CommandLine.SetOutput(&errs)
	usageTo(&help)

	if flag.CommandLine.Output() != io.Writer(&errs) {
		t.Error("usageTo must restore the flag set's own output when it is done")
	}
	if help.Len() == 0 {
		t.Error("usageTo wrote nothing to the writer it was given")
	}
	if errs.Len() != 0 {
		t.Errorf("usageTo must not write to the error output; got %q", errs.String())
	}
}
