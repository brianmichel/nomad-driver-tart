package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/hashicorp/go-hclog"
)

// runner abstracts over exec.CommandContext for testability.
type runner interface {
	Run(ctx context.Context, name string, args ...string) *exec.Cmd
}

// execRunner is the production implementation that delegates to exec.CommandContext.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// tartCLI is a wrapper around the tart CLI that implements the Client interface
type tartCLI struct {
	logger hclog.Logger
	runner runner
}

// NewTartClient creates a new tartCLI
func NewTartClient(logger hclog.Logger) Client {
	return &tartCLI{
		logger: logger.Named("tart_client"),
		runner: execRunner{},
	}
}

// tartVMInfo is the internal struct for parsing tart JSON output
type tartVMInfo struct {
	SizeOnDisk int    `json:"SizeOnDisk"`
	Name       string `json:"Name"`
	Running    bool   `json:"Running"`
	Size       int    `json:"Size"`
	Disk       int    `json:"Disk"`
	State      string `json:"State"`
	Source     string `json:"Source"`
}

// Available checks if the tart binary is installed and accessible
func (c *tartCLI) Available(ctx context.Context) (string, error) {
	cmd := c.runner.Run(ctx, "tart", "--version")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tart is not installed or not in PATH: %v (stderr: %s)",
			err, stderr.String())
	}

	version := strings.TrimSpace(stdout.String())
	c.logger.Trace("Tart version", "version", version)
	return version, nil
}

// ListVMs returns a list of all Tart VMs
func (c *tartCLI) List(ctx context.Context) ([]VMInfo, error) {
	cmd := c.runner.Run(ctx, "tart", "list", "--format", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list VMs: %w (stderr: %s)", err, stderr.String())
	}

	// Parse the JSON output from tart
	var tartVMs []tartVMInfo
	if err := json.Unmarshal(stdout.Bytes(), &tartVMs); err != nil {
		return nil, fmt.Errorf("failed to parse VM list: %w", err)
	}

	// Convert from tart-specific format to our interface format
	vms := make([]VMInfo, len(tartVMs))
	for i, vm := range tartVMs {
		vms[i] = VMInfo{
			Name:   vm.Name,
			Status: convertTartStatus(vm.State),
		}
	}

	return vms, nil
}

// Status returns the status of a specific VM
func (c *tartCLI) Status(ctx context.Context, vmName string) (VMState, error) {
	vms, err := c.List(ctx)
	if err != nil {
		return "", err
	}

	for _, vm := range vms {
		if vm.Name == vmName {
			return vm.Status, nil
		}
	}

	return "", fmt.Errorf("VM %s not found", vmName)
}

// NeedsImageDownload returns true when the referenced image is not yet
// available locally and must be pulled prior to setup.
func (c *tartCLI) NeedsImageDownload(ctx context.Context, config VMConfig) (bool, error) {
	vms, err := c.List(ctx)
	if err != nil {
		return false, err
	}
	for _, vm := range vms {
		// Tart stores locally downloaded VMs by the URL of the image
		if vm.Name == config.Driver.URL {
			return false, nil
		}
	}
	return true, nil
}

// convertTartStatus converts tart status strings to our VMState type
func convertTartStatus(tartStatus string) VMState {
	switch strings.ToLower(tartStatus) {
	case "running":
		return VMStateRunning
	case "paused":
		return VMStatePaused
	default:
		return VMStateStopped
	}
}
