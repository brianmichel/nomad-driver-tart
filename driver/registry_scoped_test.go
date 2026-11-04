package driver

import (
	"context"
	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
)

type capturingPullExecutor struct {
	stubExecutor
	command executor.ExecCommand
}

func (e *capturingPullExecutor) Launch(cmd *executor.ExecCommand) (*executor.ProcessState, error) {
	e.command = *cmd
	e.command.Env = append([]string{}, cmd.Env...)
	return &executor.ProcessState{Pid: 1234}, nil
}
func (e *capturingPullExecutor) Wait(context.Context) (*executor.ProcessState, error) {
	return &executor.ProcessState{ExitCode: 0, Time: time.Now()}, nil
}

// Exercise the actual driver's preparation -> ExecCommand -> Launch path.
// The executor is a test double; this does not qualify Nomad RPC or real Tart.
func TestPullExecutorUsesPreparedRegistryEnvironment(t *testing.T) {
	t.Setenv("TART_REGISTRY_HOSTNAME", "parent.example")
	t.Setenv("TART_REGISTRY_USERNAME", "parent-user")
	t.Setenv("TART_REGISTRY_PASSWORD", "parent-test-sentinel")
	drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
	defer drv.signalShutdown()
	drv.client = NewTartClient(hclog.NewNullLogger())
	captured := &capturingPullExecutor{}
	drv.executorFactory = func(*drivers.TaskConfig, *drivers.TaskHandle) (executor.Executor, *plugin.Client, error) {
		return captured, nil, nil
	}
	cfg := &drivers.TaskConfig{ID: "pull-task", Name: "pull", AllocID: "pull-allocation", Env: map[string]string{"JOB_MARKER": "preserved", "TART_REGISTRY_PASSWORD": "job-conflict"}}
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg
	vm := VMConfig{Nomad: cfg, Driver: TaskConfig{URL: "ghcr.io/test/image:tag", PullOnly: true, Auth: Auth{Username: "task-user", Password: "task-test-sentinel"}}}
	if _, _, err := drv.startPullOnlyTask(cfg, vm, handle); err != nil {
		t.Fatal(err)
	}
	h, _ := drv.tasks.Get(cfg.ID)
	<-h.doneCh
	if captured.command.Cmd != "tart" || len(captured.command.Args) < 2 || captured.command.Args[0] != "pull" {
		t.Fatal("pull executor command was not launched")
	}
	want := map[string]string{"TART_REGISTRY_HOSTNAME": "ghcr.io", "TART_REGISTRY_USERNAME": "task-user", "TART_REGISTRY_PASSWORD": "task-test-sentinel", "JOB_MARKER": "preserved"}
	counts := map[string]int{}
	for _, entry := range captured.command.Env {
		key, value, _ := strings.Cut(entry, "=")
		if expected, ok := want[key]; ok {
			counts[key]++
			if value != expected {
				t.Errorf("incorrect executor environment value for %s", key)
			}
		}
	}
	for key := range want {
		if counts[key] != 1 {
			t.Errorf("expected exactly one executor variable for %s; count=%d", key, counts[key])
		}
	}
}

