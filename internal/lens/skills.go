package lens

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// ListSkills 列出 .ai/skills 下已有 skill 名称。
func ListSkills(root string) ([]string, error) {
	dir := filepath.Join(root, ".ai", "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	skills := []string{}
	for _, entry := range entries {
		if entry.IsDir() && fileExists(filepath.Join(dir, entry.Name(), "SKILL.md")) {
			skills = append(skills, entry.Name())
		}
	}
	sort.Strings(skills)
	return skills, nil
}

// AddSkillOptions 描述 add-skill 的显式选择与审计策略。
type AddSkillOptions struct {
	Skill     string
	All       bool
	Force     bool
	SkipAudit bool
	ListOnly  bool
}

// AddSkill 使用完整选项安装本地或 GitHub skill。
func AddSkill(root string, source string, options AddSkillOptions) ([]AuditReport, []string, error) {
	if LooksLikeGitHubSource(source) {
		return AddGitHubSkillWithOptions(root, source, options)
	}
	report, installedName, err := installSkillDir(root, source, filepath.Base(filepath.Clean(source)), options)
	if err != nil {
		return []AuditReport{report}, nil, err
	}
	if _, err := Update(root); err != nil {
		return []AuditReport{report}, nil, err
	}
	return []AuditReport{report}, []string{installedName}, nil
}

// GitHubSkillSource 是远程 skill 下载的 GitHub 地址解析结果。
type GitHubSkillSource struct {
	Owner string
	Repo  string
	Path  string
	Ref   string
}

// LooksLikeGitHubSource 判断 add-skill 输入是否是 GitHub 仓库地址。
func LooksLikeGitHubSource(value string) bool {
	return strings.Contains(value, "github.com/") || (strings.Count(value, "/") >= 1 && !strings.HasPrefix(value, ".") && !strings.HasPrefix(value, "/"))
}

// ParseGitHubSkillSource 支持 owner/repo、owner/repo/path 与 GitHub URL。
func ParseGitHubSkillSource(value string) (GitHubSkillSource, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://github.com/")
	value = strings.TrimPrefix(value, "http://github.com/")
	value = strings.Trim(value, "/")
	if strings.Contains(value, "..") {
		return GitHubSkillSource{}, errors.New("github skill source: path traversal is not allowed")
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return GitHubSkillSource{}, errors.New("github skill source: expected owner/repo[/path]")
	}
	source := GitHubSkillSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git"), Ref: "main"}
	if len(parts) > 2 {
		if parts[2] == "tree" && len(parts) > 4 {
			source.Ref = parts[3]
			source.Path = strings.Join(parts[4:], "/")
		} else {
			source.Path = strings.Join(parts[2:], "/")
		}
	}
	return source, nil
}

func AddGitHubSkillWithOptions(root string, value string, options AddSkillOptions) ([]AuditReport, []string, error) {
	source, err := ParseGitHubSkillSource(value)
	if err != nil {
		return nil, nil, err
	}
	temp, err := os.MkdirTemp("", "prismgo-lens-skill-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(temp)
	if err := downloadGitHubZip(source, temp); err != nil {
		return nil, nil, err
	}
	candidates, err := findSkillCandidates(filepath.Join(temp, "src"), source.Path)
	if err != nil {
		return nil, nil, err
	}
	names := candidateNamesFromPairs(candidates)
	if options.ListOnly {
		return nil, names, nil
	}
	if !options.All {
		if options.Skill != "" {
			selected := map[string]skillCandidate{}
			for _, candidate := range candidates {
				selected[candidate.Name] = candidate
			}
			candidate, ok := selected[options.Skill]
			if !ok {
				return nil, names, fmt.Errorf("github skill source: skill %q not found", options.Skill)
			}
			candidates = []skillCandidate{candidate}
			names = []string{options.Skill}
		} else if len(candidates) > 1 {
			return nil, names, errors.New("github skill source: multiple skills found; use --list, --skill NAME, or --all")
		}
	}
	reports := make([]AuditReport, 0, len(candidates))
	installed := make([]string, 0, len(candidates))
	for i, candidate := range candidates {
		report, installedName, err := installSkillDir(root, candidate.Path, candidate.Name, options)
		reports = append(reports, report)
		if err != nil {
			return reports, names[:i+1], err
		}
		installed = append(installed, installedName)
	}
	if _, err := Update(root); err != nil {
		return reports, names, err
	}
	return reports, installed, nil
}

func downloadGitHubZip(source GitHubSkillSource, target string) error {
	url := githubArchiveURL(source)
	ctx, cancel := context.WithTimeout(context.Background(), githubDownloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("github skill source: download timed out after %s", githubDownloadTimeout)
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("github skill source: download failed with status %s", response.Status)
	}
	archivePath := filepath.Join(target, "source.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, io.LimitReader(response.Body, 50<<20)); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return unzip(archivePath, filepath.Join(target, "src"))
}

var githubArchiveURL = func(source GitHubSkillSource) string {
	return fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", source.Owner, source.Repo, source.Ref)
}

var githubDownloadTimeout = 30 * time.Second

func unzip(archivePath string, target string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		clean := filepath.Clean(file.Name)
		if strings.Contains(clean, "..") {
			return errors.New("github skill source: zip path traversal is not allowed")
		}
		dest := filepath.Join(target, clean)
		if !strings.HasPrefix(dest, filepath.Clean(target)+string(os.PathSeparator)) {
			return errors.New("github skill source: zip entry escapes target")
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if !file.FileInfo().Mode().IsRegular() {
			return errors.New("github skill source: zip contains non-regular file")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(source, 5<<20))
		source.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// DiscoverGitHubSkillsFromZip 读取 GitHub archive zip 并返回其中所有 skill 名称。
func DiscoverGitHubSkillsFromZip(archivePath string, requestedPath string) ([]string, error) {
	temp, err := os.MkdirTemp("", "prismgo-lens-discover-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	if err := unzip(archivePath, filepath.Join(temp, "src")); err != nil {
		return nil, err
	}
	dirs, err := findSkillDirs(filepath.Join(temp, "src"), requestedPath)
	if err != nil {
		return nil, err
	}
	return candidateNames(dirs), nil
}

// skillCandidate 把远程 skill 名称和目录路径绑定在一起。
// 设计背景：路径排序和名称排序可能不同，分离排序会导致 --skill beta 安装到 alpha 的目录。
type skillCandidate struct {
	Name string
	Path string
}

func findSkillCandidates(root string, requestedPath string) ([]skillCandidate, error) {
	paths, err := findSkillDirs(root, requestedPath)
	if err != nil {
		return nil, err
	}
	candidates := make([]skillCandidate, 0, len(paths))
	for _, path := range paths {
		metadata, err := ReadSkillMetadata(path)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, skillCandidate{Name: metadata.Name, Path: path})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Name == candidates[j].Name {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates, nil
}

func findSkillDirs(root string, requestedPath string) ([]string, error) {
	searchRoot := root
	if requestedPath != "" {
		matches := []string{}
		requested := filepath.ToSlash(filepath.Clean(requestedPath))
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || !entry.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			if filepath.ToSlash(rel) == requested || filepath.Base(path) == requested {
				matches = append(matches, path)
			}
			return nil
		})
		if len(matches) == 0 {
			return nil, errors.New("github skill source: requested path not found")
		}
		if len(matches) > 1 {
			return nil, errors.New("github skill source: multiple paths match requested skill path; use a more specific path")
		}
		searchRoot = matches[0]
	}
	found := []string{}
	_ = filepath.WalkDir(searchRoot, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == "SKILL.md" {
			found = append(found, filepath.Dir(path))
		}
		return nil
	})
	if len(found) == 0 {
		return nil, errors.New("github skill source: no SKILL.md found")
	}
	sort.Strings(found)
	return found, nil
}

func candidateNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(filepath.Clean(path)))
	}
	sort.Strings(names)
	return names
}

