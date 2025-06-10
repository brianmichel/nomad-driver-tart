package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/brianmichel/nomad-driver-tart/virtualizer"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/structs"
)

var (
	// Apple's Virtualization.framework mandates a maximum of 2 VMs per host.
	// This is enforced by the framework, trying to start 3 VMs will induce an error.
	// So we keep track of the running VMs and publish whether or not there are 'slots'
	// available on this machine to potentially schedule another VM.
	maxVMSlots        = 2
	availableSlotsKey = "driver.tart.available_slots"
	versionKey        = "driver.tart.version"
)

// handleFingerprint runs an infinite loop that sends the driver's fingerprint
// information to the given channel at a regular interval. It will stop when the
// context is canceled.
func (d *Driver) handleFingerprint(ctx context.Context, ch chan<- *drivers.Fingerprint) {
	defer close(ch)

	// Nomad expects the initial fingerprint to be sent immediately
	ticker := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			ticker.Reset(fingerprintPeriod)
			ch <- d.buildFingerprint()
		}
	}
}

// buildFingerprint returns the driver's fingerprint data
func (d *Driver) buildFingerprint() *drivers.Fingerprint {
	fp := &drivers.Fingerprint{
		Attributes:        map[string]*structs.Attribute{},
		Health:            drivers.HealthStateHealthy,
		HealthDescription: "healthy",
	}

	// Set driver attributes
	fp.Attributes["driver.tart"] = structs.NewBoolAttribute(true)

	// Check if the driver is enabled
	if !d.config.Enabled {
		fp.Health = drivers.HealthStateUndetected
		fp.HealthDescription = "disabled"
		// If driver is disabled, report that no slots are available.
		fp.Attributes[availableSlotsKey] = structs.NewBoolAttribute(false)
		return fp
	}

	// Check if virtualization software is installed and accessible
	// Use a new context for these checks to ensure timeout applies to all virtualizer calls
	fingerprintCtx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
	defer cancel()

	if err := d.virtualizer.IsInstalled(fingerprintCtx); err != nil {
		d.logger.Warn("failed to find virtualization software", "error", err)
		fp.Health = drivers.HealthStateUndetected
		fp.HealthDescription = "virtualization software not found"
		fp.Attributes[availableSlotsKey] = structs.NewIntAttribute(0, "virtualization software not found")
		return fp
	}

	// Get the Tart version and add it as an attribute
	version, err := d.virtualizer.GetVersion(fingerprintCtx)
	if err == nil && version != "" {
		fp.Attributes[versionKey] = structs.NewStringAttribute(version)
	}

	// Try to list VMs to verify virtualization software is working properly and calculate available slots
	vms, err := d.virtualizer.ListVMs(fingerprintCtx)
	if err != nil {
		d.logger.Warn("failed to list VMs", "error", err)
		fp.Health = drivers.HealthStateUnhealthy
		fp.HealthDescription = fmt.Sprintf("failed to list VMs: %v", err)
		fp.Attributes[availableSlotsKey] = structs.NewBoolAttribute(false)
		return fp
	}

	// Calculate available slots by counting only running VMs
	var runningVMsCount int
	for _, vm := range vms {
		if vm.Status == virtualizer.VMStateRunning {
			runningVMsCount++
		}
	}
	availableSlots := maxVMSlots - runningVMsCount
	if availableSlots < 0 {
		// This case implies more VMs are running than maxVMSlots, which might indicate an issue
		// or that VMs were started outside of Nomad's management for this driver.
		// For now, report 0 available slots.
		d.logger.Warn("calculated negative available slots", "running_vms", runningVMsCount, "max_slots", maxVMSlots)
		availableSlots = 0
	}
	fp.Attributes[availableSlotsKey] = structs.NewBoolAttribute(int64(availableSlots) > 0)

	return fp
}
