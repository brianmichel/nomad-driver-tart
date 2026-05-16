package driver

import (
	"testing"

	"github.com/hashicorp/go-hclog"
)

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
