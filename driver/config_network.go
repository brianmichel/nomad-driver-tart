package driver

import (
	"errors"
	"fmt"
	"strings"
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

// buildTartNetworkArgs computes the appropriate tart networking flags from NetworkConfig.
// It enforces mutual exclusivity among host, bridged, and softnet modes. Softnet is
// implicitly enabled when allow or expose lists are provided.
func buildTartNetworkArgs(cfg *NetworkConfig) ([]string, error) {
	args := []string{}
	if cfg == nil {
		return args, nil
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	bridgedIf := strings.TrimSpace(cfg.BridgedInterface)
	allow := cfg.SoftnetAllow
	expose := cfg.SoftnetExpose

	// Accept aliases for default NAT
	isDefault := mode == "" || mode == "default" || mode == "shared" || mode == "nat"
	isHost := mode == "host"
	isBridged := mode == "bridged"
	isSoftnet := mode == "softnet"

	// If no mode specified but allow/expose are set, we imply softnet.
	impliedSoftnet := isDefault && (len(allow) > 0 || len(expose) > 0)

	// Validate combinations
	if isHost {
		if bridgedIf != "" || len(allow) > 0 || len(expose) > 0 {
			return nil, errors.New("networking options conflict: host mode cannot be combined with bridged_interface or softnet options")
		}
		return []string{"--net-host"}, nil
	}

	if isBridged {
		if bridgedIf == "" {
			return nil, errors.New("bridged mode requires 'bridged_interface'")
		}
		if len(allow) > 0 || len(expose) > 0 {
			return nil, errors.New("networking options conflict: bridged mode cannot be combined with softnet options")
		}
		return []string{"--net-bridged", bridgedIf}, nil
	}

	if isSoftnet || impliedSoftnet {
		n := []string{"--net-softnet"}
		if len(allow) > 0 {
			n = append(n, "--net-softnet-allow", strings.Join(allow, ","))
		}
		if len(expose) > 0 {
			n = append(n, "--net-softnet-expose", strings.Join(expose, ","))
		}
		if bridgedIf != "" {
			return nil, errors.New("networking options conflict: softnet mode cannot be combined with bridged_interface")
		}
		return n, nil
	}

	// Unknown mode?
	if !isDefault {
		return nil, fmt.Errorf("unknown networking mode: %s", mode)
	}

	// Default shared (NAT) networking: no specific flags needed
	return args, nil
}
