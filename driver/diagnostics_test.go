package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// Eventer can write from its own goroutine, including during test cleanup.
type diagnosticBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *diagnosticBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestStartTaskDiagnosticsDoNotExposeConfigSecrets(t *testing.T) {
	for _, level := range []hclog.Level{hclog.Trace, hclog.Debug, hclog.Info, hclog.Warn, hclog.Error} {
		for _, pullOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pull_only=%t", level, pullOnly), func(t *testing.T) {
				var output diagnosticBuffer
				logger := hclog.New(&hclog.LoggerOptions{Level: level, Output: &output, JSONFormat: true})
				drv := NewTartDriver(logger).(*Driver)
				t.Cleanup(drv.signalShutdown)
				// Stop before any Tart process/executor is created; exercise the real
				// decoded-config log for both VM and prefetch error paths.
				drv.client = &testClient{mockBuilder: mockBuilder{
					needsDLErr:  errors.New("image probe unavailable"),
					registryErr: errors.New("registry unavailable"),
				}}
				cfg := &drivers.TaskConfig{ID: "task-safe", Name: "worker", AllocID: "alloc-safe"}
				driverCfg := TaskConfig{
					URL: "ghcr.io/example/image:stable", SSHUser: "admin",
					SSHPassword: "sentinel-ssh-password", PullOnly: pullOnly,
					Auth:    Auth{Username: "ci", Password: "sentinel-registry-password"},
					Command: "sentinel-sensitive-command", Args: []string{"sentinel-sensitive-argument"},
				}
				if err := cfg.EncodeConcreteDriverConfig(driverCfg); err != nil {
					t.Fatal(err)
				}
				if _, _, err := drv.StartTask(cfg); err == nil {
					t.Fatal("expected injected early failure")
				}
				logged := output.String()
				for _, secret := range []string{driverCfg.SSHPassword, driverCfg.Auth.Password, driverCfg.Command, driverCfg.Args[0]} {
					if strings.Contains(logged, secret) {
						t.Errorf("driver diagnostics exposed a sensitive config value at %s", level)
					}
				}
				if level <= hclog.Info {
					for _, want := range []string{"starting tart task", "task-safe", "alloc-safe", "worker"} {
						if !strings.Contains(logged, want) {
							t.Errorf("missing safe diagnostic context %q", want)
						}
					}
				}
			})
		}
	}
}

func TestStartupDiagnosticsDoNotExposeCommandOrBackendErrors(t *testing.T) {
	for _, level := range []hclog.Level{hclog.Trace, hclog.Debug, hclog.Info, hclog.Warn, hclog.Error} {
		for _, outcome := range []string{"completed", "failed", "retry"} {
			t.Run(fmt.Sprintf("%s/%s", level, outcome), func(t *testing.T) {
				var output diagnosticBuffer
				logger := hclog.New(&hclog.LoggerOptions{Level: level, Output: &output, JSONFormat: true})
				drv := NewTartDriver(logger).(*Driver)
				t.Cleanup(drv.signalShutdown)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				events, err := drv.TaskEvents(ctx)
				if err != nil {
					t.Fatal(err)
				}
				command := []string{"sentinel-private-command", "--token=sentinel-private-argument"}
				var calls int
				drv.client = &testClient{execFn: func(_ context.Context, _ VMConfig, opts ExecOptions) (int, error) {
					calls++
					if !reflect.DeepEqual(opts.Command, command) {
						t.Error("diagnostic filtering changed the executed command")
					}
					if outcome == "failed" {
						return -1, errors.New("sentinel-backend-detail " + strings.Join(command, " "))
					}
					if outcome == "retry" && calls == 1 {
						return -1, fmt.Errorf("%w: sentinel-backend-detail %s", errSSHDialFailed, command[1])
					}
					return 7, nil
				}}
				vm := VMConfig{Driver: TaskConfig{Command: command[0], Args: command[1:]}}
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					drv.executeStartupCommand(ctx, "task-safe", "worker", "alloc-safe", vm,
						nopWriteCloser{&strings.Builder{}}, nopWriteCloser{&strings.Builder{}})
				}()
				var received []*drivers.TaskEvent
				for len(received) < 2 {
					select {
					case event := <-events:
						received = append(received, event)
					case <-ctx.Done():
						t.Fatal("timed out waiting for startup diagnostics")
					}
				}
				select {
				case <-finished:
				case <-ctx.Done():
					t.Fatal("startup did not return")
				}
				combined := output.String()
				for _, event := range received {
					combined += fmt.Sprintf("%+v", event)
					if event.TaskID != "task-safe" || event.TaskName != "worker" || event.AllocID != "alloc-safe" {
						t.Error("startup event lost safe task context")
					}
				}
				for _, secret := range []string{command[0], command[1], "sentinel-backend-detail"} {
					if strings.Contains(combined, secret) {
						t.Error("startup log or event exposed command/backend details")
					}
				}
				terminal := received[1]
				if outcome == "failed" {
					if terminal.Message != "Startup command failed" || terminal.Annotations["error_kind"] != "execution_failed" {
						t.Error("missing safe startup failure classification")
					}
				} else if terminal.Message != "Startup command completed" || terminal.Annotations["exit_code"] != "7" {
					t.Error("missing startup exit diagnostic")
				}
				wantCalls := 1
				if outcome == "retry" {
					wantCalls = 2
				}
				if calls != wantCalls {
					t.Errorf("Exec calls = %d, want %d", calls, wantCalls)
				}
			})
		}
	}
}
