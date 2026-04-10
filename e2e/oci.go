// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// OCIImageOptions describes a tiny OCI payload layout on disk.
//
// The helper creates a temporary directory containing:
//   - vmlinuz
//   - initramfs.img
//   - rootfs.raw
//   - metadata.json
//
// The metadata file uses the driver's expected format:
//
//	{
//	  "name": "...",
//	  "version": "...",
//	  "arch": "...",
//	  "config": { ... task driver overrides ... }
//	}
//
// The returned directory can be used as a local source of truth for later
// pushing to a registry or for constructing job inputs in tests.
type OCIImageOptions struct {
	Name    string
	Version string
	Arch    string

	// Config is serialized under metadata.json's top-level "config" key.
	// It may be any JSON-marshalable value, typically a map or a typed struct.
	Config any

	// InitContents is the init script used to generate initramfs.img via the
	// busybox helper.
	InitContents string

	// KernelPath optionally overrides the kernel image to copy into the temp dir.
	// If empty, a host kernel is discovered automatically.
	KernelPath string

	// RootfsSize controls the size of the generated raw disk image.
	// Defaults to 64 MiB.
	RootfsSize int64
}

// CreateOCIImageDir creates a temporary directory with a minimal OCI-style
// payload layout suitable for tests.
func CreateOCIImageDir(t testing.TB, opts OCIImageOptions) string {
	t.Helper()

	if opts.Name == "" {
		opts.Name = "test-image"
	}
	if opts.Version == "" {
		opts.Version = "latest"
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	if opts.RootfsSize <= 0 {
		opts.RootfsSize = 64 * 1024 * 1024
	}

	dir := t.TempDir()

	kernelSrc := opts.KernelPath
	if kernelSrc == "" {
		kernelSrc = discoverKernelImage(t)
	}
	copyFile(t, kernelSrc, filepath.Join(dir, "vmlinuz"))

	initrd, err := CreateBusyboxInitrd(opts.InitContents)
	if err != nil {
		t.Fatalf("create busybox initrd: %v", err)
	}
	copyFile(t, initrd, filepath.Join(dir, "initramfs.img"))

	if err := createSparseFile(filepath.Join(dir, "rootfs.raw"), opts.RootfsSize); err != nil {
		t.Fatalf("create rootfs image: %v", err)
	}

	metadata := map[string]any{
		"name":    opts.Name,
		"version": opts.Version,
		"arch":    opts.Arch,
	}
	if opts.Config != nil {
		metadata["config"] = opts.Config
	}

	b, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), b, 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	return dir
}

func discoverKernelImage(t testing.TB) string {
	t.Helper()

	if kernel := os.Getenv("CH_KERNEL"); kernel != "" {
		if fileExists(kernel) {
			return kernel
		}
		t.Fatalf("CH_KERNEL points to missing file: %s", kernel)
	}

	if kernel := os.Getenv("KERNEL_IMAGE"); kernel != "" {
		if fileExists(kernel) {
			return kernel
		}
		t.Fatalf("KERNEL_IMAGE points to missing file: %s", kernel)
	}

	if releaseOut, err := exec.Command("uname", "-r").CombinedOutput(); err == nil {
		release := strings.TrimSpace(string(releaseOut))
		candidates := []string{
			filepath.Join("/boot", "vmlinuz-"+release),
			filepath.Join("/boot", "kernel-"+release),
		}
		for _, candidate := range candidates {
			if fileExists(candidate) {
				return candidate
			}
		}
	}

	matches, _ := filepath.Glob("/boot/vmlinuz*")
	for _, candidate := range matches {
		if fileExists(candidate) {
			return candidate
		}
	}

	t.Fatalf("could not discover a kernel image; set CH_KERNEL or KERNEL_IMAGE")
	return ""
}

func copyFile(t testing.TB, src, dst string) {
	t.Helper()

	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s -> %s: %v", src, dst, err)
	}

	if err := out.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}

func createSparseFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := f.Truncate(size); err != nil {
		return err
	}

	return f.Close()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
