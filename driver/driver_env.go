package driver

import (
	"os"
	"strings"

	"github.com/hashicorp/nomad/plugins/drivers"
)

const tartDefaultPath = "/opt/zerobrew/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// tartEnvList ensures Tart can find helper binaries such as softnet while
// preserving any PATH supplied by the task or inherited from the host.
func tartEnvList(tc *drivers.TaskConfig) []string {
	list := tc.EnvList()

	pathValue := tartDefaultPath
	if hostPath := os.Getenv("PATH"); hostPath != "" {
		pathValue += ":" + hostPath
	}

	for i, env := range list {
		if strings.HasPrefix(env, "PATH=") {
			if current := strings.TrimPrefix(env, "PATH="); current != "" {
				pathValue = tartDefaultPath + ":" + current
			}
			list[i] = "PATH=" + pathValue
			return list
		}
	}

	return append(list, "PATH="+pathValue)
}

func vmName(allocID string) string {
	return "nomad-" + allocID
}
