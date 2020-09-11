//nolint:errcheck
package gossip

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestMemberEncodeDecode(t *testing.T) {
	expected := &Member{
		ID:             1,
		Name:           "node1",
		ClientAddr:     ":2379",
		ClientURL:      "http://127.0.0.1:2379",
		PeerAddr:       ":2379",
		PeerURL:        "http://127.0.0.1:2379",
		GossipAddr:     ":7980",
		BootstrapAddrs: []string{":7981", ":7982"},
		Status:         Pending,
	}
	data, err := expected.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	m := &Member{}
	if err := m.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(expected, m); diff != "" {
		t.Errorf("Member: after Unmarshal differs: (-want +got)\n%s", diff)
	}
}

func TestGossipDelegate(t *testing.T) {
	t.Skip()
	g1 := New(&Config{
		Name:       "node1",
		GossipPort: 7980,
	})
	defer g1.Shutdown()
	go func() {
		if err := g1.Start(context.Background(), []string{":7981"}); err != nil {
			t.Fatal(err)
		}
	}()
	g2 := New(&Config{
		Name:       "node2",
		GossipPort: 7981,
	})
	defer g2.Shutdown()
	go func() {
		if err := g2.Start(context.Background(), []string{":7980"}); err != nil {
			t.Fatal(err)
		}
	}()
	g3 := New(&Config{
		Name:       "node3",
		GossipPort: 7982,
	})
	defer g3.Shutdown()
	go func() {
		if err := g3.Start(context.Background(), []string{":7981"}); err != nil {
			t.Fatal(err)
		}
	}()

	time.Sleep(500 * time.Millisecond)
	g1.Update(Pending)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if g2.nodes["node1"] == Pending && g3.nodes["node1"] == Pending {
				return
			}
		case <-ctx.Done():
			t.Fatal("timeout reached, status never propagated")
		}
	}
}
