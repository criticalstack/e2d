package manager

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/discovery"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/netutil"
	"github.com/criticalstack/e2d/pkg/pki"
	"github.com/criticalstack/e2d/pkg/snapshot"
	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Config struct {
	// name used for etcd.Embed instance, should generally be left alone so
	// that a random name is generated
	Name string

	// directory used for etcd data-dir, wal and snapshot dirs derived from
	// this by etcd
	Dir string

	// the required number of nodes that must be present to start a cluster
	RequiredClusterSize int

	// allows for explicit setting of the host ip
	Host string

	// client endpoint for accessing etcd
	ClientAddr string

	// client url created based upon the client address and use of TLS
	ClientURL url.URL

	// address used for traffic within the cluster
	PeerAddr string

	// peer url created based upon the peer address and use of TLS
	PeerURL url.URL

	// address used for gossip network
	GossipAddr string

	// host used for gossip network, derived from GossipAddr
	GossipHost string

	// port used for gossip network, derived from GossipAddr
	GossipPort int

	// key used to secure  the gossip network, derived from PeerSecurity settings
	GossipSecretKey []byte

	// addresses used to bootstrap the gossip network
	BootstrapAddrs []string

	// amount of time to attempt bootstrapping before failing
	BootstrapTimeout time.Duration

	// identifies a cloud provider to be used for peer discovery
	CloudProvider string `mapstructure:"cloud_provider"`

	// interval for creating etcd snapshots
	SnapshotInterval time.Duration

	// use gzip compression for snapshot backup
	SnapshotCompression bool

	// how often to perform a health check
	HealthCheckInterval time.Duration

	// time until an unreachable member is considered unhealthy
	HealthCheckTimeout time.Duration

	// configures authentication/transport security for clients
	ClientSecurity client.SecurityConfig

	// configures authentication/transport security within the etcd cluster
	PeerSecurity client.SecurityConfig

	// configures the level of the logger used by etcd
	EtcdLogLevel zapcore.Level

	discovery.PeerProvider
	snapshot.SnapshotProvider

	Debug bool
}

func (c *Config) validate() error {
	if c.Dir == "" {
		c.Dir = "data"
	}
	if c.SnapshotInterval == 0 {
		c.SnapshotInterval = 1 * time.Minute
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 1 * time.Minute
	}
	if c.HealthCheckTimeout == 0 {
		c.HealthCheckTimeout = 5 * time.Minute
	}
	if c.BootstrapTimeout == 0 {
		c.BootstrapTimeout = 30 * time.Minute
	}
	for i, baddr := range c.BootstrapAddrs {
		addr, err := netutil.FixUnspecifiedHostAddr(baddr)
		if err != nil {
			return errors.Wrapf(err, "cannot determine ipv4 address from host string: %#v", baddr)
		}
		c.BootstrapAddrs[i] = addr
	}

	// If the host is not set the IPv4 of the first non-loopback network
	// adapter is used. This value is only used when the host is unspecified in
	// an address.
	if c.Host == "" {
		var err error
		c.Host, err = netutil.DetectHostIPv4()
		if err != nil {
			return err
		}
	}

	// parse etcd client address
	caddr, err := netutil.ParseAddr(c.ClientAddr)
	if err != nil {
		return err
	}
	if caddr.IsUnspecified() {
		caddr.Host = c.Host
	}
	if caddr.Port == 0 {
		caddr.Port = 2379
	}
	c.ClientAddr = caddr.String()
	c.ClientURL = url.URL{Scheme: c.ClientSecurity.Scheme(), Host: c.ClientAddr}

	// parse etcd peer address
	paddr, err := netutil.ParseAddr(c.PeerAddr)
	if err != nil {
		return err
	}
	if paddr.IsUnspecified() {
		paddr.Host = c.Host
	}
	if paddr.Port == 0 {
		paddr.Port = 2380
	}
	c.PeerAddr = paddr.String()
	c.PeerURL = url.URL{Scheme: c.PeerSecurity.Scheme(), Host: c.PeerAddr}

	// parse gossip address
	gaddr, err := netutil.ParseAddr(c.GossipAddr)
	if err != nil {
		return err
	}
	if gaddr.IsUnspecified() {
		gaddr.Host = c.Host
	}
	if gaddr.Port == 0 {
		gaddr.Port = DefaultGossipPort
	}
	c.GossipAddr = gaddr.String()
	c.GossipHost, c.GossipPort, err = netutil.SplitHostPort(c.GossipAddr)
	if err != nil {
		return errors.Wrapf(err, "cannot split GossipAddr: %#v", c.GossipAddr)
	}

	// memberlist security is set implicitly based upon the PeerSecurity
	// settings
	if c.PeerSecurity.TrustedCAFile != "" {
		var err error
		c.GossipSecretKey, err = pki.GenerateCertHash(c.PeerSecurity.TrustedCAFile)
		if err != nil {
			return err
		}
	}

	if len(c.BootstrapAddrs) == 0 && c.RequiredClusterSize > 1 {
		return errors.New("must provide at least 1 BootstrapAddrs when not a single-host cluster")
	}
	switch c.RequiredClusterSize {
	case 0:
		c.RequiredClusterSize = 1
	case 1, 3, 5:
	default:
		return errors.New("value of RequiredClusterSize must be 1, 3, or 5")
	}
	if c.Name == "" {
		if name, err := getExistingNameFromDataDir(filepath.Join(c.Dir, "member/snap/db"), c.PeerURL); err == nil {
			log.Debugf("reusing name from existing data-dir: %v", name)
			c.Name = name
		} else {
			log.Debug("cannot read existing data-dir", zap.Error(err))
			c.Name = fmt.Sprintf("%X", rand.Uint64())
		}
	}
	return nil
}

// shortName returns a shorter, lowercase version of the node name. The intent
// is to make log reading easier.
func shortName(name string) string {
	if len(name) > 5 {
		name = name[:5]
	}
	return strings.ToLower(name)
}

func getExistingNameFromDataDir(path string, peerURL url.URL) (string, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return "", err
	}
	defer db.Close()
	var name string
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("members"))
		if b == nil {
			return errors.New("existing name not found")
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var m struct {
				ID       uint64   `json:"id"`
				Name     string   `json:"name"`
				PeerURLs []string `json:"peerURLs"`
			}
			if err := json.Unmarshal(v, &m); err != nil {
				log.Error("cannot unmarshal etcd member", zap.Error(err))
				continue
			}
			for _, u := range m.PeerURLs {
				if u == peerURL.String() {
					name = m.Name
					return nil
				}
			}
		}
		return errors.New("existing name not found")
	})
	return name, err
}
