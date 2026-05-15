package driver

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

type mockVirtualizer struct {
	deleteCalled bool
	deleteName   string
	stopCalled   bool
	stopName     string
}

func (m *mockVirtualizer) Available(context.Context) (string, error)        { return "", nil }
func (m *mockVirtualizer) Setup(context.Context, VMConfig) (string, error)  { return "", nil }
func (m *mockVirtualizer) Start(context.Context, string, bool) (int, error) { return 0, nil }
func (m *mockVirtualizer) Stop(_ context.Context, vmName string, _ time.Duration) error {
	m.stopCalled = true
	m.stopName = vmName
	return nil
}
func (m *mockVirtualizer) Status(context.Context, string) (VMState, error) {
	return VMStateStopped, nil
}
func (m *mockVirtualizer) Delete(_ context.Context, vmName string) error {
	m.deleteCalled = true
	m.deleteName = vmName
	return nil
}
func (m *mockVirtualizer) List(context.Context) ([]VMInfo, error) { return nil, nil }
func (m *mockVirtualizer) IPAddress(context.Context, string) (string, error) {
	return "192.168.64.10", nil
}
func (m *mockVirtualizer) Exec(context.Context, VMConfig, ExecOptions) (int, error) { return 0, nil }
func (m *mockVirtualizer) BuildStartArgs(VMConfig) ([]string, error)                { return nil, nil }
func (m *mockVirtualizer) NeedsImageDownload(context.Context, VMConfig) (bool, error) {
	return false, nil
}
func (m *mockVirtualizer) PrepareRegistryEnv(context.Context, VMConfig) ([]string, error) {
	return nil, nil
}
func (m *mockVirtualizer) BuildPullArgs(VMConfig) []string { return nil }

func TestTaskStatusIncludesDriverNetwork(t *testing.T) {
	h := &taskHandle{
		taskConfig: &drivers.TaskConfig{ID: "task-network", Name: "vm"},
		state:      drivers.TaskStateRunning,
		startedAt:  time.Now(),
		driverNetwork: &drivers.DriverNetwork{
			IP:            "192.168.64.10",
			AutoAdvertise: true,
			PortMap:       map[string]int{"opencode": 4096},
		},
	}

	status := h.TaskStatus()
	if status.NetworkOverride == nil {
		t.Fatal("expected NetworkOverride to be set")
	}
	if status.NetworkOverride.IP != "192.168.64.10" {
		t.Fatalf("expected VM IP, got %q", status.NetworkOverride.IP)
	}
	if !status.NetworkOverride.AutoAdvertise {
		t.Fatal("expected AutoAdvertise to be true")
	}
	if got := status.NetworkOverride.PortMap["opencode"]; got != 4096 {
		t.Fatalf("expected opencode port 4096, got %d", got)
	}

	status.NetworkOverride.PortMap["opencode"] = 1234
	if got := h.driverNetwork.PortMap["opencode"]; got != 4096 {
		t.Fatalf("expected TaskStatus to return a copy, original port got %d", got)
	}
}

type stubExecutor struct {
	shutdownCalled bool
	shutdownSignal string
	shutdownAfter  time.Duration
}

func (s *stubExecutor) Launch(*executor.ExecCommand) (*executor.ProcessState, error) { return nil, nil }
func (s *stubExecutor) Wait(context.Context) (*executor.ProcessState, error)         { return nil, nil }
func (s *stubExecutor) Shutdown(signal string, gracePeriod time.Duration) error {
	s.shutdownCalled = true
	s.shutdownSignal = signal
	s.shutdownAfter = gracePeriod
	return nil
}
func (s *stubExecutor) UpdateResources(*drivers.Resources) error { return nil }
func (s *stubExecutor) Version() (*executor.ExecutorVersion, error) {
	return &executor.ExecutorVersion{}, nil
}
func (s *stubExecutor) Stats(context.Context, time.Duration) (<-chan *cstructs.TaskResourceUsage, error) {
	return nil, nil
}
func (s *stubExecutor) Signal(os.Signal) error { return nil }
func (s *stubExecutor) Exec(time.Time, string, []string) ([]byte, int, error) {
	return nil, 0, nil
}
func (s *stubExecutor) ExecStreaming(context.Context, []string, bool, drivers.ExecTaskStream) error {
	return nil
}

