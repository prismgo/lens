package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/prismgo/lens/internal/lens"
)

const maxToolOutputBytes = 1 << 20

var executeToolSubprocess = defaultExecuteToolSubprocess
var executablePath = os.Executable

// writeLine writes CLI output and panics on writer failures because most CLI
// print helpers do not have an error return channel.
func writeLine(w io.Writer, args ...any) {
	if _, err := fmt.Fprintln(w, args...); err != nil {
		panic(err)
	}
}

// writeFormat writes formatted CLI output and panics on writer failures because
// callers already encode command success or failure as exit codes.
func writeFormat(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		panic(err)
	}
}

// Run 执行 Prismgo Lens CLI。
func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"prismgo-lens"}
	}
	project := "."
	rest := args[1:]
	if len(rest) >= 2 && rest[0] == "--project" {
		project = rest[1]
		rest = rest[2:]
	}
	if len(rest) == 0 || rest[0] == "--help" || rest[0] == "-h" {
		printHelp(stdout)
		return 0
	}
	switch rest[0] {
	case "install":
		return runInstall(project, rest[1:], stdin, stdout, stderr)
	case "update":
		return runUpdate(project, rest[1:], stdout, stderr)
	case "doctor":
		return runDoctor(project, stdout, stderr)
	case "mcp":
		return runMCP(project, stdin, stdout, stderr)
	case "execute-tool":
		return runExecuteTool(project, rest[1:], stdout, stderr)
	case "browser-proxy":
		return runBrowserProxy(project, rest[1:], stdout, stderr)
	case "list-skills":
		return runListSkills(project, stdout, stderr)
	case "add-skill":
		return runAddSkill(project, rest[1:], stdout, stderr)
	default:
		writeFormat(stderr, "unknown command: %s\n", rest[0])
		printHelp(stderr)
		return 2
	}
}

func printHelp(w io.Writer) {
	writeLine(w, "Prismgo Lens development tooling")
	writeLine(w)
	writeLine(w, "Usage:")
	writeLine(w, "  prismgolens [--project PATH] <command>")
	writeLine(w)
	writeLine(w, "Commands:")
	writeLine(w, "  install        install guidelines, skills, and MCP config")
	writeLine(w, "  update         resync previously installed Lens files")
	writeLine(w, "  doctor         check Prismgo Lens installation and Agent MCP config")
	writeLine(w, "  mcp            start stdio MCP JSON-RPC server")
	writeLine(w, "  execute-tool   execute a registered tool with base64 JSON args")
	writeLine(w, "  browser-proxy  start a dev-only reverse proxy that captures browser logs")
	writeLine(w, "  add-skill      audit and add a local skill directory")
	writeLine(w, "  list-skills    list local .ai skills")
}

func runInstall(project string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	options, dryRun, interactive, err := parseInstallOptions(args)
	if err != nil {
		writeLine(stderr, err)
		return 2
	}
	if dryRun {
		return printInstallDryRun(project, options, stdout, stderr)
	}
	if interactive || isInteractiveInput(stdin) {
		var err error
		options, err = promptInstallOptions(project, options, stdin, stdout)
		if err != nil {
			writeLine(stderr, err)
			return 1
		}
	}
	result, err := lens.Install(project, options)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	printInstallResult(stdout, result)
	return 0
}

