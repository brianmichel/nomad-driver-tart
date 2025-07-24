package driver

import "github.com/hashicorp/nomad/plugins/shared/hclspec"

// Config is the driver configuration set by the SetConfig RPC call
type Config struct {
	// Enabled is set to true to enable the tart driver
	Enabled bool `codec:"enabled"`
}

// TaskConfig is the driver configuration of a task within a job
type TaskConfig struct {
	URL         string `codec:"url"`
	SSHUser     string `codec:"ssh_user"`
	SSHPassword string `codec:"ssh_password"`
	ShowUI      bool   `codec:"show_ui"`
	// Network specifies the networking mode used when running the VM. Valid
	// options include "host", "softnet", etc. Defaults to "host".
	Network string `codec:"network"`
	// DiskSize is the desired disk size of the VM in gigabytes. Setting this
	// to zero will leave the disk size unchanged.
	DiskSize int  `codec:"disk_size"`
	Auth     Auth `codec:"auth"`
}

type Auth struct {
	Username string `codec:"username"`
	Password string `codec:"password"`
}

func (a Auth) IsValid() bool {
	return a.Username != "" && a.Password != ""
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
		"url":          hclspec.NewAttr("url", "string", true),
		"ssh_user":     hclspec.NewAttr("ssh_user", "string", true),
		"ssh_password": hclspec.NewAttr("ssh_password", "string", true),
		"show_ui":      hclspec.NewDefault(hclspec.NewAttr("show_ui", "bool", false), hclspec.NewLiteral("false")),
		"network":      hclspec.NewDefault(hclspec.NewAttr("network", "string", false), hclspec.NewLiteral("\"host\"")),
		"disk_size":    hclspec.NewAttr("disk_size", "number", false),
		"auth": hclspec.NewBlock("auth", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"username": hclspec.NewAttr("username", "string", true),
			"password": hclspec.NewAttr("password", "string", true),
		})),
	})
)
