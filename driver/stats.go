package driver

import (
	"context"
	"time"

	"github.com/hashicorp/nomad/plugins/drivers"
)

func (d *Driver) handleStats(ctx context.Context, ch chan *drivers.TaskResourceUsage, interval time.Duration) {
	defer close(ch)
	timer := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-timer.C:
			// Send placeholder stats
			ch <- &drivers.TaskResourceUsage{
				ResourceUsage: &drivers.ResourceUsage{
					MemoryStats: &drivers.MemoryStats{
						RSS:      0,
						Measured: []string{"RSS"},
					},
					CpuStats: &drivers.CpuStats{
						SystemMode: 0,
						UserMode:   0,
						Measured:   []string{"System Mode", "User Mode"},
					},
				},
				Timestamp: time.Now().UTC().UnixNano(),
			}
			timer.Reset(interval)
		}
	}
}
