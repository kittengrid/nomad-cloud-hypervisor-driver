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

func resolveOCIPayload(ctx context.Context, cfg *TaskConfig, cacheDir, dockerConfigPath string, logger hclog.Logger) error {
	if cfg == nil || cfg.OCIImage == "" {
		return nil
	}

	logger.Info("Resolving OCI payload metadata", "oci_image", cfg.OCIImage)

	metadataBytes, err := oci.FetchMetadata(ctx, cfg.OCIImage, dockerConfigPath)
	if err != nil {
		return fmt.Errorf("fetch OCI payload metadata: %w", err)
	}

	metadata, err := parseOCIMetadata(metadataBytes, logger)
	if err != nil {
		return fmt.Errorf("read OCI metadata: %w", err)
	}

	// Build base from OCI metadata only (no workDir yet — paths stay relative),
	// then let the job's explicit settings win. Paths are resolved against the
	// artifact workDir later, once the image is materialized.
	ociBase := buildConfigFromOCIMetadata(metadata, "", logger)
	ociBase.OCIImage = cfg.OCIImage
	*cfg = applyJobConfig(ociBase, cfg, logger)
	return nil
}

func readOCIMetadata(path string, logger hclog.Logger) (*OCIMetadataConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	logger.Info("Reading OCI metadata", "path", path)
	return parseOCIMetadata(data, logger)
}

func parseOCIMetadata(data []byte, logger hclog.Logger) (*OCIMetadataConfig, error) {
	logger.Debug("OCI metadata content", "content", string(data))

	var metadata OCIMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	logger.Info("Parsed OCI metadata", metadata)

	return metadata.Config, nil
}

// buildConfigFromOCIMetadata builds a TaskConfig from OCI image metadata.
// workDir is used to resolve relative paths from the metadata; pass "" when
// the artifact has not been materialized yet (paths will remain relative).
func buildConfigFromOCIMetadata(metadata *OCIMetadataConfig, workDir string, logger hclog.Logger) TaskConfig {
	logger.Info("Building TaskConfig from OCI metadata", "work_dir", workDir)
	logger.Debug("OCI metadata content", "metadata", metadata)

	cfg := TaskConfig{}
	if metadata == nil {
		return cfg
	}

	if metadata.Payload != nil {
		cfg.Payload = TaskPayloadConfig{}
		cfg.Payload.Kernel = resolvePath(workDir, metadata.Payload.Kernel)
		cfg.Payload.Initramfs = resolvePath(workDir, metadata.Payload.Initramfs)
		cfg.Payload.Cmdline = metadata.Payload.Cmdline
	}

	for _, d := range metadata.Disk {
		disk := d
		disk.Path = resolvePath(workDir, d.Path)
		cfg.Disk = append(cfg.Disk, disk)
	}
	if len(cfg.Disk) > 0 {
		logger.Info("OCI image config", "field", "disk", "entries", len(cfg.Disk))
	}

	if metadata.Console != nil {
		cfg.Console.Mode = metadata.Console.Mode
	}

	cfg.Network = metadata.Network
	cfg.CloudInit = metadata.CloudInit
	cfg.Serial = metadata.Serial

	return cfg
}

// applyJobConfig returns a new TaskConfig with non-zero fields from job merged
// on top of base, giving the job's explicit settings priority over whatever the
// OCI image provides — analogous to overriding a Docker image entrypoint at
// run time.
func materializeOCIPayload(ctx context.Context, cfg, jobCfg *TaskConfig, cacheDir, dockerConfigPath string, logger hclog.Logger, progress oci.ProgressFunc) error {
	if cfg == nil || cfg.OCIImage == "" {
		return nil
	}

	logger.Info("Materializing OCI payload", "oci_image", cfg.OCIImage, "cache_dir", cacheDir)
	artifact, err := oci.PullIntoCache(ctx, oci.PullOptions{Reference: cfg.OCIImage, CacheDir: cacheDir, DockerConfigPath: dockerConfigPath}, logger, progress)
	if err != nil {
		return fmt.Errorf("pull OCI payload: %w", err)
	}

	// Re-build from the downloaded metadata.json so that buildConfigFromOCIMetadata
	// resolves all paths against the real workDir, then re-apply the original job
	// overrides so explicit job settings always win.
	metadata, err := readOCIMetadata(filepath.Join(artifact.WorkDir, "metadata.json"), logger)
	if err != nil {
		return fmt.Errorf("read OCI metadata: %w", err)
	}
	ociBase := buildConfigFromOCIMetadata(metadata, artifact.WorkDir, logger)
	ociBase.OCIImage = jobCfg.OCIImage
	*cfg = applyJobConfig(ociBase, jobCfg, logger)
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
	if job.Balloon != nil {
		base.Balloon = job.Balloon
		logger.Info("job config override", "field", "balloon")
	}
	return base
}

func resolvePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}
