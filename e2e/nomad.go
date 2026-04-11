// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nomadapi "github.com/hashicorp/nomad/api"
)

const nomadReadyLine = "node registration complete"

// NomadConfig controls how a Nomad agent is started in e2e tests.
//
// If DataDir is empty, the agent is started with -dev and no data dir is
// created. If DataDir is set, the agent is started normally and the data dir is
// left intact so tests can stop/start Nomad while preserving state.
type NomadConfig struct {
	Binary      string
	ConfigPath  string
	PluginDir   string
	DataDir     string
	Address     string
	StartupWait time.Duration
}

// AllocationStatus captures the important bits of a job allocation.
type AllocationStatus struct {
	ID           string
	TaskGroup    string
	NodeID       string
	NodeName     string
	Desired      string
	ClientStatus string
}

// AllocationLogs captures stdout/stderr content for an allocation task.
type AllocationLogs struct {
	Stdout string
	Stderr string
}

// JobStatus captures the important bits of a Nomad job.
type JobStatus struct {
	ID          string
	Name        string
	Type        string
	Status      string
	Namespace   string
	Allocations []*AllocationStatus
}

// AllocStatus captures the important bits of a specific allocation.
type AllocStatus struct {
	ID           string
	JobID        string
	TaskGroup    string
	NodeID       string
	NodeName     string
	Desired      string
	ClientStatus string
}

// NomadAgent manages a Nomad agent process for tests.
type NomadAgent struct {
	cfg        NomadConfig
	cmd        *exec.Cmd
	client     *nomadapi.Client
	configFile string
	stdoutPath string
	stderrPath string
}

// NewNomadAgent returns a Nomad agent helper configured for the local e2e files.
func NewNomadAgent() *NomadAgent {
	return &NomadAgent{
		cfg: NomadConfig{
			Binary:      "nomad",
			ConfigPath:  filepath.Join(".", "agent.hcl"),
			Address:     "127.0.0.1:4646",
			StartupWait: 60 * time.Second,
		},
	}
}

// RunJob submits an HCL job file using the Nomad API client.
// Only -var=KEY=VALUE style arguments are supported.
func (n *NomadAgent) RunJob(t testing.TB, ctx context.Context, jobName, jobFile string, args ...string) {
	t.Helper()

	client := n.mustClient(t)
	jobHCL, err := os.ReadFile(jobFile)
	if err != nil {
		t.Fatalf("read job file %q: %v", jobFile, err)
	}

	variables, err := parseVarArgs(args)
	if err != nil {
		t.Fatalf("parse job vars: %v", err)
	}

	job, err := client.Jobs().ParseHCLOpts(&nomadapi.JobsParseRequest{
		JobHCL:    string(jobHCL),
		Variables: variables,
	})
	if err != nil {
		t.Fatalf("parse job %q: %v", jobFile, err)
	}

	if jobName != "" {
		if job.Name == nil {
			job.Name = &jobName
		}
		if job.ID == nil {
			job.ID = &jobName
		}
	}

	if _, _, err := client.Jobs().Register(job, nil); err != nil {
		t.Fatalf("register job %q: %v", jobFile, err)
	}
}

// JobStatus returns the important status fields for a job.
func (n *NomadAgent) JobStatus(t testing.TB, ctx context.Context, jobName string) *JobStatus {
	t.Helper()

	client := n.mustClient(t)
	job, _, err := client.Jobs().Info(jobName, nil)
	if err != nil {
		t.Fatalf("get job status %q: %v", jobName, err)
	}

	allocs, _, err := client.Jobs().Allocations(jobName, true, nil)
	if err != nil {
		t.Fatalf("get allocations for %q: %v", jobName, err)
	}

	status := &JobStatus{
		ID:          stringValue(job.ID),
		Name:        stringValue(job.Name),
		Type:        stringValue(job.Type),
		Status:      stringValue(job.Status),
		Namespace:   stringValue(job.Namespace),
		Allocations: make([]*AllocationStatus, 0, len(allocs)),
	}
	for _, a := range allocs {
		status.Allocations = append(status.Allocations, &AllocationStatus{
			ID:           a.ID,
			TaskGroup:    a.TaskGroup,
			NodeID:       a.NodeID,
			NodeName:     a.NodeName,
			Desired:      a.DesiredStatus,
			ClientStatus: a.ClientStatus,
		})
	}

	return status
}

