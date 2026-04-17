// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/nomadtest"
	testoci "github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/oci"
	testutils "github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/test_utils"
	"github.com/shoenig/test/must"
)

func TestOCIHelloFromVM(t *testing.T) {
	ctx, nomad := setup(t)
	defer purge(t, ctx, "ch-oci")()

	registry := testoci.StartTempRegistry(t)
	imageRef := testoci.PushOCIImageToRegistry(t, registry, "kittengrid/hello-vm", "latest", testoci.OCIImageOptions{
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

	nomad.RunJob(t, ctx, "ch-oci", testutils.GetFixtureFileContents(t, "oci.hcl"), "-var=oci_image="+imageRef)
	status := testutils.WaitUntil(t, 120*time.Second, func() *nomadtest.JobStatus {
		status := nomad.JobStatus(t, ctx, "ch-oci")

		return status
	}, func(s *nomadtest.JobStatus) bool { return s != nil && s.Status == "dead" })
	if len(status.Allocations) == 0 {
		t.Fatal("no allocations returned for job")
	}
	alloc := status.Allocations[0].ID
	testutils.WaitUntil(t, 120*time.Second, func() *nomadtest.AllocStatus {
		status := nomad.AllocStatus(t, ctx, alloc)
		return status
	}, func(s *nomadtest.AllocStatus) bool {
		return s != nil && (s.ClientStatus == "running" || s.ClientStatus == "complete")
	})
	logs := testutils.WaitUntil(t, 120*time.Second, func() string {
		string := nomad.AllocLogs(t, ctx, alloc, "vm").Stdout
		return string
	}, func(s string) bool {

		return strings.Contains(s, "hello from the VM")
	})
	must.StrContains(t, logs, "hello from the VM")
}
