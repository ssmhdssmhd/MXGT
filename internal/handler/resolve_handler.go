// Package handler HTTP 处理器
package handler

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/service"
	"gorm.io/gorm"
)

// ResolveHandler /api/resolve 解析接口
type ResolveHandler struct {
	svc *service.ResolveService
	db  *gorm.DB
}

// NewResolveHandler 创建解析处理器
func NewResolveHandler(db *gorm.DB, svc *service.ResolveService) *ResolveHandler {
	return &ResolveHandler{svc: svc, db: db}
}

// Resolve 处理 GET /api/resolve?url=xxx
func (h *ResolveHandler) Resolve(c echo.Context) error {
	rawURL := c.QueryParam("url")
	if rawURL == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"code": 0, "msg": "缺少 url 参数",
		})
	}

	start := time.Now()
	result, err := h.svc.Resolve(c.Request().Context(), rawURL)
	dur := int(time.Since(start).Milliseconds())
	ip := c.RealIP()

	if err != nil {
		RecordCall(h.db, "resolve", 0, 0, 0, dur, 0, ip, rawURL, err.Error())
		return c.JSON(http.StatusNotFound, map[string]any{
			"code": 0, "msg": err.Error(),
		})
	}

	cacheHit := int8(0)
	if result.CacheHit {
		cacheHit = 1
	}
	RecordCall(h.db, "resolve", result.RuleID, 0, 1, dur, cacheHit, ip, rawURL, "")

	return c.JSON(http.StatusOK, map[string]any{
		"code": 1,
		"msg":  "ok",
		"data": result,
	})
}
