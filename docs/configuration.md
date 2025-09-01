# Nomad Tart Driver Configuration

This document explains all configuration parameters for the Tart VM Nomad driver, what they do, and how to use or access the resulting features from inside the VM when applicable.


## Driver Plugin Config (nomad agent)

- `enabled`: Enable the Tart driver plugin. Defaults to `true`.
  - Location: Nomad agent config (`plugin "nomad-driver-tart" { config { ... } }`).
  - Reference: driver/config.go:6

Example:

```hcl
plugin "nomad-driver-tart" {
  config {
    enabled = true
  }
}

client {
  enabled = true
  options = {
    "driver.allowlist" = "tart"
  }
}
```


## Task Config (job spec `config { ... }`)

The following parameters go under the task’s driver config block `task { driver = "tart"; config { ... } }`.

- `url` (string, required): Tart image reference to clone (e.g. `ghcr.io/cirruslabs/macos-sequoia-base:latest`).
  - Used to `tart clone` the VM before start.
  - Reference: driver/config.go:12, driver/tart_client.go:55,92

- `ssh_user` (string, required): Username the driver uses to SSH into the VM for logs/exec.
  - Reference: driver/config.go:13, driver/tart_client.go:256

- `ssh_password` (string, required): Password used for SSH.
  - Tip: inject via Nomad template and var, not hard-coded.
  - Reference: driver/config.go:14, examples/example.nomad.hcl:30-39

- `show_ui` (bool, optional, default: `false`): Show Tart’s built-in UI window; when `false` runs headless (`--no-graphics`).
  - Reference: driver/config.go:15, driver/tart_client.go:338-345

- `disk_size` (number, optional): Desired VM disk size in gigabytes. `0` leaves disk unchanged.
  - Applied via `tart set --disk-size` during setup.
  - Reference: driver/config.go:18-19, driver/tart_client.go:128-151, 316-338

- `auth { username, password }` (block, optional): Credentials for private image registries.
  - If set, driver runs `tart login <registry> --username <u> --password-stdin` prior to clone.
  - Reference: driver/config.go:22-33, driver/tart_client.go:71-91, 404-439

- `network { ... }` (block, optional): VM networking mode and Softnet options.
  - `mode` (string): One of `shared` (default NAT), `host`, `bridged`, or `softnet`.
  - `bridged_interface` (string): Required when `mode = "bridged"` (e.g. `en0` or `Wi‑Fi`).
  - `softnet_allow` (list(string)): CIDR allowlist for Softnet; implies Softnet if mode omitted.
  - `softnet_expose` (list(string)): Port forwards `EXTERNAL:INTERNAL` for Softnet; implies Softnet if mode omitted.
  - Conflicts are validated (e.g., host mode cannot combine with Softnet/bridged flags).
  - Reference: driver/config.go:37-52, driver/networking.go:1-63

- `root_disk { ... }` (block, optional): Root disk runtime behavior.
  - `readonly` (bool, default: `false`): Mount root disk readonly (adds `ro`).
  - `caching_mode` (string): One of `automatic`, `uncached`, `cached`.
  - `sync_mode` (string): One of `fsync`, `full`, `none`.
  - Emitted as `--root-disk-opts=ro,caching=<mode>,sync=<mode>` as applicable.
  - Reference: driver/config.go:55-65, driver/disk.go:1-40

- `directory { ... }` (block list, optional): Mount host directories into the VM.
  - `name` (string, optional): Logical name for the mount (helps identify inside the guest).
  - `path` (string, required): Absolute host path to share.
  - `options { readonly, tag }`:
    - `readonly` (bool): Mount read-only (adds `:ro`).
    - `tag` (string): Add a custom tag (emitted as `tag=<value>`).
  - Each block generates a `--dir=<spec>` argument to Tart.
  - Reference: driver/config.go:68-94, driver/directories.go:1-48, driver/directories_test.go


## VM Resources (CPU, Memory)

VM CPU and memory size are derived from the Nomad `resources` block:

- `cores` (int): Number of CPU cores given to the VM.
- `memory` (MB): Memory assigned to the VM.

The driver configures these via `tart set --cpu <cores> --memory <MB>` during setup.
- Reference: driver/tart_client.go:112-151, 316-338

Example:

```hcl
resources {
  cores  = 8      # CPU cores for the VM
  memory = 10240  # MB of RAM for the VM
}
```


