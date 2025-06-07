package driver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/shirou/gopsutil/v3/process"
)

// taskHandle is a handle to a running task
type taskHandle struct {
	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	// taskConfig is the task configuration from the job
	taskConfig *drivers.TaskConfig

	// state is the state of the task
	state drivers.TaskState

	// taskPid is the PID of the task
	taskPid int

	// startedAt is when the task was started
	startedAt time.Time

	// completedAt is when the task exited
	completedAt time.Time

	// exitResult is the result of the task
	exitResult *drivers.ExitResult

	// logger is the logger for the task
	logger hclog.Logger
}

// TaskStatus returns the current status of the task
func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	status := &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.state,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			// No custom attributes for now, but something like the task PID could be useful.
			"pid": fmt.Sprintf("%d", h.taskPid),
		},
	}

	return status
}

func (h *taskHandle) handleStats(ctx context.Context, ch chan *drivers.TaskResourceUsage, interval time.Duration) {
	defer close(ch)
	timer := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			h.logger.Debug("collecting stats for tart task")
			// Send placeholder stats
			h.stateLock.RLock()
			pid := h.taskPid
			h.stateLock.RUnlock()

			if pid <= 0 {
				h.logger.Error("invalid pid", "pid", pid)
				continue
			}

			proc, err := process.NewProcess(int32(pid))
			if err != nil {
				h.logger.Error("failed to open process for pid", "pid", pid, "error", err)
				continue
			}

			memInfo, err := proc.MemoryInfo()
			if err != nil {
				h.logger.Error("failed to get memory info for pid", "pid", pid, "error", err)
			}

			cpuTimes, err := proc.Times()
			if err != nil {
				h.logger.Error("failed to get cpu times for pid", "pid", pid, "error", err)
				continue
			}

			cpuPercent, err := proc.CPUPercent()
			if err != nil {
				h.logger.Error("failed to get cpu percent for pid", "pid", pid, "error", err)
				continue
			}

			h.logger.Debug("collected stats for tart task", "cpu_times", cpuTimes, "cpu_percent", cpuPercent)

			stats := &drivers.TaskResourceUsage{
				ResourceUsage: &drivers.ResourceUsage{
					MemoryStats: &drivers.MemoryStats{
						RSS:      uint64(memInfo.RSS),
						Measured: []string{"RSS"},
					},
					CpuStats: &drivers.CpuStats{
						SystemMode: cpuTimes.System * float64(time.Second),
						UserMode:   cpuTimes.User * float64(time.Second),
						Measured:   []string{"System Mode", "User Mode"},
						// CPUPercent already returns a percentage value in the range
						// [0,100] across all CPUs. Avoid scaling it again.
						Percent: cpuPercent,
					},
				},
				Timestamp: time.Now().UTC().UnixNano(),
			}
			ch <- stats
			timer.Reset(interval)
		}
	}
}

// IsRunning returns whether the task is running
func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.state == drivers.TaskStateRunning
}
