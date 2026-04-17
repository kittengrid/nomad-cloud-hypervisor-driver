// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0
//
// metadata.json structure:
// {
//   "name": "example-image",
//   "version": "1.0.0",
//   "arch": "amd64",
//   "config": {
//     "payload": {"kernel": "vmlinuz", "initramfs": "initramfs.img", "cmdline": "console=ttyS0"},
//     "disk": [{"path": "rootfs.qcow2", "image_type": "qcow2", "ephemearl_overlay": true}],
//     "console": {"mode": "Tty"},
//     "network": [{"auto-tuntap": true}],
//     "cloud-init": {"user-data": "#cloud-config\n"}
//   }
// }

package chdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/oci"
)

type TaskConfigOverrides struct {
	Payload   *TaskPayloadConfig   `json:"payload,omitempty"`
	Disk      []TaskDiskConfig     `json:"disk,omitempty"`
	Console   *TaskConsoleConfig   `json:"console,omitempty"`
	Network   []TaskNetworkConfig  `json:"network,omitempty"`
	CloudInit *CloudInit           `json:"cloud-init,omitempty"`
	Serial    string               `json:"serial,omitempty"`
}

type OCIMetadata struct {
	Name    string               `json:"name"`
	Version string               `json:"version"`
	Arch    string               `json:"arch"`
	Config  *TaskConfigOverrides `json:"config,omitempty"`
}

func resolveOCIPayload(ctx context.Context, cfg *TaskConfig, cacheDir string, logger hclog.Logger) error {
	logger.Info("Resolving OCI payload", "oci_image", cfg.OCIImage, "cache_dir", cacheDir)
	if cfg == nil || cfg.OCIImage == "" {
		return nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	pullOptions := oci.PullOptions{
		Reference: cfg.OCIImage,
		CacheDir:  cacheDir,
	}

	artifact, err := oci.PullIntoCache(ctx, pullOptions, logger)
	if err != nil {
		return fmt.Errorf("pull OCI payload: %w", err)
	}

	overrides, err := readOCIMetadataOverrides(filepath.Join(artifact.WorkDir, "metadata.json"), logger)
	if err != nil {
		return fmt.Errorf("read OCI metadata: %w", err)
	}
	logger.Info("OCI metadata read", "overrides_present", overrides != nil)

	// Build the base config from the OCI image (metadata + filesystem defaults),
	// then let the job's explicit settings win on top — mirroring how
	// `docker run --entrypoint` overrides the image entrypoint.
	ociBase := &TaskConfig{OCIImage: cfg.OCIImage}
	if overrides != nil {
		applyTaskConfigOverrides(ociBase, overrides, artifact.WorkDir, logger)
	}
	applyOCIPayloadDefaults(ociBase, artifact.WorkDir, logger)
	applyJobConfig(ociBase, cfg, logger)
	*cfg = *ociBase
	return nil
}

func readOCIMetadataOverrides(path string, logger hclog.Logger) (*TaskConfigOverrides, error) {
	data, err := os.ReadFile(path)
	logger.Info("Reading OCI metadata", "path", path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("OCI metadata.json not found at %q: %w", path, err)
		}
		return nil, err
	}
	logger.Debug("OCI metadata content", "content", string(data))

	var metadata OCIMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return metadata.Config, nil
}