func TestPullExecutorNoAuthKeepsTaskEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name             string
		host, user, pass string
		task             map[string]string
	}{
		{"empty", "", "", "", nil},
		{"inherited", "parent.example", "parent-user", "parent-sentinel", nil},
		{"partial", "parent.example", "", "parent-sentinel", map[string]string{"TART_REGISTRY_USERNAME": "job-user"}},
		{"conflicting", "parent.example", "parent-user", "parent-sentinel", map[string]string{"TART_REGISTRY_HOSTNAME": "job.example", "TART_REGISTRY_PASSWORD": "job-sentinel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TART_REGISTRY_HOSTNAME", tc.host)
			t.Setenv("TART_REGISTRY_USERNAME", tc.user)
			t.Setenv("TART_REGISTRY_PASSWORD", tc.pass)
			drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
			defer drv.signalShutdown()
			drv.client = NewTartClient(hclog.NewNullLogger())
			captured := &capturingPullExecutor{}
			drv.executorFactory = func(*drivers.TaskConfig, *drivers.TaskHandle) (executor.Executor, *plugin.Client, error) {
				return captured, nil, nil
			}
			cfg := &drivers.TaskConfig{ID: "pull-" + tc.name, Name: "pull", AllocID: "allocation", Env: tc.task}
			handle := drivers.NewTaskHandle(taskHandleVersion)
			handle.Config = cfg
			vm := VMConfig{Nomad: cfg, Driver: TaskConfig{URL: "ghcr.io/test/image", PullOnly: true}}
			if _, _, err := drv.startPullOnlyTask(cfg, vm, handle); err != nil {
				t.Fatal(err)
			}
			h, _ := drv.tasks.Get(cfg.ID)
			<-h.doneCh
			got := map[string]string{}
			for _, entry := range captured.command.Env {
				k, v, _ := strings.Cut(entry, "=")
				if strings.HasPrefix(k, "TART_REGISTRY_") {
					got[k] = v
				}
			}
			for _, k := range []string{"TART_REGISTRY_HOSTNAME", "TART_REGISTRY_USERNAME", "TART_REGISTRY_PASSWORD"} {
				v, ok := tc.task[k]
				actual, present := got[k]
				if ok != present || (ok && v != actual) {
					t.Errorf("%s: task presence=%v launch presence=%v", k, ok, present)
				}
			}
		})
	}
}

func TestPullExecutorPartialAuthFailsBeforeLaunch(t *testing.T) {
	for _, auth := range []Auth{{Username: "user"}, {Password: "sentinel"}} {
		t.Run(auth.Username, func(t *testing.T) {
			t.Setenv("TART_REGISTRY_PASSWORD", "parent-sentinel")
			drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
			defer drv.signalShutdown()
			drv.client = NewTartClient(hclog.NewNullLogger())
			launched := false
			drv.executorFactory = func(*drivers.TaskConfig, *drivers.TaskHandle) (executor.Executor, *plugin.Client, error) {
				launched = true
				return &capturingPullExecutor{}, nil, nil
			}
			cfg := &drivers.TaskConfig{ID: "partial", Env: map[string]string{}}
			handle := drivers.NewTaskHandle(taskHandleVersion)
			handle.Config = cfg
			_, _, err := drv.startPullOnlyTask(cfg, VMConfig{Nomad: cfg, Driver: TaskConfig{URL: "ghcr.io/team/image", Auth: auth}}, handle)
			if err == nil || launched {
				t.Fatal("partial auth must fail before executor launch")
			}
		})
	}
}

type checkingRunner struct{ user, password string }

func (r checkingRunner) Run(ctx context.Context, name string, args ...string) *exec.Cmd {
	if len(args) > 0 && args[0] == "clone" {
		return exec.CommandContext(ctx, "sh", "-c", `test "$TART_REGISTRY_HOSTNAME" = ghcr.io && test "$TART_REGISTRY_USERNAME" = "$1" && test "$TART_REGISTRY_PASSWORD" = "$2"`, "check", r.user, r.password)
	}
	return exec.CommandContext(ctx, "true")
}

func TestConcurrentCloneEnvironmentsStaySeparate(t *testing.T) {
	var wg sync.WaitGroup
	for _, user := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			c := &tartCLI{logger: testLogger(t), runner: checkingRunner{user: user, password: user + "-password"}}
			_, err := c.Setup(context.Background(), VMConfig{Driver: TaskConfig{URL: "ghcr.io/team/image", Auth: Auth{Username: user, Password: user + "-password"}}, Nomad: &drivers.TaskConfig{AllocID: user}})
			if err != nil {
				t.Errorf("clone for %s failed: %v", user, err)
			}
		}(user)
	}
	wg.Wait()
}

type secretErrorRunner struct{}

func (*secretErrorRunner) Run(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "printf very-secret >&2; exit 1")
}

func TestSetupDoesNotExposeRegistryCredentialsFromStderr(t *testing.T) {
	r := &secretErrorRunner{}
	c := &tartCLI{logger: testLogger(t), runner: r}
	_, err := c.Setup(context.Background(), VMConfig{Driver: TaskConfig{URL: "ghcr.io/team/image", Auth: Auth{Username: "user", Password: "very-secret"}}, Nomad: &drivers.TaskConfig{AllocID: "a"}})
	if err == nil {
		t.Fatal("expected clone failure")
	}
	if strings.Contains(err.Error(), "very-secret") {
		t.Fatal("clone error exposed credentials")
	}
}

