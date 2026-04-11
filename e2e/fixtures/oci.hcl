# Copyright IBM Corp. 2019, 2025
# SPDX-License-Identifier: MPL-2.0

variable "oci_image" {
  type = string
}

job "ch-oci" {
  datacenters = ["dc1"]
  type        = "batch"

  group "vm" {
    task "vm" {
      driver = "cloud-hypervisor"

      resources {
        cores  = 1
        memory = 512
      }

      config {
        oci_image = var.oci_image
      }
    }
  }
}
