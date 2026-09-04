// Package web 内嵌前端静态资源。
// go:embed 在编译期把播放页打进可执行文件 → 单文件即完整程序，免额外部署 web 目录。
package web

import "embed"

// PlayerFS 内嵌的播放页静态资源（player/ 目录）
//
//go:embed player
var PlayerFS embed.FS