func runUpdate(project string, args []string, stdout io.Writer, stderr io.Writer) int {
	dryRun := false
	ignoreSkills := false
	discover := false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--ignore-skills":
			ignoreSkills = true
		case "--discover":
			discover = true
		case "--no-interaction":
		default:
			writeFormat(stderr, "unknown update option: %s\n", arg)
			return 2
		}
	}
	if dryRun {
		root, err := lens.DetectProject(project)
		if err != nil {
			writeLine(stderr, err)
			return 1
		}
		config, err := lens.ReadConfig(root.Root)
		if err != nil {
			if !os.IsNotExist(err) {
				writeLine(stderr, err)
				return 1
			}
			config, err = lens.DefaultProjectConfig(root.Root, lens.InstallOptions{Features: lens.DefaultFeatures()})
			if err != nil {
				writeLine(stderr, err)
				return 1
			}
		}
		if err := lens.ValidateConfig(root.Root, config); err != nil {
			writeLine(stderr, err)
			return 1
		}
		if ignoreSkills {
			config.Features.Skills = false
		}
		writeLine(stdout, "Dry run: true")
		writeFormat(stdout, "Project: %s\n", root.Root)
		printAgents(stdout, config.Agents)
		writeFormat(stdout, "Features: %s\n", featureList(config.Features))
		writeFormat(stdout, "Enforce tests: %t\n", config.EnforceTests)
		printInstallWritePlan(stdout, root.Root, lens.InstallOptions{Agents: config.Agents, Features: config.Features, SelectedPackageModules: config.SelectedPackageModules, EnforceTests: &config.EnforceTests})
		if discover && !printDiscoveredPackageAssets(root.Root, stdout, stderr) {
			return 1
		}
		writeLine(stdout, "No files written.")
		return 0
	}
	result, err := lens.UpdateWithOptions(project, lens.UpdateOptions{IgnoreSkills: ignoreSkills})
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	printInstallResult(stdout, result)
	if discover && !printDiscoveredPackageAssets(result.ProjectRoot, stdout, stderr) {
		return 1
	}
	return 0
}

func parseInstallOptions(args []string) (lens.InstallOptions, bool, bool, error) {
	options := lens.InstallOptions{Features: lens.DefaultFeatures()}
	featureFlagsSeen := false
	dryRun := false
	interactive := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--interactive":
			interactive = true
		case "--no-interaction":
		case "--no-fix-path":
			options.NoFixPath = true
		case "--mcp-command":
			if i+1 >= len(args) {
				return options, dryRun, interactive, errors.New("--mcp-command requires auto, name, or absolute")
			}
			value := args[i+1]
			if value != lens.MCPCommandAuto && value != lens.MCPCommandName && value != lens.MCPCommandAbsolute {
				return options, dryRun, interactive, fmt.Errorf("unsupported --mcp-command value: %s", value)
			}
			options.MCPCommandMode = value
			i++
		case "--agent":
			if i+1 >= len(args) {
				return options, dryRun, interactive, errors.New("--agent requires a name")
			}
			options.Agents = append(options.Agents, args[i+1])
			i++
		case "--package-module":
			if i+1 >= len(args) {
				return options, dryRun, interactive, errors.New("--package-module requires a module path")
			}
			options.SelectedPackageModules = append(options.SelectedPackageModules, args[i+1])
			i++
		case "--enforce-tests":
			value := true
			options.EnforceTests = &value
		case "--no-enforce-tests":
			value := false
			options.EnforceTests = &value
		case "--guidelines", "--skills", "--mcp", "--browser-logs", "--github-docs-provider":
			if !featureFlagsSeen {
				options.Features = lens.Features{}
				featureFlagsSeen = true
			}
			switch args[i] {
			case "--guidelines":
				options.Features.Guidelines = true
			case "--skills":
				options.Features.Skills = true
			case "--mcp":
				options.Features.MCP = true
			case "--browser-logs":
				options.Features.BrowserLogs = true
			case "--github-docs-provider":
				options.Features.GitHubDocsProvider = true
			}
		default:
			return options, dryRun, interactive, fmt.Errorf("unknown install option: %s", args[i])
		}
	}
	return options, dryRun, interactive, nil
}

