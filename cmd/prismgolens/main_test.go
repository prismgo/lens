package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunUsesPrismgolensCLI(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"prismgolens", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "prismgolens [--project PATH] <command>") {
		t.Fatalf("help should show formal command name, got:\n%s", stdout.String())
	}
}
