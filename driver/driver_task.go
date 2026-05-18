package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

func (d *Driver) createExecutor(cfg *drivers.TaskConfig, handle *drivers.TaskHandle) (executor.Executor, *plugin.Client, error) {
	pluginLogFile := filepath.Join(cfg.TaskDir().Dir, "executor.out")
	execConfig := &executor.ExecutorConfig{
		LogFile:  pluginLogFile,
		LogLevel: "debug",
	}

	logger := d.logger.With("task_name", handle.Config.Name, "alloc_id", handle.Config.AllocID)
	execImpl, pluginClient, err := executor.CreateExecutor(logger, d.nomadConfig, execConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create executor: %w", err)
	}
	return execImpl, pluginClient, nil
}

func openTaskLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

func (d *Driver) resolveDriverNetwork(vmConfig VMConfig) (*drivers.DriverNetwork, error) {
	ctx, cancel := context.WithTimeout(d.ctx, 45*time.Second)
	defer cancel()

	ip, err := d.waitForIPAddress(ctx, vmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to determine VM IP for driver network override: %w", err)
	}

	return &drivers.DriverNetwork{IP: ip}, nil
}

func (d *Driver) emitTaskEvent(cfg *drivers.TaskConfig, msg string, annotations map[string]string) {
	d.eventer.EmitEvent(&drivers.TaskEvent{
		TaskID:      cfg.ID,
		TaskName:    cfg.Name,
		AllocID:     cfg.AllocID,
		Timestamp:   time.Now(),
		Message:     msg,
		Annotations: annotations,
	})
}

// startPullOnlyTask runs `tart pull <url>` via the executor so that the image
// is cached locally on the Nomad client. No VM is created; the task
// completes as soon as the pull exits.
func (d *Driver) startPullOnlyTask(cfg *drivers.TaskConfig, vmConfig VMConfig, handle *drivers.TaskHandle) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	d.logger.Info("starting tart pull-only task", "url", vmConfig.Driver.URL)

	if _, err := d.client.PrepareRegistryEnv(d.ctx, vmConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare registry env: %w", err)
	}

	d.emitTaskEvent(cfg, "Pulling VM image", map[string]string{
		"url": vmConfig.Driver.URL,
	})

	execImpl, pluginClient, err := d.createExecutor(cfg, handle)
	if err != nil {
		return nil, nil, err
	}

	execCmd := &executor.ExecCommand{
		Cmd:              "tart",
		Args:             d.client.BuildPullArgs(vmConfig),
		Env:              tartEnvList(cfg),
		User:             cfg.User,
		TaskDir:          cfg.TaskDir().Dir,
		StdoutPath:       cfg.StdoutPath,
		StderrPath:       cfg.StderrPath,
		NetworkIsolation: cfg.NetworkIsolation,
	}

	ps, err := execImpl.Launch(execCmd)
	if err != nil {
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to launch pull: %w", err)
	}

	state := driverState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
		PullOnly:   true,
	}
	handle.State = drivers.TaskStateRunning
	if err := handle.SetDriverState(&state); err != nil {
		execImpl.Shutdown("", 0)
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to set driver state: %w", err)
	}

	h := &taskHandle{
		exec:         execImpl,
		pluginClient: pluginClient,
		pid:          ps.Pid,
		taskConfig:   cfg,
		state:        drivers.TaskStateRunning,
		startedAt:    time.Now(),
		logger:       d.logger,
		doneCh:       make(chan struct{}),
		pullOnly:     true,
	}

	d.tasks.Set(cfg.ID, h)
	go h.run()

	return handle, nil, nil
}

