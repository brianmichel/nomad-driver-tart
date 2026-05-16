package driver

import (
	"github.com/hashicorp/nomad/plugins/drivers"
)

func (d *Driver) tartEnvList(tc *drivers.TaskConfig) []string {
	// Patch the env list to include the homebrew paths to help tart
	// find other binaries (like softnet) as needed.
	list := tc.EnvList()
	list = append(list, "PATH=/opt/homebrew/bin:/opt/homebrew/sbin")

	return list
}

func vmName(allocID string) string {
	return "nomad-" + allocID
}
