package driver

import (
	"slices"
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
	if !slices.Equal(got, want) {
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
	if !slices.Equal(got, want) {
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
	if !slices.Equal(got, want) {
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
	if !slices.Equal(got, want) {
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
	if !slices.Equal(got, want) {
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
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendNomadPortExposures_Softnet(t *testing.T) {
	cfg := &NetworkConfig{Mode: "softnet", SoftnetAllow: []string{"0.0.0.0/0"}}
	got := appendNomadPortExposures(cfg, []nomadPortExposure{{Label: "http", HostPort: 21043, GuestPort: 8000}})
	want := &NetworkConfig{Mode: "softnet", SoftnetAllow: []string{"0.0.0.0/0"}, SoftnetExpose: []string{"21043:8000"}}
	if got == cfg {
		t.Fatal("expected a copied config")
	}
	if !slices.Equal(got.SoftnetAllow, want.SoftnetAllow) || !slices.Equal(got.SoftnetExpose, want.SoftnetExpose) || got.Mode != want.Mode {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if len(cfg.SoftnetExpose) != 0 {
		t.Fatalf("expected original config to be unchanged, got %#v", cfg)
	}
}

func TestAppendNomadPortExposures_NonSoftnetNoop(t *testing.T) {
	cfg := &NetworkConfig{Mode: "shared"}
	got := appendNomadPortExposures(cfg, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}})
	if got.Mode != "shared" || len(got.SoftnetExpose) != 0 {
		t.Fatalf("unexpected config: %#v", got)
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

func TestImpliedSoftnetForwardsAllocatedPorts(t *testing.T) {
	for _, cfg := range []*NetworkConfig{{SoftnetAllow: []string{"10.0.0.0/8"}}, {SoftnetExpose: []string{"2222:22"}}} {
		got := appendNomadPortExposures(cfg, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}})
		if !slices.Contains(got.SoftnetExpose, "21043:8000") {
			t.Fatalf("allocated forward missing: %#v", got)
		}
	}
}

func TestBuildTartNetworkArgsRejectsOrphanBridgedInterface(t *testing.T) {
	for _, mode := range []string{"", "shared", "nat", "default"} {
		if _, err := buildTartNetworkArgs(&NetworkConfig{Mode: mode, BridgedInterface: "en0"}); err == nil {
			t.Errorf("mode %q silently ignores bridged interface", mode)
		}
	}
}

func TestSoftnetAllocatedBindingValidation(t *testing.T) {
	for _, tc := range []struct {
		mode, ip string
		allow    bool
		wantErr  bool
	}{
		{"softnet", "", false, false}, {"softnet", "0.0.0.0", false, false},
		{"softnet", "127.0.0.1", false, true}, {"", "192.0.2.1", true, true},
		{"softnet", "::", false, true}, {"shared", "192.0.2.1", false, false},
	} {
		cfg := &NetworkConfig{Mode: tc.mode}
		if tc.allow {
			cfg.SoftnetAllow = []string{"10.0.0.0/8"}
		}
		err := validateNetworkConfig(cfg, []nomadPortExposure{{Label: "http", HostPort: 21043, GuestPort: 8000, HostIP: tc.ip}})
		if (err != nil) != tc.wantErr {
			t.Errorf("mode %q ip %q allow %v: %v", tc.mode, tc.ip, tc.allow, err)
		}
	}
}

func TestImpliedSoftnetDefaultAliasesDeduplicateAllocatedPorts(t *testing.T) {
	for _, mode := range []string{"", "shared", "nat", "default"} {
		cfg := &NetworkConfig{Mode: mode, SoftnetExpose: []string{"21043:8000"}}
		got := appendNomadPortExposures(cfg, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}, {HostPort: 22000, GuestPort: 22000}})
		if !slices.Equal(got.SoftnetExpose, []string{"21043:8000", "22000:22000"}) {
			t.Fatalf("mode %q: %v", mode, got.SoftnetExpose)
		}
	}
}

func TestSoftnetForwardConflictValidation(t *testing.T) {
	cases := []struct {
		name     string
		cfg      *NetworkConfig
		ports    []nomadPortExposure
		conflict bool
	}{
		{"comma separated", &NetworkConfig{Mode: "softnet", SoftnetExpose: []string{"1234:80,21043:22"}}, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}}, true},
		{"leading zero numeric pair", &NetworkConfig{Mode: "softnet", SoftnetExpose: []string{"021043:22"}}, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}}, true},
		{"identical allocations", &NetworkConfig{Mode: "softnet", SoftnetExpose: []string{"21043:8000"}}, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}, {HostPort: 21043, GuestPort: 8000}}, false},
		{"distinct ports", &NetworkConfig{Mode: "softnet", SoftnetExpose: []string{"1234:22"}}, []nomadPortExposure{{HostPort: 21043, GuestPort: 8000}}, false},
		{"non softnet allocations", &NetworkConfig{Mode: "shared"}, []nomadPortExposure{{HostPort: 21043, GuestPort: 22}, {HostPort: 21043, GuestPort: 8000}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNetworkConfig(tc.cfg, tc.ports)
			if (err != nil) != tc.conflict {
				t.Fatalf("conflict=%v, got %v", tc.conflict, err)
			}
		})
	}
}
