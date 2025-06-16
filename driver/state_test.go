package driver

import (
	"sync"
	"testing"
)

func TestTaskStoreSetGetDelete(t *testing.T) {
	t.Parallel()
	ts := newTaskStore()

	// Test Get on non-existent key
	if _, ok := ts.Get("nonexistent"); ok {
		t.Fatalf("expected no entry for nonexistent key")
	}

	// Test Set and Get
	h := &taskHandle{}
	ts.Set("test1", h)

	// Test Get on existing key
	got, ok := ts.Get("test1")
	if !ok || got != h {
		t.Fatalf("unexpected handle returned, got %v, want %v", got, h)
	}

	// Test overwrite
	h2 := &taskHandle{}
	ts.Set("test1", h2)
	if got, _ := ts.Get("test1"); got != h2 {
		t.Fatalf("expected handle to be overwritten")
	}

	// Test Delete
	ts.Delete("test1")
	if _, ok := ts.Get("test1"); ok {
		t.Fatalf("expected entry to be deleted")
	}

	// Test Delete non-existent key (should not panic)
	ts.Delete("nonexistent")
}

func TestTaskStoreConcurrentAccess(t *testing.T) {
	t.Parallel()
	ts := newTaskStore()
	const numRoutines = 100
	const numKeys = 10

	t.Logf("Starting concurrent test with %d goroutines and %d unique keys", numRoutines, numKeys)

	// Create a wait group to synchronize goroutines
	var wg sync.WaitGroup

	// Create a map to track the last handle we expect for each key
	expectedHandles := make(map[string]*taskHandle)
	var mu sync.Mutex

	// Start multiple goroutines to set values
	for i := 0; i < numRoutines; i++ {
		key := string(rune('A' + (i % numKeys)))
		h := &taskHandle{}

		// Update our expected handle to be the last one we create for each key
		mu.Lock()
		expectedHandles[key] = h
		mu.Unlock()

		wg.Add(1)
		go func(k string, handle *taskHandle) {
			defer wg.Done()
			ts.Set(k, handle)
		}(key, h)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	t.Log("Verifying all values were set correctly")

	// Verify all values were set correctly
	for key := range expectedHandles {
		got, ok := ts.Get(key)
		if !ok {
			t.Errorf("key %s was not found in the store", key)
			continue
		}
		// We only care that we got a valid handle, not which one specifically
		if got == nil {
			t.Errorf("got nil handle for key %s, expected non-nil", key)
		}
	}

	t.Log("Starting concurrent delete operations")

	// Test concurrent deletes
	for key := range expectedHandles {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			ts.Delete(k)
		}(key)
	}

	wg.Wait()

	t.Log("Verifying all values were deleted")

	// Verify all values were deleted
	for key := range expectedHandles {
		if _, ok := ts.Get(key); ok {
			t.Errorf("expected key %s to be deleted but it still exists", key)
		}
	}

	t.Log("Concurrent test completed successfully")
}

func TestTaskStoreNewTaskStore(t *testing.T) {
	t.Parallel()
	ts := newTaskStore()

	if ts == nil {
		t.Fatal("expected newTaskStore to return a non-nil taskStore")
	}

	if len(ts.store) != 0 {
		t.Errorf("expected new taskStore to be empty, got %d items", len(ts.store))
	}
}

func TestTaskStoreNilHandle(t *testing.T) {
	t.Parallel()
	ts := newTaskStore()

	// Test setting nil handle
	ts.Set("nil-handle", nil)

	got, ok := ts.Get("nil-handle")
	if !ok || got != nil {
		t.Errorf("expected nil handle, got %v, ok=%v", got, ok)
	}

	// Test deleting nil handle
	ts.Delete("nil-handle")
	if _, ok := ts.Get("nil-handle"); ok {
		t.Error("expected nil handle to be deleted")
	}
}
