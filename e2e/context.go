package e2e

import (
	"sync"
	"time"

	"github.com/criticalstack/e2d/pkg/log"
)

type Context struct {
	c     *TestCluster
	nodes []*Node
}

func (c *Context) Start() *Context {
	for _, node := range c.nodes {
		node.Start()
	}
	return c
}

func (c *Context) Remove() *Context {
	for _, n := range c.nodes {
		n.Remove()
	}
	return c
}

func (c *Context) Restart() *Context {
	var wg sync.WaitGroup
	for _, n := range c.nodes {
		wg.Add(1)

		go func(n *Node) {
			defer wg.Done()

			n.Restart()
		}(n)
	}
	wg.Wait()
	return c
}

func (c *Context) Stop() *Context {
	for _, n := range c.nodes {
		n.Stop()
	}
	return c
}

func (c *Context) Wait() *Context {
	waitChan := make(chan struct{})

	go func() {
		var wg sync.WaitGroup
		for _, node := range c.nodes {
			wg.Add(1)

			go func(node *Node) {
				defer wg.Done()

				node.Wait()
			}(node)
		}
		wg.Wait()
		waitChan <- struct{}{}
	}()

	select {
	case <-waitChan:
		break
	case <-time.After(1 * time.Minute):
		c.c.t.Fatal("timed out waiting for node state")
	}
	log.Info("healthy!")
	return c
}

func (c *Context) SaveSnapshot() *Context {
	for _, n := range c.nodes {
		n.SaveSnapshot()
	}
	return c
}

func (c *Context) TestClientSet(name string) *Context {
	cl := c.c.Node(name).Client()
	if err := cl.Set(testKey1, testValue1); err != nil {
		c.c.t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		c.c.t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		c.c.t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
	return c
}

func (c *Context) TestClientGet(name string) *Context {
	cl := c.c.Node(name).Client()
	v, err := cl.Get(testKey1)
	if err != nil {
		c.c.t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		c.c.t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
	return c
}
