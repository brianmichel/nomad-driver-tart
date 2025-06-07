package driver

import "testing"

func TestTaskStoreSetGetDelete(t *testing.T) {
	t.Parallel()
	ts := newTaskStore()
	if _, ok := ts.Get("foo"); ok {
		t.Fatalf("expected no entry for foo")
	}

	h := &taskHandle{}
	ts.Set("foo", h)

	if got, ok := ts.Get("foo"); !ok || got != h {
		t.Fatalf("unexpected handle returned")
	}

	ts.Delete("foo")
	if _, ok := ts.Get("foo"); ok {
		t.Fatalf("expected entry to be deleted")
	}
}
