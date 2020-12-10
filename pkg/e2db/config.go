package e2db

import (
	"bytes"
	"crypto/sha512"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/criticalstack/e2d/pkg/client"
	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

type Config struct {
	ClientAddr       string
	CertFile         string
	KeyFile          string
	CAFile           string
	Namespace        string
	Timeout          time.Duration
	AutoSyncInterval time.Duration
	SecretKey        []byte

	clientURL      url.URL
	key            *[32]byte
	securityConfig client.SecurityConfig
}

func (c *Config) validate() error {
	var err error
	c.ClientAddr, err = netutil.FixUnspecifiedHostAddr(c.ClientAddr)
	if err != nil {
		return err
	}
	if c.CertFile != "" || c.KeyFile != "" || c.CAFile != "" {
		if c.CertFile == "" || c.KeyFile == "" || c.CAFile == "" {
			return errors.New("must provide all values for mTLS configuration (CertFile,KeyFile,CAFile)")
		}
		c.securityConfig = client.SecurityConfig{
			CertFile:      c.CertFile,
			KeyFile:       c.KeyFile,
			TrustedCAFile: c.CAFile,
			CertAuth:      true,
		}
	}
	if len(c.SecretKey) != 0 {
		h := sha512.New512_256()
		if _, err := h.Write(c.SecretKey); err != nil {
			return err
		}
		key := [32]byte{}
		if _, err := io.ReadFull(bytes.NewReader(h.Sum(nil)), key[:]); err != nil {
			return err
		}
		c.key = &key
	}
	caddr, err := netutil.ParseAddr(c.ClientAddr)
	if err != nil {
		return err
	}
	if caddr.IsUnspecified() {
		caddr.Host, err = netutil.DetectHostIPv4()
		if err != nil {
			return err
		}
	}
	if caddr.Port == 0 {
		caddr.Port = 2379
	}
	c.ClientAddr = caddr.String()
	c.clientURL = url.URL{Scheme: c.securityConfig.Scheme(), Host: c.ClientAddr}
	c.Namespace = strings.Trim(c.Namespace, "/")
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	return nil
}
