//go:build !windows

package lens

import (
	"os"
	"path/filepath"
	"strings"
)

// WriteUserPATH 将 binDir 写入当前用户 shell profile，返回实际写入的 profile 文件列表。
func WriteUserPATH(binDir string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	targets := unixProfileTargets(home, os.Getenv("SHELL"))
	written := []string{}
	for _, target := range targets {
		command := unixPATHCommand(home, binDir, target)
		if err := writePATHProfileBlock(target, command); err != nil {
			return nil, err
		}
		written = append(written, target)
	}
	return written, nil
}

// unixProfileTargets 按当前 shell 选择常见 profile，已存在的交互配置也同步写入。
func unixProfileTargets(home string, shell string) []string {
	if strings.Contains(shell, "fish") {
		return []string{filepath.Join(home, ".config", "fish", "config.fish")}
	}
	if strings.Contains(shell, "zsh") {
		targets := []string{filepath.Join(home, ".zprofile")}
		if fileExists(filepath.Join(home, ".zshrc")) {
			targets = append(targets, filepath.Join(home, ".zshrc"))
		}
		return targets
	}
	targets := []string{filepath.Join(home, ".profile")}
	if fileExists(filepath.Join(home, ".bashrc")) {
		targets = append(targets, filepath.Join(home, ".bashrc"))
	}
	return targets
}

// writePATHProfileBlock 保留用户原有内容，仅替换 Prismgo Lens 自己管理的 PATH 块。
func writePATHProfileBlock(path string, command string) error {
	mode := os.FileMode(0o644)
	info, statErr := os.Stat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(replaceManagedPATHBlock(string(data), command)), mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

// shellPathForProfile 将用户家目录内路径转换为 $HOME 写法，避免写入机器相关绝对 home。
func shellPathForProfile(home string, path string) string {
	rel, ok := pathRelativeToHome(home, path)
	if !ok {
		return path
	}
	if rel == "." {
		return "$HOME"
	}
	return "$HOME" + string(filepath.Separator) + rel
}

// unixPATHCommand 生成 shell profile 命令，路径部分必须避免变量或命令替换被意外展开。
func unixPATHCommand(home string, binDir string, target string) string {
	pathLiteral := shellPathLiteralForProfile(home, binDir)
	if strings.HasSuffix(target, "config.fish") {
		return "fish_add_path " + pathLiteral
	}
	return `export PATH="$PATH:"` + pathLiteral
}

// shellPathLiteralForProfile 保留 $HOME 展开能力，同时对真实路径后缀做单引号保护。
func shellPathLiteralForProfile(home string, path string) string {
	rel, ok := pathRelativeToHome(home, path)
	if !ok {
		return shellSingleQuote(path)
	}
	if rel == "." {
		return "$HOME"
	}
	return "$HOME" + shellSingleQuote(string(filepath.Separator)+rel)
}

// pathRelativeToHome 只接受 home 本身或 home 内部路径，避免 /home/alice2 被误判为 /home/alice。
func pathRelativeToHome(home string, path string) (string, bool) {
	rel, err := filepath.Rel(home, path)
	if err == nil && rel == "." {
		return rel, true
	}
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel, true
	}
	return "", false
}

// shellSingleQuote 使用 POSIX/fish 均可接受的单引号形式，阻止 $, ` 和空格等字符被解释。
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
