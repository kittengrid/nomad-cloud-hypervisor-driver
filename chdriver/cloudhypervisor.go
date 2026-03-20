// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package chdriver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// CloudHypervisorProcess holds the identifiers needed to track a running
// cloud-hypervisor instance.
type CloudHypervisorProcess struct {
	Pid        int
	SocketPath string
}

// startCloudHypervisor launches cloud-hypervisor as an independent process.
// Using Setsid the child becomes a session leader so it survives driver restarts —
// when the driver process dies the child is re-parented to init rather than killed.
// The API socket is created at <socketDir>/<taskID>.sock.
func startCloudHypervisor(
	binaryPath, socketDir, taskID string,
	stdoutPath, stderrPath string,
	cfg TaskConfig,
) (*CloudHypervisorProcess, error) {
	socketPath := filepath.Join(socketDir, filepath.Base(taskID)+".sock")

	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	// Remove any stale socket left by a previous crash.
	_ = os.Remove(socketPath)

	stdout, err := os.OpenFile(stdoutPath, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open stdout path: %w", err)
	}

	stderr, err := os.OpenFile(stderrPath, os.O_WRONLY, 0)
	if err != nil {
		stdout.Close()
		return nil, fmt.Errorf("open stderr path: %w", err)
	}

	cmd := exec.Command(binaryPath, buildCHArgs(cfg, socketPath)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start cloud-hypervisor: %w", err)
	}

	go func() {
		_ = cmd.Wait()
		stdout.Close()
		stderr.Close()
	}()

	return &CloudHypervisorProcess{
		Pid:        cmd.Process.Pid,
		SocketPath: socketPath,
	}, nil
}

// buildCHArgs constructs the CLI argument list for cloud-hypervisor from a TaskConfig.
func buildCHArgs(cfg TaskConfig, socketPath string) []string {
	args := []string{"--api-socket", "path=" + socketPath}

	if cfg.Payload.Kernel != "" {
		args = append(args, "--kernel", cfg.Payload.Kernel)
	}

	for _, disk := range cfg.Disk {
		diskArg := "path=" + disk.Path
		if disk.ImageType != "" {
			diskArg += ",image_type=" + disk.ImageType
		}
		args = append(args, "--disk", diskArg)
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

	if cfg.Serial.Mode != "" {
		args = append(args, "--serial", cfg.Serial.Mode)
	}

	if cfg.Console.Mode != "" {
		args = append(args, "--console", cfg.Console.Mode)
	}

	return args
}