// AllocStatus returns the important status fields for a specific allocation.
func (n *NomadAgent) AllocStatus(t testing.TB, ctx context.Context, allocID string) *AllocStatus {
	t.Helper()

	client := n.mustClient(t)
	alloc, _, err := client.Allocations().Info(allocID, nil)
	if err != nil {
		t.Fatalf("get allocation status %q: %v", allocID, err)
	}

	return &AllocStatus{
		ID:           alloc.ID,
		JobID:        alloc.JobID,
		TaskGroup:    alloc.TaskGroup,
		NodeID:       alloc.NodeID,
		NodeName:     alloc.NodeName,
		Desired:      alloc.DesiredStatus,
		ClientStatus: alloc.ClientStatus,
	}
}

// AllocLogs returns stdout/stderr content for a task in an allocation.
func (n *NomadAgent) AllocLogs(t testing.TB, ctx context.Context, allocID, task string) *AllocationLogs {
	t.Helper()

	client := n.mustClient(t)
	alloc, _, err := client.Allocations().Info(allocID, nil)
	if err != nil {
		t.Fatalf("get allocation logs %q: %v", allocID, err)
	}

	return &AllocationLogs{
		Stdout: n.readAllocLog(t, ctx, client, alloc, task, "stdout"),
		Stderr: n.readAllocLog(t, ctx, client, alloc, task, "stderr"),
	}
}

// Start launches Nomad and waits until the expected registration line appears
// in its stdout log.
func (n *NomadAgent) Start(t testing.TB) error {
	t.Helper()

	if n.running() {
		return nil
	}

	if n.cfg.Binary == "" {
		n.cfg.Binary = "nomad"
	}
	if n.cfg.ConfigPath == "" {
		n.cfg.ConfigPath = filepath.Join(".", "agent.hcl")
	}
	if n.cfg.Address == "" {
		n.cfg.Address = "127.0.0.1:4646"
	}
	if n.cfg.PluginDir == "" {
		if env := os.Getenv("NOMAD_PLUGIN_DIR"); env != "" {
			n.cfg.PluginDir = env
		} else {
			pluginDir, err := filepath.Abs("..")
			if err != nil {
				return fmt.Errorf("resolve plugin dir: %w", err)
			}
			n.cfg.PluginDir = pluginDir
		}
	}
	if n.cfg.StartupWait <= 0 {
		n.cfg.StartupWait = 60 * time.Second
	}

	configPath, err := filepath.Abs(n.cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("resolve nomad config path: %w", err)
	}
	baseConfig, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read nomad config %q: %w", configPath, err)
	}

	finalConfigPath := configPath
	if n.cfg.DataDir != "" {
		if err := os.MkdirAll(n.cfg.DataDir, 0o755); err != nil {
			return fmt.Errorf("create nomad data dir: %w", err)
		}
		configFile, err := os.CreateTemp("", "nomad-ch-agent-*.hcl")
		if err != nil {
			return fmt.Errorf("create temp nomad config: %w", err)
		}
		n.configFile = configFile.Name()
		if _, err := fmt.Fprintf(configFile, "data_dir = %q\n\n%s", n.cfg.DataDir, string(baseConfig)); err != nil {
			_ = configFile.Close()
			return fmt.Errorf("write temp nomad config: %w", err)
		}
		if err := configFile.Close(); err != nil {
			return fmt.Errorf("close temp nomad config: %w", err)
		}
		finalConfigPath = n.configFile
	}

	stdoutFile, err := os.CreateTemp("", "nomad-ch-stdout-*.log")
	if err != nil {
		return fmt.Errorf("create nomad stdout log: %w", err)
	}
	stderrFile, err := os.CreateTemp("", "nomad-ch-stderr-*.log")
	if err != nil {
		_ = stdoutFile.Close()
		return fmt.Errorf("create nomad stderr log: %w", err)
	}
	n.stdoutPath = stdoutFile.Name()
	n.stderrPath = stderrFile.Name()
	_ = stdoutFile.Close()
	_ = stderrFile.Close()

	args := []string{"agent", "-config=" + finalConfigPath, "-plugin-dir=" + n.cfg.PluginDir}
	if n.cfg.DataDir == "" {
		args = append(args, "-dev")
	}

	n.cmd = exec.Command(n.cfg.Binary, args...)
	n.cmd.Stdout, err = os.OpenFile(n.stdoutPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open stdout log: %w", err)
	}
	n.cmd.Stderr, err = os.OpenFile(n.stderrPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}

	if err := n.cmd.Start(); err != nil {
		return fmt.Errorf("start nomad agent: %w", err)
	}

	if err := waitForNomadReadyFile(n.stdoutPath, n.stderrPath, n.cfg.StartupWait); err != nil {
		_ = n.Stop(t)
		return err
	}

	clientCfg := nomadapi.DefaultConfig()
	clientCfg.Address = "http://" + n.cfg.Address
	n.client, err = nomadapi.NewClient(clientCfg)
	if err != nil {
		_ = n.Stop(t)
		return fmt.Errorf("create nomad api client: %w", err)
	}

	t.Cleanup(func() {
		_ = n.Stop(t)
		if n.configFile != "" {
			_ = os.Remove(n.configFile)
		}
		if n.stdoutPath != "" {
			_ = os.Remove(n.stdoutPath)
		}
		if n.stderrPath != "" {
			_ = os.Remove(n.stderrPath)
		}
	})

	return nil
}

