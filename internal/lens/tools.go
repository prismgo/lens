package lens

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

const defaultSearchDocsTokenLimit = 4000
const maxSearchDocsTokenLimit = 1_000_000
const githubDocsCacheTTL = time.Hour

// Tool 描述可被 MCP 暴露并通过 execute-tool 隔离执行的只读工具。
type Tool struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	ReadOnly       bool           `json:"read_only"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	Annotations    map[string]any `json:"annotations,omitempty"`
	Handler        ToolHandler    `json:"-"`
	InputSchema    map[string]any `json:"inputSchema,omitempty"`
}

// ToolHandler 是工具执行函数，参数为项目根目录和 JSON 参数。
type ToolHandler func(root string, args json.RawMessage) (any, error)

// ToolRegistry 保存第一版允许调用的工具白名单。
type ToolRegistry struct {
	tools map[string]Tool
}

var runAppCommand = defaultRunAppCommand
var openDatabaseConnection = openConfiguredDatabaseConnection
var githubDocsSearch = defaultGitHubDocsSearch
var docsHTTPClient = http.Client{Timeout: 5 * time.Second}

// DefaultToolRegistry 注册 Prismgo Lens 第一版全部 MCP tools。
func DefaultToolRegistry() ToolRegistry {
	registry := ToolRegistry{tools: map[string]Tool{}}
	for _, tool := range []Tool{
		{Name: "application-info", Description: "Return Go, PrismGo, config, and module summary.", ReadOnly: true, TimeoutSeconds: 5, Handler: applicationInfoTool, InputSchema: objectSchema(nil, nil)},
		{Name: "search-docs", Description: "Search local PrismGo and project documentation. token_limit defaults to 4000 and is capped at 1000000.", ReadOnly: true, TimeoutSeconds: 5, Handler: searchDocsTool, InputSchema: objectSchema(map[string]any{
			"query":       stringSchema("Single local documentation search query."),
			"queries":     arraySchema(stringSchema("One local documentation search query."), "Multiple local documentation search queries."),
			"packages":    arraySchema(stringSchema("Package/module filter, for example prismgo, lens, project."), "Optional package/module filters."),
			"token_limit": integerSchema("Maximum approximate response text budget; default 4000."),
		}, nil)},
		{Name: "database-connections", Description: "List configured database connections with secret values redacted.", ReadOnly: true, TimeoutSeconds: 5, Handler: databaseConnectionsTool, InputSchema: objectSchema(nil, nil)},
		{Name: "get-database-connections", Description: "Alias of database-connections.", ReadOnly: true, TimeoutSeconds: 5, Handler: databaseConnectionsTool, InputSchema: objectSchema(nil, nil)},
		{Name: "database-schema", Description: "Return MySQL schema metadata. mode defaults to summary; full includes columns, indexes, and foreign keys.", ReadOnly: true, TimeoutSeconds: 10, Handler: databaseSchemaTool, InputSchema: objectSchema(map[string]any{
			"connection":             stringSchema("Database connection name; defaults to configured default connection."),
			"mode":                   enumSchema([]string{"summary", "full"}, "Schema detail mode; default summary."),
			"filter":                 stringSchema("Optional table name substring filter."),
			"include_column_details": booleanSchema("Include column details even when mode is summary."),
		}, nil)},
		{Name: "database-query", Description: "Validate and execute one read-only SQL statement. max_rows defaults to 100; max_bytes defaults to 262144.", ReadOnly: true, TimeoutSeconds: 10, Handler: databaseQueryTool, InputSchema: objectSchema(map[string]any{
			"sql":        stringSchema("Single read-only SQL statement."),
			"connection": stringSchema("Database connection name; defaults to configured default connection."),
			"max_rows":   integerSchema("Maximum rows to return; default 100, cap 500."),
			"max_bytes":  integerSchema("Maximum encoded response bytes; default 262144."),
		}, []string{"sql"})},
		{Name: "run-diagnostic", Description: "Run one pre-registered read-only diagnostic. This is PrismGo Lens' safe substitute for Laravel Boost tinker.", ReadOnly: true, TimeoutSeconds: 10, Handler: runDiagnosticTool, InputSchema: objectSchema(map[string]any{
			"name":  stringSchema("Registered diagnostic name."),
			"input": objectSchema(nil, nil),
		}, []string{"name"})},
		{Name: "get-config", Description: "Read a config dot path with secret redaction.", ReadOnly: true, TimeoutSeconds: 5, Handler: getConfigTool, InputSchema: objectSchema(map[string]any{"key": stringSchema("Config dot key, for example app.name.")}, []string{"key"})},
		{Name: "list-available-config-keys", Description: "List config dot keys discovered from config/*.go.", ReadOnly: true, TimeoutSeconds: 5, Handler: listConfigKeysTool, InputSchema: objectSchema(nil, nil)},
		{Name: "list-routes", Description: "List PrismGo route declarations from source with optional filters.", ReadOnly: true, TimeoutSeconds: 30, Handler: listRoutesTool, InputSchema: objectSchema(map[string]any{
			"method": stringSchema("Optional HTTP method filter."),
			"path":   stringSchema("Optional path substring filter."),
			"name":   stringSchema("Optional route name substring filter."),
		}, nil)},
		{Name: "list-console-commands", Description: "List PrismGo console command definitions from source.", ReadOnly: true, TimeoutSeconds: 30, Handler: listConsoleCommandsTool, InputSchema: objectSchema(nil, nil)},
		{Name: "list-artisan-commands", Description: "Alias of list-console-commands.", ReadOnly: true, TimeoutSeconds: 30, Handler: listConsoleCommandsTool, InputSchema: objectSchema(nil, nil)},
		{Name: "get-absolute-url", Description: "Build an absolute URL from APP_URL and a path or route name.", ReadOnly: true, TimeoutSeconds: 30, Handler: absoluteURLTool, InputSchema: objectSchema(map[string]any{
			"path": stringSchema("Path to append to APP_URL."),
			"name": stringSchema("Route name to resolve from route:list output."),
		}, nil)},
		{Name: "get-env", Description: "Read a non-secret environment variable.", ReadOnly: true, TimeoutSeconds: 5, Handler: getEnvTool, InputSchema: objectSchema(map[string]any{"key": stringSchema("Environment variable name. Secret-like names are refused.")}, []string{"key"})},
		{Name: "read-log-entries", Description: "Read tail entries from storage/logs. channel defaults to app; path is restricted to storage/logs.", ReadOnly: true, TimeoutSeconds: 5, Handler: readLogEntriesTool, InputSchema: objectSchema(map[string]any{
			"entries": integerSchema("Number of tail entries; default 50."),
			"channel": stringSchema("Log channel filename without .log; defaults to app."),
			"path":    stringSchema("Optional relative path under storage/logs; overrides channel."),
		}, nil)},
		{Name: "last-error", Description: "Return the latest error-like app log entry.", ReadOnly: true, TimeoutSeconds: 5, Handler: lastErrorTool, InputSchema: objectSchema(nil, nil)},
		{Name: "browser-logs", Description: "Read development browser logs. entries defaults to 50 and is capped at 200.", ReadOnly: true, TimeoutSeconds: 5, Handler: browserLogsTool, InputSchema: objectSchema(map[string]any{"entries": integerSchema("Number of browser log tail entries; default 50.")}, nil)},
	} {
		tool.Annotations = map[string]any{"readOnlyHint": tool.ReadOnly}
		registry.tools[tool.Name] = tool
	}
	return registry
}

// objectSchema 生成 MCP tools/list 使用的最小 JSON Schema。
// 设计背景：Agent 需要从 tools/list 直接知道参数结构，避免调用前再依赖自然语言解析。
func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func arraySchema(items map[string]any, description string) map[string]any {
	return map[string]any{"type": "array", "items": items, "description": description}
}

func enumSchema(values []string, description string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

// Lookup 从白名单中查找工具。
func (r ToolRegistry) Lookup(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// List 返回按名称排序的工具元数据。
func (r ToolRegistry) List() []Tool {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, r.tools[name])
	}
	return tools
}

// Filter 按配置收敛工具白名单。
// 参数用途：filter.Include 非空时作为允许列表，filter.Exclude 始终作为最终拒绝列表。
func (r ToolRegistry) Filter(filter PrimitiveFilter) ToolRegistry {
	tools := make(map[string]Tool)
	if len(filter.Include) == 0 {
		for name, tool := range r.tools {
			tools[name] = tool
		}
	} else {
		for _, name := range filter.Include {
			if tool, ok := r.tools[name]; ok {
				tools[name] = tool
			}
		}
	}
	for _, name := range filter.Exclude {
		delete(tools, name)
	}
	return ToolRegistry{tools: tools}
}

// ExecuteTool 通过 registry 白名单执行工具并返回 JSON 响应。
func ExecuteTool(root string, name string, args json.RawMessage) ([]byte, error) {
	project, err := DetectProject(root)
	if err != nil {
		return nil, err
	}
	tool, ok := DefaultToolRegistry().Lookup(name)
	if !ok {
		return nil, fmt.Errorf("prismgo-lens: tool %q is not registered", name)
	}
	result, err := tool.Handler(project.Root, args)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(result, "", "  ")
}

// DecodeToolArguments 解码 execute-tool 使用的 base64 JSON 参数。
func DecodeToolArguments(encoded string) (json.RawMessage, error) {
	if strings.TrimSpace(encoded) == "" {
		return json.RawMessage(`{}`), nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(data), nil
}

// ValidateReadOnlySQL 校验 database-query 只允许单条只读 SQL。
func ValidateReadOnlySQL(query string) error {
	normalized, err := normalizeSQL(query)
	if err != nil {
		return err
	}
	if normalized == "" {
		return errors.New("database-query: SQL is empty")
	}
	if hasInteriorSemicolon(normalized) {
		return errors.New("database-query: multiple statements are not allowed")
	}
	normalized = strings.TrimSpace(strings.TrimSuffix(normalized, ";"))
	upper := strings.ToUpper(normalized)
	rejected := []string{"INSERT", "UPDATE", "DELETE", "ALTER", "DROP", "TRUNCATE", "CREATE", "REPLACE", "RENAME"}
	for _, token := range rejected {
		if regexp.MustCompile(`\b`+token+`\b`).FindString(upper) != "" {
			return fmt.Errorf("database-query: write or schema keyword %q is not allowed", token)
		}
	}
	sideEffects := []string{
		`\bINTO\s+OUTFILE\b`,
		`\bINTO\s+DUMPFILE\b`,
		`\bFOR\s+UPDATE\b`,
		`\bLOCK\s+IN\s+SHARE\s+MODE\b`,
	}
	for _, pattern := range sideEffects {
		if regexp.MustCompile(pattern).FindString(upper) != "" {
			return errors.New("database-query: locking or file-writing SQL is not allowed")
		}
	}
	for _, prefix := range []string{"SELECT ", "SHOW ", "EXPLAIN ", "DESCRIBE ", "DESC "} {
		if strings.HasPrefix(upper+" ", prefix) {
			return nil
		}
	}
	if strings.HasPrefix(upper, "WITH ") && regexp.MustCompile(`(?i)\)\s*SELECT\s+`).FindString(normalized) != "" {
		return nil
	}
	return errors.New("database-query: only read-only SQL is allowed")
}

func normalizeSQL(query string) (string, error) {
	cleaned, err := stripSQLCommentsAndStrings(query)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(cleaned)
	return strings.Join(fields, " "), nil
}

// stripSQLCommentsAndStrings 保留 SQL 结构 token，移除注释和字符串内容。
// 设计背景：只读校验不能被注释中的 MySQL versioned SQL 或字符串中的关键词误导。
func stripSQLCommentsAndStrings(query string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			next, err := skipQuotedSQL(query, i, ch)
			if err != nil {
				return "", err
			}
			out.WriteByte(' ')
			i = next
			continue
		}
		if ch == '-' && i+1 < len(query) && query[i+1] == '-' && (i+2 == len(query) || isSQLWhitespace(query[i+2])) {
			i = skipLineComment(query, i+2)
			out.WriteByte(' ')
			continue
		}
		if ch == '#' {
			i = skipLineComment(query, i+1)
			out.WriteByte(' ')
			continue
		}
		if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				return "", errors.New("database-query: unterminated SQL comment")
			}
			out.WriteByte(' ')
			if i+2 < len(query) && query[i+2] == '!' {
				out.WriteString(query[i+3 : i+2+end])
			}
			i = i + 2 + end + 1
			continue
		}
		out.WriteByte(ch)
	}
	return strings.TrimSpace(out.String()), nil
}

func skipQuotedSQL(query string, start int, quote byte) (int, error) {
	for i := start + 1; i < len(query); i++ {
		if query[i] == '\\' && i+1 < len(query) {
			i++
			continue
		}
		if query[i] == quote {
			if i+1 < len(query) && query[i+1] == quote {
				i++
				continue
			}
			return i, nil
		}
	}
	return len(query) - 1, errors.New("database-query: unterminated SQL string")
}

func skipLineComment(query string, start int) int {
	for i := start; i < len(query); i++ {
		if query[i] == '\n' || query[i] == '\r' {
			return i
		}
	}
	return len(query) - 1
}

func isSQLWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func hasInteriorSemicolon(query string) bool {
	trimmed := strings.TrimSpace(query)
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == ';' && strings.TrimSpace(trimmed[i+1:]) != "" {
			return true
		}
	}
	return false
}

func applicationInfoTool(root string, _ json.RawMessage) (any, error) {
	roster := BuildRoster(root)
	env := readEnvFile(filepath.Join(root, ".env"))
	featureFlags := map[string]bool{}
	for _, feature := range roster.Features {
		featureFlags[feature.Feature] = feature.Enabled
	}
	return map[string]any{
		"go_version":        roster.RuntimeGoVersion,
		"root_module":       roster.RootModule,
		"go_directive":      roster.GoDirective,
		"app_name":          roster.RootModule,
		"app_env":           firstNonEmpty(env["APP_ENV"], os.Getenv("APP_ENV")),
		"app_debug":         firstNonEmpty(env["APP_DEBUG"], os.Getenv("APP_DEBUG")),
		"database_engine":   guessDatabaseEngine(root),
		"framework_module":  "github.com/prismgo/framework",
		"packages":          roster.GoPackages,
		"frontend_packages": roster.FrontendPackages,
		"feature_roster":    roster.Features,
		"features":          featureFlags,
		"roster":            roster,
	}, nil
}

func searchDocsTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Query      string   `json:"query"`
		Queries    []string `json:"queries"`
		Packages   []string `json:"packages"`
		TokenLimit int      `json:"token_limit"`
	}
	_ = json.Unmarshal(args, &input)
	queries := input.Queries
	if input.Query != "" {
		queries = append(queries, input.Query)
	}
	if len(queries) == 0 {
		return map[string]any{"results": []any{}, "message": "query is required"}, nil
	}
	limit := input.TokenLimit
	if limit <= 0 || limit > maxSearchDocsTokenLimit {
		limit = defaultSearchDocsTokenLimit
	}
	results, truncated := searchLocalDocs(root, queries, input.Packages, limit)
	response := map[string]any{"results": results, "github_provider": "disabled", "truncated": truncated, "source": "local"}
	config, err := ReadConfig(root)
	if err != nil || !config.Features.GitHubDocsProvider {
		return response, nil
	}
	response["github_provider"] = "enabled"
	remoteResults, err := githubDocsSearch(root, queries, input.Packages, limit)
	if err != nil {
		response["warnings"] = []string{fmt.Sprintf("github docs provider failed; local results returned: %v", err)}
		return response, nil
	}
	if len(remoteResults) > 0 {
		results = append(results, remoteResults...)
		response["results"] = results
		response["source"] = "local+github"
	}
	return response, nil
}

func databaseConnectionsTool(root string, _ json.RawMessage) (any, error) {
	config := parseDatabaseConfig(root)
	connections := make([]map[string]any, 0, len(config.Connections))
	names := make([]string, 0, len(config.Connections))
	for name := range config.Connections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values := config.Connections[name]
		summary := map[string]any{"name": name}
		for _, key := range []string{"driver", "host", "port", "database", "username", "password", "charset", "dsn"} {
			if value, ok := values[key]; ok {
				summary[key] = redactValue("database."+key, value)
			}
		}
		connections = append(connections, summary)
	}
	return map[string]any{"default": config.Default, "connections": connections}, nil
}

func databaseSchemaTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Connection           string `json:"connection"`
		Mode                 string `json:"mode"`
		Filter               string `json:"filter"`
		IncludeColumnDetails bool   `json:"include_column_details"`
	}
	_ = json.Unmarshal(args, &input)
	if input.Mode == "" {
		input.Mode = "summary"
	}
	if input.Mode != "summary" && input.Mode != "full" {
		return nil, fmt.Errorf("database-schema: unsupported mode %q", input.Mode)
	}
	config := parseDatabaseConfig(root)
	connection := input.Connection
	if connection == "" {
		connection = config.Default
	}
	driver := ""
	if values, ok := config.Connections[connection]; ok {
		driver = values["driver"]
	}
	if driver == "" && connection != "" {
		driver = connection
	}
	return databaseSchemaForDriver(root, driver, connection, input.Mode, input.Filter, input.IncludeColumnDetails)
}

// databaseSchemaForDriver 统一不同数据库 driver 的 schema introspection 输出形状。
// 参数用途：driver 是配置中的数据库 driver，connection 是连接名，mode/filter 来自 MCP tool 入参。
func databaseSchemaForDriver(root string, driver string, connection string, mode string, filter string, includeColumnDetails bool) (any, error) {
	driver = normalizeDatabaseDriver(driver)
	if mode == "" {
		mode = "summary"
	}
	switch driver {
	case "mysql":
		return mysqlDatabaseSchema(root, connection, mode, filter, includeColumnDetails)
	case "postgres":
		return postgresDatabaseSchema(root, connection, mode, filter, includeColumnDetails)
	case "sqlite":
		return sqliteDatabaseSchema(root, connection, mode, filter, includeColumnDetails)
	default:
		return map[string]any{"driver": driver, "unsupported": true, "tables": []any{}, "message": fmt.Sprintf("database-schema: driver %q is unsupported", driver)}, nil
	}
}

func normalizeDatabaseDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "mysql", "mariadb":
		return "mysql"
	case "pgsql", "postgres", "postgresql":
		return "postgres"
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

func mysqlDatabaseSchema(root string, connection string, mode string, filter string, includeColumnDetails bool) (any, error) {
	db, database, err := openDatabaseConnection(root, connection)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := `SELECT TABLE_NAME, TABLE_TYPE, TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?`
	values := []any{database}
	if filter != "" {
		query += ` AND TABLE_NAME LIKE ?`
		values = append(values, "%"+filter+"%")
	}
	query += ` ORDER BY TABLE_NAME`
	rows, err := db.QueryContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []map[string]any{}
	for rows.Next() {
		var name, tableType string
		var rowCount sql.NullInt64
		if err := rows.Scan(&name, &tableType, &rowCount); err != nil {
			return nil, err
		}
		kind := "table"
		if strings.EqualFold(tableType, "VIEW") {
			kind = "view"
		}
		table := map[string]any{"name": name, "type": kind}
		if rowCount.Valid {
			table["rows"] = rowCount.Int64
		}
		if mode == "full" || includeColumnDetails {
			columns, err := loadMySQLColumns(ctx, db, database, name)
			if err != nil {
				return nil, err
			}
			table["columns"] = columns
		}
		if mode == "full" {
			indexes, err := loadMySQLIndexes(ctx, db, database, name)
			if err != nil {
				return nil, err
			}
			foreignKeys, err := loadMySQLForeignKeys(ctx, db, database, name)
			if err != nil {
				return nil, err
			}
			table["indexes"] = indexes
			table["foreign_keys"] = foreignKeys
		}
		tables = append(tables, table)
	}
	return map[string]any{"driver": "mysql", "database": database, "tables": tables}, rows.Err()
}

func postgresDatabaseSchema(root string, connection string, mode string, filter string, includeColumnDetails bool) (any, error) {
	db, schema, err := openDatabaseConnection(root, connection)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := `SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = $1`
	values := []any{schema}
	if filter != "" {
		query += ` AND table_name LIKE $2`
		values = append(values, "%"+filter+"%")
	}
	query += ` ORDER BY table_name`
	rows, err := db.QueryContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []map[string]any{}
	for rows.Next() {
		var name, tableType string
		if err := rows.Scan(&name, &tableType); err != nil {
			return nil, err
		}
		table := map[string]any{"name": name, "type": postgresTableType(tableType)}
		if mode == "full" || includeColumnDetails {
			columns, err := loadPostgresColumns(ctx, db, schema, name)
			if err != nil {
				return nil, err
			}
			table["columns"] = columns
		}
		if mode == "full" {
			indexes, err := loadPostgresIndexes(ctx, db, schema, name)
			if err != nil {
				return nil, err
			}
			foreignKeys, err := loadPostgresForeignKeys(ctx, db, schema, name)
			if err != nil {
				return nil, err
			}
			table["indexes"] = indexes
			table["foreign_keys"] = foreignKeys
		}
		tables = append(tables, table)
	}
	return map[string]any{"driver": "postgres", "schema": schema, "tables": tables}, rows.Err()
}

func sqliteDatabaseSchema(root string, connection string, mode string, filter string, includeColumnDetails bool) (any, error) {
	db, database, err := openDatabaseConnection(root, connection)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := `SELECT name, type, sql FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%'`
	if filter != "" {
		query += ` AND name LIKE ?`
	}
	query += ` ORDER BY name`
	var rows *sql.Rows
	if filter != "" {
		rows, err = db.QueryContext(ctx, query, "%"+filter+"%")
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []map[string]any{}
	for rows.Next() {
		var name, tableType string
		var createSQL sql.NullString
		if err := rows.Scan(&name, &tableType, &createSQL); err != nil {
			return nil, err
		}
		table := map[string]any{"name": name, "type": tableType}
		if createSQL.Valid {
			table["sql"] = createSQL.String
		}
		if mode == "full" || includeColumnDetails {
			columns, err := loadSQLiteColumns(ctx, db, name)
			if err != nil {
				return nil, err
			}
			table["columns"] = columns
		}
		if mode == "full" {
			indexes, err := loadSQLiteIndexes(ctx, db, name)
			if err != nil {
				return nil, err
			}
			foreignKeys, err := loadSQLiteForeignKeys(ctx, db, name)
			if err != nil {
				return nil, err
			}
			table["indexes"] = indexes
			table["foreign_keys"] = foreignKeys
		}
		tables = append(tables, table)
	}
	return map[string]any{"driver": "sqlite", "database": database, "tables": tables}, rows.Err()
}

func databaseQueryTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		SQL        string `json:"sql"`
		Connection string `json:"connection"`
		MaxRows    int    `json:"max_rows"`
		MaxBytes   int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	if driver := configuredDatabaseDriver(root, input.Connection); driver != "" && normalizeDatabaseDriver(driver) != "mysql" {
		return nil, fmt.Errorf("database-query: driver %q is unsupported; only mysql is supported", driver)
	}
	if err := ValidateReadOnlySQL(input.SQL); err != nil {
		return nil, err
	}
	if input.MaxRows <= 0 || input.MaxRows > 500 {
		input.MaxRows = 100
	}
	if input.MaxBytes <= 0 || input.MaxBytes > 1<<20 {
		input.MaxBytes = 256 << 10
	}
	db, _, err := openDatabaseConnection(root, input.Connection)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, input.SQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	resultRows := []map[string]any{}
	usedBytes := 0
	for rows.Next() {
		values := make([]any, len(columns))
		scan := make([]any, len(columns))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, column := range columns {
			row[column] = redactDatabaseValue(column, values[i])
		}
		data, _ := json.Marshal(row)
		usedBytes += len(data)
		if usedBytes > input.MaxBytes || len(resultRows) >= input.MaxRows {
			return map[string]any{"columns": columns, "rows": resultRows, "truncated": true}, nil
		}
		resultRows = append(resultRows, row)
	}
	return map[string]any{"columns": columns, "rows": resultRows}, rows.Err()
}

func getConfigTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(args, &input)
	values := configValues(root)
	if value, ok := values[input.Key]; ok {
		return map[string]any{"key": input.Key, "value": redactValue(input.Key, value), "found": true}, nil
	}
	return map[string]any{"key": input.Key, "found": false}, nil
}

func listConfigKeysTool(root string, _ json.RawMessage) (any, error) {
	return map[string]any{"keys": configKeys(root)}, nil
}

func listRoutesTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	_ = json.Unmarshal(args, &input)
	data, err := runAppCommand(root, toolTimeout("list-routes"), "route:list", "--json")
	if err != nil {
		return nil, fmt.Errorf("list-routes: %w", err)
	}
	routes, err := decodeRouteList(data)
	if err != nil {
		return nil, err
	}
	filtered := make([]map[string]any, 0, len(routes))
	for _, route := range routes {
		method := fmt.Sprint(route["method"])
		path := routePath(route)
		name := fmt.Sprint(route["name"])
		if input.Method != "" && !strings.EqualFold(method, input.Method) {
			continue
		}
		if input.Path != "" && !strings.Contains(path, input.Path) {
			continue
		}
		if input.Name != "" && !strings.Contains(name, input.Name) {
			continue
		}
		filtered = append(filtered, route)
	}
	return map[string]any{"routes": filtered}, nil
}

