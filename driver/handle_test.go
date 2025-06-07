package driver

import (
	"testing"
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestTaskHandleTaskStatus(t *testing.T) {
	t.Parallel()
	h := &taskHandle{
		taskConfig: &drivers.TaskConfig{ID: "id", Name: "name"},
		state:      drivers.TaskStateRunning,
		pid:        123,
		startedAt:  time.Now(),
		exitResult: &drivers.ExitResult{ExitCode: 0},
	}

	st := h.TaskStatus()
	if st.ID != "id" || st.Name != "name" {
		t.Fatalf("unexpected task info: %#v", st)
	}
	if st.DriverAttributes["pid"] != "123" {
		t.Fatalf("unexpected pid attribute: %v", st.DriverAttributes["pid"])
	}
	if st.State != drivers.TaskStateRunning {
		t.Fatalf("unexpected state: %v", st.State)
	}
}

func TestTaskHandleIsRunning(t *testing.T) {
	t.Parallel()
	h := &taskHandle{state: drivers.TaskStateRunning}
	if !h.IsRunning() {
		t.Fatalf("expected running")
	}
	h.state = drivers.TaskStateExited
	if h.IsRunning() {
		t.Fatalf("expected not running")
	}
}
