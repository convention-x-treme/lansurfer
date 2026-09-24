package mediaproxy

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryStoreEvictsLeastRecentlyUsed(t *testing.T) {
	store := NewMemoryStore(10)
	_ = store.Put("big", "a", []byte("aaaa"))
	_ = store.Put("big", "b", []byte("bbbb"))
	if _, ok, _ := store.Get("big", "a"); !ok { // a is now most recent
		t.Fatal("a missing")
	}
	_ = store.Put("big", "c", []byte("cccc")) // evicts b
	if _, ok, _ := store.Get("big", "b"); ok {
		t.Error("b survived eviction")
	}
	for _, id := range []string{"a", "c"} {
		if _, ok, _ := store.Get("big", id); !ok {
			t.Errorf("%s evicted", id)
		}
	}
	_ = store.Put("big", "huge", make([]byte, 11))
	if _, ok, _ := store.Get("big", "huge"); ok {
		t.Error("oversize image stored")
	}
	if _, ok, _ := store.Get("small", "a"); ok {
		t.Error("sizes share a key")
	}
}

func TestDiskStoreRoundTrip(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), "small", "big")
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	if _, ok, err := store.Get("big", "co1"); ok || err != nil {
		t.Fatalf("Get(missing) = %v, %v", ok, err)
	}
	if err := store.Put("big", "co1", []byte("image")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	image, ok, err := store.Get("big", "co1")
	if err != nil || !ok || !bytes.Equal(image, []byte("image")) {
		t.Fatalf("Get = %q, %v, %v", image, ok, err)
	}
	if _, ok, _ := store.Get("small", "co1"); ok {
		t.Error("sizes share a file")
	}
}

func TestDiskStoreRejectsUnusableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskStore(file, "small"); err == nil {
		t.Fatal("NewDiskStore(file) error = nil")
	}
}
