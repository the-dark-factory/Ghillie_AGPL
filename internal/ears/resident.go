package ears

// resident.go keeps a whisper-server WARM for the whole interview, because
// the estate's documented lesson is that spawn cost dominates: the model
// loads once here, not once per answer. If a server already answers at the
// configured URL it is used as found and never stopped by this process; if
// nothing answers, one is started as a child and stopped on Close.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// ResidentConfig configures the resident whisper-server.
type ResidentConfig struct {
	// URL is where the server should answer, e.g. http://127.0.0.1:8932.
	URL string

	// BinPath is the whisper-server executable. Empty means "whisper-server"
	// on PATH (homebrew's whisper-cpp ships it).
	BinPath string

	// ModelPath is the ggml model file. Required when the server has to be
	// started; ignored when one already answers at URL.
	ModelPath string

	// Threads is the CPU thread count. Zero uses the binary's own default.
	Threads int

	// StartupWait bounds how long to wait for a freshly started server to
	// begin answering. Zero means 30 s — base.en warms in about two.
	StartupWait time.Duration

	// AdoptExternal permits using a server this process did NOT start.
	//
	// H1 (security review 2026-08-07): adoption used to be silent and
	// unconditional — anything that answered at URL was taken to be whisper and
	// handed every captured utterance, in full, for the whole interview. An
	// unprivileged local process that bound the port FIRST received the owner's
	// voice, under a product claim reading "what you say stays on your machine".
	// Adoption is now opt-in: by default this process starts a server it owns.
	// Turning this on is the operator saying "I know what is on that port".
	AdoptExternal bool
}

// requireLoopback refuses any whisper URL that is not plainly on this machine.
//
// This is the structural half of H1: the claim is that the owner's VOICE never
// leaves their computer, and a claim like that should be enforced by
// construction rather than by everyone remembering to pass the right flag.
// Every address the host resolves to must be loopback — resolving (rather than
// string-matching "127.0.0.1") is what makes a hostile hosts-file entry for
// "localhost" fail closed instead of silently shipping audio off-box.
func requireLoopback(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: whisper-server URL %q is not a URL: %v", ErrNotConfigured, raw, err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%w: whisper-server URL %q must be http or https, not %q",
			ErrNotConfigured, raw, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: whisper-server URL %q names no host", ErrNotConfigured, raw)
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("%w: whisper-server host %q does not resolve: %v", ErrNotConfigured, host, err)
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("%w: whisper-server host %q resolves to %s, which is NOT loopback — "+
				"the owner's voice does not leave this machine, so this is refused",
				ErrNotConfigured, host, a)
		}
	}
	return nil
}

// Resident is a running whisper-server this process can transcribe against.
type Resident struct {
	// URL is the server base to hand to WhisperServer.
	URL string

	// External reports that the server was already running and is not owned
	// by this process: Close leaves it alone.
	External bool

	cmd *exec.Cmd
}

// Args returns the whisper-server argument list, exported so the exact
// command can be logged and reproduced by hand.
func (cfg ResidentConfig) Args(host, port string) []string {
	args := []string{
		"-m", cfg.ModelPath,
		"--host", host,
		"--port", port,
	}
	if cfg.Threads > 0 {
		args = append(args, "-t", fmt.Sprint(cfg.Threads))
	}
	return args
}

// StartResident returns a usable resident server: the one already answering
// at cfg.URL, or a freshly started child. The error, when there is one, says
// exactly what was missing.
func StartResident(ctx context.Context, cfg ResidentConfig) (*Resident, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("%w: whisper-server URL is empty", ErrNotConfigured)
	}
	// Validate BEFORE probing. The old order probed first and adopted whatever
	// answered, so an off-box URL was contacted — and trusted — before anyone
	// checked where it pointed.
	if err := requireLoopback(cfg.URL); err != nil {
		return nil, err
	}
	if answers(ctx, cfg.URL) {
		if !cfg.AdoptExternal {
			return nil, fmt.Errorf("%w: something already answers at %s and this process did not start it. "+
				"Refusing to hand it the microphone: it is not known to be whisper, and adopting it silently is "+
				"how the owner's voice reaches a process they never chose. Stop it, or pass the adopt-external "+
				"option to say you know what is listening there", ErrNotConfigured, cfg.URL)
		}
		return &Resident{URL: cfg.URL, External: true}, nil
	}

	parsed, err := url.Parse(cfg.URL)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" {
		return nil, fmt.Errorf("%w: whisper-server URL %q must be http://host:port", ErrNotConfigured, cfg.URL)
	}
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("%w: no server answers at %s and no model path was given to start one", ErrNotConfigured, cfg.URL)
	}
	bin := cfg.BinPath
	if bin == "" {
		bin = "whisper-server"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%w: no server answers at %s and %s is not on PATH (brew install whisper-cpp): %w", ErrNotConfigured, cfg.URL, bin, err)
	}

	args := cfg.Args(parsed.Hostname(), parsed.Port())
	cmd := exec.CommandContext(ctx, resolved, args...)
	stderr := &lockedBuffer{}
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: starting %s %s: %w", ErrTranscribeFailed, resolved, strings.Join(args, " "), err)
	}
	r := &Resident{URL: cfg.URL, cmd: cmd}

	wait := cfg.StartupWait
	if wait <= 0 {
		wait = 30 * time.Second
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			_ = r.Close()
			return nil, fmt.Errorf("%w: %w", ErrTranscribeFailed, err)
		}
		if cmd.ProcessState != nil {
			break // died already; fall through to the failure report
		}
		if answers(ctx, cfg.URL) {
			return r, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = r.Close()
	return nil, fmt.Errorf("%w: %s %s never answered at %s within %s (output: %s)",
		ErrTranscribeFailed, resolved, strings.Join(args, " "), cfg.URL, wait, tail(stderr.String(), 400))
}

// Close stops the server if this process started it, and leaves an external
// server exactly as it was found.
func (r *Resident) Close() error {
	if r.External || r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	if err := r.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("ears: stop whisper-server: %w", err)
	}
	_ = r.cmd.Wait() // killed on purpose; a non-zero exit is by design
	return nil
}

// answers reports whether anything HTTP is alive at base. Any response at
// all counts — the probe asks for liveness, not correctness; a wrong server
// shows up immediately on the first real transcription, loudly.
func answers(ctx context.Context, base string) bool {
	probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probe, http.MethodGet, base+"/", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	if cerr := resp.Body.Close(); cerr != nil {
		return true // it answered; a close error does not un-answer it
	}
	return true
}
