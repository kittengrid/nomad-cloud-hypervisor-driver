# Copyright IBM Corp. 2019, 2025
# SPDX-License-Identifier: MPL-2.0

# intended to be used in conjunction with -dev mode

server {
  enabled          = true
  bootstrap_expect = 1
}

client {
  enabled = true
}

plugin "cloud-hypervisor-driver" {
  config {
    cloud-hypervisor-binary-path = "/usr/bin/cloud-hypervisor"
    cloud-hypervisor-socket-dir  = "/tmp/nomad-ch"
    cache-dir                    = "/tmp/nomad-ch-cache"
  }
}

plugin "raw_exec" {
  config {
    enabled = true
  }
}
