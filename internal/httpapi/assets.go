package httpapi

import (
	"embed"
	"io/fs"
)

// 页面与脚本直接编译进二进制：部署时只需一个可执行文件，
// 不依赖外部静态资源，也不受 CDN / 网络可用性影响。
//
//go:embed web/index.html web/app.js
var webFS embed.FS

// asset 按路径取出内置静态资源，路径形如 "web/app.js"。
func asset(name string) ([]byte, error) {
	return fs.ReadFile(webFS, name)
}
