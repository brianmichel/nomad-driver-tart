package driver

import (
	"context"
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
	TaskConfig TaskConfig
	// The configuration that is shared with Nomad
	NomadConfig *drivers.TaskConfig
}

// VirtualizationClient defines the interface for interacting with virtual machines
type VirtualizationClient interface {
	// Available checks if the virtualizer is installed
	Available(ctx context.Context) (string, error)

	// Setup creates or prepares a virtual machine.
	// For example, this might involve downloading an image if 'source' is a URL,
	// or preparing a pre-existing image.
	Setup(ctx context.Context, config VMConfig) (string, error)

	// Start starts a virtual machine.
	// The 'headless' parameter suggests whether to run with a GUI.
	// Returns a process ID or similar identifier, or an error.
	Start(ctx context.Context, vmName string, headless bool) (int, error)

	// Stop stops a running virtual machine.
	// 'timeout' specifies how long to wait for a graceful shutdown.
	Stop(ctx context.Context, vmName string, timeout time.Duration) error

	// Status returns the current state of a specific VM.
	Status(ctx context.Context, vmName string) (VMState, error)

	// Delete deletes a virtual machine.
	Delete(ctx context.Context, vmName string) error

	// List returns a list of all virtual machines.
	List(ctx context.Context) ([]VMInfo, error)

	// Exec executes a command on the VM, similar to SSH.
	// 'user' specifies the user to run the command as.
	// Returns the command output or an error.
	Exec(ctx context.Context, vmName, user, command string) (string, error)
}
