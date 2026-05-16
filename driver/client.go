package driver

import (
	"context"
	"io"
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
)

// VMState represents the state of a virtual machine
type VMState string

const (
	// VMStateStopped indicates the VM is not running
	VMStateStopped VMState = "stopped"
	// VMStateRunning indicates the VM is currently running
	VMStateRunning VMState = "running"
	// VMStatePaused indicates the VM is paused
	VMStatePaused VMState = "paused"
)

// VMInfo contains information about a virtual machine
type VMInfo struct {
	Name   string  `json:"name"`
	Status VMState `json:"status"`
}

type VMConfig struct {
	// The configuration that is custom to our custom driver
	Driver TaskConfig
	// The configuration that is shared with Nomad
	Nomad *drivers.TaskConfig
}

type ExecOptions struct {
	Command  []string
	Tty      bool
	Stdin    io.ReadCloser
	Stdout   io.WriteCloser
	Stderr   io.WriteCloser
	ResizeCh <-chan drivers.TerminalSize
}

// Prober checks if the underlying virtualization tool is available.
type Prober interface {
	Available(ctx context.Context) (string, error)
}

// Lister enumerates VMs.
type Lister interface {
	List(ctx context.Context) ([]VMInfo, error)
	Status(ctx context.Context, vmName string) (VMState, error)
}

// Lifecycle manages VM creation, start, stop, and deletion.
type Lifecycle interface {
	Setup(ctx context.Context, config VMConfig) (string, error)
	Start(ctx context.Context, vmName string, headless bool) (int, error)
	// Stop stops a running Tart VM. The gracePeriod is the time to wait for
	// a graceful shutdown before forcefully terminating the VM.
	Stop(ctx context.Context, vmName string, gracePeriod time.Duration) error
	Delete(ctx context.Context, vmName string) error
	CloneVM(ctx context.Context, sourceVM, targetVM string) error
	SetVMResources(ctx context.Context, vmName string, cpu, memoryMB, diskGB int) error
}

// Commander runs commands inside a VM (e.g., over SSH).
type Commander interface {
	Exec(ctx context.Context, config VMConfig, opts ExecOptions) (int, error)
}

// Networker queries VM network info.
type Networker interface {
	IPAddress(ctx context.Context, vmName string, network *NetworkConfig) (string, error)
}

// Builder constructs CLI arguments and environment for tart commands.
type Builder interface {
	BuildStartArgs(config VMConfig) ([]string, error)
	BuildPullArgs(config VMConfig) []string
	PrepareRegistryEnv(ctx context.Context, config VMConfig) ([]string, error)
	NeedsImageDownload(ctx context.Context, config VMConfig) (bool, error)
}

// Client is the composite interface for interacting with virtual machines.
type Client interface {
	Prober
	Lister
	Lifecycle
	Commander
	Networker
	Builder
}
