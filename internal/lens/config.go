package lens

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Features 记录 Prismgo Lens 安装时启用的开发能力。
// 设计思路：配置只描述 dev tool 自己的行为，不影响生产主应用启动链路。
type Features struct {
	Guidelines         bool `json:"guidelines"`
	Skills             bool `json:"skills"`
	MCP                bool `json:"mcp"`
	BrowserLogs        bool `json:"browser_logs"`
	GitHubDocsProvider bool `json:"github_docs_provider"`
}

// PrimitiveFilter 记录 MCP primitive 的 include/exclude 选择。
// 需求背景：v12 需要对齐 Laravel Boost 的 primitive 过滤能力，让团队能显式收敛暴露给 Agent 的 tool/resource/prompt。
type PrimitiveFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// MCPConfig 记录 MCP primitives 的共享配置。
// 设计思路：Features.MCP 只控制是否写入 MCP server 配置；这里控制 MCP server 运行时具体暴露哪些 primitive。
type MCPConfig struct {
	Tools     PrimitiveFilter `json:"tools,omitempty"`
	Resources PrimitiveFilter `json:"resources,omitempty"`
	Prompts   PrimitiveFilter `json:"prompts,omitempty"`
}

// ProjectConfig 是 .prismgo-lens.json 的持久化结构。
// 需求背景：install/update 需要记录团队共享的 Lens 同步范围，从而实现幂等写入和 stale skill 清理。
// ProjectRoot 只用于兼容 v12 早期已经写出的配置；新写入的共享配置不得包含本机绝对路径。
type ProjectConfig struct {
	Version                int                 `json:"version"`
	ProjectRoot            string              `json:"project_root,omitempty"`
	Agents                 []string            `json:"agents"`
	Features               Features            `json:"features"`
	SelectedPackageModules []string            `json:"selected_package_modules,omitempty"`
	AgentStrategies        map[string]string   `json:"agent_strategies,omitempty"`
	CustomAgents           []CustomAgentConfig `json:"custom_agents,omitempty"`
	EnforceTests           bool                `json:"enforce_tests,omitempty"`
	MCP                    MCPConfig           `json:"mcp,omitempty"`
	UpdatedAt              string              `json:"updated_at"`
}

// CustomAgentConfig 用共享配置表达非内置 Agent adapter。
// 设计背景：v12 不加载项目 Go plugin，自定义 Agent 只允许通过路径、MCP key 和受限 install strategy 配置。
type CustomAgentConfig struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name,omitempty"`
	GuidelinesPath string   `json:"guidelines_path"`
	SkillsPath     string   `json:"skills_path"`
	MCPPath        string   `json:"mcp_config_path"`
	MCPConfigKey   string   `json:"mcp_config_key,omitempty"`
	MCPStrategy    string   `json:"install_strategy,omitempty"`
	ShellCommand   string   `json:"shell_command,omitempty"`
	ShellArgs      []string `json:"shell_args,omitempty"`
}

// LocalConfig 是 .prismgo-lens.local.json 的本机状态结构。
// 设计思路：共享配置可以提交到仓库；本地配置只记录绝对路径、当前机器检测结果等可重建状态，默认不应提交。
type LocalConfig struct {
	Version        int      `json:"version"`
	ProjectRoot    string   `json:"project_root"`
	DetectedAgents []string `json:"detected_agents"`
	UpdatedAt      string   `json:"updated_at"`
}

// InstallOptions 描述 install 命令的非交互输入。
// 参数用途：Agents 指定要写入的 Agent adapter，Features 控制启用能力。
type InstallOptions struct {
	Agents                 []string
	Features               Features
	SelectedPackageModules []string
	EnforceTests           *bool
	FixPath                bool
	NoFixPath              bool
	MCPCommandMode         string
}

// UpdateOptions 描述 update 命令的同步策略。
// 需求背景：v12 要求 --ignore-skills 在真实 update 中也生效，避免自动化场景意外改写 Agent skill 输出。
type UpdateOptions struct {
	IgnoreSkills bool
}

// InstallResult 汇总 install/update 对用户可见的写入结果。
type InstallResult struct {
	ProjectRoot  string
	Detected     []Agent
	WrittenFiles []string
}

var installExecutablePath = os.Executable
var installLookPath = exec.LookPath
var installWriteUserPATH = WriteUserPATH

// DefaultFeatures 返回第一版默认启用能力。
func DefaultFeatures() Features {
	return Features{Guidelines: true, Skills: true, MCP: true, BrowserLogs: true}
}

