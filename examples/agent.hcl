plugin "nomad-driver-tart" {
  config {
    enabled = true
  }
}

client {
  enabled = true
  
  # Enable the tart driver
  options {
    "driver.allowlist" = "tart"
  }
}

server {
  enabled = true
  bootstrap_expect = 1
}
