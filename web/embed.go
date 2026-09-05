// Package web 内嵌前端静态资源。
// go:embed 在编译期把播放页/管理后台打进可执行文件 → 单文件即完整程序，免额外部署 web 目录。
package web

import "embed"

// PlayerFS 内嵌的播放页静态资源（player/ 目录）
//
//go:embed player
var PlayerFS embed.FS

// AdminFS 内嵌的管理后台静态资源（admin/ 目录）
//
//go:embed admin
var AdminFS embed.FS
