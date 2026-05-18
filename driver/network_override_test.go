package driver

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestResolveDriverNetworkReturnsVMIP(t *testing.T) {
	mock := &testClient{mockNetworker: mockNetworker{ip: "192.168.64.10"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	drv := &Driver{ctx: ctx, client: mock, logger: hclog.NewNullLogger()}
	vm := VMConfig{Nomad: &drivers.TaskConfig{AllocID: "alloc-network"}}

	network, err := drv.resolveDriverNetwork(vm)
	if err != nil {
		t.Fatalf("resolveDriverNetwork returned error: %v", err)
	}
	if network == nil {
		t.Fatal("expected network override")
	}
	if network.IP != "192.168.64.10" {
		t.Fatalf("expected IP 192.168.64.10, got %q", network.IP)
	}
	if network.AutoAdvertise {
		t.Fatal("expected AutoAdvertise to be false")
	}
}

func TestTaskStatusIncludesNetworkOverride(t *testing.T) {
	h := &taskHandle{
		taskConfig: &drivers.TaskConfig{ID: "task-1", Name: "vm"},
		state:      drivers.TaskStateRunning,
		startedAt:  time.Now(),
		logger:     hclog.NewNullLogger(),
		exitResult: &drivers.ExitResult{ExitCode: 7},
		networkOverride: &drivers.DriverNetwork{
			IP: "192.168.64.10",
		},
	}

	status := h.TaskStatus()
	if status.NetworkOverride == nil {
		t.Fatal("expected NetworkOverride in task status")
	}
	if status.NetworkOverride.IP != "192.168.64.10" {
		t.Fatalf("expected status network IP 192.168.64.10, got %q", status.NetworkOverride.IP)
	}
	if status.ExitResult == nil || status.ExitResult.ExitCode != 7 {
		t.Fatalf("expected exit result copy with exit code 7, got %#v", status.ExitResult)
	}

	status.NetworkOverride.IP = "10.0.0.99"
	status.ExitResult.ExitCode = 42

	if h.networkOverride.IP != "192.168.64.10" {
		t.Fatalf("expected handle network override to remain unchanged, got %q", h.networkOverride.IP)
	}
	if h.exitResult.ExitCode != 7 {
		t.Fatalf("expected handle exit result to remain unchanged, got %d", h.exitResult.ExitCode)
	}
}
