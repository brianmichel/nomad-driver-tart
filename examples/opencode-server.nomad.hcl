job "opencode-server" {
  datacenters = ["dc1"]
  type        = "batch"

  // Register this as a parameterized batch job. `nomad job run` only registers
  // the parent job; each VM starts when you dispatch it:
  //   nomad job dispatch opencode-server
  parameterized {
    payload = "forbidden"
  }

  group "servers" {
    count = 1

    // Virtualization.framework mandates a maximum of 2 VMs per host.
    constraint {
      attribute = attr.driver.tart.available_slots
      value     = "true"
    }

    network {
      port "http" {
        to = 4096
      }
    }

    task "vm" {
      driver = "tart"

      service {
        // Keep the service name static. Nomad does not interpolate allocation
        // runtime environment variables such as NOMAD_ALLOC_ID in this parent
        // parameterized job's service listing. Each dispatched allocation is
        // still a distinct service instance with its own AllocID, JobID,
        // address, and port in Nomad service discovery.
        name         = "opencode"
        provider     = "nomad"
        address_mode = "host"
        port         = "http"

        tags = ["opencode-server"]
      }

      // Set these strings directly for your environment.
      env {
        SSH_PASSWORD             = "admin"
        OPENCODE_HOSTNAME        = "0.0.0.0"
        OPENCODE_PORT            = "4096"
        OPENCODE_SERVER_USERNAME = "admin"
        OPENCODE_SERVER_PASSWORD = "change-me"
        ANTHROPIC_API_KEY        = ""
        OPENAI_API_KEY           = ""
        GITHUB_TOKEN             = ""
      }

      template {
        data        = <<EOH
SSH_PASSWORD={{ env "SSH_PASSWORD" }}
OPENCODE_HOSTNAME={{ env "OPENCODE_HOSTNAME" }}
OPENCODE_PORT={{ env "OPENCODE_PORT" }}
OPENCODE_SERVER_USERNAME={{ env "OPENCODE_SERVER_USERNAME" }}
OPENCODE_SERVER_PASSWORD={{ env "OPENCODE_SERVER_PASSWORD" }}
ANTHROPIC_API_KEY={{ env "ANTHROPIC_API_KEY" }}
OPENAI_API_KEY={{ env "OPENAI_API_KEY" }}
GITHUB_TOKEN={{ env "GITHUB_TOKEN" }}
EOH
        destination = "secrets/opencode.env"
        env         = true
      }

      template {
        data = <<EOF
#!/bin/bash
set -euo pipefail

SECRETS_FILE="/Volumes/My Shared Files/secrets/opencode.env"
if [[ -f "$SECRETS_FILE" ]]; then
  set -a
  source "$SECRETS_FILE"
  set +a
fi

export PATH="$HOME/.opencode/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
WORK_DIR="$HOME/opencode-workspace"
mkdir -p "$WORK_DIR"
cd "$WORK_DIR"

echo "opencode: startup begin $(date -u +%FT%TZ)"
echo "opencode: installing CLI if needed"
if ! command -v opencode >/dev/null 2>&1; then
  curl -fsSL https://opencode.ai/install | bash
  export PATH="$HOME/.opencode/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
fi

OPENCODE_BIN="$(command -v opencode || true)"
if [[ -z "$OPENCODE_BIN" ]]; then
  echo "opencode: opencode binary not found on PATH=$PATH"
  exit 1
fi

echo "opencode: version"
"$OPENCODE_BIN" --version || true

VM_IP="$(ipconfig getifaddr en0 2>/dev/null || true)"
if [[ -n "$VM_IP" ]]; then
  echo "opencode: VM IP is $VM_IP"
  echo "opencode: from the Nomad client, try http://$VM_IP:$${OPENCODE_PORT:-4096}"
fi

echo "opencode: serving $WORK_DIR on $${OPENCODE_HOSTNAME:-0.0.0.0}:$${OPENCODE_PORT:-4096}"
exec "$OPENCODE_BIN" serve --hostname "$${OPENCODE_HOSTNAME:-0.0.0.0}" --port "$${OPENCODE_PORT:-4096}"
EOF
        destination = "local/start-opencode.sh"
        perms       = "755"
      }

      config {
        url          = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
        ssh_user     = "admin"
        ssh_password = "${SSH_PASSWORD}"
        show_ui      = false

        // Softnet lets the driver map Nomad's allocated host port for the
        // "http" label to the guest service listening on port 4096.
        // With address_mode = "host", discover the service via the Nomad
        // service registration rather than the guest IP.
        network {
          mode          = "softnet"
          softnet_allow = ["0.0.0.0/0"]
        }

        command = "/bin/bash"
        args    = ["/Volumes/My Shared Files/local/start-opencode.sh"]

        directory {
          name = "local"
          path = "${NOMAD_TASK_DIR}"
        }
      }

      resources {
        cores  = 8
        memory = 10240
      }

      logs {
        max_files     = 5
        max_file_size = 10
      }
    }
  }
}
