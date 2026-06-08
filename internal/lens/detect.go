package lens

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Project 描述被 Lens 探测到的 PrismGo 项目。
type Project struct {
	Root string
}

// Agent 描述 Lens 第一版支持的 Agent adapter。
type Agent struct {
	Name           string
	DisplayName    string
	GuidelinesPath string
	SkillsPath     string
	MCPPath        string
	MCPConfigKey   string
	MCPStrategy    string
	ShellCommand   string
	ShellArgs      []string
}

var agentRegistry = map[string]Agent{
	"codex":       {Name: "codex", DisplayName: "Codex", GuidelinesPath: "AGENTS.md", SkillsPath: ".agents/skills", MCPPath: ".codex/config.toml", MCPConfigKey: "mcpServers", MCPStrategy: "file", ShellCommand: "codex", ShellArgs: []string{"mcp", "add", "prismgo-lens", "--"}},
	"claude_code": {Name: "claude_code", DisplayName: "Claude Code", GuidelinesPath: "CLAUDE.md", SkillsPath: ".claude/skills", MCPPath: ".mcp.json", MCPConfigKey: "mcpServers", MCPStrategy: "file", ShellCommand: "claude", ShellArgs: []string{"mcp", "add", "prismgo-lens", "--"}},
	"cursor":      {Name: "cursor", DisplayName: "Cursor", GuidelinesPath: ".cursor/rules/prismgo-lens.md", SkillsPath: ".cursor/skills", MCPPath: ".cursor/mcp.json", MCPConfigKey: "mcpServers", MCPStrategy: "file"},
	"copilot":     {Name: "copilot", DisplayName: "GitHub Copilot", GuidelinesPath: ".github/copilot-instructions.md", SkillsPath: ".github/skills", MCPPath: ".vscode/mcp.json", MCPConfigKey: "servers", MCPStrategy: "file"},
	"opencode":    {Name: "opencode", DisplayName: "OpenCode", GuidelinesPath: "AGENTS.md", SkillsPath: ".opencode/skills", MCPPath: ".opencode.json", MCPConfigKey: "mcpServers", MCPStrategy: "file"},
	"kiro":        {Name: "kiro", DisplayName: "Kiro", GuidelinesPath: ".kiro/steering/prismgo-lens.md", SkillsPath: ".kiro/skills", MCPPath: ".kiro/settings/mcp.json", MCPConfigKey: "mcpServers", MCPStrategy: "file"},
	"junie":       {Name: "junie", DisplayName: "Junie", GuidelinesPath: ".junie/guidelines.md", SkillsPath: ".junie/skills", MCPPath: ".junie/mcp.json", MCPConfigKey: "mcpServers", MCPStrategy: "file"},
}

var agentAliases = map[string]string{
	"claude": "claude_code",
}

var agentDetectionOrder = []string{"codex", "claude_code", "cursor", "copilot", "opencode", "kiro", "junie"}

// DetectProject 从任意目录向上查找 go.mod，确认当前 PrismGo 项目根目录。
func DetectProject(start string) (Project, error) {
	if start == "" {
		start = "."
	}
	current, err := filepath.Abs(start)
	if err != nil {
		return Project{}, err
	}
	for {
		goMod := filepath.Join(current, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			// Lens 可以作为独立二进制、go run @latest 或源码模块运行。
			// 项目探测只按 module name 跳过 Lens 自身，不能依赖某个临时开发目录结构。
			if moduleName, err := readGoModuleName(goMod); err != nil {
				return Project{}, err
			} else if moduleName != "github.com/prismgo/lens" {
				return Project{Root: current}, nil
			}
		}
		next := filepath.Dir(current)
		if next == current {
			return Project{}, errors.New("prismgo-lens: cannot find go.mod from project path")
		}
		current = next
	}
}

func readGoModuleName(goModPath string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", nil
}

// DetectAgents 根据项目内配置文件探测已存在的 Agent。
func DetectAgents(root string) []Agent {
	detected := make([]Agent, 0, len(agentRegistry))
	for _, name := range agentDetectionOrder {
		agent := agentRegistry[name]
		if fileExists(filepath.Join(root, agent.GuidelinesPath)) || fileExists(filepath.Join(root, agent.MCPPath)) {
			detected = append(detected, agent)
		}
	}
	return detected
}

func agentByName(name string) (Agent, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := agentAliases[key]; ok {
		key = canonical
	}
	agent, ok := agentRegistry[key]
	return agent, ok
}

func agentByNameFromConfig(config ProjectConfig, name string) (Agent, bool) {
	if agent, ok := agentByName(name); ok {
		if strategy := strings.TrimSpace(config.AgentStrategies[agent.Name]); strategy != "" {
			agent.MCPStrategy = strategy
		}
		return agent, true
	}
	custom := customAgentMap(config.CustomAgents)
	agent, ok := custom[strings.ToLower(strings.TrimSpace(name))]
	return agent, ok
}

func normalizeAgents(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if agent, ok := agentByName(name); ok && !seen[agent.Name] {
			seen[agent.Name] = true
			out = append(out, agent.Name)
		}
	}
	sort.Strings(out)
	return out
}

