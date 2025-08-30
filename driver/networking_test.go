package driver

import (
	"reflect"
	"testing"
)

func TestBuildTartNetworkArgs_Default(t *testing.T) {
	got, err := buildTartNetworkArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no args for default networking, got %v", got)
	}
}

func TestBuildTartNetworkArgs_Host(t *testing.T) {
	cfg := &NetworkConfig{Mode: "host"}
	got, err := buildTartNetworkArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--net-host"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildTartNetworkArgs_Bridged(t *testing.T) {
	cfg := &NetworkConfig{Mode: "bridged", BridgedInterface: "en0"}
	got, err := buildTartNetworkArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--net-bridged", "en0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildTartNetworkArgs_Softnet(t *testing.T) {
	cfg := &NetworkConfig{Mode: "softnet"}
	got, err := buildTartNetworkArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--net-softnet"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildTartNetworkArgs_SoftnetAllowImpliesSoftnet(t *testing.T) {
	cfg := &NetworkConfig{SoftnetAllow: []string{"192.168.0.0/24", "10.0.0.0/16"}}
	got, err := buildTartNetworkArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--net-softnet", "--net-softnet-allow", "192.168.0.0/24,10.0.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildTartNetworkArgs_SoftnetExposeImpliesSoftnet(t *testing.T) {
	cfg := &NetworkConfig{SoftnetExpose: []string{"2222:22", "8080:80"}}
	got, err := buildTartNetworkArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--net-softnet", "--net-softnet-expose", "2222:22,8080:80"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildTartNetworkArgs_SoftnetAllowAndExpose(t *testing.T) {
	cfg := &NetworkConfig{SoftnetAllow: []string{"0.0.0.0/0"}, SoftnetExpose: []string{"2222:22"}}
	got, err := buildTartNetworkArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--net-softnet", "--net-softnet-allow", "0.0.0.0/0", "--net-softnet-expose", "2222:22"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildTartNetworkArgs_Conflicts(t *testing.T) {
	cases := []*NetworkConfig{
		{Mode: "host", BridgedInterface: "en0"},
		{Mode: "host", SoftnetAllow: []string{"192.168.0.0/24"}},
		{Mode: "bridged"}, // missing interface
		{Mode: "bridged", SoftnetExpose: []string{"2222:22"}}, // softnet option with bridged
		{Mode: "softnet", BridgedInterface: "en0"},            // bridged iface with softnet
		{Mode: "weird"}, // unknown mode
	}
	for i, cfg := range cases {
		if _, err := buildTartNetworkArgs(cfg); err == nil {
			t.Fatalf("case %d: expected error for conflicting/invalid networking options, got nil", i)
		}
	}
}
