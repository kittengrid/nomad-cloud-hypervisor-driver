# Copyright IBM Corp. 2019, 2025
# SPDX-License-Identifier: MPL-2.0

job "example" {
  datacenters = ["dc1"]
  type        = "batch"

  group "example" {
    task "kittenvisor" {
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

        # Optional: inline cloud-init user-data.  When set the driver creates a
        # NoCloud seed ISO and attaches it as a read-only disk automatically.
        # The guest must have cloud-init installed; no extra disk block is needed.
        cloud_init = <<-EOT
        #cloud-config
        users:
          - name: cloud
            passwd: $6$7125787751a8d18a$sHwGySomUA1PawiNFWVCKYQN.Ec.Wzz0JtPPL1MvzFrkwmop2dq7.4CYf03A5oemPQ4pOFCCrtCelvFBEle/K.
            sudo: ALL=(ALL) NOPASSWD:ALL
            lock_passwd: False
            inactive: False
            shell: /bin/bash

        ssh_pwauth: True
        EOT
      }
    }
  }
}
