# Enterprise operator runbook (draft)

**Not final acceptance:** R10 Apple Silicon host results are unavailable. No macOS
release-signature, guest, lifecycle, resource, or network behavior has been
verified for this draft. Do not treat this as a supported-version matrix or
production qualification. Record the exact driver release/build, Nomad client,
Tart, macOS build, hardware, Softnet, image digest, and test results for each
approved combination. `go.mod` and `mise.toml` pin Go **1.27.2** for this source;
the Nomad module dependency is **1.11.3**, not a promise that a particular
client version is supported. The published [v2.0.0 release](https://github.com/brianmichel/nomad-driver-tart/releases/tag/v2.0.0)
is an artifact example, not a qualification of this later draft.

## Trust and prerequisites

Treat this implementation as **trusted-host, single-tenant**: a job can request
host directory mounts and network modes, and the driver automatically shares the
task secrets directory with the guest read-only. Read-only does not prevent a
guest from reading or exfiltrating secrets. Restrict job submission, Nomad ACLs,
variable paths, access to client hosts and logs, and host filesystem permissions.
There is no shipped restricted host-access policy; a possible restricted mode is
pending policy review. The SSH client currently accepts any host key
(`driver/tart_client_exec.go`); use a trusted isolated network and do not claim SSH
server identity verification. SSH-trust compatibility decisions remain pending.

Use a dedicated service identity with only the macOS, Tart storage, keychain,
Nomad plugin and network permissions required for the selected version and
workloads; keep the plugin directory and executables administrator-owned and not
writable by job authors. Avoid broad sudo or world-readable secrets. Install Tart
and any required Softnet helper through your approved supply chain and ensure they
are available to the Nomad service/executor on a controlled PATH. The driver
prepends common Homebrew paths for its executor; test the actual service context,
not just an interactive shell. It uses Go's SSH client, not `sshpass`.

[Tart's quick start](https://tart.run/quick-start/) documents
`brew install cirruslabs/cli/tart` and an archive install; the
[Cirrus tap](https://github.com/cirruslabs/homebrew-cli) lists Tart and Softnet.
However, an [upstream migration report](https://github.com/openai/tart/issues/1298)
identifies that quick-start tap as stale for newer versions. Confirm the official
installation path and exact formula or release archive for the **specific Tart
version** selected; [current upstream source](https://github.com/openai/tart/blob/main/docs/quick-start.md)
documents `brew install openai/tools/tart`. Neither command pins a version.
This repository's
GoReleaser configuration does not publish a driver Homebrew tap. Install the
driver from a verified GitHub release archive or build the reviewed source using
`mise run build`; the output is `dist/nomad-driver-tart`. For portable source
checks use `mise run vet` and `mise run test`; `make test` is a no-op.

Before deployment, review the exact Tart version's license and support terms,
the macOS host/guest and image licenses and redistribution/virtualization rights,
and image provenance, patch ownership and support arrangements. Pin an approved
image by immutable digest where supported and verify it with your registry policy;
`latest` in existing examples is not an enterprise pin. Obtain legal and support
approval for the exact combination, not just a generic Tart license link.

## Verify and install a release

The release configuration builds `darwin/arm64`, runs Apple `codesign` using a
Developer ID Application identity, and configures Cosign signatures for all
artifacts. The v2.0.0 release lists an archive, checksum file, and a `.sig` for
each. This is configuration and published asset evidence, **not a successful
verification**. No public verification key or expected Apple Team ID is supplied
in this repository or the listed v2.0.0 assets. Obtain the Cosign public key and
expected Apple signing identity/Team ID from the maintainer over an independent,
trusted channel; stop if unavailable or mismatched. Never use a key supplied only
alongside an otherwise untrusted download as its own trust anchor.

On the qualification Mac, download these four matching assets from the release
page into an empty directory, and independently provision the trusted public key
as `trusted-cosign.pub`:

```text
nomad-driver-tart_2.0.0_darwin_arm64.tar.gz
nomad-driver-tart_2.0.0_darwin_arm64.tar.gz.sig
nomad-driver-tart_2.0.0_checksums.txt
nomad-driver-tart_2.0.0_checksums.txt.sig
```

With an approved compatible Cosign installed, verify the signed checksum file
and archive, then check the archive against the authenticated checksum. The
filtered check avoids requiring unrelated files should a future release contain
additional checksums. Stop on any failure; inspect the checksum line before use.

```sh
cosign verify-blob --key trusted-cosign.pub --signature nomad-driver-tart_2.0.0_checksums.txt.sig nomad-driver-tart_2.0.0_checksums.txt
cosign verify-blob --key trusted-cosign.pub --signature nomad-driver-tart_2.0.0_darwin_arm64.tar.gz.sig nomad-driver-tart_2.0.0_darwin_arm64.tar.gz
grep '  nomad-driver-tart_2.0.0_darwin_arm64.tar.gz$' nomad-driver-tart_2.0.0_checksums.txt | shasum -a 256 -c
```

Ensure the checksum command actually checked exactly the intended archive; an
empty filter is not verification. Inspect archive entries before extraction,
then extract in a clean directory and check the actual binary on macOS:

```sh
tar -tzf nomad-driver-tart_2.0.0_darwin_arm64.tar.gz
tar -xzf nomad-driver-tart_2.0.0_darwin_arm64.tar.gz
codesign --verify --strict --verbose=2 ./nomad-driver-tart
codesign -dv --verbose=4 ./nomad-driver-tart 2>&1
```

Compare the displayed authority and TeamIdentifier with the independently
approved identity. `codesign --verify` alone does not establish that identity;
this workflow does not establish notarization. Save output and record the
artifact digest and key fingerprint in the qualification record. On failure,
quarantine the artifact and investigate rather than bypassing the checks.

After approval, place the binary in Nomad's configured plugin directory using
your controlled deployment mechanism. Keep the previous verified binary and its
configuration available for rollback. Configure the client as in
[`configuration.md`](configuration.md), restart through the approved maintenance
procedure, and inspect client driver health and the plugin/Tart version
attributes. This branch's version fingerprint may not exist in older releases.

## Example: protected credentials and narrow requested network

This is a syntax example, **not proof of isolation or working VM behavior**.
Set the image reference to an approved immutable digest and provision the Nomad
variable securely with ACLs restricted to this job. Do not put the password on a
command line, in the image reference, or in job source. The template uses a
Nomad variable and injects the value into driver config. `toJSON` preserves
special characters in the env file and `perms = "0600"` limits file access, as
recommended by the [Nomad template documentation](https://developer.hashicorp.com/nomad/docs/job-specification/template#environment-variables).
The secret file is also
shared with the guest. Restrict access to Nomad job data, allocations, guest and
logs. Replace the documentation CIDR with only the approved destination range.
Do not define Nomad network ports unless intended: explicitly selected Softnet
can append allocated ports to the expose list. Validate actual traffic and
helper behavior on the qualified host; supplement with independent network
controls. The mapping from `cores`/`memory` to guest sizing remains unresolved
(see [configuration](configuration.md#vm-resources-cpu-memory)).

```hcl
job "enterprise-tart" {
  datacenters = ["dc1"]
  type = "service"
  group "vm" {
    task "guest" {
      driver = "tart"
      template {
        data = <<EOH
SSH_PASSWORD={{ with nomadVar "nomad/jobs/enterprise-tart" }}{{ .ssh_password | toJSON }}{{ end }}
EOH
        destination = "secrets/ssh.env"
        perms = "0600"
        error_on_missing_key = true
        env = true
      }
      config {
        url = "registry.example.com/team/approved-image@sha256:REPLACE_WITH_APPROVED_DIGEST"
        ssh_user = "service-user"
        ssh_password = "${SSH_PASSWORD}"
        network {
          mode = "softnet"
          softnet_allow = ["192.0.2.0/24"]
        }
      }
      resources {
        cores = 2
        memory = 4096
      }
    }
  }
}
```

## Change, rollback and incidents

- **Drain before change:** identify the exact client and affected allocations;
  use `nomad node eligibility -disable <node-id>` and `nomad node drain -enable
  -no-deadline <node-id>` under your Nomad version's procedures. Without
  `-no-deadline` the documented default deadline is one hour and remaining
  allocations can be forcibly removed. Check `nomad node status
  <node-id>` and allocation status until workloads have migrated or stopped;
  ensure capacity and workload-specific disruption approval. Do not force a drain
  deadline or stop a live VM without authorization. Keep the node ineligible
  during maintenance.
- **Upgrade/rollback:** save the exact previous binary, configuration and image
  references; deploy the verified candidate and restart the client/plugin under
  your change procedure. Check driver health and run an approved canary; if it
  fails, keep the node ineligible, drain any new allocations, restore the prior
  verified binary/configuration, restart, and validate again.  Re-enable scheduling
  only after acceptance: `nomad node drain -disable -keep-ineligible <node-id>` then `nomad node
  eligibility -enable <node-id>`. Older releases may lack newer fixes/attributes;
  assess downgrade compatibility before rollback.
- **Credential rotation:** rotate guest SSH credentials in the image/guest and
  protected Nomad variable in a coordinated change, then deliberately replace
  allocations so the driver's captured config and guest credentials agree;
  do not assume a template update changes a running driver's SSH credentials.
  Rotate registry credentials at the issuer and the protected source; review the
  exact Tart version's keychain/credential-helper storage and remove obsolete
  credentials using its supported procedure. Test new pulls before revoking the
  old credential where policy allows. Restrict logs and rotate any exposed secret;
  driver diagnostics are not a general log scrubber.
- **Disk/cache pressure:** make the affected client ineligible, inspect free
  space, running allocations and Tart's local image/VM inventory. Preserve
  evidence, logs and required images; add capacity or remove only confirmed
  unused cache/images with the installed Tart version's documented commands.
  Avoid broad cache deletion or deleting live VM disks. Prewarm only when capacity
  allows and account for large pulls/clones.
- **Failed cleanup/orphans:** capture client/task events and `nomad alloc status
  <allocation-id>`, then compare with `tart list` under the same service identity.
  At this draft's base, driver VMs are named `nomad-<allocation-id>`; R02/#48
  introduces persisted per-task identities. Use the exact `vm_name` task attribute
  when available. A name or prefix alone is not proof of ownership or inactivity. Failed setup after clone can leave a VM. Correlate
  allocation, host, process and VM state; confirm it is no longer running or
  required, preserve evidence and obtain authorization before using the installed
  Tart version's stop/delete commands on that specific VM. Never bulk-delete by
  prefix. Investigate cleanup errors, permissions and disk pressure before
  returning the client to service.

## Acceptance still required

On a real Apple Silicon macOS host, record exact versions and independently verify
release checksum, Cosign signature and Apple code-signing identity; exercise
installation, ACL-protected secret delivery/rotation, resource sizing, Softnet
traffic, lifecycle, drain/rollback, cache pressure and orphan recovery. Capture
results and obtain licensing/support and security review. Syntax or Linux tests,
cross-compilation and the existence of configured signing do not satisfy these
gates. Pending policy decisions and roadmap fixes must be resolved or explicitly
accepted before final R12 sign-off.
