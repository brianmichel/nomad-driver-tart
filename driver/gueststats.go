package driver

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/nomad/drivers/shared/executor/procstats"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// writeCloseBuffer is a bytes.Buffer that implements io.WriteCloser.
type writeCloseBuffer struct{ bytes.Buffer }

func (w *writeCloseBuffer) Close() error { return nil }

// guestStats collects CPU and memory usage from inside a VM by executing
// standard macOS utilities via the virtualization client.
func guestStats(ctx context.Context, client VirtualizationClient, cfg VMConfig) (*drivers.ResourceUsage, error) {
	var outBuf, errBuf writeCloseBuffer
	opts := ExecOptions{
		Command: []string{"ps", "-axo", "rss,pcpu"},
		Stdout:  &outBuf,
		Stderr:  &errBuf,
		Tty:     false,
	}

	if _, err := client.Exec(ctx, cfg, opts); err != nil {
		return nil, fmt.Errorf("exec stats command failed: %v (stderr: %s)", err, errBuf.String())
	}

	lines := strings.Split(strings.TrimSpace(outBuf.String()), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("no stats output")
	}

	var totalRSS uint64
	var totalCPU float64
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rss, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		totalRSS += rss * 1024 // ps reports RSS in KB
		totalCPU += cpu
	}

	usage := &drivers.ResourceUsage{
		MemoryStats: &drivers.MemoryStats{
			RSS:      totalRSS,
			Measured: procstats.ExecutorBasicMeasuredMemStats,
		},
		CpuStats: &drivers.CpuStats{
			Percent:  totalCPU,
			Measured: []string{"Percent"},
		},
	}
	return usage, nil
}
