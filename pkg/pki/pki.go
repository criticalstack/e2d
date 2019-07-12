package pki

import (
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"time"

	"github.com/cloudflare/cfssl/cli/genkey"
	"github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/initca"
	clog "github.com/cloudflare/cfssl/log"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	ClientSigningProfile = "client"
	PeerSigningProfile   = "peer"
	ServerSigningProfile = "server"
)

var (
	SigningProfiles = &config.Signing{
		Default: &config.SigningProfile{
			Expiry: 5 * 365 * 24 * time.Hour,
		},
		Profiles: map[string]*config.SigningProfile{
			ClientSigningProfile: {
				Expiry: 5 * 365 * 24 * time.Hour,
				Usage: []string{
					"signing",
					"key encipherment",
					"client auth",
				},
			},
			PeerSigningProfile: {
				Expiry: 5 * 365 * 24 * time.Hour,
				Usage: []string{
					"signing",
					"key encipherment",
					"server auth",
					"client auth",
				},
			},
			ServerSigningProfile: {
				Expiry: 5 * 365 * 24 * time.Hour,
				Usage: []string{
					"signing",
					"key encipherment",
					"server auth",
				},
			},
		},
	}
)

type nopLogger struct {
}

func (nopLogger) Debug(msg string)   {}
func (nopLogger) Info(msg string)    {}
func (nopLogger) Warning(msg string) {}
func (nopLogger) Err(msg string)     {}
func (nopLogger) Crit(msg string)    {}
func (nopLogger) Emerg(msg string)   {}

type logger struct {
	l *zap.Logger
}

func (l *logger) Debug(msg string)   { l.l.Debug(msg) }
func (l *logger) Info(msg string)    { l.l.Info(msg) }
func (l *logger) Warning(msg string) { l.l.Warn(msg) }
func (l *logger) Err(msg string)     { l.l.Error(msg) }
func (l *logger) Crit(msg string)    { l.l.Error(msg) }
func (l *logger) Emerg(msg string)   { l.l.Fatal(msg) }

func init() {
	clog.SetLogger(&logger{log.NewLoggerWithLevel("cfssl", zapcore.ErrorLevel)})
}

type KeyPair struct {
	Cert    *x509.Certificate
	CertPEM []byte
	Key     crypto.Signer
	KeyPEM  []byte
}

func NewKeyPairFromPEM(certPEM, keyPEM []byte) (*KeyPair, error) {
	cert, err := helpers.ParseCertificatePEM(certPEM)
	if err != nil {
		return nil, err
	}
	key, err := helpers.ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		Cert:    cert,
		CertPEM: certPEM,
		Key:     key,
		KeyPEM:  keyPEM,
	}, nil
}

type RootCA struct {
	CA *KeyPair
	g  *csr.Generator
	sp *config.Signing
}

func NewRootCA(cr *csr.CertificateRequest) (*RootCA, error) {
	certPEM, _, keyPEM, err := initca.New(cr)
	if err != nil {
		return nil, err
	}
	ca, err := NewKeyPairFromPEM(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	r := &RootCA{
		CA: ca,
		g:  &csr.Generator{Validator: genkey.Validator},
		sp: SigningProfiles,
	}
	return r, nil
}

func NewRootCAFromFile(certpath, keypath string) (*RootCA, error) {
	certPEM, err := ioutil.ReadFile(certpath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := ioutil.ReadFile(keypath)
	if err != nil {
		return nil, err
	}
	ca, err := NewKeyPairFromPEM(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	r := &RootCA{
		CA: ca,
		g:  &csr.Generator{Validator: genkey.Validator},
		sp: SigningProfiles,
	}
	return r, nil
}

func NewDefaultRootCA() (*RootCA, error) {
	return NewRootCA(&csr.CertificateRequest{
		Names: []csr.Name{
			{
				C:  "US",
				ST: "Boston",
				L:  "MA",
				O:  "Critical Stack",
			},
		},
		KeyRequest: &csr.BasicKeyRequest{
			A: "rsa",
			S: 2048,
		},
		CN: "e2d-ca",
	})
}

func (r *RootCA) GenerateCertificates(profile string, cr *csr.CertificateRequest) (*KeyPair, error) {
	csrBytes, keyPEM, err := r.g.ProcessRequest(cr)
	if err != nil {
		return nil, err
	}
	s, err := local.NewSigner(r.CA.Key, r.CA.Cert, signer.DefaultSigAlgo(r.CA.Key), r.sp)
	if err != nil {
		return nil, err
	}
	certPEM, err := s.Sign(signer.SignRequest{
		Request: string(csrBytes),
		Profile: profile,
	})
	if err != nil {
		return nil, err
	}
	return NewKeyPairFromPEM(certPEM, keyPEM)
}

func GenerateCertHash(caCertPath string) ([]byte, error) {
	data, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("cannot parse PEM formatted block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return h[:], nil
}
