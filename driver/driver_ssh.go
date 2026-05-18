package driver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
)

// waitForIPAddress blocks until the VM reports an IP address. Retries with
// exponential backoff from 1s to 10s and returns the discovered address.
func (d *Driver) waitForIPAddress(ctx context.Context, vmConfig VMConfig) (string, error) {
	backoff := 1 * time.Second
	maxBackoff := 10 * time.Second
	name := vmName(vmConfig.Nomad.AllocID)

	for {
		ip, err := d.client.IPAddress(ctx, name, vmConfig.Driver.Network)
		if err == nil && ip != "" {
			return ip, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
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

// waitForSSH blocks until the VM reports an IP address, indicating SSH
// should be reachable. Retries with exponential backoff from 1s to 10s.
// Returns nil when ready, or ctx.Err() on cancellation.
func (d *Driver) waitForSSH(ctx context.Context, vmConfig VMConfig) error {
	_, err := d.waitForIPAddress(ctx, vmConfig)
	return err
}

// executeStartupCommand waits for SSH to become available, then runs the
// configured command+args inside the VM. Output is written to the provided
// stdout/stderr writers.  This is a one-shot execution — it does not retry
// on command failure. The VM is left running regardless of exit status.
func (d *Driver) executeStartupCommand(
	ctx context.Context,
	taskID, taskName, allocID string,
	vmConfig VMConfig,
	stdout, stderr io.WriteCloser,
) {
	// Build the full command slice.
	taskConfig := vmConfig.Driver
	var fullCmd []string
	if taskConfig.Command != "" {
		fullCmd = append([]string{taskConfig.Command}, taskConfig.Args...)
	} else if len(taskConfig.Args) > 0 {
		// Args without a command is a misconfiguration.
		d.eventer.EmitEvent(&drivers.TaskEvent{
			TaskID:    taskID,
			TaskName:  taskName,
			AllocID:   allocID,
			Timestamp: time.Now(),
			Message:   "Startup command misconfigured: args set but command is empty",
		})
		return
	} else {
		// Neither field set; nothing to do.
		return
	}

	commandStr := fullCmd[0]
	if len(fullCmd) > 1 {
		commandStr = fmt.Sprintf("%s %s", fullCmd[0], strings.Join(fullCmd[1:], " "))
	}

	d.eventer.EmitEvent(&drivers.TaskEvent{
		TaskID:    taskID,
		TaskName:  taskName,
		AllocID:   allocID,
		Timestamp: time.Now(),
		Message:   "Running startup command",
		Annotations: map[string]string{
			"command": commandStr,
		},
	})

	backoff := 1 * time.Second
	maxBackoff := 10 * time.Second

	for {
		// allow cancellation between attempts
		select {
		case <-ctx.Done():
			return
		default:
		}

		exitCode, err := d.client.Exec(ctx, vmConfig, ExecOptions{
			Command: fullCmd,
			Stdout:  stdout,
			Stderr:  stderr,
			Tty:     false,
		})

		if err == nil {
			d.eventer.EmitEvent(&drivers.TaskEvent{
				TaskID:    taskID,
				TaskName:  taskName,
				AllocID:   allocID,
				Timestamp: time.Now(),
				Message:   "Startup command completed",
				Annotations: map[string]string{
					"command":   commandStr,
					"exit_code": strconv.Itoa(exitCode),
				},
			})
			return
		}

		// Check if it was a cancellation.
		select {
		case <-ctx.Done():
			return
		default:
		}

		if errors.Is(err, errVMIPUnavailable) || errors.Is(err, errSSHDialFailed) || errors.Is(err, errSSHSessionFailed) {
			d.logger.Debug("Startup command SSH not ready; retrying", "command", commandStr, "error", err)
			time.Sleep(backoff)
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		d.eventer.EmitEvent(&drivers.TaskEvent{
			TaskID:    taskID,
			TaskName:  taskName,
			AllocID:   allocID,
			Timestamp: time.Now(),
			Message:   "Startup command failed",
			Annotations: map[string]string{
				"command": commandStr,
				"error":   err.Error(),
			},
		})
		return
	}
}
