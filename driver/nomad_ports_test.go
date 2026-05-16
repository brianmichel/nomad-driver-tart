package driver

import (
	"reflect"
	"testing"

	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestNomadPortExposuresExtractsAllocatedMappings(t *testing.T) {
	ports := structs.AllocatedPorts{
		{Label: "http", Value: 21043, To: 4096, HostIP: "192.168.1.10"},
		{Label: "ssh", Value: 22022, To: 22, HostIP: "192.168.1.10"},
	}
	cfg := &drivers.TaskConfig{
		Resources: &drivers.Resources{
			Ports: &ports,
		},
	}

	got := nomadPortExposures(cfg)
	want := []nomadPortExposure{
		{Label: "http", HostPort: 21043, GuestPort: 4096, HostIP: "192.168.1.10"},
		{Label: "ssh", HostPort: 22022, GuestPort: 22, HostIP: "192.168.1.10"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected exposures:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestNomadPortExposuresFallsBackToHostPortWhenToUnset(t *testing.T) {
	ports := structs.AllocatedPorts{
		{Label: "http", Value: 21043, To: 0, HostIP: "192.168.1.10"},
	}
	cfg := &drivers.TaskConfig{
		Resources: &drivers.Resources{
			Ports: &ports,
		},
	}

	got := nomadPortExposures(cfg)
	want := []nomadPortExposure{
		{Label: "http", HostPort: 21043, GuestPort: 21043, HostIP: "192.168.1.10"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected exposures:\nwant: %#v\n got: %#v", want, got)
	}
}
