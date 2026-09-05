package handler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// VodsHandler 影片管理处理器：列表 / 搜索 / 详情（含集数）
type VodsHandler struct {
	db *gorm.DB
}

// NewVodsHandler 创建影片管理处理器
func NewVodsHandler(db *gorm.DB) *VodsHandler {
	return &VodsHandler{db: db}
}

// List 处理 GET /admin/vods?page=1&size=20&keyword=xxx
func (h *VodsHandler) List(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	size, _ := strconv.Atoi(c.QueryParam("size"))
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	keyword := c.QueryParam("keyword")

	q := h.db.Model(&models.Vod{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR alias LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}

	var list []models.Vod
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, map[string]any{
		"page":       page,
		"size":       size,
		"total":      total,
		"items":      list,
		"total_page": (int(total) + size - 1) / size,
	})
}