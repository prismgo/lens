package lens

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const blockStart = "<prismgo-lens-guidelines>"
const blockEnd = "</prismgo-lens-guidelines>"
const managedSkillMarker = ".prismgo-lens-managed"

// WriteGuidelines 写入 .ai/guidelines 并同步到 Agent guidelines 文件。
func WriteGuidelines(root string, agentNames []string) ([]string, error) {
	return writeGuidelinesForAgentsWithConfig(root, agentsFromNames(agentNames), ProjectConfig{EnforceTests: DetectTestSetup(root)})
}

func writeGuidelinesForAgentsWithConfig(root string, agents []Agent, config ProjectConfig) ([]string, error) {
	written := []string{}
	files, err := syncBuiltinGuidelines(root, config.EnforceTests)
	if err != nil {
		return nil, err
	}
	written = append(written, files...)
	block, err := renderGuidelinesBlock(root, config)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		target := filepath.Join(root, agent.GuidelinesPath)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := replaceManagedBlock(target, block); err != nil {
			return nil, err
		}
		written = append(written, agent.GuidelinesPath)
	}
	sort.Strings(written)
	return written, nil
}

// SyncSkills 把内置 PrismGo skills 写入 .ai/skills 并同步到 Agent skills 目录。
func SyncSkills(root string, agentNames []string) ([]string, error) {
	return syncSkillsForAgents(root, agentsFromNames(agentNames))
}

func syncSkillsForAgents(root string, agents []Agent) ([]string, error) {
	written, err := syncBuiltinSkills(root)
	if err != nil {
		return nil, err
	}
	skills, err := ListSkills(root)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if err := pruneStaleManagedSkills(root, agent, skills); err != nil {
			return nil, err
		}
		for _, name := range skills {
			source := filepath.Join(root, ".ai", "skills", name)
			targetDir := filepath.Join(root, agent.SkillsPath, name)
			if err := copyDir(source, targetDir); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(targetDir, managedSkillMarker), []byte("managed by prismgo-lens\n"), 0o644); err != nil {
				return nil, err
			}
			written = append(written, filepath.ToSlash(filepath.Join(agent.SkillsPath, name, "SKILL.md")))
		}
	}
	sort.Strings(written)
	return written, nil
}

