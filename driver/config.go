package driver

import "github.com/hashicorp/nomad/plugins/shared/hclspec"

// Config is the driver configuration set by the SetConfig RPC call
type Config struct {
	// Enabled is set to true to enable the tart driver
	Enabled bool `codec:"enabled"`
}

// TaskConfig is the driver configuration of a task within a job
type TaskConfig struct {
	URL         string   `codec:"url"`
	Name        string   `codec:"name"`
	Command     string   `codec:"command"`
	Args        []string `codec:"args"`
	SSHUser     string   `codec:"ssh_user"`
	SSHPassword string   `codec:"ssh_password"`
}

var (
	// configSpec is the hcl specification returned by the ConfigSchema RPC
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		// Config options can be specified here
		"enabled": hclspec.NewDefault(
			hclspec.NewAttr("enabled", "bool", false),
			hclspec.NewLiteral("true"),
		),
	})

	// taskConfigSpec is the hcl specification for the driver config section of
	// a task within a job. It is returned in the TaskConfigSchema RPC
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"url":  hclspec.NewAttr("url", "string", true),
		"name": hclspec.NewAttr("name", "string", true),
		"command": hclspec.NewDefault(
			hclspec.NewAttr("command", "string", false),
			hclspec.NewLiteral(`""`),
		),
		"args": hclspec.NewDefault(
			hclspec.NewAttr("args", "list(string)", false),
			hclspec.NewLiteral(`[]`),
		),
		"ssh_user": hclspec.NewDefault(
			hclspec.NewAttr("ssh_user", "string", false),
			hclspec.NewLiteral(`"admin"`),
		),
		"ssh_password": hclspec.NewAttr("ssh_password", "string", false),
	})
)
