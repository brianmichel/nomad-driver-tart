package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/structs"
)

// handleFingerprint manages the channel and the flow of fingerprint data.
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
			// After the initial fingerprint we can set the proper fingerprint
			// period
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
	fp.Attributes["driver.tart.available_slots"] = structs.NewIntAttribute(0, "")

	// Check if the driver is enabled
	if !d.config.Enabled {
		fp.Health = drivers.HealthStateUndetected
		fp.HealthDescription = "disabled"
		return fp
	}

	// Check if virtualization software is installed and accessible
	ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
	defer cancel()

	if err := d.virtualizer.IsInstalled(ctx); err != nil {
		d.logger.Warn("failed to find virtualization software", "error", err)
		fp.Health = drivers.HealthStateUndetected
		fp.HealthDescription = "virtualization software not found"
		return fp
	}

	// Get the Tart version and add it as an attribute
	version, err := d.virtualizer.GetVersion(ctx)
	if err == nil && version != "" {
		fp.Attributes["driver.tart.version"] = structs.NewStringAttribute(version)
	}

	// Try to list VMs to verify virtualization software is working properly
	_, err = d.virtualizer.ListVMs(ctx)
	if err != nil {
		d.logger.Warn("failed to list VMs", "error", err)
		fp.Health = drivers.HealthStateUnhealthy
		fp.HealthDescription = fmt.Sprintf("failed to list VMs: %v", err)
		return fp
	}

	return fp
}
