package driver

import (
	"testing"

	"github.com/hashicorp/go-hclog"
)

func TestBuildPrewarmArgs(t *testing.T) {
	c := NewTartClient(hclog.NewNullLogger())
	url := "ghcr.io/cirruslabs/macos-sequoia-base:latest"
	args := c.BuildPrewarmArgs(VMConfig{TaskConfig: TaskConfig{URL: url}})

	if len(args) != 2 || args[0] != "pull" || args[1] != url {
		t.Fatalf("unexpected prewarm args: %v", args)
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