func promptInstallOptions(project string, options lens.InstallOptions, stdin io.Reader, stdout io.Writer) (lens.InstallOptions, error) {
	root, err := lens.DetectProject(project)
	if err != nil {
		return options, err
	}
	detected := lens.DetectAgents(root.Root)
	defaultAgents := options.Agents
	if len(defaultAgents) == 0 {
		for _, agent := range detected {
			defaultAgents = append(defaultAgents, agent.Name)
		}
	}
	writeLine(stdout, "Prismgo Lens install wizard")
	writeFormat(stdout, "Project: %s\n", root.Root)
	writeLine(stdout, "Dev-only: Prismgo Lens writes Agent and MCP files but is not imported by production builds.")
	if len(detected) == 0 {
		writeLine(stdout, "Detected agents: none")
	} else {
		writeFormat(stdout, "Detected agents: %s\n", strings.Join(agentNamesFromDetected(detected), ", "))
	}
	assets, assetErr := lens.DiscoverPackageAssets(root.Root)
	if assetErr != nil {
		writeFormat(stdout, "Detected package assets: unavailable (%s)\n", assetErr)
	} else if len(assets) == 0 {
		writeLine(stdout, "Detected package assets: none")
	} else {
		writeLine(stdout, "Detected package assets:")
		for _, asset := range assets {
			writeFormat(stdout, "  - %s %s %s\n", asset.Module, asset.Type, asset.Name)
		}
	}
	reader := bufio.NewReader(stdin)
	options.Agents = promptCSV(reader, stdout, "Agents", defaultAgents)
	options.Features.Guidelines = promptBool(reader, stdout, "Enable guidelines", options.Features.Guidelines)
	options.Features.Skills = promptBool(reader, stdout, "Enable skills", options.Features.Skills)
	options.Features.MCP = promptBool(reader, stdout, "Enable MCP", options.Features.MCP)
	options.Features.BrowserLogs = promptBool(reader, stdout, "Enable browser logs", options.Features.BrowserLogs)
	options.Features.GitHubDocsProvider = promptBool(reader, stdout, "Enable GitHub docs provider", options.Features.GitHubDocsProvider)
	enforceFallback := lens.DetectTestSetup(root.Root)
	if options.EnforceTests != nil {
		enforceFallback = *options.EnforceTests
	}
	enforceTests := promptBool(reader, stdout, "Enforce tests", enforceFallback)
	options.EnforceTests = &enforceTests
	moduleDefaults := options.SelectedPackageModules
	if len(moduleDefaults) == 0 {
		moduleDefaults = []string{}
	}
	options.SelectedPackageModules = promptCSV(reader, stdout, "Selected package modules (default none)", moduleDefaults)
	writeFormat(stdout, "Will write features: %s\n", featureList(options.Features))
	printInstallWritePlan(stdout, root.Root, options)
	return options, nil
}

func promptCSV(reader *bufio.Reader, stdout io.Writer, label string, defaults []string) []string {
	defaultText := strings.Join(defaults, ",")
	if defaultText == "" {
		defaultText = "none"
	}
	writeFormat(stdout, "%s [%s]: ", label, defaultText)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaults
	}
	if strings.EqualFold(line, "none") {
		return nil
	}
	values := []string{}
	for _, part := range strings.Split(line, ",") {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func promptBool(reader *bufio.Reader, stdout io.Writer, label string, fallback bool) bool {
	suffix := "y/N"
	if fallback {
		suffix = "Y/n"
	}
	writeFormat(stdout, "%s [%s]: ", label, suffix)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return fallback
	}
	return line == "y" || line == "yes" || line == "true" || line == "1"
}

func agentNamesFromDetected(agents []lens.Agent) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return names
}

