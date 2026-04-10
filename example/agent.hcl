data_dir = "/tmp/nomad-data"
log_level = "TRACE"

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
    cache-dir = "/tmp/nomad-ch-cache"
  }
}
telemetry {
  publish_allocation_metrics = true
  publish_node_metrics       = true
}
