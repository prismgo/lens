package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prismgo/lens/internal/lens"
)

func TestHandleRPCExecutesToolThroughSubprocessRunner(t *testing.T) {
	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	called := false
	executeToolSubprocess = func(_ context.Context, project string, name string, args json.RawMessage) ([]byte, error) {
		called = true
		if project != "/host" || name != "application-info" || string(args) != `{"ok":true}` {
			t.Fatalf("unexpected subprocess call: project=%s name=%s args=%s", project, name, args)
		}
		return []byte(`{"ok":true}`), nil
	}

	response := handleRPC(context.Background(), "/host", rpcRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"application-info","arguments":{"ok":true}}`),
	})
	if !called {
		t.Fatal("tools/call should execute through subprocess runner")
	}
	data, _ := json.Marshal(response.Result)
	if !strings.Contains(string(data), `{\"ok\":true}`) {
		t.Fatalf("response did not wrap subprocess output: %s", data)
	}
}

func TestHandleRPCReturnsToolSubprocessError(t *testing.T) {
	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	executeToolSubprocess = func(context.Context, string, string, json.RawMessage) ([]byte, error) {
		return nil, errors.New("boom")
	}

	response := handleRPC(context.Background(), "/host", rpcRequest{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"application-info","arguments":{}}`),
	})
	if response.Error == nil {
		t.Fatalf("expected subprocess error response: %+v", response)
	}
}

func TestHandleRPCCoversListUnknownAndInvalidParams(t *testing.T) {
	list := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	data, _ := json.Marshal(list.Result)
	if !strings.Contains(string(data), "application-info") {
		t.Fatalf("tools/list missing registry tools: %s", data)
	}
	invalid := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: json.RawMessage(`{bad`)})
	if invalid.Error == nil {
		t.Fatalf("invalid tools/call params should return error: %+v", invalid)
	}
	unknown := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 3, Method: "missing"})
	if unknown.Error == nil {
		t.Fatalf("unknown method should return error: %+v", unknown)
	}
}

func TestHandleRPCListsAndReadsApplicationInfoResourceThroughToolExecutor(t *testing.T) {
	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	var calls []string
	executeToolSubprocess = func(_ context.Context, project string, name string, args json.RawMessage) ([]byte, error) {
		calls = append(calls, project+" "+name+" "+string(args))
		if project != "/host" || name != "application-info" || string(args) != `{}` {
			t.Fatalf("unexpected resource subprocess call: project=%s name=%s args=%s", project, name, args)
		}
		return []byte(`{"go_version":"go1.26.2","packages":[]}`), nil
	}

	init := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	initData, _ := json.Marshal(init.Result)
	if !strings.Contains(string(initData), `"resources"`) {
		t.Fatalf("initialize should advertise resources capability: %s", initData)
	}

	list := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 2, Method: "resources/list"})
	listData, _ := json.Marshal(list.Result)
	if !strings.Contains(string(listData), "file://instructions/application-info.md") || !strings.Contains(string(listData), "application-info") {
		t.Fatalf("resources/list missing application-info resource: %s", listData)
	}

	read := handleRPC(context.Background(), "/host", rpcRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "resources/read",
		Params:  json.RawMessage(`{"uri":"file://instructions/application-info.md"}`),
	})
	readData, _ := json.Marshal(read.Result)
	if !strings.Contains(string(readData), "go1.26.2") || !strings.Contains(string(readData), "```json") {
		t.Fatalf("resources/read should return markdown-wrapped application info: %s", readData)
	}
	if len(calls) != 1 {
		t.Fatalf("resources/read should execute application-info once, got %v", calls)
	}
}

func TestHandleRPCListsAndGetsPrismGoCodeSimplifierPromptWithoutExecutingTools(t *testing.T) {
	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	executeToolSubprocess = func(context.Context, string, string, json.RawMessage) ([]byte, error) {
		t.Fatal("prompts/get must return guidance text without executing tools")
		return nil, nil
	}

	list := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 1, Method: "prompts/list"})
	listData, _ := json.Marshal(list.Result)
	if !strings.Contains(string(listData), "prismgo-code-simplifier") {
		t.Fatalf("prompts/list missing PrismGo simplifier prompt: %s", listData)
	}

	get := handleRPC(context.Background(), "/host", rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "prompts/get",
		Params:  json.RawMessage(`{"name":"prismgo-code-simplifier"}`),
	})
	getData, _ := json.Marshal(get.Result)
	for _, want := range []string{"application-info", "search-docs", "保持行为不变"} {
		if !strings.Contains(string(getData), want) {
			t.Fatalf("prompts/get missing %q: %s", want, getData)
		}
	}

	missing := handleRPC(context.Background(), "/host", rpcRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "prompts/get",
		Params:  json.RawMessage(`{"name":"missing"}`),
	})
	if missing.Error == nil {
		t.Fatalf("missing prompt should return an error: %+v", missing)
	}
}

