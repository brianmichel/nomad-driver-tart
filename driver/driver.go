package driver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
)

const (
	// pluginName is the name of the plugin
	pluginName = "tart"

	// fingerprintPeriod is the interval at which the driver will send fingerprint responses
	fingerprintPeriod = 30 * time.Second

	// taskHandleVersion is the version of task handle which this driver sets
	// and understands how to decode driver state
	taskHandleVersion = 1
)

var (
	// pluginInfo is the response returned for the PluginInfo RPC
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDriver,
		PluginApiVersions: []string{drivers.ApiVersion010},
		PluginVersion:     "0.1.0",
		Name:              pluginName,
	}

	// capabilities is returned by the Capabilities RPC and indicates what
	// optional features this driver supports
	capabilities = &drivers.Capabilities{
		SendSignals: true,
		Exec:        true,
		FSIsolation: drivers.FSIsolationImage,
	}
)

// Driver is a driver for running Tart VM containers
type Driver struct {
	// eventer is used to handle multiplexing of TaskEvents calls such that an
	// event can be broadcast to all callers
	eventer *eventer.Eventer

	// config is the driver configuration set by the SetConfig RPC
	config *Config

	// nomadConfig is the client config from nomad
	nomadConfig *base.ClientDriverConfig

	// tasks is the in memory datastore mapping taskIDs to rawExecDriverHandles
	tasks *taskStore

	// ctx is the context for the driver. It is passed to other subsystems to
	// coordinate shutdown
	ctx context.Context

	// signalShutdown is called when the driver is shutting down and cancels the
	// ctx passed to any subsystems
	signalShutdown context.CancelFunc

	// logger will log to the Nomad agent
	logger hclog.Logger

	// client is the interface for interacting with virtual machines
	client VirtualizationClient
}

// TaskState is the state which is encoded in the handle returned in
// StartTask. This information is needed to rebuild the task state and handler
// during recovery.
type TaskState struct {
	TaskConfig  *drivers.TaskConfig
	StartedAt   time.Time
	CompletedAt time.Time
	ExitResult  *drivers.ExitResult
}

// NewTartDriver returns a new driver plugin implementation
func NewTartDriver(logger hclog.Logger) drivers.DriverPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named(pluginName)

	// Create a TartClient as our default virtualizer implementation
	client := NewTartClient(logger)

	return &Driver{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &Config{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
		client:         client,
	}
}

// PluginInfo returns information describing the plugin.
func (d *Driver) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

// ConfigSchema returns the plugin configuration schema.
func (d *Driver) ConfigSchema() (*hclspec.Spec, error) {
	return configSpec, nil
}

// SetConfig is called by the client to pass the configuration for the plugin.
func (d *Driver) SetConfig(cfg *base.Config) error {
	var config Config
	if len(cfg.PluginConfig) != 0 {
		if err := base.MsgPackDecode(cfg.PluginConfig, &config); err != nil {
			return err
		}
	}

	d.config = &config
	if cfg.AgentConfig != nil {
		d.nomadConfig = cfg.AgentConfig.Driver
	}

	return nil
}

// TaskConfigSchema returns the HCL schema for the configuration of a task.
func (d *Driver) TaskConfigSchema() (*hclspec.Spec, error) {
	return taskConfigSpec, nil
}

// Capabilities returns the features supported by the driver.
func (d *Driver) Capabilities() (*drivers.Capabilities, error) {
	return capabilities, nil
}

// Fingerprint returns a channel that will be used to send health information to
// Nomad. This is the primary method Nomad uses to detect driver health.
func (d *Driver) Fingerprint(ctx context.Context) (<-chan *drivers.Fingerprint, error) {
	ch := make(chan *drivers.Fingerprint)
	go d.handleFingerprint(ctx, ch)
	return ch, nil
}

