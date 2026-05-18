package driver

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// taskStore is an in-memory datastore for taskHandles
type taskStore struct {
	store map[string]*taskHandle
	lock  sync.RWMutex
}

// newTaskStore returns a new task store
func newTaskStore() *taskStore {
	return &taskStore{
		store: map[string]*taskHandle{},
	}
}

// Set stores a task handle
func (ts *taskStore) Set(id string, handle *taskHandle) {
	ts.lock.Lock()
	defer ts.lock.Unlock()
	ts.store[id] = handle
}

// Get retrieves a task handle
func (ts *taskStore) Get(id string) (*taskHandle, bool) {
	ts.lock.RLock()
	defer ts.lock.RUnlock()
	handle, ok := ts.store[id]
	return handle, ok
}

// Delete removes a task handle
func (ts *taskStore) Delete(id string) {
	ts.lock.Lock()
	defer ts.lock.Unlock()
	delete(ts.store, id)
}

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

	// startupCancel cancels the startup command goroutine when the
	// task stops or is destroyed.
	startupCancel context.CancelFunc

	// exitResult is the result of the task
	exitResult *drivers.ExitResult

	// logger is the logger for the task
	logger hclog.Logger

	// doneCh is closed when the task has finished executing
	doneCh chan struct{}

	// pullOnly indicates this task is a short-lived image prefetch; the
	// driver must skip VM-lifecycle operations (run/stop/delete) for it.
	pullOnly bool

	// networkOverride is the driver-provided network metadata used by
	// Nomad service registrations with address_mode = "driver".
	networkOverride *drivers.DriverNetwork
}

// TaskStatus returns the current status of the task
func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	var exitResult *drivers.ExitResult
	if h.exitResult != nil {
		copy := *h.exitResult
		exitResult = &copy
	}

	status := &drivers.TaskStatus{
		ID:              h.taskConfig.ID,
		Name:            h.taskConfig.Name,
		State:           h.state,
		StartedAt:       h.startedAt,
		CompletedAt:     h.completedAt,
		ExitResult:      exitResult,
		NetworkOverride: h.networkOverride.Copy(),
		DriverAttributes: map[string]string{
			// No custom attributes for now, but something like the task PID could be useful.
			"pid": strconv.Itoa(h.pid),
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
	// Keep the startup command alive for the lifetime of the task and only
	// cancel it once the backing VM task exits.
	if h.startupCancel != nil {
		defer h.startupCancel()
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

func (d *Driver) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)

	var result *drivers.ExitResult
	ps, err := handle.exec.Wait(ctx)
	if err != nil {
		result = &drivers.ExitResult{
			Err: fmt.Errorf("executor: error waiting on process: %w", err),
		}
	} else {
		result = &drivers.ExitResult{
			ExitCode: ps.ExitCode,
			Signal:   ps.Signal,
		}
	}

	select {
	case <-ctx.Done():
	case <-d.ctx.Done():
	case ch <- result:
	}
}
