// Package acceptance reads and strictly validates an extension's acceptance
// statement — the machine-checkable "here is what done looks like" file that
// travels in a bundle beside its surface (design memory:
// project_extension_test_gate_and_catalogue).
//
// This package answers ONE narrow question: is a given acceptance statement
// well-formed and self-consistent? It runs no surface, drives no agent,
// compares no output and reaches no conclusion about whether an extension does
// its job. Establishing whether the checks hold is a SEPARATE proven acceptance
// core's work, reached through a thin exec-shim in the external-front shape this
// repo already uses for proven cores. This package only makes sure the input
// handed to that core is not malformed; it is input validation, nothing more.
//
// ★ A MALFORMED STATEMENT IS REFUSED WHOLE. Every doubt — an unknown field, a
// path that climbs out of the bundle, a check that expects nothing — resolves at
// Parse time to a named sentinel saying which. A statement that does not parse
// never reaches the core.
package acceptance

// Spec is one extension's whole acceptance statement: what its surface is for,
// and the checks that describe what "done" looks like. A Spec is trustworthy
// only when it was obtained from Parse or Load; a zero Spec or one assembled by
// hand has been through no validation.
type Spec struct {
	// Purpose names the extension and states, in one sentence, what it is for.
	Purpose Purpose `json:"purpose"`
	// Checks is the non-empty list of re-runnable observations of the surface.
	Checks []Check `json:"checks"`
}

// Purpose names the extension's intent for a human and for the machine.
type Purpose struct {
	// ID is a kebab-case identifier for the purpose, e.g. "flag-duplicate-payments".
	ID string `json:"id"`
	// Sentence is a one-line human statement of what the surface is meant to do.
	Sentence string `json:"sentence"`
}

// Check is one re-runnable observation of the surface: given some input, run
// this argv, and the surface's behaviour is expected to match these
// expectations. A Check states an expectation; it does not evaluate one.
type Check struct {
	// Name is a kebab-case identifier, unique within the Spec.
	Name string `json:"name"`
	// Given is the input this check hands to the surface.
	Given Given `json:"given"`
	// Run is the argv, relative to the bundle root, that exercises the surface.
	// The token "{given}" expands to Given.File, after which Given.Args are
	// appended. Expansion is the core's concern; this package validates the
	// argv's shape only.
	Run []string `json:"run"`
	// Expect states what the run should produce.
	Expect Expect `json:"expect"`
}

// Given is the input a check hands to the surface: an optional bundle-relative
// file, and optional extra arguments.
type Given struct {
	// File, when set, is a bundle-relative path — never absolute, never
	// climbing out of the bundle with a ".." segment.
	File string `json:"file,omitempty"`
	// Args are extra arguments appended after the expanded run argv.
	Args []string `json:"args,omitempty"`
}

// Expect states what a check's run is expected to produce. At least one of its
// fields must be set; an expectation that expects nothing is refused as
// incoherent.
type Expect struct {
	// StdoutExact, when non-nil, is the exact stdout the run should produce. It
	// is a pointer so that the empty string (expect no output at all) is
	// distinguishable from "unset".
	StdoutExact *string `json:"stdout_exact,omitempty"`
	// StdoutMatches, when non-empty, is an RE2 regular expression that stdout
	// should match.
	StdoutMatches string `json:"stdout_matches,omitempty"`
	// ExitCode, when non-nil, is the exit status the run should end with. It is
	// a pointer so that 0 (expect success) is distinguishable from "unset"; a
	// nil ExitCode is read by the core as an expected status of 0.
	ExitCode *int `json:"exit_code,omitempty"`
}