func listConsoleCommandsTool(root string, _ json.RawMessage) (any, error) {
	data, err := runAppCommand(root, toolTimeout("list-console-commands"), "list", "--format=json")
	if err != nil {
		return nil, fmt.Errorf("list-console-commands: %w", err)
	}
	commands, err := decodeConsoleCommandList(data)
	if err != nil {
		return nil, err
	}
	return map[string]any{"commands": commands}, nil
}

func absoluteURLTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(args, &input)
	env := readEnvFile(filepath.Join(root, ".env"))
	base := firstNonEmpty(env["APP_URL"], os.Getenv("APP_URL"), "http://127.0.0.1:8051")
	path := input.Path
	if path == "" && input.Name != "" {
		routes, err := listRoutesTool(root, json.RawMessage(fmt.Sprintf(`{"name":%q}`, input.Name)))
		if err != nil {
			return nil, err
		}
		for _, item := range routes.(map[string]any)["routes"].([]map[string]any) {
			if fmt.Sprint(item["name"]) == input.Name {
				path = routePath(item)
				break
			}
		}
		if path == "" {
			return nil, fmt.Errorf("get-absolute-url: route name %q was not found", input.Name)
		}
	}
	return map[string]string{"url": strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")}, nil
}

func getEnvTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(args, &input)
	if isSecretKey(input.Key) {
		return nil, errors.New("get-env: secret-like keys are refused")
	}
	env := readEnvFile(filepath.Join(root, ".env"))
	return map[string]any{"key": input.Key, "value": firstNonEmpty(env[input.Key], os.Getenv(input.Key))}, nil
}

func readLogEntriesTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Entries int    `json:"entries"`
		Channel string `json:"channel"`
		Path    string `json:"path"`
	}
	_ = json.Unmarshal(args, &input)
	if input.Entries <= 0 || input.Entries > 200 {
		input.Entries = 50
	}
	path, err := resolveLogPath(root, input.Channel, input.Path)
	if err != nil {
		return nil, err
	}
	entries := tailLogEntries(path, input.Entries)
	return map[string]any{"entries": entries, "path": filepath.ToSlash(strings.TrimPrefix(path, root+string(os.PathSeparator)))}, nil
}

func lastErrorTool(root string, _ json.RawMessage) (any, error) {
	entries := tailLogEntries(filepath.Join(root, "storage", "logs", "app.log"), 500)
	for i := len(entries) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(entries[i]), "error") {
			return map[string]string{"entry": truncate(entries[i], 2000)}, nil
		}
	}
	return map[string]any{"entry": "", "found": false}, nil
}

func browserLogsTool(root string, args json.RawMessage) (any, error) {
	var input struct {
		Entries int `json:"entries"`
	}
	_ = json.Unmarshal(args, &input)
	if input.Entries <= 0 || input.Entries > 200 {
		input.Entries = 50
	}
	return map[string]any{"entries": tailLines(filepath.Join(root, "storage", "logs", "browser.log"), input.Entries)}, nil
}

// resolveLogPath 把日志选择器约束在 storage/logs 内。
// 需求背景：read-log-entries 需要支持多 channel/path，但 MCP tool 仍然只能读取项目日志，不能变成任意文件读取器。
func resolveLogPath(root string, channel string, relPath string) (string, error) {
	logRoot := filepath.Join(root, "storage", "logs")
	if strings.TrimSpace(relPath) == "" {
		channel = strings.TrimSpace(channel)
		if channel == "" {
			channel = "app"
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(channel) {
			return "", fmt.Errorf("read-log-entries: invalid log channel %q", channel)
		}
		relPath = channel + ".log"
	}
	if filepath.IsAbs(relPath) {
		return "", errors.New("read-log-entries: log path must be relative to storage/logs")
	}
	clean := filepath.Clean(relPath)
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", errors.New("read-log-entries: log path must stay within storage/logs")
	}
	target := filepath.Join(logRoot, clean)
	rel, err := filepath.Rel(logRoot, target)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", errors.New("read-log-entries: log path must stay within storage/logs")
	}
	// 只读真实日志文件。词法路径在 storage/logs 内还不够，符号链接可能跳到项目外。
	realLogRoot, err := filepath.EvalSymlinks(logRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return target, nil
		}
		return "", err
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		if os.IsNotExist(err) {
			return target, nil
		}
		return "", err
	}
	realRel, err := filepath.Rel(realLogRoot, realTarget)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(realRel, ".."+string(os.PathSeparator)) || realRel == ".." {
		return "", errors.New("read-log-entries: log path must stay within storage/logs")
	}
	return target, nil
}

func toolTimeout(name string) time.Duration {
	tool, ok := DefaultToolRegistry().Lookup(name)
	if !ok || tool.TimeoutSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(tool.TimeoutSeconds) * time.Second
}

func decodeRouteList(data []byte) ([]map[string]any, error) {
	var routes []map[string]any
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, err
	}
	for _, route := range routes {
		path := routePath(route)
		if path != "" {
			route["path"] = path
		}
	}
	return routes, nil
}

