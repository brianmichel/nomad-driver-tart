package driver

import (
    "context"
    "encoding/json"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    "github.com/hashicorp/go-hclog"
    "github.com/hashicorp/nomad/plugins/drivers"
)

// cmdRecord captures a single executed command invocation for assertions.
type cmdRecord struct {
    Name string   `json:"name"`
    Args []string `json:"args"`
    Env  []string `json:"env"`
}

// TestHelperProcess is invoked as a subprocess to record command invocations.
// It writes a JSON line with the command/args/env to the file at CMD_LOG.
func TestHelperProcess(t *testing.T) {
    if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
        return
    }
    // Find the split marker "--" to extract original command name/args.
    sep := 0
    for i, a := range os.Args {
        if a == "--" {
            sep = i
            break
        }
    }
    // Default if not found
    name := ""
    var args []string
    if sep > 0 && sep+1 < len(os.Args) {
        name = os.Args[sep+1]
        if sep+2 <= len(os.Args) {
            args = os.Args[sep+2:]
        }
    }

    rec := cmdRecord{Name: name, Args: args, Env: os.Environ()}
    b, _ := json.Marshal(rec)
    logPath := os.Getenv("CMD_LOG")
    if logPath != "" {
        f, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
        if f != nil {
            defer f.Close()
            f.Write(append(b, '\n'))
        }
    }
    // Always succeed
    os.Exit(0)
}

// Test that when TaskConfig.Auth is provided, we run tart login with the
// provided credentials and then tart clone, passing through the environment.
func TestSetup_UsesTaskAuthAndEnv(t *testing.T) {
    t.Setenv("GO_WANT_HELPER_PROCESS", "1")
    t.Setenv("SENTINEL_VAR", "present")

    tmp := t.TempDir()
    logPath := filepath.Join(tmp, "cmd.log")
    t.Setenv("CMD_LOG", logPath)

    // Swap the command creator to route invocations to TestHelperProcess.
    orig := execCommandContext
    execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
        // Reinvoke the test binary and route to TestHelperProcess
        ha := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
        return exec.CommandContext(ctx, os.Args[0], ha...)
    }
    defer func() { execCommandContext = orig }()

    // Build a VMConfig with concrete TaskConfig.Auth
    vmc := VMConfig{
        TaskConfig: TaskConfig{
            URL: "ghcr.io/example/private:latest",
            Auth: Auth{Username: "user1", Password: "pass1"},
        },
        NomadConfig: &drivers.TaskConfig{AllocID: "alloc-123"},
    }

    c := NewTartClient(testLogger(t))
    if _, err := c.Setup(context.Background(), vmc); err != nil {
        t.Fatalf("Setup returned error: %v", err)
    }

    // Read back invocations
    data, err := os.ReadFile(logPath)
    if err != nil {
        t.Fatalf("reading log: %v", err)
    }
    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    var loginRec, cloneRec *cmdRecord
    for _, ln := range lines {
        var r cmdRecord
        if err := json.Unmarshal([]byte(ln), &r); err != nil {
            t.Fatalf("parse record: %v", err)
        }
        if r.Name == "tart" && len(r.Args) > 0 {
            if r.Args[0] == "login" && loginRec == nil {
                rr := r
                loginRec = &rr
            }
            if r.Args[0] == "clone" && cloneRec == nil {
                rr := r
                cloneRec = &rr
            }
        }
    }
    if loginRec == nil {
        t.Fatalf("expected a login invocation, none found. records: %v", len(lines))
    }
    // check username flag present
    foundUser := false
    for i := 0; i < len(loginRec.Args)-1; i++ {
        if loginRec.Args[i] == "--username" && loginRec.Args[i+1] == "user1" {
            foundUser = true
            break
        }
    }
    if !foundUser {
        t.Fatalf("--username user1 not found in login args: %v", loginRec.Args)
    }
    // ensure env is passed
    if !envContains(loginRec.Env, "SENTINEL_VAR", "present") {
        t.Fatalf("login env missing SENTINEL_VAR=present")
    }
    if cloneRec == nil {
        t.Fatalf("expected a clone invocation, none found")
    }
    if !envContains(cloneRec.Env, "SENTINEL_VAR", "present") {
        t.Fatalf("clone env missing SENTINEL_VAR=present")
    }
}

// Test that when TaskConfig.Auth is not valid, we do not run login and rely
// on env vars, and that env is passed to clone.
func TestSetup_NoTaskAuth_UsesEnvAndSkipsLogin(t *testing.T) {
    t.Setenv("GO_WANT_HELPER_PROCESS", "1")
    t.Setenv("SENTINEL_VAR", "present")
    t.Setenv("TART_REGISTRY_USER", "envuser")
    t.Setenv("TART_REGISTRY_PASSWORD", "envpass")

    tmp := t.TempDir()
    logPath := filepath.Join(tmp, "cmd.log")
    t.Setenv("CMD_LOG", logPath)

    orig := execCommandContext
    execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
        ha := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
        return exec.CommandContext(ctx, os.Args[0], ha...)
    }
    defer func() { execCommandContext = orig }()

    vmc := VMConfig{
        TaskConfig: TaskConfig{
            URL: "ghcr.io/example/private:latest",
            // No Auth provided
        },
        NomadConfig: &drivers.TaskConfig{AllocID: "alloc-abc"},
    }

    c := NewTartClient(testLogger(t))
    if _, err := c.Setup(context.Background(), vmc); err != nil {
        t.Fatalf("Setup returned error: %v", err)
    }

    data, err := os.ReadFile(logPath)
    if err != nil {
        t.Fatalf("reading log: %v", err)
    }
    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    var sawLogin bool
    var cloneRec *cmdRecord
    for _, ln := range lines {
        var r cmdRecord
        if err := json.Unmarshal([]byte(ln), &r); err != nil {
            t.Fatalf("parse record: %v", err)
        }
        if r.Name == "tart" && len(r.Args) > 0 {
            switch r.Args[0] {
            case "login":
                sawLogin = true
            case "clone":
                rr := r
                cloneRec = &rr
            }
        }
    }
    if sawLogin {
        t.Fatalf("did not expect a login invocation when Auth is not provided")
    }
    if cloneRec == nil {
        t.Fatalf("expected a clone invocation, none found")
    }
    if !envContains(cloneRec.Env, "TART_REGISTRY_USER", "envuser") || !envContains(cloneRec.Env, "TART_REGISTRY_PASSWORD", "envpass") {
        t.Fatalf("clone env missing TART_REGISTRY_* from environment")
    }
}

// envContains checks for key=value in env slice
func envContains(env []string, key, val string) bool {
    want := key + "=" + val
    for _, e := range env {
        if e == want {
            return true
        }
    }
    return false
}

// testLogger returns a no-op logger since TartClient requires one.
func testLogger(tb testing.TB) hclog.Logger {
    tb.Helper()
    return hclog.New(&hclog.LoggerOptions{Level: hclog.Off})
}