func syncBuiltinGuidelines(root string, enforceTests bool) ([]string, error) {
	written := []string{}
	base := ".ai"
	err := fs.WalkDir(builtinAIAssets, base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if entry.IsDir() || !isBuiltinGuidelinePath(rel) {
			return nil
		}
		if isTestingGuidelinePath(rel) && !enforceTests {
			return nil
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		written = append(written, rel)
		// 项目级同路径 guideline 可以覆盖内置 guideline；install/update 不强行覆盖用户版本。
		if fileExists(target) {
			return nil
		}
		data, err := builtinAIAssets.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return nil, err
	}
	return written, nil
}

func syncBuiltinSkills(root string) ([]string, error) {
	written := []string{}
	base := ".ai/skills"
	err := fs.WalkDir(builtinAIAssets, base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(path)
		target := filepath.Join(root, filepath.FromSlash(rel))
		written = append(written, rel)
		// 项目同名资产优先级高于内置资产；这里不覆盖已有文件，保持 v12 override 语义。
		if fileExists(target) {
			return nil
		}
		data, err := builtinAIAssets.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, skill := range builtinSkillNamesFromWritten(written) {
		marker := filepath.Join(root, ".ai", "skills", skill, managedSkillMarker)
		if err := os.WriteFile(marker, []byte("managed by prismgo-lens\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return written, nil
}

func builtinSkillNamesFromWritten(paths []string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, path := range paths {
		parts := strings.Split(filepath.ToSlash(path), "/")
		if len(parts) < 3 || seen[parts[2]] {
			continue
		}
		seen[parts[2]] = true
		names = append(names, parts[2])
	}
	sort.Strings(names)
	return names
}

func pruneStaleManagedSkills(root string, agent Agent, active []string) error {
	activeSet := map[string]bool{}
	for _, name := range active {
		activeSet[name] = true
	}
	base := filepath.Join(root, agent.SkillsPath)
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || activeSet[entry.Name()] {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		if fileExists(filepath.Join(dir, managedSkillMarker)) {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteMCPConfig 以结构化 JSON 或保守 TOML 片段写入 Agent MCP 配置。
func WriteMCPConfig(root string, agentNames []string) ([]string, error) {
	return writeMCPConfigForAgents(root, agentsFromNames(agentNames), defaultMCPCommandPlan())
}

func writeMCPConfigForAgents(root string, agents []Agent, mcpPlan MCPCommandPlan) ([]string, error) {
	written := []string{}
	for _, agent := range agents {
		strategy := firstNonEmpty(agent.MCPStrategy, "file")
		if strategy == "none" {
			continue
		}
		if strategy == "shell" {
			plan := agentMCPInstallPlanWithCommand(agent, mcpPlan)
			if err := runAgentMCPInstallCommand(root, plan); err != nil {
				return nil, err
			}
			written = append(written, agent.Name+":mcp-shell")
			continue
		}
		target := filepath.Join(root, agent.MCPPath)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if strings.HasSuffix(target, ".toml") {
			if err := writeCodexTOMLWithPlan(target, mcpPlan); err != nil {
				return nil, err
			}
		} else if err := writeJSONMCPWithKeyAndPlan(target, firstNonEmpty(agent.MCPConfigKey, "mcpServers"), mcpPlan); err != nil {
			return nil, err
		}
		written = append(written, agent.MCPPath)
	}
	sort.Strings(written)
	return written, nil
}

func replaceManagedBlock(path string, block string) error {
	existingBytes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := string(existingBytes)
	start := strings.Index(existing, blockStart)
	end := strings.Index(existing, blockEnd)
	if start >= 0 && end > start {
		end += len(blockEnd)
		existing = strings.TrimSpace(existing[:start]) + "\n\n" + strings.TrimSpace(existing[end:])
	}
	next := strings.TrimSpace(existing)
	if next != "" {
		next += "\n\n"
	}
	next += block + "\n"
	return os.WriteFile(path, []byte(next), 0o644)
}

func writeJSONMCP(path string, agentName string) error {
	key := "mcpServers"
	if agentName == "copilot" {
		key = "servers"
	}
	return writeJSONMCPWithKey(path, key)
}

func writeJSONMCPWithKey(path string, key string) error {
	return writeJSONMCPWithKeyAndPlan(path, key, defaultMCPCommandPlan())
}

func writeJSONMCPWithKeyAndPlan(path string, key string, plan MCPCommandPlan) error {
	content := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &content); err != nil {
			return fmt.Errorf("mcp config %s: invalid JSON: %w", path, err)
		}
	}
	servers := map[string]any{}
	if existing, ok := content[key]; ok {
		typed, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("mcp config %s: %s must be an object", path, key)
		}
		servers = typed
	}
	servers["prismgo-lens"] = map[string]any{
		"command": plan.Command,
		"args":    plan.Args,
	}
	content[key] = servers
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func writeCodexTOMLWithPlan(path string, plan MCPCommandPlan) error {
	existingBytes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(strings.TrimSpace(string(existingBytes))) > 0 {
		var parsed map[string]any
		if err := toml.Unmarshal(existingBytes, &parsed); err != nil {
			return err
		}
	}
	existing := removeTOMLTable(string(existingBytes), "[mcp_servers.prismgo-lens]")
	block := "[mcp_servers.prismgo-lens]\ncommand = " + strconv.Quote(plan.Command) + "\nargs = " + tomlStringArray(plan.Args) + "\n"
	next := strings.TrimSpace(existing)
	if next != "" {
		next += "\n\n"
	}
	next += block
	return os.WriteFile(path, []byte(next), 0o644)
}

// defaultMCPCommandPlan 固定使用可提交的短命令名；PATH 修复和模式解析由后续 install 任务接入。
func defaultMCPCommandPlan() MCPCommandPlan {
	return MCPCommandPlanForInstall(MCPCommandOptions{
		Mode:               MCPCommandName,
		CommandName:        "prismgo-lens",
		CommandFoundInPath: true,
	})
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func removeTOMLTable(content string, table string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == table {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func renderGuidelinesBlock(root string, config ProjectConfig) (string, error) {
	sections, err := builtinGuidelineSections(root, config.EnforceTests)
	if err != nil {
		return "", err
	}
	sections = append(sections, builtinProjectGuideline(root))
	sections = append(sections, userGuidelines(root)...)
	return blockStart + "\n" + strings.TrimSpace(strings.Join(nonEmptyStrings(sections), "\n")) + "\n" + blockEnd, nil
}

func builtinGuidelineSections(root string, enforceTests bool) ([]string, error) {
	sections := []string{}
	err := fs.WalkDir(builtinAIAssets, ".ai", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if entry.IsDir() || !isBuiltinGuidelinePath(rel) {
			return nil
		}
		if isTestingGuidelinePath(rel) && !enforceTests {
			return nil
		}
		data, err := builtinGuidelineData(root, path)
		if err != nil {
			return err
		}
		sections = append(sections, strings.TrimSpace(string(data)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(sections)
	return sections, nil
}

func builtinGuidelineData(root string, path string) ([]byte, error) {
	if root != "" {
		target := filepath.Join(root, filepath.FromSlash(filepath.ToSlash(path)))
		if fileExists(target) {
			return os.ReadFile(target)
		}
	}
	return builtinAIAssets.ReadFile(path)
}

func isBuiltinGuidelinePath(path string) bool {
	if !strings.HasSuffix(path, ".md") || strings.HasPrefix(path, ".ai/skills/") {
		return false
	}
	return path != ".ai/skills"
}

func isTestingGuidelinePath(path string) bool {
	return strings.HasPrefix(path, ".ai/testing/") || strings.HasPrefix(path, ".ai/guidelines/testing/")
}

func builtinProjectGuideline(root string) string {
	rules := []string{}
	if fileExists(filepath.Join(root, "config", "queue.go")) {
		rules = append(rules, "- Use PrismGo queue docs and read-only diagnostics when touching queue workers.")
	}
	if len(rules) == 0 {
		return ""
	}
	return "# Prismgo Lens Project Signals\n\n" + strings.Join(rules, "\n") + "\n"
}

func userGuidelines(root string) []string {
	base := filepath.Join(root, ".ai", "guidelines")
	sections := []string{}
	_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, base+string(os.PathSeparator)))
		if strings.HasPrefix(rel, "prismgo/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			return nil
		}
		sections = append(sections, strings.TrimSpace(string(data)))
		return nil
	})
	sort.Strings(sections)
	return sections
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
