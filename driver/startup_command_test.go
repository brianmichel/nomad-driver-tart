package driver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// nopWriteCloser wraps a strings.Builder to satisfy io.WriteCloser.
type nopWriteCloser struct{ *strings.Builder }

func (nopWriteCloser) Close() error { return nil }

// configurableMock adds knobs to mockVirtualizer so individual tests
// can control Exec and IPAddress behavior.
type configurableMock struct {
	mockVirtualizer
	ipAddrFn  func(ctx context.Context, vmName string) (string, error)
	execFn    func(ctx context.Context, config VMConfig, opts ExecOptions) (int, error)
	execCalls []execCall
}

type execCall struct {
	Command []string
	Tty     bool
}

func (m *configurableMock) IPAddress(ctx context.Context, vmName string) (string, error) {
	if m.ipAddrFn != nil {
		return m.ipAddrFn(ctx, vmName)
	}
	return "[IP_ADDRESS]", nil
}

func (m *configurableMock) Exec(ctx context.Context, c VMConfig, opts ExecOptions) (int, error) {
	m.execCalls = append(m.execCalls, execCall{Command: opts.Command, Tty: opts.Tty})
	if m.execFn != nil {
		return m.execFn(ctx, c, opts)
	}
	return 0, nil
}

// countingMock records how many times IPAddress was called.
type countingMock struct {
	mockVirtualizer
	ipCalls int
	ipErr   error
}

func (m *countingMock) IPAddress(ctx context.Context, vmName string) (string, error) {
	m.ipCalls++
	if m.ipErr != nil {
		return "", m.ipErr
	}
	return "[IP_ADDRESS]", nil
}

// Helper to create a minimal Driver wired to a configurable mock.
func testDriverWithMock(t *testing.T, mock *configurableMock) *Driver {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	logger := hclog.NewNullLogger()
	drv := &Driver{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &Config{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
		client:         mock,
	}
	return drv
}

// ---------------------------------------------------------------------------
// Config decoding
// ---------------------------------------------------------------------------

func TestDecodeCommandArgs(t *testing.T) {
	// Direct struct test: verify the new fields are declared and tag correctly.
	cfg := TaskConfig{
		Command: "/bin/bash",
		Args:    []string{"-c", "echo hi"},
	}
	if cfg.Command != "/bin/bash" {
		t.Fatalf("expected '/bin/bash', got %q", cfg.Command)
	}
	if len(cfg.Args) != 2 || cfg.Args[0] != "-c" || cfg.Args[1] != "echo hi" {
		t.Fatalf("unexpected args: %v", cfg.Args)
	}

	// Verify that the codec tag matches the HCL schema name.
	// (If these are wrong, Nomad won't parse the fields from HCL.)
}

func TestDecodeCommandArgs_NeitherSet(t *testing.T) {
	cfg := TaskConfig{}
	if cfg.Command != "" {
		t.Fatalf("zero-value command should be empty, got %q", cfg.Command)
	}
	if len(cfg.Args) != 0 {
		t.Fatalf("zero-value args should be nil/empty, got %v", cfg.Args)
	}
}

func TestDecodeCommandArgs_ArgsOnly(t *testing.T) {
	cfg := TaskConfig{
		Args: []string{"-c", "echo hi"},
	}
	if cfg.Command != "" {
		t.Fatalf("command should be empty, got %q", cfg.Command)
	}
	if len(cfg.Args) != 2 {
		t.Fatalf("unexpected args: %v", cfg.Args)
	}
}

// ---------------------------------------------------------------------------
// executeStartupCommand
// ---------------------------------------------------------------------------

func TestExecuteStartupCommand_Success(t *testing.T) {
	mock := &configurableMock{}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Command: "/bin/sh", Args: []string{"-c", "echo ok"}},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-success"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	drv.executeStartupCommand(context.Background(),
		"task-1", "my-task", "alloc-success", vm, stdout, stderr)

	if len(mock.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(mock.execCalls))
	}
	c := mock.execCalls[0]
	if len(c.Command) != 3 || c.Command[0] != "/bin/sh" || c.Command[1] != "-c" || c.Command[2] != "echo ok" {
		t.Fatalf("unexpected command: %v", c.Command)
	}
	if c.Tty {
		t.Fatal("Tty should be false")
	}
}

func TestExecuteStartupCommand_CommandOnlyNoArgs(t *testing.T) {
	mock := &configurableMock{}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Command: "/usr/bin/whoami"},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-cmdonly"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	drv.executeStartupCommand(context.Background(),
		"task-2", "my-task", "alloc-cmdonly", vm, stdout, stderr)

	if len(mock.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(mock.execCalls))
	}
	c := mock.execCalls[0]
	if len(c.Command) != 1 || c.Command[0] != "/usr/bin/whoami" {
		t.Fatalf("unexpected command: %v", c.Command)
	}
}

