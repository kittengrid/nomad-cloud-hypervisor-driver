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

type TaskPayloadOverride struct {
	Kernel    *string `json:"kernel,omitempty"`
	Initramfs *string `json:"initramfs,omitempty"`
	Cmdline   *string `json:"cmdline,omitempty"`
}

type TaskDiskOverride struct {
	Path             *string `json:"path,omitempty"`
	ImageType        *string `json:"image_type,omitempty"`
	Readonly         *bool   `json:"readonly,omitempty"`
	EphemeralOverlay *bool   `json:"ephemeral_overlay,omitempty"`
	OCIImage         *string `json:"oci_image,omitempty"`
}

type TaskConsoleOverride struct {
	Mode *string `json:"mode,omitempty"`
}

type TaskNetworkOverride struct {
	Mac              *string `json:"mac,omitempty"`
	Tap              *string `json:"tap,omitempty"`
	AutoTuntap       *bool   `json:"auto-tuntap,omitempty"`
	AutoTuntapBridge *string `json:"auto-tuntap-bridge,omitempty"`
}

type CloudInitOverride struct {
	UserData *string `json:"user-data,omitempty"`
	MetaData *string `json:"meta-data,omitempty"`
}

type TaskConfigOverrides struct {
	Payload   *TaskPayloadOverride  `json:"payload,omitempty"`
	Disk      []TaskDiskOverride    `json:"disk,omitempty"`
	Console   *TaskConsoleOverride  `json:"console,omitempty"`
	Network   []TaskNetworkOverride `json:"network,omitempty"`
	CloudInit *CloudInitOverride    `json:"cloud-init,omitempty"`
	Serial    *string               `json:"serial,omitempty"`
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

	if overrides != nil {
		applyTaskConfigOverrides(cfg, overrides, artifact.WorkDir, logger)
	}

	applyOCIPayloadDefaults(cfg, artifact.WorkDir, logger)
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
		if overrides.Payload.Kernel != nil {
			resolved := resolvePath(baseDir, *overrides.Payload.Kernel)
			cfg.Payload.Kernel = resolved
			logger.Info("OCI override applied", "field", "payload.kernel", "value", resolved)
		}
		if overrides.Payload.Initramfs != nil {
			resolved := resolvePath(baseDir, *overrides.Payload.Initramfs)
			cfg.Payload.Initramfs = resolved
			logger.Info("OCI override applied", "field", "payload.initramfs", "value", resolved)
		}
		if overrides.Payload.Cmdline != nil {
			cfg.Payload.Cmdline = *overrides.Payload.Cmdline
			logger.Info("OCI override applied", "field", "payload.cmdline", "value", cfg.Payload.Cmdline)
		}
	}

	if overrides.Disk != nil {
		cfg.Disk = make([]TaskDiskConfig, 0, len(overrides.Disk))
		logger.Info("OCI override applied", "field", "disk", "entries", len(overrides.Disk))
		for _, disk := range overrides.Disk {
			updated := TaskDiskConfig{}
			if disk.Path != nil {
				updated.Path = resolvePath(baseDir, *disk.Path)
			}
			if disk.ImageType != nil {
				updated.ImageType = *disk.ImageType
			}
			if disk.Readonly != nil {
				updated.Readonly = *disk.Readonly
			}
			if disk.EphemeralOverlay != nil {
				updated.EphemeralOverlay = *disk.EphemeralOverlay
			}
			if disk.OCIImage != nil {
				updated.OCIImage = *disk.OCIImage
			}
			cfg.Disk = append(cfg.Disk, updated)
		}
	}

	if overrides.Console != nil {
		if overrides.Console.Mode != nil {
			cfg.Console.Mode = *overrides.Console.Mode
			logger.Info("OCI override applied", "field", "console.mode", "value", cfg.Console.Mode)
		}
	}

	if overrides.Network != nil {
		cfg.Network = make([]TaskNetworkConfig, 0, len(overrides.Network))
		logger.Info("OCI override applied", "field", "network", "entries", len(overrides.Network))
		for _, net := range overrides.Network {
			updated := TaskNetworkConfig{}
			if net.Mac != nil {
				updated.Mac = *net.Mac
			}
			if net.Tap != nil {
				updated.Tap = *net.Tap
			}
			if net.AutoTuntap != nil {
				updated.AutoTuntap = *net.AutoTuntap
			}
			if net.AutoTuntapBridge != nil {
				updated.AutoTuntapBridge = *net.AutoTuntapBridge
			}
			cfg.Network = append(cfg.Network, updated)
		}
	}

	if overrides.CloudInit != nil {
		cfg.CloudInit = &CloudInit{}
		logger.Info("OCI override applied", "field", "cloud-init")
		if overrides.CloudInit.UserData != nil {
			cfg.CloudInit.UserData = *overrides.CloudInit.UserData
			logger.Info("OCI override applied", "field", "cloud-init.user-data")
		}
		if overrides.CloudInit.MetaData != nil {
			cfg.CloudInit.MetaData = *overrides.CloudInit.MetaData
			logger.Info("OCI override applied", "field", "cloud-init.meta-data")
		}
	}
	if overrides.Serial != nil {
		cfg.Serial = *overrides.Serial
		logger.Info("OCI override applied", "field", "serial", "value", cfg.Serial)
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
