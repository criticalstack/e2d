//nolint
package e2e

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
	snapshotutil "github.com/criticalstack/e2d/pkg/snapshot/util"
)

type Node struct {
	*manager.Manager
	c *TestCluster

	removed bool
	started bool
}

func (n *Node) Client() *manager.Client {
	scheme := "https"
	if n.Config().CAKey == "" {
		scheme = "http"
	}
	clientURL := url.URL{Scheme: scheme, Host: n.Config().ClientAddr.String()}
	cfg := &client.Config{
		ClientURLs: []string{clientURL.String()},
		Timeout:    5 * time.Second,
	}
	if n.Config().CACert != "" {
		dir := filepath.Dir(n.Config().CACert)
		cfg.SecurityConfig = client.SecurityConfig{
			CertFile:      filepath.Join(dir, "client.crt"),
			KeyFile:       filepath.Join(dir, "client.key"),
			CertAuth:      true,
			TrustedCAFile: n.Config().CACert,
		}
	}
	cc, err := client.New(cfg)
	if err != nil {
		panic(err)
	}
	return &manager.Client{
		Client:  cc,
		Timeout: 5 * time.Second,
	}
}

func (n *Node) Name() string {
	return n.Etcd().Name
}

func (n *Node) Remove() *Node {
	if n.started {
		n.c.t.Fatalf("cannot remove node %q, must be stopped first", n.Name())
	}
	n.removed = true
	return n
}

func (n *Node) Start() *Node {
	if n.removed || n.started {
		return nil
	}
	log.Infof("starting node: %q\n", n.Name())
	n.started = true

	go func() {
		if err := n.Run(); err != nil {
			n.c.t.Fatalf("cannot start node %q: %v", n.Name(), err)
		}
	}()
	return n
}

func (n *Node) Stop() *Node {
	if !n.started {
		return n
	}
	log.Infof("stopping node: %q\n", n.Name())
	n.HardStop()
	n.started = false
	return n
}

func (n *Node) Restart() *Node {
	if !n.started {
		return n
	}
	if err := n.Manager.Restart(); err != nil {
		n.c.t.Fatalf("cannot restart node %q: %v", n.Name(), err)
	}
	return n
}

func (n *Node) Wait() *Node {
	switch {
	case n.removed:
		nodes := n.c.getStarted()
		log.Infof("waiting for the node %#v to be removed from the following nodes: %v\n", n.Name(), nodes)
		waiting := nodes
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		var wg sync.WaitGroup
		for _, name := range nodes {
			wg.Add(1)

			go func(name string) {
				defer wg.Done()

				for {
					select {
					case removedNode := <-n.c.Node(name).RemoveCh():
						if removedNode == n.Name() {
							for i, name := range waiting {
								if name == removedNode {
									waiting = append(waiting[:i], waiting[i+1:]...)
								}
							}
							return
						}
					case <-ctx.Done():
						n.c.t.Fatalf("timed out waiting for nodes to be removed: %q", strings.Join(waiting, ","))
						return
					}
				}
			}(name)
		}
		wg.Wait()
	case n.started:
		log.Infof("waiting for the node %q to be started\n", n.Name())
		for {
			if n.Etcd().IsRunning() {
				log.Infof("node %q started successfully!\n", n.Name())
				return n
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return n
}

func (n *Node) SaveSnapshot() *Node {
	data, size, _, err := n.Etcd().CreateSnapshot(0)
	if err != nil {
		n.c.t.Fatal(err)
	}
	if n.Config().SnapshotConfiguration.Encryption {
		key, err := manager.ReadEncryptionKey(n.Config().CAKey)
		if err != nil {
			n.c.t.Fatal(err)
		}
		data = snapshotutil.NewEncrypterReadCloser(data, &key, size)
	}
	if n.Config().SnapshotConfiguration.Compression {
		data = snapshotutil.NewGzipReadCloser(data)
	}
	if err := n.Snapshotter().Save(data); err != nil {
		n.c.t.Fatal(err)
	}
	return n
}
