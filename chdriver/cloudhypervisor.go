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

	args, err := buildCHArgs(driverConfig, socketBasePath, taskID)
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

// buildCHArgs constructs the CLI argument list for cloud-hypervisor from a TaskConfig.
func buildCHArgs(cfg TaskConfig, socketBasePath string, taskID string) ([]string, error) {
	args := []string{"--api-socket", "path=" + socketBasePath + ".sock"}

	args = append(args, "--serial", "socket="+socketBasePath+".serial.sock")
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
			diskArgEntry += ",backing_files=on"
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

	if cfg.Cpus.BootVcpus > 0 {
		cpuArg := fmt.Sprintf("boot=%d", cfg.Cpus.BootVcpus)
		if cfg.Cpus.MaxVcpus > cfg.Cpus.BootVcpus {
			cpuArg += fmt.Sprintf(",max=%d", cfg.Cpus.MaxVcpus)
		}
		args = append(args, "--cpus", cpuArg)
	}

	if cfg.Memory.Size > 0 {
		args = append(args, "--memory", fmt.Sprintf("size=%d", cfg.Memory.Size))
	}

	if cfg.Console.Mode != "" {
		args = append(args, "--console", cfg.Console.Mode)
	}

	return args, nil
}