func TestHandleRPCExposesRunDiagnosticToolThroughSubprocessExecutor(t *testing.T) {
	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	executeToolSubprocess = func(_ context.Context, project string, name string, args json.RawMessage) ([]byte, error) {
		if project != "/host" || name != "run-diagnostic" || !strings.Contains(string(args), "current-config-summary") {
			t.Fatalf("unexpected diagnostic subprocess call: project=%s name=%s args=%s", project, name, args)
		}
		return []byte(`{"diagnostic":"current-config-summary","read_only":true}`), nil
	}

	list := handleRPC(context.Background(), "/host", rpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	listData, _ := json.Marshal(list.Result)
	if !strings.Contains(string(listData), "run-diagnostic") {
		t.Fatalf("tools/list missing run-diagnostic: %s", listData)
	}

	call := handleRPC(context.Background(), "/host", rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"run-diagnostic","arguments":{"name":"current-config-summary"}}`),
	})
	callData, _ := json.Marshal(call.Result)
	if call.Error != nil || !strings.Contains(string(callData), "current-config-summary") {
		t.Fatalf("tools/call should return diagnostic subprocess output: result=%s error=%v", callData, call.Error)
	}
}

func TestHandleRPCUsesProjectMCPPrimitiveFilters(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# agent\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	config := map[string]any{
		"version":      1,
		"project_root": root,
		"agents":       []string{"codex"},
		"features":     map[string]bool{"mcp": true},
		"mcp": map[string]any{
			"tools":     map[string][]string{"exclude": {"application-info"}},
			"resources": map[string][]string{"exclude": {"file://instructions/application-info.md"}},
			"prompts":   map[string][]string{"exclude": {"prismgo-code-simplifier"}},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prismgo-lens.json"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	listTools := handleRPC(context.Background(), root, rpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	toolsData, _ := json.Marshal(listTools.Result)
	if strings.Contains(string(toolsData), "application-info") {
		t.Fatalf("tools/list should honor excluded tool: %s", toolsData)
	}
	callTool := handleRPC(context.Background(), root, rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"application-info","arguments":{}}`),
	})
	if callTool.Error == nil {
		t.Fatalf("tools/call should reject excluded tool: %+v", callTool)
	}

	listResources := handleRPC(context.Background(), root, rpcRequest{JSONRPC: "2.0", ID: 3, Method: "resources/list"})
	resourcesData, _ := json.Marshal(listResources.Result)
	if strings.Contains(string(resourcesData), "application-info") {
		t.Fatalf("resources/list should honor excluded resource: %s", resourcesData)
	}
	readResource := handleRPC(context.Background(), root, rpcRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "resources/read",
		Params:  json.RawMessage(`{"uri":"file://instructions/application-info.md"}`),
	})
	if readResource.Error == nil {
		t.Fatalf("resources/read should reject excluded resource: %+v", readResource)
	}

	listPrompts := handleRPC(context.Background(), root, rpcRequest{JSONRPC: "2.0", ID: 5, Method: "prompts/list"})
	promptsData, _ := json.Marshal(listPrompts.Result)
	if strings.Contains(string(promptsData), "prismgo-code-simplifier") {
		t.Fatalf("prompts/list should honor excluded prompt: %s", promptsData)
	}
	getPrompt := handleRPC(context.Background(), root, rpcRequest{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "prompts/get",
		Params:  json.RawMessage(`{"name":"prismgo-code-simplifier"}`),
	})
	if getPrompt.Error == nil {
		t.Fatalf("prompts/get should reject excluded prompt: %+v", getPrompt)
	}
}

