package driver

import (
	"context"
	"os/exec"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// testRunner records command invocations for assertions.
type testRunner struct {
	calls []testRunnerCall
}

type testRunnerCall struct {
	Name string
	Args []string
}

func (r *testRunner) Run(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.calls = append(r.calls, testRunnerCall{Name: name, Args: args})
	return exec.CommandContext(ctx, "true") // no-op command
}

func TestSetup_UsesTaskAuthAndEnv(t *testing.T) {
	runner := &testRunner{}
	c := &tartCLI{
		logger: testLogger(t),
		runner: runner,
	}

	vmc := VMConfig{
		Driver: TaskConfig{
			URL:  "ghcr.io/example/private:latest",
			Auth: Auth{Username: "user1", Password: "pass1"},
		},
		Nomad: &drivers.TaskConfig{AllocID: "alloc-123"},
	}

	if _, err := c.Setup(context.Background(), vmc); err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}

	for _, call := range runner.calls {
		if call.Name == "tart" && len(call.Args) > 0 && call.Args[0] == "login" {
			t.Fatal("task auth must not invoke tart login")
		}
	}
	if len(runner.calls) == 0 || runner.calls[0].Args[0] != "clone" {
		t.Fatal("expected clone invocation")
	}
}

func TestSetup_NoTaskAuth_UsesEnvAndSkipsLogin(t *testing.T) {
	runner := &testRunner{}
	c := &tartCLI{
		logger: testLogger(t),
		runner: runner,
	}

	vmc := VMConfig{
		Driver: TaskConfig{
			URL: "ghcr.io/example/private:latest",
		},
		Nomad: &drivers.TaskConfig{AllocID: "alloc-abc"},
	}

	if _, err := c.Setup(context.Background(), vmc); err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}

	var sawLogin bool
	var foundClone bool
	for _, call := range runner.calls {
		if call.Name == "tart" && len(call.Args) > 0 {
			if call.Args[0] == "login" {
				sawLogin = true
			}
			if call.Args[0] == "clone" {
				foundClone = true
			}
		}
	}
	if sawLogin {
		t.Fatalf("did not expect a login invocation when Auth is not provided")
	}
	if !foundClone {
		t.Fatalf("expected a clone invocation, got %v", runner.calls)
	}
}

func testLogger(tb testing.TB) hclog.Logger {
	tb.Helper()
	return hclog.New(&hclog.LoggerOptions{Level: hclog.Off})
}
