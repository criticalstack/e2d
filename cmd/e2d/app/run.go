package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/cmdutil"
	"github.com/criticalstack/e2d/pkg/discovery"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
	"github.com/criticalstack/e2d/pkg/snapshot"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type runOptions struct {
	Name       string `env:"E2D_NAME"`
	DataDir    string `env:"E2D_DATA_DIR"`
	Host       string `env:"E2D_HOST"`
	ClientAddr string `env:"E2D_CLIENT_ADDR"`
	PeerAddr   string `env:"E2D_PEER_ADDR"`
	GossipAddr string `env:"E2D_GOSSIP_ADDR"`

	CACert     string `env:"E2D_CA_CERT"`
	CAKey      string `env:"E2D_CA_KEY"`
	PeerCert   string `env:"E2D_PEER_CERT"`
	PeerKey    string `env:"E2D_PEER_KEY"`
	ServerCert string `env:"E2D_SERVER_CERT"`
	ServerKey  string `env:"E2D_SERVER_KEY"`

	BootstrapAddrs      string `env:"E2D_BOOTSTRAP_ADDRS"`
	RequiredClusterSize int    `env:"E2D_REQUIRED_CLUSTER_SIZE"`

	HealthCheckInterval time.Duration `env:"E2D_HEALTH_CHECK_INTERVAL"`
	HealthCheckTimeout  time.Duration `env:"E2D_HEALTH_CHECK_TIMEOUT"`

	PeerDiscovery string `env:"E2D_PEER_DISCOVERY"`

	SnapshotBackupURL   string        `env:"E2D_SNAPSHOT_BACKUP_URL"`
	SnapshotCompression bool          `env:"E2D_SNAPSHOT_COMPRESSION"`
	SnapshotEncryption  bool          `env:"E2D_SNAPSHOT_ENCRYPTION"`
	SnapshotInterval    time.Duration `env:"E2D_SNAPSHOT_INTERVAL"`

	AWSAccessKey       string `env:"E2D_AWS_ACCESS_KEY"`
	AWSSecretKey       string `env:"E2D_AWS_SECRET_KEY"`
	AWSRoleSessionName string `env:"E2D_AWS_ROLE_SESSION_NAME"`

	DOAccessToken  string `env:"E2D_DO_ACCESS_TOKEN"`
	DOSpacesKey    string `env:"E2D_DO_SPACES_KEY"`
	DOSpacesSecret string `env:"E2D_DO_SPACES_SECRET"`
}