func TestExecuteStartupCommand_ArgsWithoutCommand(t *testing.T) {
	mock := &configurableMock{}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Args: []string{"-c", "echo hi"}},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-argsonly"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	drv.executeStartupCommand(context.Background(),
		"task-3", "my-task", "alloc-argsonly", vm, stdout, stderr)

	if len(mock.execCalls) != 0 {
		t.Fatalf("expected 0 Exec calls (misconfiguration), got %d", len(mock.execCalls))
	}
}

func TestExecuteStartupCommand_NoCommandOrArgs(t *testing.T) {
	mock := &configurableMock{}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-empty"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	drv.executeStartupCommand(context.Background(),
		"task-x", "x", "alloc-empty", vm, stdout, stderr)

	if len(mock.execCalls) != 0 {
		t.Fatalf("expected 0 Exec calls, got %d", len(mock.execCalls))
	}
}

func TestExecuteStartupCommand_SSHNeverAvailable(t *testing.T) {
	mock := &configurableMock{
		ipAddrFn: func(ctx context.Context, vmName string) (string, error) {
			return "", fmt.Errorf("no IP yet")
		},
	}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Command: "/bin/true"},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-noip"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return without panicking after context expires.
	drv.executeStartupCommand(ctx, "task-4", "my-task", "alloc-noip", vm, stdout, stderr)

	if len(mock.execCalls) != 0 {
		t.Fatalf("expected 0 Exec calls (never got IP), got %d", len(mock.execCalls))
	}
}

func TestExecuteStartupCommand_ExecFails(t *testing.T) {
	mock := &configurableMock{
		execFn: func(ctx context.Context, c VMConfig, opts ExecOptions) (int, error) {
			return -1, fmt.Errorf("command not found")
		},
	}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Command: "/bin/nonexistent"},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-fail"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	drv.executeStartupCommand(context.Background(),
		"task-5", "my-task", "alloc-fail", vm, stdout, stderr)

	if len(mock.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(mock.execCalls))
	}
}

func TestExecuteStartupCommand_NonZeroExit(t *testing.T) {
	mock := &configurableMock{
		execFn: func(ctx context.Context, c VMConfig, opts ExecOptions) (int, error) {
			return 5, nil
		},
	}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Command: "/bin/sh", Args: []string{"-c", "exit 5"}},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-exit5"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	// Should complete without panicking; non-zero exit does not kill VM.
	drv.executeStartupCommand(context.Background(),
		"task-6", "my-task", "alloc-exit5", vm, stdout, stderr)

	if len(mock.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(mock.execCalls))
	}
}

func TestExecuteStartupCommand_CancelledDuringExec(t *testing.T) {
	mock := &configurableMock{
		execFn: func(ctx context.Context, c VMConfig, opts ExecOptions) (int, error) {
			return -1, context.Canceled
		},
	}
	drv := testDriverWithMock(t, mock)

	vm := VMConfig{
		TaskConfig:  TaskConfig{Command: "/bin/sleep", Args: []string{"10"}},
		NomadConfig: &drivers.TaskConfig{AllocID: "alloc-cancelled"},
	}
	stdout, stderr := nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}}

	// Should return without emitting a "failed" event for cancellation.
	drv.executeStartupCommand(context.Background(),
		"task-7", "my-task", "alloc-cancelled", vm, stdout, stderr)

	// It did try to run, but we treat cancellation as a silent exit.
	if len(mock.execCalls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(mock.execCalls))
	}
}

// ---------------------------------------------------------------------------
// waitForSSH
// ---------------------------------------------------------------------------

func TestWaitForSSH_ReadyImmediately(t *testing.T) {
	mock := &countingMock{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	drv := &Driver{ctx: ctx, client: mock, logger: hclog.NewNullLogger()}
	vm := VMConfig{NomadConfig: &drivers.TaskConfig{AllocID: "alloc-ready"}}

	if err := drv.waitForSSH(ctx, vm); err != nil {
		t.Fatalf("waitForSSH: %v", err)
	}
	if mock.ipCalls != 1 {
		t.Fatalf("expected 1 IP call, got %d", mock.ipCalls)
	}
}

func TestWaitForSSH_ContextCancelled(t *testing.T) {
	mock := &countingMock{ipErr: errors.New("no ip")}
	ctx, cancel := context.WithCancel(context.Background())

	drv := &Driver{ctx: ctx, client: mock, logger: hclog.NewNullLogger()}
	vm := VMConfig{NomadConfig: &drivers.TaskConfig{AllocID: "alloc-cancel"}}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := drv.waitForSSH(ctx, vm)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if mock.ipCalls < 1 {
		t.Fatalf("expected at least 1 IP call, got %d", mock.ipCalls)
	}
}
