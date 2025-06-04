job "example-tart" {
  datacenters = ["dc1"]
  type        = "service"

  group "example" {
    count = 1

    task "tart-vm" {
      driver = "tart"

      config {
        image   = "example-vm"
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
