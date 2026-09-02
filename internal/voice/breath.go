package voice

// breath.go prepares the ONE breath this seam adds outside an utterance: the
// audible offered-floor in-breath that follows a question. It is a hand-over
// signal, not speech, so it appears in no plan sidecar and claims no engine
// license — internal/interview/voice.go documents where it may be used and why
// nowhere else.
//
// The sample discipline is the approved run's, unchanged: one speaker for the
// whole conversation (speaker 1089, gender-matched — nine matched breaths from
// nine different men is the "second voice" bug wearing a disguise), the medium
// class, the approved quiet-variant level (−4.9 dB). textplan.py applies that
// level while SPLICING breaths into speech and exposes no standalone
// level-matched breath output, so the gain law is restated here from its
// splice line (demo/textplan.py, "mean-preserving local level match"):
//
//	breath = breath/peak(breath) × rms(speech) × 0.35 × 10^(−4.9/20)
//
// with the local-gain factor 1 — there is one breath and no neighbourhood, so
// the geometric-mean normalisation is exactly 1 by construction, and the RMS
// is the whole rendered question rather than a 1200 ms window. Both deviations
// are disclosed here rather than hidden.

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Bank defaults, matching the approved run.
//
// DefaultBankDir resolves at start, under the ghillie home: $GHILLIE_HOME when
// the machine names one, otherwise ~/.ghillie. Its absence is fine — the voice
// pipeline states it honestly rather than guessing at somebody else's layout.
var DefaultBankDir = ghillieHomeSub("breathbank")

const (

	// DefaultSpeaker is the approved run's single breath speaker.
	DefaultSpeaker = "1089"

	// DefaultClass is the breath class used for the offered floor: a medium
	// inhalation — audible intent without the drama of a long one.
	DefaultClass = "medium"

	// DefaultQuietDB is the approved quiet-variant breath level.
	DefaultQuietDB = -4.9
)

// BreathBank picks and level-matches breath samples from the recorded bank.
//
// The zero value is not usable; NewBreathBank supplies the approved defaults.
type BreathBank struct {
	// Dir is the bank directory, holding manifest.tsv and the samples.
	Dir string

	// Speaker is the single speaker whose breaths may be used.
	Speaker string

	// Class is the breath class to draw from.
	Class string

	// QuietDB is the level, in dB, applied on top of the speech-matched gain.
	QuietDB float64
}

// NewBreathBank returns a bank with the approved run's discipline compiled in.
func NewBreathBank() *BreathBank {
	return &BreathBank{
		Dir:     DefaultBankDir,
		Speaker: DefaultSpeaker,
		Class:   DefaultClass,
		QuietDB: DefaultQuietDB,
	}
}

// validate reports whether the bank has what it needs.
func (b *BreathBank) validate() error {
	if b.Dir == "" || b.Speaker == "" || b.Class == "" {
		return fmt.Errorf("%w: breath bank needs a directory, a speaker and a class", ErrNotConfigured)
	}
	return nil
}

// Pick chooses one bank sample for the given utterance text: deterministic —
// the same question always draws the same breath, reproducible byte for byte —
// by hashing the text over the sorted eligible samples. Only the configured
// speaker and class are eligible; an empty pool is an explicit error, never a
// silent substitution from another speaker.
func (b *BreathBank) Pick(text string) (path string, err error) {
	if err := b.validate(); err != nil {
		return "", err
	}
	pool, err := b.eligible()
	if err != nil {
		return "", err
	}
	if len(pool) == 0 {
		return "", fmt.Errorf("%w: breath bank %s holds no %s samples for speaker %s — refusing to borrow another speaker's lungs", ErrNotConfigured, b.Dir, b.Class, b.Speaker)
	}
	sum := sha1.Sum([]byte(text))
	idx := int(binary.BigEndian.Uint32(sum[:4]) % uint32(len(pool)))
	return filepath.Join(b.Dir, pool[idx]), nil
}

// eligible reads manifest.tsv and returns the sorted eligible sample names.
func (b *BreathBank) eligible() (names []string, err error) {
	f, err := os.Open(filepath.Join(b.Dir, "manifest.tsv"))
	if err != nil {
		return nil, fmt.Errorf("%w: breath bank manifest: %w", ErrNotConfigured, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close breath bank manifest: %w", cerr)
		}
	}()

	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first { // header: file  duration_s  kind  speaker  gender  …
			first = false
			continue
		}
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 5 {
			continue
		}
		if fields[2] == b.Class && fields[3] == b.Speaker {
			names = append(names, fields[0])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read breath bank manifest: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// OfferedFloor prepares the offered-floor in-breath for a question whose
// rendered speech is at speechWav: the deterministic sample for that text,
// level-matched to the speech by the law restated at the top of this file,
// written beside the speech WAV. It returns the path of the prepared breath.
func (b *BreathBank) OfferedFloor(ctx context.Context, text, speechWav string) (breathWav string, err error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("offered floor: %w", err)
	}
	sample, err := b.Pick(text)
	if err != nil {
		return "", err
	}

	speech, speechRate, err := readWAV(speechWav)
	if err != nil {
		return "", fmt.Errorf("%w: level-matching against the question: %w", ErrRenderFailed, err)
	}
	breath, breathRate, err := readWAV(sample)
	if err != nil {
		return "", fmt.Errorf("%w: breath sample: %w", ErrRenderFailed, err)
	}
	if breathRate != speechRate {
		return "", fmt.Errorf("%w: breath sample %s is %d Hz against %d Hz speech — this seam does not resample and refuses to guess", ErrRenderFailed, sample, breathRate, speechRate)
	}

	peak := peakOf(breath)
	if peak == 0 {
		return "", fmt.Errorf("%w: breath sample %s is silence", ErrRenderFailed, sample)
	}
	gain := rmsOf(speech) * 0.35 * math.Pow(10, b.QuietDB/20) / peak
	scaled := make([]float64, len(breath))
	for i, s := range breath {
		scaled[i] = s * gain
	}

	breathWav = strings.TrimSuffix(speechWav, ".wav") + ".breath.wav"
	if err := writeWAV(breathWav, scaled, speechRate); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}
	return breathWav, nil
}

// ghillieHomeSub joins path elements under the ghillie home: $GHILLIE_HOME when
// the machine names one, otherwise ~/.ghillie. It is the same resolution the
// commands use, kept here so this package needs nothing from them.
func ghillieHomeSub(parts ...string) string {
	home := os.Getenv("GHILLIE_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".ghillie")
	}
	return filepath.Join(append([]string{home}, parts...)...)
}
