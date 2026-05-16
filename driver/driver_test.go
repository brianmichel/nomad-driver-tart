package driver

import (
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestStopTaskDeletesVM(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &testClient{}
	drv.client = mock

	exec := &stubExecutor{}
	doneCh := make(chan struct{})
	close(doneCh)

	allocID := "alloc-stop"
	drv.tasks.Set("task-stop", &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      "task-stop",
			Name:    "test-stop",
			AllocID: allocID,
		},
		state:  drivers.TaskStateRunning,
		exec:   exec,
		doneCh: doneCh,
		logger: drv.logger,
	})

	if err := drv.StopTask("task-stop", time.Second, "SIGINT"); err != nil {
		t.Fatalf("StopTask returned error: %v", err)
	}

	if !exec.shutdownCalled {
		t.Fatal("expected executor Shutdown to be called")
	}

	if !mock.stopCalled {
		t.Fatal("expected Stop to be called")
	}
	if !mock.deleteCalled {
		t.Fatal("expected Delete to be called")
	}
}

func TestStopTaskPullOnlySkipsVMOps(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &testClient{}
	drv.client = mock

	exec := &stubExecutor{}
	doneCh := make(chan struct{})
	close(doneCh)

	drv.tasks.Set("task-pull-only-stop", &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      "task-pull-only-stop",
			Name:    "test-pull-only-stop",
			AllocID: "alloc-pull-only-stop",
		},
		state:    drivers.TaskStateRunning,
		exec:     exec,
		doneCh:   doneCh,
		logger:   drv.logger,
		pullOnly: true,
	})

	if err := drv.StopTask("task-pull-only-stop", time.Second, "SIGINT"); err != nil {
		t.Fatalf("StopTask returned error: %v", err)
	}
	if mock.stopCalled {
		t.Fatal("expected virtualizer Stop to NOT be called for a pull_only task")
	}
	if mock.deleteCalled {
		t.Fatal("expected virtualizer Delete to NOT be called for a pull_only task")
	}
	if !exec.shutdownCalled {
		t.Fatal("expected executor Shutdown to still be called for a pull_only task")
	}
}

func TestDestroyTaskPullOnlySkipsDelete(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &testClient{}
	drv.client = mock

	drv.tasks.Set("task-pull-only-destroy", &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      "task-pull-only-destroy",
			Name:    "test-pull-only-destroy",
			AllocID: "alloc-pull-only-destroy",
		},
		state:    drivers.TaskStateExited,
		logger:   drv.logger,
		pullOnly: true,
	})

	if err := drv.DestroyTask("task-pull-only-destroy", false); err != nil {
		t.Fatalf("DestroyTask returned error: %v", err)
	}
	if mock.deleteCalled {
		t.Fatal("expected virtualizer Delete to NOT be called for a pull_only task")
	}
	if _, ok := drv.tasks.Get("task-pull-only-destroy"); ok {
		t.Fatalf("expected task to be removed from store")
	}
}

func TestDestroyTaskDeletesVM(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &testClient{}
	drv.client = mock

	allocID := "alloc-abc"
	drv.tasks.Set("task-123", &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      "task-123",
			Name:    "test",
			AllocID: allocID,
		},
		state:  drivers.TaskStateExited,
		logger: drv.logger,
	})

	if err := drv.DestroyTask("task-123", false); err != nil {
		t.Fatalf("DestroyTask returned error: %v", err)
	}

	if !mock.deleteCalled {
		t.Fatalf("expected Delete to be called on the virtualizer")
	}

	if _, ok := drv.tasks.Get("task-123"); ok {
		t.Fatalf("expected task to be removed from store")
	}
}