// Install 生成 Prismgo Lens 的配置、guidelines、skills 与 Agent 配置。
func Install(root string, options InstallOptions) (InstallResult, error) {
	project, err := DetectProject(root)
	if err != nil {
		return InstallResult{}, err
	}
	if len(options.Agents) == 0 {
		options.Agents = agentNames(DetectAgents(project.Root))
	}
	if options.Features == (Features{}) {
		options.Features = DefaultFeatures()
	}
	enforceTests := DetectTestSetup(project.Root)
	if options.EnforceTests != nil {
		enforceTests = *options.EnforceTests
	}
	mcpPlan, err := installMCPCommandPlan(options)
	if err != nil {
		return InstallResult{}, err
	}
	config := ProjectConfig{
		Version:                1,
		Agents:                 normalizeAgents(options.Agents),
		Features:               options.Features,
		SelectedPackageModules: normalizePackageModuleSelection(options.SelectedPackageModules),
		EnforceTests:           enforceTests,
		UpdatedAt:              time.Now().UTC().Format(time.RFC3339),
	}
	if !options.NoFixPath {
		if files, err := repairInstallPATH(mcpPlan); err != nil {
			return InstallResult{}, err
		} else if len(files) > 0 {
			result, err := syncProjectWithMCPPlan(project.Root, config, mcpPlan)
			if err != nil {
				return InstallResult{}, err
			}
			result.WrittenFiles = append(result.WrittenFiles, files...)
			sort.Strings(result.WrittenFiles)
			return result, nil
		}
	}
	return syncProjectWithMCPPlan(project.Root, config, mcpPlan)
}

// DefaultProjectConfig 生成 install/update 在没有共享配置时使用的安全默认配置。
// 需求背景：update --dry-run 也要能预览首次同步行为，但不能写出配置文件。
func DefaultProjectConfig(root string, options InstallOptions) (ProjectConfig, error) {
	project, err := DetectProject(root)
	if err != nil {
		return ProjectConfig{}, err
	}
	if len(options.Agents) == 0 {
		options.Agents = agentNames(DetectAgents(project.Root))
	}
	if options.Features == (Features{}) {
		options.Features = DefaultFeatures()
	}
	enforceTests := DetectTestSetup(project.Root)
	if options.EnforceTests != nil {
		enforceTests = *options.EnforceTests
	}
	return ProjectConfig{
		Version:                1,
		ProjectRoot:            project.Root,
		Agents:                 normalizeAgents(options.Agents),
		Features:               options.Features,
		SelectedPackageModules: normalizePackageModuleSelection(options.SelectedPackageModules),
		EnforceTests:           enforceTests,
		UpdatedAt:              time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// Update 按 .prismgo-lens.json 记录重新同步 Lens 管理的内容。
func Update(root string) (InstallResult, error) {
	return UpdateWithOptions(root, UpdateOptions{})
}

// UpdateWithOptions 按 .prismgo-lens.json 记录重新同步 Lens 管理的内容，并应用本次命令选项。
func UpdateWithOptions(root string, options UpdateOptions) (InstallResult, error) {
	project, err := DetectProject(root)
	if err != nil {
		return InstallResult{}, err
	}
	config, err := ReadConfig(project.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return Install(project.Root, InstallOptions{Agents: agentNames(DetectAgents(project.Root)), Features: DefaultFeatures()})
		}
		return InstallResult{}, err
	}
	if err := ValidateConfig(project.Root, config); err != nil {
		return InstallResult{}, err
	}
	if options.IgnoreSkills {
		config.Features.Skills = false
	}
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return syncProject(project.Root, config)
}

// ReadConfig 读取 .prismgo-lens.json。
func ReadConfig(root string) (ProjectConfig, error) {
	data, err := os.ReadFile(filepath.Join(root, ".prismgo-lens.json"))
	if err != nil {
		return ProjectConfig{}, err
	}
	var config ProjectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return ProjectConfig{}, err
	}
	return config, nil
}

func syncProject(root string, config ProjectConfig) (InstallResult, error) {
	return syncProjectWithMCPPlan(root, config, defaultMCPCommandPlan())
}

func syncProjectWithMCPPlan(root string, config ProjectConfig, mcpPlan MCPCommandPlan) (InstallResult, error) {
	if err := ValidateConfig(root, config); err != nil {
		return InstallResult{}, err
	}
	written := make([]string, 0, 8)
	if err := writeConfig(root, config); err != nil {
		return InstallResult{}, err
	}
	written = append(written, ".prismgo-lens.json")
	if len(config.SelectedPackageModules) > 0 {
		files, err := SyncSelectedPackageAssets(root, config.SelectedPackageModules)
		if err != nil {
			return InstallResult{}, err
		}
		written = append(written, files...)
	}
	if config.Features.Guidelines {
		files, err := writeGuidelinesForAgentsWithConfig(root, agentsFromConfig(config), config)
		if err != nil {
			return InstallResult{}, err
		}
		written = append(written, files...)
	}
	if config.Features.Skills {
		files, err := syncSkillsForAgents(root, agentsFromConfig(config))
		if err != nil {
			return InstallResult{}, err
		}
		written = append(written, files...)
	}
	if config.Features.MCP {
		files, err := writeMCPConfigForAgents(root, agentsFromConfig(config), mcpPlan)
		if err != nil {
			return InstallResult{}, err
		}
		written = append(written, files...)
	}
	detected := DetectAgents(root)
	if err := writeLocalConfig(root, LocalConfig{
		Version:        1,
		ProjectRoot:    root,
		DetectedAgents: agentNames(detected),
		UpdatedAt:      config.UpdatedAt,
	}); err != nil {
		return InstallResult{}, err
	}
	written = append(written, ".prismgo-lens.local.json")
	sort.Strings(written)
	return InstallResult{ProjectRoot: root, Detected: detected, WrittenFiles: written}, nil
}