func decodeConsoleCommandList(data []byte) ([]map[string]any, error) {
	var wrapped struct {
		Commands []map[string]any `json:"commands"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Commands != nil {
		return wrapped.Commands, nil
	}
	var commands []map[string]any
	if err := json.Unmarshal(data, &commands); err != nil {
		return nil, err
	}
	return commands, nil
}

func routePath(route map[string]any) string {
	value := ""
	for _, key := range []string{"path", "uri"} {
		if raw, ok := route[key]; ok && raw != nil {
			value = strings.TrimSpace(fmt.Sprint(raw))
			if value != "" {
				break
			}
		}
	}
	if value == "" {
		return ""
	}
	return "/" + strings.TrimLeft(value, "/")
}

func searchLocalDocs(root string, queries []string, packages []string, limit int) ([]map[string]any, bool) {
	paths := []string{"docs", ".ai/guidelines", ".ai/skills", "CLAUDE.md", "AGENTS.md", "CONTEXT.md"}
	results := []map[string]any{}
	used := 0
	truncated := false
	filters := packageFilterSet(packages)
	for _, rel := range paths {
		base := filepath.Join(root, rel)
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			meta := docMetadata(root, path)
			if !docPackageAllowed(meta["package"], filters) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			text := string(data)
			lower := strings.ToLower(text)
			for _, query := range queries {
				if strings.Contains(lower, strings.ToLower(query)) {
					snippet := truncate(text, 500)
					if used+len(snippet) > limit {
						remaining := limit - used
						if remaining <= 0 {
							truncated = true
							return filepath.SkipDir
						}
						snippet = truncate(snippet, remaining)
						truncated = true
					}
					used += len(snippet)
					result := map[string]any{
						"path":    filepath.ToSlash(strings.TrimPrefix(path, root+string(os.PathSeparator))),
						"snippet": snippet,
					}
					for key, value := range meta {
						result[key] = value
					}
					results = append(results, result)
					break
				}
			}
			return nil
		})
	}
	return results, truncated
}

// DocsSearchProvider 定义后续 hosted semantic docs provider 的最小接口。
// 设计背景：v12 先把 search-docs 调用链做成 provider-ready；GitHub provider 失败时只降级到本地结果，不阻塞 Agent 获取项目文档。
type DocsSearchProvider interface {
	Search(root string, queries []string, packages []string, limit int) ([]map[string]any, error)
}

// defaultGitHubDocsSearch 是 GitHub 文档 provider 的保守默认实现。
// 需求背景：没有配置远程索引端点时，不做网络扫描；测试和后续发布版可以通过注入函数或 provider 实现接入 GitHub 缓存索引。
func defaultGitHubDocsSearch(root string, queries []string, packages []string, limit int) ([]map[string]any, error) {
	endpoint := strings.TrimSpace(os.Getenv("PRISMGO_LENS_GITHUB_DOCS_URL"))
	if endpoint == "" {
		return nil, nil
	}
	return githubJSONDocsProvider{Endpoint: endpoint}.Search(root, queries, packages, limit)
}

type githubJSONDocsProvider struct {
	Endpoint string
}

// Search 调用预构建的 GitHub 文档 JSON 索引端点。
// 参数用途：root 保留给 provider 定位项目缓存目录；当前实现不写缓存，失败由上层 fallback 到本地搜索。
func (provider githubJSONDocsProvider) Search(root string, queries []string, packages []string, limit int) ([]map[string]any, error) {
	request, err := http.NewRequest(http.MethodGet, provider.Endpoint, nil)
	if err != nil {
		return nil, err
	}
	query := request.URL.Query()
	query.Set("q", strings.Join(queries, " "))
	if len(packages) > 0 {
		query.Set("packages", strings.Join(packages, ","))
	}
	query.Set("limit", fmt.Sprint(limit))
	request.URL.RawQuery = query.Encode()
	cacheKey := githubDocsCacheKey(request.URL.String())
	if results, ok := readGitHubDocsCache(root, cacheKey); ok {
		return results, nil
	}
	response, err := docsHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("github docs provider rate limited: HTTP %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("github docs provider failed: HTTP %d", response.StatusCode)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	for _, result := range payload.Results {
		if _, ok := result["source"]; !ok {
			result["source"] = "github"
		}
	}
	_ = writeGitHubDocsCache(root, cacheKey, payload.Results)
	return payload.Results, nil
}

type githubDocsCachePayload struct {
	CachedAt string           `json:"cached_at"`
	Results  []map[string]any `json:"results"`
}

func githubDocsCacheKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func readGitHubDocsCache(root string, key string) ([]map[string]any, bool) {
	path := filepath.Join(root, ".prismgo-lens", "cache", "github-docs", key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var payload githubDocsCachePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	cachedAt, err := time.Parse(time.RFC3339, payload.CachedAt)
	if err != nil || time.Since(cachedAt) > githubDocsCacheTTL {
		return nil, false
	}
	for _, result := range payload.Results {
		if _, ok := result["source"]; !ok {
			result["source"] = "github"
		}
	}
	return payload.Results, true
}

func writeGitHubDocsCache(root string, key string, results []map[string]any) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	path := filepath.Join(root, ".prismgo-lens", "cache", "github-docs", key+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := githubDocsCachePayload{CachedAt: time.Now().UTC().Format(time.RFC3339), Results: results}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func packageFilterSet(packages []string) map[string]bool {
	if len(packages) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, pkg := range packages {
		pkg = strings.ToLower(strings.TrimSpace(pkg))
		if pkg != "" {
			set[pkg] = true
		}
	}
	return set
}

func docPackageAllowed(pkg string, filters map[string]bool) bool {
	if len(filters) == 0 {
		return true
	}
	return filters[strings.ToLower(pkg)]
}

func docMetadata(root string, path string) map[string]string {
	rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(os.PathSeparator)))
	meta := map[string]string{"source": "local", "package": "project", "language": "unknown"}
	switch {
	case strings.HasPrefix(rel, "docs/prismgo/") || strings.HasPrefix(rel, "docs/framework/") || strings.HasPrefix(rel, ".ai/guidelines/prismgo/"):
		meta["package"] = "prismgo"
	case strings.Contains(strings.ToLower(rel), "lens"):
		meta["package"] = "lens"
	case rel == "CLAUDE.md" || rel == "AGENTS.md" || rel == "CONTEXT.md":
		meta["package"] = "project"
	}
	switch {
	case strings.Contains(rel, "/zh_CN/") || strings.Contains(rel, "/zh/") || strings.Contains(rel, "中文"):
		meta["language"] = "zh_CN"
	case strings.Contains(rel, "/en/"):
		meta["language"] = "en"
	}
	return meta
}

func configKeys(root string) []string {
	values := configValues(root)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func configValues(root string) map[string]string {
	values := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(root, "config", "*.go"))
	env := readEnvFile(filepath.Join(root, ".env"))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".go")
		data, _ := os.ReadFile(file)
		stack := []string{name}
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			for strings.HasPrefix(line, "},") || line == "}" || line == "}," {
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
				break
			}
			mapMatches := regexp.MustCompile(`"([a-zA-Z0-9_.-]+)"\s*:\s*map\[string\](?:interface\{\}|any)\s*\{`).FindAllStringSubmatch(line, -1)
			if len(mapMatches) > 0 {
				for _, match := range mapMatches {
					stack = append(stack, match[1])
				}
				if !strings.Contains(line, "Env(") {
					continue
				}
			}
			envMatches := regexp.MustCompile(`"([a-zA-Z0-9_.-]+)"\s*:\s*Env\("([^"]+)",\s*([^)]+)\)`).FindAllStringSubmatch(line, -1)
			if len(envMatches) > 0 {
				for _, match := range envMatches {
					key := strings.Join(append(append([]string{}, stack...), match[1]), ".")
					values[key] = resolveEnvValue(env, match[2], match[3])
				}
				continue
			}
			if match := regexp.MustCompile(`^"([a-zA-Z0-9_.-]+)"\s*:\s*"([^"]*)"`).FindStringSubmatch(line); len(match) == 3 {
				key := strings.Join(append(append([]string{}, stack...), match[1]), ".")
				values[key] = match[2]
			}
		}
	}
	return values
}

