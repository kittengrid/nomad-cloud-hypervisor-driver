// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// CloudHypervisorProcess holds the identifiers needed to track a running
// cloud-hypervisor instance.
type CloudHypervisorProcess struct {
	Pid            int
	SocketBasePath string
	exec           executor.Executor
}

const (
	// Cloud-hypervisor memory overhead reserved from the cgroup limit.
	// Covers VMM baseline, virtio queues, tap buffers, etc.
	// Ephemeral overlay disks use direct=on to bypass the host page cache,
	// so no extra headroom is needed for qcow2 image caching.
	chMemoryOverheadBytes = 512 * 1024 * 1024 // 512 MiB

	// Don't run a guest smaller than this.
	chMinGuestMemoryBytes = 256 * 1024 * 1024 // 256 MiB
)

// startCloudHypervisor launches cloud-hypervisor as an independent process.
// Using Setsid the child becomes a session leader so it survives driver restarts —
// when the driver process dies the child is re-parented to init rather than killed.
// The API socket is created at <socketDir>/<taskID>.sock.
func startCloudHypervisor(
	binaryPath, socketDir string,
	cfg *drivers.TaskConfig,
	driverConfig TaskConfig,
	exec executor.Executor,
	logger hclog.Logger,
) (*CloudHypervisorProcess, error) {
	taskID := cfg.ID
	stdoutPath := cfg.StdoutPath
	stderrPath := cfg.StderrPath

	socketBasePath := filepath.Join(socketDir, filepath.Base(taskID))

	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	// Remove any stale socket left by a previous crash.
	_ = os.Remove(socketBasePath + ".sock")

	args, err := buildCHArgs(driverConfig, cfg.Resources, socketBasePath, taskID)
	if err != nil {
		return nil, fmt.Errorf("build cloud-hypervisor args: %w", err)
	}

	execCmd := &executor.ExecCommand{
		Cmd:        binaryPath,
		Args:       args,
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
		Resources:  cfg.Resources,
	}
	logger.Debug("launching cloud-hypervisor", "cmd", execCmd.Cmd, "args", execCmd.Args, "stdout", execCmd.StdoutPath, "stderr", execCmd.StderrPath)

	ps, err := exec.Launch(execCmd)
	if err != nil {
		return nil, fmt.Errorf("launch cloud-hypervisor: %w", err)
	}

	return &CloudHypervisorProcess{
		Pid:            ps.Pid,
		SocketBasePath: socketBasePath,
		exec:           exec,
	}, nil
}

// buildCHArgs constructs the CLI argument list for cloud-hypervisor from a TaskConfig
// and the Nomad-allocated resources (used to derive vCPU count and memory size).
func buildCHArgs(cfg TaskConfig, resources *drivers.Resources, socketBasePath string, taskID string) ([]string, error) {
	args := []string{"--api-socket", "path=" + socketBasePath + ".sock"}
	if cfg.Serial != "" {
		args = append(args, "--serial", cfg.Serial)
	}

	//	args = append(args, "--serial", "socket="+socketBasePath+".serial.sock")
	if cfg.Payload.Kernel != "" {
		args = append(args, "--kernel", cfg.Payload.Kernel)
	}

	if cfg.Payload.Initramfs != "" {
		args = append(args, "--initramfs", cfg.Payload.Initramfs)
	}

	if cfg.Payload.Cmdline != "" {
		args = append(args, "--cmdline", cfg.Payload.Cmdline)
	}

	diskArgs := make([]string, 0)
	for _, disk := range cfg.Disk {
		diskArgEntry := "path=" + disk.Path
		if disk.ImageType != "" {
			diskArgEntry += ",image_type=" + disk.ImageType
		}
		if disk.Readonly {
			diskArgEntry += ",readonly=on"
		}
		if disk.EphemeralOverlay {
			if disk.ReflinkCopy {
				// Standalone copy — no backing file chain, so direct=on
				// fully bypasses the host page cache for the entire image.
				diskArgEntry += ",direct=on"
			} else {
				// qcow2 backing-file chain: direct=on only applies to the
				// overlay, not the backing file, so we skip it.
				diskArgEntry += ",backing_files=on"
			}
		}
		diskArgs = append(diskArgs, diskArgEntry)
	}

	if len(diskArgs) > 0 {
		args = append(args, "--disk")
		args = append(args, diskArgs...)
	}

	networkArgs := make([]string, 0)
	for _, net := range cfg.Network {
		netArgEntry := ""

		if net.AutoTuntap {
			ifaceName := TaskIDToTapIfaceName(taskID)
			netArgEntry = "tap=" + ifaceName
			if net.Mac != "" {
				netArgEntry += ",mac=" + net.Mac
			}
		} else {
			netArgEntry = "tap=" + net.Tap
			if net.Mac != "" {
				netArgEntry += ",mac=" + net.Mac
			}
		}
		networkArgs = append(networkArgs, netArgEntry)
	}
	if len(networkArgs) > 0 {
		args = append(args, "--net")
		args = append(args, networkArgs...)
	}

	vcpus := vcpusFromResources(resources)
	args = append(args, "--cpus", fmt.Sprintf("boot=%d", vcpus))

	if memBytes := memoryBytesFromResources(resources); memBytes > 0 {
		guestBytes := memBytes - chMemoryOverheadBytes
		if guestBytes < chMinGuestMemoryBytes {
			return nil, fmt.Errorf(
				"task memory %d bytes too small for cloud-hypervisor: need at least %d (overhead %d + min guest %d)",
				memBytes, chMemoryOverheadBytes+chMinGuestMemoryBytes,
				chMemoryOverheadBytes, chMinGuestMemoryBytes,
			)
		}
		args = append(args, "--memory", fmt.Sprintf("size=%d", guestBytes))
	}

	if cfg.Console.Mode != "" {
		args = append(args, "--console", cfg.Console.Mode)
	}

	return args, nil
}

// vcpusFromResources derives the vCPU count to pass to cloud-hypervisor.
// When the task uses dedicated cores (resources { cores = N }), the length of
// ReservedCores gives the exact count.  When only a CPU-share budget is set
// (resources { cpu = MHz }), we approximate 1 vCPU per 1000 MHz, with a
// minimum of 1.
func vcpusFromResources(r *drivers.Resources) int {
	if r == nil || r.NomadResources == nil {
		return 1
	}
	cpu := r.NomadResources.Cpu
	if len(cpu.ReservedCores) > 0 {
		return len(cpu.ReservedCores)
	}
	if cpu.CpuShares > 0 {
		return max(1, int(cpu.CpuShares)/1000)
	}
	return 1
}

// memoryBytesFromResources converts the Nomad memory allocation (MB) to bytes
// for the cloud-hypervisor --memory flag.
func memoryBytesFromResources(r *drivers.Resources) int64 {
	if r == nil || r.NomadResources == nil {
		return 0
	}
	return r.NomadResources.Memory.MemoryMB * 1024 * 1024
}
