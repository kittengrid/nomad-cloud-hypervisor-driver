// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/nomad/drivers/shared/executor"
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
	binaryPath, socketDir, taskID string,
	stdoutPath, stderrPath string,
	cfg TaskConfig,
	exec executor.Executor,
) (*CloudHypervisorProcess, error) {
	socketBasePath := filepath.Join(socketDir, filepath.Base(taskID))

	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	// Remove any stale socket left by a previous crash.
	_ = os.Remove(socketBasePath + ".sock")

	execCmd := &executor.ExecCommand{
		Cmd:        binaryPath,
		Args:       buildCHArgs(cfg, socketBasePath),
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
	}

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
func buildCHArgs(cfg TaskConfig, socketBasePath string) []string {
	args := []string{"--api-socket", "path=" + socketBasePath + ".sock"}
	args = append(args, "--serial", "socket="+socketBasePath+".serial.sock")
	if cfg.Payload.Kernel != "" {
		args = append(args, "--kernel", cfg.Payload.Kernel)
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
		diskArgs = append(diskArgs, diskArgEntry)
	}

	args = append(args, "--disk")
	args = append(args, diskArgs...)

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

	return args
}