func readEnvFile(path string) map[string]string {
	env := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		env[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	return env
}

type databaseConfig struct {
	Default     string
	Connections map[string]map[string]string
}

func parseDatabaseConfig(root string) databaseConfig {
	values := configValues(root)
	config := databaseConfig{Default: firstNonEmpty(values["database.default"], "mysql"), Connections: map[string]map[string]string{}}
	prefix := "database.connections."
	for key, value := range values {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) != 2 {
			continue
		}
		if config.Connections[parts[0]] == nil {
			config.Connections[parts[0]] = map[string]string{}
		}
		config.Connections[parts[0]][parts[1]] = value
	}
	if len(config.Connections) == 0 {
		config.Connections["mysql"] = map[string]string{"driver": "mysql"}
	}
	return config
}

func configuredDatabaseDriver(root string, connection string) string {
	config := parseDatabaseConfig(root)
	if connection == "" {
		connection = config.Default
	}
	if values, ok := config.Connections[connection]; ok {
		return values["driver"]
	}
	return connection
}

func openConfiguredDatabaseConnection(root string, connection string) (*sql.DB, string, error) {
	config := parseDatabaseConfig(root)
	if connection == "" {
		connection = config.Default
	}
	values, ok := config.Connections[connection]
	if !ok {
		return nil, "", fmt.Errorf("database: connection %q not configured", connection)
	}
	switch normalizeDatabaseDriver(firstNonEmpty(values["driver"], connection)) {
	case "mysql":
		return openMySQLConnection(root, connection)
	case "postgres":
		return openPostgresConnection(values)
	case "sqlite":
		return openSQLiteConnection(root, values)
	default:
		return nil, "", fmt.Errorf("database: driver %q is unsupported", values["driver"])
	}
}

func openMySQLConnection(root string, connection string) (*sql.DB, string, error) {
	config := parseDatabaseConfig(root)
	if connection == "" {
		connection = config.Default
	}
	values, ok := config.Connections[connection]
	if !ok {
		return nil, "", fmt.Errorf("database: connection %q not configured", connection)
	}
	if values["driver"] != "" && values["driver"] != "mysql" {
		return nil, "", fmt.Errorf("database: driver %q is unsupported; only mysql is supported", values["driver"])
	}
	database := values["database"]
	dsn := values["dsn"]
	if dsn == "" {
		host := firstNonEmpty(values["host"], "127.0.0.1")
		port := firstNonEmpty(values["port"], "3306")
		user := firstNonEmpty(values["username"], "root")
		password := values["password"]
		charset := firstNonEmpty(values["charset"], "utf8mb4")
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=%s&parseTime=true", user, password, host, port, database, charset)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, "", err
	}
	return db, database, nil
}

func openPostgresConnection(values map[string]string) (*sql.DB, string, error) {
	schema := firstNonEmpty(values["schema"], "public")
	dsn := values["dsn"]
	if dsn == "" {
		host := firstNonEmpty(values["host"], "127.0.0.1")
		port := firstNonEmpty(values["port"], "5432")
		user := firstNonEmpty(values["username"], values["user"], "postgres")
		password := values["password"]
		database := firstNonEmpty(values["database"], "postgres")
		sslMode := firstNonEmpty(values["sslmode"], values["ssl_mode"], "disable")
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, password, database, sslMode)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, "", err
	}
	return db, schema, nil
}

func openSQLiteConnection(root string, values map[string]string) (*sql.DB, string, error) {
	database := firstNonEmpty(values["database"], values["dsn"])
	if database == "" {
		return nil, "", errors.New("database: sqlite database path is required")
	}
	// SQLite 配置常用相对路径；按项目根目录解析，避免 Lens 进程 cwd 改变后读错数据库文件。
	if shouldResolveSQLitePath(database) {
		database = filepath.Join(root, database)
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return nil, "", err
	}
	return db, database, nil
}

func shouldResolveSQLitePath(database string) bool {
	if filepath.IsAbs(database) {
		return false
	}
	lower := strings.ToLower(database)
	return database != ":memory:" && !strings.HasPrefix(lower, "file:")
}

