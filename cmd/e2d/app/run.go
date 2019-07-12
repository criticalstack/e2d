package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/discovery"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
	"github.com/criticalstack/e2d/pkg/provider/aws"
	do "github.com/criticalstack/e2d/pkg/provider/digitalocean"
	"github.com/criticalstack/e2d/pkg/snapshot"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "start a managed etcd instance",
	Run: func(cmd *cobra.Command, args []string) {
		log.SetLevel(zapcore.DebugLevel)

		var peerProvider discovery.PeerProvider
		var err error
		switch strings.ToLower(viper.GetString("provider")) {
		case "aws":
			peerProvider, err = aws.NewClient(&aws.Config{})
			if err != nil {
				log.Fatal(err)
			}
		case "do", "digitalocean":
			log.Debug(viper.GetString("digitalocean-api-token"))
			peerProvider, err = do.NewClient(&do.Config{
				AccessToken: viper.GetString("digitalocean-api-token"),
			})
			if err != nil {
				log.Fatal(err)
			}
		default:
			peerProvider = &discovery.NoopProvider{}
		}

		baddrs := make([]string, 0)

		// user-provided bootstrap addresses take precedence
		if viper.GetString("bootstrap-addrs") != "" {
			baddrs = strings.Split(viper.GetString("bootstrap-addrs"), ",")
		}

		if viper.GetInt("required-cluster-size") > 1 && len(baddrs) == 0 {
			addrs, err := peerProvider.GetAddrs(context.Background())
			if err != nil {
				log.Fatal(err)
			}
			log.Debugf("cloud provided addresses: %v", addrs)
			for _, addr := range addrs {
				baddrs = append(baddrs, fmt.Sprintf("%s:%d", addr, manager.DefaultGossipPort))
			}
			log.Debugf("bootstrap addrs: %v", baddrs)
			if len(baddrs) == 0 {
				log.Fatal("bootstrap addresses must be provided")
			}
		}

		var snapshotProvider snapshot.SnapshotProvider
		snapURL := viper.GetString("snapshot-backup-url")
		if snapURL != "" {
			providerType, bucket := snapshot.ParseSnapshotBackupURL(snapURL)
			if providerType == "" {
				log.Fatalf("error parsing backup url %s (is it a full URL like s3://abc?)", snapURL)
			}

			switch providerType {
			case snapshot.FileProviderType:
				var err error
				snapshotProvider, err = snapshot.NewFileSnapshotter(bucket)
				if err != nil {
					log.Fatal(err)
				}
			case snapshot.S3ProviderType:
				var err error
				snapshotProvider, err = snapshot.NewAWSSnapshotter(&aws.Config{
					BucketName:      bucket,
					RoleSessionName: viper.GetString("aws-role-session-name"),
				})
				if err != nil {
					log.Fatal(err)
				}
			case snapshot.SpacesProviderType:
				u, err := url.Parse(snapURL)
				if err != nil {
					log.Fatal(err)
				}
				snapshotProvider, err = snapshot.NewDigitalOceanSnapshotter(&do.Config{
					SpacesURL:       u.Host,
					SpacesAccessKey: viper.GetString("digitalocean-spaces-key"),
					SpacesSecretKey: viper.GetString("digitalocean-spaces-secret"),
					SpaceName:       bucket,
				})
				if err != nil {
					log.Fatal(err)
				}
			default:
				log.Fatalf("unsupported snapshot url format: %#v", snapURL)
			}
		}

		m, err := manager.New(&manager.Config{
			Name:                viper.GetString("name"),
			Dir:                 viper.GetString("data-dir"),
			Host:                viper.GetString("host"),
			ClientAddr:          viper.GetString("client-addr"),
			PeerAddr:            viper.GetString("peer-addr"),
			GossipAddr:          viper.GetString("gossip-addr"),
			BootstrapAddrs:      baddrs,
			RequiredClusterSize: viper.GetInt("required-cluster-size"),
			CloudProvider:       viper.GetString("provider"),
			SnapshotInterval:    viper.GetDuration("snapshot-interval"),
			SnapshotCompression: viper.GetBool("snapshot-compression"),
			HealthCheckInterval: viper.GetDuration("healthcheck-interval"),
			HealthCheckTimeout:  viper.GetDuration("healthcheck-timeout"),
			ClientSecurity: client.SecurityConfig{
				CertFile:      viper.GetString("server-cert"),
				KeyFile:       viper.GetString("server-key"),
				TrustedCAFile: viper.GetString("ca-cert"),
			},
			PeerSecurity: client.SecurityConfig{
				CertFile:      viper.GetString("peer-cert"),
				KeyFile:       viper.GetString("peer-key"),
				TrustedCAFile: viper.GetString("ca-cert"),
			},
			PeerProvider:     peerProvider,
			SnapshotProvider: snapshotProvider,
			Debug:            viper.GetBool("verbose"),
		})
		if err != nil {
			log.Fatalf("%+v", err)
		}
		if err := m.Run(); err != nil {
			log.Fatalf("%+v", err)
		}
	},
}

func init() {
	runCmd.Flags().String("name", "", "specify a name for the node")
	runCmd.Flags().String("data-dir", "", "etcd data-dir")
	runCmd.Flags().String("ca-cert", "", "etcd trusted ca certificate")
	runCmd.Flags().String("server-cert", "", "etcd server certificate")
	runCmd.Flags().String("server-key", "", "etcd server private key")
	runCmd.Flags().String("peer-cert", "", "etcd peer certificate")
	runCmd.Flags().String("peer-key", "", "etcd peer private key")
	runCmd.Flags().String("host", "", "host IPv4")
	runCmd.Flags().String("client-addr", "0.0.0.0:2379", "etcd client addrress")
	runCmd.Flags().String("peer-addr", "0.0.0.0:2380", "etcd peer addrress")
	runCmd.Flags().String("gossip-addr", "0.0.0.0:7980", "gossip address")
	runCmd.Flags().String("bootstrap-addrs", "", "initial addresses used for node discovery")
	runCmd.Flags().IntP("required-cluster-size", "n", 1, "size of the etcd cluster should be {1,3,5}")
	runCmd.Flags().String("provider", "", "cloud provider")
	runCmd.Flags().String("digitalocean-api-token", "", "provider authentication token")
	runCmd.Flags().String("digitalocean-spaces-key", "", "provider authentication token")
	runCmd.Flags().String("digitalocean-spaces-secret", "", "provider authentication token")
	runCmd.Flags().Duration("snapshot-interval", 1*time.Minute, "frequency of etcd snapshots")
	runCmd.Flags().String("snapshot-backup-url", "", "an absolute path to shared filesystem storage (like file:///etcd-backups) or cloud storage bucket (like s3://etcd-backups) for snapshot backups")
	runCmd.Flags().Bool("snapshot-compression", false, "compression snapshots with gzip")
	runCmd.Flags().String("aws-role-session-name", "", "")
	runCmd.Flags().Duration("healthcheck-interval", 1*time.Minute, "")
	runCmd.Flags().Duration("healthcheck-timeout", 5*time.Minute, "")
	viper.BindPFlags(runCmd.Flags())
	viper.SetEnvPrefix("e2d")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
}
