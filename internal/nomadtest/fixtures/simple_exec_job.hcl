// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

job "simple-exec" {
  type = "batch"

  group "grp" {
    task "hello" {
      driver = "raw_exec"

      config {
        command = "/bin/sh"
        args    = ["-c", "echo hello world"]
      }

      resources {
        cpu    = 100
        memory = 64
      }
    }
  }
}
