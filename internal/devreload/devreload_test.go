package devreload

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestEnabled(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "": false, "0": false, "true": false} {
		t.Setenv(EnvVar, value)
		if got := Enabled(); got != want {
			t.Fatalf("Enabled() with %s=%q = %v; want %v", EnvVar, value, got, want)
		}
	}
}

func TestHandler_holdsConnectionUntilClientCloses(t *testing.T) {
	t.Parallel()
	returned := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r)
		close(returned)
	}))
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-returned:
		t.Fatal("handler returned while the client was still connected")
	case <-time.After(100 * time.Millisecond):
	}

	_ = conn.Close()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after the client disconnected")
	}
}

func TestHandler_rejectsPlainHTTP(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodGet, Path, nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 for a non-websocket request", rec.Code)
	}
}
