package driver

import (
	"context"
	"strconv"
	"time"

	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/shirou/gopsutil/v3/process"
)

// collectStats periodically emits resource usage statistics for the VM task.
// It aggregates stats from the tart process as well as the associated
// "Virtual Machine Service for Tart" process which owns the VM's disk image.
func (d *Driver) collectStats(ctx context.Context, h *taskHandle, interval time.Duration, ch chan *drivers.TaskResourceUsage) {
	defer close(ch)

	var cfg TaskConfig
	if err := h.taskConfig.DecodeDriverConfig(&cfg); err != nil {
		h.logger.Error("failed to decode driver config for stats", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	procCache := make(map[int]*process.Process)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		allocVMName := d.generateVMName(h.taskConfig.AllocID)

		pids := d.relatedPIDs(allocVMName, h.pid)
		usageByPid := make(map[string]*drivers.ResourceUsage)

		var totalRSS, totalSwap uint64
		var sysMode, userMode, percent float64

		for _, pid := range pids {
			proc, ok := procCache[pid]
			if !ok {
				p, err := process.NewProcess(int32(pid))
				if err != nil {
					h.logger.Debug("failed to create process for stats", "pid", pid, "err", err)
					continue
				}
				proc = p
				procCache[pid] = p
			}

			mem, err := proc.MemoryInfo()
			if err != nil {
				h.logger.Debug("failed to get memory info", "pid", pid, "err", err)
				continue
			}

			cpuTimes, err := proc.Times()
			if err != nil {
				h.logger.Debug("failed to get cpu times", "pid", pid, "err", err)
				continue
			}

			cpuPercent, err := proc.Percent(0)
			if err != nil {
				h.logger.Debug("failed to get cpu percent", "pid", pid, "err", err)
				continue
			}

			ru := &drivers.ResourceUsage{
				MemoryStats: &drivers.MemoryStats{
					RSS:      mem.RSS,
					Swap:     mem.Swap,
					Measured: executor.ExecutorBasicMeasuredMemStats,
				},
				CpuStats: &drivers.CpuStats{
					SystemMode: cpuTimes.System * float64(time.Second),
					UserMode:   cpuTimes.User * float64(time.Second),
					Percent:    cpuPercent,
					Measured:   executor.ExecutorBasicMeasuredCpuStats,
				},
			}

			usageByPid[strconv.Itoa(pid)] = ru
			totalRSS += mem.RSS
			totalSwap += mem.Swap
			sysMode += ru.CpuStats.SystemMode
			userMode += ru.CpuStats.UserMode
			percent += cpuPercent
		}

		agg := &drivers.TaskResourceUsage{
			ResourceUsage: &drivers.ResourceUsage{
				MemoryStats: &drivers.MemoryStats{
					RSS:      totalRSS,
					Swap:     totalSwap,
					Measured: executor.ExecutorBasicMeasuredMemStats,
				},
				CpuStats: &drivers.CpuStats{
					SystemMode: sysMode,
					UserMode:   userMode,
					Percent:    percent,
					Measured:   executor.ExecutorBasicMeasuredCpuStats,
				},
			},
			Timestamp: time.Now().UTC().UnixNano(),
			Pids:      usageByPid,
		}

		select {
		case <-ctx.Done():
			return
		case ch <- agg:
		}
	}
}
