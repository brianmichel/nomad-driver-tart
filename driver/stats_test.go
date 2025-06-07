package driver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
)

func buildTestBinary(t *testing.T, output string) {
	src := filepath.Join(t.TempDir(), "main.go")
	program := `package main
import (
    "os"
    "time"
)
func main() {
    f, err := os.Open(os.Args[1])
    if err != nil { panic(err) }
    defer f.Close()
    for {
        time.Sleep(time.Second)
    }
}`
	if err := os.WriteFile(src, []byte(program), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", output, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build error: %v, %s", err, out)
	}
}

func TestRelatedPIDs(t *testing.T) {
	t.Parallel()
	d := &Driver{}

	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	t.Cleanup(func() { os.Setenv("HOME", oldHome) })

	vmName := "vm1"
	diskDir := filepath.Join(home, ".tart", vmName)
	if err := os.MkdirAll(diskDir, 0755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(diskDir, "disk.img")
	if err := os.WriteFile(disk, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "com.apple.Virtualization.VirtualMachine")
	buildTestBinary(t, binPath)

	cmd := exec.Command(binPath, disk)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	time.Sleep(300 * time.Millisecond)

	pids := d.relatedPIDs(vmName, 0)
	found := false
	for _, p := range pids {
		if p == cmd.Process.Pid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pid %d not found in %v", cmd.Process.Pid, pids)
	}
}

func TestCollectStatsIncludesRelatedProcess(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", home)
	t.Cleanup(func() { os.Setenv("HOME", oldHome) })

	vmName := "vm1"
	diskDir := filepath.Join(home, ".tart", vmName)
	if err := os.MkdirAll(diskDir, 0755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(diskDir, "disk.img")
	if err := os.WriteFile(disk, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "com.apple.Virtualization.VirtualMachine")
	buildTestBinary(t, binPath)

	cmd := exec.Command(binPath, disk)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	time.Sleep(300 * time.Millisecond)

	tc := &drivers.TaskConfig{ID: "task", Name: "task"}
	if err := tc.EncodeConcreteDriverConfig(&TaskConfig{Name: vmName}); err != nil {
		t.Fatal(err)
	}

	h := &taskHandle{
		taskConfig: tc,
		pid:        0,
		logger:     hclog.NewNullLogger(),
	}

	d := &Driver{logger: hclog.NewNullLogger()}

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan *drivers.TaskResourceUsage)
	go d.collectStats(ctx, h, 100*time.Millisecond, ch)
	var usage *drivers.TaskResourceUsage
	select {
	case usage = <-ch:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for stats")
	}

	found := false
	for pidStr := range usage.Pids {
		if pidStr == strconv.Itoa(cmd.Process.Pid) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pid %d in stats %+v", cmd.Process.Pid, usage)
	}
}