func candidateNamesFromPairs(candidates []skillCandidate) []string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.Name)
	}
	return names
}

func installSkillDir(root string, source string, name string, options AddSkillOptions) (AuditReport, string, error) {
	report := AuditReport{Level: RiskLow}
	var err error
	if options.SkipAudit && !options.Force {
		return report, "", errors.New("add-skill: --skip-audit requires --force")
	}
	if !fileExists(filepath.Join(source, "SKILL.md")) {
		return report, "", errors.New("add-skill: source directory must contain SKILL.md")
	}
	metadata, err := ReadSkillMetadata(source)
	if err != nil {
		return report, "", err
	}
	name = metadata.Name
	if !options.SkipAudit {
		report, err = AuditSkill(source)
		if err != nil {
			return report, "", err
		}
		if report.Level == RiskCritical && !(options.Force && options.SkipAudit) {
			return report, "", errors.New("add-skill: critical risk skill refused; rerun with --force --skip-audit after manual review")
		}
		if report.Level == RiskHigh && !options.Force {
			return report, "", errors.New("add-skill: high risk skill refused; rerun with --force after manual review")
		}
	}
	if name == "." || name == string(os.PathSeparator) || strings.Contains(name, "..") {
		return report, "", errors.New("add-skill: invalid skill name")
	}
	target := filepath.Join(root, ".ai", "skills", name)
	inside, err := pathInside(source, target)
	if err != nil {
		return report, "", err
	}
	if inside {
		return report, "", errors.New("add-skill: target directory is inside source directory")
	}
	if err := copyDir(source, target); err != nil {
		return report, "", err
	}
	return report, name, syncInstalledSkill(root, name)
}

