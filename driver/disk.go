package driver

import (
	"fmt"
	"strings"
)

func buildRootDiskArgs(cfg *RootDiskOptions) ([]string, error) {
	args := []string{}
	if cfg == nil {
		return args, nil
	}

	if cfg.ReadOnly {
		args = append(args, "ro")
	}

	if cfg.CachingMode != nil {
		var rawCachingMode = CleanValue(*cfg.CachingMode)
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
		var rawSyncMode = CleanValue(*cfg.SyncMode)
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
