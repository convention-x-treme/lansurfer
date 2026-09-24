package mediaproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DiskStore keeps covers as {dir}/{size}/{id}. It never evicts; operators
// may delete the directory to reset it.
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

func (d *DiskStore) Get(size, id string) ([]byte, bool, error) {
	image, err := os.ReadFile(filepath.Join(d.dir, size, id))
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
	temp, err := os.CreateTemp(filepath.Join(d.dir, size), ".tmp-*")
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
	return os.Rename(temp.Name(), filepath.Join(d.dir, size, id))
}
