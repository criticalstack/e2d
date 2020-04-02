package app

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudflare/cfssl/csr"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/netutil"
	"github.com/criticalstack/e2d/pkg/pki"
	"github.com/spf13/cobra"
)

type pkiOptions struct {
	CACert string
	CAKey  string
}

func newPKICmd() *cobra.Command {
	o := &pkiOptions{}

	cmd := &cobra.Command{
		Use:   "pki",
		Short: "manage e2d pki",
	}

	cmd.PersistentFlags().StringVar(&o.CACert, "ca-cert", "", "")
	cmd.PersistentFlags().StringVar(&o.CAKey, "ca-key", "", "")

	cmd.AddCommand(
		newPKIInitCmd(o),
		newPKIGenCertsCmd(o),
	)
	return cmd
}

func newPKIInitCmd(pkiOpts *pkiOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "initialize a new CA",
		Run: func(cmd *cobra.Command, args []string) {
			path := filepath.Dir(pkiOpts.CACert)
			if path != "" {
				if err := os.MkdirAll(path, 0755); err != nil && !os.IsExist(err) {
					log.Fatal(err)
				}
			}
			r, err := pki.NewDefaultRootCA()
			if err != nil {
				log.Fatal(err)
			}
			if err := writeFile(pkiOpts.CACert, r.CA.CertPEM, 0644); err != nil {
				log.Fatal(err)
			}
			if err := writeFile(pkiOpts.CAKey, r.CA.KeyPEM, 0600); err != nil {
				log.Fatal(err)
			}
		},
	}
	return cmd
}

type pkiGenCertsOptions struct {
	Hosts     string
	OutputDir string
}

func newPKIGenCertsCmd(pkiOpts *pkiOptions) *cobra.Command {
	o := pkiGenCertsOptions{}

	cmd := &cobra.Command{
		Use:   "gencerts",
		Short: "generate certificates/private keys",
		Run: func(cmd *cobra.Command, args []string) {
			var hosts []string
			if o.Hosts != "" {
				hosts = strings.Split(o.Hosts, ",")
			}
			r, err := pki.NewRootCAFromFile(pkiOpts.CACert, pkiOpts.CAKey)
			if err != nil {
				log.Fatal(err)
			}
			hostIP, err := netutil.DetectHostIPv4()
			if err != nil {
				log.Fatal(err)
			}
			if o.OutputDir != "" {
				if err := os.MkdirAll(o.OutputDir, 0755); err != nil && !os.IsExist(err) {
					log.Fatal(err)
				}
			}
			hosts = appendHosts(hosts, "127.0.0.1", hostIP)
			certs, err := r.GenerateCertificates(pki.ServerSigningProfile, newCertificateRequest("etcd server", hosts...))
			if err != nil {
				log.Fatal(err)
			}
			if err := writeFile(filepath.Join(o.OutputDir, "server.crt"), certs.CertPEM, 0644); err != nil {
				log.Fatal(err)
			}
			if err := writeFile(filepath.Join(o.OutputDir, "server.key"), certs.KeyPEM, 0600); err != nil {
				log.Fatal(err)
			}
			certs, err = r.GenerateCertificates(pki.PeerSigningProfile, newCertificateRequest("etcd peer", hosts...))
			if err != nil {
				log.Fatal(err)
			}
			if err := writeFile(filepath.Join(o.OutputDir, "peer.crt"), certs.CertPEM, 0644); err != nil {
				log.Fatal(err)
			}
			if err := writeFile(filepath.Join(o.OutputDir, "peer.key"), certs.KeyPEM, 0600); err != nil {
				log.Fatal(err)
			}
			certs, err = r.GenerateCertificates(pki.ClientSigningProfile, newCertificateRequest("etcd client"))
			if err != nil {
				log.Fatal(err)
			}
			if err := writeFile(filepath.Join(o.OutputDir, "client.crt"), certs.CertPEM, 0644); err != nil {
				log.Fatal(err)
			}
			if err := writeFile(filepath.Join(o.OutputDir, "client.key"), certs.KeyPEM, 0600); err != nil {
				log.Fatal(err)
			}
			log.Info("generated certificates successfully.")
		},
	}

	cmd.Flags().StringVar(&o.Hosts, "hosts", "", "")
	cmd.Flags().StringVar(&o.OutputDir, "output-dir", "", "")

	return cmd
}

func appendHosts(hosts []string, newHosts ...string) []string {
	for _, newHost := range newHosts {
		if newHost == "" {
			continue
		}
		for _, host := range hosts {
			if newHost == host {
				continue
			}
		}
		hosts = append(hosts, newHost)
	}
	return hosts
}

func newCertificateRequest(commonName string, hosts ...string) *csr.CertificateRequest {
	return &csr.CertificateRequest{
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
		CN:    commonName,
	}
}

func writeFile(filename string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(filename, data, perm)
}
