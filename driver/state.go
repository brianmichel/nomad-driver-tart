package driver

import "sync"

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
