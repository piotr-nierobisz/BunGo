package engine_gcp

import "testing"

func TestNewGCPEngine(t *testing.T) {
	t.Parallel()
	e := NewGCPEngine("myfn")
	if e.FunctionName != "myfn" {
		t.Fatal(e.FunctionName)
	}
}
