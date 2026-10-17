package driver

import (
	"errors"
	"testing"

	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestFingerprintPluginVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		probeErr error
		listErr  error
		health   drivers.HealthState
	}{
		{name: "healthy", enabled: true, health: drivers.HealthStateHealthy},
		{name: "disabled", health: drivers.HealthStateUndetected},
		{name: "tart missing", enabled: true, probeErr: errors.New("not found"), health: drivers.HealthStateUndetected},
		{name: "list failed", enabled: true, listErr: errors.New("list failed"), health: drivers.HealthStateUnhealthy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDriverWithMock(t, &testClient{
				mockProber: mockProber{version: "2.32.1", err: tc.probeErr},
				mockLister: mockLister{err: tc.listErr},
			})
			d.config.Enabled = tc.enabled
			info, err := d.PluginInfo()
			if err != nil {
				t.Fatal(err)
			}
			if info.PluginVersion != Version || Version == "" {
				t.Fatalf("plugin info version = %q, build version = %q", info.PluginVersion, Version)
			}
			fp := d.buildFingerprint()
			if fp.Health != tc.health {
				t.Fatalf("health = %v, want %v", fp.Health, tc.health)
			}
			attr := fp.Attributes[pluginVersionKey]
			if attr == nil || attr.String == nil || *attr.String != Version {
				t.Fatalf("plugin version attribute = %#v, want %q", attr, Version)
			}
			if tc.enabled && tc.probeErr == nil {
				attr := fp.Attributes[versionKey]
				if attr == nil || attr.String == nil || *attr.String != "2.32.1" {
					t.Fatalf("Tart CLI version attribute = %#v, want 2.32.1", attr)
				}
			}
		})
	}
}