func applyTaskConfigOverrides(cfg *TaskConfig, overrides *TaskConfigOverrides, baseDir string, logger hclog.Logger) {
	if cfg == nil || overrides == nil {
		return
	}

	if overrides.Payload != nil {
		if overrides.Payload.Kernel != "" {
			cfg.Payload.Kernel = resolvePath(baseDir, overrides.Payload.Kernel)
			logger.Info("OCI override applied", "field", "payload.kernel", "value", cfg.Payload.Kernel)
		}
		if overrides.Payload.Initramfs != "" {
			cfg.Payload.Initramfs = resolvePath(baseDir, overrides.Payload.Initramfs)
			logger.Info("OCI override applied", "field", "payload.initramfs", "value", cfg.Payload.Initramfs)
		}
		if overrides.Payload.Cmdline != "" {
			cfg.Payload.Cmdline = overrides.Payload.Cmdline
			logger.Info("OCI override applied", "field", "payload.cmdline", "value", cfg.Payload.Cmdline)
		}
	}

	if len(overrides.Disk) > 0 {
		cfg.Disk = overrides.Disk
		for i := range cfg.Disk {
			cfg.Disk[i].Path = resolvePath(baseDir, cfg.Disk[i].Path)
		}
		logger.Info("OCI override applied", "field", "disk", "entries", len(cfg.Disk))
	}

	if overrides.Console != nil && overrides.Console.Mode != "" {
		cfg.Console.Mode = overrides.Console.Mode
		logger.Info("OCI override applied", "field", "console.mode", "value", cfg.Console.Mode)
	}

	if len(overrides.Network) > 0 {
		cfg.Network = overrides.Network
		logger.Info("OCI override applied", "field", "network", "entries", len(cfg.Network))
	}

	if overrides.CloudInit != nil {
		cfg.CloudInit = overrides.CloudInit
		logger.Info("OCI override applied", "field", "cloud-init")
	}

	if overrides.Serial != "" {
		cfg.Serial = overrides.Serial
		logger.Info("OCI override applied", "field", "serial", "value", cfg.Serial)
	}
}

// applyJobConfig merges non-zero fields from the job's task config onto base,
// giving the job's explicit settings priority over whatever the OCI image
// provides — analogous to overriding a Docker image entrypoint at run time.
func applyJobConfig(base, job *TaskConfig, logger hclog.Logger) {
	if job.Payload.Kernel != "" {
		base.Payload.Kernel = job.Payload.Kernel
		logger.Info("job config override", "field", "payload.kernel", "value", job.Payload.Kernel)
	}
	if job.Payload.Initramfs != "" {
		base.Payload.Initramfs = job.Payload.Initramfs
		logger.Info("job config override", "field", "payload.initramfs", "value", job.Payload.Initramfs)
	}
	if job.Payload.Cmdline != "" {
		base.Payload.Cmdline = job.Payload.Cmdline
		logger.Info("job config override", "field", "payload.cmdline", "value", job.Payload.Cmdline)
	}
	if len(job.Disk) > 0 {
		base.Disk = job.Disk
		logger.Info("job config override", "field", "disk", "entries", len(job.Disk))
	}
	if job.Console.Mode != "" {
		base.Console.Mode = job.Console.Mode
		logger.Info("job config override", "field", "console.mode", "value", job.Console.Mode)
	}
	if len(job.Network) > 0 {
		base.Network = job.Network
		logger.Info("job config override", "field", "network", "entries", len(job.Network))
	}
	if job.CloudInit != nil {
		base.CloudInit = job.CloudInit
		logger.Info("job config override", "field", "cloud-init")
	}
	if job.Serial != "" {
		base.Serial = job.Serial
		logger.Info("job config override", "field", "serial", "value", job.Serial)
	}
}

func applyOCIPayloadDefaults(cfg *TaskConfig, workDir string, logger hclog.Logger) {
	if cfg == nil {
		return
	}

	kernelPath := filepath.Join(workDir, "vmlinuz")
	if cfg.Payload.Kernel == "" && fileExists(kernelPath) {
		cfg.Payload.Kernel = kernelPath
		logger.Info("OCI payload kernel set", "path", kernelPath)
	}

	initramfsPath := filepath.Join(workDir, "initrd.img")
	if cfg.Payload.Initramfs == "" && fileExists(initramfsPath) {
		cfg.Payload.Initramfs = initramfsPath
		logger.Info("OCI payload initramfs set", "path", initramfsPath)
	}

	rootfsPath := filepath.Join(workDir, "rootfs.qcow2")
	if fileExists(rootfsPath) && !hasDiskPath(cfg.Disk) {
		cfg.Disk = append(cfg.Disk, TaskDiskConfig{
			Path:      rootfsPath,
			ImageType: "qcow2",
		})
		logger.Info("OCI payload rootfs attached", "path", rootfsPath)
	}
}

func hasDiskPath(disks []TaskDiskConfig) bool {
	for _, disk := range disks {
		if disk.Path != "" {
			return true
		}
	}
	return false
}

func resolvePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
