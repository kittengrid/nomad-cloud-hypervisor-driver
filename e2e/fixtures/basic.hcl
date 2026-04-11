# Copyright IBM Corp. 2019, 2025
# SPDX-License-Identifier: MPL-2.0

variable "kernel" {
  type = string
}

variable "initramfs" {
  type = string
}

variable "rootfs" {
  type = string
}

variable "cmdline" {
  type = string
}

variable "console_mode" {
  type    = string
  default = "off"
}

variable "oci_image" {
  type    = string
  default = ""
}

job "ch-basic" {
  datacenters = ["dc1"]
  type        = "service"

  group "vm" {
    task "vm" {
      driver = "cloud-hypervisor"

      resources {
        cores  = 1
        memory = 512
      }

      config {
        oci_image = var.oci_image

        payload {
          kernel    = var.kernel
          initramfs = var.initramfs
          cmdline   = var.cmdline
        }

        disk {
          path       = var.rootfs
          image_type = "raw"
        }

        console {
          mode = var.console_mode
        }
      }
    }
  }
}
