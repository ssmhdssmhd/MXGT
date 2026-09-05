package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/PaesslerAG/jsonpath"
	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// MappingsHandler 站点映射（七大站预置 + 自定义）处理器
type MappingsHandler struct {
	db *gorm.DB
}

// NewMappingsHandler 创建站点映射处理器
func NewMappingsHandler(db *gorm.DB) *MappingsHandler {
	return &MappingsHandler{db: db}
}

// List 处理 GET /admin/mappings（按优先级倒序）
func (h *MappingsHandler) List(c echo.Context) error {
	var list []models.SiteMapping
	if err := h.db.Order("priority DESC, id ASC").Find(&list).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, list)
}

// Create 处理 POST /admin/mappings（新增自定义站点映射）
func (h *MappingsHandler) Create(c echo.Context) error {
	var m models.SiteMapping
	if err := c.Bind(&m); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if m.SiteCode == "" || m.SiteName == "" || m.SiteDomain == "" || m.NameField == "" {
		return fail(c, http.StatusBadRequest, "site_code / site_name / site_domain / name_field 必填")
	}
	if m.Enabled == 0 {
		m.Enabled = 1
	}
	if err := h.db.Create(&m).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "创建失败: "+err.Error())
	}
	return ok(c, m)
}

// Update 处理 PUT /admin/mappings/:id
func (h *MappingsHandler) Update(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	var m models.SiteMapping
	if err := h.db.First(&m, id).Error; err != nil {
		return fail(c, http.StatusNotFound, "映射不存在")
	}
	var req models.SiteMapping
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	// 字段级别覆盖
	if req.SiteName != "" {
		m.SiteName = req.SiteName
	}
	if req.SiteDomain != "" {
		m.SiteDomain = req.SiteDomain
	}
	if req.SiteIcon != "" {
		m.SiteIcon = req.SiteIcon
	}
	if req.NameField != "" {
		m.NameField = req.NameField
	}
	if req.AliasField != "" {
		m.AliasField = req.AliasField
	}
	if req.CoverField != "" {
		m.CoverField = req.CoverField
	}
	if req.YearField != "" {
		m.YearField = req.YearField
	}
	if req.RegionField != "" {
		m.RegionField = req.RegionField
	}
	if req.CategoryField != "" {
		m.CategoryField = req.CategoryField
	}
	if req.RemarkField != "" {
		m.RemarkField = req.RemarkField
	}
	if req.EpisodesPath != "" {
		m.EpisodesPath = req.EpisodesPath
	}
	if req.EpisodeNoRule != "" {
		m.EpisodeNoRule = req.EpisodeNoRule
	}
	if req.EpisodeURLRule != "" {
		m.EpisodeURLRule = req.EpisodeURLRule
	}
	if req.ExtractRuleID != 0 {
		m.ExtractRuleID = req.ExtractRuleID
	}
	if req.Priority != 0 {
		m.Priority = req.Priority
	}
	if req.Enabled != 0 {
		m.Enabled = req.Enabled
	}
	if err := h.db.Save(&m).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "更新失败: "+err.Error())
	}
	return ok(c, m)
}

// Delete 处理 DELETE /admin/mappings/:id（预置站点不可删）
func (h *MappingsHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	var m models.SiteMapping
	if err := h.db.First(&m, id).Error; err != nil {
		return fail(c, http.StatusNotFound, "映射不存在")
	}
	if m.IsBuiltin == 1 {
		return fail(c, http.StatusForbidden, "预置站点不可删除")
	}
	if err := h.db.Delete(&models.SiteMapping{}, id).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "删除失败: "+err.Error())
	}
	return ok(c, map[string]any{"deleted": id})
}

// TestRequest 字段映射测试请求
type TestRequest struct {
	SiteCode  string `json:"site_code"`  // 可选：从预置映射读取规则
	RawJSON   string `json:"raw_json"`   // 原始 JSON 数据（字符串）
	NameField string `json:"name_field"`
	EpisodesPath string `json:"episodes_path"`
}

// Test 处理 POST /admin/mapping/test（验证 JSONPath 字段映射提取结果）
func (h *MappingsHandler) Test(c echo.Context) error {
	var req TestRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if req.RawJSON == "" {
		return fail(c, http.StatusBadRequest, "raw_json 必填")
	}
	// 若给了 site_code，自动加载预置映射规则
	if req.SiteCode != "" {
		var m models.SiteMapping
		if err := h.db.Where("site_code = ?", req.SiteCode).First(&m).Error; err == nil {
			if req.NameField == "" {
				req.NameField = m.NameField
			}
			if req.EpisodesPath == "" {
				req.EpisodesPath = m.EpisodesPath
			}
		}
	}

	// 解析原始 JSON
	var doc any
	if err := json.Unmarshal([]byte(req.RawJSON), &doc); err != nil {
		return fail(c, http.StatusBadRequest, "raw_json 不是合法 JSON: "+err.Error())
	}

	result := map[string]any{}

	// 提取剧名
	if req.NameField != "" {
		if v, err := extractByRule(req.NameField, doc); err == nil {
			result["name"] = v
		} else {
			result["name"] = map[string]any{"error": err.Error()}
		}
	}
	// 提取集数数组
	if req.EpisodesPath != "" {
		if v, err := extractByRule(req.EpisodesPath, doc); err == nil {
			result["episodes"] = v
		} else {
			result["episodes"] = map[string]any{"error": err.Error()}
		}
	}

	return ok(c, result)
}

// extractByRule 按规则提取：jsonpath:xxx / regex:xxx / 原样 jsonpath
func extractByRule(rule string, doc any) (any, error) {
	rule = strings.TrimSpace(rule)
	path := rule
	if strings.HasPrefix(rule, "jsonpath:") {
		path = strings.TrimPrefix(rule, "jsonpath:")
	}
	if strings.HasPrefix(rule, "regex:") {
		return nil, nil // regex 需字符串上下文，测试接口仅演示 jsonpath
	}
	return jsonpath.Get(path, doc)
}
