// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package oci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cavaliergopher/cpio"
)

// CreateBusyboxInitrd creates a minimal initramfs image backed by busybox.
//
// The returned image contains:
//   - /init with the provided contents
//   - /bin/busybox copied from the busybox binary found in PATH
//   - common busybox applet symlinks in /bin (e.g. sh, echo, poweroff)
//
// The init contents are written as-is if they already start with a shebang.
// Otherwise they are wrapped in a simple /bin/sh script header.
func CreateBusyboxInitrd(initContents string) (string, error) {
	busyboxPath, err := exec.LookPath("busybox")
	if err != nil {
		return "", fmt.Errorf("locate busybox: %w", err)
	}

	busyboxData, err := os.ReadFile(busyboxPath)
	if err != nil {
		return "", fmt.Errorf("read busybox binary: %w", err)
	}

	initData := normalizeInitContents(initContents)

	out, err := os.CreateTemp("", "nomad-ch-initrd-*.cpio")
	if err != nil {
		return "", fmt.Errorf("create initrd file: %w", err)
	}

	w := cpio.NewWriter(out)
	if err := writeBusyboxInitrd(w, busyboxData, initData); err != nil {
		_ = w.Close()
		_ = out.Close()
		_ = os.Remove(out.Name())
		return "", err
	}

	if err := w.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(out.Name())
		return "", fmt.Errorf("close cpio archive: %w", err)
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(out.Name())
		return "", fmt.Errorf("close initrd file: %w", err)
	}

	return out.Name(), nil
}

func normalizeInitContents(initContents string) []byte {
	if strings.HasPrefix(initContents, "#!") {
		if !strings.HasSuffix(initContents, "\n") {
			initContents += "\n"
		}
		return []byte(initContents)
	}

	if !strings.HasSuffix(initContents, "\n") {
		initContents += "\n"
	}

	return []byte("#!/bin/sh\nset -eux\n" + initContents)
}

func writeBusyboxInitrd(w *cpio.Writer, busyboxData, initData []byte) error {
	now := time.Now()

	for _, dir := range []string{".", "bin", "dev", "proc", "sys", "tmp", "etc"} {
		if err := w.WriteHeader(&cpio.Header{
			Name:    dir,
			Mode:    cpio.FileMode(cpio.TypeDir | 0o755),
			Links:   2,
			ModTime: now,
		}); err != nil {
			return fmt.Errorf("write directory %q: %w", dir, err)
		}
	}

	if err := writeFile(w, "init", 0o755, initData, now); err != nil {
		return err
	}

	if err := writeFile(w, filepath.Join("bin", "busybox"), 0o755, busyboxData, now); err != nil {
		return err
	}

	for _, link := range []string{"sh", "echo", "cat", "mount", "umount", "poweroff", "reboot", "halt", "sleep"} {
		target := "busybox"

		if err := w.WriteHeader(&cpio.Header{
			Name:     filepath.Join("bin", link),
			Linkname: target, // fine to keep for in-memory metadata
			Mode:     cpio.FileMode(cpio.TypeSymlink | 0o755),
			Size:     int64(len(target)),
			ModTime:  now,
		}); err != nil {
			return fmt.Errorf("write symlink header %q: %w", link, err)
		}

		if _, err := w.Write([]byte(target)); err != nil {
			return fmt.Errorf("write symlink target %q: %w", link, err)
		}
	}

	return nil
}

func writeFile(w *cpio.Writer, name string, perm os.FileMode, payload []byte, modTime time.Time) error {
	if err := w.WriteHeader(&cpio.Header{
		Name:    name,
		Mode:    cpio.FileMode(cpio.TypeReg | cpio.FileMode(perm)),
		Size:    int64(len(payload)),
		ModTime: modTime,
	}); err != nil {
		return fmt.Errorf("write file %q header: %w", name, err)
	}

	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write file %q body: %w", name, err)
	}

	return nil
}
