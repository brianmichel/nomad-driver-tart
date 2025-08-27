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
            "mode":               hclspec.NewAttr("mode", "string", false),
            "bridged_interface":  hclspec.NewAttr("bridged_interface", "string", false),
            "softnet_allow":      hclspec.NewAttr("softnet_allow", "list(string)", false),
            "softnet_expose":     hclspec.NewAttr("softnet_expose", "list(string)", false),
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
