package e2db

import (
	"bytes"
	"encoding/gob"

	"github.com/criticalstack/e2d/pkg/e2db/crypto"
)

type Codec interface {
	Encode(interface{}) ([]byte, error)
	Decode([]byte, interface{}) error
}

type gobCodec struct{}

func (*gobCodec) Encode(iface interface{}) ([]byte, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(iface); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (*gobCodec) Decode(data []byte, iface interface{}) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(iface)
}

type encryptedGobCodec struct {
	key *[32]byte
}

func (c *encryptedGobCodec) Encode(iface interface{}) ([]byte, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(iface); err != nil {
		return nil, err
	}
	return crypto.Encrypt(b.Bytes(), c.key)
}

func (c *encryptedGobCodec) Decode(ciphertext []byte, iface interface{}) error {
	plaintext, err := crypto.Decrypt(ciphertext, c.key)
	if err != nil {
		return err
	}
	return gob.NewDecoder(bytes.NewReader(plaintext)).Decode(iface)
}
