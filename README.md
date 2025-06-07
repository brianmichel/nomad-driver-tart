# Nomad Driver for Tart VMs

A custom task driver for HashiCorp Nomad that enables orchestration and management of [Tart](https://github.com/cirruslabs/tart) virtual machines on macOS.

## Overview

This driver allows Nomad to manage the lifecycle of Tart VMs, providing a way to run macOS virtual machines as Nomad tasks. It integrates with Nomad's ecosystem, enabling users to deploy and manage Tart VMs through Nomad's job specification.

## Features

- Basic task lifecycle management (start, stop, destroy)
- Task status reporting
- Signal forwarding to tasks
- Placeholder for resource usage statistics

## Requirements

- Go 1.20 or later
- Nomad 1.6.x or later
- macOS with Tart installed

## Building

To build the driver plugin:

```bash
make build
# Cross compile for Apple Silicon
GOOS=darwin GOARCH=arm64 make build
```

This will create a `nomad-driver-tart` binary in the project root.

## Installation

1. Build the plugin as described above
2. Place the binary in a directory where Nomad can find it
3. Configure Nomad to use the plugin (see example configuration below)

## Configuration

### Nomad Agent Configuration

Create or modify your Nomad agent configuration to include the Tart driver plugin:

```hcl
plugin "nomad-driver-tart" {
  config {
    enabled = true
  }
}

client {
  enabled = true
  
  options {
    "driver.allowlist" = "tart"
  }
}
```

### Job Specification

Here's an example job specification that uses the Tart driver:

```hcl
job "example-tart" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel = 1
    // Downloading a VM image can take a while as they are
    // tens of GBs in size. Give our jobs enough grace to
    // get setup properly.
    healthy_deadline  = "30m"
    progress_deadline = "60m"
  }

  group "example" {
    count = 1

    task "tart-vm" {
      driver = "tart"

      config {
        url = "ghcr.io/cirruslabs/macos-sequoia-vanilla:latest"
        name = "example-vm"
        command = "/bin/echo"
        args    = ["Hello from Tart VM!"]
      }

      resources {
        cpu    = 500
        memory = 256
      }

      logs {
        max_files     = 3
        max_file_size = 10
      }
    }
  }
}
```

## Usage

1. Start the Nomad agent with the plugin:

```bash
nomad agent -dev -config=./examples/agent.hcl -plugin-dir=$(pwd)
```

2. In another terminal, run a job that uses the Tart driver:

```bash
nomad run ./examples/example.nomad
```

3. Check the status of the job and get the allocation ID:

```bash
nomad status
```

4. View the logs from the task:

```bash
nomad logs <ALLOCATION_ID>
```

## Development

This driver is currently in development and provides basic functionality. Future enhancements may include:

- Proper Tart VM lifecycle management
- Resource isolation and management
- Network configuration
- Volume mounts
- Health checking

### Continuous Integration

A GitHub Actions workflow automatically formats, vets, and builds the driver for darwin/arm64 on every pull request and push to `main`. Tagging a commit with `v*` also uploads the built binary as a release artifact.


## License

See [LICENSE](LICENSE) file.
An experimental task driver for Nomad using the Tart virtualization tool.
