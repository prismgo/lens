package lens

import (
	"fmt"
	"sort"
)

const (
	PrimitiveTool     = "tool"
	PrimitiveResource = "resource"
	PrimitivePrompt   = "prompt"
)

// Primitive 描述 MCP tool/resource/prompt 的统一元数据。
// 设计背景：v12 的 include/exclude 校验需要统一入口，但旧 Tool/Resource/Prompt 输出结构必须保持不变。
type Primitive struct {
	Kind           string
	Key            string
	Name           string
	URI            string
	Description    string
	InputSchema    map[string]any
	ReadOnly       bool
	TimeoutSeconds int
}

// PrimitiveRegistry 聚合全部 MCP primitive，统一提供 list/filter/lookup 能力。
type PrimitiveRegistry struct {
	primitives map[string]Primitive
	tools      ToolRegistry
	resources  ResourceRegistry
	prompts    PromptRegistry
}

// DefaultPrimitiveRegistry 从现有三套 registry 构建统一 primitive registry。
func DefaultPrimitiveRegistry() PrimitiveRegistry {
	tools := DefaultToolRegistry()
	resources := DefaultResourceRegistry()
	prompts := DefaultPromptRegistry()
	registry := PrimitiveRegistry{
		primitives: map[string]Primitive{},
		tools:      tools,
		resources:  resources,
		prompts:    prompts,
	}
	for _, tool := range tools.List() {
		registry.primitives[primitiveMapKey(PrimitiveTool, tool.Name)] = Primitive{
			Kind:           PrimitiveTool,
			Key:            tool.Name,
			Name:           tool.Name,
			Description:    tool.Description,
			InputSchema:    tool.InputSchema,
			ReadOnly:       tool.ReadOnly,
			TimeoutSeconds: tool.TimeoutSeconds,
		}
	}
	for _, resource := range resources.List() {
		registry.primitives[primitiveMapKey(PrimitiveResource, resource.URI)] = Primitive{
			Kind:        PrimitiveResource,
			Key:         resource.URI,
			Name:        resource.Name,
			URI:         resource.URI,
			Description: resource.Description,
			ReadOnly:    true,
		}
	}
	for _, prompt := range prompts.List() {
		registry.primitives[primitiveMapKey(PrimitivePrompt, prompt.Name)] = Primitive{
			Kind:        PrimitivePrompt,
			Key:         prompt.Name,
			Name:        prompt.Name,
			Description: prompt.Description,
			ReadOnly:    true,
		}
	}
	return registry
}

// Lookup 按 kind 和 key 查找 primitive；resource key 使用 URI，tool/prompt key 使用 name。
func (r PrimitiveRegistry) Lookup(kind string, key string) (Primitive, bool) {
	primitive, ok := r.primitives[primitiveMapKey(kind, key)]
	return primitive, ok
}

// List 返回按 kind/key 排序的 primitive 元数据。
func (r PrimitiveRegistry) List() []Primitive {
	keys := make([]string, 0, len(r.primitives))
	for key := range r.primitives {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]Primitive, 0, len(keys))
	for _, key := range keys {
		out = append(out, r.primitives[key])
	}
	return out
}

// Filter 返回应用 MCP include/exclude 后的统一 registry。
func (r PrimitiveRegistry) Filter(config MCPConfig) PrimitiveRegistry {
	return PrimitiveRegistry{
		primitives: filteredPrimitiveMap(r.primitives, config),
		tools:      r.tools.Filter(config.Tools),
		resources:  r.resources.Filter(config.Resources),
		prompts:    r.prompts.Filter(config.Prompts),
	}
}

// ValidateConfig 用统一 primitive lookup 校验 MCP include/exclude。
func (r PrimitiveRegistry) ValidateConfig(config MCPConfig) error {
	for kind, filter := range map[string]PrimitiveFilter{
		PrimitiveTool:     config.Tools,
		PrimitiveResource: config.Resources,
		PrimitivePrompt:   config.Prompts,
	} {
		for _, key := range append(filter.Include, filter.Exclude...) {
			if _, ok := r.Lookup(kind, key); !ok {
				return fmt.Errorf("prismgo-lens config: unsupported MCP %s %q", kind, key)
			}
		}
	}
	return nil
}

func (r PrimitiveRegistry) ToolRegistry() ToolRegistry {
	return r.tools
}

func (r PrimitiveRegistry) ResourceRegistry() ResourceRegistry {
	return r.resources
}

func (r PrimitiveRegistry) PromptRegistry() PromptRegistry {
	return r.prompts
}

func filteredPrimitiveMap(primitives map[string]Primitive, config MCPConfig) map[string]Primitive {
	out := map[string]Primitive{}
	for _, item := range []struct {
		kind   string
		filter PrimitiveFilter
	}{
		{PrimitiveTool, config.Tools},
		{PrimitiveResource, config.Resources},
		{PrimitivePrompt, config.Prompts},
	} {
		if len(item.filter.Include) == 0 {
			for key, primitive := range primitives {
				if primitive.Kind == item.kind {
					out[key] = primitive
				}
			}
		} else {
			for _, key := range item.filter.Include {
				if primitive, ok := primitives[primitiveMapKey(item.kind, key)]; ok {
					out[primitiveMapKey(item.kind, key)] = primitive
				}
			}
		}
		for _, key := range item.filter.Exclude {
			delete(out, primitiveMapKey(item.kind, key))
		}
	}
	return out
}

func primitiveMapKey(kind string, key string) string {
	return kind + "\x00" + key
}
