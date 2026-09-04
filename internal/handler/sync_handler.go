package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/service"
)

// SyncHandler 采集同步接口
type SyncHandler struct {
	svc *service.SyncService
}

// NewSyncHandler 创建同步处理器
func NewSyncHandler(svc *service.SyncService) *SyncHandler {
	return &SyncHandler{svc: svc}
}

// SyncRequest 同步请求
type SyncRequest struct {
	Keyword string `json:"keyword"` // 采集关键词，空则采集全量/最新
}

// Sync 处理 POST /admin/sync（触发多源采集 → 匹配 → 入库）
func (h *SyncHandler) Sync(c echo.Context) error {
	var req SyncRequest
	_ = c.Bind(&req)

	results, err := h.svc.Sync(c.Request().Context(), req.Keyword)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}

	totalFetched, totalCreated, totalEp := 0, 0, 0
	for _, r := range results {
		totalFetched += r.Fetched
		totalCreated += r.Created
		totalEp += r.Episodes
	}
	return ok(c, map[string]any{
		"sources": results,
		"total": map[string]int{
			"fetched": totalFetched, "created": totalCreated, "episodes": totalEp,
		},
	})
}
