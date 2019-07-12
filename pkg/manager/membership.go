package manager

import (
	"context"
	"sync"
	"time"

	"github.com/criticalstack/e2d/pkg/log"
	"go.uber.org/zap"
)

type removerFunc func(string) error

type clusterMembership struct {
	timeout time.Duration
	fn      removerFunc

	mu        sync.RWMutex
	suspects  map[string]time.Time
	hasQuorum bool
}

func newClusterMembership(ctx context.Context, d time.Duration, fn removerFunc) *clusterMembership {
	c := &clusterMembership{
		timeout:  d,
		fn:       fn,
		suspects: make(map[string]time.Time),
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for name, t := range c.suspects {
					// check if the node has been evicted past the health
					// timeout before proceeding to remove
					if t.Add(c.timeout).After(time.Now()) {
						continue
					}
					if err := c.removeMember(name); err != nil {
						log.Debug("cannot remove member", zap.Error(err))
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return c
}

func (c *clusterMembership) addSuspect(name string) {
	c.mu.Lock()
	c.suspects[name] = time.Now()
	c.mu.Unlock()
}

func (c *clusterMembership) removeSuspect(name string) {
	c.mu.Lock()
	delete(c.suspects, name)
	c.mu.Unlock()
}

func (c *clusterMembership) removeMember(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.hasQuorum {
		return nil
	}
	if err := c.fn(name); err != nil {
		return err
	}
	delete(c.suspects, name)
	return nil
}

func (c *clusterMembership) ensureQuorum(q bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// quorum has not changed, so no further actions taken
	if q == c.hasQuorum {
		return c.hasQuorum
	}
	c.hasQuorum = q
	c.suspects = make(map[string]time.Time)
	return c.hasQuorum
}
