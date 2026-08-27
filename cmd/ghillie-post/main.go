// Command ghillie-post runs the read-side Gmail connector: the owner's post,
// presented under the owner's grants instead of a provider's notification
// regime (proposal_connector_gmail_2026-08-25, APPROVED: route A, read-only).
//
// It polls, asks the proven delivery policy about each arrival, and speaks
// the answers: present_now and interrupt to standard output as they happen,
// digest and queue_until appended to their files under the state directory
// for ghillie to fold in at the right moments. Refusals leave a count and no
// content — a refused sender does not get their subject line read aloud into
// a log either.
//
//	ghillie-post -login                 # one-time consent in the owner's browser
//	ghillie-post -once                  # single poll, then exit (cron-friendly)
//	ghillie-post                        # poll loop
//
// The proven decider is named by DELIVERY_POLICY_DECIDER; without it NOTHING
// is presented and each arrival records a gap — fail closed, loudly.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/tonygair/ghillie/internal/gapledger"
	"github.com/tonygair/ghillie/internal/post"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("ghillie-post: %v", err)
	}
}

type options struct {
	credentials string
	stateDir    string
	grants      string
	presence    string
	quietFrom   string
	quietTo     string
	poll        time.Duration
	maxBatch    int
	login       bool
	once        bool
}

func run() (err error) {
	var o options
	home, _ := os.UserHomeDir()
	defaultState := filepath.Join(home, ".ghillie", "post")
	// The estate's own vault location is kept as fallback so an
	// already-configured machine keeps working; the public default is
	// ~/.ghillie.
	if legacy := filepath.Join(home, "ObVault", "ghillie-home", "post"); dirExists(legacy) {
		defaultState = legacy
	}

	flag.StringVar(&o.credentials, "credentials", filepath.Join(defaultState, "client.json"), "the OWNER'S OAuth client file (Google 'installed' JSON) — route A means this is theirs")
	flag.StringVar(&o.stateDir, "state", defaultState, "durable state dir (token, watermark, digest, queue, gap ledger) — never /tmp")
	flag.StringVar(&o.grants, "grants", "", "grants file (default <state>/grants.json); absent file = empty table = refuse everything, which is the no-spam default")
	flag.StringVar(&o.presence, "presence", "free", "the owner's presence: free, occupied, away or asleep — an input to presentation, never an output to anyone")
	flag.StringVar(&o.quietFrom, "quiet-from", "22:00", "quiet hours start (local HH:MM)")
	flag.StringVar(&o.quietTo, "quiet-to", "07:00", "quiet hours end (local HH:MM)")
	flag.DurationVar(&o.poll, "poll", 2*time.Minute, "poll interval")
	flag.IntVar(&o.maxBatch, "max-batch", 25, "most arrivals fetched per poll")
	flag.BoolVar(&o.login, "login", false, "run the one-time consent flow and exit")
	flag.BoolVar(&o.once, "once", false, "poll once and exit")
	flag.Parse()

	if err := os.MkdirAll(o.stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if o.grants == "" {
		o.grants = filepath.Join(o.stateDir, "grants.json")
	}

	creds, err := post.LoadClientCredentials(o.credentials)
	if err != nil {
		return err
	}
	store := post.TokenStore{Path: filepath.Join(o.stateDir, "token.json")}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if o.login {
		return post.Login(ctx, creds, store, func(u string) {
			fmt.Println("Open this in your browser to hand ghillie the read-only post key:")
			fmt.Println("  " + u)
		})
	}

	table, err := post.LoadTable(o.grants)
	if err != nil {
		return err
	}
	log.Printf("post: %d sender grant(s) loaded from %s", len(table), o.grants)
	if os.Getenv(post.EnvDecider) == "" {
		log.Printf("post: ⚠ %s unset — every arrival will be recorded as a gap and NOTHING will be presented", post.EnvDecider)
	}

	client := &post.Client{TokenFn: func(ctx context.Context) (string, error) {
		return post.AccessToken(ctx, creds, store)
	}}
	decider := post.Decider{Gaps: gapledger.Open(filepath.Join(o.stateDir, "gap-ledger.jsonl"))}
	marks := watermark{path: filepath.Join(o.stateDir, "watermark.json")}

	for {
		if err := pollOnce(ctx, o, client, decider, table, marks); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			// One failed poll is not a dead connector: log and let the next
			// tick try again — but never pretend it worked.
			log.Printf("post: poll failed: %v", err)
		}
		if o.once {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(o.poll):
		}
	}
}