func isInteractiveInput(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func printInstallDryRun(project string, options lens.InstallOptions, stdout io.Writer, stderr io.Writer) int {
	root, err := lens.DetectProject(project)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	agents := options.Agents
	if len(agents) == 0 {
		for _, agent := range lens.DetectAgents(root.Root) {
			agents = append(agents, agent.Name)
		}
	}
	writeLine(stdout, "Dry run: true")
	writeFormat(stdout, "Project: %s\n", root.Root)
	if len(agents) == 0 {
		writeLine(stdout, "Agents: none")
	} else {
		printAgents(stdout, agents)
	}
	writeFormat(stdout, "Features: %s\n", featureList(options.Features))
	if options.NoFixPath {
		writeLine(stdout, "PATH fix: disabled")
	} else {
		writeLine(stdout, "PATH fix: enabled")
	}
	if options.MCPCommandMode != "" {
		writeFormat(stdout, "MCP command mode: %s\n", options.MCPCommandMode)
	}
	if options.EnforceTests != nil {
		writeFormat(stdout, "Enforce tests: %t\n", *options.EnforceTests)
	}
	printInstallWritePlan(stdout, root.Root, lens.InstallOptions{Agents: agents, Features: options.Features, SelectedPackageModules: options.SelectedPackageModules, EnforceTests: options.EnforceTests})
	if options.Features.MCP {
		printMCPInstallPlans(stdout, lens.AgentMCPInstallPlans(agents))
	}
	if len(options.SelectedPackageModules) > 0 {
		writeFormat(stdout, "Selected package modules: %s\n", strings.Join(options.SelectedPackageModules, ", "))
	}
	writeLine(stdout, "Dev-only: Prismgo Lens is an independent tool module and is not imported by production builds.")
	writeLine(stdout, "No files written.")
	return 0
}

func runDoctor(project string, stdout io.Writer, stderr io.Writer) int {
	report, err := lens.Doctor(project)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	writeFormat(stdout, "Project: %s\n", report.ProjectRoot)
	writeFormat(stdout, "Command: %s\n", report.Command)
	writeFormat(stdout, "PATH: %s\n", report.PathStatus)
	for _, warning := range report.Warnings {
		writeFormat(stdout, "Warning: %s\n", warning)
	}
	return 0
}

func printAgents(stdout io.Writer, agents []string) {
	if len(agents) == 0 {
		writeLine(stdout, "Agents: none")
		return
	}
	writeFormat(stdout, "Agents: %s\n", strings.Join(agents, ", "))
}

func printInstallWritePlan(stdout io.Writer, root string, options lens.InstallOptions) {
	writeLine(stdout, "Will write:")
	writeLine(stdout, "  - .prismgo-lens.json")
	writeLine(stdout, "  - .prismgo-lens.local.json")
	if options.Features.Guidelines {
		writeLine(stdout, "  - .ai guidelines")
	}
	if options.Features.Skills {
		writeLine(stdout, "  - .ai/skills and selected Agent skills directories")
	}
	if options.Features.MCP {
		writeLine(stdout, "  - Agent MCP config")
	}
	if len(options.SelectedPackageModules) > 0 {
		writeFormat(stdout, "  - package asset targets for %s\n", strings.Join(options.SelectedPackageModules, ", "))
	}
	writeFormat(stdout, "Merge behavior: managed Prismgo Lens blocks are replaced; existing project .ai files at the same path override built-ins under %s.\n", root)
}

func printMCPInstallPlans(stdout io.Writer, plans []lens.AgentMCPInstallPlan) {
	if len(plans) == 0 {
		return
	}
	writeLine(stdout, "MCP install plans:")
	for _, plan := range plans {
		writeFormat(stdout, "  - %s: strategy=%s config=%s command=%s %s\n", plan.Agent, plan.Strategy, plan.ConfigPath, plan.Command, strings.Join(plan.Args, " "))
	}
}

func featureList(features lens.Features) string {
	names := []string{}
	if features.Guidelines {
		names = append(names, "guidelines")
	}
	if features.Skills {
		names = append(names, "skills")
	}
	if features.MCP {
		names = append(names, "mcp")
	}
	if features.BrowserLogs {
		names = append(names, "browser-logs")
	}
	if features.GitHubDocsProvider {
		names = append(names, "github-docs-provider")
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func printInstallResult(w io.Writer, result lens.InstallResult) {
	writeFormat(w, "Project: %s\n", result.ProjectRoot)
	if len(result.Detected) == 0 {
		writeLine(w, "Agents: none detected; rerun install after creating an Agent config")
	} else {
		names := make([]string, 0, len(result.Detected))
		for _, agent := range result.Detected {
			names = append(names, agent.Name)
		}
		writeFormat(w, "Agents: %s\n", strings.Join(names, ", "))
	}
	writeLine(w, "Dev-only: Prismgo Lens is an independent tool module and is not imported by production builds.")
	writeLine(w, "Shared config: .prismgo-lens.json")
	writeLine(w, "Local config: .prismgo-lens.local.json")
	writeLine(w, "Written:")
	for _, file := range result.WrittenFiles {
		writeFormat(w, "  - %s\n", file)
	}
}

func printDiscoveredPackageAssets(root string, stdout io.Writer, stderr io.Writer) bool {
	assets, err := lens.DiscoverPackageAssets(root)
	if err != nil {
		writeLine(stderr, err)
		return false
	}
	if len(assets) == 0 {
		writeLine(stdout, "Discovered package assets: none")
		return true
	}
	writeLine(stdout, "Discovered package assets:")
	for _, asset := range assets {
		writeFormat(stdout, "  - %s %s %s %s\n", asset.Module, asset.Type, asset.Name, asset.Path)
	}
	writeLine(stdout, "Package assets are listed only; enable or install them explicitly after review.")
	return true
}

func runExecuteTool(project string, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		writeLine(stderr, "execute-tool requires a tool name")
		return 2
	}
	encoded := ""
	if len(args) >= 2 {
		encoded = args[1]
	}
	decoded, err := lens.DecodeToolArguments(encoded)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	data, err := lens.ExecuteTool(project, args[0], decoded)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	writeLine(stdout, string(data))
	return 0
}

func runListSkills(project string, stdout io.Writer, stderr io.Writer) int {
	root, err := lens.DetectProject(project)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	skills, err := lens.ListSkills(root.Root)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	for _, skill := range skills {
		writeLine(stdout, skill)
	}
	return 0
}

func runAddSkill(project string, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		writeLine(stderr, "add-skill requires a local directory or GitHub source")
		return 2
	}
	options := lens.AddSkillOptions{}
	source := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--list":
			options.ListOnly = true
		case "--all":
			options.All = true
		case "--force":
			options.Force = true
		case "--skip-audit":
			options.SkipAudit = true
		case "--skill":
			if i+1 >= len(args) {
				writeLine(stderr, "--skill requires a name")
				return 2
			}
			options.Skill = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "--") {
				writeFormat(stderr, "unknown add-skill option: %s\n", args[i])
				return 2
			}
			source = args[i]
		}
	}
	if source == "" {
		writeLine(stderr, "add-skill requires a source")
		return 2
	}
	root, err := lens.DetectProject(project)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	reports, candidates, err := lens.AddSkill(root.Root, source, options)
	if err != nil {
		if len(candidates) > 0 {
			writeLine(stderr, "candidate skills:")
			for _, candidate := range candidates {
				writeFormat(stderr, "  - %s\n", candidate)
			}
		}
		writeLine(stderr, err)
		return 1
	}
	if options.ListOnly {
		for _, candidate := range candidates {
			writeLine(stdout, candidate)
		}
		return 0
	}
	data, _ := json.MarshalIndent(map[string]any{"installed": candidates, "reports": reports}, "", "  ")
	writeLine(stdout, string(data))
	return 0
}

