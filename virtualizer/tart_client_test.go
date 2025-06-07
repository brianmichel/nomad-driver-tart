package virtualizer

import "testing"

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