func TestHandleRPCUsesDefaultRegistriesWithoutProjectConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	for _, request := range []rpcRequest{
		{JSONRPC: "2.0", ID: 1, Method: "tools/list"},
		{JSONRPC: "2.0", ID: 2, Method: "resources/list"},
		{JSONRPC: "2.0", ID: 3, Method: "prompts/list"},
	} {
		response := handleRPC(context.Background(), root, request)
		if response.Error != nil {
			t.Fatalf("%s should use default registries without config: %+v", request.Method, response)
		}
		data, _ := json.Marshal(response.Result)
		if !strings.Contains(string(data), "application-info") && !strings.Contains(string(data), "prismgo-code-simplifier") {
			t.Fatalf("%s default result missing known primitive: %s", request.Method, data)
		}
	}
}

func TestHandleRPCReturnsConfigErrorsForInvalidMCPPrimitiveConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	config := map[string]any{
		"version":      1,
		"project_root": root,
		"agents":       []string{"codex"},
		"features":     map[string]bool{"mcp": true},
		"mcp": map[string]any{
			"prompts": map[string][]string{"include": {"missing-prompt"}},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prismgo-lens.json"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	for _, request := range []rpcRequest{
		{JSONRPC: "2.0", ID: 1, Method: "tools/list"},
		{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: json.RawMessage(`{"name":"application-info","arguments":{}}`)},
		{JSONRPC: "2.0", ID: 3, Method: "resources/list"},
		{JSONRPC: "2.0", ID: 4, Method: "resources/read", Params: json.RawMessage(`{"uri":"file://instructions/application-info.md"}`)},
		{JSONRPC: "2.0", ID: 5, Method: "prompts/list"},
		{JSONRPC: "2.0", ID: 6, Method: "prompts/get", Params: json.RawMessage(`{"name":"prismgo-code-simplifier"}`)},
	} {
		response := handleRPC(context.Background(), root, request)
		if response.Error == nil {
			t.Fatalf("%s should return config error: %+v", request.Method, response)
		}
		data, _ := json.Marshal(response.Error)
		if !strings.Contains(string(data), "unsupported MCP prompt") {
			t.Fatalf("%s error should mention invalid primitive config: %s", request.Method, data)
		}
	}

	if err := os.WriteFile(filepath.Join(root, ".prismgo-lens.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	response := handleRPC(context.Background(), root, rpcRequest{JSONRPC: "2.0", ID: 7, Method: "tools/list"})
	if response.Error == nil {
		t.Fatalf("corrupt config should return an error: %+v", response)
	}
}

func TestHandleRPCResourceAndPromptErrorPaths(t *testing.T) {
	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	executeToolSubprocess = func(context.Context, string, string, json.RawMessage) ([]byte, error) {
		return nil, errors.New("resource boom")
	}

	cases := []rpcRequest{
		{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: json.RawMessage(`{bad`)},
		{JSONRPC: "2.0", ID: 2, Method: "resources/read", Params: json.RawMessage(`{"uri":"file://missing.md"}`)},
		{JSONRPC: "2.0", ID: 3, Method: "resources/read", Params: json.RawMessage(`{"uri":"file://instructions/application-info.md"}`)},
		{JSONRPC: "2.0", ID: 4, Method: "prompts/get", Params: json.RawMessage(`{bad`)},
	}
	for _, request := range cases {
		response := handleRPC(context.Background(), "/host", request)
		if response.Error == nil {
			t.Fatalf("%s should return error for params %s: %+v", request.Method, request.Params, response)
		}
	}
}

func TestRunCoversTopLevelHelpAndErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	badConfigRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(badConfigRoot, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write bad go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badConfigRoot, ".prismgo-lens.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	cases := [][]string{
		{"prismgo-lens"},
		{"prismgo-lens", "--project", filepath.Join(root, "missing"), "install"},
		{"prismgo-lens", "--project", badConfigRoot, "update"},
		{"prismgo-lens", "--project", filepath.Join(root, "missing"), "list-skills"},
		{"prismgo-lens", "unknown"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		_ = Run(args, strings.NewReader(""), &stdout, &stderr)
		if stdout.Len() == 0 && stderr.Len() == 0 {
			t.Fatalf("%v should produce output", args)
		}
	}
}

func TestDefaultExecuteToolSubprocessRejectsUnknownToolBeforeExec(t *testing.T) {
	if _, err := defaultExecuteToolSubprocess(context.Background(), "/host", "missing-tool", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected allowlist error, got %v", err)
	}
}

func TestDefaultExecuteToolSubprocessUsesToolBudgetAndLimitsOutput(t *testing.T) {
	old := executablePath
	t.Cleanup(func() { executablePath = old })
	script := filepath.Join(t.TempDir(), "lens-helper.sh")
	body := `#!/bin/sh
case "$PRISMLENS_MODE" in
  sleep) sleep 2 ;;
  large) head -c 1048577 /dev/zero | tr '\000' x ;;
  fail) echo boom >&2; exit 7 ;;
  *) printf '{"ok":true}' ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	executablePath = func() (string, error) { return script, nil }

	data, err := defaultExecuteToolSubprocess(context.Background(), "/host", "application-info", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("subprocess success failed: data=%s err=%v", data, err)
	}

	t.Setenv("PRISMLENS_MODE", "fail")
	if _, err := defaultExecuteToolSubprocess(context.Background(), "/host", "application-info", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("subprocess failure should include stderr, got %v", err)
	}

	t.Setenv("PRISMLENS_MODE", "large")
	if _, err := defaultExecuteToolSubprocess(context.Background(), "/host", "application-info", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("large subprocess output should be rejected, got %v", err)
	}

	t.Setenv("PRISMLENS_MODE", "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := defaultExecuteToolSubprocess(ctx, "/host", "application-info", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("subprocess should respect caller/tool timeout, got %v", err)
	}
}

func TestRunExecuteToolAndBrowserProxyValidateArguments(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cases := [][]string{
		{"prismgo-lens", "--project", root, "execute-tool"},
		{"prismgo-lens", "--project", root, "execute-tool", "application-info", "not-base64"},
		{"prismgo-lens", "--project", root, "browser-proxy"},
		{"prismgo-lens", "--project", root, "browser-proxy", "--target"},
		{"prismgo-lens", "--project", root, "browser-proxy", "--listen"},
		{"prismgo-lens", "--project", root, "browser-proxy", "--bogus"},
		{"prismgo-lens", "--project", root, "browser-proxy", "--target", "://bad"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := Run(args, strings.NewReader(""), &stdout, &stderr); code == 0 {
			t.Fatalf("%v should fail validation: stdout=%s stderr=%s", args, stdout.String(), stderr.String())
		}
	}
}

func TestRunExecuteToolPrintsApplicationInfo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n\ngo 1.26.2\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "execute-tool", "application-info", "e30="}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("execute-tool exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"go_version"`) || !strings.Contains(stdout.String(), `"packages"`) {
		t.Fatalf("execute-tool output missing application info: %s", stdout.String())
	}
}

func TestRunAddSkillValidatesOptions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cases := [][]string{
		{"prismgo-lens", "--project", root, "add-skill"},
		{"prismgo-lens", "--project", root, "add-skill", "--skill"},
		{"prismgo-lens", "--project", root, "add-skill", "--unknown", "source"},
		{"prismgo-lens", "--project", filepath.Join(root, "missing"), "add-skill", "source"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := Run(args, strings.NewReader(""), &stdout, &stderr); code == 0 {
			t.Fatalf("%v should fail validation: stdout=%s stderr=%s", args, stdout.String(), stderr.String())
		}
	}
}

func TestShouldInjectBrowserLoggerChecksStatusHeadersAndContentType(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}}
	if !shouldInjectBrowserLogger(response) {
		t.Fatal("successful unencoded HTML should be injectable")
	}
	for name, response := range map[string]*http.Response{
		"redirect": {StatusCode: http.StatusFound, Header: http.Header{"Content-Type": []string{"text/html"}}},
		"encoded":  {StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}, "Content-Encoding": []string{"gzip"}}},
		"json":     {StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}},
	} {
		if shouldInjectBrowserLogger(response) {
			t.Fatalf("%s response should not be injectable", name)
		}
	}
}

