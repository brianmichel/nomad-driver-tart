package driver

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/drivers"
)

type recordingRunner struct {
	name   string
	args   []string
	stdout string
	stderr string
	exit   int
}

func slicesContainSequence(haystack, needle []string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if reflect.DeepEqual(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func (r *recordingRunner) Run(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.name = name
	r.args = append([]string(nil), args...)
	script := "printf '%s' \"$TART_TEST_STDOUT\"; printf '%s' \"$TART_TEST_STDERR\" >&2; exit \"$TART_TEST_EXIT\""
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Env = append(cmd.Environ(),
		"TART_TEST_STDOUT="+r.stdout,
		"TART_TEST_STDERR="+r.stderr,
		"TART_TEST_EXIT="+strconv.Itoa(r.exit),
	)
	return cmd
}

func TestBuildPullArgs(t *testing.T) {
	c := NewTartClient(hclog.NewNullLogger())
	url := "ghcr.io/cirruslabs/macos-sequoia-base:latest"
	args := c.BuildPullArgs(VMConfig{Driver: TaskConfig{URL: url}})

	if len(args) != 2 || args[0] != "pull" || args[1] != url {
		t.Fatalf("unexpected pull args: %v", args)
	}
}

func TestConvertTartStatus(t *testing.T) {
	cases := map[string]VMState{
		"running": VMStateRunning,
		"Running": VMStateRunning,
		"paused":  VMStatePaused,
		"PAUSED":  VMStatePaused,
		"stopped": VMStateStopped,
		"unknown": VMStateStopped,
	}

	for input, expected := range cases {
		input := input
		expected := expected
		t.Run(input, func(t *testing.T) {
			if got := convertTartStatus(input); got != expected {
				t.Fatalf("status %s: expected %s got %s", input, expected, got)
			}
		})
	}
}

func TestShellQuoteCommand(t *testing.T) {
	got := shellQuoteCommand([]string{"/bin/bash", "/Volumes/My Shared Files/alloc/startup.sh", "O'Hare"})
	want := "'/bin/bash' '/Volumes/My Shared Files/alloc/startup.sh' 'O'\\''Hare'"
	if got != want {
		t.Fatalf("unexpected quoted command:\nwant: %s\n got: %s", want, got)
	}
}

func TestIPAddressUsesDefaultResolverForNonBridgedNetworking(t *testing.T) {
	r := &recordingRunner{stdout: "192.168.64.10\n"}
	c := &tartCLI{logger: hclog.NewNullLogger(), runner: r}

	ip, err := c.IPAddress(context.Background(), "vm-1", &NetworkConfig{Mode: "shared"})
	if err != nil {
		t.Fatalf("IPAddress returned error: %v", err)
	}
	if ip != "192.168.64.10" {
		t.Fatalf("expected IP 192.168.64.10, got %q", ip)
	}
	if r.name != "tart" {
		t.Fatalf("expected tart command, got %q", r.name)
	}
	wantArgs := []string{"ip", "vm-1"}
	if !reflect.DeepEqual(r.args, wantArgs) {
		t.Fatalf("unexpected args: want %v got %v", wantArgs, r.args)
	}
}

func TestIPAddressUsesARPResolverForBridgedNetworking(t *testing.T) {
	r := &recordingRunner{stdout: "10.0.0.55\n"}
	c := &tartCLI{logger: hclog.NewNullLogger(), runner: r}

	ip, err := c.IPAddress(context.Background(), "vm-bridge", &NetworkConfig{Mode: "bridged"})
	if err != nil {
		t.Fatalf("IPAddress returned error: %v", err)
	}
	if ip != "10.0.0.55" {
		t.Fatalf("expected IP 10.0.0.55, got %q", ip)
	}
	wantArgs := []string{"ip", "--resolver=arp", "vm-bridge"}
	if !reflect.DeepEqual(r.args, wantArgs) {
		t.Fatalf("unexpected args: want %v got %v", wantArgs, r.args)
	}
}

func TestBuildStartArgsAddsSoftnetExposeFromNomadPorts(t *testing.T) {
	c := &tartCLI{logger: hclog.NewNullLogger(), runner: &recordingRunner{}}
	ports := structs.AllocatedPorts{{Label: "http", Value: 21043, To: 8000, HostIP: "0.0.0.0"}}
	cfg := VMConfig{
		Driver: TaskConfig{
			Network: &NetworkConfig{Mode: "softnet", SoftnetAllow: []string{"0.0.0.0/0"}},
		},
		Nomad: &drivers.TaskConfig{
			AllocID: "alloc-1",
			Resources: &drivers.Resources{
				Ports: &ports,
			},
		},
	}

	args, err := c.BuildStartArgs(cfg)
	if err != nil {
		t.Fatalf("BuildStartArgs returned error: %v", err)
	}
	if !reflect.DeepEqual(args[:3], []string{"run", "nomad-alloc-1", "--no-graphics"}) {
		t.Fatalf("unexpected leading args: %v", args)
	}
	if !slicesContainSequence(args, []string{"--net-softnet", "--net-softnet-allow", "0.0.0.0/0", "--net-softnet-expose", "21043:8000"}) {
		t.Fatalf("expected softnet expose args in %v", args)
	}
}

func TestBuildStartArgsDeduplicatesNumericExplicitForwards(t *testing.T) {
	for _, mode := range []string{"softnet", "", "default", "shared", "nat"} {
		for _, explicit := range []string{"1234:80,21043:8000", "021043:08000"} {
			t.Run("mode="+mode+"/explicit="+explicit, func(t *testing.T) {
				ports := structs.AllocatedPorts{{Label: "http", Value: 21043, To: 8000}, {Label: "other", Value: 22000, To: 9000}}
				network := &NetworkConfig{Mode: mode, SoftnetExpose: []string{explicit}}
				cfg := VMConfig{Driver: TaskConfig{Network: network}, Nomad: &drivers.TaskConfig{AllocID: "a", Resources: &drivers.Resources{Ports: &ports}}}
				c := &tartCLI{logger: hclog.NewNullLogger(), runner: &recordingRunner{}}
				args, err := c.BuildStartArgs(cfg)
				if err != nil {
					t.Fatalf("BuildStartArgs returned error: %v", err)
				}
				if !slicesContainSequence(args, []string{"--net-softnet-expose", explicit + ",22000:9000"}) {
					t.Fatalf("expected original explicit forward and one distinct allocation, got %v", args)
				}
				if !reflect.DeepEqual(network.SoftnetExpose, []string{explicit}) {
					t.Fatalf("original explicit forwards changed: %v", network.SoftnetExpose)
				}
			})
		}
	}
}

func TestBuildStartArgsRejectsSpecificAllocatedHostIP(t *testing.T) {
	ports := structs.AllocatedPorts{{Label: "http", Value: 21043, To: 8000, HostIP: "127.0.0.1"}}
	c := &tartCLI{logger: hclog.NewNullLogger(), runner: &recordingRunner{}}
	_, err := c.BuildStartArgs(VMConfig{Driver: TaskConfig{Network: &NetworkConfig{SoftnetAllow: []string{"10.0.0.0/8"}}}, Nomad: &drivers.TaskConfig{AllocID: "a", Resources: &drivers.Resources{Ports: &ports}}})
	if err == nil {
		t.Fatal("expected rejection of unsupported specific host IP")
	}
}

func TestStartTaskRejectsUnsupportedHostIPBeforeSetup(t *testing.T) {
	ports := structs.AllocatedPorts{{Label: "http", Value: 21043, To: 8000, HostIP: "127.0.0.1"}}
	mock := &testClient{mockLifecycle: mockLifecycle{setupErr: fmt.Errorf("setup reached")}}
	drv := testDriverWithMock(t, mock)
	cfg := &drivers.TaskConfig{ID: "task", AllocID: "alloc", Resources: &drivers.Resources{Ports: &ports}}
	if err := cfg.EncodeConcreteDriverConfig(TaskConfig{SSHUser: "user", SSHPassword: "password", Network: &NetworkConfig{Mode: "softnet"}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := drv.StartTask(cfg)
	if err == nil || !strings.Contains(err.Error(), "host IP") {
		t.Fatalf("expected host IP validation error, got %v", err)
	}
	if mock.setupCalled {
		t.Fatal("setup ran before network validation")
	}
}

func TestStartTaskRejectsConflictingSoftnetForwardsBeforeSetup(t *testing.T) {
	for _, mode := range []string{"softnet", "", "default", "shared", "nat"} {
		t.Run("mode="+mode, func(t *testing.T) {
			ports := structs.AllocatedPorts{{Label: "http", Value: 21043, To: 8000}}
			mock := &testClient{mockLifecycle: mockLifecycle{setupErr: fmt.Errorf("setup reached")}}
			drv := testDriverWithMock(t, mock)
			cfg := &drivers.TaskConfig{ID: "task", AllocID: "alloc", Resources: &drivers.Resources{Ports: &ports}}
			if err := cfg.EncodeConcreteDriverConfig(TaskConfig{SSHUser: "user", SSHPassword: "password", Network: &NetworkConfig{Mode: mode, SoftnetExpose: []string{"21043:22"}}}); err != nil {
				t.Fatal(err)
			}
			_, _, err := drv.StartTask(cfg)
			if mock.probeCalled || mock.setupCalled || mock.cloneCalled {
				t.Errorf("validation occurred after image probe/setup/clone: %+v", mock)
			}
			if err == nil || !strings.Contains(err.Error(), "21043") || !strings.Contains(err.Error(), "conflict") {
				t.Fatalf("expected port conflict, got %v", err)
			}
		})
	}
}

func TestStartTaskRejectsConflictingAllocatedSoftnetForwardsBeforeSetup(t *testing.T) {
	ports := structs.AllocatedPorts{{Label: "one", Value: 21043, To: 22}, {Label: "two", Value: 21043, To: 8000}}
	mock := &testClient{mockLifecycle: mockLifecycle{setupErr: fmt.Errorf("setup reached")}}
	drv := testDriverWithMock(t, mock)
	cfg := &drivers.TaskConfig{ID: "task", AllocID: "alloc", Resources: &drivers.Resources{Ports: &ports}}
	if err := cfg.EncodeConcreteDriverConfig(TaskConfig{SSHUser: "user", SSHPassword: "password", Network: &NetworkConfig{Mode: "softnet"}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := drv.StartTask(cfg)
	if mock.probeCalled || mock.setupCalled || mock.cloneCalled {
		t.Error("image probe/setup/clone reached")
	}
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict, got %v", err)
	}
}
