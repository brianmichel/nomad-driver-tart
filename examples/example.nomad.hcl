job "macos-sequoia-vanilla" {
  datacenters = ["dc1"]
  type        = "service"

  update {
    // Don't leave anything running in parallel when we're rescheduling.
    max_parallel = 0
    // Downloading a VM image can take a while as they are
    // tens of GBs in size. Give our jobs enough grace to
    // get setup properly.
    healthy_deadline  = "30m"
    progress_deadline = "60m"
  }

  group "vms" {
    count = 1

    // Virtualization.framework mandates a maximum of 2 VMs per host.
    // We can use this constraint to avoid scheduling errors due to
    // attempted VM oversubscription on a node.
    constraint {
      attribute = attr.driver.tart.available_slots
      value     = "true"
    }

    task "vm" {
      driver = "tart"

      # Setup password with a secure Nomad var
      # Example:
      #   nomad var put nomad/jobs/macos-sequoia-vanilla ssh_password="your VM password"
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
        # Whether or not to show the built-in Tart UI for the VM
        # Defaults to false
        show_ui = true
        # (optional) Networking options (mutually exclusive)
        # Default: shared/NAT (no option required)
        # network {
        #   mode = "bridged"           # or "host" | "softnet"
        #   bridged_interface = "en0"  # or "Wi-Fi" when mode = "bridged"
        #   softnet_allow = ["192.168.0.0/24"]
        #   softnet_expose = ["2222:22", "8080:80"]
        # }
        #
        # (optional) Root Disk options
        # root_disk {
        #   readonly = true            # or true
        #   caching_mode = "automatic" # (optional), or "uncached" | "cached"
        #   sync_mode = "full"         # (optional), or "fsync" | "none"
        # }
        # (optional) External directory mapping options
        # Specify which directories on the physical machine to
        # map into the virtual machine
        # directory {
        #   name = "downloads"
        #   path = "/Users/admin/Downloads"
        #   options = {
        #     readonly = true
        #   }
        # }
        # directory {
        #   name = "desktop"
        #   path = "/Users/admin/Desktop"
        # }
      }

      resources {
        cores  = 8
        memory = 10240 # 10GB
      }

      logs {
        max_files     = 3
        max_file_size = 10
      }
    }
  }
}
