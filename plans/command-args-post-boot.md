# Plan: `command` + `args` — Post-Boot Script Execution in the Tart Driver

## Context

The Tart driver currently boots a VM and streams its syslog over SSH, but
there's no way to run a startup command or script inside the VM **as part of the
job lifecycle**. Users want to define a script (potentially constructed via
Nomad `template` blocks) that gets executed inside the VM after it boots —
for example, to start `opencode` in server mode, provision tools, or run
configuration commands.

Following the convention established by Nomad's Docker, exec, raw_exec, and
java drivers, we'll add optional `command` (string) and `args` (list of string)
fields to the task config. When present, the driver will:

1. Boot the VM normally
2. Wait for SSH to become reachable (reuse the existing retry/backoff pattern)
3. Run the specified command inside the VM via SSH
4. Stream stdout/stderr to the Nomad task logs
5. Leave the VM running (the command is a one-shot; the VM lifetime is `tart run`)

## Approach

Add a new startup goroutine that runs alongside the existing syslog streaming
goroutine. Both share the same stdout/stderr file handles and use the same SSH
retry/backoff pattern already in `streamSyslogWithRetry`. The startup command
runs **once** (not continually retried like syslog), and errors are emitted as
task events rather than killing the VM.

### Key design decisions

| Decision | Rationale |
|---|---|
| Run command **after** syslog streaming starts | Syslog provides observability during the command run |
| **Do not** retry on command failure | A failed startup command is a user configuration issue; retrying would mask problems |
| Share stdout/stderr files between syslog and startup goroutines | All output lands in `nomad logs` in one place; no new file descriptors |
| Emit task events for startup progress | Users can see "startup command started/completed/failed" via `nomad alloc status` |
| `command` alone is sufficient; `args` is optional | Mirroring Docker driver semantics: `command` overrides entrypoint, `args` is the argument list |
| No-op when both fields are absent | Fully backward compatible |

## Files to modify

| File | Change |
|---|---|
| `driver/config.go` | Add `Command` and `Args` fields to `TaskConfig`; add HCL schema entries |
| `driver/driver.go` | Wire startup command execution into `StartTask`; add `executeStartupCommand` method; extract `waitForSSH` helper |
| `driver/handle.go` | Add `startupCancel` field to `taskHandle`; wire cancellation in `run()` |
| `driver/virtualizer.go` | **(no change)** — `ExecOptions` already has `Command []string` |
| `driver/tart_client.go` | **(no change)** — `Exec()` already accepts arbitrary `[]string` commands via SSH |
| `driver/startup_command_test.go` | **New** — table-driven tests covering all edge cases |
| `driver/driver_test.go` | Add assertions that `command`/`args` are decoded correctly from HCL |
| `docs/configuration.md` | Document `command` + `args` |
| `examples/example.nomad.hcl` | Add commented example showing the feature |

## Reuse

| Existing code | File | How it's reused |
|---|---|---|
| `streamSyslogWithRetry` retry/backoff loop | `driver/driver.go:273-308` | Extracted into a shared `waitForSSH` helper used by both syslog and startup command |
| `d.client.Exec(ctx, vmConfig, ExecOptions{...})` | `driver/tart_client.go:190-259` | Called directly to run the startup command |
| `d.eventer.EmitEvent(...)` | Used throughout `driver.go` | Emit startup progress events |
| `mockVirtualizer.Exec(...)` | `driver/driver_test.go:46` | Already stubs `Exec` for unit tests |
| `execCommandContext` swap pattern | `driver/setup_auth_test.go:79-83` | Same pattern for testing `tart` CLI calls in integration-style tests |

## Steps

### 1. Refactor: extract a shared `waitForSSH` helper

