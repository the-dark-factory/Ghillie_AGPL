// Command ghillie-slack runs the read-side Slack connector: the owner's
// messages presented under the owner's grants instead of Slack's notification
// regime (route A, the owner's own token, READ-ONLY v1).
//
// ⚠ SAY IT PLAINLY, EVERY START: this reads under the OWNER'S OWN Slack token.
// The scopes are history/read only — channels:history, groups:history,
// im:history, users:read — and there is NO send path in this binary:
// chat.postMessage appears nowhere. The token stays on this machine, 0600, and
// the owner can revoke it from their Slack account at any time.
//
// It polls, asks the proven delivery policy about each arrival, and speaks the
// answers: present_now and interrupt to standard output as they happen, digest
// and queue_until appended to their files under the state directory. Refusals
// leave a count and no content — a refused sender's text is not read aloud into
// a log either.
//
//	ghillie-slack -login                # one-time OAuth consent in the browser
//	ghillie-slack -once                 # single poll, then exit (cron-friendly)
//	ghillie-slack                       # poll loop
//
// The proven decider is named by DELIVERY_POLICY_DECIDER; without it NOTHING is
// presented and each arrival records a gap — fail closed, loudly.
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
	"github.com/tonygair/ghillie/internal/slackpost"
)

// version is the release this binary was cut from, set at build time by the
// release path (-ldflags "-X main.version=vX.Y.Z").
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("ghillie-slack: %v", err)
	}
}

