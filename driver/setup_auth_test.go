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

	var loginIdx, cloneIdx int
	foundLogin := false
	for i, call := range runner.calls {
		if call.Name == "tart" && len(call.Args) > 0 {
			if call.Args[0] == "login" {
				loginIdx = i
				foundLogin = true
			}
			if call.Args[0] == "clone" {
				cloneIdx = i
			}
		}
	}
	if !foundLogin {
		t.Fatalf("expected a login invocation, got %v", runner.calls)
	}
	loginArgs := runner.calls[loginIdx].Args
	foundUser := false
	for i := 0; i < len(loginArgs)-1; i++ {
		if loginArgs[i] == "--username" && loginArgs[i+1] == "user1" {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Fatalf("--username user1 not found in login args: %v", loginArgs)
	}
	if cloneIdx == 0 && len(runner.calls) < 2 {
		t.Fatalf("expected a clone invocation, got %v", runner.calls)
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