func TestRegistryAuthRejectsPartialCredentials(t *testing.T) {
	c := &tartCLI{logger: testLogger(t), runner: &testRunner{}}
	for _, auth := range []Auth{{Username: "user"}, {Password: "password"}} {
		_, err := c.PrepareRegistryEnv(context.Background(), VMConfig{Driver: TaskConfig{URL: "ghcr.io/team/image", Auth: auth}})
		if err == nil {
			t.Fatal("partial auth must not fall back to inherited credentials")
		}
	}
}

// Expectations follow Tart 2.41.1 RegistryHostTests: URL.host retains case,
// while URL.port is rendered as a canonical decimal integer.
func TestRegistryAuthUsesNormalizedProviderHost(t *testing.T) {
	for _, tc := range []struct{ ref, host string }{
		{"localhost:05000/team/image:tag", "localhost:5000"},
		{"127.0.0.1:05000/team/image@sha256:abcd", "127.0.0.1:5000"},
		{"Docker.IO/team/image:tag", "Docker.IO"},
		{"DOCKER.IO:00443/team/image:tag", "DOCKER.IO:443"},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			c := &tartCLI{logger: testLogger(t), runner: &testRunner{}}
			cfg := VMConfig{Driver: TaskConfig{URL: tc.ref, Auth: Auth{Username: "user", Password: "sentinel"}}}
			env, err := c.PrepareRegistryEnv(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, entry := range env {
				if strings.HasPrefix(entry, "TART_REGISTRY_HOSTNAME=") {
					count++
					if entry != "TART_REGISTRY_HOSTNAME="+tc.host {
						t.Errorf("wrong provider hostname: %s", entry)
					}
				}
			}
			if count != 1 {
				t.Fatalf("hostname count: %d", count)
			}
			if args := c.BuildPullArgs(cfg); len(args) != 2 || args[1] != tc.ref {
				t.Fatal("reference was rewritten")
			}
		})
	}
}

func TestRegistryAuthRejectsAmbiguousHostSyntax(t *testing.T) {
	for _, ref := range []string{"https://ghcr.io/a", "[::1]:5000/a", "user@host/a", "host_name/a", "host..example/a", "host:abc/a", "host:65536/a", "host:0/a"} {
		t.Run(ref, func(t *testing.T) {
			c := &tartCLI{logger: testLogger(t), runner: &testRunner{}}
			_, err := c.PrepareRegistryEnv(context.Background(), VMConfig{Driver: TaskConfig{URL: ref, Auth: Auth{Username: "user", Password: "sentinel"}}})
			if err == nil {
				t.Fatal("ambiguous host must fail closed")
			}
		})
	}
}

func TestRegistryAuthOverridesInheritedEnvironmentWithoutLogin(t *testing.T) {
	t.Setenv("TART_REGISTRY_HOSTNAME", "other.example")
	t.Setenv("TART_REGISTRY_USERNAME", "host-user")
	t.Setenv("TART_REGISTRY_PASSWORD", "host-password")
	r := &testRunner{}
	c := &tartCLI{logger: testLogger(t), runner: r}
	cfg := VMConfig{Driver: TaskConfig{URL: "ghcr.io/team/image:latest", Auth: Auth{Username: "task-user", Password: "task-password"}}, Nomad: &drivers.TaskConfig{Env: map[string]string{"TART_REGISTRY_PASSWORD": "injected"}}}
	env, err := c.PrepareRegistryEnv(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Fatal("registry preparation must not invoke tart login")
	}
	want := map[string]string{"TART_REGISTRY_HOSTNAME": "ghcr.io", "TART_REGISTRY_USERNAME": "task-user", "TART_REGISTRY_PASSWORD": "task-password"}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if expected, found := want[k]; ok && found {
			if v != expected {
				t.Fatalf("unexpected value for %s", k)
			}
			delete(want, k)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing registry variables: %v", want)
	}
}
