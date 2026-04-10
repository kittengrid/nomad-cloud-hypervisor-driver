// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"fmt"
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
	cfg        NomadConfig
	cmd        *exec.Cmd
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
			StartupWait: 60 * time.Second,
		},
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

	if err := waitForNomadReadyFile(n.stdoutPath, n.cfg.StartupWait); err != nil {
		_ = n.Stop(t)
		return err
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

func waitForNomadReadyFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), nomadReadyLine) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("nomad agent did not emit %q within %s", nomadReadyLine, timeout)
}

func (n *NomadAgent) running() bool {
	return n.cmd != nil && n.cmd.Process != nil && n.cmd.ProcessState == nil
}
