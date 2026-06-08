package lens

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PackageAsset 描述当前项目依赖模块随包分发的 Prismgo Lens AI 资产。
// 需求背景：v12 对齐 Laravel Boost 的 third-party package assets 发现能力，但默认只列出候选，不自动启用。
type PackageAsset struct {
	Module  string `json:"module"`
	Version string `json:"version,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Path    string `json:"path"`
}

type goListModule struct {
	Path    string        `json:"Path"`
	Version string        `json:"Version,omitempty"`
	Dir     string        `json:"Dir,omitempty"`
	Main    bool          `json:"Main,omitempty"`
	Replace *goListModule `json:"Replace,omitempty"`
}

var listGoModules = defaultListGoModules

// DiscoverPackageAssets 扫描当前项目实际依赖模块中的 Prismgo Lens guidelines/skills 候选。
// 设计思路：只读取 go list 返回的模块目录，不遍历整个 module cache，避免把无关缓存资产带入当前项目。
func DiscoverPackageAssets(root string) ([]PackageAsset, error) {
	modules, err := listGoModules(root)
	if err != nil {
		return nil, err
	}
	assets := []PackageAsset{}
	for _, module := range modules {
		if module.Main {
			continue
		}
		dir := moduleAssetDir(module)
		if dir == "" {
			continue
		}
		found, err := discoverPackageAssetsInModule(module, dir)
		if err != nil {
			return nil, err
		}
		assets = append(assets, found...)
	}
	sort.Slice(assets, func(i, j int) bool {
		left := assets[i].Module + "\x00" + assets[i].Type + "\x00" + assets[i].Name + "\x00" + assets[i].Path
		right := assets[j].Module + "\x00" + assets[j].Type + "\x00" + assets[j].Name + "\x00" + assets[j].Path
		return left < right
	})
	return assets, nil
}

// SyncSelectedPackageAssets 把配置中显式选择的依赖模块 AI 资产复制到项目 .ai source tree。
// 设计背景：第三方资产默认只发现不启用；只有共享配置记录的模块才进入 guidelines/skills 同步链路。
func SyncSelectedPackageAssets(root string, selectedModules []string) ([]string, error) {
	selected := packageModuleSet(selectedModules)
	if len(selected) == 0 {
		return nil, nil
	}
	modules, err := listGoModules(root)
	if err != nil {
		return nil, err
	}
	written := []string{}
	for _, module := range modules {
		if module.Main || !selected[module.Path] {
			continue
		}
		dir := moduleAssetDir(module)
		if dir == "" {
			continue
		}
		files, err := syncPackageAssetsInModule(root, module, dir)
		if err != nil {
			return nil, err
		}
		written = append(written, files...)
	}
	sort.Strings(written)
	return written, nil
}

func normalizePackageModuleSelection(modules []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(modules))
	for _, module := range modules {
		module = strings.TrimSpace(module)
		if module == "" || seen[module] {
			continue
		}
		seen[module] = true
		normalized = append(normalized, module)
	}
	sort.Strings(normalized)
	return normalized
}

func packageModuleSet(modules []string) map[string]bool {
	set := map[string]bool{}
	for _, module := range normalizePackageModuleSelection(modules) {
		set[module] = true
	}
	return set
}

func syncPackageAssetsInModule(root string, module goListModule, dir string) ([]string, error) {
	assets, err := discoverPackageAssetsInModule(module, dir)
	if err != nil {
		return nil, err
	}
	written := []string{}
	for _, asset := range assets {
		source := filepath.Join(dir, filepath.FromSlash(asset.Path))
		switch asset.Type {
		case "guideline":
			targetRel := filepath.ToSlash(filepath.Join(".ai", "guidelines", "packages", safePackageModulePath(asset.Module), asset.Name))
			target := filepath.Join(root, filepath.FromSlash(targetRel))
			if fileExists(target) {
				written = append(written, targetRel)
				continue
			}
			if err := copyRegularFile(source, target); err != nil {
				return nil, err
			}
			written = append(written, targetRel)
		case "skill":
			targetRel := filepath.ToSlash(filepath.Join(".ai", "skills", asset.Name, "SKILL.md"))
			targetDir := filepath.Join(root, ".ai", "skills", asset.Name)
			if fileExists(filepath.Join(targetDir, "SKILL.md")) && !fileExists(filepath.Join(targetDir, managedSkillMarker)) {
				written = append(written, targetRel)
				continue
			}
			report, err := AuditSkill(filepath.Dir(source))
			if err != nil {
				return nil, err
			}
			if report.Level == RiskCritical || report.Level == RiskHigh {
				return nil, fmt.Errorf("package assets sync: skill %s from %s has %s risk and must be reviewed with add-skill", asset.Name, asset.Module, report.Level)
			}
			if err := copyDir(filepath.Dir(source), targetDir); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(targetDir, managedSkillMarker), []byte("managed by prismgo-lens package asset\n"), 0o644); err != nil {
				return nil, err
			}
			written = append(written, targetRel)
		}
	}
	return written, nil
}

func safePackageModulePath(module string) string {
	module = strings.TrimSpace(module)
	module = strings.Trim(module, "/")
	replacer := strings.NewReplacer("\\", "_", ":", "_", "..", "_")
	module = replacer.Replace(module)
	if module == "" {
		return "unknown"
	}
	return module
}

func copyRegularFile(source string, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("package assets sync: source must be a regular file")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func defaultListGoModules(root string) ([]goListModule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", "all")
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("package assets discover: go list timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("package assets discover: go list failed: %w: %s", err, truncate(string(data), 2000))
	}
	return decodeGoListModules(data)
}

func decodeGoListModules(data []byte) ([]goListModule, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	modules := []goListModule{}
	for {
		var module goListModule
		if err := decoder.Decode(&module); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func moduleAssetDir(module goListModule) string {
	if module.Replace != nil && module.Replace.Dir != "" {
		return module.Replace.Dir
	}
	return module.Dir
}

func discoverPackageAssetsInModule(module goListModule, dir string) ([]PackageAsset, error) {
	base := filepath.Join(dir, "resources", "prismgo-lens")
	assets := []PackageAsset{}
	guidelines, err := filepath.Glob(filepath.Join(base, "guidelines", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range guidelines {
		asset, ok, err := packageAssetFromPath(module, dir, "guideline", filepath.Base(path), path)
		if err != nil {
			return nil, err
		}
		if ok {
			assets = append(assets, asset)
		}
	}
	skillsRoot := filepath.Join(base, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return assets, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsRoot, entry.Name(), "SKILL.md")
		asset, ok, err := packageAssetFromPath(module, dir, "skill", entry.Name(), path)
		if err != nil {
			return nil, err
		}
		if ok {
			assets = append(assets, asset)
		}
	}
	return assets, nil
}

func packageAssetFromPath(module goListModule, dir string, kind string, name string, path string) (PackageAsset, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PackageAsset{}, false, nil
		}
		return PackageAsset{}, false, err
	}
	if !info.Mode().IsRegular() {
		return PackageAsset{}, false, nil
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return PackageAsset{}, false, err
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return PackageAsset{}, false, fmt.Errorf("package assets discover: asset %s escapes module %s", path, module.Path)
	}
	return PackageAsset{
		Module:  module.Path,
		Version: module.Version,
		Type:    kind,
		Name:    name,
		Path:    filepath.ToSlash(rel),
	}, true, nil
}
