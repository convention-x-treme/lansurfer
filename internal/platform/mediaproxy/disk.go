package mediaproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DiskStore keeps images as {dir}/{bucket}/{id}. It never evicts covers;
// Service.Prune removes unknown avatars. Operators may delete the directory
// to reset it.
type DiskStore struct {
	dir string
}

func NewDiskStore(dir string, buckets ...string) (*DiskStore, error) {
	for _, bucket := range buckets {
		if err := os.MkdirAll(filepath.Join(dir, bucket), 0o750); err != nil {
			return nil, fmt.Errorf("create media cache directory: %w", err)
		}
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return nil, fmt.Errorf("media cache directory is not writable: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return &DiskStore{dir: dir}, nil
}

// errUnsafeName rejects bucket or id values that are not one plain path
// element, so no caller can reach outside the store directory.
var errUnsafeName = errors.New("media cache name is not a single path element")

// path joins bucket and id under d.dir. Each must be one clean element: not
// empty, not "." or "..", and free of separators.
func (d *DiskStore) path(bucket, id string) (string, error) {
	for _, name := range []string{bucket, id} {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || !filepath.IsLocal(name) {
			return "", fmt.Errorf("%w: %q", errUnsafeName, name)
		}
	}
	return filepath.Join(d.dir, bucket, id), nil
}

func (d *DiskStore) Get(size, id string) ([]byte, bool, error) {
	name, err := d.path(size, id)
	if err != nil {
		return nil, false, err
	}
	image, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return image, true, nil
}

// Put writes through a temp file and rename so a reader never sees a
// partial image. Temp names start with "." and cannot collide with IDs.
func (d *DiskStore) Put(size, id string, image []byte) error {
	name, err := d.path(size, id)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(name), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name()) // no-op after a successful rename
	if _, err := temp.Write(image); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), name)
}

// IDs lists cached images, skipping the "." temp files of in-flight Puts.
func (d *DiskStore) IDs(bucket string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.dir, bucket))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}

func (d *DiskStore) Delete(bucket, id string) error {
	name, err := d.path(bucket, id)
	if err != nil {
		return err
	}
	if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