func newRunCmd() *cobra.Command {
	o := &runOptions{}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "start a managed etcd instance",
		Run: func(cmd *cobra.Command, args []string) {
			peerGetter, err := getPeerGetter(o)
			if err != nil {
				log.Fatalf("%+v", err)
			}

			baddrs, err := getInitialBootstrapAddrs(o, peerGetter)
			if err != nil {
				log.Fatalf("%+v", err)
			}

			snapshotter, err := getSnapshotProvider(o)
			if err != nil {
				log.Fatalf("%+v", err)
			}

			m, err := manager.New(&manager.Config{
				Name:                o.Name,
				Dir:                 o.DataDir,
				Host:                o.Host,
				ClientAddr:          o.ClientAddr,
				PeerAddr:            o.PeerAddr,
				GossipAddr:          o.GossipAddr,
				BootstrapAddrs:      baddrs,
				RequiredClusterSize: o.RequiredClusterSize,
				SnapshotInterval:    o.SnapshotInterval,
				SnapshotCompression: o.SnapshotCompression,
				SnapshotEncryption:  o.SnapshotEncryption,
				HealthCheckInterval: o.HealthCheckInterval,
				HealthCheckTimeout:  o.HealthCheckTimeout,
				ClientSecurity: client.SecurityConfig{
					CertFile:      o.ServerCert,
					KeyFile:       o.ServerKey,
					TrustedCAFile: o.CACert,
				},
				PeerSecurity: client.SecurityConfig{
					CertFile:      o.PeerCert,
					KeyFile:       o.PeerKey,
					TrustedCAFile: o.CACert,
				},
				CACertFile:  o.CACert,
				CAKeyFile:   o.CAKey,
				PeerGetter:  peerGetter,
				Snapshotter: snapshotter,
				Debug:       globalOptions.verbose,
			})
			if err != nil {
				log.Fatalf("%+v", err)
			}
			if err := m.Run(); err != nil {
				log.Fatalf("%+v", err)
			}
		},
	}

	cmd.Flags().StringVar(&o.Name, "name", "", "specify a name for the node")
	cmd.Flags().StringVar(&o.DataDir, "data-dir", "", "etcd data-dir")
	cmd.Flags().StringVar(&o.Host, "host", "", "host IPv4 (defaults to 127.0.0.1 if unset)")
	cmd.Flags().StringVar(&o.ClientAddr, "client-addr", "0.0.0.0:2379", "etcd client addrress")
	cmd.Flags().StringVar(&o.PeerAddr, "peer-addr", "0.0.0.0:2380", "etcd peer addrress")
	cmd.Flags().StringVar(&o.GossipAddr, "gossip-addr", "0.0.0.0:7980", "gossip address")

	cmd.Flags().StringVar(&o.CACert, "ca-cert", "", "etcd trusted ca certificate")
	cmd.Flags().StringVar(&o.CAKey, "ca-key", "", "etcd ca key")
	cmd.Flags().StringVar(&o.PeerCert, "peer-cert", "", "etcd peer certificate")
	cmd.Flags().StringVar(&o.PeerKey, "peer-key", "", "etcd peer private key")
	cmd.Flags().StringVar(&o.ServerCert, "server-cert", "", "etcd server certificate")
	cmd.Flags().StringVar(&o.ServerKey, "server-key", "", "etcd server private key")

	cmd.Flags().StringVar(&o.BootstrapAddrs, "bootstrap-addrs", "", "initial addresses used for node discovery")
	cmd.Flags().IntVarP(&o.RequiredClusterSize, "required-cluster-size", "n", 1, "size of the etcd cluster should be {1,3,5}")

	cmd.Flags().DurationVar(&o.HealthCheckInterval, "health-check-interval", 1*time.Minute, "")
	cmd.Flags().DurationVar(&o.HealthCheckTimeout, "health-check-timeout", 5*time.Minute, "")

	cmd.Flags().StringVar(&o.PeerDiscovery, "peer-discovery", "", "which method {aws-autoscaling-group,ec2-tags,do-tags} to use to discover peers")

	cmd.Flags().DurationVar(&o.SnapshotInterval, "snapshot-interval", 1*time.Minute, "frequency of etcd snapshots")
	cmd.Flags().StringVar(&o.SnapshotBackupURL, "snapshot-backup-url", "", "an absolute path to shared filesystem storage (like file:///etcd-backups) or cloud storage bucket (like s3://etcd-backups) for snapshot backups")
	cmd.Flags().BoolVar(&o.SnapshotCompression, "snapshot-compression", false, "compression snapshots with gzip")
	cmd.Flags().BoolVar(&o.SnapshotEncryption, "snapshot-encryption", false, "encrypt snapshots with aes-256")

	cmd.Flags().StringVar(&o.AWSAccessKey, "aws-access-key", "", "")
	cmd.Flags().StringVar(&o.AWSSecretKey, "aws-secret-key", "", "")
	cmd.Flags().StringVar(&o.AWSRoleSessionName, "aws-role-session-name", "", "")

	cmd.Flags().StringVar(&o.DOAccessToken, "do-access-token", "", "DigitalOcean personal access token")
	cmd.Flags().StringVar(&o.DOSpacesKey, "do-spaces-key", "", "DigitalOcean spaces access key")
	cmd.Flags().StringVar(&o.DOSpacesSecret, "do-spaces-secret", "", "DigitalOcean spaces secret")
	if err := cmdutil.SetEnvs(o); err != nil {
		log.Debug("cannot set environment variables", zap.Error(err))
	}

	return cmd
}

