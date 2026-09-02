// Package fill is the brief-fill seam: a ghillie fills in only the bits he
// does not know are his to say.
//
// A ghillie often knows his mistress's or master's desires — that is why he is
// trusted — and a ghillie who asks his owner things he should know is a bad
// ghillie. But the knowing is safe only because it is BOUNDED and CHECKABLE:
// whether a known answer may go to the factory unasked is decided per item by
// the proven Brief_Fill_Policy_Pkg (ledger 208) through its front named by
// BRIEF_FILL_DECIDER, against a standing rule the owner granted as an owner
// act. The ladder's law holds throughout: no rule, no disclosure — the absence
// of a rule is a recorded refusal, never a guess.
//
// This package holds NO decision. It establishes the two facts the proven core
// needs — is a candidate answer held, and what the owner's rules say about
// this item class — hands them to the front, and obeys the verdict:
//
//	fill_silently  → answered from the standing rule, recorded, not asked
//	ask_with_offer → asked, with the known answer offered out loud
//	ask_plain      → asked exactly as it would have been
//	refuse_item    → the fill machinery stands down; asked unaided, gap recorded
//
// A decider that cannot be asked (unset, missing, non-zero exit) is the same
// as refuse for safety and LOUDER in the log: every affected item is asked
// unaided. Nothing here ever invents an answer, and nothing here judges one.
package fill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/protocol"
)

// DeciderEnv names the proven fill-policy front (Brief_Fill_Policy_Pkg,
// ledger 208). Unset means the fill machinery is unwired: everything is asked.
const DeciderEnv = "BRIEF_FILL_DECIDER"

// StateFilled is the State a filled answer reports. It is deliberately NOT a
// ledger-123 conduct state: the item was never conducted — it was answered
// from the owner's standing rule, and the factory can see exactly that.
const StateFilled = "Filled"

