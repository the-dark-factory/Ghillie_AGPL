// Package glass speaks the OpenClaw Gateway WebSocket protocol (wire v4) so a
// third-party control UI can attach to ghillie.
//
// DOCTRINE. This package is GLASS, not seam: it carries frames and decides
// nothing. Every admission question it meets is handed to the proven gate
// (internal/gate, ada-factory ledger 112). See project_unproven_glass_proven_seam_ui.
//
// TRANSPORT. RFC 6455 framing is github.com/coder/websocket v1.8.15 (ISC, zero
// transitive dependencies). It replaced a hand-rolled framer on 2026-08-25.
// That framer worked, but frame parsing on a security product is the wrong
// thing to own: ClawJacked was a zero-click WebSocket hijack, and this is the
// layer it lived in. Two concrete defects the library closes:
//
//   - ORIGIN. The hand-rolled server accepted any Origin, so any page in the
//     user's browser could open a socket to a local ghillie and drive it. The
//     library authenticates Origin against the request host by default
//     (accept.go: `if !opts.InsecureSkipVerify { authenticateOrigin(...) }`).
//     That is the specific defence against the ClawJacked shape.
//   - FRAGMENTATION. The hand-rolled reader rejected continuation frames
//     outright; a conforming client that split a large frame would have been
//     dropped. The library reassembles them.
//
// Borrowed method, logged per feedback_ada_first_methods_register.
package glass

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// maxFrameBytes bounds one inbound payload. A control UI sends small JSON; a
// frame larger than this is a fault or an attack, never legitimate traffic.
const maxFrameBytes = 1 << 20

// readTimeout bounds how long one idle connection may hold a slot.
const readTimeout = 5 * time.Minute

// conn is one accepted WebSocket connection.
type conn struct {
	ws  *websocket.Conn
	ctx context.Context

	// writeMu serializes writes: the read loop answers requests while
	// broadcast delivers events, and interleaving two JSON texts inside one
	// frame is corruption the reader cannot recover from.
	writeMu sync.Mutex

	// challengeNonce is the connect.challenge nonce this connection was
	// issued; a device block on connect must echo exactly it.
	challengeNonce string
}

// accept performs the RFC 6455 server handshake.
//
// originPatterns are additional authorized origins. Empty means same-origin
// only, which is the correct default: the control UI is served beside the
// gateway, so nothing else has business opening a socket.
func accept(w http.ResponseWriter, r *http.Request, originPatterns []string) (result *conn, err error) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: originPatterns,
		// Compression stays off. It buys nothing on small JSON frames and
		// compression oracles are a class of bug worth not having.
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("glass: accept: %w", err)
	}
	ws.SetReadLimit(maxFrameBytes)
	return &conn{ws: ws, ctx: r.Context()}, nil
}

// readText reads one text message. Ping/pong and fragmentation are handled by
// the library. A binary message is refused: gateway frames are JSON text, and
// silently accepting another encoding would widen the parser's input.
func (c *conn) readText() (payload []byte, err error) {
	ctx, cancel := context.WithTimeout(c.ctx, readTimeout)
	defer cancel()

	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, errors.New("glass: expected a text frame")
	}
	return data, nil
}

// writeText sends one text message.
func (c *conn) writeText(payload []byte) (err error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	if err = c.ws.Write(ctx, websocket.MessageText, payload); err != nil {
		return fmt.Errorf("glass: write: %w", err)
	}
	return nil
}

// Close ends the connection with a normal-closure status.
func (c *conn) Close() error {
	return c.ws.Close(websocket.StatusNormalClosure, "")
}

// CloseWithProtocolError ends a connection that broke the protocol, so the peer
// learns why rather than seeing an abrupt drop.
func (c *conn) CloseWithProtocolError(reason string) error {
	return c.ws.Close(websocket.StatusProtocolError, reason)
}