// StartTask returns a task handle and a driver network if necessary.
func (d *Driver) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var taskConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&taskConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	d.logger.Info("starting tart task", "task_cfg", hclog.Fmt("%+v", taskConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	allocVMName := d.generateVMName(cfg.AllocID)
	// Check if the VM already exists before attempting a download
	vms, err := d.client.List(d.ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list VMs: %v", err)
	}

	vmExists := false
	for _, vm := range vms {
		// Tart stores locally downloaded VMs by the URL of the image
		// see if we have already cloned the image so that we can skip
		// downloading the image from the registry.
		if vm.Name == taskConfig.URL {
			vmExists = true
			break
		}
	}

	if !vmExists {
		d.logger.Info("VM image not found locally, downloading", "url", taskConfig.URL)
		d.eventer.EmitEvent(&drivers.TaskEvent{
			TaskID:    cfg.ID,
			TaskName:  cfg.Name,
			AllocID:   cfg.AllocID,
			Timestamp: time.Now(),
			Message:   "Downloading VM image",
			Annotations: map[string]string{
				"url": taskConfig.URL,
			},
		})
	}

	vmConfig := VMConfig{
		TaskConfig:  taskConfig,
		NomadConfig: cfg,
	}

	if _, err := d.client.Setup(d.ctx, vmConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to setup VM: %v", err)
	}

	if !vmExists {
		d.eventer.EmitEvent(&drivers.TaskEvent{
			TaskID:    cfg.ID,
			TaskName:  cfg.Name,
			AllocID:   cfg.AllocID,
			Timestamp: time.Now(),
			Message:   "VM image download complete",
			Annotations: map[string]string{
				"url": taskConfig.URL,
			},
		})
	}

	pluginLogFile := filepath.Join(cfg.TaskDir().Dir, "executor.out")
	execConfig := &executor.ExecutorConfig{
		LogFile:  pluginLogFile,
		LogLevel: "debug",
	}

	logger := d.logger.With("task_name", handle.Config.Name, "alloc_id", handle.Config.AllocID)
	execImpl, pluginClient, err := executor.CreateExecutor(logger, d.nomadConfig, execConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create executor: %v", err)
	}

	args := []string{"run", allocVMName}
	if !taskConfig.ShowUI {
		args = append(args, "--no-graphics")
	}

	// Add secrets dir as read-only mount
	args = append(args, fmt.Sprintf("--dir=%s:ro", cfg.TaskDir().SecretsDir))

	// Apply networking options per task config
	netArgs, err := buildTartNetworkArgs(taskConfig.Network)
	if err != nil {
		pluginClient.Kill()
		return nil, nil, err
	}
	args = append(args, netArgs...)

	execCmd := &executor.ExecCommand{
		Cmd:              "tart",
		Args:             args,
		Env:              d.TartEnvList(cfg),
		User:             cfg.User,
		TaskDir:          cfg.TaskDir().Dir,
		StdoutPath:       cfg.StdoutPath,
		StderrPath:       cfg.StderrPath,
		NetworkIsolation: cfg.NetworkIsolation,
	}

	ps, err := execImpl.Launch(execCmd)
	if err != nil {
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to launch VM: %v", err)
	}

	// Store the driver state on the handle
	state := TaskState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
	}

	handle.State = drivers.TaskStateRunning

	// Encode the driver state
	if err := handle.SetDriverState(&state); err != nil {
		execImpl.Shutdown("", 0)
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	h := &taskHandle{
		exec:         execImpl,
		pluginClient: pluginClient,
		pid:          ps.Pid,
		taskConfig:   cfg,
		state:        drivers.TaskStateRunning,
		startedAt:    time.Now().Round(time.Millisecond),
		logger:       d.logger,
		doneCh:       make(chan struct{}),
	}

	stdoutFile, err := os.OpenFile(cfg.StdoutPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open stdout file: %v", err)
	}

	stderrFile, err := os.OpenFile(cfg.StderrPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open stderr file: %v", err)
	}

	syslogCtx, cancel := context.WithCancel(d.ctx)
	h.syslogCancel = cancel
	d.logger.Trace("Starting log streaming", "stdout_path", cfg.StdoutPath, "stderr_path", cfg.StderrPath)

	// Start the streaming in a goroutine that handles file closing
	go func() {
		defer stdoutFile.Close()
		defer stderrFile.Close()

		// Run syslog streaming with retry/backoff until it connects or context cancels
		d.streamSyslogWithRetry(syslogCtx, vmConfig, stdoutFile, stderrFile)
	}()
	d.tasks.Set(cfg.ID, h)
	go h.run()

	// Return a driver handle
	return handle, nil, nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *Driver) RecoverTask(h *drivers.TaskHandle) error {
	if h == nil {
		return fmt.Errorf("error: handle cannot be nil")
	}

	if h.Version != taskHandleVersion {
		return fmt.Errorf("error: incompatible handle version of %d", h.Version)
	}

	var taskState TaskState
	if err := h.GetDriverState(&taskState); err != nil {
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	// Start the task with the previous configuration.
	d.logger.Info("recovered tart task", "task_id", h.Config.ID)
	_, _, err := d.StartTask(taskState.TaskConfig)
	if err != nil {
		return fmt.Errorf("failed to start task: %v", err)
	}

	return nil
}

// WaitTask returns a channel used to notify Nomad when a task exits.
func (d *Driver) WaitTask(ctx context.Context, taskID string) (<-chan *drivers.ExitResult, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	ch := make(chan *drivers.ExitResult)
	go d.handleWait(ctx, handle, ch)
	return ch, nil
}

// StopTask stops a running task with the given signal and within the timeout window.
func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	allocVMName := d.generateVMName(handle.taskConfig.AllocID)

	// Attempt to gracefully stop the VM via the virtualizer
	var taskConfig TaskConfig
	if err := handle.taskConfig.DecodeDriverConfig(&taskConfig); err == nil {
		if err := d.client.Stop(d.ctx, allocVMName, timeout); err != nil {
			d.logger.Warn("failed to stop VM via virtualizer", "error", err)
		}

		if err := d.client.Delete(d.ctx, allocVMName); err != nil {
			d.logger.Warn("failed to delete VM via virtualizer", "error", err)
		}
	}

	if err := handle.exec.Shutdown(signal, timeout); err != nil {
		if handle.pluginClient != nil && handle.pluginClient.Exited() {
			return nil
		}
		return fmt.Errorf("executor Shutdown failed: %v", err)
	}

	<-handle.doneCh
	handle.pluginClient.Kill()

	d.logger.Info("stopped tart task", "task_id", taskID)
	return nil
}

// DestroyTask cleans up and removes a task that has terminated.
func (d *Driver) DestroyTask(taskID string, force bool) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	if handle.IsRunning() && !force {
		return fmt.Errorf("cannot destroy running task")
	}

	if !handle.pluginClient.Exited() {
		if err := handle.exec.Shutdown("", 0); err != nil {
			handle.logger.Error("destroying executor failed", "error", err)
		}
		handle.pluginClient.Kill()
	}

	d.tasks.Delete(taskID)
	d.logger.Info("destroyed tart task", "task_id", taskID)
	return nil
}