func runBrowserProxy(project string, args []string, stdout io.Writer, stderr io.Writer) int {
	target := ""
	listen := "127.0.0.1:8052"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 >= len(args) {
				writeLine(stderr, "--target requires a URL")
				return 2
			}
			target = args[i+1]
			i++
		case "--listen":
			if i+1 >= len(args) {
				writeLine(stderr, "--listen requires an address")
				return 2
			}
			listen = args[i+1]
			i++
		default:
			writeFormat(stderr, "unknown browser-proxy option: %s\n", args[i])
			return 2
		}
	}
	if target == "" {
		writeLine(stderr, "browser-proxy requires --target")
		return 2
	}
	root, err := lens.DetectProject(project)
	if err != nil {
		writeLine(stderr, err)
		return 1
	}
	handler, err := newBrowserProxyHandler(root.Root, target)
	if err != nil {
		writeLine(stderr, err)
		return 2
	}
	writeFormat(stdout, "Prismgo Lens browser proxy listening on http://%s -> %s\n", listen, target)
	if err := http.ListenAndServe(listen, handler); err != nil {
		writeLine(stderr, err)
		return 1
	}
	return 0
}

func browserProxyHandler(root string, target string) http.Handler {
	handler, err := newBrowserProxyHandler(root, target)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, err.Error(), http.StatusBadGateway)
		})
	}
	return handler
}

