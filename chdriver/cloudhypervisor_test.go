// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"testing"

	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/shoenig/test/must"
)

func TestBuildCHArgs(t *testing.T) {
	cfg := TaskConfig{
		Payload: TaskPayloadConfig{
			Kernel:    "/tmp/vmlinuz",
			Initramfs: "/tmp/initramfs",
			Cmdline:   "console=ttyS0 root=/dev/vda1",
		},
		Disk: []TaskDiskConfig{
			{
				Path:      "/tmp/rootfs.raw",
				ImageType: "raw",
				Readonly:  true,
			},
			{
				Path:             "/tmp/overlay.qcow2",
				ImageType:        "qcow2",
				EphemeralOverlay: true,
			},
		},
		Network: []TaskNetworkConfig{
			{
				AutoTuntap: true,
				Mac:        "52:54:00:12:34:56",
			},
			{
				Tap: "tap1",
			},
		},
		Console: TaskConsoleConfig{Mode: "off"},
		Serial:  "socket=/tmp/ch-sock.serial.sock",
	}

	resources := &drivers.Resources{
		NomadResources: &structs.AllocatedTaskResources{
			Cpu: structs.AllocatedCpuResources{
				ReservedCores: []uint16{0, 1},
			},
			Memory: structs.AllocatedMemoryResources{
				MemoryMB: 512,
			},
		},
	}

	args, err := buildCHArgs(cfg, resources, "/tmp/ch-sock", "alloc/task/abc")
	must.NoError(t, err)

	must.Eq(t, []string{
		"--api-socket", "path=/tmp/ch-sock.sock",
		"--serial", "socket=/tmp/ch-sock.serial.sock",
		"--kernel", "/tmp/vmlinuz",
		"--initramfs", "/tmp/initramfs",
		"--cmdline", "console=ttyS0 root=/dev/vda1",
		"--disk",
		"path=/tmp/rootfs.raw,image_type=raw,readonly=on",
		"path=/tmp/overlay.qcow2,image_type=qcow2,backing_files=on",
		"--net",
		"tap=tap-abc,mac=52:54:00:12:34:56",
		"tap=tap1",
		"--cpus", "boot=2",
		"--memory", "size=536870912",
		"--console", "off",
	}, args)
}

func TestVcpusFromResources(t *testing.T) {
	t.Run("reserved cores", func(t *testing.T) {
		resources := &drivers.Resources{
			NomadResources: &structs.AllocatedTaskResources{
				Cpu: structs.AllocatedCpuResources{ReservedCores: []uint16{0, 1, 2}},
			},
		}
		must.Eq(t, 3, vcpusFromResources(resources))
	})

	t.Run("cpu shares", func(t *testing.T) {
		resources := &drivers.Resources{
			NomadResources: &structs.AllocatedTaskResources{
				Cpu: structs.AllocatedCpuResources{CpuShares: 2500},
			},
		}
		must.Eq(t, 2, vcpusFromResources(resources))
	})

	t.Run("nil resources", func(t *testing.T) {
		must.Eq(t, 1, vcpusFromResources(nil))
	})
}

func TestMemoryBytesFromResources(t *testing.T) {
	t.Run("memory set", func(t *testing.T) {
		resources := &drivers.Resources{
			NomadResources: &structs.AllocatedTaskResources{
				Memory: structs.AllocatedMemoryResources{MemoryMB: 256},
			},
		}
		must.Eq(t, int64(256*1024*1024), memoryBytesFromResources(resources))
	})

	t.Run("nil resources", func(t *testing.T) {
		must.Eq(t, int64(0), memoryBytesFromResources(nil))
	})
}