func TestRunAddSkillInstallsAndListSkillsShowsSyncedSkill(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	source := filepath.Join(root, "cli-skill")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: cli-skill\ndescription: CLI skill fixture.\n---\n\n# CLI\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "add-skill", source}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add-skill exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"prismgo-lens", "--project", root, "list-skills"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list-skills exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cli-skill") {
		t.Fatalf("list-skills missing installed skill: %s", stdout.String())
	}
}

func TestRunInstallUpdateAndMCPMainLoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# agent\n"), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "install"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Project:", "Shared config:", ".prismgo-lens.json", "Local config:", ".prismgo-lens.local.json"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("install output missing %q: %s", want, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), ".prismgo-lens.json") {
		t.Fatalf("install output missing summary: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"prismgo-lens", "--project", root, "update"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update exit=%d stderr=%s", code, stderr.String())
	}

	old := executeToolSubprocess
	t.Cleanup(func() { executeToolSubprocess = old })
	executeToolSubprocess = func(context.Context, string, string, json.RawMessage) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	}
	stdout.Reset()
	stderr.Reset()
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"application-info","arguments":{}}}` + "\n")
	code = Run([]string{"prismgo-lens", "--project", root, "mcp"}, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "protocolVersion") || !strings.Contains(stdout.String(), `{\"ok\":true}`) {
		t.Fatalf("mcp output missing responses: %s", stdout.String())
	}
}

