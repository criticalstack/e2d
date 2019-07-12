package e2db

import (
	"bytes"
	"encoding/gob"
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
