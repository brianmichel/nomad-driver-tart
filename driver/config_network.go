package driver

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
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

// appendNomadPortExposures returns a copy of cfg with Nomad-allocated port
// mappings appended to Softnet expose rules. It only modifies configurations
// using explicit or implied Softnet; other modes keep their existing behavior.
func appendNomadPortExposures(cfg *NetworkConfig, exposures []nomadPortExposure) *NetworkConfig {
	if len(exposures) == 0 {
		return cfg
	}

	copyCfg := &NetworkConfig{}
	if cfg != nil {
		*copyCfg = *cfg
		copyCfg.SoftnetAllow = slices.Clone(cfg.SoftnetAllow)
		copyCfg.SoftnetExpose = slices.Clone(cfg.SoftnetExpose)
	}

	if !usesSoftnet(copyCfg) {
		return copyCfg
	}

	seen := make(map[string]struct{}, len(copyCfg.SoftnetExpose))
	for _, expose := range copyCfg.SoftnetExpose {
		seen[expose] = struct{}{}
		for _, spec := range strings.Split(expose, ",") {
			parts := strings.Split(spec, ":")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Trim(parts[0], "0123456789") != "" || strings.Trim(parts[1], "0123456789") != "" {
				continue
			}
			host, hostErr := strconv.Atoi(parts[0])
			guest, guestErr := strconv.Atoi(parts[1])
			if hostErr == nil && guestErr == nil {
				seen[fmt.Sprintf("%d:%d", host, guest)] = struct{}{}
			}
		}
	}
	for _, exposure := range exposures {
		if exposure.HostPort <= 0 || exposure.GuestPort <= 0 {
			continue
		}
		spec := fmt.Sprintf("%d:%d", exposure.HostPort, exposure.GuestPort)
		if _, ok := seen[spec]; ok {
			continue
		}
		copyCfg.SoftnetExpose = append(copyCfg.SoftnetExpose, spec)
		seen[spec] = struct{}{}
	}

	return copyCfg
}

// usesSoftnet matches the modes accepted by buildTartNetworkArgs, including
// historically supported default aliases with Softnet options.
func usesSoftnet(cfg *NetworkConfig) bool {
	if cfg == nil {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	return mode == "softnet" || ((mode == "" || mode == "default" || mode == "shared" || mode == "nat") && (len(cfg.SoftnetAllow) > 0 || len(cfg.SoftnetExpose) > 0))
}

// validateNetworkConfig checks the configuration and any automatic forwards before
// VM setup. Tart's EXTERNAL:INTERNAL syntax cannot select a specific host IP.
func validateNetworkConfig(cfg *NetworkConfig, exposures []nomadPortExposure) error {
	if _, err := buildTartNetworkArgs(cfg); err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	if usesSoftnet(cfg) {
		// Tart passes a comma-separated list to Softnet. Inspect numeric pairs
		// only; leave malformed or unsupported syntax to upstream validation.
		explicit := make(map[int][]int)
		for _, entry := range cfg.SoftnetExpose {
			for _, spec := range strings.Split(entry, ",") {
				parts := strings.Split(spec, ":")
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Trim(parts[0], "0123456789") != "" || strings.Trim(parts[1], "0123456789") != "" {
					continue
				}
				host, hostErr := strconv.Atoi(parts[0])
				guest, guestErr := strconv.Atoi(parts[1])
				if hostErr == nil && guestErr == nil {
					explicit[host] = append(explicit[host], guest)
				}
			}
		}
		allocated := make(map[int]int)
		for _, exposure := range exposures {
			if exposure.HostPort > 0 && exposure.GuestPort > 0 {
				for _, guest := range explicit[exposure.HostPort] {
					if guest != exposure.GuestPort {
						return fmt.Errorf("softnet port conflict: external port %d maps to both %d and %d", exposure.HostPort, guest, exposure.GuestPort)
					}
				}
				if guest, ok := allocated[exposure.HostPort]; ok && guest != exposure.GuestPort {
					return fmt.Errorf("softnet port conflict: external port %d maps to both %d and %d", exposure.HostPort, guest, exposure.GuestPort)
				}
				allocated[exposure.HostPort] = exposure.GuestPort
			}
			if exposure.HostPort > 0 && exposure.GuestPort > 0 && exposure.HostIP != "" && exposure.HostIP != "0.0.0.0" {
				return fmt.Errorf("softnet cannot honor allocated host IP %q for port %q: Tart supports only egress-interface port forwarding", exposure.HostIP, exposure.Label)
			}
		}
	}
	return nil
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

	if bridgedIf != "" {
		return nil, errors.New("bridged_interface requires bridged mode")
	}

	// Default shared (NAT) networking: no specific flags needed
	return args, nil
}
