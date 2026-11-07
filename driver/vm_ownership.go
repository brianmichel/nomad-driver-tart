package driver

import (
	"fmt"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// vmOwnership is persisted independently of mutable task configuration.
type vmOwnership struct {
	VMName   string
	TaskID   string
	TaskName string
	AllocID  string
	Legacy   bool
}

func newVMOwnership(cfg *drivers.TaskConfig, pullOnly bool) (vmOwnership, error) {
	if cfg == nil || cfg.ID == "" || cfg.Name == "" || cfg.AllocID == "" {
		return vmOwnership{}, fmt.Errorf("task ID, task name and allocation ID are required for VM ownership")
	}
	owner := vmOwnership{VMName: taskVMName(cfg), TaskID: cfg.ID, TaskName: cfg.Name, AllocID: cfg.AllocID}
	if pullOnly {
		owner.VMName = ""
	}
	return owner, nil
}
func (o vmOwnership) validate(cfg *drivers.TaskConfig, taskID string, pullOnly bool) error {
	expected, err := newVMOwnership(cfg, pullOnly)
	if err != nil {
		return err
	}
	if o.TaskID != taskID || o.TaskID != expected.TaskID || o.TaskName != expected.TaskName || o.AllocID != expected.AllocID {
		return fmt.Errorf("VM task/allocation ownership mismatch")
	}
	if o.Legacy {
		if pullOnly {
			return fmt.Errorf("pull-only task cannot own a legacy VM")
		}
		expected.VMName = vmName(cfg.AllocID)
	}
	if o.VMName != expected.VMName {
		return fmt.Errorf("persisted VM identity does not match task ownership")
	}
	return nil
}
func decodeVMOwnership(handle *drivers.TaskHandle, state driverState) (vmOwnership, error) {
	if handle == nil || handle.Config == nil || state.TaskConfig == nil {
		return vmOwnership{}, fmt.Errorf("recovery task configuration is required")
	}
	outer, inner := handle.Config, state.TaskConfig
	if outer.ID != inner.ID || outer.AllocID != inner.AllocID || outer.Name != inner.Name {
		return vmOwnership{}, fmt.Errorf("recovery handle task/allocation identity mismatch")
	}
	owner := state.VMOwnership
	switch handle.Version {
	case 1:
		if owner != (vmOwnership{}) {
			return vmOwnership{}, fmt.Errorf("version 1 handle contains unexpected VM ownership metadata")
		}
		var err error
		owner, err = newVMOwnership(inner, state.PullOnly)
		if err != nil {
			return vmOwnership{}, err
		}
		if !state.PullOnly {
			owner.VMName = vmName(inner.AllocID)
			owner.Legacy = true
		}
	case 2:
		// Version 2 always carries the exact persisted identity, including legacy migration.
	default:
		return vmOwnership{}, fmt.Errorf("incompatible handle version %d", handle.Version)
	}
	if err := owner.validate(inner, outer.ID, state.PullOnly); err != nil {
		return vmOwnership{}, err
	}
	return owner, nil
}
