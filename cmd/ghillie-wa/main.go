// Command ghillie-wa runs the read-side WhatsApp connector: the owner's
// messages presented under the owner's grants instead of Meta's notification
// regime (proposal_connector_whatsapp_2026-08-25, APPROVED: route A).
//
// ⚠ SAY IT PLAINLY, EVERY START: route A links to the owner's PERSONAL
// account over the linked-device protocol. That is not an official API,
// automating a personal account violates WhatsApp's terms of service, and
// the ban risk that follows is the OWNER'S to accept. This program prints
// that at startup because an owner who was not told chose nothing.
//
// Message content is never read: a presentation is "a message from X". The
// proven delivery policy (DELIVERY_POLICY_DECIDER) decides every
// presentation; without it nothing is presented and each arrival records a
// gap. There is no send path in this binary.
//
//	ghillie-wa -login     # pair as a linked device (QR in the terminal)
//	ghillie-wa            # run: present arrivals under the owner's grants
package main

import (
	"context"
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
	"github.com/tonygair/ghillie/internal/wapost"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	// modernc.org/sqlite is the PURE-GO driver, chosen deliberately: the dist
	// matrix builds with CGO off, and a CGO driver silently vanishes from such
	// a build — the binary then fails at first real use, found in the 08-26
	// release sweep. Pure Go keeps the one-command matrix honest.
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("ghillie-wa: %v", err)
	}
}

// version is the release this binary was cut from, set at build time by the
// release path (-ldflags "-X main.version=vX.Y.Z").
var version = "dev"

// ghillieHome returns the directory this machine keeps ghillie's durable state
// in: $GHILLIE_HOME when set, otherwise ~/.ghillie. It matches ghillie's own
// resolution so the three binaries share one home by default and one override.
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

func run() error {
	defaultState := filepath.Join(ghillieHome(), "wa")

	var (
		showVersion = flag.Bool("version", false, "print the version and exit")
		stateDir    = flag.String("state", defaultState, "durable state dir (session store, grants, digest, queue, gap ledger) — never /tmp")
		grants      = flag.String("grants", "", "grants file (default <state>/grants.json), keys are phone numbers; absent = refuse everyone")
		presence    = flag.String("presence", "free", "the owner's presence: free, occupied, away or asleep — an input to presentation, NEVER reported back to WhatsApp")
		quietFrom   = flag.String("quiet-from", "22:00", "quiet hours start (local HH:MM)")
		quietTo     = flag.String("quiet-to", "07:00", "quiet hours end (local HH:MM)")
		login       = flag.Bool("login", false, "pair this ghillie as a linked device, then exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("ghillie-wa %s\n", version)
		return nil
	}

	fmt.Println("⚠ route A: linked-device protocol on the owner's PERSONAL account.")
	fmt.Println("  Not an official API; automation violates WhatsApp's terms; the")
	fmt.Println("  account-ban risk is the owner's. In exchange the E2E keys stay on")
	fmt.Println("  this machine and no business proxy holds the owner's messages.")

	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	if *grants == "" {
		*grants = filepath.Join(*stateDir, "grants.json")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	container, err := sqlstore.New(ctx, "sqlite",
		"file:"+filepath.Join(*stateDir, "session.db")+"?_pragma=foreign_keys(1)",
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("device store: %w", err)
	}
	client := whatsmeow.NewClient(device, waLog.Noop)

	if *login {
		return pair(ctx, client)
	}
	if client.Store.ID == nil {
		return fmt.Errorf("not paired — run ghillie-wa -login first")
	}

	table, err := wapost.LoadTable(*grants)
	if err != nil {
		return err
	}
	log.Printf("wa: %d sender grant(s) loaded from %s", len(table), *grants)
	if os.Getenv(post.EnvDecider) == "" {
		log.Printf("wa: ⚠ %s unset — every arrival will be recorded as a gap and NOTHING will be presented", post.EnvDecider)
	}
	decider := post.Decider{Gaps: gapledger.Open(filepath.Join(*stateDir, "gap-ledger.jsonl"))}

	refused := 0
	client.AddEventHandler(func(raw interface{}) {
		evt, isMsg := raw.(*events.Message)
		if !isMsg {
			return
		}
		a, ok := wapost.FromEvent(evt)
		if !ok {
			return
		}
		quiet := inQuietWindow(time.Now(), *quietFrom, *quietTo)
		d, derr := wapost.DecideArrival(ctx, decider, table, a, *presence, quiet)
		if derr != nil {
			log.Printf("wa: not presented (decider unavailable): %v", derr)
			return
		}
		switch d {
		case post.PresentNow, post.Interrupt:
			who := a.Sender
			if a.PushName != "" {
				// The push name is the sender's own claim — shown, not trusted.
				who = a.Sender + " (" + a.PushName + ")"
			}
			suffix := ""
			if a.Chat != "" {
				suffix = " in group " + a.Chat
			}
			fmt.Printf("%s ▸ message from %s%s\n", strings.ToUpper(string(d)), who, suffix)
		case post.Digest, post.QueueUntil:
			if aerr := appendArrival(filepath.Join(*stateDir, string(d)+".jsonl"), a); aerr != nil {
				log.Printf("wa: %v", aerr)
			}
		case post.Refuse:
			refused++
			log.Printf("wa: arrival refused (no live grant) — %d refused so far", refused)
		}
	})

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	log.Printf("wa: connected; presenting under the owner's grants")
	<-ctx.Done()
	client.Disconnect()
	return nil
}

// pair runs the QR linked-device flow once.
func pair(ctx context.Context, client *whatsmeow.Client) error {
	if client.Store.ID != nil {
		fmt.Println("already paired; delete the session store to pair afresh")
		return nil
	}
	qrCh, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("qr channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect for pairing: %w", err)
	}
	defer client.Disconnect()
	for item := range qrCh {
		switch item.Event {
		case "code":
			fmt.Println("On the phone: WhatsApp ▸ Settings ▸ Linked devices ▸ Link a device")
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
		case "success":
			fmt.Println("paired — this ghillie now holds a linked-device key (session.db, this machine only)")
			return nil
		default:
			// timeout / error events end the channel; fall through.
		}
	}
	return fmt.Errorf("pairing ended without success — run -login again")
}

// appendArrival adds one identity-only record to a digest/queue file.
func appendArrival(path string, a wapost.Arrival) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("wa: %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("wa: %s: %w", path, cerr)
		}
	}()
	line := fmt.Sprintf("{\"sender\":%q,\"chat\":%q,\"at\":%q}\n", a.Sender, a.Chat, a.At.Format(time.RFC3339))
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("wa: %s: %w", path, err)
	}
	return nil
}

// inQuietWindow mirrors ghillie-post's: local quiet hours, midnight-crossing.
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