func TestStopTaskDeletesVM(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &mockVirtualizer{}
	drv.client = mock

	exec := &stubExecutor{}
	doneCh := make(chan struct{})
	close(doneCh)

	taskID := "task-stop"
	allocID := "alloc-stop"
	drv.tasks.Set(taskID, &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      taskID,
			Name:    "test-stop",
			AllocID: allocID,
		},
		state:        drivers.TaskStateRunning,
		exec:         exec,
		pluginClient: nil,
		doneCh:       doneCh,
		logger:       drv.logger,
	})

	if err := drv.StopTask(taskID, time.Second, "SIGINT"); err != nil {
		t.Fatalf("StopTask returned error: %v", err)
	}

	if !exec.shutdownCalled {
		t.Fatal("expected executor Shutdown to be called")
	}

	expectedName := drv.generateVMName(allocID)

	if !mock.stopCalled || mock.stopName != expectedName {
		t.Fatalf("expected Stop to be called with %q", expectedName)
	}

	if !mock.deleteCalled || mock.deleteName != expectedName {
		t.Fatalf("expected Delete to be called with %q", expectedName)
	}
}

func TestStopTaskPullOnlySkipsVMOps(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &mockVirtualizer{}
	drv.client = mock

	exec := &stubExecutor{}
	doneCh := make(chan struct{})
	close(doneCh)

	taskID := "task-pull-only-stop"
	drv.tasks.Set(taskID, &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      taskID,
			Name:    "test-pull-only-stop",
			AllocID: "alloc-pull-only-stop",
		},
		state:    drivers.TaskStateRunning,
		exec:     exec,
		doneCh:   doneCh,
		logger:   drv.logger,
		pullOnly: true,
	})

	if err := drv.StopTask(taskID, time.Second, "SIGINT"); err != nil {
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

	mock := &mockVirtualizer{}
	drv.client = mock

	taskID := "task-pull-only-destroy"
	drv.tasks.Set(taskID, &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      taskID,
			Name:    "test-pull-only-destroy",
			AllocID: "alloc-pull-only-destroy",
		},
		state:    drivers.TaskStateExited,
		logger:   drv.logger,
		pullOnly: true,
	})

	if err := drv.DestroyTask(taskID, false); err != nil {
		t.Fatalf("DestroyTask returned error: %v", err)
	}
	if mock.deleteCalled {
		t.Fatal("expected virtualizer Delete to NOT be called for a pull_only task")
	}
	if _, ok := drv.tasks.Get(taskID); ok {
		t.Fatalf("expected task %q to be removed from store", taskID)
	}
}

func TestDestroyTaskDeletesVM(t *testing.T) {
	logger := hclog.NewNullLogger()
	drv := NewTartDriver(logger).(*Driver)

	mock := &mockVirtualizer{}
	drv.client = mock

	taskID := "task-123"
	allocID := "alloc-abc"
	drv.tasks.Set(taskID, &taskHandle{
		taskConfig: &drivers.TaskConfig{
			ID:      taskID,
			Name:    "test",
			AllocID: allocID,
		},
		state:  drivers.TaskStateExited,
		logger: drv.logger,
	})

	if err := drv.DestroyTask(taskID, false); err != nil {
		t.Fatalf("DestroyTask returned error: %v", err)
	}

	if !mock.deleteCalled {
		t.Fatalf("expected Delete to be called on the virtualizer")
	}

	expectedName := drv.generateVMName(allocID)
	if mock.deleteName != expectedName {
		t.Fatalf("expected Delete to be called with %q, got %q", expectedName, mock.deleteName)
	}

	if _, ok := drv.tasks.Get(taskID); ok {
		t.Fatalf("expected task %q to be removed from store", taskID)
	}
}
