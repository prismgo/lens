package lens

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Diagnostic 描述 PrismGo Lens 允许执行的只读诊断项。
// 需求背景：v12 明确不实现任意 Go eval，diagnostic registry 用预注册只读项替代 Laravel Boost tinker。
type Diagnostic struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	ReadOnly        bool              `json:"read_only"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	InputSchema     map[string]any    `json:"inputSchema,omitempty"`
	AllowedPackages []string          `json:"allowed_packages,omitempty"`
	Handler         DiagnosticHandler `json:"-"`
}

// DiagnosticHandler 是诊断项处理函数。
// 参数用途：root 是宿主项目根目录，input 是 run-diagnostic.input 的 JSON 内容。
type DiagnosticHandler func(root string, input json.RawMessage) (any, error)

// DiagnosticRegistry 保存默认允许暴露给 Agent 的诊断白名单。
type DiagnosticRegistry struct {
	diagnostics map[string]Diagnostic
}

// DefaultDiagnosticRegistry 注册 v12 第一批只读诊断项。
func DefaultDiagnosticRegistry() DiagnosticRegistry {
	registry := DiagnosticRegistry{diagnostics: map[string]Diagnostic{}}
	for _, diagnostic := range []Diagnostic{
		{Name: "current-config-summary", Description: "List discovered config keys and database connection names without returning secret values.", ReadOnly: true, TimeoutSeconds: 5, Handler: currentConfigSummaryDiagnostic, InputSchema: objectSchema(nil, nil)},
		{Name: "route-match-dry-run", Description: "Filter runtime route metadata by method and path without executing handlers.", ReadOnly: true, TimeoutSeconds: 30, Handler: routeMatchDryRunDiagnostic, InputSchema: objectSchema(map[string]any{
			"method": stringSchema("Optional HTTP method filter."),
			"path":   stringSchema("Optional route path substring."),
		}, nil)},
		{Name: "console-command-metadata", Description: "Return runtime console command metadata without running commands.", ReadOnly: true, TimeoutSeconds: 30, Handler: consoleCommandMetadataDiagnostic, InputSchema: objectSchema(nil, nil)},
		{Name: "database-connection-ping", Description: "Ping the configured database connection and return status only.", ReadOnly: true, TimeoutSeconds: 5, Handler: databaseConnectionPingDiagnostic, InputSchema: objectSchema(map[string]any{
			"connection": stringSchema("Database connection name; defaults to configured default connection."),
		}, nil)},
		{Name: "queue-connection-summary", Description: "Summarize queue-related config keys without dispatching jobs.", ReadOnly: true, TimeoutSeconds: 5, Handler: queueConnectionSummaryDiagnostic, InputSchema: objectSchema(nil, nil)},
		{Name: "horizon-store-health-summary", Description: "Summarize Horizon local storage/log availability without starting workers.", ReadOnly: true, TimeoutSeconds: 5, Handler: horizonStoreHealthSummaryDiagnostic, InputSchema: objectSchema(nil, nil)},
	} {
		registry.diagnostics[diagnostic.Name] = diagnostic
	}
	return registry
}

// Lookup 从诊断白名单按名称查找诊断项。
func (r DiagnosticRegistry) Lookup(name string) (Diagnostic, bool) {
	diagnostic, ok := r.diagnostics[name]
	return diagnostic, ok
}

// List 返回按名称排序的诊断元数据。
func (r DiagnosticRegistry) List() []Diagnostic {
	names := make([]string, 0, len(r.diagnostics))
	for name := range r.diagnostics {
		names = append(names, name)
	}
	sort.Strings(names)
	diagnostics := make([]Diagnostic, 0, len(names))
	for _, name := range names {
		diagnostics = append(diagnostics, r.diagnostics[name])
	}
	return diagnostics
}

func runDiagnosticTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	diagnostic, ok := DefaultDiagnosticRegistry().Lookup(input.Name)
	if !ok {
		return nil, fmt.Errorf("run-diagnostic: diagnostic %q is not registered", input.Name)
	}
	if !diagnostic.ReadOnly {
		return nil, fmt.Errorf("run-diagnostic: diagnostic %q is not read-only", input.Name)
	}
	if diagnostic.TimeoutSeconds <= 0 {
		return nil, fmt.Errorf("run-diagnostic: diagnostic %q must declare a timeout", input.Name)
	}
	if len(input.Input) == 0 {
		input.Input = json.RawMessage(`{}`)
	}
	result, err := diagnostic.Handler(root, input.Input)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"diagnostic":      diagnostic.Name,
		"read_only":       diagnostic.ReadOnly,
		"timeout_seconds": diagnostic.TimeoutSeconds,
		"result":          result,
	}, nil
}

func currentConfigSummaryDiagnostic(root string, _ json.RawMessage) (any, error) {
	database := parseDatabaseConfig(root)
	connectionNames := make([]string, 0, len(database.Connections))
	for name := range database.Connections {
		connectionNames = append(connectionNames, name)
	}
	sort.Strings(connectionNames)
	return map[string]any{
		"config_keys":          configKeys(root),
		"default_database":     database.Default,
		"database_connections": connectionNames,
	}, nil
}

func routeMatchDryRunDiagnostic(root string, input json.RawMessage) (any, error) {
	var args struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	_ = json.Unmarshal(input, &args)
	toolArgs, _ := json.Marshal(map[string]string{"method": args.Method, "path": args.Path})
	return listRoutesTool(root, toolArgs)
}

func consoleCommandMetadataDiagnostic(root string, _ json.RawMessage) (any, error) {
	return listConsoleCommandsTool(root, json.RawMessage(`{}`))
}

func databaseConnectionPingDiagnostic(root string, input json.RawMessage) (any, error) {
	var args struct {
		Connection string `json:"connection"`
	}
	_ = json.Unmarshal(input, &args)
	db, database, err := openDatabaseConnection(root, args.Connection)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return map[string]any{"ok": false, "database": database, "error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "database": database}, nil
}

func queueConnectionSummaryDiagnostic(root string, _ json.RawMessage) (any, error) {
	values := configValues(root)
	summary := map[string]string{}
	for key, value := range values {
		if strings.HasPrefix(key, "queue.") {
			summary[key] = redactValue(key, value)
		}
	}
	return map[string]any{
		"configured": len(summary) > 0,
		"keys":       sortedStringMap(summary),
	}, nil
}

func horizonStoreHealthSummaryDiagnostic(root string, _ json.RawMessage) (any, error) {
	logPath := filepath.Join(root, "storage", "logs", "horizon.log")
	return map[string]any{
		"horizon_config_present": fileExists(filepath.Join(root, "config", "horizon.go")),
		"storage_logs_present":   dirExists(filepath.Join(root, "storage", "logs")),
		"horizon_log_entries":    len(tailLines(logPath, 20)),
	}, nil
}

func sortedStringMap(values map[string]string) []map[string]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, map[string]string{"key": key, "value": values[key]})
	}
	return result
}
