package gziputil

import (
	"bytes"
	"compress/gzip"
	"io"
)

var GzipMagicHeader = []byte{'\x1f', '\x8b'}

func IsCompressed(r io.ReaderAt) (bool, error) {
	buf := make([]byte, 2)
	n, err := r.ReadAt(buf, 0)
	if n == 0 && err == io.EOF {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf, GzipMagicHeader), nil
}

func NewGzipReadCloser(r io.ReadCloser, level int) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer r.Close()

		gw, _ := gzip.NewWriterLevel(pw, level)
		defer gw.Close()

		_, err := io.Copy(gw, r)
		if err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return pr
}

type readCloser struct {
	io.Reader
	closeFunc func() error
}

func (r *readCloser) Close() error { return r.closeFunc() }

func NewGunzipReadCloser(r io.ReadCloser) (io.ReadCloser, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &readCloser{
		Reader: gr,
		closeFunc: func() error {
			defer r.Close()
			return gr.Close()
		},
	}, nil
}
