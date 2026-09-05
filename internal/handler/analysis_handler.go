package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/analyzer"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// AnalysisHandler 分析引擎处理器
type AnalysisHandler struct {
	db *gorm.DB
}

// NewAnalysisHandler 创建分析引擎处理器
func NewAnalysisHandler(db *gorm.DB) *AnalysisHandler {
	return &AnalysisHandler{db: db}
}

// Test 处理 POST /admin/analysis/test（测试 URL 的资源类型识别）
func (h *AnalysisHandler) Test(c echo.Context) error {
	var req struct {
		URL string `json:"url"`
	}
	if err := c.Bind(&req); err != nil || req.URL == "" {
		return fail(c, http.StatusBadRequest, "url 必填")
	}
	return ok(c, analyzer.Parse(req.URL))
}

// GetSettings 读取分析设置（单行，不存在则返回默认）
func (h *AnalysisHandler) GetSettings(c echo.Context) error {
	s, err := loadAnalysisSetting(h.db)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, s)
}

// UpdateSettings 更新分析设置
func (h *AnalysisHandler) UpdateSettings(c echo.Context) error {
	var req models.AnalysisSetting
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	s, err := loadAnalysisSetting(h.db)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	if req.Enabled != 0 {
		s.Enabled = req.Enabled
	}
	if req.Priority != "" {
		s.Priority = req.Priority
	}
	if req.AIEnabled != 0 {
		s.AIEnabled = req.AIEnabled
	}
	if req.AIProvider != "" {
		s.AIProvider = req.AIProvider
	}
	if req.AIAPIKey != "" {
		s.AIAPIKey = req.AIAPIKey
	}
	if req.AIEndpoint != "" {
		s.AIEndpoint = req.AIEndpoint
	}
	if req.UnknownMode != "" {
		s.UnknownMode = req.UnknownMode
	}
	if err := h.db.Save(&s).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
	}
	return ok(c, s)
}

// loadAnalysisSetting 读取分析设置，不存在则建默认行
func loadAnalysisSetting(db *gorm.DB) (models.AnalysisSetting, error) {
	var s models.AnalysisSetting
	if err := db.First(&s, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s = models.DefaultAnalysisSetting()
			if err := db.Create(&s).Error; err != nil {
				return s, err
			}
			return s, nil
		}
		return s, err
	}
	return s, nil
}