package engine

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bungo "github.com/piotr-nierobisz/BunGo"
)

func TestHTTPSEngine_Start_requiresCerts(t *testing.T) {
	t.Parallel()
	e := NewHTTPSEngine("", "")
	srv := bungo.NewServer(nil, "")
	err := e.Start(":0", srv)
	if err == nil {
		t.Fatal("expected error for missing cert")
	}
}

func TestHTTPSEngine_Start_requiresKey(t *testing.T) {
	t.Parallel()
	err := NewHTTPSEngine("cert.pem", "").Start(":0", bungo.NewServer(nil, ""))
	if err == nil || !strings.Contains(err.Error(), "keyFile is required") {
		t.Fatalf("expected keyFile error, got %v", err)
	}
}

// writeSelfSignedPair writes a PEM certificate and its PKCS#8 private key into dir.
func writeSelfSignedPair(t *testing.T, dir, name string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, name+".crt")
	keyFile = filepath.Join(dir, name+".key")
	mustWrite(t, certFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	mustWrite(t, keyFile, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})))
	return certFile, keyFile
}

func TestHTTPSEngine_Start_tlsConfigErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pem")
	mustWrite(t, garbage, "this is not PEM")
	certA, _ := writeSelfSignedPair(t, dir, "a")
	_, keyB := writeSelfSignedPair(t, dir, "b")

	tests := []struct {
		name     string
		cert     string
		key      string
		wantMsg  string
		notExist bool
	}{
		{name: "certificate file missing", cert: filepath.Join(dir, "absent.crt"), key: filepath.Join(dir, "absent.key"), notExist: true},
		{name: "files are not PEM", cert: garbage, key: garbage, wantMsg: "failed to find any PEM data"},
		{name: "key does not match certificate", cert: certA, key: keyB, wantMsg: "private key does not match public key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := bungo.NewServer(nil, mustWebDir(t))
			// Bind an ephemeral localhost port; certificate loading must fail before any request is served.
			err := startResult(t, func() error { return NewHTTPSEngine(tt.cert, tt.key).Start("127.0.0.1:0", srv) })
			if err == nil {
				t.Fatal("expected TLS configuration error")
			}
			if tt.notExist && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("want fs.ErrNotExist, got %v", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("error %q should contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestHTTPSEngine_Start_compileErrorStopsBeforeListening(t *testing.T) {
	eng := NewHTTPSEngine("missing.crt", "missing.key")
	srv := brokenViewServer(t, eng)
	// If compilation were skipped, the missing certificate would surface instead.
	err := startResult(t, func() error { return eng.Start("127.0.0.1:0", srv) })
	if err == nil || !strings.Contains(err.Error(), "esbuild") {
		t.Fatalf("expected compile error before TLS setup, got %v", err)
	}
}
