package driver

import (
	"context"
	"time"
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

// Virtualizer defines the interface for interacting with virtual machines
type Virtualizer interface {
	// IsInstalled checks if the virtualization software is installed and accessible
	IsInstalled(ctx context.Context) error
	
	// GetVersion returns the version of the virtualization software
	GetVersion(ctx context.Context) (string, error)
	
	// ListVMs returns a list of all virtual machines
	ListVMs(ctx context.Context) ([]VMInfo, error)
	
	// GetVMStatus returns the status of a specific VM
	GetVMStatus(ctx context.Context, vmName string) (VMState, error)
	
	// RunVM starts a virtual machine
	RunVM(ctx context.Context, vmName string, headless bool) error
	
	// StopVM stops a running virtual machine
	StopVM(ctx context.Context, vmName string, timeout time.Duration) error
	
	// CloneVM clones a virtual machine
	CloneVM(ctx context.Context, sourceVM, targetVM string) error
	
	// DeleteVM deletes a virtual machine
	DeleteVM(ctx context.Context, vmName string) error
	
	// IPAddress returns the IP address of a running VM
	IPAddress(ctx context.Context, vmName string) (string, error)
	
	// SSH executes a command on the VM via SSH
	SSH(ctx context.Context, vmName, user, command string) (string, error)
}
