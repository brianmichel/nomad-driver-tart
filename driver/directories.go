package driver

import (
	"fmt"
	"strings"
)

// buildDirectoryArgs converts directory mount config into tart --dir flags.
// For each mount we emit a single arg using equals form:
//
//	--dir=<[name:]path[:options]>
//
// where options are comma-separated (e.g., ro,tag=mytag)
func buildDirectoryArgs(dirs []DirectoryMount) ([]string, error) {
	if len(dirs) == 0 {
		return []string{}, nil
	}

	// Each mount adds one arg: "--dir=<spec>"
	args := make([]string, 0, len(dirs))
	for _, d := range dirs {
		path := strings.TrimSpace(d.Path)
		if path == "" {
			return nil, fmt.Errorf("directory.path is required for directory mounts")
		}

		// Start with optional name prefix
		var specBuilder strings.Builder
		name := strings.TrimSpace(d.Name)
		if name != "" {
			specBuilder.WriteString(name)
			specBuilder.WriteString(":")
		}
		specBuilder.WriteString(path)

		// Collect options
		if d.Options != nil {
			opts := make([]string, 0, 2)
			if d.Options.ReadOnly {
				opts = append(opts, "ro")
			}
			tag := strings.TrimSpace(d.Options.Tag)
			if tag != "" {
				opts = append(opts, "tag="+tag)
			}
			if len(opts) > 0 {
				specBuilder.WriteString(":")
				specBuilder.WriteString(strings.Join(opts, ","))
			}
		}

		args = append(args, "--dir="+specBuilder.String())
	}
	return args, nil
}