func TestRunMCPNotificationsDoNotWriteResponses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var stdout, stderr bytes.Buffer
	input := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	code := Run([]string{"prismgo-lens", "--project", root, "mcp"}, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp notification exit=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("mcp notifications must not write responses, got %q", stdout.String())
	}
}

func TestRunInstallDryRunAndFlagsDoNotWriteFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "install", "--dry-run", "--no-interaction", "--agent", "codex", "--guidelines", "--mcp", "--package-module", "example.com/prismgo/pkg"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry-run install exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Dry run: true", "Agents: codex", "Features: guidelines, mcp", "PATH fix: enabled", "MCP install plans:", "codex: strategy=file config=.codex/config.toml command=prismgo-lens --project . mcp", "Selected package modules: example.com/prismgo/pkg"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("dry-run output missing %q: %s", want, stdout.String())
		}
	}
	for _, path := range []string{".prismgo-lens.json", "AGENTS.md", ".codex/config.toml"} {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			t.Fatalf("dry-run should not write %s", path)
		}
	}
}

func TestInstallAcceptsNoFixPathAndMCPCommandMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"prismgo-lens", "--project", root, "install", "--no-interaction", "--no-fix-path", "--mcp-command", "name", "--agent", "codex", "--mcp"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Shared config: .prismgo-lens.json") {
		t.Fatalf("install output missing config summary:\n%s", stdout.String())
	}
}

func TestDoctorReportsProjectAndPathStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"prismgo-lens", "--project", root, "doctor"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Project:", "Command:", "PATH:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunInstallInteractiveWizardPersistsSelections(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	input := strings.NewReader("codex\nY\nn\nY\nn\nY\nY\nexample.com/prismgo/pkg\n")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "install", "--interactive"}, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("interactive install exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	config, err := lens.ReadConfig(root)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Join(config.Agents, ",") != "codex" {
		t.Fatalf("interactive agents not persisted: %+v", config.Agents)
	}
	if !config.Features.Guidelines || config.Features.Skills || !config.Features.MCP || config.Features.BrowserLogs || !config.Features.GitHubDocsProvider {
		t.Fatalf("interactive features not persisted: %+v", config.Features)
	}
	if !config.EnforceTests {
		t.Fatalf("interactive enforce_tests not persisted: %+v", config)
	}
	if strings.Join(config.SelectedPackageModules, ",") != "example.com/prismgo/pkg" {
		t.Fatalf("interactive package modules not persisted: %+v", config.SelectedPackageModules)
	}
	for _, want := range []string{"Prismgo Lens install wizard", "Detected agents:", "Detected package assets:", "Enforce tests", "Will write:", "Merge behavior:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("interactive wizard missing %q: %s", want, stdout.String())
		}
	}
}

