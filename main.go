// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins"
	"github.com/kittengrid/nomad-cloud-hypervisor-driver/chdriver"
)

func main() {
	// Serve the plugin
	plugins.Serve(factory)
}

// factory returns a new instance of a nomad driver plugin
func factory(log hclog.Logger) interface{} {
	return chdriver.NewPlugin(log)
}
