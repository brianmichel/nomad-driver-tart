package driver

import (
	"crypto/sha256"
	"fmt"
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

// taskVMName is opaque, bounded, and unambiguous across allocation/task identities.
// Allocation-only callers are retained for compatibility with the low-level client;
// Driver.StartTask requires full task identity for all new VM ownership records.
func taskVMName(cfg *drivers.TaskConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.ID == "" && cfg.Name == "" {
		return vmName(cfg.AllocID)
	}
	digest := sha256.New()
	for _, part := range []string{cfg.AllocID, cfg.ID, cfg.Name} {
		fmt.Fprintf(digest, "%d:%s", len(part), part)
	}
	return fmt.Sprintf("nomad-%x", digest.Sum(nil))
}

func (cfg VMConfig) vmName() string {
	if cfg.ownership != (vmOwnership{}) {
		return cfg.ownership.VMName
	}
	return taskVMName(cfg.Nomad)
}

// vmName preserves the pre-v2 allocation-only identity for legacy recovery.
func vmName(allocID string) string {
	return "nomad-" + allocID
}
