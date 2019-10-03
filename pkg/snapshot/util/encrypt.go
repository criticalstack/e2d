package util

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/criticalstack/e2d/pkg/snapshot/crypto"
)

var encryptedSnapshotHeader = []byte("ENCRYPTED:")

func isEncrypted(r *io.ReadCloser) bool {
	return bytes.Equal(
		peek(r, len(encryptedSnapshotHeader)),
		encryptedSnapshotHeader,
	)
}

// NewEncrypterReadCloser wraps a data stream with encryption using the
// provided key. The size of the stream is required ahead of time to provide an
// offset for the message authentication signature.
func NewEncrypterReadCloser(r io.ReadCloser, key *[32]byte, size int64) io.ReadCloser {
	return pipe(func(w io.Writer) error {
		defer r.Close()
		if _, err := w.Write(encryptedSnapshotHeader); err != nil {
			return err
		}
		if _, err := w.Write(putVarint(size)); err != nil {
			return err
		}
		return crypto.Encrypt(r, w, key)
	})
}

var ErrNoEncryptionKey = errors.New("no encryption key provided")

// NewDecrypterReadCloser wraps a data stream with decryption using the
// provided key.
func NewDecrypterReadCloser(r io.ReadCloser, key *[32]byte) io.ReadCloser {
	if !isEncrypted(&r) {
		return r
	}
	return pipe(func(w io.Writer) error {
		defer r.Close()
		header := make([]byte, len(encryptedSnapshotHeader))
		if _, err := io.ReadFull(r, header); err != nil {
			return err
		}
		if key == nil {
			return ErrNoEncryptionKey
		}
		size, err := binary.ReadVarint(&byteReader{r})
		if err != nil {
			return err
		}
		return crypto.Decrypt(r, w, size, key)
	})
}
