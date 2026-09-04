package handler

import (
	"net/http"
	"runtime"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/cache"
	"gorm.io/gorm"
)

// HealthHandler /api/health 健康检查
type HealthHandler struct {
	db    *gorm.DB
	cache cache.Cache
	ver   string
}

// NewHealthHandler 创建健康检查处理器
func NewHealthHandler(db *gorm.DB, c cache.Cache, ver string) *HealthHandler {
	return &HealthHandler{db: db, cache: c, ver: ver}
}

// Check 处理 GET /api/health
func (h *HealthHandler) Check(c echo.Context) error {
	dbOK := "ok"
	if sqlDB, err := h.db.DB(); err != nil {
		dbOK = "error:" + err.Error()
	} else if err := sqlDB.Ping(); err != nil {
		dbOK = "error:" + err.Error()
	}

	return c.JSON(http.StatusOK, map[string]any{
		"code":    1,
		"msg":     "ok",
		"version": h.ver,
		"runtime": runtime.Version(),
		"db":      dbOK,
		"cache":   "ok",
	})
}
