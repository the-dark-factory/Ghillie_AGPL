package voice

// player.go is local playback, and nothing more: this slice plays audio on
// THIS machine's speakers. No relay, no streaming, no daemon.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Player plays one WAV to completion on the local machine.
type Player interface {
	// Play blocks until the file has finished playing or ctx is done.
	Play(ctx context.Context, wavPath string) error
}

// afplayPath is macOS's stock command-line player.
const afplayPath = "/usr/bin/afplay"

// AFPlay plays audio through macOS's afplay. The same exec-boundary
// discipline as the renderer: a fixed external binary, explicit errors.
type AFPlay struct {
	// BinPath is the afplay executable.
	BinPath string
}

// NewAFPlay returns the local player, or an explicit error when this machine
// cannot play audio this way — a voiceless machine must be loud about it, not
// discovered mid-interview.
func NewAFPlay() (*AFPlay, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("%w: afplay playback is darwin-only and this is %s", ErrNotConfigured, runtime.GOOS)
	}
	if _, err := os.Stat(afplayPath); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotConfigured, afplayPath, err)
	}
	return &AFPlay{BinPath: afplayPath}, nil
}

// Play runs afplay over one file and blocks until it finishes.
func (p *AFPlay) Play(ctx context.Context, wavPath string) error {
	if p.BinPath == "" {
		return fmt.Errorf("%w: player has no binary", ErrNotConfigured)
	}
	cmd := exec.CommandContext(ctx, p.BinPath, wavPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s %s: %w (stderr: %s)",
			ErrPlaybackFailed, p.BinPath, wavPath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
