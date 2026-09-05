package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// SettingsHandler 前端设置（单行表）处理器：读取 / 更新播放页伪装路径等配置
type SettingsHandler struct {
	db *gorm.DB
}

// NewSettingsHandler 创建前端设置处理器
func NewSettingsHandler(db *gorm.DB) *SettingsHandler {
	return &SettingsHandler{db: db}
}

// DefaultSettings 默认前端设置（单行表为空时兜底）
func DefaultSettings() models.FrontendSetting {
	return models.FrontendSetting{
		ID:          1,
		PlayPath:    "/",
		URLParam:    "url",
		AliasParams: "video,src,link,v,u",
		Skin:        "default",
		PlayerType:  "dplayer",
		CrossOrigin: 1,
		CacheTTL:    3600,
	}
}

// Get 读取单行设置（不存在则返回默认）
func (h *SettingsHandler) Get(c echo.Context) error {
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, s)
}

// PublicGet 公开读取（播放页前端用，无需登录）
func (h *SettingsHandler) PublicGet(c echo.Context) error {
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, s)
}

// Update 更新单行设置（PUT /admin/settings）
func (h *SettingsHandler) Update(c echo.Context) error {
	var req models.FrontendSetting
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	// 仅覆盖非空字段，保持单行 id=1
	if req.PlayPath != "" {
		s.PlayPath = req.PlayPath
	}
	if req.URLParam != "" {
		s.URLParam = req.URLParam
	}
	if req.AliasParams != "" {
		s.AliasParams = req.AliasParams
	}
	if req.Skin != "" {
		s.Skin = req.Skin
	}
	if req.PlayerType != "" {
		s.PlayerType = req.PlayerType
	}
	if req.LogoURL != "" {
		s.LogoURL = req.LogoURL
	}
	if req.APIBase != "" {
		s.APIBase = req.APIBase
	}
	if req.FooterText != "" {
		s.FooterText = req.FooterText
	}
	if req.Beian != "" {
		s.Beian = req.Beian
	}
	if req.CrossOrigin != 0 {
		s.CrossOrigin = req.CrossOrigin
	}
	if req.CacheTTL != 0 {
		s.CacheTTL = req.CacheTTL
	}
	if err := h.db.Save(&s).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
	}
	return ok(c, s)
}

// load 读取单行设置，不存在则创建默认行
func (h *SettingsHandler) load() (models.FrontendSetting, error) {
	var s models.FrontendSetting
	if err := h.db.First(&s, 1).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			s = DefaultSettings()
			if err := h.db.Create(&s).Error; err != nil {
				return s, err
			}
			return s, nil
		}
		return s, err
	}
	return s, nil
}
