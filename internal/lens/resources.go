package lens

import "sort"

const ApplicationInfoResourceURI = "file://instructions/application-info.md"

// Resource 描述 MCP resources/list 可暴露的只读资源。
// 设计背景：v12 对齐 Laravel Boost 当前 main，只先提供 application-info 资源。
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mimeType"`
	ToolName    string `json:"-"`
}

// ResourceRegistry 保存 MCP resources 的白名单。
type ResourceRegistry struct {
	resources map[string]Resource
}

// DefaultResourceRegistry 注册 v12 第一批 MCP resources。
func DefaultResourceRegistry() ResourceRegistry {
	resource := Resource{
		URI:         ApplicationInfoResourceURI,
		Name:        "application-info",
		Description: "Runtime, package, and PrismGo feature context for this project.",
		MIMEType:    "text/markdown",
		ToolName:    "application-info",
	}
	return ResourceRegistry{resources: map[string]Resource{resource.URI: resource}}
}

// Lookup 从资源白名单中按 URI 查找资源。
func (r ResourceRegistry) Lookup(uri string) (Resource, bool) {
	resource, ok := r.resources[uri]
	return resource, ok
}

// List 返回按 URI 排序的资源元数据。
func (r ResourceRegistry) List() []Resource {
	uris := make([]string, 0, len(r.resources))
	for uri := range r.resources {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	resources := make([]Resource, 0, len(uris))
	for _, uri := range uris {
		resources = append(resources, r.resources[uri])
	}
	return resources
}

// Filter 按配置收敛 resource 白名单。
// 参数用途：filter.Include 使用 resource URI，filter.Exclude 也使用 resource URI，避免 name 与 URI 混淆。
func (r ResourceRegistry) Filter(filter PrimitiveFilter) ResourceRegistry {
	resources := make(map[string]Resource)
	if len(filter.Include) == 0 {
		for uri, resource := range r.resources {
			resources[uri] = resource
		}
	} else {
		for _, uri := range filter.Include {
			if resource, ok := r.resources[uri]; ok {
				resources[uri] = resource
			}
		}
	}
	for _, uri := range filter.Exclude {
		delete(resources, uri)
	}
	return ResourceRegistry{resources: resources}
}