// StartTask returns a task handle and a driver network if necessary.
func (d *Driver) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var taskConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&taskConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %w", err)
	}

	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	vmConfig := VMConfig{
		Driver: taskConfig,
		Nomad:  cfg,
	}
	vmConfig.Driver.Directories = resolveDirectoryMounts(cfg, vmConfig.Driver.Directories)
	d.logger.Info("starting tart task", "task_cfg", hclog.Fmt("%+v", vmConfig.Driver))
	if exposures := nomadPortExposures(cfg); len(exposures) > 0 {
		d.logger.Debug("found Nomad allocated port mappings for Tart networking", "ports", hclog.Fmt("%+v", exposures))
	}

	if taskConfig.PullOnly {
		return d.startPullOnlyTask(cfg, vmConfig, handle)
	}

	if taskConfig.SSHUser == "" || taskConfig.SSHPassword == "" {
		return nil, nil, fmt.Errorf("ssh_user and ssh_password are required unless pull_only = true")
	}

	needsDownload, err := d.client.NeedsImageDownload(d.ctx, vmConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check image availability: %w", err)
	}
	if needsDownload {
		d.logger.Info("VM image not found locally, downloading", "url", taskConfig.URL)
		d.emitTaskEvent(cfg, "Downloading VM image", map[string]string{
			"url": taskConfig.URL,
		})
	}

	if _, err := d.client.Setup(d.ctx, vmConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to setup VM: %w", err)
	}

	if needsDownload {
		d.emitTaskEvent(cfg, "VM image download complete", map[string]string{
			"url": taskConfig.URL,
		})
	}

	execImpl, pluginClient, err := d.createExecutor(cfg, handle)
	if err != nil {
		return nil, nil, err
	}

	args, err := d.client.BuildStartArgs(vmConfig)
	if err != nil {
		pluginClient.Kill()
		return nil, nil, err
	}

	execCmd := &executor.ExecCommand{
		Cmd:              "tart",
		Args:             args,
		Env:              tartEnvList(cfg),
		User:             cfg.User,
		TaskDir:          cfg.TaskDir().Dir,
		StdoutPath:       cfg.StdoutPath,
		StderrPath:       cfg.StderrPath,
		NetworkIsolation: cfg.NetworkIsolation,
	}

	ps, err := execImpl.Launch(execCmd)
	if err != nil {
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to launch VM: %w", err)
	}

	// Store the driver state on the handle
	state := driverState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
	}

	handle.State = drivers.TaskStateRunning

	// Encode the driver state
	if err := handle.SetDriverState(&state); err != nil {
		execImpl.Shutdown("", 0)
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to set driver state: %w", err)
	}

	networkOverride, err := d.resolveDriverNetwork(vmConfig)
	if err != nil {
		d.logger.Warn("failed to determine driver network override; continuing without one", "task_id", cfg.ID, "error", err)
	}

	h := &taskHandle{
		exec:            execImpl,
		pluginClient:    pluginClient,
		pid:             ps.Pid,
		taskConfig:      cfg,
		state:           drivers.TaskStateRunning,
		startedAt:       time.Now(),
		logger:          d.logger,
		doneCh:          make(chan struct{}),
		networkOverride: networkOverride.Copy(),
	}

	stdoutFile, err := openTaskLog(cfg.StdoutPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open stdout file: %w", err)
	}

	stderrFile, err := openTaskLog(cfg.StderrPath)
	if err != nil {
		stdoutFile.Close()
		return nil, nil, fmt.Errorf("failed to open stderr file: %w", err)
	}

	d.tasks.Set(cfg.ID, h)

	// If a startup command is configured, run it once SSH is available.
	if taskConfig.Command != "" || len(taskConfig.Args) > 0 {
		startupCtx, startupCancel := context.WithCancel(d.ctx)
		h.startupCancel = startupCancel
		go func() {
			defer startupCancel()
			defer stdoutFile.Close()
			defer stderrFile.Close()
			d.executeStartupCommand(startupCtx, cfg.ID, cfg.Name, cfg.AllocID,
				vmConfig, stdoutFile, stderrFile)
		}()
	} else {
		stdoutFile.Close()
		stderrFile.Close()
	}

	go h.run()

	// Return a driver handle
	return handle, networkOverride.Copy(), nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *Driver) RecoverTask(h *drivers.TaskHandle) error {
	if h == nil {
		return fmt.Errorf("error: handle cannot be nil")
	}

	if h.Version != taskHandleVersion {
		return fmt.Errorf("error: incompatible handle version of %d", h.Version)
	}

	var taskState driverState
	if err := h.GetDriverState(&taskState); err != nil {
		return fmt.Errorf("failed to decode task state from handle: %w", err)
	}

	// Start the task with the previous configuration.
	d.logger.Info("recovered tart task", "task_id", h.Config.ID)
	_, _, err := d.StartTask(taskState.TaskConfig)
	if err != nil {
		return fmt.Errorf("failed to start task: %w", err)
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

	var allocVMName string
	if handle.taskConfig != nil && !handle.pullOnly {
		allocVMName = vmName(handle.taskConfig.AllocID)
		if err := d.client.Stop(d.ctx, allocVMName, timeout); err != nil {
			d.logger.Warn("failed to stop VM via virtualizer", "task_id", taskID, "error", err)
		}
	} else if handle.taskConfig == nil {
		d.logger.Warn("task config missing while stopping task", "task_id", taskID)
	}

	if err := handle.exec.Shutdown(signal, timeout); err != nil {
		if handle.pluginClient != nil && handle.pluginClient.Exited() {
			return nil
		}
		return fmt.Errorf("executor Shutdown failed: %w", err)
	}

	<-handle.doneCh

	if handle.pluginClient != nil {
		handle.pluginClient.Kill()
	} else {
		handle.logger.Warn("plugin client missing while stopping task")
	}

	if allocVMName != "" {
		if err := d.client.Delete(d.ctx, allocVMName); err != nil {
			d.logger.Warn("failed to delete VM via virtualizer", "task_id", taskID, "error", err)
		}
	}

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

	if handle.pluginClient != nil && !handle.pluginClient.Exited() {
		if handle.exec != nil {
			if err := handle.exec.Shutdown("", 0); err != nil {
				handle.logger.Error("destroying executor failed", "error", err)
			}
		} else {
			handle.logger.Warn("executor missing while destroying task")
		}
		handle.pluginClient.Kill()
	}

	if handle.taskConfig != nil && !handle.pullOnly {
		allocVMName := vmName(handle.taskConfig.AllocID)
		if err := d.client.Delete(d.ctx, allocVMName); err != nil {
			d.logger.Warn("failed to delete VM via virtualizer", "task_id", taskID, "error", err)
		}
	} else if handle.taskConfig == nil {
		d.logger.Warn("task config missing while destroying task", "task_id", taskID)
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
		return nil, fmt.Errorf("failed to decode driver config: %w", err)
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
		Driver: taskCfg,
		Nomad:  handle.taskConfig,
	}

	exitCode, err := d.client.Exec(ctx, vmConfig, execOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to exec command: %w", err)
	}

	return &drivers.ExitResult{ExitCode: exitCode}, nil
}
