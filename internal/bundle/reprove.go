// The re-prove seam: how an installed extension's PROOF is checked at this
// machine, per the source-bundle format (proposal_source_bundle_format
// 2026-08-26, approved).
//
// A bundle that carries core/src/proof.gpr claims to be RE-PROVABLE: the
// recipient can re-derive its proof from the shipped source with their own
// prover, build the front with their own toolchain, and check the built
// binary against the shipped truth table. Two honest rungs:
//
//	RUNG A — carried: digest and manifest verified, proofs read but not
//	         re-run. Needs no toolchain. The ledger says so in plain words.
//	RUNG B — re-proved here: gnatprove runs from clean on the shipped
//	         source; zero unproved or NO INSTALL. Then the front is built
//	         and every truth-table row checked. The refusal is mechanical.
//
// The rung is the owner's choice (-reprove), recorded, and never silently
// downgraded.
package bundle

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Proof-rung notes as they appear in the ledger. Plain words, not codes —
// the ledger is read by people.
const (
	ProofCarried  = "proofs carried, not re-derived here"
	ProofReproved = "re-proved on this machine"
)

// ClaimsProof reports whether the bundle ships re-provable source: the
// claim IS the presence of the exact project file the proof runs under.
func ClaimsProof(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "core", "src", "proof.gpr"))
	return err == nil && !info.IsDir()
}

// VerifyManifest checks every file listed in MANIFEST.sha256 against its
// recorded digest. A bundle without a manifest passes with checked == 0 —
// the archive digest still covers it whole; the manifest adds per-file
// accountability when present. A listed file that is missing or altered is
// a refusal, not a warning.
func VerifyManifest(dir string) (checked int, err error) {
	raw, readErr := os.ReadFile(filepath.Join(dir, "MANIFEST.sha256"))
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return 0, nil
		}
		return 0, fmt.Errorf("bundle: manifest unreadable: %w", readErr)
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return checked, fmt.Errorf("bundle: manifest line %q is not '<sha256>  <path>'", line)
		}
		want, rel := fields[0], filepath.Clean(fields[1])
		if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return checked, fmt.Errorf("bundle: manifest names a path outside the bundle: %q", rel)
		}
		body, fileErr := os.ReadFile(filepath.Join(dir, rel))
		if fileErr != nil {
			return checked, fmt.Errorf("bundle: manifest names %s but it cannot be read: %w", rel, fileErr)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != strings.ToLower(want) {
			return checked, fmt.Errorf("bundle: %s does not match its manifest digest — the delivery is altered; re-fetch it, never repair it", rel)
		}
		checked++
	}
	return checked, nil
}

// FindProver locates gnatprove: the owner's explicit choice first
// (GHILLIE_PROVER), then the PATH. No prover is not an error of the
// bundle's — the caller decides between Rung A and refusing.
func FindProver() (string, error) {
	if p := os.Getenv("GHILLIE_PROVER"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("GHILLIE_PROVER points at %s but nothing is there", p)
		}
		return p, nil
	}
	if p, err := exec.LookPath("gnatprove"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no prover here: set GHILLIE_PROVER or put gnatprove on PATH (the FSF gnatprove package alone is enough — it also carries the build tools)")
}

// Reprove runs the prover from clean over the shipped proof project.
// Zero unproved or an error — there is no partial credit.
func Reprove(dir, prover string) error {
	src := filepath.Join(dir, "core", "src")
	_ = os.RemoveAll(filepath.Join(src, "obj")) // from clean, always
	cmd := exec.Command(prover, "-P", "proof.gpr", "-f", "-U", "--level=2")
	cmd.Dir = src
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("bundle: the prover would not run: %w\n%s", runErr, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "medium:") || strings.Contains(l, "high:") ||
			strings.Contains(l, "error:") {
			return fmt.Errorf("bundle: the proof did NOT discharge here — refusing the install; the prover said:\n  %s", l)
		}
	}
	return nil
}

// BuildAndTable builds the front with the recipient's own toolchain and
// checks every row of the shipped truth table against the built binary.
// Returns the path of the built front. Any mismatch is a refusal.
//
// TABLE.tsv rows are three tab-separated columns:
//
//	<argv, space-separated>	<expected stdout, trimmed>	<expected exit>
//
// Comment lines (#) and blanks are skipped.
func BuildAndTable(dir, prover string) (frontPath string, err error) {
	front := filepath.Join(dir, "front")
	gprbuild, err := findGprbuild(prover)
	if err != nil {
		return "", err
	}
	build := exec.Command(gprbuild, "-q", "-P", "edge.gpr")
	build.Dir = front
	build.Env = append(os.Environ(), "PATH="+filepath.Dir(gprbuild)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("bundle: the front would not build here: %w\n%s", buildErr, out)
	}
	frontPath, err = builtFront(front)
	if err != nil {
		return "", err
	}

	raw, readErr := os.ReadFile(filepath.Join(front, "TABLE.tsv"))
	if readErr != nil {
		return "", fmt.Errorf("bundle: TABLE.tsv unreadable: %w", readErr)
	}
	rows := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			return "", fmt.Errorf("bundle: TABLE.tsv row %q is not argv<TAB>stdout<TAB>exit", line)
		}
		argv := strings.Fields(cols[0])
		wantOut, wantExit := strings.TrimSpace(cols[1]), strings.TrimSpace(cols[2])
		run := exec.Command(frontPath, argv...)
		out, _ := run.Output() // exit code read below; stderr is the front's own report channel
		gotExit := run.ProcessState.ExitCode()
		if strings.TrimSpace(string(out)) != wantOut || fmt.Sprint(gotExit) != wantExit {
			return "", fmt.Errorf("bundle: the built front DISAGREES with its shipped table — refusing the install: %q gave %q (exit %d), table says %q (exit %s)",
				cols[0], strings.TrimSpace(string(out)), gotExit, wantOut, wantExit)
		}
		rows++
	}
	if rows == 0 {
		return "", fmt.Errorf("bundle: TABLE.tsv holds no rows — an unchecked front is not a checked front")
	}
	return frontPath, nil
}

// findGprbuild looks beside the prover first — the FSF gnatprove package
// carries its own build tools under libexec/spark/bin — then the PATH.
func findGprbuild(prover string) (string, error) {
	beside := filepath.Join(filepath.Dir(prover), "..", "libexec", "spark", "bin", "gprbuild")
	if _, err := os.Stat(beside); err == nil {
		return beside, nil
	}
	if p, err := exec.LookPath("gprbuild"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no gprbuild here (looked beside the prover and on PATH)")
}

// builtFront finds the one executable the build produced in front/.
func builtFront(front string) (string, error) {
	entries, err := os.ReadDir(front)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_front") {
			continue
		}
		info, infoErr := e.Info()
		if infoErr == nil && info.Mode()&0o111 != 0 {
			return filepath.Join(front, e.Name()), nil
		}
	}
	return "", fmt.Errorf("bundle: the build reported success but no *_front executable is in front/")
}
