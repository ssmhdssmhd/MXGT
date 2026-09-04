package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// SourcesHandler 采集源 CRUD（支持对接多个采集源）
type SourcesHandler struct {
	db *gorm.DB
}

// NewSourcesHandler 创建采集源处理器
func NewSourcesHandler(db *gorm.DB) *SourcesHandler {
	return &SourcesHandler{db: db}
}

// SourceDTO 采集源绑定 DTO（extract_rules 支持对象或字符串）
type SourceDTO struct {
	Name         string          `json:"name"`
	SourceType   string          `json:"source_type"`
	FetchURL     string          `json:"fetch_url"`
	Method       string          `json:"method"`
	Headers      json.RawMessage `json:"headers"`
	ExtractRules json.RawMessage `json:"extract_rules"`
	Priority     int             `json:"priority"`
	Enabled      int8            `json:"enabled"`
}

// toModel 转数据库模型
func (d *SourceDTO) toModel() *models.Source {
	src := &models.Source{
		Name:       d.Name,
		SourceType: d.SourceType,
		FetchURL:   d.FetchURL,
		Method:     d.Method,
		Priority:   d.Priority,
		Enabled:    d.Enabled,
	}
	if len(d.Headers) > 0 {
		src.HeadersJSON = string(d.Headers)
	}
	if len(d.ExtractRules) > 0 {
		src.ExtractRules = string(d.ExtractRules)
	}
	return src
}

// List 处理 GET /admin/sources
func (h *SourcesHandler) List(c echo.Context) error {
	var sources []models.Source
	if err := h.db.Order("priority DESC, id ASC").Find(&sources).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	for i := range sources {
		sources[i].Headers = json.RawMessage(sources[i].HeadersJSON)
	}
	return ok(c, sources)
}

// Create 处理 POST /admin/sources
func (h *SourcesHandler) Create(c echo.Context) error {
	var dto SourceDTO
	if err := c.Bind(&dto); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if dto.Name == "" || dto.FetchURL == "" {
		return fail(c, http.StatusBadRequest, "name / fetch_url 必填")
	}
	if dto.SourceType == "" {
		dto.SourceType = "api"
	}
	src := dto.toModel()
	if src.Enabled == 0 {
		src.Enabled = 1
	}
	if err := h.db.Create(src).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "创建失败: "+err.Error())
	}
	src.Headers = json.RawMessage(src.HeadersJSON)
	return ok(c, src)
}

// Update 处理 PUT /admin/sources/:id
func (h *SourcesHandler) Update(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	var src models.Source
	if err := h.db.First(&src, id).Error; err != nil {
		return fail(c, http.StatusNotFound, "采集源不存在")
	}
	var dto SourceDTO
	if err := c.Bind(&dto); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if dto.Name != "" {
		src.Name = dto.Name
	}
	if dto.FetchURL != "" {
		src.FetchURL = dto.FetchURL
	}
	if dto.SourceType != "" {
		src.SourceType = dto.SourceType
	}
	if dto.Method != "" {
		src.Method = dto.Method
	}
	if len(dto.Headers) > 0 {
		src.HeadersJSON = string(dto.Headers)
	}
	if len(dto.ExtractRules) > 0 {
		src.ExtractRules = string(dto.ExtractRules)
	}
	if dto.Priority != 0 {
		src.Priority = dto.Priority
	}
	if dto.Enabled != 0 {
		src.Enabled = dto.Enabled
	}
	if err := h.db.Save(&src).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "更新失败: "+err.Error())
	}
	src.Headers = json.RawMessage(src.HeadersJSON)
	return ok(c, src)
}

// Delete 处理 DELETE /admin/sources/:id
func (h *SourcesHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	if err := h.db.Delete(&models.Source{}, id).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "删除失败: "+err.Error())
	}
	return ok(c, map[string]any{"deleted": id})
}
