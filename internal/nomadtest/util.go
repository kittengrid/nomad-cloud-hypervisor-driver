// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package nomadtest

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const nomadReadyLine = "node registration complete"

// parseVarArgs parses a list of -var=KEY=VALUE arguments into a Nomad variable
// string suitable for the Jobs.ParseHCLOpts API.
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

// waitForNomadReadyFile polls stdout and stderr log files until the Nomad
// agent emits its "node registration complete" line or the timeout expires.
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
	return fmt.Errorf("nomad agent did not emit %q within %s\nstdout:\n%s\nstderr:\n%s",
		nomadReadyLine, timeout, stdout, stderr)
}