func installMCPCommandPlan(options InstallOptions) (MCPCommandPlan, error) {
	executable, _ := installExecutablePath()
	if strings.HasSuffix(filepath.Base(executable), ".test") {
		return defaultMCPCommandPlan(), nil
	}
	found, err := installLookPath("prismgolens")
	commandFound := err == nil && found != ""
	return MCPCommandPlanForInstall(MCPCommandOptions{
		Mode:               options.MCPCommandMode,
		CommandName:        "prismgolens",
		ExecutablePath:     executable,
		CommandFoundInPath: commandFound,
	}), nil
}

func repairInstallPATH(plan MCPCommandPlan) ([]string, error) {
	if plan.Source == "path-name" || plan.Command == "" {
		return nil, nil
	}
	return installWriteUserPATH(filepath.Dir(plan.Command))
}

// ValidateConfig 校验 Lens 配置只描述当前项目内的 dev-only 同步状态。
// 需求背景：坏配置不能被 update 静默覆盖，否则会隐藏越界路径或未知 Agent。
func ValidateConfig(root string, config ProjectConfig) error {
	if config.Version != 1 {
		return fmt.Errorf("prismgo-lens config: unsupported version %d", config.Version)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if config.ProjectRoot != "" {
		absConfigRoot, err := filepath.Abs(config.ProjectRoot)
		if err != nil {
			return err
		}
		if absConfigRoot != absRoot {
			return fmt.Errorf("prismgo-lens config: project_root %q must match detected project root %q", absConfigRoot, absRoot)
		}
	}
	for _, name := range config.Agents {
		if _, ok := agentByNameFromConfig(config, name); !ok {
			return fmt.Errorf("prismgo-lens config: unsupported agent %q", name)
		}
	}
	if err := validateCustomAgents(config.CustomAgents); err != nil {
		return err
	}
	if err := validateAgentStrategies(config); err != nil {
		return err
	}
	return DefaultPrimitiveRegistry().ValidateConfig(config.MCP)
}

func validateCustomAgents(agents []CustomAgentConfig) error {
	seen := map[string]bool{}
	for _, agent := range agents {
		name := strings.TrimSpace(agent.Name)
		if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("prismgo-lens config: invalid custom agent name %q", agent.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("prismgo-lens config: duplicate custom agent %q", agent.Name)
		}
		seen[key] = true
		for label, path := range map[string]string{"guidelines_path": agent.GuidelinesPath, "skills_path": agent.SkillsPath, "mcp_config_path": agent.MCPPath} {
			if strings.TrimSpace(path) == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) || filepath.Clean(path) == ".." {
				return fmt.Errorf("prismgo-lens config: custom agent %s must be a project-relative path", label)
			}
		}
		if err := validateMCPStrategy(firstNonEmpty(agent.MCPStrategy, "file")); err != nil {
			return fmt.Errorf("prismgo-lens config: custom agent %s", err)
		}
	}
	return nil
}

func validateAgentStrategies(config ProjectConfig) error {
	for name, strategy := range config.AgentStrategies {
		if _, ok := agentByNameFromConfig(config, name); !ok {
			return fmt.Errorf("prismgo-lens config: unsupported agent strategy target %q", name)
		}
		if err := validateMCPStrategy(strategy); err != nil {
			return err
		}
		if strategy == "shell" {
			agent, _ := agentByNameFromConfig(config, name)
			if strings.TrimSpace(agent.ShellCommand) == "" {
				return fmt.Errorf("prismgo-lens config: shell strategy for %s requires shell_command", name)
			}
		}
	}
	return nil
}

func validateMCPStrategy(strategy string) error {
	switch strings.TrimSpace(strategy) {
	case "file", "shell", "none":
		return nil
	default:
		return fmt.Errorf("prismgo-lens config: unsupported MCP install strategy %q", strategy)
	}
}

func writeConfig(root string, config ProjectConfig) error {
	// 共享配置只保留团队应提交的选择；本机绝对路径由 .prismgo-lens.local.json 承载。
	config.ProjectRoot = ""
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, ".prismgo-lens.json"), data, 0o644)
}

func writeLocalConfig(root string, config LocalConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, ".prismgo-lens.local.json"), data, 0o644)
}