// Stop terminates the Nomad agent process.
func (n *NomadAgent) Stop(t testing.TB) error {
	t.Helper()

	if n.cmd == nil || n.cmd.Process == nil {
		return nil
	}
	if n.cmd.ProcessState != nil && n.cmd.ProcessState.Exited() {
		n.cmd = nil
		n.client = nil
		return nil
	}

	_ = n.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- n.cmd.Wait() }()

	select {
	case <-time.After(10 * time.Second):
		_ = n.cmd.Process.Kill()
		<-done
	case <-done:
	}

	n.cmd = nil
	n.client = nil
	return nil
}

func waitForNomadReadyFile(stdoutPath, stderrPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var stdout, stderr string
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(stdoutPath); err == nil {
			stdout = string(data)
			if strings.Contains(stdout, nomadReadyLine) {
				return nil
			}
		}
		if data, err := os.ReadFile(stderrPath); err == nil {
			stderr = string(data)
			if strings.Contains(stderr, nomadReadyLine) {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("nomad agent did not emit %q within %s\nstdout:\n%s\nstderr:\n%s", nomadReadyLine, timeout, stdout, stderr)
}

func (n *NomadAgent) running() bool {
	return n.cmd != nil && n.cmd.Process != nil && n.cmd.ProcessState == nil
}

func (n *NomadAgent) mustClient(t testing.TB) *nomadapi.Client {
	t.Helper()
	if n.client == nil {
		t.Fatal("nomad client not initialized")
	}
	return n.client
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (n *NomadAgent) readAllocLog(t testing.TB, ctx context.Context, client *nomadapi.Client, alloc *nomadapi.Allocation, task, logType string) string {
	t.Helper()

	if ctx == nil {
		ctx = context.Background()
	}

	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var out strings.Builder
	frames, errs := client.AllocFS().Logs(alloc, true, task, logType, "start", 0, logCtx.Done(), nil)
	for frames != nil || errs != nil {
		select {
		case frame, ok := <-frames:
			if !ok {
				frames = nil
				continue
			}
			if frame != nil && len(frame.Data) > 0 {
				_, _ = out.Write(frame.Data)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				if coded, ok := err.(interface{ Code() int }); ok && coded.Code() == http.StatusNotFound {
					return strings.TrimSpace(out.String())
				}
				t.Fatalf("read allocation log %q/%q: %v", alloc.ID, logType, err)
			}
			errs = nil
		case <-logCtx.Done():
			frames = nil
			errs = nil
		}
	}
	return strings.TrimSpace(out.String())
}

func parseVarArgs(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}

	lines := make([]string, 0, len(args))
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-var=") {
			return "", fmt.Errorf("unsupported arg %q (only -var=KEY=VALUE is supported)", arg)
		}
		kv := strings.TrimPrefix(arg, "-var=")
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid variable %q", arg)
		}
		lines = append(lines, fmt.Sprintf("%s = %q", parts[0], parts[1]))
	}
	return strings.Join(lines, "\n"), nil
}
