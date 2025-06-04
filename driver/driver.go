package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/brianmichel/nomad-driver-tart/virtualizer"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	"github.com/hashicorp/nomad/plugins/shared/structs"
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

	// configSpec is the hcl specification returned by the ConfigSchema RPC
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		// Config options can be specified here
		"enabled": hclspec.NewDefault(
			hclspec.NewAttr("enabled", "bool", false),
			hclspec.NewLiteral("true"),
		),
	})

	// taskConfigSpec is the hcl specification for the driver config section of
	// a task within a job. It is returned in the TaskConfigSchema RPC
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"url":  hclspec.NewAttr("url", "string", true),
		"name": hclspec.NewAttr("name", "string", true),
		"command": hclspec.NewDefault(
			hclspec.NewAttr("command", "string", false),
			hclspec.NewLiteral(`""`),
		),
		"args": hclspec.NewDefault(
			hclspec.NewAttr("args", "list(string)", false),
			hclspec.NewLiteral(`[]`),
		),
	})

	// capabilities is returned by the Capabilities RPC and indicates what
	// optional features this driver supports
	capabilities = &drivers.Capabilities{
		SendSignals: true,
		Exec:        false,
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

	// virtualizer is the interface for interacting with virtual machines
	virtualizer virtualizer.Virtualizer
}

// Config is the driver configuration set by the SetConfig RPC call
type Config struct {
	// Enabled is set to true to enable the tart driver
	Enabled bool `codec:"enabled"`
}

// TaskConfig is the driver configuration of a task within a job
type TaskConfig struct {
	URL     string   `codec:"url"`
	Name    string   `codec:"name"`
	Command string   `codec:"command"`
	Args    []string `codec:"args"`
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
	virtualizer := virtualizer.NewTartClient(logger)

	return &Driver{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &Config{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
		virtualizer:    virtualizer,
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

// handleFingerprint manages the channel and the flow of fingerprint data.
func (d *Driver) handleFingerprint(ctx context.Context, ch chan<- *drivers.Fingerprint) {
	defer close(ch)

	// Nomad expects the initial fingerprint to be sent immediately
	ticker := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			// After the initial fingerprint we can set the proper fingerprint
			// period
			ticker.Reset(fingerprintPeriod)
			ch <- d.buildFingerprint()
		}
	}
}

// buildFingerprint returns the driver's fingerprint data
func (d *Driver) buildFingerprint() *drivers.Fingerprint {
	fp := &drivers.Fingerprint{
		Attributes:        map[string]*structs.Attribute{},
		Health:            drivers.HealthStateHealthy,
		HealthDescription: "healthy",
	}

	// Set driver attributes
	fp.Attributes["driver.tart"] = structs.NewBoolAttribute(true)

	// Check if the driver is enabled
	if !d.config.Enabled {
		fp.Health = drivers.HealthStateUndetected
		fp.HealthDescription = "disabled"
		return fp
	}

	// Check if virtualization software is installed and accessible
	ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
	defer cancel()

	if err := d.virtualizer.IsInstalled(ctx); err != nil {
		d.logger.Warn("failed to find virtualization software", "error", err)
		fp.Health = drivers.HealthStateUnhealthy
		fp.HealthDescription = "virtualization software not found"
		return fp
	}

	// Get the Tart version and add it as an attribute
	version, err := d.virtualizer.GetVersion(ctx)
	if err == nil && version != "" {
		fp.Attributes["driver.tart.version"] = structs.NewStringAttribute(version)
	}

	// Try to list VMs to verify virtualization software is working properly
	_, err = d.virtualizer.ListVMs(ctx)
	if err != nil {
		d.logger.Warn("failed to list VMs", "error", err)
		fp.Health = drivers.HealthStateUnhealthy
		fp.HealthDescription = fmt.Sprintf("failed to list VMs: %v", err)
		return fp
	}

	return fp
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

	d.logger.Info("starting tart task", "driver_cfg", hclog.Fmt("%+v", taskConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	// Store the driver state on the handle
	state := TaskState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
	}

	// Encode the driver state
	if err := handle.SetDriverState(&state); err != nil {
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	handle.State = drivers.TaskStateRunning

	// Store the task in the in-memory datastore
	d.tasks.Set(cfg.ID, &taskHandle{
		taskConfig: cfg,
		state:      drivers.TaskStateRunning,
		startedAt:  time.Now(),
		logger:     d.logger,
	})

	// Let the virtualizer do whatever might be needed to set up the VM image to be ready to execute it.
	if err := d.virtualizer.SetupVM(d.ctx, taskConfig.Name, taskConfig.URL); err != nil {
		return nil, nil, fmt.Errorf("failed to setup VM: %v", err)
	}

	if err := d.virtualizer.RunVM(d.ctx, taskConfig.Name, false); err != nil {
		return nil, nil, fmt.Errorf("failed to run VM: %v", err)
	}

	// Return a driver handle
	return handle, nil, nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *Driver) RecoverTask(handle *drivers.TaskHandle) error {
	if handle == nil {
		return fmt.Errorf("error: handle cannot be nil")
	}

	if handle.Version != taskHandleVersion {
		return fmt.Errorf("error: incompatible handle version of %d", handle.Version)
	}

	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	// TODO: Implement task recovery logic
	// For now, just create a basic task handle
	d.tasks.Set(handle.Config.ID, &taskHandle{
		taskConfig: taskState.TaskConfig,
		state:      drivers.TaskStateRunning,
		startedAt:  taskState.StartedAt,
		logger:     d.logger,
	})

	d.logger.Info("recovered tart task", "task_id", handle.Config.ID)
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

func (d *Driver) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)

	// TODO: Implement actual VM monitoring
	// For now, just simulate a running task that completes after a while
	select {
	case <-ctx.Done():
		return
	case <-d.ctx.Done():
		return
	case <-time.After(30 * time.Second):
		// Simulate task completion
		handle.state = drivers.TaskStateExited
		handle.completedAt = time.Now()
		handle.exitResult = &drivers.ExitResult{
			ExitCode: 0,
			Signal:   0,
		}
		ch <- handle.exitResult
	}
}

// StopTask stops a running task with the given signal and within the timeout window.
func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	if err := d.virtualizer.StopVM(d.ctx, handle.taskConfig.Name, timeout); err != nil {
		return fmt.Errorf("failed to stop VM: %v", err)
	}

	// TODO: Implement actual VM stopping logic
	// For now, just update the task state
	handle.state = drivers.TaskStateExited
	handle.completedAt = time.Now()
	handle.exitResult = &drivers.ExitResult{
		ExitCode: 0,
		Signal:   0,
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

	// TODO: Implement actual VM cleanup logic
	// For now, just remove the task from the store
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
	_, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	// TODO: Implement actual VM stats collection
	// For now, just return a placeholder
	ch := make(chan *drivers.TaskResourceUsage)
	go d.handleStats(ctx, ch, interval)
	return ch, nil
}

func (d *Driver) handleStats(ctx context.Context, ch chan *drivers.TaskResourceUsage, interval time.Duration) {
	defer close(ch)
	timer := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-timer.C:
			// Send placeholder stats
			ch <- &drivers.TaskResourceUsage{
				ResourceUsage: &drivers.ResourceUsage{
					MemoryStats: &drivers.MemoryStats{
						RSS:      0,
						Measured: []string{"RSS"},
					},
					CpuStats: &drivers.CpuStats{
						SystemMode: 0,
						UserMode:   0,
						Measured:   []string{"System Mode", "User Mode"},
					},
				},
				Timestamp: time.Now().UTC().UnixNano(),
			}
			timer.Reset(interval)
		}
	}
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