func loadMySQLColumns(ctx context.Context, db *sql.DB, database string, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []map[string]any{}
	for rows.Next() {
		var name, dataType, columnType, nullable, key, extra, comment string
		var def sql.NullString
		if err := rows.Scan(&name, &dataType, &columnType, &nullable, &def, &key, &extra, &comment); err != nil {
			return nil, err
		}
		column := map[string]any{"name": name, "type": dataType, "column_type": columnType, "nullable": nullable == "YES", "key": key}
		if def.Valid {
			column["default"] = def.String
		}
		if extra != "" {
			column["extra"] = extra
		}
		if comment != "" {
			column["comment"] = comment
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func loadMySQLIndexes(ctx context.Context, db *sql.DB, database string, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string]map[string]any{}
	order := []string{}
	for rows.Next() {
		var name, column string
		var nonUnique, seq int
		if err := rows.Scan(&name, &nonUnique, &column, &seq); err != nil {
			return nil, err
		}
		index, ok := grouped[name]
		if !ok {
			index = map[string]any{"name": name, "unique": nonUnique == 0, "columns": []string{}}
			grouped[name] = index
			order = append(order, name)
		}
		index["columns"] = append(index["columns"].([]string), column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	indexes := make([]map[string]any, 0, len(order))
	for _, name := range order {
		indexes = append(indexes, grouped[name])
	}
	return indexes, nil
}

func loadMySQLForeignKeys(ctx context.Context, db *sql.DB, database string, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL ORDER BY CONSTRAINT_NAME, ORDINAL_POSITION`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	foreignKeys := []map[string]any{}
	for rows.Next() {
		var name, column, refTable, refColumn string
		if err := rows.Scan(&name, &column, &refTable, &refColumn); err != nil {
			return nil, err
		}
		foreignKeys = append(foreignKeys, map[string]any{"name": name, "column": column, "referenced_table": refTable, "referenced_column": refColumn})
	}
	return foreignKeys, rows.Err()
}

func postgresTableType(tableType string) string {
	if strings.EqualFold(tableType, "VIEW") {
		return "view"
	}
	return "table"
}

func loadPostgresColumns(ctx context.Context, db *sql.DB, schema string, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []map[string]any{}
	for rows.Next() {
		var name, dataType, nullable string
		var def sql.NullString
		if err := rows.Scan(&name, &dataType, &nullable, &def); err != nil {
			return nil, err
		}
		column := map[string]any{"name": name, "type": dataType, "nullable": nullable == "YES"}
		if def.Valid {
			column["default"] = def.String
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func loadPostgresIndexes(ctx context.Context, db *sql.DB, schema string, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 ORDER BY indexname`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexes := []map[string]any{}
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			return nil, err
		}
		indexes = append(indexes, map[string]any{"name": name, "definition": definition, "unique": strings.Contains(strings.ToUpper(definition), "UNIQUE INDEX")})
	}
	return indexes, rows.Err()
}

func loadPostgresForeignKeys(ctx context.Context, db *sql.DB, schema string, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `SELECT tc.constraint_name, kcu.column_name, ccu.table_name AS foreign_table_name, ccu.column_name AS foreign_column_name FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = $1 AND tc.table_name = $2 ORDER BY tc.constraint_name, kcu.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	foreignKeys := []map[string]any{}
	for rows.Next() {
		var name, column, refTable, refColumn string
		if err := rows.Scan(&name, &column, &refTable, &refColumn); err != nil {
			return nil, err
		}
		foreignKeys = append(foreignKeys, map[string]any{"name": name, "column": column, "referenced_table": refTable, "referenced_column": refColumn})
	}
	return foreignKeys, rows.Err()
}

func loadSQLiteColumns(ctx context.Context, db *sql.DB, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdentifier(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []map[string]any{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &def, &pk); err != nil {
			return nil, err
		}
		column := map[string]any{"name": name, "type": columnType, "nullable": notNull == 0, "primary_key": pk > 0}
		if def.Valid {
			column["default"] = def.String
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func loadSQLiteIndexes(ctx context.Context, db *sql.DB, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list(%s)", quoteSQLiteIdentifier(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexes := []map[string]any{}
	for rows.Next() {
		var seq, unique int
		var name, origin string
		var partial any
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		indexes = append(indexes, map[string]any{"name": name, "unique": unique == 1, "origin": origin})
	}
	return indexes, rows.Err()
}

func loadSQLiteForeignKeys(ctx context.Context, db *sql.DB, table string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", quoteSQLiteIdentifier(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	foreignKeys := []map[string]any{}
	for rows.Next() {
		var id, seq int
		var refTable, column, refColumn, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &column, &refColumn, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		foreignKeys = append(foreignKeys, map[string]any{"name": fmt.Sprintf("fk_%d_%d", id, seq), "column": column, "referenced_table": refTable, "referenced_column": refColumn, "on_update": onUpdate, "on_delete": onDelete})
	}
	return foreignKeys, rows.Err()
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func defaultRunAppCommand(root string, timeout time.Duration, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "."}, args...)...)
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errors.New("main application command timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, truncate(string(data), 2000))
	}
	return data, nil
}

func resolveEnvValue(env map[string]string, envKey string, defaultExpr string) string {
	if value, ok := env[envKey]; ok {
		return value
	}
	if value, ok := os.LookupEnv(envKey); ok {
		return value
	}
	defaultExpr = strings.TrimSpace(strings.TrimSuffix(defaultExpr, ","))
	if strings.HasPrefix(defaultExpr, "Env(") {
		inner := strings.TrimPrefix(strings.TrimSuffix(defaultExpr, ")"), "Env(")
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			return resolveEnvValue(env, strings.Trim(strings.TrimSpace(parts[0]), `"`), parts[1])
		}
	}
	if strings.HasPrefix(defaultExpr, `"`) && strings.HasSuffix(defaultExpr, `"`) {
		return strings.Trim(defaultExpr, `"`)
	}
	return strings.Trim(defaultExpr, `"`)
}

func redactDatabaseValue(column string, value any) any {
	if isSecretKey(column) {
		return "[redacted]"
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	default:
		return typed
	}
}

func tailLines(path string, count int) []string {
	lines, err := readTailLines(path, count)
	if err != nil {
		return []string{}
	}
	return lines
}

func tailLogEntries(path string, count int) []string {
	if count <= 0 {
		return []string{}
	}
	lines, err := readTailLines(path, maxInt(count*8, count+20))
	if err != nil {
		return []string{}
	}
	entries := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(entries) == 0 || !isLogContinuation(line) {
			entries = append(entries, line)
			continue
		}
		entries[len(entries)-1] += "\n" + line
	}
	if len(entries) <= count {
		return entries
	}
	return entries[len(entries)-count:]
}

func readTailLines(path string, count int) ([]string, error) {
	if count <= 0 {
		return []string{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("log path must be a regular file")
	}
	if info.Size() == 0 {
		return []string{}, nil
	}
	data, err := readTailBytes(file, info.Size(), count)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 0 && int64(len(data)) < info.Size() {
		// 尾部窗口可能从一行中间开始；丢弃首个不完整行，保证返回内容是完整日志行。
		lines = lines[1:]
	}
	if len(lines) <= count {
		return lines, nil
	}
	return lines[len(lines)-count:], nil
}

func readTailBytes(file *os.File, size int64, count int) ([]byte, error) {
	const chunkSize int64 = 64 * 1024
	window := chunkSize
	for {
		if window > size {
			window = size
		}
		offset := size - window
		data := make([]byte, window)
		if _, err := file.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if offset == 0 || strings.Count(string(data), "\n") > count {
			return data, nil
		}
		window *= 2
	}
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func isLogContinuation(line string) bool {
	trimmed := strings.TrimSpace(line)
	if json.Valid([]byte(trimmed)) || strings.HasPrefix(trimmed, "[") {
		return false
	}
	if len(line) > 0 && (line[0] == '\t' || line[0] == ' ') {
		return true
	}
	if strings.HasPrefix(trimmed, "goroutine ") || strings.Contains(trimmed, ".go:") {
		return true
	}
	if regexp.MustCompile(`^[A-Za-z0-9_./*-]+\.[A-Za-z0-9_./*-]+\(`).FindStringIndex(trimmed) != nil {
		return true
	}
	return false
}

func redactValue(key string, value string) string {
	if isSecretKey(key) {
		return "[redacted]"
	}
	return value
}

func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "key")
}

func guessDatabaseEngine(root string) string {
	data, _ := os.ReadFile(filepath.Join(root, "config", "database.go"))
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "mysql") {
		return "mysql"
	}
	if strings.Contains(lower, "sqlite") {
		return "sqlite"
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
