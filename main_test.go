package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelpListsPublicCommands(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"prismgo-lens", "--help"}, strings.NewReader(""), &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run help exit = %d, want 0; output=%s", code, stdout.String())
	}
	for _, command := range []string{"install", "update", "mcp", "execute-tool", "browser-proxy", "add-skill", "list-skills"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help output missing %q: %s", command, stdout.String())
		}
	}
}
