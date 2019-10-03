package util

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"

	"github.com/criticalstack/e2d/pkg/snapshot/crypto"
	"github.com/google/go-cmp/cmp"
)

func TestSnapshotEncrypter(t *testing.T) {
	plaintext := []byte("testing")
	r := ioutil.NopCloser(bytes.NewReader(plaintext))

	key := crypto.NewEncryptionKey()
	enc := NewEncrypterReadCloser(r, key, int64(len(plaintext)))

	defer enc.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, enc); err != nil {
		t.Fatal(err)
	}

	r = ioutil.NopCloser(bytes.NewReader(out.Bytes()))

	dec := NewDecrypterReadCloser(r, key)
	defer dec.Close()

	out.Reset()

	if _, err := io.Copy(&out, dec); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(plaintext, out.Bytes()); diff != "" {
		t.Errorf("after Decrypt differs: (-want +got)\n%s", diff)
	}
}

func TestSnapshotEncrypterMessageAuth(t *testing.T) {
	plaintext := []byte("testing")
	r := ioutil.NopCloser(bytes.NewReader(plaintext))

	key := crypto.NewEncryptionKey()
	enc := NewEncrypterReadCloser(r, key, int64(len(plaintext)))
	defer enc.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, enc); err != nil {
		t.Fatal(err)
	}

	bad := out.Bytes()
	bad[20] = byte('B')
	bad[21] = byte('A')
	bad[22] = byte('D')

	r = ioutil.NopCloser(bytes.NewReader(bad))

	dec := NewDecrypterReadCloser(r, key)
	defer dec.Close()

	out.Reset()

	_, err := io.Copy(&out, dec)
	if err != crypto.ErrMessageAuthFailed {
		t.Fatalf("expected ErrMessageAuthFailed, received %v", err)
	}
}

func TestSnapshotGzipEncrypter(t *testing.T) {
	plaintext := []byte("testing")
	r := ioutil.NopCloser(bytes.NewReader(plaintext))

	key := crypto.NewEncryptionKey()
	enc := NewEncrypterReadCloser(r, key, int64(len(plaintext)))
	enc = NewGzipReadCloser(enc)

	defer enc.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, enc); err != nil {
		t.Fatal(err)
	}

	r = ioutil.NopCloser(bytes.NewReader(out.Bytes()))

	dec := NewGunzipReadCloser(r)
	dec = NewDecrypterReadCloser(dec, key)
	defer dec.Close()

	out.Reset()

	if _, err := io.Copy(&out, dec); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(plaintext, out.Bytes()); diff != "" {
		t.Errorf("after Decrypt differs: (-want +got)\n%s", diff)
	}
}