func parsePeerDiscovery(s string) (string, []discovery.KeyValue) {
	kvs := make([]discovery.KeyValue, 0)
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return s, kvs
	}
	pairs := strings.Split(parts[1], ",")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		switch len(parts) {
		case 1:
			kvs = append(kvs, discovery.KeyValue{Key: parts[0], Value: ""})
		case 2:
			kvs = append(kvs, discovery.KeyValue{Key: parts[0], Value: parts[1]})
		}
	}
	return parts[0], kvs
}

func getPeerGetter(o *runOptions) (discovery.PeerGetter, error) {
	method, kvs := parsePeerDiscovery(o.PeerDiscovery)
	log.Info("peer-discovery", zap.String("method", method), zap.String("kvs", fmt.Sprintf("%v", kvs)))
	switch strings.ToLower(method) {
	case "aws-autoscaling-group":
		// TODO(chris): needs to take access key/secret
		return discovery.NewAmazonAutoScalingPeerGetter()
	case "ec2-tags":
		return discovery.NewAmazonInstanceTagPeerGetter(kvs)
	case "do-tags":
		if len(kvs) == 0 {
			return nil, errors.New("must provide at least 1 tag")
		}
		return discovery.NewDigitalOceanPeerGetter(&discovery.DigitalOceanConfig{
			AccessToken: o.DOAccessToken,
			TagValue:    kvs[0].Key,
		})
	case "k8s-labels":
		return nil, errors.New("peer getter not yet implemented")
	}
	return &discovery.NoopGetter{}, nil
}

func getInitialBootstrapAddrs(o *runOptions, peerGetter discovery.PeerGetter) ([]string, error) {
	baddrs := make([]string, 0)

	// user-provided bootstrap addresses take precedence
	if o.BootstrapAddrs != "" {
		baddrs = strings.Split(o.BootstrapAddrs, ",")
	}

	if o.RequiredClusterSize > 1 && len(baddrs) == 0 {
		addrs, err := peerGetter.GetAddrs(context.Background())
		if err != nil {
			return nil, err
		}
		log.Debugf("cloud provided addresses: %v", addrs)
		for _, addr := range addrs {
			baddrs = append(baddrs, fmt.Sprintf("%s:%d", addr, manager.DefaultGossipPort))
		}
		log.Debugf("bootstrap addrs: %v", baddrs)
		if len(baddrs) == 0 {
			return nil, errors.Errorf("bootstrap addresses must be provided")
		}
	}
	return baddrs, nil
}

func getSnapshotProvider(o *runOptions) (snapshot.Snapshotter, error) {
	if o.SnapshotBackupURL == "" {
		return nil, nil
	}
	u, err := snapshot.ParseSnapshotBackupURL(o.SnapshotBackupURL)
	if err != nil {
		return nil, err
	}

	switch u.Type {
	case snapshot.FileType:
		return snapshot.NewFileSnapshotter(u.Path)
	case snapshot.S3Type:
		return snapshot.NewAmazonSnapshotter(&snapshot.AmazonConfig{
			RoleSessionName: o.AWSRoleSessionName,
			Bucket:          u.Bucket,
			Key:             u.Path,
		})
	case snapshot.SpacesType:
		return snapshot.NewDigitalOceanSnapshotter(&snapshot.DigitalOceanConfig{
			SpacesURL:       o.SnapshotBackupURL,
			SpacesAccessKey: o.DOSpacesKey,
			SpacesSecretKey: o.DOSpacesSecret,
		})
	default:
		return nil, errors.Errorf("unsupported snapshot url format: %#v", o.SnapshotBackupURL)
	}
}
