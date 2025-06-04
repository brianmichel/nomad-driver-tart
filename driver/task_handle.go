package driver

import (
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
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

	// startedAt is when the task was started
	startedAt time.Time

	// completedAt is when the task exited
	completedAt time.Time

	// exitResult is the result of the task
	exitResult *drivers.ExitResult

	// logger is the logger for the task
	logger hclog.Logger
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
			"pid": "0", // Placeholder for now
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
