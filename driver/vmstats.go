package driver

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/nomad/client/lib/cpustats"
	"github.com/hashicorp/nomad/drivers/shared/executor/procstats"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/shirou/gopsutil/v3/process"
)

// vmStatsTracker tracks resource usage for virtualization processes
// backing a Tart VM. It exposes methods for gathering per-process
// stats which can then be aggregated alongside the executor stats.
type vmStatsTracker struct {
	compute      cpustats.Compute
	systemCPU    *cpustats.Tracker
	procTrackers map[int]*procCPUTrackers
}

type procCPUTrackers struct {
	total  *cpustats.Tracker
	user   *cpustats.Tracker
	system *cpustats.Tracker
}

func newVMStatsTracker(compute cpustats.Compute) *vmStatsTracker {
	return &vmStatsTracker{
		compute:      compute,
		systemCPU:    cpustats.New(compute),
		procTrackers: make(map[int]*procCPUTrackers),
	}
}

// virtualizationPIDs returns the pids of running virtualization processes
// associated with the VM located at vmPath. Processes are matched by checking
// open file descriptors for paths within the VM directory.
func virtualizationPIDs(vmPath string) []int {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	var pids []int
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		if !strings.Contains(name, "Virtual Machine Service") {
			continue
		}
		files, err := p.OpenFiles()
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasPrefix(f.Path, vmPath) {
				pids = append(pids, int(p.Pid))
				break
			}
		}
	}
	return pids
}

func (v *vmStatsTracker) prune(pids []int) {
	pidSet := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		pidSet[pid] = struct{}{}
	}
	for pid := range v.procTrackers {
		if _, ok := pidSet[pid]; !ok {
			delete(v.procTrackers, pid)
		}
	}
}

func (v *vmStatsTracker) statsForPID(pid int) (*drivers.ResourceUsage, error) {
	pr, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, err
	}
	memInfo, err := pr.MemoryInfo()
	if err != nil {
		return nil, err
	}
	timesInfo, err := pr.Times()
	if err != nil {
		return nil, err
	}

	t, ok := v.procTrackers[pid]
	if !ok {
		t = &procCPUTrackers{
			total:  cpustats.New(v.compute),
			user:   cpustats.New(v.compute),
			system: cpustats.New(v.compute),
		}
		v.procTrackers[pid] = t
	}

	const sec = float64(time.Second)
	totalPercent := t.total.Percent(timesInfo.Total() * sec)
	userPercent := t.user.Percent(timesInfo.User * sec)
	systemPercent := t.system.Percent(timesInfo.System * sec)

	usage := &drivers.ResourceUsage{
		MemoryStats: &drivers.MemoryStats{
			RSS:      memInfo.RSS,
			Swap:     memInfo.Swap,
			Measured: procstats.ExecutorBasicMeasuredMemStats,
		},
		CpuStats: &drivers.CpuStats{
			SystemMode: systemPercent,
			UserMode:   userPercent,
			Percent:    totalPercent,
			Measured:   procstats.ExecutorBasicMeasuredCpuStats,
		},
	}
	return usage, nil
}

// collect gathers stats for all virtualization processes associated with the
// VM path.
func (v *vmStatsTracker) collect(vmPath string) (procstats.ProcUsages, *drivers.TaskResourceUsage) {
	pids := virtualizationPIDs(vmPath)
	v.prune(pids)
	stats := make(procstats.ProcUsages)
	for _, pid := range pids {
		ru, err := v.statsForPID(pid)
		if err != nil {
			continue
		}
		stats[strconv.Itoa(pid)] = ru
	}
	if len(stats) == 0 {
		return nil, nil
	}
	agg := procstats.Aggregate(v.systemCPU, stats)
	return stats, agg
}

// vmPathFor returns the expected path of the VM on disk for a given VM name.
func vmPathFor(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".tart", "vms", name)
}
