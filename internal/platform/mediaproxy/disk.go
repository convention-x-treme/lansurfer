package mediaproxy

import (
	"crypto/rand"
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
	dir  string
	root *os.Root
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open media cache directory: %w", err)
	}
	return &DiskStore{dir: dir, root: root}, nil
}

// errUnsafeName rejects bucket or id values that are not one plain path
// element, so no caller can reach outside the store directory.
var errUnsafeName = errors.New("media cache name is not a single path element")

// name returns the root-relative path bucket/id. Each must be one clean
// element: not empty, not "." or "..", and free of separators. The os.Root
// additionally stops symlinks from leading out of the directory.
func name(bucket, id string) (string, error) {
	for _, element := range []string{bucket, id} {
		if element == "" || element == "." || element == ".." || strings.ContainsAny(element, `/\`) {
			return "", fmt.Errorf("%w: %q", errUnsafeName, element)
		}
	}
	return bucket + "/" + id, nil
}

func (d *DiskStore) Get(size, id string) ([]byte, bool, error) {
	file, err := name(size, id)
	if err != nil {
		return nil, false, err
	}
	image, err := d.root.ReadFile(file)
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
	file, err := name(size, id)
	if err != nil {
		return err
	}
	tempName := size + "/.tmp-" + rand.Text()
	temp, err := d.root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer d.root.Remove(tempName) // no-op after a successful rename
	if _, err := temp.Write(image); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return d.root.Rename(tempName, file)
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
	file, err := name(bucket, id)
	if err != nil {
		return err
	}
	if err := d.root.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
