package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpListsPublicCommands(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"prismgo-lens", "--help"}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run help exit = %d, want 0; output=%s", code, stdout.String())
	}
	output := stdout.String()
	for _, command := range []string{"install", "update", "mcp", "execute-tool", "browser-proxy", "add-skill", "list-skills"} {
		if !strings.Contains(output, command) {
			t.Fatalf("help output missing %q: %s", command, output)
		}
	}
}

func TestRunExecuteToolPrintsJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module prismgo\n\ngo 1.26.2\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"prismgo-lens", "--project", root, "execute-tool", "application-info", "e30="}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("execute-tool exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"app_name"`) {
		t.Fatalf("execute-tool output missing app_name: %s", stdout.String())
	}
}
