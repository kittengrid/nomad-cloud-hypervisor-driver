// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

// Run these tests manually by setting the e2e tag when running go test, e.g.
//
//	go test -tags=e2e -v
//
// For editing set: export GOFLAGS='--tags=e2e'

package e2e

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

const timeout = 60 * time.Second

var runningRe = regexp.MustCompile(`Status\s+=\s+running`)

func pause() {
	if os.Getenv("CI") == "" {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
}

func setup(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(func() {
		run(t, ctx, "nomad", "system", "gc")
		cancel()
	})
	pause()
	return ctx
}

func run(t *testing.T, ctx context.Context, command string, args ...string) string {
	t.Logf("RUN '%s %s'", command, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, command, args...)
	b, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(b))
	if err != nil {
		t.Log("ERR:", err)
		t.Log("OUT:", output)
		t.FailNow()
	}
	return output
}

func purge(t *testing.T, ctx context.Context, job string) func() {
	return func() {
		t.Log("STOP", job)
		cmd := exec.CommandContext(ctx, "nomad", "job", "stop", "-purge", job)
		b, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(b))
		if err != nil {
			t.Log("ERR:", err)
			t.Log("OUT:", output)
		}
		pause()
	}
}

func requireRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
}

func requireEnv(t *testing.T, key string) string {
	value := os.Getenv(key)
	if value == "" {
		t.Skipf("missing %s", key)
	}
	return value
}

func TestPluginStarts(t *testing.T) {
	ctx := setup(t)

	status := run(t, ctx, "nomad", "node", "status", "-self", "-verbose")
	pluginRe := regexp.MustCompile(`cloud-hypervisor\s+true\s+true\s+Healthy`)
	must.RegexMatch(t, pluginRe, status)
}

func TestBasicVM(t *testing.T) {
	ctx := setup(t)
	defer purge(t, ctx, "ch-basic")()

	kernel := requireEnv(t, "CH_KERNEL")
	initramfs := requireEnv(t, "CH_INITRAMFS")
	rootfs := requireEnv(t, "CH_ROOTFS")
	cmdline := requireEnv(t, "CH_CMDLINE")

	_ = run(t, ctx, "nomad", "job", "run",
		"-var=kernel="+kernel,
		"-var=initramfs="+initramfs,
		"-var=rootfs="+rootfs,
		"-var=cmdline="+cmdline,
		"./jobs/basic.hcl",
	)

	status := run(t, ctx, "nomad", "job", "status", "ch-basic")
	must.RegexMatch(t, runningRe, status)

	stopOutput := run(t, ctx, "nomad", "job", "stop", "ch-basic")
	must.StrContains(t, stopOutput, `finished with status "complete"`)
}
