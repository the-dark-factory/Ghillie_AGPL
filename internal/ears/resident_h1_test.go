package ears

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequireLoopback is the structural half of H1: the owner's voice does not
// leave this machine, enforced by construction rather than by remembering a
// flag.
func TestRequireLoopback(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "loopback v4", url: "http://127.0.0.1:8932", wantErr: false},
		{name: "loopback v6", url: "http://[::1]:8932", wantErr: false},
		{name: "localhost resolves to loopback", url: "http://localhost:8932", wantErr: false},
		{name: "public IP refused", url: "http://8.8.8.8:8932", wantErr: true},
		{name: "private LAN IP refused", url: "http://192.168.0.42:8932", wantErr: true},
		{name: "off-box host refused", url: "http://example.com:8932", wantErr: true},
		{name: "non-http scheme refused", url: "ftp://127.0.0.1:8932", wantErr: true},
		{name: "no host refused", url: "http://", wantErr: true},
		{name: "not a url refused", url: "://nonsense", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requireLoopback(tc.url)
			if tc.wantErr && err == nil {
				t.Fatalf("requireLoopback(%q) = nil, want refusal — voice could leave the machine", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("requireLoopback(%q) = %v, want nil", tc.url, err)
			}
			if tc.wantErr && !errors.Is(err, ErrNotConfigured) {
				t.Errorf("error = %v, want it to wrap ErrNotConfigured", err)
			}
		})
	}
}

// TestStartResidentRefusesOffBoxBeforeProbing: an off-box URL must be refused
// WITHOUT being contacted. The old order probed first and adopted whatever
// answered, so a hostile endpoint was reached before anyone checked where it
// pointed.
func TestStartResidentRefusesOffBoxBeforeProbing(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer srv.Close()
	// Point at the test server by a NON-loopback name; it must never be hit.
	_, err := StartResident(context.Background(), ResidentConfig{URL: "http://example.com:8932"})
	if err == nil {
		t.Fatal("off-box whisper URL accepted")
	}
	if reached {
		t.Error("the off-box endpoint was contacted before validation")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("error = %v, want it to say why (not loopback)", err)
	}
}

// TestStartResidentWillNotSilentlyAdopt is the behavioural half of H1: a
// process that squats the port must NOT be handed the microphone just because
// it answers.
func TestStartResidentWillNotSilentlyAdopt(t *testing.T) {
	// A squatter: answers HTTP, is not whisper.
	squatter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer squatter.Close()

	t.Run("refused by default", func(t *testing.T) {
		_, err := StartResident(context.Background(), ResidentConfig{URL: squatter.URL})
		if err == nil {
			t.Fatal("a squatting process was silently adopted and would receive every utterance")
		}
		if !errors.Is(err, ErrNotConfigured) {
			t.Errorf("error = %v, want ErrNotConfigured", err)
		}
	})

	t.Run("adopted only when the operator says so", func(t *testing.T) {
		r, err := StartResident(context.Background(), ResidentConfig{URL: squatter.URL, AdoptExternal: true})
		if err != nil {
			t.Fatalf("explicit adoption refused: %v", err)
		}
		if !r.External {
			t.Error("adopted server not marked External — Close would kill a process we do not own")
		}
	})
}
