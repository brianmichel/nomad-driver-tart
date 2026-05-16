package driver

import (
	"fmt"
	"strings"
)

// Options to specify how the root disk of the VM should be
// prepared by the tart tool.
type RootDiskOptions struct {
	ReadOnly    bool    `codec:"readonly"`
	SyncMode    *string `codec:"sync_mode"`
	CachingMode *string `codec:"caching_mode"`
}

func buildRootDiskArgs(cfg *RootDiskOptions) ([]string, error) {
	args := []string{}
	if cfg == nil {
		return args, nil
	}

	if cfg.ReadOnly {
		args = append(args, "ro")
	}

	if cfg.CachingMode != nil {
		var rawCachingMode = normalize(*cfg.CachingMode)
		var caching string
		switch rawCachingMode {
		case "automatic":
			caching = "automatic"
		case "uncached":
			caching = "uncached"
		case "cached":
			caching = "cached"
		}

		args = append(args, fmt.Sprintf("caching=%s", caching))
	}

	if cfg.SyncMode != nil {
		var rawSyncMode = normalize(*cfg.SyncMode)
		var sync string
		switch rawSyncMode {
		case "fsync":
			sync = "fsync"
		case "full":
			sync = "full"
		case "none":
			sync = "none"
		}
		args = append(args, fmt.Sprintf("sync=%s", sync))
	}

	return []string{fmt.Sprintf("--root-disk-opts=%s", strings.Join(args, ","))}, nil
}

// normalize cleans the input value by converting it to lowercase and trimming whitespace.
func normalize(value string) string {
	value = strings.ToLower(value)
	value = strings.TrimSpace(value)
	return value
}
