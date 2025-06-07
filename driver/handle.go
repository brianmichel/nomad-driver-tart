package driver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// taskHandle is a handle to a running task
type taskHandle struct {
	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	// taskConfig is the task configuration from the job
	taskConfig *drivers.TaskConfig

	// state is the state of the task
	state drivers.TaskState

	// pid is the PID of the task
	pid int

	// exec is the Nomad executor managing the task process
	exec executor.Executor

	// pluginClient is the go-plugin client associated with the executor
	pluginClient *plugin.Client

	// startedAt is when the task was started
	startedAt time.Time

	// completedAt is when the task exited
	completedAt time.Time

	// syslogCancel cancels the syslog streaming goroutine
	syslogCancel context.CancelFunc

	// exitResult is the result of the task
	exitResult *drivers.ExitResult

	// logger is the logger for the task
	logger hclog.Logger

	// doneCh is closed when the task has finished executing
	doneCh chan struct{}
}

// TaskStatus returns the current status of the task
func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	status := &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.state,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			// No custom attributes for now, but something like the task PID could be useful.
			"pid": fmt.Sprintf("%d", h.pid),
		},
	}

	return status
}

// IsRunning returns whether the task is running
func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.state == drivers.TaskStateRunning
}

// run waits on the executor and updates the task state when the process exits.
func (h *taskHandle) run() {
	defer close(h.doneCh)
	if h.syslogCancel != nil {
		defer h.syslogCancel()
	}

	h.stateLock.Lock()
	if h.exitResult == nil {
		h.exitResult = &drivers.ExitResult{}
	}
	h.stateLock.Unlock()

	ps, err := h.exec.Wait(context.Background())

	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	if err != nil {
		h.exitResult.Err = err
		h.state = drivers.TaskStateUnknown
		h.completedAt = time.Now()
		return
	}

	h.state = drivers.TaskStateExited
	h.exitResult.ExitCode = ps.ExitCode
	h.exitResult.Signal = ps.Signal
	h.completedAt = ps.Time
}