// SkillMetadata 是 Agent Skill frontmatter 中 Lens 必须理解的标准字段。
// 设计背景：v12 对齐 Boost 的 SkillComposer，安装和同步前必须校验 name/description，未知字段不参与权限判断。
type SkillMetadata struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Raw         map[string]any `yaml:"-"`
}

// ReadSkillMetadata 解析 SKILL.md YAML frontmatter，并校验 Agent Skills 必需字段。
// 参数用途：dir 是包含 SKILL.md 的 skill 目录；调用方用返回的 Name 作为稳定安装名。
func ReadSkillMetadata(dir string) (SkillMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return SkillMetadata{}, err
	}
	frontmatter, err := skillFrontmatter(data)
	if err != nil {
		return SkillMetadata{}, err
	}
	values := map[string]any{}
	if err := yaml.Unmarshal([]byte(frontmatter), &values); err != nil {
		return SkillMetadata{}, fmt.Errorf("skill frontmatter: parse YAML: %w", err)
	}
	metadata := SkillMetadata{Raw: values}
	if value, ok := values["name"].(string); ok {
		metadata.Name = strings.TrimSpace(value)
	}
	if value, ok := values["description"].(string); ok {
		metadata.Description = strings.TrimSpace(value)
	}
	if metadata.Name == "" {
		return SkillMetadata{}, errors.New("skill frontmatter: name is required")
	}
	if metadata.Description == "" {
		return SkillMetadata{}, errors.New("skill frontmatter: description is required")
	}
	return metadata, nil
}

func skillFrontmatter(data []byte) (string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", errors.New("skill frontmatter: missing YAML frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", errors.New("skill frontmatter: closing delimiter is required")
	}
	return rest[:end], nil
}

func pathInside(parent string, child string) (bool, error) {
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return false, err
	}
	absChild, err := filepath.Abs(child)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(absParent, absChild)
	if err != nil {
		return false, err
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."), nil
}

func syncInstalledSkill(root string, name string) error {
	source := filepath.Join(root, ".ai", "skills", name)
	for _, agent := range DetectAgents(root) {
		if err := copyDir(source, filepath.Join(root, agent.SkillsPath, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(source string, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o755)
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		// Skill 同步只复制普通文件，避免符号链接把源目录外内容带入 Agent skill 目录。
		if !info.Mode().IsRegular() {
			return errors.New("copy skill directory: source contains non-regular file")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}
