// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

func TestOCIHelloFromVM(t *testing.T) {
	ctx, nomad := setup(t)
	defer purge(t, ctx, "ch-oci")()

	registry := StartTempRegistry(t)
	imageRef := PushOCIImageToRegistry(t, registry, "kittengrid/hello-vm", "latest", OCIImageOptions{
		InitContents: `#!/bin/sh
set -eux
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
echo "hello from the VM"
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

	nomad.RunJob(t, ctx, "ch-oci", "./jobs/oci.hcl", "-var=oci_image="+imageRef)
	t.Logf("submitted job with OCI image %s", imageRef)

	status := waitUntil(t, 120*time.Second, func() *JobStatus {
		t.Logf("fetching job status for ch-oci")
		status := nomad.JobStatus(t, ctx, "ch-oci")
		t.Logf("job status: %#v", status)

		return status
	}, func(s *JobStatus) bool { return s != nil && s.Status == "dead" })
	if len(status.Allocations) == 0 {
		t.Fatal("no allocations returned for job")
	}
	alloc := status.Allocations[0].ID
	t.Logf("job has allocation %s", alloc)

	allocStatus := waitUntil(t, 120*time.Second, func() *AllocStatus {
		t.Logf("fetching alloc status for alloc %s", alloc)
		status := nomad.AllocStatus(t, ctx, alloc)
		t.Logf("alloc status: %#v", status)

		return status
	}, func(s *AllocStatus) bool {
		return s != nil && (s.ClientStatus == "running" || s.ClientStatus == "complete")
	})
	t.Logf("alloc status: %#v", allocStatus)

	logs := waitUntil(t, 120*time.Second, func() string {
		t.Logf("fetching logs for alloc %s", alloc)

		string := nomad.AllocLogs(t, ctx, alloc, "vm").Stdout
		t.Logf("logs:\n%s", string)

		return string
	}, func(s string) bool {

		t.Logf("checking logs for alloc %s", alloc)

		return strings.Contains(s, "hello from the VM")
	})
	must.StrContains(t, logs, "hello from the VM")
}

func submit(t *testing.T, ctx context.Context, command string, args ...string) {
	t.Helper()
	t.Logf("SUBMIT '%s %s'", command, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, command, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", command, err)
	}
}

func waitUntil[T any](t *testing.T, timeout time.Duration, fn func() T, ok func(T) bool) T {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last T
	for time.Now().Before(deadline) {
		last = fn()
		if ok(last) {
			return last
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting; last value: %#v", last)
	return last
}
