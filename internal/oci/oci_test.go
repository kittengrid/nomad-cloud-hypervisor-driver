// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package oci

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

func TestPushAndPullOCIImageRoundTrip(t *testing.T) {
	ctx := context.Background()
	registry := StartTempRegistry(t)

	sourceDir := CreateOCIImageDir(t, OCIImageOptions{
		InitContents: `#!/bin/sh
set -eux
mount -t proc proc /proc
mount -t sysfs sysfs /sys
echo "hello from the initrd"
poweroff -f
`,
		Config: map[string]any{
			"payload": map[string]any{
				"cmdline":   "console=ttyS0",
				"kernel":    "vmlinuz",
				"initramfs": "initrd.img",
			},
			"serial": "tty",
		},
	})

	ref := pushOCIImageDirToRegistry(t, registry, "kittengrid/roundtrip", "latest", sourceDir)

	cacheDir := t.TempDir()
	logger := hclog.NewNullLogger()
	artifact, err := PullIntoCache(ctx, PullOptions{Reference: ref, CacheDir: cacheDir}, logger, nil)
	if err != nil {
		t.Fatalf("pull into cache: %v", err)
	}

	assertDirsEqual(t, sourceDir, artifact.WorkDir)
}

func pushOCIImageDirToRegistry(t testing.TB, reg *TempRegistry, repository, tag, dir string) string {
	t.Helper()

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

func assertDirsEqual(t *testing.T, wantDir, gotDir string) {
	t.Helper()

	want := collectFiles(t, wantDir)
	got := collectFiles(t, gotDir)

	if !reflect.DeepEqual(keys(want), keys(got)) {
		t.Fatalf("directory entries differ:\nwant: %v\ngot:  %v", keys(want), keys(got))
	}

	for _, name := range keys(want) {
		if !reflect.DeepEqual(want[name], got[name]) {
			t.Fatalf("file %q differs", name)
		}
	}
}

func collectFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()

	files := map[string][]byte{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == ".complete" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = b
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
