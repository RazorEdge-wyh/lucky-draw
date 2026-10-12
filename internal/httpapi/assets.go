package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// 内置控制台直接编译进二进制：部署时只需一个可执行文件，
// 不依赖外部静态资源，也不受 CDN 可用性影响。
//
//go:embed web/index.html web/app.js
var webFS embed.FS

const appJSContentType = "text/javascript; charset=utf-8"

// asset 按路径取出内置静态资源，路径形如 "web/app.js"。
func asset(name string) ([]byte, error) {
	return fs.ReadFile(webFS, name)
}

// handleAsset 提供内置静态资源（目前只有 app.js）。
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasPrefix(name, "web/") {
		name = "web/" + name
	}
	// 只允许命中预先声明的两个文件，杜绝路径穿越。
	switch name {
	case "web/index.html", "web/app.js":
	default:
		http.NotFound(w, r)
		return
	}

	data, err := asset(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if name == "web/app.js" {
		w.Header().Set("Content-Type", appJSContentType)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, _ = w.Write(data)
}
