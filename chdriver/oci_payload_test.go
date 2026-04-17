// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/shoenig/test/must"
)

func TestReadOCIMetadataOverrides(t *testing.T) {
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "metadata.json")

	metadata := `{
  "name": "test-image",
  "version": "1.0.0",
  "arch": "amd64",
  "config": {
    "payload": {
      "kernel": "vmlinuz",
      "initramfs": "initramfs.img",
      "cmdline": "console=ttyS0"
    },
    "disk": [
      {
        "path": "rootfs.raw",
        "image_type": "raw",
        "readonly": true,
        "ephemeral_overlay": false
      }
    ],
    "console": {"mode": "Tty"},
    "network": [
      {"mac": "52:54:00:aa:bb:cc", "auto-tuntap": true, "auto-tuntap-bridge": "br0"}
    ],
    "cloud-init": {"user-data": "#cloud-config\n"},
    "serial": "socket=/tmp/serial"
  }
}`

	must.NoError(t, os.WriteFile(metadataPath, []byte(metadata), 0o644))

	overrides, err := readOCIMetadataOverrides(metadataPath, hclog.NewNullLogger())
	must.NoError(t, err)
	must.NotNil(t, overrides)
	must.NotNil(t, overrides.Payload)
	must.NotNil(t, overrides.Console)
	must.SliceLen(t, 1, overrides.Disk)
	must.SliceLen(t, 1, overrides.Network)
	must.NotNil(t, overrides.CloudInit)
}

func TestApplyTaskConfigOverrides(t *testing.T) {
	dir := t.TempDir()

	cfg := &TaskConfig{}
	overrides := &TaskConfigOverrides{
		Payload: &TaskPayloadConfig{
			Kernel:    "vmlinuz",
			Initramfs: "initramfs.img",
			Cmdline:   "console=ttyS0",
		},
		Disk: []TaskDiskConfig{
			{
				Path:      "rootfs.raw",
				ImageType: "raw",
				Readonly:  true,
			},
		},
		Console: &TaskConsoleConfig{Mode: "Tty"},
		Network: []TaskNetworkConfig{
			{
				Mac:              "52:54:00:aa:bb:cc",
				AutoTuntap:       true,
				AutoTuntapBridge: "br0",
			},
		},
		CloudInit: &CloudInit{UserData: "#cloud-config\n"},
		Serial:    "socket=/tmp/serial",
	}

	logger := hclog.NewNullLogger()
	applyTaskConfigOverrides(cfg, overrides, dir, logger)

	must.Eq(t, filepath.Join(dir, "vmlinuz"), cfg.Payload.Kernel)
	must.Eq(t, filepath.Join(dir, "initramfs.img"), cfg.Payload.Initramfs)
	must.Eq(t, "console=ttyS0", cfg.Payload.Cmdline)
	must.SliceLen(t, 1, cfg.Disk)
	must.Eq(t, filepath.Join(dir, "rootfs.raw"), cfg.Disk[0].Path)
	must.Eq(t, "raw", cfg.Disk[0].ImageType)
	must.True(t, cfg.Disk[0].Readonly)
	must.Eq(t, "Tty", cfg.Console.Mode)
	must.SliceLen(t, 1, cfg.Network)
	must.Eq(t, "52:54:00:aa:bb:cc", cfg.Network[0].Mac)
	must.True(t, cfg.Network[0].AutoTuntap)
	must.Eq(t, "br0", cfg.Network[0].AutoTuntapBridge)
	must.NotNil(t, cfg.CloudInit)
	must.Eq(t, "#cloud-config\n", cfg.CloudInit.UserData)
	must.Eq(t, "socket=/tmp/serial", cfg.Serial)
}

func TestApplyJobConfig(t *testing.T) {
	// Base comes from OCI image; job config wins on every non-zero field.
	base := &TaskConfig{
		Payload:  TaskPayloadConfig{Kernel: "/oci/vmlinuz", Initramfs: "/oci/initrd.img", Cmdline: "console=ttyS0"},
		Disk:     []TaskDiskConfig{{Path: "/oci/rootfs.qcow2", ImageType: "qcow2"}},
		Console:  TaskConsoleConfig{Mode: "Tty"},
		Network:  []TaskNetworkConfig{{AutoTuntap: true}},
		CloudInit: &CloudInit{UserData: "#oci-cloud-config\n"},
		Serial:   "tty",
	}
	job := &TaskConfig{
		Payload: TaskPayloadConfig{Kernel: "/job/custom-kernel", Cmdline: "quiet"},
		Serial:  "socket=/tmp/serial",
	}

	applyJobConfig(base, job, hclog.NewNullLogger())

	// Job fields win.
	must.Eq(t, "/job/custom-kernel", base.Payload.Kernel)
	must.Eq(t, "quiet", base.Payload.Cmdline)
	must.Eq(t, "socket=/tmp/serial", base.Serial)

	// OCI fields survive where job left them empty.
	must.Eq(t, "/oci/initrd.img", base.Payload.Initramfs)
	must.SliceLen(t, 1, base.Disk)
	must.Eq(t, "/oci/rootfs.qcow2", base.Disk[0].Path)
	must.Eq(t, "Tty", base.Console.Mode)
	must.SliceLen(t, 1, base.Network)
	must.NotNil(t, base.CloudInit)
}

func TestApplyOCIPayloadDefaults(t *testing.T) {
	dir := t.TempDir()
	must.NoError(t, os.WriteFile(filepath.Join(dir, "vmlinuz"), []byte("kernel"), 0o644))
	must.NoError(t, os.WriteFile(filepath.Join(dir, "initrd.img"), []byte("initramfs"), 0o644))
	must.NoError(t, os.WriteFile(filepath.Join(dir, "rootfs.qcow2"), []byte("rootfs"), 0o644))

	cfg := &TaskConfig{}
	applyOCIPayloadDefaults(cfg, dir, hclog.NewNullLogger())

	must.Eq(t, filepath.Join(dir, "vmlinuz"), cfg.Payload.Kernel)
	must.Eq(t, filepath.Join(dir, "initrd.img"), cfg.Payload.Initramfs)
	must.SliceLen(t, 1, cfg.Disk)
	must.Eq(t, filepath.Join(dir, "rootfs.qcow2"), cfg.Disk[0].Path)
	must.Eq(t, "qcow2", cfg.Disk[0].ImageType)
}
