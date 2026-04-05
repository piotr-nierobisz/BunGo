package bungo

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestRegisterEmbeddedAssetsFS_roundTrip(t *testing.T) {
	prev := getEmbeddedAssetsFS()
	defer RegisterEmbeddedAssetsFS(prev)

	mem := fstest.MapFS{"web/x.txt": &fstest.MapFile{Data: []byte("ok")}}
	RegisterEmbeddedAssetsFS(mem)
	got := getEmbeddedAssetsFS()
	if got == nil {
		t.Fatal("nil fs")
	}
	b, err := fs.ReadFile(got, "web/x.txt")
	if err != nil || string(b) != "ok" {
		t.Fatalf("read: %v %q", err, b)
	}
}
