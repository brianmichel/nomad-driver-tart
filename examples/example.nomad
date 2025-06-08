job "macos-sequoia-vanilla" {
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

  group "vms" {
    count = 2

    task "vm" {
      driver = "tart"

      # Setup password with a secure Nomad var
      # Example:
      #   nomad var put nomad/jobs/macos-sequoia-vanilla ssh_password="your VM password"
      template {
        data        = <<EOH
SSH_PASSWORD={{ with nomadVar "nomad/jobs/macos-sequoia-vanilla" }}{{ .ssh_password }}{{ end }}
EOH
        destination = "secrets/file.env"
        env         = true
      }

      config {
        url          = "ghcr.io/cirruslabs/macos-sequoia-vanilla:latest"
        ssh_user     = "admin"
        ssh_password = "${SSH_PASSWORD}"
        # Whether or not to show the built-in Tart UI for the VM
        # Defaults to false
        show_ui      = true
      }

      resources {
        cores = 4
        memory = 8192 # 8GB
      }

      logs {
        max_files     = 3
        max_file_size = 10
      }
    }
  }
}
