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

      # Setup password with a secure Nomad var
      # Example:
      #   nomad var put nomad/jobs/example-tart ssh_password="your VM password"
      template {
        data        = <<EOH
SSH_PASSWORD={{ with nomadVar "nomad/jobs/example-tart" }}{{ .ssh_password }}{{ end }}
EOH
        destination = "secrets/file.env"
        env         = true
      }

      config {
        url          = "ghcr.io/cirruslabs/macos-sequoia-vanilla:latest"
        name         = "example-vm"
        ssh_user     = "admin"
        ssh_password = "${SSH_PASSWORD}"
        command      = "/bin/echo"
        args         = ["Hello from Tart VM!"]
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
