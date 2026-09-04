// Package router 路由注册：分层组织 API（健康 / 解析 / 代理 / 静态资源）
package router

import (
	"io/fs"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/ssmhdssmhd/MXGT/internal/cache"
	"github.com/ssmhdssmhd/MXGT/internal/config"
	"github.com/ssmhdssmhd/MXGT/internal/handler"
	mw "github.com/ssmhdssmhd/MXGT/internal/middleware"
	"github.com/ssmhdssmhd/MXGT/internal/service"
	"github.com/ssmhdssmhd/MXGT/web"
	"gorm.io/gorm"
)

// New 组装 Echo 应用（CORS + API + 内嵌播放页静态资源）
func New(db *gorm.DB, c cache.Cache, cfg *config.Config, version string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// 中间件
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	// 业务服务（分层）
	resolveSvc := service.NewResolveService(db, c, cfg)

	// API 层
	api := e.Group("/api")
	api.GET("/health", handler.NewHealthHandler(db, c, version).Check)
	api.GET("/resolve", handler.NewResolveHandler(resolveSvc).Resolve)
	api.GET("/proxy/stream", handler.NewProxyHandler().Stream)

	// 对外：苹果 CMS v10 兼容输出（ac=list / detail / search / play）
	e.GET("/api.php/provide/vod/", handler.NewCMSHandler(db).Provide)

	// 管理后台层（JWT 保护；规则/采集源支持对接多条，可动态增删改）
	admin := handler.NewAdminHandler(db)
	rules := handler.NewRulesHandler(db)
	sources := handler.NewSourcesHandler(db)
	syncSvc := service.NewSyncService(db)

	e.POST("/admin/login", admin.Login)
	adminGroup := e.Group("/admin", mw.JWTAuth)
	adminGroup.GET("/rules", rules.List)
	adminGroup.POST("/rules", rules.Create)
	adminGroup.PUT("/rules/:id", rules.Update)
	adminGroup.DELETE("/rules/:id", rules.Delete)

	// 采集源管理（多源对接）
	adminGroup.GET("/sources", sources.List)
	adminGroup.POST("/sources", sources.Create)
	adminGroup.PUT("/sources/:id", sources.Update)
	adminGroup.DELETE("/sources/:id", sources.Delete)

	// 采集同步（多源采集 → 匹配 → 入库）
	adminGroup.POST("/sync", handler.NewSyncHandler(syncSvc).Sync)

	// 静态资源（go:embed 内嵌播放页，单文件运行）
	// 注意：/api/* 精确路由优先于 /* 静态通配
	if sub, err := fs.Sub(web.PlayerFS, "player"); err == nil {
		e.StaticFS("/", sub)
	}

	return e
}
