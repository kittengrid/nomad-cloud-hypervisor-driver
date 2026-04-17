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

type OCIMetadataConfig struct {
	Payload   *TaskPayloadConfig  `json:"payload,omitempty"`
	Disk      []TaskDiskConfig    `json:"disk,omitempty"`
	Console   *TaskConsoleConfig  `json:"console,omitempty"`
	Network   []TaskNetworkConfig `json:"network,omitempty"`
	CloudInit *CloudInit          `json:"cloud-init,omitempty"`
	Serial    string              `json:"serial,omitempty"`
}

type OCIMetadata struct {
	Name    string             `json:"name"`
	Version string             `json:"version"`
	Arch    string             `json:"arch"`
	Config  *OCIMetadataConfig `json:"config,omitempty"`
}

func resolveOCIPayload(ctx context.Context, cfg *TaskConfig, cacheDir string, logger hclog.Logger) error {
	if cfg == nil || cfg.OCIImage == "" {
		return nil
	}

	logger.Info("Resolving OCI payload metadata", "oci_image", cfg.OCIImage)

	metadataBytes, err := oci.FetchMetadata(ctx, cfg.OCIImage)
	if err != nil {
		return fmt.Errorf("fetch OCI payload metadata: %w", err)
	}

	metadata, err := parseOCIMetadata(metadataBytes, logger)
	if err != nil {
		return fmt.Errorf("read OCI metadata: %w", err)
	}

	// Build base from OCI metadata only, then let the job's explicit settings
	// win — mirroring how `docker run --entrypoint` overrides the image
	// entrypoint. The actual artifact is materialized later in StartTask.
	ociBase := buildConfigFromOCIMetadata(metadata, logger)
	ociBase.OCIImage = cfg.OCIImage
	*cfg = applyJobConfig(ociBase, cfg, logger)
	return nil
}

func readOCIMetadata(path string, logger hclog.Logger) (*OCIMetadataConfig, error) {
	logger.Info("Reading OCI metadata", "path", path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("OCI metadata.json not found at %q: %w", path, err)
		}
		return nil, err
	}
	return parseOCIMetadata(data, logger)
}

func parseOCIMetadata(data []byte, logger hclog.Logger) (*OCIMetadataConfig, error) {
	logger.Debug("OCI metadata content", "content", string(data))

	var metadata OCIMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return metadata.Config, nil
}

// buildConfigFromOCIMetadata builds a TaskConfig from OCI image metadata only.
// Relative paths and filesystem defaults are applied later, once the artifact
// has been materialized locally.
func buildConfigFromOCIMetadata(metadata *OCIMetadataConfig, logger hclog.Logger) TaskConfig {
	cfg := TaskConfig{}

	if metadata != nil {
		if metadata.Payload != nil {
			if metadata.Payload.Kernel != "" {
				cfg.Payload.Kernel = metadata.Payload.Kernel
				logger.Info("OCI image config", "field", "payload.kernel", "value", cfg.Payload.Kernel)
			}
			if metadata.Payload.Initramfs != "" {
				cfg.Payload.Initramfs = metadata.Payload.Initramfs
				logger.Info("OCI image config", "field", "payload.initramfs", "value", cfg.Payload.Initramfs)
			}
			if metadata.Payload.Cmdline != "" {
				cfg.Payload.Cmdline = metadata.Payload.Cmdline
				logger.Info("OCI image config", "field", "payload.cmdline", "value", cfg.Payload.Cmdline)
			}
		}
		if len(metadata.Disk) > 0 {
			cfg.Disk = make([]TaskDiskConfig, len(metadata.Disk))
			copy(cfg.Disk, metadata.Disk)
			logger.Info("OCI image config", "field", "disk", "entries", len(cfg.Disk))
		}
		if metadata.Console != nil && metadata.Console.Mode != "" {
			cfg.Console.Mode = metadata.Console.Mode
			logger.Info("OCI image config", "field", "console.mode", "value", cfg.Console.Mode)
		}
		if len(metadata.Network) > 0 {
			cfg.Network = metadata.Network
			logger.Info("OCI image config", "field", "network", "entries", len(cfg.Network))
		}
		if metadata.CloudInit != nil {
			cfg.CloudInit = metadata.CloudInit
			logger.Info("OCI image config", "field", "cloud-init")
		}
		if metadata.Serial != "" {
			cfg.Serial = metadata.Serial
			logger.Info("OCI image config", "field", "serial", "value", cfg.Serial)
		}
	}

	return cfg
}

// applyOCIArtifact applies workdir-relative path resolution and filesystem defaults
// after the OCI artifact has been materialized locally.
func applyOCIArtifact(cfg *TaskConfig, workDir string, logger hclog.Logger) {
	if cfg == nil {
		return
	}

	if cfg.Payload.Kernel != "" {
		cfg.Payload.Kernel = resolvePath(workDir, cfg.Payload.Kernel)
	}
	if cfg.Payload.Initramfs != "" {
		cfg.Payload.Initramfs = resolvePath(workDir, cfg.Payload.Initramfs)
	}
	for i := range cfg.Disk {
		cfg.Disk[i].Path = resolvePath(workDir, cfg.Disk[i].Path)
	}

	if kernelPath := filepath.Join(workDir, "vmlinuz"); cfg.Payload.Kernel == "" && fileExists(kernelPath) {
		cfg.Payload.Kernel = kernelPath
		logger.Info("OCI payload kernel set", "path", kernelPath)
	}
	if initramfsPath := filepath.Join(workDir, "initrd.img"); cfg.Payload.Initramfs == "" && fileExists(initramfsPath) {
		cfg.Payload.Initramfs = initramfsPath
		logger.Info("OCI payload initramfs set", "path", initramfsPath)
	}
	if rootfsPath := filepath.Join(workDir, "rootfs.qcow2"); fileExists(rootfsPath) && !hasDiskPath(cfg.Disk) {
		cfg.Disk = append(cfg.Disk, TaskDiskConfig{Path: rootfsPath, ImageType: "qcow2"})
		logger.Info("OCI payload rootfs attached", "path", rootfsPath)
	}
}

// applyJobConfig returns a new TaskConfig with non-zero fields from job merged
// on top of base, giving the job's explicit settings priority over whatever the
// OCI image provides — analogous to overriding a Docker image entrypoint at
// run time.
func materializeOCIPayload(ctx context.Context, cfg *TaskConfig, cacheDir string, logger hclog.Logger) error {
	if cfg == nil || cfg.OCIImage == "" {
		return nil
	}

	logger.Info("Materializing OCI payload", "oci_image", cfg.OCIImage, "cache_dir", cacheDir)
	artifact, err := oci.PullIntoCache(ctx, oci.PullOptions{Reference: cfg.OCIImage, CacheDir: cacheDir}, logger)
	if err != nil {
		return fmt.Errorf("pull OCI payload: %w", err)
	}

	applyOCIArtifact(cfg, artifact.WorkDir, logger)
	return nil
}

func applyJobConfig(base TaskConfig, job *TaskConfig, logger hclog.Logger) TaskConfig {
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
	return base
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
