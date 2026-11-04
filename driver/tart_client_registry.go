package driver

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var registryAuthorityPattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*(?:\.[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)*(?::[0-9]+)?$`)

// PrepareRegistryEnv scopes explicit credentials to this command's registry.
// Never mutate process environment or persist task credentials in Keychain.
func (c *tartCLI) PrepareRegistryEnv(ctx context.Context, config VMConfig) ([]string, error) {
	env := os.Environ()
	if config.Nomad != nil {
		env = append(env, config.Nomad.EnvList()...)
	}

	auth := config.Driver.Auth
	if auth.Username == "" && auth.Password == "" {
		return env, nil
	}
	if !auth.IsValid() {
		return nil, fmt.Errorf("registry auth requires both username and password")
	}
	// Tart's Reference.g4 host grammar permits ASCII alphanumeric labels
	// separated by dots and single hyphens. Reject ambiguous authorities.
	authority, _, hasPath := strings.Cut(config.Driver.URL, "/")
	if !hasPath || !registryAuthorityPattern.MatchString(authority) {
		return nil, fmt.Errorf("registry reference has an unsupported hostname")
	}
	host, port, hasPort := strings.Cut(authority, ":")
	if hasPort {
		n, err := strconv.Atoi(strings.TrimLeft(port, "0"))
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("registry reference has an unsupported port")
		}
		host += ":" + strconv.Itoa(n)
	}
	filtered := env[:0]
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TART_REGISTRY_HOSTNAME" && key != "TART_REGISTRY_USERNAME" && key != "TART_REGISTRY_PASSWORD" {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "TART_REGISTRY_HOSTNAME="+host,
		"TART_REGISTRY_USERNAME="+auth.Username, "TART_REGISTRY_PASSWORD="+auth.Password), nil
}

// BuildPullArgs returns the tart CLI args used to prefetch an image into
// the local OCI cache without creating a named VM.
func (c *tartCLI) BuildPullArgs(config VMConfig) []string {
	return []string{"pull", config.Driver.URL}
}

// BuildStartArgs computes the full set of CLI arguments required to start a
// VM based on the provided configuration. This centralizes tart-specific flag
// construction away from the driver.
func (c *tartCLI) BuildStartArgs(config VMConfig) ([]string, error) {
	vmName := vmName(config.Nomad.AllocID)

	args := []string{"run", vmName}
	if !config.Driver.ShowUI {
		args = append(args, "--no-graphics")
	}

	// Mount the Nomad task's secrets directory read-only if present
	if config.Nomad != nil {
		td := config.Nomad.TaskDir()
		if td != nil && td.SecretsDir != "" {
			// Ensure that the secrets directory is mounted with a name to ensure
			// multiple directories can be mounted if needed.
			args = append(args, fmt.Sprintf("--dir=secrets:%s:ro", td.SecretsDir))
		}
	}

	networkCfg := appendNomadPortExposures(config.Driver.Network, nomadPortExposures(config.Nomad))

	netArgs, err := buildTartNetworkArgs(networkCfg)
	if err != nil {
		return nil, err
	}

	rootDiskArgs, err := buildRootDiskArgs(config.Driver.RootDisk)
	if err != nil {
		return nil, err
	}

	dirArgs, err := buildDirectoryArgs(config.Driver.Directories)
	if err != nil {
		return nil, err
	}

	args = append(args, netArgs...)
	args = append(args, rootDiskArgs...)
	args = append(args, dirArgs...)

	return args, nil
}

// registryHost extracts the registry host from an image URL. It attempts to
// parse the URL and, if no host is present, falls back to splitting the string
// on the first '/'.
func registryHost(image string) (string, error) {
	u, err := url.Parse(image)
	if err != nil {
		return "", err
	}

	if u.Host != "" {
		return u.Host, nil
	}

	// If the URL didn't have a host (e.g. missing scheme), derive it by
	// taking everything before the first '/'.
	parts := strings.SplitN(image, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("invalid image reference: %s", image)
	}
	return parts[0], nil
}
