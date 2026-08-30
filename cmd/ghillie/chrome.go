package main

// THE CHROME SPEAKS THE INTERFACE LANGUAGE (v0.1.3).
//
// Until this file existed, GHILLIE_LANG was wired but NARROW: the four
// localised keys fired only from inside a live interview (a status note, a
// catalogue offer, a delivery notice). Every one of the STATIC commands a
// newcomer types first — -version, -h, -abilities-available — printed English
// no matter what pack was installed, so the natural way to check localisation
// was also the way guaranteed to conclude it did nothing. Two synthetic
// walkthroughs on Linux containers reached exactly that conclusion on
// 2026-08-30, independently, on both architectures.
//
// The rule the widening obeys is the one internal/locale already states and
// does not get to be softened here: CHROME may render from a draft pack;
// REFUSALS render from a pack only when a human has marked it reviewed. Every
// string below is chrome — a heading, a label, a version line. Nothing on this
// path can refuse anything, which is precisely why it is safe to translate it
// from a machine draft.

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tonygair/ghillie/internal/locale"
)

// versionLine renders the -version line in the interface language. The version
// string itself is never translated: it is an identifier, and a tag rendered
// differently per locale is a tag nobody can grep a bug report for.
func versionLine(v string) string {
	return fmt.Sprintf(locale.T("chrome.version"), v)
}

// langLine reports the interface language actually in force and the file it
// came from, or the empty string when the answer is plain English from the
// built-in table.
//
// It is printed rather than inferred because the two failure modes a member
// hits are indistinguishable in silence: GHILLIE_LANG naming a language with no
// pack anywhere, and a pack that was found but whose keys are missing. Naming
// the source file separates them in one line.
func langLine() string {
	if locale.Lang() == "en" {
		return ""
	}
	return fmt.Sprintf(locale.T("chrome.lang"), locale.Lang(), locale.Source())
}

// usageTo writes the -h banner — the version line, the language line when a
// pack is in force — and then the flag defaults, all to w.
//
// The FLAG DESCRIPTIONS STAY ENGLISH, deliberately. They are the reference
// documentation for a security boundary, they run to paragraphs, and a machine
// draft of them would be a machine draft of the safety argument. The banner is
// what proves the pack is live; the flags are what proves it is understood, and
// that waits for a reviewer.
func usageTo(w io.Writer) {
	prev := flag.CommandLine.Output()
	flag.CommandLine.SetOutput(w)
	defer flag.CommandLine.SetOutput(prev)

	fmt.Fprintln(w, versionLine(version))
	if l := langLine(); l != "" {
		fmt.Fprintln(w, l)
	}
	fmt.Fprintln(w, locale.T("chrome.usage"))
	flag.PrintDefaults()
}

// usage sends -h to STDOUT, not stderr.
//
// Go's flag package defaults usage to stderr, which is right for the error
// path — a mistyped flag is a diagnostic and belongs there — and wrong for the
// path where a person asked to read the help. `ghillie -h | head` and
// `ghillie -h | grep glass` are the two most obvious things to do with a
// hundred-flag binary, and both come back empty when the help goes to stderr.
// The quickstart prints three lines of -h to show the locale pack working, so
// this is not hypothetical. Parse ERRORS still go to stderr: installUsage
// leaves the flag set pointed there and usageTo only borrows the output for as
// long as it is printing.
func usage() { usageTo(os.Stdout) }

// installUsage points the flag package at usage. It is called before
// flag.Parse, which is what makes -h reach it at all.
func installUsage() {
	flag.CommandLine.SetOutput(os.Stderr)
	flag.Usage = usage
}
