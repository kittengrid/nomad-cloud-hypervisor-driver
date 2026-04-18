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

func TestReadOCIMetadata(t *testing.T) {
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
  }}`

	must.NoError(t, os.WriteFile(metadataPath, []byte(metadata), 0o644))

	overrides, err := readOCIMetadata(metadataPath, hclog.NewNullLogger())
	must.NoError(t, err)
	must.NotNil(t, overrides)
	must.NotNil(t, overrides.Payload)
	must.NotNil(t, overrides.Console)
	must.SliceLen(t, 1, overrides.Disk)
	must.SliceLen(t, 1, overrides.Network)
	must.NotNil(t, overrides.CloudInit)
}

func TestBuildConfigFromOCIMetadata(t *testing.T) {
	dir := t.TempDir()
	metadata := &OCIMetadataConfig{
		Payload: &TaskPayloadConfig{
			Kernel:  "vmlinuz",
			Cmdline: "console=ttyS0",
		},
		Disk: []TaskDiskConfig{
			{Path: "rootfs.raw", ImageType: "raw", Readonly: true},
			{Path: "data.raw", ImageType: "raw"},
		},
		Console:   &TaskConsoleConfig{Mode: "Tty"},
		Network:   []TaskNetworkConfig{{Mac: "52:54:00:aa:bb:cc", AutoTuntap: true, AutoTuntapBridge: "br0"}},
		CloudInit: &CloudInit{UserData: "#cloud-config\n"},
		Serial:    "socket=/tmp/serial",
	}

	cfg := buildConfigFromOCIMetadata(metadata, dir, hclog.NewNullLogger())

	// Paths are resolved relative to workDir.
	must.Eq(t, filepath.Join(dir, "vmlinuz"), cfg.Payload.Kernel)
	must.Eq(t, "", cfg.Payload.Initramfs) // not in metadata
	must.Eq(t, "console=ttyS0", cfg.Payload.Cmdline)
	must.SliceLen(t, 2, cfg.Disk)
	must.Eq(t, filepath.Join(dir, "rootfs.raw"), cfg.Disk[0].Path)
	must.Eq(t, "raw", cfg.Disk[0].ImageType)
	must.True(t, cfg.Disk[0].Readonly)
	must.Eq(t, filepath.Join(dir, "data.raw"), cfg.Disk[1].Path)
	must.Eq(t, "Tty", cfg.Console.Mode)
	must.SliceLen(t, 1, cfg.Network)
	must.Eq(t, "52:54:00:aa:bb:cc", cfg.Network[0].Mac)
	must.True(t, cfg.Network[0].AutoTuntap)
	must.Eq(t, "br0", cfg.Network[0].AutoTuntapBridge)
	must.NotNil(t, cfg.CloudInit)
	must.Eq(t, "#cloud-config\n", cfg.CloudInit.UserData)
	must.Eq(t, "socket=/tmp/serial", cfg.Serial)
}

func TestBuildConfigFromOCIMetadata_NoWorkDir(t *testing.T) {
	// Without a workDir (fast metadata phase), relative paths stay relative.
	metadata := &OCIMetadataConfig{
		Payload: &TaskPayloadConfig{Kernel: "vmlinuz"},
		Disk:    []TaskDiskConfig{{Path: "rootfs.raw", ImageType: "raw"}},
	}

	cfg := buildConfigFromOCIMetadata(metadata, "", hclog.NewNullLogger())

	must.Eq(t, "vmlinuz", cfg.Payload.Kernel)
	must.Eq(t, "rootfs.raw", cfg.Disk[0].Path)
}

func TestBuildConfigFromOCIMetadata_AbsolutePathsUnchanged(t *testing.T) {
	// Absolute paths in metadata are left unchanged even when workDir is set.
	metadata := &OCIMetadataConfig{
		Payload: &TaskPayloadConfig{Kernel: "/boot/vmlinuz"},
		Disk:    []TaskDiskConfig{{Path: "/data/rootfs.raw", ImageType: "raw"}},
	}

	cfg := buildConfigFromOCIMetadata(metadata, "/some/workdir", hclog.NewNullLogger())

	must.Eq(t, "/boot/vmlinuz", cfg.Payload.Kernel)
	must.Eq(t, "/data/rootfs.raw", cfg.Disk[0].Path)
}

func TestApplyJobConfig(t *testing.T) {
	base := TaskConfig{
		Payload:   TaskPayloadConfig{Kernel: "/oci/vmlinuz", Initramfs: "/oci/initrd.img", Cmdline: "console=ttyS0"},
		Disk:      []TaskDiskConfig{{Path: "/oci/rootfs.qcow2", ImageType: "qcow2"}},
		Console:   TaskConsoleConfig{Mode: "Tty"},
		Network:   []TaskNetworkConfig{{AutoTuntap: true}},
		CloudInit: &CloudInit{UserData: "#oci-cloud-config\n"},
		Serial:    "tty",
	}
	job := &TaskConfig{
		Payload: TaskPayloadConfig{Kernel: "/job/custom-kernel", Cmdline: "quiet"},
		Serial:  "socket=/tmp/serial",
	}

	result := applyJobConfig(base, job, hclog.NewNullLogger())

	// Job fields win.
	must.Eq(t, "/job/custom-kernel", result.Payload.Kernel)
	must.Eq(t, "quiet", result.Payload.Cmdline)
	must.Eq(t, "socket=/tmp/serial", result.Serial)

	// OCI fields survive where the job left them empty.
	must.Eq(t, "/oci/initrd.img", result.Payload.Initramfs)
	must.SliceLen(t, 1, result.Disk)
	must.Eq(t, "/oci/rootfs.qcow2", result.Disk[0].Path)
	must.Eq(t, "Tty", result.Console.Mode)
	must.SliceLen(t, 1, result.Network)
	must.NotNil(t, result.CloudInit)

	// base is not mutated.
	must.Eq(t, "/oci/vmlinuz", base.Payload.Kernel)
	must.Eq(t, "console=ttyS0", base.Payload.Cmdline)
	must.Eq(t, "tty", base.Serial)
}
