package driver

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers/fsisolation"
	"github.com/stretchr/testify/require"
)

func TestDriver_Capabilities(t *testing.T) {
	logger := hclog.NewNullLogger()
	driverPlugin := NewTartDriver(logger)

	caps, err := driverPlugin.Capabilities()
	require.NoError(t, err)
	require.NotNil(t, caps)

	// Expected values are based on the 'capabilities' variable in driver.go
	// SendSignals: false
	// Exec:        true
	// FSIsolation: fsisolation.Image
	require.False(t, caps.SendSignals, "Capabilities.SendSignals should be false")
	require.True(t, caps.Exec, "Capabilities.Exec should be true")
	require.Equal(t, fsisolation.Image, caps.FSIsolation, "Capabilities.FSIsolation should be fsisolation.Image")
}
