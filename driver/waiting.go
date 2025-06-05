package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/brianmichel/nomad-driver-tart/virtualizer"
	"github.com/hashicorp/nomad/plugins/drivers"
)

func (d *Driver) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)

	// Extract the VM name from the task config by decoding the driver config
	var taskConfig TaskConfig
	if err := handle.taskConfig.DecodeDriverConfig(&taskConfig); err != nil {
		d.logger.Error("failed to decode driver config", "error", err)
		ch <- &drivers.ExitResult{
			ExitCode: 1,
			Signal:   0,
			Err:      fmt.Errorf("failed to decode driver config: %v", err),
		}
		return
	}

	vmName := taskConfig.Name
	d.logger.Debug("monitoring VM status", "vm_name", vmName)

	// Poll interval for checking VM status
	pollInterval := 5 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Debug("context cancelled, stopping VM monitoring", "vm_name", vmName)
			return
		case <-d.ctx.Done():
			d.logger.Debug("driver context cancelled, stopping VM monitoring", "vm_name", vmName)
			return
		case <-ticker.C:
			// Check the VM status
			status, err := d.virtualizer.GetVMStatus(ctx, vmName)
			if err != nil {
				d.logger.Warn("failed to get VM status, assuming VM is stopped", "vm_name", vmName, "error", err)
				// If we can't get the status, assume the VM has exited with an error
				handle.stateLock.Lock()
				handle.state = drivers.TaskStateExited
				handle.completedAt = time.Now()
				handle.exitResult = &drivers.ExitResult{
					ExitCode: 1,
					Signal:   0,
					Err:      fmt.Errorf("failed to get VM status: %v", err),
				}
				exitResult := handle.exitResult
				handle.stateLock.Unlock()

				ch <- exitResult
				return
			}

			// If the VM is not running, it has exited
			if status != virtualizer.VMStateRunning {
				// Before assuming the VM is stopped, try to get the status one more time
				// to avoid false negatives from transient issues
				time.Sleep(1 * time.Second)
				status, err = d.virtualizer.GetVMStatus(ctx, vmName)
				if err == nil && status == virtualizer.VMStateRunning {
					d.logger.Debug("VM is actually running after second check", "vm_name", vmName)
					continue
				}

				// For Tart VMs, if we're running a command that completes quickly,
				// the VM might still be considered "running" from Nomad's perspective
				// even if the command inside has completed.
				// We'll consider this a successful completion of the task.
				d.logger.Info("VM is no longer running", "vm_name", vmName, "status", status)

				handle.stateLock.Lock()
				handle.state = drivers.TaskStateExited
				handle.completedAt = time.Now()

				// Assume successful exit if the VM stopped normally
				exitCode := 0
				if status != virtualizer.VMStateStopped {
					// If it's in any other state (like paused or error), consider it an abnormal exit
					exitCode = 1
				}

				handle.exitResult = &drivers.ExitResult{
					ExitCode: exitCode,
					Signal:   0,
				}
				exitResult := handle.exitResult
				handle.stateLock.Unlock()

				ch <- exitResult
				return
			}

			// VM is still running, continue monitoring
			d.logger.Debug("VM still running", "vm_name", vmName, "status", status)
		}
	}
}
