package engine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	bungo "github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/internal/devreload"
)

// devReloadServer serves a one-page BunGo app through CreateHandler.
func devReloadServer(t *testing.T, newServer func(http.Handler) *httptest.Server) *httptest.Server {
	t.Helper()
	eng := NewHTTPEngine()
	srv := bungo.NewServer(eng, mustWebDir(t))
	srv.Page(bungo.PageRoute{Path: "/", Template: "home.gohtml"})
	h, err := eng.CreateHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	ts := newServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func TestCreateHandler_liveReloadEndpointOnlyInDevMode(t *testing.T) {
	t.Setenv(devreload.EnvVar, "")
	ts := devReloadServer(t, httptest.NewServer)
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+devreload.Path, nil)
	if err == nil {
		_ = conn.Close()
		t.Fatal("live-reload endpoint mounted outside dev mode")
	}
	if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("status = %d; want no upgrade outside dev mode", resp.StatusCode)
	}
}

// TestCreateHandler_liveReloadOverHTTPS is a regression test: HTTPS pages
// dialed wss:// on a separate plain-ws dev port, so live reload never
// connected. The endpoint now lives on the app's own TLS server.
func TestCreateHandler_liveReloadOverHTTPS(t *testing.T) {
	t.Setenv(devreload.EnvVar, "1")
	ts := devReloadServer(t, httptest.NewTLSServer)

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `window.location.host + "`+devreload.Path+`"`) {
		t.Fatalf("page does not inject a same-origin live-reload client:\n%s", body)
	}

	dialer := websocket.Dialer{TLSClientConfig: ts.Client().Transport.(*http.Transport).TLSClientConfig}
	conn, resp, err := dialer.Dial("wss"+strings.TrimPrefix(ts.URL, "https")+devreload.Path, nil)
	if err != nil {
		t.Fatalf("wss live-reload dial failed: %v (response %v)", err, resp)
	}
	_ = conn.Close()
}
