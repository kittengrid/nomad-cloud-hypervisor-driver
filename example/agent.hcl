# Copyright IBM Corp. 2019, 2025
# SPDX-License-Identifier: MPL-2.0

log_level = "TRACE"

plugin "hello-driver" {
  config {
    cloud-hypervisor-binary-path = "/usr/bin/cloud-hypervisor"
  }
}
