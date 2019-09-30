package snapshot

import (
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

type FileSnapshotter struct {
	file string
}

func NewFileSnapshotter(path string) (*FileSnapshotter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && !os.IsExist(err) {
		return nil, errors.Wrapf(err, "cannot create snapshot directory: %#v", filepath.Dir(path))
	}
	return &FileSnapshotter{file: path}, nil
}

func (fs *FileSnapshotter) Load() (io.ReadCloser, error) {
	return os.Open(fs.file)
}

func (fs *FileSnapshotter) Save(r io.ReadCloser) error {
	defer r.Close()
	f, err := os.OpenFile(fs.file, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	return err
}