func newBrowserProxyHandler(root string, target string) (http.Handler, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("browser-proxy target must include scheme and host")
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(parsed)
			request.Out.Host = parsed.Host
		},
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if !shouldInjectBrowserLogger(response) {
			return nil
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		body = lens.InjectBrowserLogger(response.StatusCode, response.Header, body)
		response.Body = io.NopCloser(strings.NewReader(string(body)))
		response.ContentLength = int64(len(body))
		response.Header.Set("Content-Length", fmt.Sprint(len(body)))
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/_prismgo_lens/browser-logs", lens.BrowserLogHandler(root))
	mux.Handle("/", proxy)
	return mux, nil
}

func shouldInjectBrowserLogger(response *http.Response) bool {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	if response.Header.Get("Content-Encoding") != "" {
		return false
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	return strings.Contains(contentType, "text/html")
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

func runMCP(project string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		var request rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			writeRPC(stdout, rpcResponse{JSONRPC: "2.0", Error: map[string]any{"code": -32700, "message": err.Error()}})
			continue
		}
		response := handleRPC(context.Background(), project, request)
		if response != nil {
			writeRPC(stdout, *response)
		}
	}
	if err := scanner.Err(); err != nil {
		writeLine(stderr, err)
		return 1
	}
	return 0
}

func handleRPC(ctx context.Context, project string, request rpcRequest) *rpcResponse {
	if request.ID == nil {
		// JSON-RPC notification 没有 id，协议要求服务端不能写 response。
		// MCP 客户端会发送 notifications/initialized，这里统一按只读 no-op 处理。
		return nil
	}
	response := &rpcResponse{JSONRPC: "2.0", ID: request.ID}
	registries, registryErr := mcpRegistriesForProject(project)
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]string{"name": "prismgo-lens", "version": "0.1.0"}, "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{}}}
	case "tools/list":
		if registryErr != nil {
			response.Error = map[string]any{"code": -32000, "message": registryErr.Error()}
			return response
		}
		response.Result = map[string]any{"tools": registries.primitives.ToolRegistry().List()}
	case "tools/call":
		if registryErr != nil {
			response.Error = map[string]any{"code": -32000, "message": registryErr.Error()}
			return response
		}
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = map[string]any{"code": -32602, "message": err.Error()}
			return response
		}
		if _, ok := registries.primitives.ToolRegistry().Lookup(params.Name); !ok {
			response.Error = map[string]any{"code": -32000, "message": fmt.Sprintf("prismgo-lens: tool %q is not registered", params.Name)}
			return response
		}
		data, err := executeToolSubprocess(ctx, project, params.Name, params.Arguments)
		if err != nil {
			response.Error = map[string]any{"code": -32000, "message": err.Error()}
			return response
		}
		response.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": string(data)}}}
	case "resources/list":
		if registryErr != nil {
			response.Error = map[string]any{"code": -32000, "message": registryErr.Error()}
			return response
		}
		response.Result = map[string]any{"resources": registries.primitives.ResourceRegistry().List()}
	case "resources/read":
		if registryErr != nil {
			response.Error = map[string]any{"code": -32000, "message": registryErr.Error()}
			return response
		}
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = map[string]any{"code": -32602, "message": err.Error()}
			return response
		}
		resource, ok := registries.primitives.ResourceRegistry().Lookup(params.URI)
		if !ok {
			response.Error = map[string]any{"code": -32000, "message": fmt.Sprintf("prismgo-lens: resource %q is not registered", params.URI)}
			return response
		}
		data, err := executeToolSubprocess(ctx, project, resource.ToolName, json.RawMessage(`{}`))
		if err != nil {
			response.Error = map[string]any{"code": -32000, "message": err.Error()}
			return response
		}
		// 资源复用 application-info tool 的隔离执行结果，只负责转换为 MCP resource 内容。
		text := "```json\n" + strings.TrimSpace(string(data)) + "\n```"
		response.Result = map[string]any{"contents": []map[string]string{{
			"uri":      resource.URI,
			"mimeType": resource.MIMEType,
			"text":     text,
		}}}
	case "prompts/list":
		if registryErr != nil {
			response.Error = map[string]any{"code": -32000, "message": registryErr.Error()}
			return response
		}
		response.Result = map[string]any{"prompts": registries.primitives.PromptRegistry().List()}
	case "prompts/get":
		if registryErr != nil {
			response.Error = map[string]any{"code": -32000, "message": registryErr.Error()}
			return response
		}
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = map[string]any{"code": -32602, "message": err.Error()}
			return response
		}
		prompt, ok := registries.primitives.PromptRegistry().Lookup(params.Name)
		if !ok {
			response.Error = map[string]any{"code": -32000, "message": fmt.Sprintf("prismgo-lens: prompt %q is not registered", params.Name)}
			return response
		}
		text, err := lens.ReadPrompt(prompt)
		if err != nil {
			response.Error = map[string]any{"code": -32000, "message": err.Error()}
			return response
		}
		// Prompt 只返回指导文本，不编排工具，也不修改项目文件。
		response.Result = map[string]any{"description": prompt.Description, "messages": []map[string]any{{
			"role": "user",
			"content": map[string]string{
				"type": "text",
				"text": text,
			},
		}}}
	default:
		response.Error = map[string]any{"code": -32601, "message": "method not found"}
	}
	return response
}

