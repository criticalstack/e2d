package util

import (
	"bytes"
	"compress/gzip"
	"io"
)

var gzipMagicHeader = []byte{'\x1f', '\x8b'}

func isCompressed(r *io.ReadCloser) bool {
	return bytes.Equal(
		peek(r, len(gzipMagicHeader)),
		gzipMagicHeader,
	)
}

func newGzipReadCloser(r io.ReadCloser, level int) io.ReadCloser {
	return pipe(func(w io.Writer) error {
		defer r.Close()
		gw, err := gzip.NewWriterLevel(w, level)
		if err != nil {
			return err
		}
		if _, err := io.Copy(gw, r); err != nil {
			return err
		}
		return gw.Close()
	})
}

// NewGzipReadCloser wraps a data stream with a gzip.Writer. If encryption is
// also detected, gzip should not use compression.
func NewGzipReadCloser(r io.ReadCloser) io.ReadCloser {
	if isEncrypted(&r) {
		return newGzipReadCloser(r, gzip.NoCompression)
	}
	return newGzipReadCloser(r, gzip.BestCompression)
}

// NewGunzipReadCloser wraps a data stream with a gzip.Reader.
func NewGunzipReadCloser(r io.ReadCloser) io.ReadCloser {
	if !isCompressed(&r) {
		return r
	}
	gr, err := gzip.NewReader(r)
	if err != nil {
		return &readCloser{r, func() error { return err }}
	}
	return &readCloser{
		Reader: gr,
		closeFunc: func() error {
			defer r.Close()
			return gr.Close()
		},
	}
}
