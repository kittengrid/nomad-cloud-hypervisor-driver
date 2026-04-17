// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package oci

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// OCIImageOptions describes a tiny OCI payload layout on disk.
//
// The helper creates a temporary directory containing:
//   - vmlinuz
//   - initramfs.img
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
	copyFile(t, initrd, filepath.Join(dir, "initrd.img"))

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

// PushOCIImageToRegistry creates a local OCI image from opts and pushes it to
// the provided temporary registry.
func PushOCIImageToRegistry(t testing.TB, reg interface{ Reference(string, string) string }, repository, tag string, opts OCIImageOptions) string {
	t.Helper()

	dir := CreateOCIImageDir(t, opts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ref := reg.Reference(repository, tag)
	repo, err := remote.NewRepository(ref)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	repo.PlainHTTP = true
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}

	metadataBytes := readFile(t, filepath.Join(dir, "metadata.json"))
	configDesc, err := oras.PushBytes(ctx, repo, "application/json", metadataBytes)
	if err != nil {
		t.Fatalf("push metadata.json: %v", err)
	}

	layers := []ocispec.Descriptor{}
	for _, name := range []string{"vmlinuz", "initrd.img"} {
		b := readFile(t, filepath.Join(dir, name))
		desc, err := oras.PushBytes(ctx, repo, "application/octet-stream", b)
		if err != nil {
			t.Fatalf("push %s: %v", name, err)
		}
		desc.Annotations = map[string]string{ocispec.AnnotationTitle: name}
		layers = append(layers, desc)
	}

	manifestDesc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		ConfigDescriptor: &configDesc,
		Layers:           layers,
	})
	if err != nil {
		t.Fatalf("pack manifest: %v", err)
	}

	if _, err := oras.Tag(ctx, repo, manifestDesc.Digest.String(), tag); err != nil {
		t.Fatalf("tag manifest: %v", err)
	}

	return ref
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

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
