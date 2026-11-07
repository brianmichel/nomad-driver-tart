package driver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

type blockingDeleteClient struct {
	testClient
	entered chan struct{}
	release chan struct{}
}

func (c *blockingDeleteClient) Delete(context.Context, string) error {
	c.entered <- struct{}{}
	<-c.release
	return nil
}

func TestConcurrentDestroyCannotDeleteReplacementClaim(t *testing.T) {
	d := NewTartDriver(hclog.NewNullLogger()).(*Driver)
	defer d.signalShutdown()
	c := &blockingDeleteClient{entered: make(chan struct{}, 2), release: make(chan struct{}, 2)}
	d.client = c
	cfg := &drivers.TaskConfig{ID: "task", Name: "name", AllocID: "alloc"}
	old := identityHandle(t, cfg, drivers.TaskStateExited)
	if d.tasks.reserveOwnership(cfg.ID, old.ownership) == nil {
		t.Fatal("reserve old")
	}
	d.tasks.Set(cfg.ID, old)
	results := make(chan error, 2)
	go func() { results <- d.DestroyTask(cfg.ID, false) }()
	<-c.entered
	go func() { results <- d.DestroyTask(cfg.ID, false) }()
	// An unprotected second destroy reaches VM deletion while the first is in flight.
	select {
	case <-c.entered:
		c.release <- struct{}{}
		if err := <-results; err != nil {
			t.Fatal(err)
		}
		replacement := d.tasks.reserveOwnership(cfg.ID, old.ownership)
		if replacement == nil {
			t.Fatal("replacement reserve failed")
		}
		c.release <- struct{}{}
		<-results
		if d.tasks.reserveOwnership(cfg.ID, old.ownership) != nil {
			t.Fatal("stale destroy removed replacement reservation")
		}
		t.Fatal("concurrent destroy reached VM deletion")
	case <-time.After(300 * time.Millisecond):
	}
	c.release <- struct{}{}
	if err := <-results; err != nil && !errors.Is(err, drivers.ErrTaskNotFound) {
		t.Fatal(err)
	}
	if err := <-results; err != nil && !errors.Is(err, drivers.ErrTaskNotFound) {
		t.Fatal(err)
	}
	select {
	case <-c.entered:
		t.Fatal("stale destroy reached VM deletion")
	default:
	}
}
