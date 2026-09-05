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
	"github.com/ssmhdssmhd/MXGT/internal/models"
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
	api.GET("/resolve", handler.NewResolveHandler(db, resolveSvc).Resolve)
	api.GET("/proxy/stream", handler.NewProxyHandler(db).Stream)

	// 对外：苹果 CMS v10 兼容输出（ac=list / detail / search / play）
	e.GET("/api.php/provide/vod/", handler.NewCMSHandler(db).Provide)

	// 管理后台层（JWT 保护；规则/采集源支持对接多条，可动态增删改）
	admin := handler.NewAdminHandler(db)
	rules := handler.NewRulesHandler(db)
	sources := handler.NewSourcesHandler(db)
	syncSvc := service.NewSyncService(db)
	settings := handler.NewSettingsHandler(db)
	mappings := handler.NewMappingsHandler(db)

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

	// 前端设置（单行表：播放页伪装路径 / 参数别名 / 皮肤等）
	adminGroup.GET("/settings", settings.Get)
	adminGroup.PUT("/settings", settings.Update)

	// 站点映射（七大站预置 + 自定义）
	adminGroup.GET("/mappings", mappings.List)
	adminGroup.POST("/mappings", mappings.Create)
	adminGroup.PUT("/mappings/:id", mappings.Update)
	adminGroup.DELETE("/mappings/:id", mappings.Delete)
	adminGroup.POST("/mapping/test", mappings.Test)

	// 公开：播放页前端设置（播放页读取，无需登录）
	e.GET("/api/settings", settings.PublicGet)

	// 仪表盘统计（调用日志 / 趋势 / TOP）
	stats := handler.NewStatsHandler(db)
	adminGroup.GET("/stats/overview", stats.Overview)
	adminGroup.GET("/stats/trends", stats.Trends)
	adminGroup.GET("/stats/rules-top", stats.RulesTop)
	adminGroup.GET("/stats/sources-top", stats.SourcesTop)
	adminGroup.GET("/call-logs", stats.CallLogs)

	// 分析引擎（URL 资源类型识别）
	analysis := handler.NewAnalysisHandler(db)
	adminGroup.POST("/analysis/test", analysis.Test)
	adminGroup.GET("/analysis/settings", analysis.GetSettings)
	adminGroup.PUT("/analysis/settings", analysis.UpdateSettings)

	// 匹配策略（AI/规则双通道 + 阈值 + 直接资源去插播）
	matching := handler.NewMatchingHandler(db)
	adminGroup.GET("/matching/settings", matching.GetSettings)
	adminGroup.PUT("/matching/settings", matching.UpdateSettings)
	adminGroup.POST("/matching/test", matching.Test)

	// 管理后台 UI：/admin-ui → admin/index.html（独立前缀，避免与 /admin API 冲突）
	e.GET("/admin-ui", func(c echo.Context) error {
		data, err := fs.ReadFile(web.AdminFS, "admin/index.html")
		if err != nil {
			return c.String(http.StatusNotFound, "管理后台不存在")
		}
		c.Response().Header().Set(echo.HeaderContentType, "text/html; charset=utf-8")
		return c.String(http.StatusOK, string(data))
	})

	// 播放页静态资源 root
	if sub, err := fs.Sub(web.PlayerFS, "player"); err == nil {
		e.StaticFS("/", sub)

		// 伪装路径动态注册：从 frontend_settings 读取 play_path（如 /mx.php /play.php）
		var fsSetting models.FrontendSetting
		if db.First(&fsSetting, 1).Error == nil &&
			fsSetting.PlayPath != "" && fsSetting.PlayPath != "/" && fsSetting.PlayPath != "/index.html" {
			path := fsSetting.PlayPath
			e.GET(path, func(c echo.Context) error {
				data, err := fs.ReadFile(sub, "index.html")
				if err != nil {
					return c.String(http.StatusNotFound, "播放页不存在")
				}
				c.Response().Header().Set(echo.HeaderContentType, "text/html; charset=utf-8")
				return c.String(http.StatusOK, string(data))
			})
		}
	}

	return e
}
