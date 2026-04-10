// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	StartupWait time.Duration
}

// NomadAgent manages a Nomad agent process for tests.
type NomadAgent struct {
	cfg         NomadConfig
	cmd         *exec.Cmd
	configFile  string
	stdoutPath  string
	stderrPath  string
	ownsDataDir bool
}

// NewNomadAgent returns a Nomad agent helper configured for the local e2e files.
func NewNomadAgent() *NomadAgent {
	return &NomadAgent{
		cfg: NomadConfig{
			Binary:      "nomad",
			ConfigPath:  filepath.Join(".", "agent.hcl"),
			PluginDir:   filepath.Clean(filepath.Join("..")),
			StartupWait: 60 * time.Second,
		},
	}
}

// Start launches Nomad and waits until the expected registration line appears
// on stdout.
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
	if n.cfg.PluginDir == "" {
		if env := os.Getenv("NOMAD_PLUGIN_DIR"); env != "" {
			n.cfg.PluginDir = env
		} else {
			n.cfg.PluginDir = filepath.Clean(filepath.Join(".."))
		}
	}
	if n.cfg.StartupWait <= 0 {
		n.cfg.StartupWait = 60 * time.Second
	}

	baseConfig, err := os.ReadFile(n.cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("read nomad config %q: %w", n.cfg.ConfigPath, err)
	}

	configPath := n.cfg.ConfigPath
	if n.cfg.DataDir != "" {
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
		configPath = n.configFile
	} else {
		configPath = n.cfg.ConfigPath
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

	args := []string{"agent", "-config=" + configPath, "-plugin-dir=" + n.cfg.PluginDir}
	if n.cfg.DataDir == "" {
		args = append(args, "-dev")
	} else {
		if err := os.MkdirAll(n.cfg.DataDir, 0o755); err != nil {
			_ = stdoutFile.Close()
			_ = stderrFile.Close()
			return fmt.Errorf("create nomad data dir: %w", err)
		}
		n.ownsDataDir = false
	}

	n.cmd = exec.Command(n.cfg.Binary, args...)

	stdoutPipe, err := n.cmd.StdoutPipe()
	if err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := n.cmd.StderrPipe()
	if err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := n.cmd.Start(); err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		return fmt.Errorf("start nomad agent: %w", err)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrFile, stderrPipe)
	}()

	ready, scanErr := waitForNomadReady(stdoutPipe, stdoutFile, n.cfg.StartupWait)
	_ = stderrFile.Close()
	<-stderrDone
	_ = stdoutFile.Close()

	if scanErr != nil {
		_ = n.Stop(t)
		return scanErr
	}
	if !ready {
		_ = n.Stop(t)
		return fmt.Errorf("nomad agent did not emit %q within %s", nomadReadyLine, n.cfg.StartupWait)
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
	return nil
}

func waitForNomadReady(stdout io.Reader, stdoutFile *os.File, timeout time.Duration) (bool, error) {
	type result struct {
		ready bool
		err   error
	}

	resultCh := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(io.TeeReader(stdout, stdoutFile))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), nomadReadyLine) {
				resultCh <- result{ready: true}
				return
			}
		}
		resultCh <- result{ready: false, err: scanner.Err()}
	}()

	select {
	case <-time.After(timeout):
		return false, nil
	case res := <-resultCh:
		return res.ready, res.err
	}
}

func (n *NomadAgent) running() bool {
	return n.cmd != nil && n.cmd.Process != nil && n.cmd.ProcessState == nil
}
