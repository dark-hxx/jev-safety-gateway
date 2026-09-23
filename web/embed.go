// Package web embeds the admin frontend static assets.
package web

import (
	"embed"
	"io/fs"
)

// dist 是 Vue 工程（web/src）经 `npm run build` 产出的构建结果，随源码一并提交入库。
// 构建与运行阶段都不依赖 Node：`go build -o gateway ./cmd/gateway` 即可产出
// 含完整控制台界面的二进制。
//
//go:embed dist
var files embed.FS

// FS returns the embedded static assets rooted so that index.html is at "/".
func FS() fs.FS {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		panic("web: embedded dist directory missing: " + err.Error())
	}
	return sub
}
