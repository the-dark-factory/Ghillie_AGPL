// Command ghillie-discord runs the read-side Discord connector: the owner's
// configured channels presented under the owner's grants instead of Discord's
// notification regime (route A, the owner's own bot token, READ-ONLY v1).
//
// ⚠ SAY IT PLAINLY, EVERY START: this reads under a bot token the owner
// created in the Discord developer portal. Running a bot on a server is subject
// to Discord's terms and the server's own rules; the bot must be a member of a
// channel to read it. The ONLY endpoint called is GET /channels/{id}/messages —
// a read — and there is NO send path in this binary: POST message appears
// nowhere, and no gateway socket is opened. The token stays on this machine,
// 0600.
//
// It polls each configured channel, asks the proven delivery policy about each
// arrival, and speaks the answers: present_now and interrupt to standard output
// as they happen, digest and queue_until appended to their files under the
// state directory. Refusals leave a count and no content.
//
//	ghillie-discord -login              # validate the bot token, print setup
//	ghillie-discord -once               # single poll, then exit (cron-friendly)
//	ghillie-discord                     # poll loop
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

	"github.com/tonygair/ghillie/internal/dcpost"
	"github.com/tonygair/ghillie/internal/gapledger"
	"github.com/tonygair/ghillie/internal/post"
)

// version is the release this binary was cut from, set at build time by the
// release path (-ldflags "-X main.version=vX.Y.Z").
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatalf("ghillie-discord: %v", err)
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
	channels  string
	presence  string
	quietFrom string
	quietTo   string
	poll      time.Duration
	maxBatch  int
	login     bool
	once      bool
}