// pollOnce fetches arrivals since the watermark and hands each to the proven
// policy, oldest first so presentation order matches arrival order.
func pollOnce(ctx context.Context, o options, client *post.Client, decider post.Decider, table post.Table, marks watermark) error {
	since := marks.load()
	ids, err := client.ListNewIDs(ctx, since, o.maxBatch)
	if err != nil {
		return err
	}
	var arrivals []post.Arrival
	for _, id := range ids {
		a, gerr := client.GetArrival(ctx, id)
		if gerr != nil {
			return gerr
		}
		if !a.At.After(since) {
			// after: is whole-second granular; the watermark is exact.
			continue
		}
		arrivals = append(arrivals, a)
	}
	sort.Slice(arrivals, func(i, j int) bool { return arrivals[i].At.Before(arrivals[j].At) })

	refused := 0
	quiet := inQuietWindow(time.Now(), o.quietFrom, o.quietTo)
	for _, a := range arrivals {
		d, derr := decider.DecideArrival(ctx, table, a.From, o.presence, quiet)
		if derr != nil {
			// Fail closed: nothing presented, and the reason is in the log,
			// not in the owner's face.
			log.Printf("post: not presented (decider unavailable): %v", derr)
			marks.save(a.At)
			continue
		}
		switch d {
		case post.PresentNow, post.Interrupt:
			fmt.Printf("%s ▸ %s — %s\n", strings.ToUpper(string(d)), post.CanonicalAddress(a.From), a.Subject)
		case post.Digest, post.QueueUntil:
			if aerr := appendLine(filepath.Join(o.stateDir, string(d)+".jsonl"), a); aerr != nil {
				return aerr
			}
		case post.Refuse:
			// Count, no content: a refused sender's words appear nowhere.
			refused++
		}
		marks.save(a.At)
	}
	if refused > 0 {
		log.Printf("post: %d arrival(s) refused (no live grant)", refused)
	}
	return nil
}

// watermark is the last-presented arrival time, durable across runs.
type watermark struct{ path string }

func (w watermark) load() time.Time {
	raw, err := os.ReadFile(w.path)
	if err != nil {
		// First run: look back one day rather than at the whole mailbox.
		return time.Now().Add(-24 * time.Hour)
	}
	var t time.Time
	if err := json.Unmarshal(raw, &t); err != nil {
		return time.Now().Add(-24 * time.Hour)
	}
	return t
}

func (w watermark) save(t time.Time) {
	if raw, err := json.Marshal(t); err == nil {
		// Failure to persist the watermark re-presents mail next run —
		// annoying, never unsafe; the log line is enough.
		if werr := os.WriteFile(w.path, raw, 0o600); werr != nil {
			log.Printf("post: watermark: %v", werr)
		}
	}
}

// appendLine adds one arrival to a digest/queue file as a JSON line.
func appendLine(path string, a post.Arrival) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("post: %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("post: %s: %w", path, cerr)
		}
	}()
	line, err := json.Marshal(struct {
		From    string    `json:"from"`
		Subject string    `json:"subject"`
		At      time.Time `json:"at"`
	}{post.CanonicalAddress(a.From), a.Subject, a.At})
	if err != nil {
		return fmt.Errorf("post: encode arrival: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("post: %s: %w", path, err)
	}
	return nil
}

// inQuietWindow reports whether now falls inside the owner's quiet hours,
// handling windows that cross midnight (22:00-07:00 is the default).
func inQuietWindow(now time.Time, from, to string) bool {
	parse := func(s string) (int, bool) {
		t, err := time.Parse("15:04", s)
		if err != nil {
			return 0, false
		}
		return t.Hour()*60 + t.Minute(), true
	}
	f, okF := parse(from)
	t, okT := parse(to)
	if !okF || !okT || f == t {
		return false
	}
	n := now.Hour()*60 + now.Minute()
	if f < t {
		return n >= f && n < t
	}
	return n >= f || n < t
}

// dirExists reports whether the path exists as a directory.
func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
