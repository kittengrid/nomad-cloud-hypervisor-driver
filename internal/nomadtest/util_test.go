// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package nomadtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- parseVarArgs ---

func TestParseVarArgs_empty(t *testing.T) {
	got, err := parseVarArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty string, got %q", got)
	}
}

func TestParseVarArgs_single(t *testing.T) {
	got, err := parseVarArgs([]string{"-var=foo=bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `foo = "bar"`
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestParseVarArgs_multiple(t *testing.T) {
	got, err := parseVarArgs([]string{"-var=a=1", "-var=b=hello world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a = \"1\"\nb = \"hello world\""
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestParseVarArgs_valueContainsEquals(t *testing.T) {
	// VALUE part may itself contain '=' — only the first '=' is the separator.
	got, err := parseVarArgs([]string{"-var=url=http://example.com/path?a=b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `url = "http://example.com/path?a=b"`
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestParseVarArgs_missingPrefix(t *testing.T) {
	_, err := parseVarArgs([]string{"foo=bar"})
	if err == nil {
		t.Fatal("expected error for missing -var= prefix")
	}
}

func TestParseVarArgs_missingEquals(t *testing.T) {
	_, err := parseVarArgs([]string{"-var=noequalssign"})
	if err == nil {
		t.Fatal("expected error for missing = in variable")
	}
}

// --- waitForNomadReadyFile ---

func TestWaitForNomadReadyFile_stdout(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout.log")
	stderr := filepath.Join(dir, "stderr.log")

	writeLog(t, stdout, nomadReadyLine+"\n")
	writeLog(t, stderr, "some stderr\n")

	if err := waitForNomadReadyFile(stdout, stderr, 2*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNomadReadyFile_stderr(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout.log")
	stderr := filepath.Join(dir, "stderr.log")

	writeLog(t, stdout, "some output\n")
	writeLog(t, stderr, nomadReadyLine+"\n")

	if err := waitForNomadReadyFile(stdout, stderr, 2*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNomadReadyFile_readyLineEmbedded(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout.log")
	stderr := filepath.Join(dir, "stderr.log")

	writeLog(t, stdout, "starting agent\n2025-01-01 "+nomadReadyLine+", id=abc\n")
	writeLog(t, stderr, "")

	if err := waitForNomadReadyFile(stdout, stderr, 2*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForNomadReadyFile_timeout(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout.log")
	stderr := filepath.Join(dir, "stderr.log")

	writeLog(t, stdout, "not ready yet\n")
	writeLog(t, stderr, "also not ready\n")

	err := waitForNomadReadyFile(stdout, stderr, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), nomadReadyLine) {
		t.Fatalf("error should mention the expected line, got: %v", err)
	}
}

func TestWaitForNomadReadyFile_missingFiles(t *testing.T) {
	// Files don't exist yet — should time out gracefully, not panic.
	err := waitForNomadReadyFile("/nonexistent/stdout.log", "/nonexistent/stderr.log", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error for missing files")
	}
}

func writeLog(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