type mcpRegistries struct {
	primitives lens.PrimitiveRegistry
}

func mcpRegistriesForProject(project string) (mcpRegistries, error) {
	registries := mcpRegistries{primitives: lens.DefaultPrimitiveRegistry()}
	root, err := lens.DetectProject(project)
	if err != nil {
		return registries, nil
	}
	config, err := lens.ReadConfig(root.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return registries, nil
		}
		return registries, err
	}
	if err := lens.ValidateConfig(root.Root, config); err != nil {
		return registries, err
	}
	registries.primitives = registries.primitives.Filter(config.MCP)
	return registries, nil
}

func defaultExecuteToolSubprocess(ctx context.Context, project string, name string, args json.RawMessage) ([]byte, error) {
	tool, ok := lens.DefaultToolRegistry().Lookup(name)
	if !ok {
		return nil, fmt.Errorf("prismgo-lens: tool %q is not registered", name)
	}
	timeout := time.Duration(tool.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executable, err := executablePath()
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(args)
	cmd := exec.CommandContext(ctx, executable, "--project", project, "execute-tool", name, encoded)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errors.New("prismgo-lens: tool subprocess timed out")
	}
	if len(output) > maxToolOutputBytes {
		return nil, fmt.Errorf("prismgo-lens: tool subprocess output exceeded %d bytes", maxToolOutputBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func writeRPC(w io.Writer, response rpcResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		panic(err)
	}
	writeLine(w, string(data))
}
