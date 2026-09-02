// Command ghillie-telegram runs the read-side Telegram connector: the owner's
// bot inbound presented under the owner's grants instead of Telegram's
// notification regime (route A, the owner's own bot token, READ-ONLY v1).
//
// ⚠ SAY IT PLAINLY, EVERY START: this reads under a bot token the owner
// created with BotFather. The ONLY Bot API method it calls is getUpdates — a
// read — and there is NO send path in this binary: sendMessage appears
// nowhere. A bot receives only what is sent to it (and, with group privacy
// off, its groups); that inbound is the owner's, by design. The token stays on
// this machine, 0600.
//
// It long-polls, asks the proven delivery policy about each arrival, and speaks
// the answers: present_now and interrupt to standard output as they happen,
// digest and queue_until appended to their files under the state directory.
// Refusals leave a count and no content.
//
//	ghillie-telegram -login             # validate the bot token, print setup
//	ghillie-telegram -once              # single poll, then exit (cron-friendly)
//	ghillie-telegram                    # long-poll loop
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
	"strings"
	"syscall"
	"time"

	"github.com/tonygair/ghillie/internal/gapledger"
	"github.com/tonygair/ghillie/internal/post"
	"github.com/tonygair/ghillie/internal/tgpost"
)

// version is the release this binary was cut from, set at build time by the
// release path (-ldflags "-X main.version=vX.Y.Z").
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("ghillie-telegram: %v", err)
	}
}

// ghillieHome returns the directory this machine keeps ghillie's durable state
// in: $GHILLIE_HOME when set, otherwise ~/.ghillie.
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
	token     string
	stateDir  string
	grants    string
	presence  string
	quietFrom string
	quietTo   string
	poll      time.Duration
	timeout   int
	maxBatch  int
	login     bool
	once      bool
}

func run() (err error) {
	var o options
	defaultState := filepath.Join(ghillieHome(), "telegram")

	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.StringVar(&o.token, "token", filepath.Join(defaultState, "token"), "file holding the OWNER'S bot token from BotFather — this machine only, 0600")
	flag.StringVar(&o.stateDir, "state", defaultState, "durable state dir (watermark, digest, queue, gap ledger) — never /tmp")
	flag.StringVar(&o.grants, "grants", "", "grants file (default <state>/grants.json), keys are Telegram handles; absent = refuse everyone")
	flag.StringVar(&o.presence, "presence", "free", "the owner's presence: free, occupied, away or asleep — an input to presentation, NEVER reported back to Telegram")
	flag.StringVar(&o.quietFrom, "quiet-from", "22:00", "quiet hours start (local HH:MM)")
	flag.StringVar(&o.quietTo, "quiet-to", "07:00", "quiet hours end (local HH:MM)")
	flag.DurationVar(&o.poll, "poll", 2*time.Minute, "poll interval (between long-poll requests)")
	flag.IntVar(&o.timeout, "long-poll", 30, "getUpdates long-poll timeout in seconds")
	flag.IntVar(&o.maxBatch, "max-batch", 25, "most updates fetched per poll")
	flag.BoolVar(&o.login, "login", false, "validate the bot token, print setup instructions, and exit")
	flag.BoolVar(&o.once, "once", false, "poll once and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ghillie-telegram %s\n", version)
		return nil
	}

	fmt.Println("⚠ route A: the owner's OWN bot token, read-only. The only Bot API method")
	fmt.Println("  called is getUpdates; no send path exists in this binary. A bot sees only")
	fmt.Println("  what is sent to it (and its groups with privacy off). Token stays local (0600).")

	if err := os.MkdirAll(o.stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if o.grants == "" {
		o.grants = filepath.Join(o.stateDir, "grants.json")
	}

	token, err := readToken(o.token)
	if err != nil {
		return err
	}
	client := &tgpost.Client{Token: token}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if o.login {
		name, verr := client.Username(ctx)
		if verr != nil {
			return fmt.Errorf("token did not validate: %w", verr)
		}
		fmt.Printf("token valid — bot is @%s\n", name)
		fmt.Println("Setup: 1) create the bot with @BotFather and paste its token into the -token file;")
		fmt.Println("       2) to receive group messages, turn OFF the bot's group privacy in BotFather;")
		fmt.Println("       3) run ghillie-telegram to begin long-polling. This connector never sends.")
		return nil
	}

	table, err := tgpost.LoadTable(o.grants)
	if err != nil {
		return err
	}
	log.Printf("telegram: %d sender grant(s) loaded from %s", len(table), o.grants)
	if os.Getenv(post.EnvDecider) == "" {
		log.Printf("telegram: ⚠ %s unset — every arrival will be recorded as a gap and NOTHING will be presented", post.EnvDecider)
	}

	decider := post.Decider{Gaps: gapledger.Open(filepath.Join(o.stateDir, "gap-ledger.jsonl"))}
	marks := watermark{path: filepath.Join(o.stateDir, "offset.json")}

	for {
		if err := pollOnce(ctx, o, client, decider, table, marks); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			// One failed poll is not a dead connector: log and let the next
			// tick try again — but never pretend it worked.
			log.Printf("telegram: poll failed: %v", err)
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

// pollOnce long-polls for updates since the offset and hands each arrival to
// the proven policy in cursor order, then persists the advanced offset.
func pollOnce(ctx context.Context, o options, client *tgpost.Client, decider post.Decider, table post.Table, marks watermark) error {
	offset := marks.load()
	arrivals, next, err := client.GetUpdates(ctx, offset, o.timeout, o.maxBatch)
	if err != nil {
		return err
	}

	refused := 0
	quiet := inQuietWindow(time.Now(), o.quietFrom, o.quietTo)
	for _, a := range arrivals {
		d, derr := tgpost.DecideArrival(ctx, decider, table, a, o.presence, quiet)
		if derr != nil {
			// Fail closed: nothing presented, the reason in the log.
			log.Printf("telegram: not presented (decider unavailable): %v", derr)
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
	}
	// Persist the cursor even when the batch presented nothing, so refused and
	// decider-less updates are not re-fetched forever.
	marks.save(next)
	if refused > 0 {
		log.Printf("telegram: %d arrival(s) refused (no live grant)", refused)
	}
	return nil
}

// readToken loads the bot token from its file, trimmed. The token is a
// credential, so a missing file is a clear "put your BotFather token here".
func readToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("telegram: bot token file %s (paste your BotFather token there): %w", path, err)
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" {
		return "", errors.New("telegram: bot token file is empty")
	}
	return tok, nil
}

// watermark is the last-seen Telegram update offset, durable across runs.
type watermark struct{ path string }

func (w watermark) load() int64 {
	raw, err := os.ReadFile(w.path)
	if err != nil {
		// First run: 0 asks Telegram for whatever it still holds (a few days).
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

func (w watermark) save(n int64) {
	if raw, err := json.Marshal(n); err == nil {
		// Failure to persist re-presents next run — annoying, never unsafe.
		if werr := os.WriteFile(w.path, raw, 0o600); werr != nil {
			log.Printf("telegram: offset: %v", werr)
		}
	}
}

// appendLine adds one arrival to a digest/queue file as a JSON line.
func appendLine(path string, a tgpost.Arrival) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("telegram: %s: %w", path, cerr)
		}
	}()
	line, err := json.Marshal(struct {
		From    string    `json:"from"`
		Subject string    `json:"subject"`
		At      time.Time `json:"at"`
	}{a.From, a.Subject, a.At})
	if err != nil {
		return fmt.Errorf("telegram: encode arrival: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("telegram: %s: %w", path, err)
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
