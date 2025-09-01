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
	// DiskSize is the desired disk size of the VM in gigabytes. Setting this
	// to zero will leave the disk size unchanged.
	DiskSize int  `codec:"disk_size"`
	Auth     Auth `codec:"auth"`

	// Network contains networking options for the VM
	Network *NetworkConfig `codec:"network"`

	// Root disk options on how to configure the VM
	RootDisk *RootDiskOptions `codec:"root_disk"`

	// Directories is a blocklist of host directories to mount into the VM
	Directories []DirectoryMount `codec:"directory"`
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
		"disk_size":    hclspec.NewAttr("disk_size", "number", false),
		"auth": hclspec.NewBlock("auth", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"username": hclspec.NewAttr("username", "string", true),
			"password": hclspec.NewAttr("password", "string", true),
		})),

		// Networking options block
		// mode: "host" | "bridged" | "softnet" | "shared" (default)
		// softnet_allow/expose imply softnet when mode is not specified
		"network": hclspec.NewBlock("network", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"mode":              hclspec.NewAttr("mode", "string", false),
			"bridged_interface": hclspec.NewAttr("bridged_interface", "string", false),
			"softnet_allow":     hclspec.NewAttr("softnet_allow", "list(string)", false),
			"softnet_expose":    hclspec.NewAttr("softnet_expose", "list(string)", false),
		})),

		// Root disk options block
		"root_disk": hclspec.NewBlock("root_disk", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"readonly":     hclspec.NewDefault(hclspec.NewAttr("readonly", "bool", false), hclspec.NewLiteral("false")),
			"caching_mode": hclspec.NewAttr("caching_mode", "string", false),
			"sync_mode":    hclspec.NewAttr("sync_mode", "string", false),
		})),

		"directory": hclspec.NewBlockList("directory", hclspec.NewObject(map[string]*hclspec.Spec{
			"name": hclspec.NewAttr("name", "string", true),
			"path": hclspec.NewAttr("path", "string", true),
			"options": hclspec.NewBlock("options", false, hclspec.NewObject(map[string]*hclspec.Spec{
				"readonly": hclspec.NewAttr("readonly", "bool", true),
				"tag":      hclspec.NewAttr("tag", "string", false),
			})),
		})),
	})
)

// NetworkConfig describes networking configuration for a task
type NetworkConfig struct {
	// Mode selects networking mode: "host", "bridged", "softnet", or "shared" (default NAT)
	Mode string `codec:"mode"`
	// BridgedInterface is used when Mode == "bridged" to select the interface
	BridgedInterface string `codec:"bridged_interface"`
	// SoftnetAllow CIDRs when using Softnet; implies Softnet if Mode unspecified
	SoftnetAllow []string `codec:"softnet_allow"`
	// SoftnetExpose EXTERNAL:INTERNAL TCP port forward specs when using Softnet; implies Softnet
	SoftnetExpose []string `codec:"softnet_expose"`
}

// Options to specify how the root disk of the VM should be
// prepared by the tart tool.
type RootDiskOptions struct {
	ReadOnly    bool    `codec:"readonly"`
	SyncMode    *string `codec:"sync_mode"`
	CachingMode *string `codec:"caching_mode"`
}

// DirectoryMount represents a single directory block item from the config
// with an optional name (purely descriptive), required host path, and
// optional options.
type DirectoryMount struct {
	Name    string            `codec:"name"`
	Path    string            `codec:"path"`
	Options *DirectoryOptions `codec:"options"`
}

// DirectoryOptions controls how a directory mount is handled by tart.
// - readonly: when true, append ":ro" to the mount spec
// - tag: when set, append "@tag" to the mount spec
type DirectoryOptions struct {
	ReadOnly bool   `codec:"readonly"`
	Tag      string `codec:"tag"`
}
