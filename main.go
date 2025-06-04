package main

import (
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins"

	"github.com/brianmichel/nomad-driver-tart/driver"
)

func main() {
	// Serve the plugin
	plugins.Serve(factory)
}

// factory returns a new Nomad driver plugin.
func factory(logger hclog.Logger) interface{} {
	return driver.NewTartDriver(logger)
}
