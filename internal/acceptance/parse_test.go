package acceptance

import (
	"errors"
	"strings"
	"testing"
)

// validSpec is a well-formed statement in the shape the paid-twice example
// takes. Cases below mutate a copy of this idea to isolate one flaw at a time.
const validSpec = `{
  "purpose": {
    "id": "flag-duplicate-payments",
    "sentence": "Flags likely duplicate payments in a CSV the person provides."
  },
  "checks": [
    {
      "name": "flags-a-double",
      "given": { "file": "surface/sample.csv" },
      "run": ["surface/run.sh", "{given}"],
      "expect": { "stdout_exact": "hit\n", "exit_code": 0 }
    },
    {
      "name": "stays-silent",
      "given": { "file": "surface/no-doubles.csv" },
      "run": ["surface/run.sh", "{given}"],
      "expect": { "stdout_exact": "" }
    }
  ]
}`

func TestParse_ValidSpecIsAccepted(t *testing.T) {
	spec, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("valid statement refused: %v", err)
	}
	if spec.Purpose.ID != "flag-duplicate-payments" {
		t.Errorf("purpose id = %q, want flag-duplicate-payments", spec.Purpose.ID)
	}
	if len(spec.Checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(spec.Checks))
	}
	// The empty-string expectation must survive as a set-but-empty value, not
	// be collapsed into "unset".
	silent := spec.Checks[1].Expect
	if silent.StdoutExact == nil {
		t.Error("stdout_exact of the silent check is nil; \"\" must remain meaningful")
	} else if *silent.StdoutExact != "" {
		t.Errorf("stdout_exact = %q, want empty", *silent.StdoutExact)
	}
	// exit_code 0 given explicitly must survive as a set pointer, not unset.
	if spec.Checks[0].Expect.ExitCode == nil || *spec.Checks[0].Expect.ExitCode != 0 {
		t.Error("explicit exit_code 0 did not survive as a set pointer")
	}
}

func TestParse_MissingExitCodeIsUnset(t *testing.T) {
	spec, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("valid statement refused: %v", err)
	}
	// The silent check omits exit_code; it must read as unset (nil), which the
	// core treats as an expected status of 0.
	if spec.Checks[1].Expect.ExitCode != nil {
		t.Error("omitted exit_code should be nil (unset)")
	}
}

func TestParse_Refusals(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{
			name: "not well-formed json",
			in:   `{"purpose": {`,
			want: ErrMalformed,
		},
		{
			name: "trailing content after the statement",
			in:   validSpec + `{"extra":true}`,
			want: ErrMalformed,
		},
		{
			name: "wrong json type for checks",
			in:   `{"purpose":{"id":"x","sentence":"s"},"checks":"nope"}`,
			want: ErrMalformed,
		},
		{
			name: "unknown top-level field",
			in:   `{"purpose":{"id":"x","sentence":"s"},"checks":[],"surprise":1}`,
			want: ErrUnknownField,
		},
		{
			name: "unknown field inside a check",
			in:   `{"purpose":{"id":"x","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{"exit_code":0},"oops":1}]}`,
			want: ErrUnknownField,
		},
		{
			name: "purpose id not kebab",
			in:   `{"purpose":{"id":"Flag_Duplicates","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{"exit_code":0}}]}`,
			want: ErrBadID,
		},
		{
			name: "check name not kebab",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"Not Kebab","given":{},"run":["r"],"expect":{"exit_code":0}}]}`,
			want: ErrBadID,
		},
		{
			name: "no checks",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[]}`,
			want: ErrNoChecks,
		},
		{
			name: "duplicate check names",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"dup","given":{},"run":["r"],"expect":{"exit_code":0}},{"name":"dup","given":{},"run":["r"],"expect":{"exit_code":0}}]}`,
			want: ErrDuplicateName,
		},
		{
			name: "empty run argv",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":[],"expect":{"exit_code":0}}]}`,
			want: ErrNoRun,
		},
		{
			name: "absolute given.file",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{"file":"/etc/passwd"},"run":["r"],"expect":{"exit_code":0}}]}`,
			want: ErrBadPath,
		},
		{
			name: "dotdot given.file",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{"file":"../secrets.csv"},"run":["r"],"expect":{"exit_code":0}}]}`,
			want: ErrBadPath,
		},
		{
			name: "absolute run token",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["/bin/sh","-c","x"],"expect":{"exit_code":0}}]}`,
			want: ErrBadPath,
		},
		{
			name: "dotdot run token",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["surface/../../escape.sh"],"expect":{"exit_code":0}}]}`,
			want: ErrBadPath,
		},
		{
			name: "check expects nothing",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{}}]}`,
			want: ErrNoExpectation,
		},
		{
			name: "stdout_matches is not a valid regexp",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{"stdout_matches":"("}}]}`,
			want: ErrBadExpectation,
		},
		{
			name: "exit_code out of range",
			in:   `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{"exit_code":999}}]}`,
			want: ErrBadExpectation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Parse refused with %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParse_OverSizeBoundIsRefused(t *testing.T) {
	// A sentence padded past the byte bound must be refused unread as too large,
	// not parsed.
	big := `{"purpose":{"id":"ok","sentence":"` + strings.Repeat("a", maxSpecBytes) + `"},"checks":[]}`
	_, err := Parse([]byte(big))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize statement refused with %v, want ErrTooLarge", err)
	}
}

func TestParse_FlagAndPlaceholderTokensAreNotPaths(t *testing.T) {
	// A run argv that mixes a bundle-relative program, a flag and the {given}
	// placeholder must be accepted: none of these escape the bundle.
	in := `{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{"file":"surface/in.csv","args":["--strict"]},"run":["surface/run.sh","-n","{given}"],"expect":{"stdout_matches":"^ok$"}}]}`
	if _, err := Parse([]byte(in)); err != nil {
		t.Fatalf("flag/placeholder argv refused: %v", err)
	}
}

// stdouts distinguishes a set-empty stdout_exact from an unset one at the
// StdoutExact pointer, guarding the "" semantics the schema depends on.
func TestStdoutExact_EmptyVsUnset(t *testing.T) {
	set, err := Parse([]byte(`{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{"stdout_exact":""}}]}`))
	if err != nil {
		t.Fatalf("set-empty stdout_exact refused: %v", err)
	}
	if set.Checks[0].Expect.StdoutExact == nil {
		t.Fatal("set-empty stdout_exact came back nil")
	}
	unsetExpect, err := Parse([]byte(`{"purpose":{"id":"ok","sentence":"s"},"checks":[{"name":"c","given":{},"run":["r"],"expect":{"exit_code":0}}]}`))
	if err != nil {
		t.Fatalf("unset stdout_exact refused: %v", err)
	}
	if unsetExpect.Checks[0].Expect.StdoutExact != nil {
		t.Error("unset stdout_exact should be nil")
	}
}