func TestRunUpdateDryRunWithoutConfigUsesDefaultPreviewAndDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	dependency := filepath.Join(root, "deps", "pkg")
	if err := os.MkdirAll(filepath.Join(dependency, "resources", "prismgo-lens", "guidelines"), 0o755); err != nil {
		t.Fatalf("mkdir dependency guidelines: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "go.mod"), []byte("module example.com/prismgo/pkg\n"), 0o644); err != nil {
		t.Fatalf("write dependency go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "resources", "prismgo-lens", "guidelines", "pkg.md"), []byte("# Package\n"), 0o644); err != nil {
		t.Fatalf("write dependency guideline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(`module host

require example.com/prismgo/pkg v0.0.0

replace example.com/prismgo/pkg => ./deps/pkg
`), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "update", "--dry-run", "--discover", "--no-interaction"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update dry-run exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"Dry run: true", "Features: guidelines, skills, mcp, browser-logs", "Will write:", "Discovered package assets:", "example.com/prismgo/pkg", "No files written."} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("update dry-run without config missing %q: %s", want, stdout.String())
		}
	}
	if fileExists(filepath.Join(root, ".prismgo-lens.json")) {
		t.Fatalf("update --dry-run must not create shared config")
	}

	if err := os.WriteFile(filepath.Join(root, ".prismgo-lens.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"prismgo-lens", "--project", root, "update", "--dry-run", "--no-interaction"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("corrupt config dry-run should fail: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunInstallEnforceTestsFlagsPersistConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag string
		want bool
	}{
		{name: "enabled", flag: "--enforce-tests", want: true},
		{name: "disabled", flag: "--no-enforce-tests", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}
			var stdout, stderr bytes.Buffer
			code := Run([]string{"prismgo-lens", "--project", root, "install", "--no-interaction", "--agent", "codex", tc.flag}, strings.NewReader(""), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("install exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			config, err := lens.ReadConfig(root)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			if config.EnforceTests != tc.want {
				t.Fatalf("enforce_tests=%v, want %v", config.EnforceTests, tc.want)
			}
		})
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRunInstallInteractiveWizardOutputContainsExpectedSections(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# agent\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"prismgo-lens", "--project", root, "install", "--interactive", "--no-enforce-tests"}, strings.NewReader("\n\n\n\n\n\n\n\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("interactive install exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"Detected agents:", "Detected package assets:", "Enable guidelines", "Enforce tests", "Will write:", ".prismgo-lens.local.json", "Merge behavior:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("interactive output missing %q: %s", want, stdout.String())
		}
	}
}

func TestInstallAndUpdateFlagParsingCoversAutomationErrors(t *testing.T) {
	options, dryRun, interactive, err := parseInstallOptions([]string{"--dry-run", "--interactive", "--agent", "codex", "--package-module", "example.com/prismgo/pkg", "--skills", "--browser-logs", "--github-docs-provider", "--enforce-tests"})
	if err != nil {
		t.Fatalf("parse install options: %v", err)
	}
	if !dryRun || !interactive || strings.Join(options.Agents, ",") != "codex" || strings.Join(options.SelectedPackageModules, ",") != "example.com/prismgo/pkg" || !options.Features.Skills || !options.Features.BrowserLogs || !options.Features.GitHubDocsProvider || options.EnforceTests == nil || !*options.EnforceTests {
		t.Fatalf("unexpected parsed install options: dryRun=%v interactive=%v options=%+v", dryRun, interactive, options)
	}
	if _, _, _, err := parseInstallOptions([]string{"--agent"}); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("missing --agent value should fail, got %v", err)
	}
	if _, _, _, err := parseInstallOptions([]string{"--package-module"}); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("missing --package-module value should fail, got %v", err)
	}
	if _, _, _, err := parseInstallOptions([]string{"--bogus"}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown install option should fail, got %v", err)
	}

	root := t.TempDir()
	dependency := filepath.Join(root, "deps", "pkg")
	if err := os.MkdirAll(filepath.Join(dependency, "resources", "prismgo-lens", "skills", "pkg-debug"), 0o755); err != nil {
		t.Fatalf("mkdir dependency skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "go.mod"), []byte("module example.com/prismgo/pkg\n"), 0o644); err != nil {
		t.Fatalf("write dependency go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "resources", "prismgo-lens", "skills", "pkg-debug", "SKILL.md"), []byte("# Pkg Debug\n"), 0o644); err != nil {
		t.Fatalf("write dependency skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(`module host

require example.com/prismgo/pkg v0.0.0

replace example.com/prismgo/pkg => ./deps/pkg
`), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	config := map[string]any{
		"version":      1,
		"project_root": root,
		"agents":       []string{"codex"},
		"features":     map[string]bool{"guidelines": true, "skills": true, "mcp": true},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".prismgo-lens.json"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"prismgo-lens", "--project", root, "update", "--dry-run", "--ignore-skills", "--discover", "--no-interaction"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update dry-run exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Dry run: true") || !strings.Contains(stdout.String(), "Features: guidelines, mcp") || !strings.Contains(stdout.String(), "pkg-debug") {
		t.Fatalf("update dry-run output should reflect ignored skills: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"prismgo-lens", "--project", root, "update", "--ignore-skills", "--no-interaction"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update ignore-skills exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "prismgo-debug", "SKILL.md")); err == nil {
		t.Fatalf("update --ignore-skills should not sync skills, stdout=%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"prismgo-lens", "--project", root, "update", "--bogus"}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unknown update option") {
		t.Fatalf("unknown update option should fail: code=%d stderr=%s", code, stderr.String())
	}
}

func TestFeatureListAndInstallDryRunNoAgentBranch(t *testing.T) {
	if featureList(lens.Features{}) != "none" {
		t.Fatalf("empty features should render none")
	}
	all := featureList(lens.Features{Guidelines: true, Skills: true, MCP: true, BrowserLogs: true, GitHubDocsProvider: true})
	for _, want := range []string{"guidelines", "skills", "mcp", "browser-logs", "github-docs-provider"} {
		if !strings.Contains(all, want) {
			t.Fatalf("feature list missing %q: %s", want, all)
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := printInstallDryRun(root, lens.InstallOptions{Features: lens.Features{}}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Agents: none") || !strings.Contains(stdout.String(), "Features: none") {
		t.Fatalf("dry-run no-agent branch failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestBrowserProxyInjectsHTMLLeavesJSONAndStoresBrowserLogs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<html><body><h1>ok</h1></body></html>")
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()

	proxy := httptest.NewServer(browserProxyHandler(root, target.URL))
	defer proxy.Close()

	html, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatalf("get html through proxy: %v", err)
	}
	defer closeTestResource(t, "html response body", html.Body.Close)
	htmlBody, _ := io.ReadAll(html.Body)
	if !strings.Contains(string(htmlBody), "/_prismgo_lens/browser-logs") {
		t.Fatalf("html response should contain injected logger: %s", htmlBody)
	}

	jsonResponse, err := http.Get(proxy.URL + "/api")
	if err != nil {
		t.Fatalf("get json through proxy: %v", err)
	}
	defer closeTestResource(t, "json response body", jsonResponse.Body.Close)
	jsonBody, _ := io.ReadAll(jsonResponse.Body)
	if string(jsonBody) != `{"ok":true}` {
		t.Fatalf("json response should be unchanged: %s", jsonBody)
	}

	logResponse, err := http.Post(proxy.URL+"/_prismgo_lens/browser-logs", "application/json", strings.NewReader(`{"level":"error","args":["boom"]}`))
	if err != nil {
		t.Fatalf("post browser log: %v", err)
	}
	defer closeTestResource(t, "log response body", logResponse.Body.Close)
	if logResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("browser log status = %d", logResponse.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(root, "storage", "logs", "browser.log"))
	if err != nil {
		t.Fatalf("read browser log: %v", err)
	}
	if !strings.Contains(string(data), "boom") {
		t.Fatalf("browser log was not persisted: %s", data)
	}
}

// closeTestResource reports cleanup failures from deferred test resources.
func closeTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Fatalf("close %s: %v", label, err)
	}
}

func TestBrowserProxyDoesNotBufferNonHTMLOrEncodedResponses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module host\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Content-Encoding", "identity")
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "data: ok\n\n")
		case "/encoded":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = io.WriteString(w, "compressed")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0, 1, 2, 3})
		}
	}))
	defer target.Close()
	proxy := httptest.NewServer(browserProxyHandler(root, target.URL))
	defer proxy.Close()

	for _, path := range []string{"/stream", "/encoded", "/binary"} {
		response, err := http.Get(proxy.URL + path)
		if err != nil {
			t.Fatalf("get %s through proxy: %v", path, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if strings.Contains(string(body), "/_prismgo_lens/browser-logs") {
			t.Fatalf("%s response should not be injected: %q", path, body)
		}
	}
}