// InspectTask returns detailed status information for the referenced taskID.
func (d *Driver) InspectTask(taskID string) (*drivers.TaskStatus, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	return handle.TaskStatus(), nil
}

// TaskStats returns a channel which the driver should send stats to at the given interval.
func (d *Driver) TaskStats(ctx context.Context, taskID string, interval time.Duration) (<-chan *drivers.TaskResourceUsage, error) {
	h, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}
	return h.exec.Stats(ctx, interval)
}

// TaskEvents returns a channel that the plugin can use to emit task related events.
func (d *Driver) TaskEvents(ctx context.Context) (<-chan *drivers.TaskEvent, error) {
	return d.eventer.TaskEvents(ctx)
}

// SignalTask forwards a signal to a task.
func (d *Driver) SignalTask(taskID string, signal string) error {
	_, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	// TODO: Implement actual VM signaling logic
	d.logger.Info("signaling tart task", "task_id", taskID, "signal", signal)
	return nil
}

// ExecTask returns the result of executing the given command inside a task.
func (d *Driver) ExecTask(taskID string, cmd []string, timeout time.Duration) (*drivers.ExecTaskResult, error) {
	_, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	// Exec is not supported
	return nil, fmt.Errorf("exec is not supported by the tart driver")
}

// ExecTaskStreaming executes a command inside the VM backing the allocation and
// streams the input and output over the provided ExecOptions. The VM is
// contacted over SSH and the session will remain active for the lifetime of the
// context.
func (d *Driver) ExecTaskStreaming(ctx context.Context, taskID string, opts *drivers.ExecOptions) (*drivers.ExitResult, error) {
	defer opts.Stdout.Close()
	defer opts.Stderr.Close()
	defer opts.Stdin.Close()

	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	var taskCfg TaskConfig
	if err := handle.taskConfig.DecodeDriverConfig(&taskCfg); err != nil {
		return nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	execOptions := ExecOptions{
		Command:  opts.Command,
		Tty:      opts.Tty,
		Stdin:    opts.Stdin,
		Stdout:   opts.Stdout,
		Stderr:   opts.Stderr,
		ResizeCh: opts.ResizeCh,
	}

	vmConfig := VMConfig{
		TaskConfig:  taskCfg,
		NomadConfig: handle.taskConfig,
	}

	exitCode, err := d.client.Exec(ctx, vmConfig, execOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to exec command: %v", err)
	}

	return &drivers.ExitResult{ExitCode: exitCode}, nil
}

func (d *Driver) generateVMName(allocationID string) string {
	return fmt.Sprintf("nomad-%s", allocationID)
}

// streamSyslogWithRetry attempts to start syslog streaming inside the VM using
// SSH and retries with exponential backoff until it succeeds or the context is
// cancelled. It can take a little while for the VM to become responsive so retrying
// is critical to making this reliant.
func (d *Driver) streamSyslogWithRetry(ctx context.Context, vmConfig VMConfig, stdout, stderr io.WriteCloser) {
	backoff := 1 * time.Second
	maxBackoff := 10 * time.Second

	for {
		// allow cancellation between attempts
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Attempt to start log streaming over SSH
		_, err := d.client.Exec(ctx, vmConfig, ExecOptions{
			Command: []string{"/usr/bin/log", "stream", "--style", "syslog", "--level=info"},
			Stdout:  stdout,
			Stderr:  stderr,
			Tty:     false,
		})

		if err != nil {
			// Check if we should give up due to cancellation
			select {
			case <-ctx.Done():
				return
			default:
			}

			d.logger.Warn("Log streaming failed; will retry", "error", err)
			time.Sleep(backoff)
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		// Exec returned without error; streaming ended or succeeded then exited.
		return
	}
}

func (d *Driver) TartEnvList(tc *drivers.TaskConfig) []string {
	// Patch the env list to include the homebrew paths to help tart
	// find other binaries (like softnet) as needed.
	list := tc.EnvList()
	list = append(list, "PATH=/opt/homebrew/bin:/opt/homebrew/sbin")

	return list
}