- [ ] In `driver/driver.go`, extract the retry/backoff loop from
  `streamSyslogWithRetry` into a standalone function:

  ```go
  // waitForSSH blocks until SSH is reachable on the VM or ctx is cancelled.
  // It performs a lightweight probe by attempting to get the VM's IP address.
  // Returns nil when the VM is ready, or ctx.Err() on cancellation.
  func (d *Driver) waitForSSH(ctx context.Context, vmConfig VMConfig) error {
      backoff := 1 * time.Second
      maxBackoff := 10 * time.Second

      for {
          select {
          case <-ctx.Done():
              return ctx.Err()
          default:
          }

          // Lightweight probe: just check that we can get the VM's IP.
          // If the IP is available, SSH is almost certainly ready.
          vmName := d.generateVMName(vmConfig.NomadConfig.AllocID)
          ip, err := d.client.IPAddress(ctx, vmName)
          if err == nil && ip != "" {
              return nil
          }

          select {
          case <-ctx.Done():
              return ctx.Err()
          default:
          }

          time.Sleep(backoff)
          if backoff < maxBackoff {
              backoff *= 2
              if backoff > maxBackoff {
                  backoff = maxBackoff
              }
          }
      }
  }
  ```

  Note: Using `IPAddress` as the probe (rather than a full `Exec`) is
  lighter-weight. The Tart client's `IPAddress` method runs `tart ip <vm>`,
  which is fast and indicates the VM is far enough along in boot that SSH
  will be  respond.

- [ ] Replace the retry loop body in `streamSyslogWithRetry` with a call to
  `d.waitForSSH(syslogCtx, vmConfig)`. The method becomes:

  ```go
  func (d *Driver) streamSyslogWithRetry(ctx context.Context, vmConfig VMConfig, stdout, stderr io.WriteCloser) {
      if err := d.waitForSSH(ctx, vmConfig); err != nil {
          return // context cancelled
      }

      _, err := d.client.Exec(ctx, vmConfig, ExecOptions{
          Command: []string{"/usr/bin/log", "stream", "--style", "syslog", "--level=info"},
          Stdout:  stdout,
          Stderr:  stderr,
          Tty:     false,
      })

      if err != nil {
          d.logger.Warn("Log streaming ended with error", "error", err)
      }
  }
  ```

  Wait — this changes the semantics: the old code retries syslog streaming
  indefinitely (useful if SSH drops), while the new approach would only
  wait for initial SSH readiness and then run syslog once. To preserve the
  original behavior, keep `streamSyslogWithRetry` as-is but have it call
  `waitForSSH` before its first `Exec` attempt. The retry loop stays for
  connection drops.

  **Better approach to avoid scope creep:** Don't refactor
  `streamSyslogWithRetry` at all. Instead, create a separate `waitForSSH`
  that the new startup path calls. This avoids touching the syslog code
  path and eliminates risk of regression.

- [ ] Update: Keep `streamSyslogWithRetry` untouched. Add `waitForSSH` as
  a new standalone method that is only called by `executeStartupCommand`.

### 2. Add `Command` and `Args` to `TaskConfig` and HCL schema

- [ ] In `config.go`, add fields to `TaskConfig`:

  ```go
  // Command is an optional command to run inside the VM after SSH becomes
  // available, following the convention of Nomad's Docker/exec drivers.
  // Output is streamed to the task's stdout/stderr.
  Command string   `codec:"command"`
  // Args are optional arguments passed to Command.
  Args    []string `codec:"args"`
  ```

- [ ] In the `taskConfigSpec` HCL schema, add:

  ```go
  "command": hclspec.NewAttr("command", "string", false),
  "args":    hclspec.NewAttr("args", "list(string)", false),
  ```

No validation is needed at the schema level — an empty `command` means "don't
run anything," which is the fully-backward-compatible default.

### 3. Add the `startupCancel` field to `taskHandle`

- [ ] In `handle.go`, add the field:

  ```go
  type taskHandle struct {
      // ... existing fields ...

      // startupCancel cancels the startup command goroutine when the
      // task stops or is destroyed.
      startupCancel context.CancelFunc
  }
  ```

- [ ] In `handle.run()`, cancel startup before syslog:

  ```go
  func (h *taskHandle) run() {
      defer close(h.doneCh)
      if h.startupCancel != nil {
          h.startupCancel()
      }
      if h.syslogCancel != nil {
          defer h.syslogCancel()
      }
      // ... rest unchanged
  }
  ```

This ordering ensures the startup goroutine stops before the syslog goroutine
closes the shared file handles.

### 4. Add `waitForSSH` and `executeStartupCommand` methods on `Driver`

- [ ] Add `waitForSSH`:

  ```go
  // waitForSSH blocks until the VM reports an IP address (indicating SSH
  // should be reachable) or ctx is cancelled. Retries with exponential
  // backoff from 1s to 10s.
  func (d *Driver) waitForSSH(ctx context.Context, vmConfig VMConfig) error
  ```

