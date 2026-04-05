package fileutil

import (
	"path/filepath"
	"testing"
)

func TestNormalizeSlashPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  foo/bar  ", "foo/bar"},
		{`a\b\c`, "a/b/c"},
		{"//x//y", "//x//y"},
	}
	for _, tt := range tests {
		if got := NormalizeSlashPath(tt.in); got != tt.want {
			t.Errorf("NormalizeSlashPath(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanRelativeSlashPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in     string
		want   string
		wantOk bool
	}{
		{"", "", false},
		{"foo/bar", "foo/bar", true},
		{"/foo/bar", "foo/bar", true},
		{"./foo", "foo", true},
		{"../escape", "", false},
		{"foo/../bar", "bar", true},
		{"..", "", false},
		{"a/b/../c", "a/c", true},
	}
	for _, tt := range tests {
		got, ok := CleanRelativeSlashPath(tt.in)
		if ok != tt.wantOk || got != tt.want {
			t.Errorf("CleanRelativeSlashPath(%q) = (%q, %v); want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOk)
		}
	}
}

func TestCleanProjectRelativePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in     string
		want   string
		wantOk bool
	}{
		{"", "", false},
		{"/abs", "", false},
		{"proj/pkg", "proj/pkg", true},
		{"./x", "x", true},
		{"../up", "", false},
	}
	for _, tt := range tests {
		got, ok := CleanProjectRelativePath(tt.in)
		if ok != tt.wantOk || got != tt.want {
			t.Errorf("CleanProjectRelativePath(%q) = (%q, %v); want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOk)
		}
	}
}

func TestJoinRootAndSlashPath(t *testing.T) {
	t.Parallel()
	root := filepath.FromSlash("/tmp/proj")
	got := JoinRootAndSlashPath(root, "a/b/c")
	want := filepath.Clean(filepath.Join(root, "a", "b", "c"))
	if got != want {
		t.Fatalf("JoinRootAndSlashPath = %q; want %q", got, want)
	}
}
