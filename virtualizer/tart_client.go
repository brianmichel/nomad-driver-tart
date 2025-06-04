package virtualizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
)

// TartClient is a wrapper around the tart CLI that implements the Virtualizer interface
type TartClient struct {
	logger hclog.Logger
}

// NewTartClient creates a new TartClient
func NewTartClient(logger hclog.Logger) *TartClient {
	return &TartClient{
		logger: logger.Named("tart_client"),
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

// IsInstalled checks if the tart binary is installed and accessible
func (c *TartClient) IsInstalled(ctx context.Context) error {
	_, err := c.GetVersion(ctx)
	return err
}

// GetVersion returns the version of the tart binary
func (c *TartClient) GetVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "tart", "--version")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tart is not installed or not in PATH: %v (stderr: %s)",
			err, stderr.String())
	}

	version := strings.TrimSpace(stdout.String())
	c.logger.Debug("Tart version", "version", version)
	return version, nil
}

// SetupVM creates a new Tart VM from a URL
func (c *TartClient) SetupVM(ctx context.Context, vmName string, url string) error {
	c.logger.Debug("Setting up Tart VM", "name", vmName, "url", url)
	cmd := exec.CommandContext(ctx, "tart", "clone", url, vmName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create VM %s from URL %s: %v (stderr: %s)",
			vmName, url, err, stderr.String())
	}

	return nil
}

// RunVM starts a Tart VM with the given name
func (c *TartClient) RunVM(ctx context.Context, vmName string, headless bool) error {
	args := []string{"run", vmName}
	if headless {
		args = append(args, "--no-graphics")
	}

	c.logger.Debug("Starting Tart VM", "name", vmName, "headless", headless)
	cmd := exec.CommandContext(ctx, "tart", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start VM %s: %v", vmName, err)
	}

	// Don't wait for the command to complete as it will block until the VM is stopped
	return nil
}

// StopVM stops a running Tart VM
func (c *TartClient) StopVM(ctx context.Context, vmName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.logger.Debug("Stopping Tart VM", "name", vmName)
	cmd := exec.CommandContext(ctx, "tart", "stop", vmName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop VM %s: %v (stderr: %s)", vmName, err, stderr.String())
	}

	return nil
}

// ListVMs returns a list of all Tart VMs
func (c *TartClient) ListVMs(ctx context.Context) ([]VMInfo, error) {
	cmd := exec.CommandContext(ctx, "tart", "list", "--format", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list VMs: %v (stderr: %s)", err, stderr.String())
	}

	// Parse the JSON output from tart
	var tartVMs []tartVMInfo
	if err := json.Unmarshal(stdout.Bytes(), &tartVMs); err != nil {
		return nil, fmt.Errorf("failed to parse VM list: %v", err)
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

// GetVMStatus returns the status of a specific VM
func (c *TartClient) GetVMStatus(ctx context.Context, vmName string) (VMState, error) {
	vms, err := c.ListVMs(ctx)
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

// CloneVM clones a Tart VM
func (c *TartClient) CloneVM(ctx context.Context, sourceVM, targetVM string) error {
	c.logger.Debug("Cloning Tart VM", "source", sourceVM, "target", targetVM)
	cmd := exec.CommandContext(ctx, "tart", "clone", sourceVM, targetVM)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clone VM %s to %s: %v (stderr: %s)",
			sourceVM, targetVM, err, stderr.String())
	}

	return nil
}

// DeleteVM deletes a Tart VM
func (c *TartClient) DeleteVM(ctx context.Context, vmName string) error {
	c.logger.Debug("Deleting Tart VM", "name", vmName)
	cmd := exec.CommandContext(ctx, "tart", "delete", vmName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete VM %s: %v (stderr: %s)", vmName, err, stderr.String())
	}

	return nil
}

// IPAddress returns the IP address of a running VM
func (c *TartClient) IPAddress(ctx context.Context, vmName string) (string, error) {
	cmd := exec.CommandContext(ctx, "tart", "ip", vmName)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get IP address for VM %s: %v (stderr: %s)",
			vmName, err, stderr.String())
	}

	// Trim any whitespace or newlines
	return strings.TrimSpace(stdout.String()), nil
}

// This method is now replaced by IsInstalled to match the Virtualizer interface

// SSH executes an SSH command on the VM
func (c *TartClient) SSH(ctx context.Context, vmName, user, command string) (string, error) {
	args := []string{"ssh", vmName}
	if user != "" {
		args = append(args, "--user", user)
	}
	if command != "" {
		args = append(args, command)
	}

	cmd := exec.CommandContext(ctx, "tart", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to execute SSH command on VM %s: %v (stderr: %s)",
			vmName, err, stderr.String())
	}

	return stdout.String(), nil
}
