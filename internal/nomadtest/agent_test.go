// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package nomadtest

import (
	"os"
	"testing"
)

func TestAgentStartsAndStatus(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}

	agent := NewNomadAgent()

	if err := agent.Start(t); err != nil {
		t.Fatalf("start nomad agent: %v", err)
	}

	leader := agent.Status(t)
	if leader == "" {
		t.Fatal("expected non-empty leader address from /v1/status/leader")
	}

}

func TestRestartWithTheSameDataDir(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}

	agent := NewNomadAgent()
	agent.SetDataDir(t.TempDir())

	if err := agent.Start(t); err != nil {
		t.Fatalf("start nomad agent: %v", err)
	}

	if err := agent.Stop(t); err != nil {
		t.Fatalf("stop nomad agent: %v", err)
	}

	if err := agent.Start(t); err != nil {
		t.Fatalf("restart nomad agent: %v", err)
	}
	leader := agent.Status(t)
	if leader == "" {
		t.Fatal("expected non-empty leader address from /v1/status/leader")
	}
}
