package pki

import (
	"fmt"
	"testing"

	"github.com/cloudflare/cfssl/csr"
)

func TestGenerateCertificates(t *testing.T) {
	hosts := []string{"10.10.0.1", "10.10.0.2"}

	r, err := NewDefaultRootCA()
	if err != nil {
		t.Fatal(err)
	}

	kp, err := r.GenerateCertificates(PeerSigningProfile, &csr.CertificateRequest{
		Names: []csr.Name{
			{
				C:  "US",
				ST: "Boston",
				L:  "MA",
			},
		},
		KeyRequest: &csr.KeyRequest{
			A: "rsa",
			S: 2048,
		},
		Hosts: hosts,
		CN:    "etcd peer",
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("kp.certPEM = %s\n", kp.CertPEM)
	fmt.Printf("kp.keyPEM = %s\n", kp.KeyPEM)
}
