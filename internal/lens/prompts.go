package lens

import (
	"embed"
	"fmt"
	"sort"
)

//go:embed assets/prompts/*.md
var promptAssets embed.FS

// Prompt 描述 MCP prompts/list 可暴露的任务指导文本。
// 设计背景：v12 只提供 Boost code simplifier 的 PrismGo 等价项，不提供升级类占位 prompt。
type Prompt struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AssetPath   string `json:"-"`
}

// PromptRegistry 保存 MCP prompts 的白名单。
type PromptRegistry struct {
	prompts map[string]Prompt
}

// DefaultPromptRegistry 注册 v12 第一批 MCP prompts。
func DefaultPromptRegistry() PromptRegistry {
	prompt := Prompt{
		Name:        "prismgo-code-simplifier",
		Description: "Simplify recent Go/PrismGo changes while preserving behavior.",
		AssetPath:   "assets/prompts/prismgo-code-simplifier.md",
	}
	return PromptRegistry{prompts: map[string]Prompt{prompt.Name: prompt}}
}

// Lookup 从 prompt 白名单中按名称查找 prompt。
func (r PromptRegistry) Lookup(name string) (Prompt, bool) {
	prompt, ok := r.prompts[name]
	return prompt, ok
}

// List 返回按名称排序的 prompt 元数据。
func (r PromptRegistry) List() []Prompt {
	names := make([]string, 0, len(r.prompts))
	for name := range r.prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	prompts := make([]Prompt, 0, len(names))
	for _, name := range names {
		prompts = append(prompts, r.prompts[name])
	}
	return prompts
}

// Filter 按配置收敛 prompt 白名单。
// 参数用途：filter.Include 非空时只保留指定 prompt，filter.Exclude 用于最终禁用 prompt。
func (r PromptRegistry) Filter(filter PrimitiveFilter) PromptRegistry {
	prompts := make(map[string]Prompt)
	if len(filter.Include) == 0 {
		for name, prompt := range r.prompts {
			prompts[name] = prompt
		}
	} else {
		for _, name := range filter.Include {
			if prompt, ok := r.prompts[name]; ok {
				prompts[name] = prompt
			}
		}
	}
	for _, name := range filter.Exclude {
		delete(prompts, name)
	}
	return PromptRegistry{prompts: prompts}
}

// ReadPrompt 读取内置 prompt 模板内容。
func ReadPrompt(prompt Prompt) (string, error) {
	data, err := promptAssets.ReadFile(prompt.AssetPath)
	if err != nil {
		return "", fmt.Errorf("prismgo-lens: read prompt %s: %w", prompt.Name, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("prismgo-lens: prompt %s is empty", prompt.Name)
	}
	return string(data), nil
}