func agentNames(agents []Agent) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return normalizeAgents(names)
}

func agentsFromNames(names []string) []Agent {
	agents := make([]Agent, 0, len(names))
	for _, name := range normalizeAgents(names) {
		if agent, ok := agentByName(name); ok {
			agents = append(agents, agent)
		}
	}
	return agents
}

func agentsFromConfig(config ProjectConfig) []Agent {
	seen := map[string]bool{}
	agents := make([]Agent, 0, len(config.Agents))
	for _, name := range config.Agents {
		agent, ok := agentByNameFromConfig(config, name)
		if !ok || seen[agent.Name] {
			continue
		}
		seen[agent.Name] = true
		agents = append(agents, agent)
	}
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents
}

func customAgentMap(configs []CustomAgentConfig) map[string]Agent {
	agents := map[string]Agent{}
	for _, config := range configs {
		name := strings.ToLower(strings.TrimSpace(config.Name))
		if name == "" {
			continue
		}
		displayName := strings.TrimSpace(config.DisplayName)
		if displayName == "" {
			displayName = config.Name
		}
		strategy := strings.TrimSpace(config.MCPStrategy)
		if strategy == "" {
			strategy = "file"
		}
		key := strings.TrimSpace(config.MCPConfigKey)
		if key == "" {
			key = "mcpServers"
		}
		agents[name] = Agent{
			Name:           config.Name,
			DisplayName:    displayName,
			GuidelinesPath: config.GuidelinesPath,
			SkillsPath:     config.SkillsPath,
			MCPPath:        config.MCPPath,
			MCPConfigKey:   key,
			MCPStrategy:    strategy,
			ShellCommand:   config.ShellCommand,
			ShellArgs:      config.ShellArgs,
		}
	}
	return agents
}

// AgentMCPInstallPlan 描述 Agent MCP 安装策略的可审计计划。
// 需求背景：v12 需要把 Agent adapter 从“只写文件路径”提升为可解释的 install strategy，
// dry-run 可以展示将写入的配置位置和结构化 stdio 命令，后续 shell strategy 也能复用同一计划结构。
type AgentMCPInstallPlan struct {
	Agent       string
	DisplayName string
	Strategy    string
	ConfigPath  string
	Command     string
	Args        []string
}

// AgentMCPInstallPlans 返回指定 Agent 的 MCP 安装计划。
// 参数用途：names 是用户配置或 CLI 选择的 Agent 名称，支持 alias 并自动去重排序。
func AgentMCPInstallPlans(names []string) []AgentMCPInstallPlan {
	plans := make([]AgentMCPInstallPlan, 0, len(names))
	for _, agent := range agentsFromNames(names) {
		plans = append(plans, agentMCPInstallPlan(agent))
	}
	return plans
}

func agentMCPInstallPlan(agent Agent) AgentMCPInstallPlan {
	return agentMCPInstallPlanWithCommand(agent, defaultMCPCommandPlan())
}

func agentMCPInstallPlanWithCommand(agent Agent, commandPlan MCPCommandPlan) AgentMCPInstallPlan {
	plan := AgentMCPInstallPlan{
		Agent:       agent.Name,
		DisplayName: agent.DisplayName,
		Strategy:    firstNonEmpty(agent.MCPStrategy, "file"),
		ConfigPath:  agent.MCPPath,
		Command:     commandPlan.Command,
		Args:        commandPlan.Args,
	}
	if plan.Strategy == "shell" {
		plan.Command = agent.ShellCommand
		plan.Args = append(append([]string{}, agent.ShellArgs...), "go")
		plan.Args = append(plan.Args, mcpGoRunArgs()...)
	}
	return plan
}

func mcpGoRunArgs() []string {
	return []string{"run", "github.com/prismgo/lens/cmd/prismgo-lens@latest", "--project", ".", "mcp"}
}

var runAgentMCPInstallCommand = defaultRunAgentMCPInstallCommand

func defaultRunAgentMCPInstallCommand(root string, plan AgentMCPInstallPlan) error {
	cmd := exec.Command(plan.Command, plan.Args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("agent mcp shell install failed for " + plan.Agent + ": " + strings.TrimSpace(string(output)))
	}
	return nil
}

// AgentHTTPMCPConfig 返回指定 Agent 可使用的 HTTP MCP 配置 payload。
// 需求背景：v12 只对齐 Laravel Boost 的 HTTP MCP config 形状，默认不启动 HTTP server，也不把该 payload 写入 Agent 配置。
func AgentHTTPMCPConfig(agentName string, serverURL string) (map[string]any, error) {
	agent, ok := agentByName(agentName)
	if !ok {
		return nil, errors.New("http mcp config: unsupported agent")
	}
	parsed, err := url.Parse(serverURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("http mcp config: server URL must be absolute http or https")
	}
	key := "mcpServers"
	if agent.Name == "copilot" {
		key = "servers"
	}
	return map[string]any{
		key: map[string]any{
			"prismgo-lens": map[string]any{
				"type": "http",
				"url":  serverURL,
			},
		},
	}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