// ghillieHome returns the directory this machine keeps ghillie's durable state
// in: $GHILLIE_HOME when set, otherwise ~/.ghillie. It matches ghillie's own
// resolution so the binaries share one home by default and one override.
func ghillieHome() string {
	if p := os.Getenv("GHILLIE_HOME"); p != "" {
		return p
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(h, ".ghillie")
}

type options struct {
	credentials string
	token       string
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
	defaultState := filepath.Join(ghillieHome(), "slack")

	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.StringVar(&o.credentials, "credentials", filepath.Join(defaultState, "client.json"), "the OWNER'S Slack app file ({client_id,client_secret}) — used by -login only")
	flag.StringVar(&o.token, "token", "", "file holding a ready-made read-only token (xoxp/xoxb); overrides the -login token store when set")
	flag.StringVar(&o.stateDir, "state", defaultState, "durable state dir (token, watermark, digest, queue, gap ledger) — never /tmp")
	flag.StringVar(&o.grants, "grants", "", "grants file (default <state>/grants.json), keys are Slack user ids; absent = refuse everyone")
	flag.StringVar(&o.presence, "presence", "free", "the owner's presence: free, occupied, away or asleep — an input to presentation, NEVER reported back to Slack")
	flag.StringVar(&o.quietFrom, "quiet-from", "22:00", "quiet hours start (local HH:MM)")
	flag.StringVar(&o.quietTo, "quiet-to", "07:00", "quiet hours end (local HH:MM)")
	flag.DurationVar(&o.poll, "poll", 2*time.Minute, "poll interval")
	flag.IntVar(&o.maxBatch, "max-batch", 25, "most arrivals fetched per poll")
	flag.BoolVar(&o.login, "login", false, "run the one-time OAuth consent flow and exit")
	flag.BoolVar(&o.once, "once", false, "poll once and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ghillie-slack %s\n", version)
		return nil
	}

	fmt.Println("⚠ route A: the owner's OWN Slack token, read-only (channels/groups/im history,")
	fmt.Println("  users:read). No send path exists in this binary; the token stays on this")
	fmt.Println("  machine (0600) and is revocable from the owner's Slack account.")

	if err := os.MkdirAll(o.stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if o.grants == "" {
		o.grants = filepath.Join(o.stateDir, "grants.json")
	}
	store := slackpost.TokenStore{Path: filepath.Join(o.stateDir, "token.json")}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if o.login {
		creds, cerr := slackpost.LoadClientCredentials(o.credentials)
		if cerr != nil {
			return cerr
		}
		return slackpost.Login(ctx, creds, store, func(u string) {
			fmt.Println("Open this in your browser to hand ghillie the read-only Slack key:")
			fmt.Println("  " + u)
			fmt.Println("Setup: create a Slack app, add the user scopes " + slackpost.UserScopes + ",")
			fmt.Println("set the redirect URL to the loopback shown above, then install to your workspace.")
		})
	}

	tokenFn, err := tokenSource(o, store)
	if err != nil {
		return err
	}

	table, err := slackpost.LoadTable(o.grants)
	if err != nil {
		return err
	}
	log.Printf("slack: %d sender grant(s) loaded from %s", len(table), o.grants)
	if os.Getenv(post.EnvDecider) == "" {
		log.Printf("slack: ⚠ %s unset — every arrival will be recorded as a gap and NOTHING will be presented", post.EnvDecider)
	}

	client := &slackpost.Client{TokenFn: tokenFn}
	decider := post.Decider{Gaps: gapledger.Open(filepath.Join(o.stateDir, "gap-ledger.jsonl"))}
	marks := watermark{path: filepath.Join(o.stateDir, "watermark.json")}

	for {
		if err := pollOnce(ctx, o, client, decider, table, marks); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			// One failed poll is not a dead connector: log and let the next
			// tick try again — but never pretend it worked.
			log.Printf("slack: poll failed: %v", err)
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

// tokenSource resolves the token function: a -token file wins, otherwise the
// OAuth store filled by -login. Either way the poller asks for the token fresh
// per call rather than holding one.
func tokenSource(o options, store slackpost.TokenStore) (func(ctx context.Context) (string, error), error) {
	if o.token != "" {
		raw, err := os.ReadFile(o.token)
		if err != nil {
			return nil, fmt.Errorf("slack: token file: %w", err)
		}
		tok := strings.TrimSpace(string(raw))
		if tok == "" {
			return nil, errors.New("slack: token file is empty")
		}
		return func(context.Context) (string, error) { return tok, nil }, nil
	}
	return func(ctx context.Context) (string, error) {
		return slackpost.AccessToken(ctx, store)
	}, nil
}

// pollOnce fetches arrivals since the watermark and hands each to the proven
// policy, oldest first so presentation order matches arrival order.
func pollOnce(ctx context.Context, o options, client *slackpost.Client, decider post.Decider, table post.Table, marks watermark) error {
	since := marks.load()
	ids, err := client.ListNewIDs(ctx, since, o.maxBatch)
	if err != nil {
		return err
	}
	var arrivals []slackpost.Arrival
	for _, id := range ids {
		a, gerr := client.GetArrival(ctx, id)
		if gerr != nil {
			return gerr
		}
		if !a.At.After(since) {
			// oldest= is second-granular; the watermark is exact.
			continue
		}
		arrivals = append(arrivals, a)
	}
	sort.Slice(arrivals, func(i, j int) bool { return arrivals[i].At.Before(arrivals[j].At) })

	refused := 0
	quiet := inQuietWindow(time.Now(), o.quietFrom, o.quietTo)
	for _, a := range arrivals {
		d, derr := slackpost.DecideArrival(ctx, decider, table, a, o.presence, quiet)
		if derr != nil {
			// Fail closed: nothing presented, and the reason is in the log,
			// not in the owner's face.
			log.Printf("slack: not presented (decider unavailable): %v", derr)
			marks.save(a.At)
			continue
		}
		switch d {
		case post.PresentNow, post.Interrupt:
			fmt.Printf("%s ▸ %s — %s\n", strings.ToUpper(string(d)), a.From, a.Subject)
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
		log.Printf("slack: %d arrival(s) refused (no live grant)", refused)
	}
	return nil
}

// watermark is the last-presented arrival time, durable across runs.
type watermark struct{ path string }

func (w watermark) load() time.Time {
	raw, err := os.ReadFile(w.path)
	if err != nil {
		// First run: look back one day rather than at the whole history.
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
		// Failure to persist re-presents next run — annoying, never unsafe.
		if werr := os.WriteFile(w.path, raw, 0o600); werr != nil {
			log.Printf("slack: watermark: %v", werr)
		}
	}
}

// appendLine adds one arrival to a digest/queue file as a JSON line.
func appendLine(path string, a slackpost.Arrival) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("slack: %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("slack: %s: %w", path, cerr)
		}
	}()
	line, err := json.Marshal(struct {
		From    string    `json:"from"`
		Subject string    `json:"subject"`
		At      time.Time `json:"at"`
	}{a.From, a.Subject, a.At})
	if err != nil {
		return fmt.Errorf("slack: encode arrival: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("slack: %s: %w", path, err)
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