func run() (err error) {
	var o options
	defaultState := filepath.Join(ghillieHome(), "discord")

	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.StringVar(&o.token, "token", filepath.Join(defaultState, "token"), "file holding the OWNER'S bot token — this machine only, 0600")
	flag.StringVar(&o.stateDir, "state", defaultState, "durable state dir (watermark, digest, queue, gap ledger) — never /tmp")
	flag.StringVar(&o.grants, "grants", "", "grants file (default <state>/grants.json), keys are Discord usernames; absent = refuse everyone")
	flag.StringVar(&o.channels, "channels", "", "channels file (default <state>/channels.txt), one channel snowflake per line; absent = watch nothing")
	flag.StringVar(&o.presence, "presence", "free", "the owner's presence: free, occupied, away or asleep — an input to presentation, NEVER reported back to Discord")
	flag.StringVar(&o.quietFrom, "quiet-from", "22:00", "quiet hours start (local HH:MM)")
	flag.StringVar(&o.quietTo, "quiet-to", "07:00", "quiet hours end (local HH:MM)")
	flag.DurationVar(&o.poll, "poll", 2*time.Minute, "poll interval")
	flag.IntVar(&o.maxBatch, "max-batch", 25, "most messages fetched per channel per poll")
	flag.BoolVar(&o.login, "login", false, "validate the bot token, print setup instructions, and exit")
	flag.BoolVar(&o.once, "once", false, "poll once and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ghillie-discord %s\n", version)
		return nil
	}

	fmt.Println("⚠ route A: the owner's OWN bot token, read-only. The only endpoint called is")
	fmt.Println("  GET /channels/{id}/messages; no send path exists and no gateway is opened.")
	fmt.Println("  Running a bot is subject to Discord's terms and each server's rules; the bot")
	fmt.Println("  reads only channels it is a member of. Token stays local (0600).")

	if err := os.MkdirAll(o.stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if o.grants == "" {
		o.grants = filepath.Join(o.stateDir, "grants.json")
	}
	if o.channels == "" {
		o.channels = filepath.Join(o.stateDir, "channels.txt")
	}

	token, err := readToken(o.token)
	if err != nil {
		return err
	}
	client := &dcpost.Client{Token: token}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if o.login {
		name, verr := client.Username(ctx)
		if verr != nil {
			return fmt.Errorf("token did not validate: %w", verr)
		}
		fmt.Printf("token valid — bot is %s\n", name)
		fmt.Println("Setup: 1) create an application and bot in the Discord developer portal;")
		fmt.Println("       2) enable the MESSAGE CONTENT intent, invite the bot to the server(s);")
		fmt.Println("       3) list the channel ids to watch in " + o.channels + " (one per line);")
		fmt.Println("       4) run ghillie-discord. This connector never posts.")
		return nil
	}

	channels, err := dcpost.LoadChannels(o.channels)
	if err != nil {
		return err
	}
	log.Printf("discord: %d channel(s) watched from %s", len(channels), o.channels)
	if len(channels) == 0 {
		log.Printf("discord: ⚠ no channels listed in %s — nothing will be polled", o.channels)
	}

	table, err := dcpost.LoadTable(o.grants)
	if err != nil {
		return err
	}
	log.Printf("discord: %d sender grant(s) loaded from %s", len(table), o.grants)
	if os.Getenv(post.EnvDecider) == "" {
		log.Printf("discord: ⚠ %s unset — every arrival will be recorded as a gap and NOTHING will be presented", post.EnvDecider)
	}

	decider := post.Decider{Gaps: gapledger.Open(filepath.Join(o.stateDir, "gap-ledger.jsonl"))}
	marks := watermark{path: filepath.Join(o.stateDir, "watermark.json")}

	for {
		if err := pollOnce(ctx, o, client, decider, table, channels, marks); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			// One failed poll is not a dead connector: log and let the next
			// tick try again — but never pretend it worked.
			log.Printf("discord: poll failed: %v", err)
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

// pollOnce reads each channel's messages since its per-channel watermark and
// hands each arrival to the proven policy, oldest first so presentation order
// matches arrival order. The watermark advances to the highest snowflake seen.
func pollOnce(ctx context.Context, o options, client *dcpost.Client, decider post.Decider, table post.Table, channels []string, marks watermark) error {
	quiet := inQuietWindow(time.Now(), o.quietFrom, o.quietTo)
	refused := 0
	for _, channel := range channels {
		after := marks.load(channel)
		arrivals, merr := client.Messages(ctx, channel, after, o.maxBatch)
		if merr != nil {
			return merr
		}
		sort.Slice(arrivals, func(i, j int) bool { return arrivals[i].At.Before(arrivals[j].At) })

		highest := after
		for _, a := range arrivals {
			d, derr := dcpost.DecideArrival(ctx, decider, table, a, o.presence, quiet)
			if derr != nil {
				// Fail closed: nothing presented, the reason in the log. Still
				// advance the watermark so it is not re-fetched forever.
				log.Printf("discord: not presented (decider unavailable): %v", derr)
				if dcpost.HigherSnowflake(a.ID, highest) {
					highest = a.ID
				}
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
				// Count, no content: a refused author's words appear nowhere.
				refused++
			}
			if dcpost.HigherSnowflake(a.ID, highest) {
				highest = a.ID
			}
		}
		if highest != after {
			marks.save(channel, highest)
		}
	}
	if refused > 0 {
		log.Printf("discord: %d arrival(s) refused (no live grant)", refused)
	}
	return nil
}

// readToken loads the bot token from its file, trimmed. A missing file is a
// clear "put your bot token here".
func readToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("discord: bot token file %s (paste your bot token there): %w", path, err)
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" {
		return "", errors.New("discord: bot token file is empty")
	}
	return tok, nil
}

// watermark is the last-seen message snowflake per channel, durable across
// runs. Discord's after= filter is a snowflake, not a time, so the watermark
// is that id.
type watermark struct{ path string }

func (w watermark) loadAll() map[string]string {
	m := map[string]string{}
	raw, err := os.ReadFile(w.path)
	if err != nil {
		return m
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]string{}
	}
	return m
}

func (w watermark) load(channel string) string {
	return w.loadAll()[channel]
}

func (w watermark) save(channel, id string) {
	m := w.loadAll()
	m[channel] = id
	if raw, err := json.Marshal(m); err == nil {
		// Failure to persist re-presents next run — annoying, never unsafe.
		if werr := os.WriteFile(w.path, raw, 0o600); werr != nil {
			log.Printf("discord: watermark: %v", werr)
		}
	}
}

// appendLine adds one arrival to a digest/queue file as a JSON line.
func appendLine(path string, a dcpost.Arrival) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("discord: %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("discord: %s: %w", path, cerr)
		}
	}()
	line, err := json.Marshal(struct {
		From    string    `json:"from"`
		Channel string    `json:"channel"`
		Subject string    `json:"subject"`
		At      time.Time `json:"at"`
	}{a.From, a.Channel, a.Subject, a.At})
	if err != nil {
		return fmt.Errorf("discord: encode arrival: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("discord: %s: %w", path, err)
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
