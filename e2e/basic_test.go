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

	"github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/nomadtest"
	"github.com/shoenig/test/must"
)

const timeout = 60 * time.Second

func pause() {
	if os.Getenv("CI") == "" {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
}

// onlyTest skips the current test if the E2E_TEST environment variable is set
// and does not match the test name.  Leave E2E_TEST unset to run all tests.
//
//	E2E_TEST=TestOCIHelloFromVM go test -tags=e2e -v ./e2e/...
func onlyTest(t *testing.T) {
	t.Helper()
	if name := os.Getenv("E2E_TEST"); name != "" && t.Name() != name {
		t.Skipf("skipping %s: E2E_TEST=%s", t.Name(), name)
	}
}

func setup(t *testing.T) (context.Context, *nomadtest.NomadAgent) {
	onlyTest(t)
	requireRoot(t)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(func() {
		cancel()
	})

	nomad := nomadtest.NewNomadAgent()
	if err := nomad.Start(t); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = nomad.Stop(t)
	})

	pause()
	return ctx, nomad
}

func run(t *testing.T, ctx context.Context, command string, args ...string) string {
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

func requireRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
}

func TestPluginStarts(t *testing.T) {
	ctx, _ := setup(t)

	status := run(t, ctx, "nomad", "node", "status", "-self", "-verbose")
	pluginRe := regexp.MustCompile(`cloud-hypervisor\s+true\s+true\s+Healthy`)
	must.RegexMatch(t, pluginRe, status)
}
