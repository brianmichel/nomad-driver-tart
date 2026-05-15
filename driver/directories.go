package driver

import (
	"fmt"
	"strings"

	"github.com/hashicorp/nomad/client/allocdir"
	"github.com/hashicorp/nomad/client/taskenv"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// resolveDirectoryMounts rewrites Nomad task directory variables in directory
// mounts to the corresponding host paths Tart expects for --dir flags.
func resolveDirectoryMounts(cfg *drivers.TaskConfig, dirs []DirectoryMount) []DirectoryMount {
	if cfg == nil || len(dirs) == 0 {
		return dirs
	}

	td := cfg.TaskDir()
	if td == nil {
		return dirs
	}

	replacer := strings.NewReplacer(
		"${"+taskenv.TaskLocalDir+"}", td.LocalDir,
		"${"+taskenv.AllocDir+"}", td.SharedAllocDir,
		"${"+taskenv.SecretsDir+"}", td.SecretsDir,
	)

	resolved := make([]DirectoryMount, len(dirs))
	copy(resolved, dirs)
	for i := range resolved {
		path := replacer.Replace(resolved[i].Path)
		path = replacePathPrefix(path, allocdir.TaskLocalContainerPath, td.LocalDir)
		path = replacePathPrefix(path, allocdir.SharedAllocContainerPath, td.SharedAllocDir)
		path = replacePathPrefix(path, allocdir.TaskSecretsContainerPath, td.SecretsDir)
		resolved[i].Path = path
	}

	return resolved
}

func replacePathPrefix(path, from, to string) string {
	if path == from {
		return to
	}

	prefix := from + "/"
	if strings.HasPrefix(path, prefix) {
		return to + "/" + strings.TrimPrefix(path, prefix)
	}

	return path
}

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