// Rule is one standing licence the owner granted: for items of this class,
// this is the answer, and Licensed says whether it may still be sent unasked.
// A revoked rule keeps its answer — he still knows, it is just no longer his
// to say, which is exactly the Ask_With_Offer posture.
type Rule struct {
	ItemClass string `json:"item_class"`
	Answer    string `json:"answer"`
	Licensed  bool   `json:"licensed"`
	GrantedAt string `json:"granted_at"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

// Store is the owner's standing fill rules, kept beside the state file. Every
// mutation is an OWNER ACT through the command line — nothing on the wire (a
// brief, a page, the mouth) can reach these.
type Store struct {
	Path  string
	Rules []Rule
}

type storeFile struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

// Load reads the store. A missing file is an EMPTY store, not an error — v1
// ships with zero rules and grows them from real interviews. A present but
// unreadable file is an error the caller must carry: that is the
// no_rule_record case, and it must refuse fills, never default to empty.
func Load(path string) (*Store, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fill rules %s: %w", path, err)
	}
	var f storeFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("fill rules %s: %w — refusing to guess: an unreadable rule store refuses fills (no_rule_record), it does not become an empty one", path, err)
	}
	return &Store{Path: path, Rules: f.Rules}, nil
}

// Save writes the store, 0600 — it holds the owner's standing answers.
func (s *Store) Save() error {
	body, err := json.MarshalIndent(storeFile{Version: 1, Rules: s.Rules}, "", "  ")
	if err != nil {
		return fmt.Errorf("fill rules: %w", err)
	}
	if err := os.WriteFile(s.Path, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("fill rules %s: %w", s.Path, err)
	}
	return nil
}

// Allow grants or refreshes one standing rule — the OWNER's act.
func (s *Store) Allow(itemClass, answer string, now time.Time) error {
	itemClass = strings.TrimSpace(itemClass)
	if itemClass == "" {
		return errors.New("fill rules: an empty item class licenses nothing")
	}
	if strings.TrimSpace(answer) == "" {
		return errors.New("fill rules: a rule carries the standing answer; an empty one would be an invented fill waiting to happen")
	}
	at := now.UTC().Format(time.RFC3339)
	for i := range s.Rules {
		if s.Rules[i].ItemClass == itemClass {
			s.Rules[i].Answer = answer
			s.Rules[i].Licensed = true
			s.Rules[i].GrantedAt = at
			s.Rules[i].RevokedAt = ""
			return nil
		}
	}
	s.Rules = append(s.Rules, Rule{ItemClass: itemClass, Answer: answer, Licensed: true, GrantedAt: at})
	return nil
}

// Revoke withdraws the licence but KEEPS the answer: the ghillie still knows,
// it is no longer his to say — later fills become offers.
func (s *Store) Revoke(itemClass string, now time.Time) error {
	for i := range s.Rules {
		if s.Rules[i].ItemClass == itemClass {
			s.Rules[i].Licensed = false
			s.Rules[i].RevokedAt = now.UTC().Format(time.RFC3339)
			return nil
		}
	}
	return fmt.Errorf("fill rules: no rule for %q to revoke", itemClass)
}

// Lookup establishes the two facts the proven core takes, for one item class.
// An empty class matches nothing by construction — untagged items are asked.
func (s *Store) Lookup(itemClass string) (answer string, known bool, ruleClass string) {
	if itemClass == "" {
		return "", false, "reaches_not"
	}
	for _, r := range s.Rules {
		if r.ItemClass == itemClass {
			if r.Licensed {
				return r.Answer, true, "licensed"
			}
			return r.Answer, true, "reaches_not"
		}
	}
	return "", false, "reaches_not"
}

// Logger is the narration sink, same shape as the interview's.
type Logger interface {
	Printf(format string, v ...any)
}

// Interviewer is what fill wraps — satisfied by *interview.Session and by
// terminal.Interviewer alike.
type Interviewer interface {
	Conduct(ctx context.Context, b *brief.Brief) ([]protocol.Answer, error)
}

// Filler decorates an Interviewer: each brief item goes to the proven decider
// first, filled items are answered from the store, and only the remainder is
// conducted. It changes nothing about HOW the remainder is put — conduct is
// the wrapped interviewer's, untouched.
type Filler struct {
	inner    Interviewer
	store    *Store
	storeErr error // non-nil ⇒ no_rule_record for every item
	decider  string
	log      Logger
	now      func() time.Time
}

// Wrap builds the filler. storeErr carries a rule store that exists but could
// not be read — the no_rule_record posture, which refuses fills item by item
// rather than failing the interview.
func Wrap(inner Interviewer, store *Store, storeErr error, decider string, log Logger) *Filler {
	return &Filler{inner: inner, store: store, storeErr: storeErr, decider: decider, log: log, now: time.Now}
}

// verdict asks the proven front. Exit contract: 0 with one word on stdout is
// an answer; anything else means the decider could not be asked.
func (f *Filler) verdict(known bool, ruleClass string) (string, error) {
	out, err := exec.Command(f.decider, "decide", strconv.FormatBool(known), ruleClass).Output()
	if err != nil {
		return "", fmt.Errorf("fill decider would not answer (known=%v rule=%s): %w", known, ruleClass, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Conduct partitions the brief, conducts the remainder, and reports the merged
// answers ordered by item id.
func (f *Filler) Conduct(ctx context.Context, b *brief.Brief) (answers []protocol.Answer, err error) {
	filled := []protocol.Answer{}
	residual := &brief.Brief{BriefID: b.BriefID, Version: b.Version}

	for _, item := range b.Items {
		var answer string
		var known bool
		ruleClass := "no_rule_record"
		if f.storeErr == nil {
			answer, known, ruleClass = f.store.Lookup(item.ItemClass)
		}

		v, verr := f.verdict(known, ruleClass)
		if verr != nil {
			// Decider unavailable: the conservative door. Asked unaided, said
			// out loud in the log — a gap, not a crash.
			f.log.Printf("item %d: %v — asking unaided (gap)", item.ID, verr)
			residual.Items = append(residual.Items, item)
			continue
		}

		switch v {
		case "fill_silently":
			f.log.Printf("item %d (%s): filled from the owner's standing rule — recorded, not asked", item.ID, item.ItemClass)
			filled = append(filled, protocol.Answer{
				ItemID:   item.ID,
				State:    StateFilled,
				Answered: true,
				Text:     answer,
				Attempts: 0,
				At:       f.now().UTC().Format(time.RFC3339),
			})
		case "ask_with_offer":
			// He knows, and he still defers where it is not his to say. The
			// offer is put OUT LOUD as part of the question; the person's
			// reply is recorded verbatim as ever — ghillie never reads a
			// "yes" as the offered text, because interpreting a reply would
			// be judging it, and the factory judges.
			item.Want = fmt.Sprintf("%s — I'd have said: %s. Shall I put that, or would you answer differently?", item.Want, answer)
			residual.Items = append(residual.Items, item)
		case "refuse_item":
			f.log.Printf("item %d (%s): fill machinery stood down (%s) — asking unaided (gap)", item.ID, item.ItemClass, ruleClass)
			residual.Items = append(residual.Items, item)
		case "ask_plain":
			residual.Items = append(residual.Items, item)
		default:
			// A word this glue does not know is a decider this glue cannot
			// trust — same conservative door as unavailable.
			f.log.Printf("item %d: fill decider said %q, which this glue does not know — asking unaided (gap)", item.ID, v)
			residual.Items = append(residual.Items, item)
		}
	}

	if len(residual.Items) == 0 {
		// Everything was filled: nothing to conduct, nobody to disturb. The
		// log carries what happened; the answers carry StateFilled.
		f.log.Printf("brief %s: every item filled from standing rules — nothing to ask", b.BriefID)
		return filled, nil
	}

	conducted, err := f.inner.Conduct(ctx, residual)
	if err != nil {
		return nil, err
	}
	answers = append(filled, conducted...)
	sort.Slice(answers, func(i, j int) bool { return answers[i].ItemID < answers[j].ItemID })
	return answers, nil
}
