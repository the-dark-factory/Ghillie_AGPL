package acceptance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxSpecBytes bounds an acceptance statement. A statement is a short
// description of what "done" looks like, not a payload; anything larger is
// refused unread rather than parsed.
const maxSpecBytes = 64 << 10

// The sentinel errors this package refuses with. Each names one way an
// acceptance statement can be malformed or self-inconsistent. Callers match
// them with errors.Is; every return wraps one with %w and adds which check or
// field was at fault.
var (
	// ErrTooLarge means the statement is over the size bound.
	ErrTooLarge = errors.New("acceptance: statement is larger than an acceptance statement should be")
	// ErrMalformed means the bytes are not well-formed JSON, or carry trailing
	// content, or a field holds the wrong JSON type.
	ErrMalformed = errors.New("acceptance: statement is not well-formed")
	// ErrUnknownField means the statement carries a field this schema does not define.
	ErrUnknownField = errors.New("acceptance: statement carries a field the schema does not define")
	// ErrBadID means an identifier — a purpose id or a check name — is not kebab-shaped.
	ErrBadID = errors.New("acceptance: identifier is not kebab-shaped")
	// ErrNoChecks means the statement declares no checks; there is nothing to describe.
	ErrNoChecks = errors.New("acceptance: statement declares no checks")
	// ErrDuplicateName means two checks share a name.
	ErrDuplicateName = errors.New("acceptance: two checks share a name")
	// ErrNoRun means a check has an empty run argv; there is nothing to exercise.
	ErrNoRun = errors.New("acceptance: check has an empty run argv")
	// ErrBadPath means a given.file or a run token is absolute or climbs out of
	// the bundle with a ".." segment.
	ErrBadPath = errors.New("acceptance: path is absolute or climbs out of the bundle")
	// ErrNoExpectation means a check expects nothing — none of stdout_exact,
	// stdout_matches or exit_code is set.
	ErrNoExpectation = errors.New("acceptance: check expects nothing")
	// ErrBadExpectation means a check's expectation is incoherent — an
	// uncompilable stdout_matches, or an exit_code outside 0..255.
	ErrBadExpectation = errors.New("acceptance: check expectation is incoherent")
)

// kebab is the shape every identifier must take: lower-case alphanumerics in
// hyphen-separated groups, with no leading, trailing or doubled hyphen.
var kebab = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Load reads the acceptance statement at path, then validates it exactly as
// Parse does. A statement over the size bound is refused by its file size
// before it is read into memory. It establishes nothing about the extension's
// behaviour; it only confirms the file is a well-formed statement.
func Load(path string) (*Spec, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	if info.Size() > maxSpecBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes, over the %d-byte bound", ErrTooLarge, path, info.Size(), maxSpecBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("acceptance: %w", err)
	}
	spec, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("acceptance: %s: %w", path, err)
	}
	return spec, nil
}

// Parse validates raw acceptance-statement bytes and returns the Spec they
// describe. It decodes strictly — unknown fields, trailing content and wrong
// JSON types are all refused — and then checks the statement is
// self-consistent. It runs nothing and reaches no conclusion about the
// extension; a nil error means only that the statement is well-formed input.
func Parse(data []byte) (*Spec, error) {
	if len(data) > maxSpecBytes {
		return nil, fmt.Errorf("%w: %d bytes, over the %d-byte bound", ErrTooLarge, len(data), maxSpecBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, classify(err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: trailing content after the statement", ErrMalformed)
	}
	if err := validate(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// classify maps a json decode error onto the right sentinel: a strict-decoder
// unknown-field complaint becomes ErrUnknownField; a syntax or type mismatch
// becomes ErrMalformed.
func classify(err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if strings.Contains(err.Error(), "unknown field") {
		return fmt.Errorf("%w: %v", ErrUnknownField, err)
	}
	return fmt.Errorf("%w: %v", ErrMalformed, err)
}

// validate confirms a decoded Spec is self-consistent. It is the whole of the
// structural check: identifiers are kebab-shaped, there is at least one check,
// names are unique, every check has a run argv and at least one expectation,
// and no path escapes the bundle.
func validate(s *Spec) error {
	if !kebab.MatchString(s.Purpose.ID) {
		return fmt.Errorf("%w: purpose id %q", ErrBadID, s.Purpose.ID)
	}
	if len(s.Checks) == 0 {
		return ErrNoChecks
	}
	seen := make(map[string]bool, len(s.Checks))
	for i := range s.Checks {
		c := &s.Checks[i]
		if !kebab.MatchString(c.Name) {
			return fmt.Errorf("%w: check name %q", ErrBadID, c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("%w: %q", ErrDuplicateName, c.Name)
		}
		seen[c.Name] = true
		if len(c.Run) == 0 {
			return fmt.Errorf("%w: check %q", ErrNoRun, c.Name)
		}
		if c.Given.File != "" && escapes(c.Given.File) {
			return fmt.Errorf("%w: check %q given.file %q", ErrBadPath, c.Name, c.Given.File)
		}
		for _, tok := range c.Run {
			if escapes(tok) {
				return fmt.Errorf("%w: check %q run token %q", ErrBadPath, c.Name, tok)
			}
		}
		if err := validateExpect(c); err != nil {
			return err
		}
	}
	return nil
}

// escapes reports whether a bundle-relative token would reach outside the
// bundle: an absolute path, or one with a ".." segment. Plain flags and the
// "{given}" placeholder are not paths and never escape.
func escapes(tok string) bool {
	if strings.HasPrefix(tok, "/") || filepath.IsAbs(tok) {
		return true
	}
	for _, seg := range strings.Split(tok, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// validateExpect confirms a check's expectation is coherent: at least one arm
// is set, any stdout_matches compiles as RE2, and any exit_code sits in 0..255.
func validateExpect(c *Check) error {
	e := &c.Expect
	if e.StdoutExact == nil && e.StdoutMatches == "" && e.ExitCode == nil {
		return fmt.Errorf("%w: check %q", ErrNoExpectation, c.Name)
	}
	if e.StdoutMatches != "" {
		if _, err := regexp.Compile(e.StdoutMatches); err != nil {
			return fmt.Errorf("%w: check %q stdout_matches %q: %v", ErrBadExpectation, c.Name, e.StdoutMatches, err)
		}
	}
	if e.ExitCode != nil && (*e.ExitCode < 0 || *e.ExitCode > 255) {
		return fmt.Errorf("%w: check %q exit_code %d is outside 0..255", ErrBadExpectation, c.Name, *e.ExitCode)
	}
	return nil
}
