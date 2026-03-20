# Copyright IBM Corp. 2019, 2025
# SPDX-License-Identifier: MPL-2.0

job "example" {
  datacenters = ["dc1"]
  type        = "batch"

  group "example" {
    task "hello-world" {
      driver = "cloud-hypervisor"

      resources {
        memory = 1073
      }

      config {
        payload {
          kernel = "/home/jose/projects/kittengrid/ch-tests/hypervisor-fw"
        }

        disk {
          path       = "/home/jose/projects/kittengrid/ch-tests/ubuntu.raw"
          image_type = "raw"
        }

        cpus {
          boot_vcpus = 2
          max_vcpus  = 2
        }

        memory {
          size = 1073741824
        }

        console {
          mode = "off"
        }
      }
    }
  }
}
