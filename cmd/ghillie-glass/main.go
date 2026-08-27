// Command ghillie-glass serves the OpenClaw gateway handshake so a third-party
// control UI can attach to ghillie.
//
// It serves the chat slice — connect, sessions.list, chat.history, chat.send,
// and the "chat" event stream — which is enough for the control UI's chat page
// to attach, replay a transcript and carry an interview. What it does NOT do
// is speak for ghillie: with no interview wired to the surface, an attached
// page sees the transcript and its own sends, and nothing answers. Wiring the
// real interview (enrol → brief → conduct) through interview.GlassSurface is
// cmd/ghillie's job, not this diagnostic's.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tonygair/ghillie/internal/glass"
	"github.com/tonygair/ghillie/internal/interview"
)

// The glass server IS an interview surface's chat: this assertion is where the
// two packages are first visible together, and it turns "the adapter fits"
// into a compile-time fact rather than a claim.
var _ interview.GlassChat = (*glass.Server)(nil)

func main() {
	addr := flag.String("addr", "127.0.0.1:8788", "listen address")
	version := flag.String("version", "ghillie-glass-spike", "reported server.version")
	flag.Parse()

	srv := glass.NewServer(*version)

	mux := http.NewServeMux()
	mux.Handle("/ws", srv)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok\n")); err != nil {
			log.Printf("healthz write: %v", err)
		}
	})

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("ghillie-glass: gateway v%d handshake on ws://%s/ws", glass.ProtocolVersion, *addr)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("ghillie-glass: %v", err)
	}
}
