package mediaproxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
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

func TestDiskStoreRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cache")
	store, err := NewDiskStore(dir, "small")
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ bucket, id string }{
		{"small", "../victim"}, {"small", "a/b"}, {"small", `a\b`}, {"small", ".."}, {"small", "."},
		{"small", ""}, {"..", "victim"}, {"", "x"}, {"../small", "x"}, {"small", "/etc/passwd"},
	} {
		if _, _, err := store.Get(c.bucket, c.id); err == nil {
			t.Errorf("Get(%q, %q) error = nil", c.bucket, c.id)
		}
		if err := store.Put(c.bucket, c.id, []byte("evil")); err == nil {
			t.Errorf("Put(%q, %q) error = nil", c.bucket, c.id)
		}
		if err := store.Delete(c.bucket, c.id); err == nil {
			t.Errorf("Delete(%q, %q) error = nil", c.bucket, c.id)
		}
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "keep" {
		t.Errorf("victim = %q, %v", got, err)
	}
	var found []string
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, _ error) error {
		if !e.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	if len(found) != 1 || found[0] != victim {
		t.Errorf("unexpected files after rejected writes: %v", found)
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

// An avatar that is no longer anyone's selection is personal data with no
// purpose left, so Prune must delete its bytes from both store kinds while
// keeping selected avatars and the temp files of in-flight writes.
func TestPruneDeletesImagesTheCatalogNoLongerKnows(t *testing.T) {
	disk, err := NewDiskStore(t.TempDir(), "avatar", "big")
	if err != nil {
		t.Fatal(err)
	}
	stores := map[string]Store{"disk": disk, "memory": NewMemoryStore(1 << 20)}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"selected", "unlinked"} {
				_ = store.Put("avatar", id, []byte(id))
			}
			_ = store.Put("big", "unlinked", []byte("cover")) // another Service's bucket
			known := func(_ context.Context, id string) (bool, error) { return id == "selected", nil }
			service := NewService(store, nil, known, slog.New(slog.NewTextHandler(io.Discard, nil)), "avatar")
			if err := service.Prune(t.Context()); err != nil {
				t.Fatalf("Prune: %v", err)
			}
			if _, ok, _ := store.Get("avatar", "selected"); !ok {
				t.Error("selected avatar deleted")
			}
			if _, ok, _ := store.Get("avatar", "unlinked"); ok {
				t.Error("unknown avatar kept")
			}
			if _, ok, _ := store.Get("big", "unlinked"); !ok {
				t.Error("pruned outside the Service's buckets")
			}
		})
	}
	temp, err := os.CreateTemp(filepath.Join(disk.dir, "avatar"), ".tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	_ = temp.Close()
	if ids, _ := disk.IDs("avatar"); len(ids) != 1 || ids[0] != "selected" {
		t.Errorf("IDs = %v, want only the selected avatar", ids)
	}
}
