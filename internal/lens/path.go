package lens

import "strings"

const (
	// MCPCommandAuto 表示安装时自动选择最适合当前机器的 MCP 启动命令。
	MCPCommandAuto = "auto"
	// MCPCommandName 表示强制使用 PATH 中可解析的命令名，便于跨机器共享配置。
	MCPCommandName = "name"
	// MCPCommandAbsolute 表示强制使用当前机器上的可执行文件绝对路径。
	MCPCommandAbsolute = "absolute"
)

const pathBlockStart = "# >>> prismgo-lens PATH >>>"
const pathBlockEnd = "# <<< prismgo-lens PATH <<<"

// UserPATHStore 抽象当前用户 PATH 的读写位置，Windows 实现使用注册表。
type UserPATHStore interface {
	ReadUserPATH() (string, error)
	WriteUserPATH(value string) error
}

// MCPCommandOptions 描述生成 MCP 安装命令时需要的本机命令探测结果。
type MCPCommandOptions struct {
	// Mode 指定命令选择模式，空值时按 auto 处理。
	Mode string
	// CommandName 是期望写入配置的短命令名，通常是 prismgo-lens。
	CommandName string
	// ExecutablePath 是当前机器探测到的可执行文件绝对路径。
	ExecutablePath string
	// CommandFoundInPath 表示 CommandName 是否可通过 PATH 直接执行。
	CommandFoundInPath bool
}

// MCPCommandPlan 描述最终写入或执行的 MCP 启动命令。
type MCPCommandPlan struct {
	// Command 是进程启动命令本体，可以是短命令名、绝对路径或 go。
	Command string
	// Args 是 Command 后追加的参数列表，保持结构化便于后续写入不同配置格式。
	Args []string
	// Source 记录命令来源，便于后续诊断安装策略。
	Source string
}

// MCPCommandPlanForInstall 根据本机探测结果生成 Prismgo Lens MCP 安装命令。
func MCPCommandPlanForInstall(options MCPCommandOptions) MCPCommandPlan {
	mode := firstNonEmpty(options.Mode, MCPCommandAuto)
	name := firstNonEmpty(options.CommandName, "prismgo-lens")
	args := []string{"--project", ".", "mcp"}
	fallback := MCPCommandPlan{Command: "go", Args: []string{"run", "github.com/prismgo/lens/cmd/prismgo-lens@latest", "--project", ".", "mcp"}, Source: "go-run-dev"}
	if mode == MCPCommandName {
		return MCPCommandPlan{Command: name, Args: args, Source: "path-name"}
	}
	if mode == MCPCommandAbsolute {
		if options.ExecutablePath != "" {
			return MCPCommandPlan{Command: options.ExecutablePath, Args: args, Source: "absolute-executable"}
		}
		return fallback
	}
	if options.CommandFoundInPath {
		return MCPCommandPlan{Command: name, Args: args, Source: "path-name"}
	}
	if options.ExecutablePath != "" {
		return MCPCommandPlan{Command: options.ExecutablePath, Args: args, Source: "absolute-executable"}
	}
	return fallback
}

// AppendPATHValue 在 PATH 字符串末尾追加目录，并按平台规则避免重复写入。
func AppendPATHValue(existing string, dir string, sep string, caseInsensitive bool) string {
	parts := strings.Split(existing, sep)
	for _, part := range parts {
		left := strings.TrimSpace(part)
		right := strings.TrimSpace(dir)
		if caseInsensitive {
			if strings.EqualFold(left, right) {
				return existing
			}
		} else if left == right {
			return existing
		}
	}
	if strings.TrimSpace(existing) == "" {
		return dir
	}
	return strings.TrimRight(existing, sep) + sep + dir
}

// appendUserPATH 通过抽象存储追加 PATH，返回值表示是否实际写入，便于测试平台无关逻辑。
func appendUserPATH(store UserPATHStore, binDir string, sep string, caseInsensitive bool) (bool, error) {
	existing, err := store.ReadUserPATH()
	if err != nil {
		return false, err
	}
	next := AppendPATHValue(existing, binDir, sep, caseInsensitive)
	if next == existing {
		return false, nil
	}
	if err := store.WriteUserPATH(next); err != nil {
		return false, err
	}
	return true, nil
}

// replaceManagedPATHBlock 用固定标记维护 Prismgo Lens PATH 片段，避免重复写入用户 profile。
func replaceManagedPATHBlock(existing string, command string) string {
	block := pathBlockStart + "\n" + command + "\n" + pathBlockEnd
	for {
		start := strings.Index(existing, pathBlockStart)
		if start < 0 {
			break
		}
		endOffset := strings.Index(existing[start+len(pathBlockStart):], pathBlockEnd)
		if endOffset < 0 {
			break
		}
		end := start + len(pathBlockStart) + endOffset + len(pathBlockEnd)
		existing = strings.TrimSpace(existing[:start]) + "\n" + strings.TrimSpace(existing[end:])
	}
	next := strings.TrimSpace(existing)
	if next != "" {
		next += "\n\n"
	}
	return next + block + "\n"
}
