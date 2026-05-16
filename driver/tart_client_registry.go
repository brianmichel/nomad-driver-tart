package driver

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// PrepareRegistryEnv builds the environment slice passed to tart commands
// and performs a `tart login` against the target registry when task-level
// auth credentials are configured. The resulting environment is suitable for
// reuse across subsequent tart invocations (clone, pull, etc.).
func (c *tartCLI) PrepareRegistryEnv(ctx context.Context, config VMConfig) ([]string, error) {
	env := os.Environ()
	if config.Nomad != nil {
		env = append(env, config.Nomad.EnvList()...)
	}

	if !config.Driver.Auth.IsValid() {
		c.logger.Trace("Auth not provided; relying on env vars for registry access")
		return env, nil
	}

	host, err := registryHost(config.Driver.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	loginCmd := c.runner.Run(ctx, "tart", "login", host, "--username", config.Driver.Auth.Username, "--password-stdin")
	loginCmd.Stdin = strings.NewReader(config.Driver.Auth.Password)
	loginCmd.Env = env

	var stderr bytes.Buffer
	loginCmd.Stderr = &stderr

	if err := loginCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to login to container registry: %w (stderr: %s)", err, stderr.String())
	}

	return env, nil
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

	netArgs, err := buildTartNetworkArgs(config.Driver.Network)
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
