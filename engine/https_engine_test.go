package engine

import (
	"testing"

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
