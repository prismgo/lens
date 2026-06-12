package lens

// DoctorReport 汇总当前 Prismgo Lens 安装状态，供 CLI doctor 输出。
type DoctorReport struct {
	ProjectRoot string
	Command     string
	PathStatus  string
	Warnings    []string
}

// Doctor 检查项目根、当前可执行文件和 prismgo-lens 是否已进入 PATH。
func Doctor(project string) (DoctorReport, error) {
	root, err := DetectProject(project)
	if err != nil {
		return DoctorReport{}, err
	}
	executable, err := installExecutablePath()
	if err != nil || executable == "" {
		executable = "unknown"
	}
	status := "missing"
	if found, err := installLookPath("prismgo-lens"); err == nil && found != "" {
		status = "ok"
	}
	report := DoctorReport{
		ProjectRoot: root.Root,
		Command:     executable,
		PathStatus:  status,
	}
	if status != "ok" {
		report.Warnings = append(report.Warnings, "prismgo-lens is not visible in the current PATH; installed MCP config may use an absolute command path until a new shell is opened")
	}
	return report, nil
}
