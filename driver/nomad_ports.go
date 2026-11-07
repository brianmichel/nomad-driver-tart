package driver

import (
	"github.com/hashicorp/nomad/plugins/drivers"
)

// nomadPortExposure describes enough of Nomad's allocated port mapping for the
// Tart driver to build Softnet forwarding rules.
type nomadPortExposure struct {
	Label     string
	HostPort  int
	GuestPort int
	HostIP    string
}

// nomadPortExposures extracts Nomad's allocated port mappings from the task
// config for Softnet expose rules.
func nomadPortExposures(cfg *drivers.TaskConfig) []nomadPortExposure {
	if cfg == nil || cfg.Resources == nil || cfg.Resources.Ports == nil {
		return nil
	}

	ports := *cfg.Resources.Ports
	out := make([]nomadPortExposure, 0, len(ports))
	for _, port := range ports {
		guestPort := port.To
		if guestPort <= 0 {
			guestPort = port.Value
		}

		out = append(out, nomadPortExposure{
			Label:     port.Label,
			HostPort:  port.Value,
			GuestPort: guestPort,
			HostIP:    port.HostIP,
		})
	}

	return out
}
