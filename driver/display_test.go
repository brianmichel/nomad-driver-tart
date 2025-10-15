package driver

import "testing"

func TestFormatDisplayResolution_NilConfig(t *testing.T) {
	res, err := formatDisplayResolution(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "" {
		t.Fatalf("expected empty resolution for nil display config, got %q", res)
	}
}

func TestFormatDisplayResolution_WithinBounds(t *testing.T) {
	cfg := &DisplayConfig{Width: 1280, Height: 720}
	res, err := formatDisplayResolution(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "1280x720"; res != want {
		t.Fatalf("got %s, want %s", res, want)
	}
}

func TestFormatDisplayResolution_ClampBounds(t *testing.T) {
	cfg := &DisplayConfig{Width: 3900, Height: 3000}
	res, err := formatDisplayResolution(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "3840x2160"; res != want {
		t.Fatalf("got %s, want %s", res, want)
	}
}

func TestFormatDisplayResolution_LowValuesClamp(t *testing.T) {
	cfg := &DisplayConfig{Width: 640, Height: 480}
	res, err := formatDisplayResolution(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "800x600"; res != want {
		t.Fatalf("got %s, want %s", res, want)
	}
}

func TestFormatDisplayResolution_InvalidValues(t *testing.T) {
	cases := []*DisplayConfig{
		{Width: 0, Height: 720},
		{Width: 1280, Height: -1},
	}

	for i, cfg := range cases {
		if _, err := formatDisplayResolution(cfg); err == nil {
			t.Fatalf("case %d: expected error for invalid display values", i)
		}
	}
}
