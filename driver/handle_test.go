package driver

import (
	"context"
	"os"
	"testing"
	"time"

	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/shared/executor"
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

type blockingExecutor struct {
	waitCh chan struct{}
}

func (b *blockingExecutor) Launch(*executor.ExecCommand) (*executor.ProcessState, error) {
	return nil, nil
}
func (b *blockingExecutor) Wait(context.Context) (*executor.ProcessState, error) {
	<-b.waitCh
	return &executor.ProcessState{ExitCode: 0, Time: time.Now()}, nil
}
func (b *blockingExecutor) Shutdown(string, time.Duration) error     { return nil }
func (b *blockingExecutor) UpdateResources(*drivers.Resources) error { return nil }
func (b *blockingExecutor) Version() (*executor.ExecutorVersion, error) {
	return &executor.ExecutorVersion{}, nil
}
func (b *blockingExecutor) Stats(context.Context, time.Duration) (<-chan *cstructs.TaskResourceUsage, error) {
	return nil, nil
}
func (b *blockingExecutor) Signal(os.Signal) error { return nil }
func (b *blockingExecutor) Exec(time.Time, string, []string) ([]byte, int, error) {
	return nil, 0, nil
}
func (b *blockingExecutor) ExecStreaming(context.Context, []string, bool, drivers.ExecTaskStream) error {
	return nil
}

func TestTaskHandleRunDefersStartupCancelUntilExit(t *testing.T) {
	t.Parallel()

	cancelled := make(chan struct{})
	exec := &blockingExecutor{waitCh: make(chan struct{})}
	h := &taskHandle{
		taskConfig: &drivers.TaskConfig{ID: "id", Name: "name"},
		state:      drivers.TaskStateRunning,
		exec:       exec,
		doneCh:     make(chan struct{}),
		startupCancel: func() {
			close(cancelled)
		},
	}

	go h.run()

	select {
	case <-cancelled:
		t.Fatal("startup was cancelled before task exit")
	case <-time.After(50 * time.Millisecond):
	}

	close(exec.waitCh)

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("startup was not cancelled after task exit")
	}
}
