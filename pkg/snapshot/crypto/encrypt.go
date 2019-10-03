package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"io"
)

// NewEncryptionKey generates a random 256-bit key for Encrypt() and
// Decrypt(). It panics if the source of randomness fails.
func NewEncryptionKey() *[32]byte {
	key := [32]byte{}
	_, err := io.ReadFull(rand.Reader, key[:])
	if err != nil {
		panic(err)
	}
	return &key
}

// NewRandomIV generates a random 128-bit IV for use with AES encryption. It
// panics if the source of randomness fails.
func NewRandomIV() []byte {
	iv := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic(err)
	}
	return iv
}

// Encrypt encrypts data using 256-bit AES-CTR and provides message
// authentication by signing the data with HMAC-512_256.
func Encrypt(in io.Reader, out io.Writer, key *[32]byte) error {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	iv := NewRandomIV()
	if _, err := out.Write(iv); err != nil {
		return err
	}
	s := cipher.StreamWriter{
		S: cipher.NewCTR(block, iv),
		W: out,
	}
	h := hmac.New(sha512.New512_256, key[:])
	if _, err := io.Copy(io.MultiWriter(s, h), in); err != nil {
		return err
	}
	_, err = out.Write(h.Sum(nil))
	return err
}

var ErrMessageAuthFailed = errors.New("message authentication failed")

// Decrypt decrypts data using 256-bit AES-CTR and provides message
// authentication verifying the HMAC-512_256 signature.
func Decrypt(in io.Reader, out io.Writer, size int64, key *[32]byte) error {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	iv := make([]byte, 16)
	if _, err := io.ReadFull(in, iv); err != nil {
		return err
	}
	s := cipher.StreamReader{
		S: cipher.NewCTR(block, iv),
		R: io.LimitReader(in, size),
	}
	h := hmac.New(sha512.New512_256, key[:])
	if _, err := io.Copy(io.MultiWriter(out, h), s); err != nil {
		return err
	}
	sig := make([]byte, 32)
	if _, err := io.ReadFull(in, sig); err != nil {
		return err
	}
	if !hmac.Equal(h.Sum(nil), sig) {
		return ErrMessageAuthFailed
	}
	return nil
}