- [ ] Add `executeStartupCommand`:

  ```go
  // executeStartupCommand waits for SSH to become available, then runs
  // the configured command+args inside the VM. Output is written to the
  // provided stdout/stderr writers.  This is a one-shot execution — it
  // does not retry on command failure. The VM is left running regardless
  // of the command's exit status.
  func (d *Driver) executeStartupCommand(
      ctx context.Context,
      vmConfig VMConfig,
      stdout, stderr io.Writer,
  )
  ```

  Behavior:
  1. Call `d.waitForSSH(ctx, vmConfig)` — if context is cancelled, return
     silently.
  2.encoding Build the command slice:
     - If `Command` is set, use it as element 0 followed by `Args...`.
     - If `Command` is empty but `Args` is set, emit a
       `"Startup command misconfigured"` task event and return.
     - If both are empty, return immediately (shouldn't happen since
       caller checks this, but guard anyway).
  3. Emit a `"Running startup command"` task event with the command
     in the annotations.
  4. Call `d.client.Exec(ctx, vmConfig, ExecOptions{
         Command: fullCmd,
         Stdout:  stdout,
         Stderr:  stderr,
         Tty:     false,
     })`
  5. If `Exec` returns an error:
     - If it's a context cancellation, return silently.
     - Otherwise emit `"Startup command failed"` with the error.
  6. If `Exec` returns a non-zero exit code, emit
     `"Startup command completed"` with `annotations["exit_code"]`.
  7. If `Exec` returns exit code 0, emit
     `"Startup command completed"`.

  **The VM is never killed by this method.** Errors are informational.

### 5. Wire into `StartTask`

- [ ] In `StartTask()`, right after the syslog goroutine is spawned and
  the task handle is stored, add:

  ```go
  // If a startup command is configured, run it once SSH is available.
  if taskConfig.Command != "" || len(taskConfig.Args) > 0 {
      startupCtx, startupCancel := context.WithCancel(d.ctx)
      h.startupCancel = startupCancel
      go func() {
          defer startupCancel()
          d.executeStartupCommand(startupCtx, vmConfig, stdoutFile, stderrFile)
      }()
  }
  ```

  This must be placed **after** `d.tasks.Set(cfg.ID, h)` and **before**
  `go h.run()`, so the `taskHandle` is16t available when `run()` fires
  the cancellation.

  Actually, looking more carefully at the order in the current code:

  ```go
  go func() {
      defer stdoutFile.Close()
      defer stderrFile.Close()
      d.streamSyslogWithRetry(syslogCtx, vmConfig, stdoutFile, stderrFile)
  }()
  d.tasks.Set(cfg.ID, h)
  go h.run()
  ```

  The startup goroutine should be spawned in the same area. Since it
  shares `stdoutFile`/`stderrFile` with syslog, and those files are
  closed by the syslog goroutine's deferred close, the startup goroutine
  must finish before syslog closes them. This is handled by the
  cancellation order in `h.run()`: startup is cancelled first, so the
  startup goroutine exits, THEN syslog is cancelled which triggers the
  deferred close.

### 6. Testing

#### 6a. Config decoding tests

- [ ] In a new `config_test.go` (or extend `driver_test.go`), add tests
  that verify `TaskConfig` correctly decodes:
  - `command` alone
  - `command` + `args`
  - `args` without `command`
  - Neither field present (both zero values)

#### 6b. Startup behavior tests (new file `startup_command_test.go`)

All tests use the existing `mockVirtualizer` from `driver_test.go`,
potentially extended with configurable `Exec` behavior.

- [ ] **No command set** — `StartTask` with no `command`/`args`.
  Verifies `mockVirtualizer.Exec` is never called for startup purposes.
  Task enters running state normally.

- [ ] **Command succeeds (exit 0)** — `command = "/bin/sh"`, `args =
  ["-c", "echo ok"]`. Verifies:
  - Task enters running state
  - `Exec` is called with `["/bin/sh", "-c", "echo ok"]`
  - A "completed" task event with exit_code=0 is emitted

- [ ] **Command fails (non-zero exit)** — `command = "/bin/sh"`, `args =
  ["-c", "exit 5"]`. Verifies:
  - Task **stays running** (VM not killed)
  - A "completed" task event with exit_code=5 is emitted

