# Linux test portability investigation

Baseline: `d256b75279d78cbe7b34cedb3210270136699238`, Go 1.27.2 via mise, Linux host. The driver uses Nomad's executor and targets Tart on macOS; successful cross-compilation is not a macOS/Tart runtime test.

## Root cause and correction

The main module required runc v1.4.0 but replaced **every** runc version with v1.1.9. The old runc expects older runtime-spec and cilium APIs: its `specconv` assigns the current `*int64` PIDs limit to an `int64`, and its eBPF code uses a removed `RawAttachProgramOptions.Replace` field. Linux builds include these files; Darwin builds do not exercise the same code.

The override was introduced in commit `95bcc641` ("Add mise"), while changing Nomad from v1.10.2 to v1.10.1. That commit gives no explanation for the pin; its original rationale cannot be established from the commit message or diff. Nomad v1.10.1 itself required runc v1.2.6, runtime-spec v1.2.1, and cilium/ebpf v0.16.0. Current Nomad v1.11.3 requires runc v1.4.0, runtime-spec v1.3.0, and cilium/ebpf v0.17.3; this project selects cilium/ebpf v0.18.0. The selected runc is therefore aligned with Nomad's direct requirement, not a downgrade to accommodate tests. Removing the stale override and running `go mod tidy` updates the actual indirect graph, including go-criu/v5 to v7, and go.sum. No security assessment of the resulting complete dependency graph is implied.

## Reproduction and verification

Commands below were run in the worktree. All success statements mean exit 0. No Tart service or macOS host was available.

### RED, before correction

```sh
mise exec -- go test ./... -count=1
```

Exit 1: `runc@v1.1.9/libcontainer/specconv/spec_linux.go:749:29: cannot use r.Pids.Limit (variable of type *int64) as int64 value in assignment` and `runc@v1.1.9/libcontainer/cgroups/ebpf/ebpf_linux.go:190:3: unknown field Replace in struct literal of type link.RawAttachProgramOptions`; both project packages failed to build. This identical full-suite command is the red/green regression check.

A narrower diagnostic also failed with those same two errors:

```sh
mise exec -- go test github.com/opencontainers/runc/libcontainer/specconv github.com/opencontainers/runc/libcontainer/cgroups/ebpf -run '^$'
```

After correction this diagnostic is not a valid green check: the old `libcontainer/cgroups/ebpf` package no longer exists in runc v1.4.0 (the specconv package itself passed). The full project suite below exercises the actual current graph without depending on obsolete package paths.

### GREEN and additional checks

```sh
mise exec -- go mod edit -dropreplace=github.com/opencontainers/runc
mise exec -- go mod tidy
mise exec -- go test ./... -count=1
mise exec -- go vet ./...
mise exec -- go mod verify
mise exec -- go list -m github.com/opencontainers/runc github.com/opencontainers/runtime-spec github.com/cilium/ebpf
git diff --check
```

All passed: root package has no test files; driver tests passed (4.112s on repeat); vet and diff check were silent; module verification returned `all modules verified`. The selected versions were runc v1.4.0, runtime-spec v1.3.0, and cilium/ebpf v0.18.0.

```sh
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 mise exec -- go build ./...
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 mise exec -- go vet ./...
mkdir -p /opt/data/profiles/coding/cache/scratch/tart-darwin-test-compile
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 mise exec -- go test -c -o /opt/data/profiles/coding/cache/scratch/tart-darwin-test-compile/driver.test ./driver
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 mise exec -- go test -c -o /opt/data/profiles/coding/cache/scratch/tart-darwin-test-compile/main.test .
```

All passed. The root package has no test files; the driver test binary was produced. Its first eight bytes were `cffaedfe0c000001` (64-bit little-endian Mach-O, ARM64 CPU). These commands compile but do not execute Darwin tests. An initial combined check also attempted `file` on the binary; that final inspection command failed because `file` is not installed, after the build, vet and test-compile commands had succeeded. Header inspection via Python replaced it.

## Remaining risk

Run the actual test suite and Nomad/Tart VM lifecycle integration on a supported macOS arm64 host. The purpose of the historical runc override remains undocumented, so maintainers should confirm whether it addressed a now-obsolete platform-specific issue. This change does not claim a security audit or runtime validation of runc or Tart.
