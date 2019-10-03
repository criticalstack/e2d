package util

import (
	"bytes"
	"encoding/binary"
	"io"
)

type readCloser struct {
	io.Reader
	closeFunc func() error
}

func (r *readCloser) Close() error { return r.closeFunc() }

func pipe(fn func(io.Writer) error) *io.PipeReader {
	pr, pw := io.Pipe()
	go func() {
		if err := fn(pw); err != nil {
			//nolint:errcheck
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()
	return pr
}

func peek(r *io.ReadCloser, n int) []byte {
	buf := make([]byte, n)
	if _, err := io.ReadFull(*r, buf); err != nil {
		*r = &readCloser{*r, func() error { return err }}
		return nil
	}
	*r = &readCloser{
		Reader:    io.MultiReader(bytes.NewReader(buf), *r),
		closeFunc: (*r).Close,
	}
	return buf
}

func putVarint(size int64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	l := binary.PutVarint(buf, size)
	return buf[0:l]
}

type byteReader struct {
	io.Reader
}

func (r *byteReader) ReadByte() (byte, error) {
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}