- [ ] **Command binary not found** — `command = "/bin/nonexistent"`.
  Verifies:
  - `Exec` returns an SSH error
  - A "failed" task event is emitted
  - Task stays running

- [ ] **SSH never available** — `mockVirtualizer.IPAddress` always
  returns `""` / error. Verifies:
  - `waitForSSH` retries until context cancellation
  - When task is stopped, goroutine exits cleanly
  - No panic, no leaked goroutine

  Note: this requires adding `IPAddress` to `mockVirtualizer` (currently
  not stubbed there).

- [ ] **Args without command** — `args = ["-c", "echo hi"]`, no
  `command`. Verifies:
  - A "misconfigured" task event is emitted
  - `Exec` is never called
  - Task stays running

- [ ] **VM stops during startup execution** — context is cancelled while
  `executeStartupCommand` is in `d.client.Exec()`. Verifies:
  - Goroutine exits without panicking
  - No writes to closed files

- [ ] **Pull-only task ignores command** — `pull_only = true` with
  `command` set. Verifies:
  - `startPullOnlyTask` path is taken
  - No startup goroutine is spawned
  - Task completes when pull finishes

- [ ] **Task recovery replays startup** — Simulate `RecoverTask` calling
  `StartTask`. Verifies a fresh startup goroutine is spawned (the
  recovered task gets a startup command just like the original).

#### 6c. Integration-style test

- [ ] Following the pattern in `setup_auth_test.go`, swap
  `execCommandContext` and verify that when a startup command is
  configured, the correct args flow through to the SSH execution layer.

### 7. Documentation and example

- [ ] Add `command` + `args` to `docs/configuration.md` under the task
  config section, with usage notes:
  - Both are optional
  - `command` is the executable path inside the VM
  - `args` are arguments passed to it
  - Output goes to `nomad logs`
  - Non-zero exits do not kill the VM
  - Works with `template` blocks for script injection

- [ ] Add a commented example in `examples/example.nomad.hcl`:

  ```hcl
  # (optional) Post-boot command to run inside the VM
  # after SSH becomes available. Output goes to nomad logs.
  # command = "/bin/bash"
  # args    = ["-c", "echo 'VM is ready'"]
  ```

## Edge cases and failure modes

| Scenario | Behavior |
|---|---|
| `command` + `args` both absent | No change; task behaves exactly as before |
| `command` set but VM never gets an IP | `waitForSSH` retries until context cancelled; goroutine exits cleanly on stop |
| `command` runs but exits non-zero | Event emitted with exit code; VM stays running |
| `command` is not a valid path inside VM | SSH session returns error; event emitted; VM stays running |
| VM stops while command is running | Context cancelled; SSH session killed; goroutine exits |
| `pull_only = true` + `command` set | `startPullOnlyTask` path ignores command; no VM created |
| Nomad client restarts (task recovery) | `RecoverTask` → `StartTask` spawns fresh startup goroutine |
| Multiple jobs on same host | Each alloc has unique VM name; startup commands isolated |
| `args` set but `command` empty | Misconfiguration event emitted; VM stays up; no Exec call |
| Startup command produces lots of output | Goes to the same stdout file as syslog; subject to Nomad's log rotation |
| `command` with spaces/special chars | HCL string escaping handles this; passed verbatim to SSH |

## Verification

1. **Build**: `make build`
2. **Unit tests**: `go test ./driver/... -v -count=1`
3. **Race detector**: `go test ./driver/... -race -count=1`
4. **Manual integration test**:
   - Start Nomad dev agent with the plugin
   - Submit a job with:
     ```hcl
     config {
       url          = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
       ssh_user     = "admin"
       ssh_password = "..."
       command      = "/bin/sh"
       args         = ["-c", "echo 'startup ran' >> /tmp/startup.log"]
     }
     ```
   - Confirm `nomad alloc exec <alloc> cat /tmp/startup.log` shows
     `startup ran`
   - Confirm `nomad alloc status <alloc>` events show
     "Running startup command" and "Startup command completed"
   - Confirm `nomad logs <alloc>` contains both syslog and startup output
5. **Backward compat**: Submit same job **without** `command`/`args` —
   verify identical behavior to current release
6. **Failure mode**: Set `command = "/bin/nonexistent"` — verify VM
   stays running, event shows failure