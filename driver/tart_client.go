package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
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

// Available checks if the tart binary is installed and accessible
func (c *TartClient) Available(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "tart", "--version")

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

// SetupVM creates a new Tart VM from a URL
func (c *TartClient) Setup(ctx context.Context, config VMConfig) (string, error) {
	vmName := c.generateVMName(config.NomadConfig.AllocID)
	url := config.TaskConfig.URL

	c.logger.Trace("Setting up Tart VM", "name", vmName, "url", url)
	cmd := exec.CommandContext(ctx, "tart", "clone", url, vmName)

	// Configure VM resources before starting it using the Nomad resources block
	var cpuCores int = 4    // Default to 4 cores
	var memoryMB int = 4096 // Default to 4GB of memory
	if config.NomadConfig.Resources != nil && config.NomadConfig.Resources.LinuxResources != nil {
		// TODO: See if there's a better way of getting the number of cores
		cpuCores = len(strings.Split(config.NomadConfig.Resources.LinuxResources.CpusetCpus, ","))
		memoryMB = int(config.NomadConfig.Resources.LinuxResources.MemoryLimitBytes / 1024 / 1024)
	}

	diskGB := config.TaskConfig.DiskSize

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create VM %s from URL %s: %v (stderr: %s)",
			vmName, url, err, stderr.String())
	}

	if err := c.SetVMResources(ctx, vmName, cpuCores, memoryMB, diskGB); err != nil {
		return "", fmt.Errorf("failed to set VM resources: %v", err)
	}

	return vmName, nil
}

// RunVM starts a Tart VM with the given name
func (c *TartClient) Start(ctx context.Context, vmName string, headless bool) (int, error) {
	args := []string{"run", vmName}
	if headless {
		args = append(args, "--no-graphics")
	}

	c.logger.Trace("Starting Tart VM", "name", vmName, "headless", headless)
	cmd := exec.CommandContext(ctx, "tart", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("failed to start VM %s: %v", vmName, err)
	}

	// Don't wait for the command to complete as it will block until the VM is stopped
	return cmd.Process.Pid, nil
}

// StopVM stops a running Tart VM
func (c *TartClient) Stop(ctx context.Context, vmName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
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

// ListVMs returns a list of all Tart VMs
func (c *TartClient) List(ctx context.Context) ([]VMInfo, error) {
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

// Status returns the status of a specific VM
func (c *TartClient) Status(ctx context.Context, vmName string) (VMState, error) {
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

// CloneVM clones a Tart VM
func (c *TartClient) CloneVM(ctx context.Context, sourceVM, targetVM string) error {
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
func (c *TartClient) Delete(ctx context.Context, vmName string) error {
	c.logger.Trace("Deleting Tart VM", "name", vmName)
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

// Exec executes an SSH command on the VM
func (c *TartClient) Exec(ctx context.Context, config VMConfig, opts ExecOptions) (int, error) {
	if len(opts.Command) == 0 {
		return -1, fmt.Errorf("command is required but was empty")
	}

	vmName := c.generateVMName(config.NomadConfig.AllocID)

	ip, err := c.IPAddress(ctx, vmName)
	if err != nil || ip == "" {
		return -1, fmt.Errorf("failed to get VM IP: %v", err)
	}

	sshArgs := []string{"-q", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"}
	if opts.Tty {
		sshArgs = append(sshArgs, "-tt")
	} else {
		sshArgs = append(sshArgs, "-T")
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", config.TaskConfig.SSHUser, ip))
	sshArgs = append(sshArgs, opts.Command...)

	sshPassArgs := []string{"-p", config.TaskConfig.SSHPassword, "ssh"}
	sshPassArgs = append(sshPassArgs, sshArgs...)

	cmd := exec.CommandContext(ctx, "sshpass", sshPassArgs...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	var sshErr error
	if opts.Tty {
		ptmx, err := pty.Start(cmd)
		if err != nil {
			return -1, fmt.Errorf("failed to start ssh command: %v", err)
		}
		defer ptmx.Close()

		go func() { io.Copy(ptmx, opts.Stdin) }()
		go func() { io.Copy(opts.Stdout, ptmx) }()

		go func() {
			for sz := range opts.ResizeCh {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(sz.Height), Cols: uint16(sz.Width)})
			}
		}()

		sshErr = cmd.Wait()
	} else {
		cmd.Stdin = opts.Stdin
		cmd.Stdout = opts.Stdout
		cmd.Stderr = opts.Stderr

		sshErr = cmd.Run()
	}

	exitCode := 0
	if sshErr != nil {
		if ee, ok := sshErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if ctx.Err() != nil {
			return -1, ctx.Err()
		} else {
			return -1, sshErr
		}
	} else if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return exitCode, nil
}

// SetVMResources modifies CPU cores, memory (MB), and disk size (GB) for a VM.
func (c *TartClient) SetVMResources(ctx context.Context, vmName string, cpu, memoryMB, diskGB int) error {
	args := []string{"set", vmName}
	if cpu > 0 {
		args = append(args, "--cpu", fmt.Sprintf("%d", cpu))
	}
	if memoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%d", memoryMB))
	}
	if diskGB > 0 {
		args = append(args, "--disk-size", fmt.Sprintf("%d", diskGB))
	}

	if len(args) == 2 {
		return nil
	}

	c.logger.Trace("Setting VM resources", "name", vmName, "args", args)
	cmd := exec.CommandContext(ctx, "tart", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set resources for VM %s: %v (stderr: %s)", vmName, err, stderr.String())
	}
	return nil
}

func (c *TartClient) generateVMName(allocationID string) string {
	return fmt.Sprintf("nomad-%s", allocationID)
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
