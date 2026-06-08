package lens

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Roster 汇总 Lens 需要共享的宿主项目运行时、依赖和功能信息。
// 设计背景：v12 的 application-info、search-docs、guidelines 和 prompts 都需要同一套项目事实，避免每个 tool 自己零散扫描。
type Roster struct {
	RootModule       string          `json:"root_module"`
	GoDirective      string          `json:"go_directive,omitempty"`
	RuntimeGoVersion string          `json:"runtime_go_version"`
	GoPackages       []RosterPackage `json:"go_packages"`
	FrontendPackages []RosterPackage `json:"frontend_packages,omitempty"`
	Features         []RosterFeature `json:"features"`
}

// RosterPackage 描述 Go module 或前端 package 的版本和替换状态。
// 参数用途：Replacement 只记录 go.mod replace 右侧，帮助 Agent 判断当前是否使用本地 PrismGo module。
type RosterPackage struct {
	Module      string `json:"module,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

// RosterFeature 描述 PrismGo 框架能力是否在当前项目中出现。
// 设计思路：只记录框架级能力，不记录 Workorder 业务模型，保持 Lens 与宿主业务解耦。
type RosterFeature struct {
	Feature string `json:"feature"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source,omitempty"`
}

// BuildRoster 扫描宿主项目，生成 PrismGo/Lens 共用的项目事实。
func BuildRoster(root string) Roster {
	goInfo := parseGoModRoster(filepath.Join(root, "go.mod"))
	return Roster{
		RootModule:       goInfo.RootModule,
		GoDirective:      goInfo.GoDirective,
		RuntimeGoVersion: runtimeVersion(),
		GoPackages:       goInfo.Packages,
		FrontendPackages: parseFrontendPackages(filepath.Join(root, "web", "package.json")),
		Features:         detectFeatureRoster(root),
	}
}

type goModRoster struct {
	RootModule  string
	GoDirective string
	Packages    []RosterPackage
}

func parseGoModRoster(path string) goModRoster {
	data, err := os.ReadFile(path)
	if err != nil {
		return goModRoster{}
	}
	info := goModRoster{}
	replacements := map[string]string{}
	requires := map[string]string{}
	inRequire := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "module ") {
			info.RootModule = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			continue
		}
		if strings.HasPrefix(line, "go ") {
			info.GoDirective = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			continue
		}
		if line == "require (" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		if inRequire {
			addRequireLine(requires, line)
			continue
		}
		if strings.HasPrefix(line, "require ") {
			addRequireLine(requires, strings.TrimSpace(strings.TrimPrefix(line, "require ")))
			continue
		}
		if strings.HasPrefix(line, "replace ") {
			addReplaceLine(replacements, strings.TrimSpace(strings.TrimPrefix(line, "replace ")))
		}
	}
	for module := range replacements {
		if _, ok := requires[module]; !ok {
			requires[module] = ""
		}
	}
	modules := make([]string, 0, len(requires))
	for module := range requires {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		info.Packages = append(info.Packages, RosterPackage{
			Module:      module,
			Version:     requires[module],
			Replacement: replacements[module],
			Kind:        "go",
		})
	}
	return info
}

func addRequireLine(requires map[string]string, line string) {
	fields := strings.Fields(strings.Split(line, "//")[0])
	if len(fields) >= 2 {
		requires[fields[0]] = fields[1]
	}
}

func addReplaceLine(replacements map[string]string, line string) {
	parts := strings.Split(line, "=>")
	if len(parts) != 2 {
		return
	}
	left := strings.Fields(strings.TrimSpace(parts[0]))
	right := strings.Fields(strings.TrimSpace(parts[1]))
	if len(left) == 0 || len(right) == 0 {
		return
	}
	replacements[left[0]] = right[0]
}

