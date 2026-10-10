package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteExclusiveKeepsPrivateExistingManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "control.json")
	if err := writeExclusive(path, control{RequestID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusive(path, control{RequestID: "second"}); err == nil {
		t.Fatal("existing manifest was overwritten")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("manifest mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"request_id":"first"}`+"\n" {
		t.Fatalf("existing manifest changed: %q", data)
	}
}

func TestBytesLinesHandlesJSONLWithoutFinalNewline(t *testing.T) {
	got := bytesLines([]byte("one\n\ntwo"))
	if len(got) != 2 || string(got[0]) != "one" || string(got[1]) != "two" {
		t.Fatalf("lines = %q, want [one two]", got)
	}
}
