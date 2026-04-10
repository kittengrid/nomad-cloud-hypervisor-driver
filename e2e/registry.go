// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const tempRegistryImage = "registry:2"

// TempRegistry represents a temporary local OCI registry.
type TempRegistry struct {
	URL           string
	ContainerName string
	StorageDir    string
}

// StartTempRegistry starts a temporary local registry and registers cleanup.
func StartTempRegistry(t testing.TB) *TempRegistry {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required for the temporary registry helper")
	}

	storageDir, err := os.MkdirTemp("", "nomad-ch-registry-*")
	if err != nil {
		t.Fatalf("create registry storage dir: %v", err)
	}

	port, err := freeTCPPort()
	if err != nil {
		_ = os.RemoveAll(storageDir)
		t.Fatalf("find free registry port: %v", err)
	}

	name := fmt.Sprintf("nomad-ch-registry-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:5000", port),
		"-v", fmt.Sprintf("%s:/var/lib/registry", storageDir),
		tempRegistryImage,
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(storageDir)
		t.Fatalf("start registry container: %v: %s", err, strings.TrimSpace(string(out)))
	}

	registry := &TempRegistry{
		URL:           fmt.Sprintf("127.0.0.1:%d", port),
		ContainerName: name,
		StorageDir:    storageDir,
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", name).Run()
		_ = os.RemoveAll(storageDir)
	})

	registry.waitReady(t)
	return registry
}

func (r *TempRegistry) waitReady(t testing.TB) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	url := "http://" + r.URL + "/v2/"
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("create registry request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("registry at %s did not become ready", url)
}

func (r *TempRegistry) Reference(repository, tag string) string {
	return fmt.Sprintf("%s/%s:%s", r.URL, repository, tag)
}

func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.Port, nil
}