func parseFrontendPackages(path string) []RosterPackage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	names := map[string]string{}
	for name, version := range pkg.Dependencies {
		names[name] = version
	}
	for name, version := range pkg.DevDependencies {
		if _, exists := names[name]; !exists {
			names[name] = version
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	out := make([]RosterPackage, 0, len(ordered))
	for _, name := range ordered {
		out = append(out, RosterPackage{Name: name, Version: names[name], Kind: "frontend"})
	}
	return out
}

func detectFeatureRoster(root string) []RosterFeature {
	candidates := prismGoFeatureCandidates()
	moduleFeatures := detectPrismGoModuleFeatures(root)
	importFeatures := detectPrismGoImportFeatures(root)
	features := make([]RosterFeature, 0, len(candidates))
	for _, candidate := range candidates {
		enabled := false
		source := candidate.projectSource
		if candidate.projectSource != "" && fileExists(filepath.Join(root, filepath.FromSlash(candidate.projectSource))) {
			enabled = true
		}
		if moduleSource, ok := moduleFeatures[candidate.name]; ok {
			enabled = true
			source = moduleSource
		}
		if importSource, ok := importFeatures[candidate.name]; ok {
			enabled = true
			source = importSource
		}
		features = append(features, RosterFeature{Feature: candidate.name, Enabled: enabled, Source: source})
	}
	return features
}

type prismGoFeatureCandidate struct {
	name          string
	projectSource string
	moduleSubdir  string
	importSuffix  string
}

func prismGoFeatureCandidates() []prismGoFeatureCandidate {
	return []prismGoFeatureCandidate{
		{name: "cache", projectSource: "config/cache.go", moduleSubdir: "cache", importSuffix: "cache"},
		{name: "console", projectSource: "app/cmd", moduleSubdir: "console", importSuffix: "console"},
		{name: "cookie", projectSource: "config/session.go", moduleSubdir: "cookie", importSuffix: "cookie"},
		{name: "database", projectSource: "config/database.go", moduleSubdir: "database", importSuffix: "database"},
		{name: "filesystem", projectSource: "config/filesystem.go", moduleSubdir: "filesystem", importSuffix: "filesystem"},
		{name: "horizon", projectSource: "config/horizon.go", moduleSubdir: "horizon", importSuffix: "horizon"},
		{name: "logger", projectSource: "config/log.go", moduleSubdir: "log", importSuffix: "log"},
		{name: "queue", projectSource: "config/queue.go", moduleSubdir: "queue", importSuffix: "queue"},
		{name: "route", projectSource: "routes", moduleSubdir: "route", importSuffix: "route"},
		{name: "schema", projectSource: "database", moduleSubdir: "schema", importSuffix: "schema"},
		{name: "session", projectSource: "config/session.go", moduleSubdir: "session", importSuffix: "session"},
		{name: "translation", projectSource: "lang", moduleSubdir: "translation", importSuffix: "translation"},
		{name: "vue-vite", projectSource: "web/package.json"},
	}
}

func detectPrismGoModuleFeatures(root string) map[string]string {
	modules, err := listGoModules(root)
	if err != nil {
		return nil
	}
	features := map[string]string{}
	for _, module := range modules {
		dir := moduleAssetDir(module)
		if dir == "" || !isPrismGoFrameworkModule(module.Path) {
			continue
		}
		if feature, ok := prismGoFeatureFromModulePath(module.Path); ok && dirExists(dir) {
			features[feature] = "go module " + module.Path
			continue
		}
		for _, candidate := range prismGoFeatureCandidates() {
			if candidate.moduleSubdir == "" {
				continue
			}
			if dirExists(filepath.Join(dir, filepath.FromSlash(candidate.moduleSubdir))) {
				features[candidate.name] = "go module " + module.Path + "/" + candidate.moduleSubdir
			}
		}
	}
	return features
}

func detectPrismGoImportFeatures(root string) map[string]string {
	features := map[string]string{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && fileExists(filepath.Join(path, "go.mod")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(data)
		for _, candidate := range prismGoFeatureCandidates() {
			if candidate.importSuffix == "" {
				continue
			}
			importPath := "github.com/prismgo/framework/" + candidate.importSuffix
			if strings.Contains(text, `"`+importPath+`"`) {
				features[candidate.name] = "import " + importPath
			}
		}
		return nil
	})
	return features
}

func isPrismGoFrameworkModule(module string) bool {
	return module == "github.com/prismgo/framework" || strings.HasPrefix(module, "github.com/prismgo/framework/")
}

func prismGoFeatureFromModulePath(module string) (string, bool) {
	const prefix = "github.com/prismgo/framework/"
	if !strings.HasPrefix(module, prefix) {
		return "", false
	}
	featurePath := strings.TrimPrefix(module, prefix)
	for _, candidate := range prismGoFeatureCandidates() {
		if candidate.moduleSubdir != "" && featurePath == candidate.moduleSubdir {
			return candidate.name, true
		}
	}
	return "", false
}

func runtimeVersion() string {
	return strings.TrimSpace(runtime.Version())
}

// DetectTestSetup 判断宿主项目是否已有可执行测试基础。
// 需求背景：v12 对齐 Boost 的 enforce tests 策略，只在项目已有测试/覆盖率约定时默认强制 Agent 跑测试。
func DetectTestSetup(root string) bool {
	hasTests := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && fileExists(filepath.Join(path, "go.mod")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(os.PathSeparator)))
		if strings.Contains(rel, "/.git/") || strings.Contains(rel, "/storage/tmp/") {
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.go") || strings.HasPrefix(rel, "tests/") {
			hasTests = true
			return filepath.SkipAll
		}
		return nil
	})
	if !hasTests {
		return false
	}
	if fileExists(filepath.Join(root, "scripts", "coverage.sh")) || fileExists(filepath.Join(root, "scripts", "coverage.ps1")) {
		return true
	}
	for _, rel := range []string{"CLAUDE.md", "AGENTS.md"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err == nil && (strings.Contains(string(data), "coverage") || strings.Contains(string(data), "go test")) {
			return true
		}
	}
	return false
}