## Secrets and Env From Nomad

- Nomad templates with `destination = "secrets/..."` and `env = true` populate a file in the allocation’s secrets dir. The driver automatically mounts the allocation’s secrets directory into the VM as read-only via `--dir=secrets:<path>:ro`.
- Reference: driver/tart_client.go:346-354, examples/example.nomad.hcl:30-39

How to use inside the VM:
- Locate shared directories (see “Access from Inside the VM”). Your secrets file (e.g. `secrets.env`) will be under the mounted secrets share.
- Source or read the file as needed (e.g., `set -a; . /path/to/secrets.env; set +a`).


## Networking Details and Using from the VM

Modes mapped to Tart flags:
- `shared`/`nat`/`default` (default): NAT; no special flags.
- `host`: Adds `--net-host` (VM shares host’s network namespace characteristics).
- `bridged`: Adds `--net-bridged <interface>`; requires `bridged_interface`.
- `softnet`: Adds `--net-softnet` plus optional `--net-softnet-allow <cidrs>` and `--net-softnet-expose <ports>`.

Softnet port mappings:
- `softnet_expose = ["2222:22", "8080:80"]` makes the VM’s internal ports reachable from the host network at the listed external ports.
- Inside the VM: services listen on their normal internal ports; no changes needed.

Reference: driver/networking.go:1-63, driver/config.go:37-52


## Access from Inside the VM

SSH
- Connect with the configured `ssh_user` and `ssh_password`.
- If you need the VM IP, on the host run `tart ip nomad-<ALLOC_ID>` (the driver names VMs `nomad-<allocid>`). Reference: driver/driver.go:229, driver/tart_client.go:301-314

Shared directories (including secrets)
- The driver passes Tart `--dir` flags for each directory mount.
- Inside macOS guests, Tart exposes shared directories via VirtioFS. To discover mount points:
  - Run `mount | grep -i virtiofs` or inspect volumes in Finder to locate the share named after your `directory.name` (when provided).
  - If `name` is omitted, the host path may be used to derive the visible name; use `mount` to confirm.

Root disk options
- Applied at start via `--root-disk-opts=...`; no guest action required. Read-only root will prevent writes to the system volume.

Logs
- The driver streams syslog from the VM using `log stream --style syslog --level info`; task logs are visible with `nomad logs`.
  - Reference: driver/driver.go:288-325


## End-to-End Example

```hcl
job "macos-sequoia-vanilla" {
  datacenters = ["dc1"]
  type        = "service"

  group "vms" {
    count = 1

    task "vm" {
      driver = "tart"

      template {
        data        = <<EOH
SSH_PASSWORD={{ with nomadVar "nomad/jobs/macos-sequoia-vanilla" }}{{ .ssh_password }}{{ end }}
EOH
        destination = "secrets/secrets.env"
        env         = true
      }

      config {
        url          = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
        ssh_user     = "admin"
        ssh_password = "${SSH_PASSWORD}"
        show_ui      = false
        disk_size    = 60

        # Networking examples (choose one mode)
        # network {
        #   mode = "bridged"
        #   bridged_interface = "en0"
        # }
        # network {
        #   softnet_allow  = ["192.168.0.0/24"]  # implies softnet
        #   softnet_expose = ["2222:22", "8080:80"]
        # }

        # Root disk behavior
        # root_disk {
        #   readonly     = true
        #   caching_mode = "automatic"  # or: uncached, cached
        #   sync_mode    = "full"       # or: fsync, none
        # }

        # Directory mounts
        # directory {
        #   name = "assets"
        #   path = "/Users/me/project/assets"
        #   options {
        #     readonly = true
        #     tag      = "assets"
        #   }
        # }
      }

      resources {
        cores  = 8
        memory = 10240
      }

      logs {
        max_files     = 3
        max_file_size = 10
      }
    }
  }
}
```


## Notes and Limitations

- Exec via Nomad’s standard `alloc exec` is not supported by this driver; the driver itself uses SSH for internal log streaming and exec calls.
  - Reference: driver/driver.go:196-222, 242-283
- Images are cloned on first use; large images take time. Update and progress deadlines in your job’s `update { }` block accordingly.
- Virtualization.framework on macOS typically limits concurrent VMs per host; consider using constraints in your job to avoid oversubscription (see `examples/example.nomad.hcl`).

