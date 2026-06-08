package lens

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// BrowserLoggerScript 返回开发环境浏览器日志捕获脚本。
func BrowserLoggerScript() string {
	return `(function(){
  const endpoint = "/_prismgo_lens/browser-logs";
  const send = function(level,args){try{navigator.sendBeacon(endpoint, JSON.stringify({level:level,args:Array.from(args),at:new Date().toISOString()}));}catch(e){}};
  ["log","info","warn","error","table"].forEach(function(level){const original=console[level];console[level]=function(){send(level,arguments);return original.apply(console,arguments);};});
  window.addEventListener("error", function(event){send("error",[event.message,event.filename,event.lineno]);});
  window.addEventListener("unhandledrejection", function(event){send("error",[String(event.reason)]);});
})();`
}

// InjectBrowserLogger 只向 HTML 响应注入开发脚本，非 HTML 内容保持不变。
func InjectBrowserLogger(status int, header http.Header, body []byte) []byte {
	if status < 200 || status >= 300 {
		return body
	}
	if !strings.Contains(strings.ToLower(header.Get("Content-Type")), "text/html") {
		return body
	}
	script := []byte("<script>" + BrowserLoggerScript() + "</script>")
	closing := bytes.LastIndex(bytes.ToLower(body), []byte("</body>"))
	if closing < 0 {
		return append(body, script...)
	}
	next := make([]byte, 0, len(body)+len(script))
	next = append(next, body[:closing]...)
	next = append(next, script...)
	next = append(next, body[closing:]...)
	return next
}

// WriteBrowserLog 把浏览器日志写入 storage/logs/browser.log。
func WriteBrowserLog(root string, line []byte) error {
	target := filepath.Join(root, "storage", "logs", "browser.log")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(formatBrowserLogLine(line), '\n'))
	return err
}

// formatBrowserLogLine 把浏览器 sendBeacon JSON envelope 转换为稳定的单行日志。
// 设计背景：浏览器默认以 text/plain 发送 sendBeacon body，Lens 需要独立于 Content-Type 解析 payload。
func formatBrowserLogLine(line []byte) []byte {
	trimmed := bytes.TrimSpace(line)
	var payload struct {
		Level string            `json:"level"`
		Args  []json.RawMessage `json:"args"`
		At    string            `json:"at"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil || payload.Level == "" {
		return trimmed
	}
	parts := make([]string, 0, len(payload.Args))
	for _, arg := range payload.Args {
		parts = append(parts, formatBrowserLogArg(arg))
	}
	prefix := payload.Level + ":"
	if payload.At != "" {
		prefix = payload.At + " " + prefix
	}
	message := strings.TrimSpace(prefix + " " + strings.Join(parts, " "))
	return []byte(message)
}

// formatBrowserLogArg 稳定格式化 console 参数，避免 map 默认顺序或 Go 类型名泄露到日志中。
func formatBrowserLogArg(arg json.RawMessage) string {
	var value any
	if err := json.Unmarshal(arg, &value); err != nil {
		return strings.TrimSpace(string(arg))
	}
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return "null"
	case bool, float64:
		return fmt.Sprint(typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

// BrowserLogHandler 返回 Lens 包内开发日志接收端。
// 需求背景：生产路由不注册该 handler，仅供 dev proxy 或测试按需挂载到 /_prismgo_lens/browser-logs。
func BrowserLogHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(bytes.TrimSpace(body)) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := WriteBrowserLog(root, body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
