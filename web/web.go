// Package web 内嵌前端构建产物（web/dist 由 make web 生成）。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS 返回前端产物文件系统（根为 dist 目录）。
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// Ready 判断前端产物是否已构建（存在 index.html）。
func Ready() bool {
	if _, err := fs.Stat(distFS, "dist/index.html"); err == nil {
		return true
	}
	// make web 产物在子目录（vite 多层资源），index.html 为准
	return false
}
