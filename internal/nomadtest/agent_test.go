// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package nomadtest

import (
	"os"
	"strings"
	"testing"
	"time"

	testutils "github.com/kittengrid/nomad-cloud-hypervisor-driver/internal/test_utils"
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

func TestSimpleExecJob(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}

	agent := NewNomadAgent()
	if err := agent.Start(t); err != nil {
		t.Fatalf("start nomad agent: %v", err)
	}

	ctx := t.Context()
	agent.RunJob(t, ctx, "simple-exec", testutils.GetFixtureFileContents(t, "simple_exec_job.hcl"))

	status := testutils.WaitUntil(t, 60*time.Second, func() *JobStatus {
		return agent.JobStatus(t, ctx, "simple-exec")
	}, func(s *JobStatus) bool { return s != nil && s.Status == "dead" })

	if len(status.Allocations) == 0 {
		t.Fatal("no allocations for job")
	}

	allocID := status.Allocations[0].ID
	allocStatus := agent.AllocStatus(t, ctx, allocID)
	t.Logf("alloc status: %+v", allocStatus)

	logs := agent.AllocLogs(t, ctx, allocID, "hello")
	t.Logf("stdout: %s", logs.Stdout)
	if !strings.Contains(logs.Stdout, "hello world") {
		t.Fatalf("expected stdout to contain %q, got %q", "hello world", logs.Stdout)
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
