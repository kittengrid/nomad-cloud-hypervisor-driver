// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package testutils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// GetFixtureFileContents reads a file from the fixtures/ directory relative to
// the caller's source file. This allows tests to locate fixtures without
// depending on the working directory.
func GetFixtureFileContents(t testing.TB, name string) string {
	t.Helper()
	_, callerFile, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(callerFile), "fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return string(data)
}

// WaitUntil polls fn every second until ok(result) returns true or the timeout
// expires. The last value returned by fn is returned. If the timeout expires,
// the test is failed with a diagnostic message.
func WaitUntil[T any](t *testing.T, timeout time.Duration, fn func() T, ok func(T) bool) T {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last T
	for time.Now().Before(deadline) {
		last = fn()
		if ok(last) {
			return last
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out after %s; last value: %#v", timeout, last)
	return last
}
