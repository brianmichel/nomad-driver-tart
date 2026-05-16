job "python-fileserver" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    max_parallel      = 0
    healthy_deadline  = "30m"
    progress_deadline = "60m"
  }

  group "servers" {
    count = 1

    constraint {
      attribute = attr.driver.tart.available_slots
      value     = "true"
    }

    network {
      port "http" {
        to = 8000
      }
    }

    task "vm" {
      driver = "tart"

      service {
        name         = "python-fileserver"
        provider     = "nomad"
        port         = "http"
        address_mode = "host"

        tags = ["tart", "python", "fileserver"]
      }

      template {
        data = <<EOF
#!/bin/bash
set -euo pipefail

cd "$HOME"

echo "fileserver: startup begin $(date -u +%FT%TZ)"
echo "fileserver: serving $HOME on 0.0.0.0:8000"
exec python3 -m http.server 8000 --bind 0.0.0.0 --directory "$HOME"
EOF
        destination = "local/start-fileserver.sh"
        perms       = "755"
      }

      config {
        url          = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
        ssh_user     = "admin"
        ssh_password = "admin"
        show_ui      = false

        // Softnet is the mode that can eventually expose Nomad-allocated
        // host ports via Tart's --net-softnet-expose behavior.
        network {
          mode          = "softnet"
          softnet_allow = ["0.0.0.0/0"]
        }

        command = "/bin/bash"
        args    = ["/Volumes/My Shared Files/local/start-fileserver.sh"]

        directory {
          name = "local"
          path = "${NOMAD_TASK_DIR}"
        }
      }

      resources {
        cores  = 4
        memory = 8192
      }

      logs {
        max_files     = 5
        max_file_size = 10
      }
    }
  }
}
