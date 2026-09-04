package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// RulesHandler 解析规则 CRUD（支持对接多条规则，按优先级匹配）
type RulesHandler struct {
	db *gorm.DB
}

// NewRulesHandler 创建规则处理器
func NewRulesHandler(db *gorm.DB) *RulesHandler {
	return &RulesHandler{db: db}
}

// List 处理 GET /admin/rules
func (h *RulesHandler) List(c echo.Context) error {
	var rules []models.ExtractRule
	if err := h.db.Order("priority DESC, id ASC").Find(&rules).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	// 把 DB 存储的 JSON 字符串还原为对象返回
	for i := range rules {
		rules[i].ConfigJSON = json.RawMessage(rules[i].RuleConfig)
	}
	return ok(c, rules)
}

// Create 处理 POST /admin/rules
func (h *RulesHandler) Create(c echo.Context) error {
	var rule models.ExtractRule
	if err := c.Bind(&rule); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if rule.Name == "" || rule.URLPattern == "" || rule.ExtractorType == "" {
		return fail(c, http.StatusBadRequest, "name / url_pattern / extractor_type 必填")
	}
	// 前端传的 rule_config 对象 → 转成字符串入库
	if len(rule.ConfigJSON) > 0 {
		rule.RuleConfig = string(rule.ConfigJSON)
	}
	if rule.Enabled == 0 {
		rule.Enabled = 1
	}
	if err := h.db.Create(&rule).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "创建失败: "+err.Error())
	}
	rule.ConfigJSON = json.RawMessage(rule.RuleConfig)
	return ok(c, rule)
}

// Update 处理 PUT /admin/rules/:id
func (h *RulesHandler) Update(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	var rule models.ExtractRule
	if err := h.db.First(&rule, id).Error; err != nil {
		return fail(c, http.StatusNotFound, "规则不存在")
	}
	var body models.ExtractRule
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if body.Name != "" {
		rule.Name = body.Name
	}
	if body.URLPattern != "" {
		rule.URLPattern = body.URLPattern
	}
	if body.ExtractorType != "" {
		rule.ExtractorType = body.ExtractorType
	}
	if len(body.ConfigJSON) > 0 {
		rule.RuleConfig = string(body.ConfigJSON)
	}
	if body.NeedProxy != 0 {
		rule.NeedProxy = body.NeedProxy
	}
	if body.Priority != 0 || body.Priority == 0 {
		rule.Priority = body.Priority
	}
	if body.Enabled != 0 {
		rule.Enabled = body.Enabled
	}
	if err := h.db.Save(&rule).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "更新失败: "+err.Error())
	}
	rule.ConfigJSON = json.RawMessage(rule.RuleConfig)
	return ok(c, rule)
}

// Delete 处理 DELETE /admin/rules/:id
func (h *RulesHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	if err := h.db.Delete(&models.ExtractRule{}, id).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "删除失败: "+err.Error())
	}
	return ok(c, map[string]any{"deleted": id})
}
