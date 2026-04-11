// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package nomadtest

import (
	"testing"
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
