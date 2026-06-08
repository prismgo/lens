package lens

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RiskLevel 表示远程 skill 本地静态审计风险等级。
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// RiskFinding 描述 skill 审计命中的风险规则。
type RiskFinding struct {
	File   string `json:"file"`
	Rule   string `json:"rule"`
	Level  string `json:"level"`
	Reason string `json:"reason"`
}

// AuditReport 是 skill 风险审计的结果。
type AuditReport struct {
	Level    RiskLevel     `json:"level"`
	Findings []RiskFinding `json:"findings"`
}

// AuditSkill 扫描 skill 目录，默认拒绝高风险和 critical 风险。
func AuditSkill(dir string) (AuditReport, error) {
	report := AuditReport{Level: RiskLow}
	rules := []auditRule{
		{rule: "rm -rf", pattern: `(?i)\brm\s+-rf\b`, level: RiskCritical, reason: "包含递归强制删除命令"},
		{rule: "sudo", pattern: `(?i)\bsudo\b`, level: RiskHigh, reason: "包含提权命令"},
		{rule: "remote-script-exec", pattern: `(?is)\b(curl|wget)\b[^\n|;]*\|\s*(?:sudo\s+)?(?:/bin/)?(?:ba)?sh\b`, level: RiskCritical, reason: "包含远程脚本直接执行"},
		{rule: "git reset --hard", pattern: `(?i)\bgit\s+reset\s+--hard\b`, level: RiskHigh, reason: "包含破坏性 Git 重置"},
		{rule: "DROP TABLE", pattern: `(?i)\bDROP\s+TABLE\b`, level: RiskCritical, reason: "包含破坏性数据库语句"},
		{rule: "../", pattern: `\.\./`, level: RiskMedium, reason: "包含路径穿越片段"},
		{rule: "absolute-path-write", pattern: `(?i)(writefile|create|openfile|>\s*)\s*\(?\s*["']/(?:etc|var|usr|bin|sbin|root|home|tmp)/`, level: RiskHigh, reason: "包含绝对路径写入"},
		{rule: "undeclared-shell-exec", pattern: `(?i)(exec\.Command|subprocess\.|os\.system|child_process|shell:\s*true|sh\s+-c|bash\s+-c)`, level: RiskHigh, reason: "包含未声明的 shell 或进程执行"},
	}
	compiled := make([]compiledAuditRule, 0, len(rules))
	for _, rule := range rules {
		compiled = append(compiled, compiledAuditRule{auditRule: rule, regexp: regexp.MustCompile(rule.pattern)})
	}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if isHiddenPath(rel) {
			level := RiskMedium
			rule := "hidden-file"
			reason := "包含隐藏文件"
			if info.Size() > 256<<10 {
				level = RiskHigh
				rule = "large-hidden-file"
				reason = "包含大型隐藏文件"
			}
			report.Findings = append(report.Findings, RiskFinding{File: rel, Rule: rule, Level: string(level), Reason: reason})
			report.Level = maxRisk(report.Level, level)
		}
		if entry.Type()&0o111 != 0 {
			report.Findings = append(report.Findings, RiskFinding{File: rel, Rule: "executable", Level: string(RiskHigh), Reason: "包含可执行文件"})
			report.Level = maxRisk(report.Level, RiskHigh)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			report.Findings = append(report.Findings, RiskFinding{File: rel, Rule: "symlink", Level: string(RiskHigh), Reason: "包含符号链接文件，可能读取 skill 目录外内容"})
			report.Level = maxRisk(report.Level, RiskHigh)
			return nil
		}
		if !info.Mode().IsRegular() {
			report.Findings = append(report.Findings, RiskFinding{File: rel, Rule: "non-regular-file", Level: string(RiskHigh), Reason: "包含非普通文件"})
			report.Level = maxRisk(report.Level, RiskHigh)
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		for _, rule := range compiled {
			if rule.regexp.FindStringIndex(text) != nil {
				report.Findings = append(report.Findings, RiskFinding{File: rel, Rule: rule.rule, Level: string(rule.level), Reason: rule.reason})
				report.Level = maxRisk(report.Level, rule.level)
			}
		}
		return nil
	})
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].File == report.Findings[j].File {
			return report.Findings[i].Rule < report.Findings[j].Rule
		}
		return report.Findings[i].File < report.Findings[j].File
	})
	return report, err
}

type auditRule struct {
	rule    string
	pattern string
	level   RiskLevel
	reason  string
}

type compiledAuditRule struct {
	auditRule
	regexp *regexp.Regexp
}

func isHiddenPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func maxRisk(current RiskLevel, next RiskLevel) RiskLevel {
	order := map[RiskLevel]int{RiskLow: 0, RiskMedium: 1, RiskHigh: 2, RiskCritical: 3}
	if order[next] > order[current] {
		return next
	}
	return current
}
