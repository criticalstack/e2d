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
	"github.com/spf13/viper"
)

var pkiCmd = &cobra.Command{
	Use:   "pki",
	Short: "manage e2d pki",
}

var pkiInitCmd = &cobra.Command{
	Use:   "init",
	Short: "initialize a new CA",
	Run: func(cmd *cobra.Command, args []string) {
		path := filepath.Dir(viper.GetString("pki-ca-cert"))
		if path != "" {
			if err := os.MkdirAll(path, 0755); err != nil && !os.IsExist(err) {
				log.Fatal(err)
			}
		}
		r, err := pki.NewDefaultRootCA()
		if err != nil {
			log.Fatal(err)
		}
		if err := writeFile(viper.GetString("pki-ca-cert"), r.CA.CertPEM, 0644); err != nil {
			log.Fatal(err)
		}
		if err := writeFile(viper.GetString("pki-ca-key"), r.CA.KeyPEM, 0600); err != nil {
			log.Fatal(err)
		}
	},
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
		KeyRequest: &csr.BasicKeyRequest{
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

var pkiGenCertsCmd = &cobra.Command{
	Use:   "gencerts",
	Short: "generate certificates/private keys",
	Run: func(cmd *cobra.Command, args []string) {
		var hosts []string
		if viper.GetString("hosts") != "" {
			hosts = strings.Split(viper.GetString("hosts"), ",")
		}
		r, err := pki.NewRootCAFromFile(viper.GetString("pki-ca-cert"), viper.GetString("pki-ca-key"))
		if err != nil {
			log.Fatal(err)
		}
		hostIP, err := netutil.DetectHostIPv4()
		if err != nil {
			log.Fatal(err)
		}
		if viper.GetString("output-dir") != "" {
			if err := os.MkdirAll(viper.GetString("output-dir"), 0755); err != nil && !os.IsExist(err) {
				log.Fatal(err)
			}
		}
		hosts = append([]string{"127.0.0.1", hostIP}, hosts...)
		certs, err := r.GenerateCertificates(pki.ServerSigningProfile, newCertificateRequest("etcd server", hosts...))
		if err != nil {
			log.Fatal(err)
		}
		if err := writeFile(filepath.Join(viper.GetString("output-dir"), "server.crt"), certs.CertPEM, 0644); err != nil {
			log.Fatal(err)
		}
		if err := writeFile(filepath.Join(viper.GetString("output-dir"), "server.key"), certs.KeyPEM, 0600); err != nil {
			log.Fatal(err)
		}
		certs, err = r.GenerateCertificates(pki.PeerSigningProfile, newCertificateRequest("etcd peer", hosts...))
		if err != nil {
			log.Fatal(err)
		}
		if err := writeFile(filepath.Join(viper.GetString("output-dir"), "peer.crt"), certs.CertPEM, 0644); err != nil {
			log.Fatal(err)
		}
		if err := writeFile(filepath.Join(viper.GetString("output-dir"), "peer.key"), certs.KeyPEM, 0600); err != nil {
			log.Fatal(err)
		}
		certs, err = r.GenerateCertificates(pki.ClientSigningProfile, newCertificateRequest("etcd client"))
		if err != nil {
			log.Fatal(err)
		}
		if err := writeFile(filepath.Join(viper.GetString("output-dir"), "client.crt"), certs.CertPEM, 0644); err != nil {
			log.Fatal(err)
		}
		if err := writeFile(filepath.Join(viper.GetString("output-dir"), "client.key"), certs.KeyPEM, 0600); err != nil {
			log.Fatal(err)
		}
		log.Info("generated certificates successfully.")
	},
}

func init() {
	pkiCmd.PersistentFlags().String("ca-cert", "", "")
	pkiCmd.PersistentFlags().String("ca-key", "", "")
	viper.BindPFlag("pki-ca-cert", pkiCmd.PersistentFlags().Lookup("ca-cert"))
	viper.BindPFlag("pki-ca-key", pkiCmd.PersistentFlags().Lookup("ca-key"))

	pkiGenCertsCmd.Flags().String("hosts", "", "")
	pkiGenCertsCmd.Flags().String("output-dir", "", "")
	viper.BindPFlag("hosts", pkiGenCertsCmd.Flags().Lookup("hosts"))
	viper.BindPFlag("output-dir", pkiGenCertsCmd.Flags().Lookup("output-dir"))

	pkiCmd.AddCommand(pkiInitCmd, pkiGenCertsCmd)
}
