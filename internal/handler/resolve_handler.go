// Package handler HTTP 处理器
package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/service"
)

// ResolveHandler /api/resolve 解析接口
type ResolveHandler struct {
	svc *service.ResolveService
}

// NewResolveHandler 创建解析处理器
func NewResolveHandler(svc *service.ResolveService) *ResolveHandler {
	return &ResolveHandler{svc: svc}
}

// Resolve 处理 GET /api/resolve?url=xxx
func (h *ResolveHandler) Resolve(c echo.Context) error {
	rawURL := c.QueryParam("url")
	if rawURL == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"code": 0, "msg": "缺少 url 参数",
		})
	}

	result, err := h.svc.Resolve(c.Request().Context(), rawURL)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{
			"code": 0, "msg": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"code": 1,
		"msg":  "ok",
		"data": result,
	})
}
