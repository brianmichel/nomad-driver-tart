// Pre-pull a Tart image onto selected Nomad clients so that subsequent
// VM-running tasks can skip the registry download.
//
// This is a `sysbatch` job: Nomad runs it once per eligible client and the
// allocation completes when `tart pull` exits. Re-running the job is safe;
// tart will no-op when the image is already cached.
//
// Example:
//   nomad job run examples/prewarm.nomad.hcl
//   nomad job status tart-prewarm-macos-sequoia
job "tart-prewarm-macos-sequoia" {
  datacenters = ["dc1"]
  type        = "sysbatch"

  // Restrict to nodes that have the tart driver available. Add your own
  // constraints (node class, hostname, etc.) to target specific clients.
  constraint {
    attribute = "${attr.driver.tart}"
    value     = "1"
  }

  group "prewarm" {
    task "pull" {
      driver = "tart"

      config {
        url       = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
        pull_only = true

        // auth is optional; only required for private registries.
        // auth {
        //   username = "..."
        //   password = "..."
        // }
      }

      resources {
        cpu    = 200
        memory = 256
      }
    }
  }
}
