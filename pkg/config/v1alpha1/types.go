package v1alpha1

import (
	"encoding/json"
	"fmt"
	"net"

	fmtutil "github.com/criticalstack/crit/pkg/util/fmt"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/criticalstack/e2d/pkg/discovery"
	"github.com/criticalstack/e2d/pkg/log"
	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

const (
	DefaultClientPort  = 2379
	DefaultPeerPort    = 2380
	DefaultGossipPort  = 7980
	DefaultMetricsPort = 4001
)

func init() {
	_ = AddToScheme(clientsetscheme.Scheme)
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Configuration struct {
	metav1.TypeMeta `json:",inline"`

	// directory used for etcd data-dir, wal and snapshot dirs derived from
	// this by etcd
	DataDir string `json:"dataDir"`

	// the required number of nodes that must be present to start a cluster
	RequiredClusterSize int `json:"requiredClusterSize"`

	// client endpoint for accessing etcd
	ClientAddr APIEndpoint `json:"clientAddr"`

	// address used for traffic within the cluster
	PeerAddr APIEndpoint `json:"peerAddr"`

	// address used for gossip network
	GossipAddr APIEndpoint `json:"gossipAddr"`

	// CA certificate file location
	// +optional
	CACert string `json:"caCert"`

	// CA private key file location
	// +optional
	CAKey string `json:"caKey"`

	// how often to perform a health check
	HealthCheckInterval metav1.Duration `json:"healthCheckInterval"`

	// time until an unreachable member is considered unhealthy
	HealthCheckTimeout metav1.Duration `json:"healthCheckTimeout"`

	// DiscoveryConfiguration provides configuration for discovery of e2d
	// peers.
	DiscoveryConfiguration DiscoveryConfiguration `json:"discovery"`
	// SnapshotConfiguration provides configuration for periodic snapshot
	// backups of etcd.
	SnapshotConfiguration SnapshotConfiguration `json:"snapshot"`
	// MetricsConfiguration provides configuration for serving prometheus
	// metrics.
	MetricsConfiguration MetricsConfiguration `json:"metrics"`

	// name used for etcd.Embed instance, should generally be left alone so
	// that a random name is generated
	// +optional
	OverrideName string `json:"overrideName"`

	// Default: error
	// +optional
	EtcdLogLevel string

	// Default: error
	// +optional
	MemberlistLogLevel string

	// +optional
	CompactionInterval metav1.Duration `json:"compactionInterval"`
	// UnsafeNoFsync disables all uses of fsync. Setting this is unsafe and
	// will cause data loss.
	// +optional
	UnsafeNoFsync bool `json:"unsafeNoFsync"`

	// +optional
	DisablePreVote bool `json:"DisablePreVote"`
}

func (cfg *Configuration) Validate() error {
	SetDefaults_Configuration(cfg)

	switch cfg.RequiredClusterSize {
	case 1, 3, 5:
	default:
		return errors.New("value of RequiredClusterSize must be 1, 3, or 5")
	}
	for i, p := range cfg.DiscoveryConfiguration.InitialPeers {
		addr, err := netutil.FixUnspecifiedHostAddr(p)
		if err != nil {
			return errors.Wrapf(err, "cannot determine ipv4 address from host string: %#v", p)
		}
		cfg.DiscoveryConfiguration.InitialPeers[i] = addr
	}
	if len(cfg.DiscoveryConfiguration.InitialPeers) == 0 && cfg.RequiredClusterSize > 1 {
		return errors.New("must provide at least 1 initial peer when not a single-host cluster")
	}

	if cfg.SnapshotConfiguration.Encryption && cfg.CAKey == "" {
		return errors.New("must provide ca key for snapshot encryption")
	}
	host, err := netutil.DetectHostIPv4()
	if err != nil {
		return err
	}

	if cfg.ClientAddr.IsUnspecified() {
		cfg.ClientAddr.Host = host
	}
	if cfg.ClientAddr.Port == 0 {
		cfg.ClientAddr.Port = DefaultClientPort
	}
	if cfg.PeerAddr.IsUnspecified() {
		cfg.PeerAddr.Host = host
	}
	if cfg.PeerAddr.Port == 0 {
		cfg.PeerAddr.Port = DefaultPeerPort
	}
	if cfg.GossipAddr.IsUnspecified() {
		cfg.GossipAddr.Host = host
	}
	if cfg.GossipAddr.Port == 0 {
		cfg.GossipAddr.Port = DefaultGossipPort
	}
	if !cfg.MetricsConfiguration.Addr.IsZero() {
		if cfg.MetricsConfiguration.Addr.Host == "" {
			cfg.MetricsConfiguration.Addr.Host = "0.0.0.0"
		}
		if cfg.MetricsConfiguration.Addr.IsUnspecified() {
			cfg.MetricsConfiguration.Addr.Host = host
		}
		if cfg.MetricsConfiguration.Addr.Port == 0 {
			cfg.MetricsConfiguration.Addr.Port = DefaultMetricsPort
		}
	}

	return nil
}

type DiscoveryType string

const (
	AmazonAutoscalingGroup DiscoveryType = "aws/autoscaling-group"
	AmazonTags             DiscoveryType = "aws/tags"
	DigitalOceanTags       DiscoveryType = "digitalocean/tags"
)

type DiscoveryConfiguration struct {
	// initial set of addresses used to bootstrap the gossip network
	// +optional
	InitialPeers []string `json:"initialPeers"`

	// amount of time to attempt bootstrapping before failing
	BootstrapTimeout metav1.Duration `json:"bootstrapTimeout"`

	// +optional
	Type DiscoveryType `json:"type"`

	// +optional
	ExtraArgs map[string]string `json:"extraArgs"`
}

func (d *DiscoveryConfiguration) Setup() (discovery.PeerGetter, error) {
	kvs := make([]discovery.KeyValue, 0)
	for k, v := range d.ExtraArgs {
		kvs = append(kvs, discovery.KeyValue{Key: k, Value: v})
	}
	switch d.Type {
	case AmazonAutoscalingGroup:
		return discovery.NewAmazonAutoScalingPeerGetter()
	case AmazonTags:
		return discovery.NewAmazonInstanceTagPeerGetter(kvs)
	case DigitalOceanTags:
		if len(kvs) == 0 {
			return nil, errors.New("must provide at least 1 tag")
		}
		docfg := &discovery.DigitalOceanConfig{
			TagValue: kvs[0].Key,
		}
		return discovery.NewDigitalOceanPeerGetter(docfg)
		//case "k8s-labels":
		//return nil, errors.New("peer getter not yet implemented")
	}
	return &discovery.NoopGetter{}, nil
}

type SnapshotConfiguration struct {
	// use gzip compression for snapshot backup
	// +optional
	Compression bool `json:"compression"`

	// use aes-256 encryption for snapshot backup
	// +optional
	Encryption bool `json:"encryption"`

	// interval for creating etcd snapshots
	Interval metav1.Duration `json:"interval"`

	// +optional
	File string `json:"file"`

	// +optional
	ExtraArgs map[string]string `json:"extraArgs"`
}

// APIEndpoint represents a reachable endpoint using scheme-less host:port
// (port is optional).
type APIEndpoint struct {
	// The IP or DNS name portion of an address.
	Host string `json:"host"`

	// The port part of an address (optional).
	Port int32 `json:"port"`
}

// String returns a formatted version HOST:PORT of this APIEndpoint.
func (v APIEndpoint) String() string {
	return fmt.Sprintf("%s:%d", v.Host, v.Port)
}

// IsZero returns true if host and the port are zero values.
func (v APIEndpoint) IsZero() bool {
	return v.Host == "" && v.Port == 0
}

func (v APIEndpoint) IsUnspecified() bool {
	return net.ParseIP(v.Host).IsUnspecified()
}

func (v *APIEndpoint) UnmarshalJSON(data []byte) error {
	type alias APIEndpoint
	if reterr := json.Unmarshal(data, (*alias)(v)); reterr != nil {
		if err := v.tryUnmarshalText(fmtutil.Unquote(string(data))); err != nil {
			log.Debug("tryUnmarshalText", zap.Error(err))
			return reterr
		}
	}
	return nil
}

func (v *APIEndpoint) tryUnmarshalText(s string) (err error) {
	host, port, err := netutil.SplitHostPort(s)
	v.Host = host
	v.Port = int32(port)
	return err
}

type MetricsType string

const (
	MetricsBasic     MetricsType = "basic"
	MetricsExtensive MetricsType = "extensive"
)

type MetricsConfiguration struct {
	// Addr is the addressed used to serve prometheus metrics.
	// +optional
	Addr APIEndpoint `json:"addr"`
	// Type is used to specify the type of metrics to be served. It can
	// currently be only "basic" or "extensive".
	// Default: "basic
	// +optional
	Type MetricsType `json:"type"`
	// DisableAuth disables the usage of TLS authentication for the metrics
	// endpoint. By default, the usage of TLS is determined if CACert and CAKey
	// were provided, and if so the client derived security is used.
	// Default: false
	// +optional
	DisableAuth bool `json:"disableAuth"`
}
