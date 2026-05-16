package driver

import (
	"context"
	"os/exec"
	"reflect"
	"strconv"
	"testing"

	"github.com/hashicorp/go-hclog"
)

type recordingRunner struct {
	name   string
	args   []string
	stdout string
	stderr string
	exit   int
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
