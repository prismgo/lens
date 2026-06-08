package lens

import "embed"

// builtinAIAssets 保存 Lens 内置 AI 资产树。
// 避免把复杂 skill/rule 内容硬编码在 Go 字符串中。
//
//go:embed .ai/*.md .ai/lens/*.md .ai/go/*.md .ai/prismgo/*.md .ai/vue-vite/*.md .ai/testing/*.md .ai/guidelines/prismgo/*.md .ai/guidelines/testing/*.md .ai/skills/*/SKILL.md .ai/skills/*/rules/*.md
var builtinAIAssets embed.FS
