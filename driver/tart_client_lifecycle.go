package driver

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCPUCores = 4
	defaultMemoryMB = 4096
)

// SetupVM creates a new Tart VM from a URL
func (c *tartCLI) Setup(ctx context.Context, config VMConfig) (string, error) {
	env, err := c.PrepareRegistryEnv(ctx, config)
	if err != nil {
		return "", err
	}

	vmName := vmName(config.Nomad.AllocID)
	url := config.Driver.URL

	c.logger.Trace("Setting up Tart VM", "name", vmName, "url", url)
	cmd := c.runner.Run(ctx, "tart", "clone", url, vmName)
	cmd.Env = env

	// Configure VM resources before starting it using the Nomad resources block
	cpuCores := defaultCPUCores
	memoryMB := defaultMemoryMB
	if config.Nomad.Resources != nil && config.Nomad.Resources.LinuxResources != nil {
		// TODO: See if there's a better way of getting the number of cores
		cpuCores = len(strings.Split(config.Nomad.Resources.LinuxResources.CpusetCpus, ","))
		memoryMB = int(config.Nomad.Resources.LinuxResources.MemoryLimitBytes / 1024 / 1024)
	}

	diskGB := config.Driver.DiskSize

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create VM %s from URL %s: %v (stderr: %s)",
			vmName, url, err, stderr.String())
	}

	if err := c.SetVMResources(ctx, vmName, cpuCores, memoryMB, diskGB); err != nil {
		return "", fmt.Errorf("failed to set VM resources: %w", err)
	}

	return vmName, nil
}

// RunVM starts a Tart VM with the given name
func (c *tartCLI) Start(ctx context.Context, vmName string, headless bool) (int, error) {
	args := []string{"run", vmName}
	if headless {
		args = append(args, "--no-graphics")
	}

	c.logger.Trace("Starting Tart VM", "name", vmName, "headless", headless)
	cmd := c.runner.Run(ctx, "tart", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("failed to start VM %s: %v", vmName, err)
	}

	// Don't wait for the command to complete as it will block until the VM is stopped
	return cmd.Process.Pid, nil
}

// StopVM stops a running Tart VM. The gracePeriod is the time to wait for
// a graceful shutdown before forcefully terminating the VM.
func (c *tartCLI) Stop(ctx context.Context, vmName string, gracePeriod time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, gracePeriod)
	defer cancel()

	c.logger.Trace("Stopping Tart VM", "name", vmName)
	cmd := exec.CommandContext(ctx, "tart", "stop", vmName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop VM %s: %v (stderr: %s)", vmName, err, stderr.String())
	}

	return nil
}

// CloneVM clones a Tart VM
func (c *tartCLI) CloneVM(ctx context.Context, sourceVM, targetVM string) error {
	c.logger.Trace("Cloning Tart VM", "source", sourceVM, "target", targetVM)
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
func (c *tartCLI) Delete(ctx context.Context, vmName string) error {
	c.logger.Trace("Deleting Tart VM", "name", vmName)
	cmd := exec.CommandContext(ctx, "tart", "delete", vmName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete VM %s: %v (stderr: %s)", vmName, err, stderr.String())
	}

	return nil
}

// IPAddress returns the IP address of a running VM.
// Bridged networking requires ARP-based resolution because Tart cannot rely on
// the host DHCP lease database in that mode.
func (c *tartCLI) IPAddress(ctx context.Context, vmName string, network *NetworkConfig) (string, error) {
	args := []string{"ip"}
	if network != nil && strings.EqualFold(strings.TrimSpace(network.Mode), "bridged") {
		args = append(args, "--resolver=arp")
	}
	args = append(args, vmName)

	cmd := c.runner.Run(ctx, "tart", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %v (stderr: %s)", errVMIPUnavailable, err, stderr.String())
	}

	// Trim any whitespace or newlines
	return strings.TrimSpace(stdout.String()), nil
}

// SetVMResources modifies CPU cores, memory (MB), and disk size (GB) for a VM.
func (c *tartCLI) SetVMResources(ctx context.Context, vmName string, cpu, memoryMB, diskGB int) error {
	args := []string{"set", vmName}
	if cpu > 0 {
		args = append(args, "--cpu", strconv.Itoa(cpu))
	}
	if memoryMB > 0 {
		args = append(args, "--memory", strconv.Itoa(memoryMB))
	}
	if diskGB > 0 {
		args = append(args, "--disk-size", strconv.Itoa(diskGB))
	}

	if len(args) == 2 {
		return nil
	}

	c.logger.Trace("Setting VM resources", "name", vmName, "args", args)
	cmd := c.runner.Run(ctx, "tart", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set resources for VM %s: %v (stderr: %s)", vmName, err, stderr.String())
	}
	return nil
}
