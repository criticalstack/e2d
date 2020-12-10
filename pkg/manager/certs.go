package manager

import (
	"bytes"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"io"
	"io/ioutil"
	"net"
	"path/filepath"

	"github.com/criticalstack/crit/pkg/kubernetes/pki"
	"github.com/pkg/errors"

	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

func WriteNewCA(dir string) error {
	ca, err := pki.NewCertificateAuthority("ca", &pki.Config{
		CommonName: "etcd",
	})
	if err != nil {
		return err
	}
	return ca.WriteFiles(dir)
}

type CertificateAuthority struct {
	*pki.CertificateAuthority

	dir      string
	ips      []net.IP
	dnsnames []string
}

func LoadCertificateAuthority(cert, key string, names ...string) (*CertificateAuthority, error) {
	caCert, err := pki.ReadCertFromFile(cert)
	if err != nil {
		return nil, err
	}
	caKey, err := pki.ReadKeyFromFile(key)
	if err != nil {
		return nil, err
	}
	ca := &CertificateAuthority{
		CertificateAuthority: &pki.CertificateAuthority{
			KeyPair: &pki.KeyPair{
				Name: "ca",
				Cert: caCert,
				Key:  caKey,
			},
		},
		dir: filepath.Dir(cert),
	}
	if !contains(names, "127.0.0.1") {
		names = append(names, "127.0.0.1")
	}
	hostIP, err := netutil.DetectHostIPv4()
	if err != nil {
		return nil, err
	}
	if !contains(names, hostIP) {
		names = append(names, hostIP)
	}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			ca.ips = append(ca.ips, ip)
			continue
		}
		ca.dnsnames = append(ca.dnsnames, name)
	}
	return ca, nil
}

func (ca *CertificateAuthority) WriteAll() error {
	fns := []func() error{
		ca.WriteServerCertAndKey,
		ca.WritePeerCertAndKey,
		ca.WriteClientCertAndKey,
	}
	for _, fn := range fns {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func (ca *CertificateAuthority) WriteCA() error {
	return ca.WriteFiles(ca.dir)
}

func (ca *CertificateAuthority) WriteServerCertAndKey() error {
	server, err := ca.NewSignedKeyPair("server", &pki.Config{
		CommonName: "etcd-server",
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		AltNames: pki.AltNames{
			DNSNames: ca.dnsnames,
			IPs:      ca.ips,
		},
	})
	if err != nil {
		return err
	}
	return server.WriteFiles(ca.dir)
}

func (ca *CertificateAuthority) WritePeerCertAndKey() error {
	peer, err := ca.NewSignedKeyPair("peer", &pki.Config{
		CommonName: "etcd-peer",
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
		AltNames: pki.AltNames{
			DNSNames: ca.dnsnames,
			IPs:      ca.ips,
		},
	})
	if err != nil {
		return err
	}
	return peer.WriteFiles(ca.dir)
}

func (ca *CertificateAuthority) WriteClientCertAndKey() error {
	client, err := ca.NewSignedKeyPair("client", &pki.Config{
		CommonName: "etcd-client",
		Usages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
	})
	if err != nil {
		return err
	}
	return client.WriteFiles(ca.dir)
}

func contains(ss []string, match string) bool {
	for _, s := range ss {
		if s == match {
			return true
		}
	}
	return false
}

func ReadEncryptionKey(caKey string) (key [32]byte, err error) {
	data, err := ioutil.ReadFile(caKey)
	if err != nil {
		return key, err
	}
	block, _ := pem.Decode(data)
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		return key, errors.Wrapf(err, "cannot parse ca key file: %#v", caKey)
	}
	h := sha512.New512_256()
	if _, err := h.Write(block.Bytes); err != nil {
		return key, err
	}
	if _, err := io.ReadFull(bytes.NewReader(h.Sum(nil)), key[:]); err != nil {
		return key, err
	}
	return key, nil
}
