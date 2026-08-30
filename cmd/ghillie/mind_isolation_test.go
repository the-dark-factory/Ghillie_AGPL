package main

// mind_isolation_test.go is the WALL, expressed as a test.
//
// A model is not a decision procedure. It is not reproducible, it cannot be
// proved, and it will happily produce a confident verdict on anything it is
// shown. This product's claim is that its judgements are settled by proven
// cores — so the mind must be reachable from the conversational surface and
// from nowhere else. That is a structural property, and a structural property
// that is not tested is an intention.

import (
	"go/build"
	"path"
	"strings"
	"testing"
)

// modulePath is this module, so the walk can tell our own packages (which it
// must follow) from the standard library and vendored code (which it need not).
const modulePath = "github.com/tonygair/ghillie"

// mindPackage is the package that must not appear in a decision path.
const mindPackage = modulePath + "/internal/mind"

// TestMindIsNotImportedByTheDecisionPaths asserts that no package carrying a
// judgement can reach internal/mind, transitively.
//
// TRANSITIVELY IS THE WORD THAT MATTERS. A direct import would be caught by
// reading the file; what this catches is the import three hops down that
// arrives with a refactor nobody thought was about inference.
func TestMindIsNotImportedByTheDecisionPaths(t *testing.T) {
	tests := []struct {
		name string
		pkg  string
		why  string
	}{
		{
			name: "the gate",
			pkg:  modulePath + "/internal/gate",
			why:  "permission verdicts mirror the proven cores; a model must never be asked whether an act is allowed",
		},
		{
			name: "the conduct wall",
			pkg:  modulePath + "/internal/conduct",
			why:  "turn-taking, the question ledger and the attempt bound are proven cores — inference does not get a vote in them",
		},
		{
			name: "the brief",
			pkg:  modulePath + "/internal/brief",
			why:  "what is asked, and what state an item is in, is the factory's and the ledger's — not a model's paraphrase",
		},
		{
			name: "the interview",
			pkg:  modulePath + "/internal/interview",
			why:  "ghillie PRESENTS and ASKS; the factory judges. A mind here would start interpreting answers",
		},
		{
			name: "the terminal",
			pkg:  modulePath + "/internal/terminal",
			why:  "the poll loop, the signature check and the act-or-refuse are plumbing over proven cores; a refusal must be derivable, not generated",
		},
		{
			name: "the CV gate",
			pkg:  modulePath + "/internal/cvgate",
			why:  "disclosure tiers are the owner's rules, evaluated the same way every time",
		},
		{
			name: "the bundle installer",
			pkg:  modulePath + "/internal/bundle",
			why:  "admission — the five-part contract and the proof check — is decided by verification, never by being talked round",
		},
		{
			name: "the protocol",
			pkg:  modulePath + "/internal/protocol",
			why:  "what goes on the wire, and what a signature covers, is fixed by the format",
		},
		{
			name: "the frame decoder",
			pkg:  modulePath + "/internal/frame",
			why:  "an instruction means what it decodes to; nothing about it is open to interpretation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reached, route := reaches(t, tc.pkg, mindPackage)
			if reached {
				t.Errorf("%s can reach %s via %s.\n%s",
					tc.pkg, mindPackage, strings.Join(route, " → "), tc.why)
			}
		})
	}
}

// TestTheTalkSurfaceDoesReachTheMind is the other half, and it is not
// ceremony: a wall test that passes because the mind is imported by nothing at
// all would prove only that the feature was deleted.
func TestTheTalkSurfaceDoesReachTheMind(t *testing.T) {
	reached, _ := reaches(t, modulePath+"/cmd/ghillie", mindPackage)
	if !reached {
		t.Fatalf("%s does not import %s — the mind socket is not wired to anything", modulePath+"/cmd/ghillie", mindPackage)
	}
}

// reaches reports whether from can import target transitively, and the route
// by which it does. Only packages inside this module are followed: the
// standard library cannot import ours, so walking it would cost time and prove
// nothing.
func reaches(t *testing.T, from, target string) (found bool, route []string) {
	t.Helper()
	seen := map[string]bool{}

	var walk func(pkg string, trail []string) []string
	walk = func(pkg string, trail []string) []string {
		if seen[pkg] {
			return nil
		}
		seen[pkg] = true
		trail = append(append([]string(nil), trail...), short(pkg))

		p, err := build.Import(pkg, "", 0)
		if err != nil {
			t.Fatalf("resolve %s: %v", pkg, err)
		}
		// Imports, not TestImports: a test file may legitimately reach for a
		// fake, and it is the SHIPPED graph that carries the promise.
		for _, imp := range p.Imports {
			if imp == target {
				return append(trail, short(target))
			}
			if !strings.HasPrefix(imp, modulePath+"/") {
				continue
			}
			if hit := walk(imp, trail); hit != nil {
				return hit
			}
		}
		return nil
	}

	if hit := walk(from, nil); hit != nil {
		return true, hit
	}
	return false, nil
}

// short trims the module prefix so a failure reads as a route, not a wall of
// repeated import paths.
func short(pkg string) string {
	if pkg == modulePath {
		return path.Base(modulePath)
	}
	return strings.TrimPrefix(pkg, modulePath+"/")
}
