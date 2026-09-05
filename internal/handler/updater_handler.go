package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"github.com/ssmhdssmhd/MXGT/internal/updater"
	"gorm.io/gorm"
)

// jsonUnmarshal 解析 JSON 字符串
func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

// jsonRawArray 序列化字符串切片为 RawMessage
func jsonRawArray(arr []string) json.RawMessage {
	b, _ := json.Marshal(arr)
	return b
}

// UpdaterHandler 自动更新处理器（M14）：配置 / 镜像测速 / 检查更新 / 一键下载 / 日志
type UpdaterHandler struct {
	db      *gorm.DB
	version string
}

// NewUpdaterHandler 创建自动更新处理器
func NewUpdaterHandler(db *gorm.DB, version string) *UpdaterHandler {
	return &UpdaterHandler{db: db, version: version}
}

// GetConfig 读取更新配置
func (h *UpdaterHandler) GetConfig(c echo.Context) error {
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	// 输出 mirrors 数组
	if s.MirrorsJSON != "" {
		_ = jsonUnmarshal(s.MirrorsJSON, &s.Mirrors)
	} else {
		s.Mirrors = jsonRawArray(updater.DefaultMirrors())
	}
	return ok(c, s)
}

// UpdateConfig 更新配置（PUT /admin/update/config）
func (h *UpdaterHandler) UpdateConfig(c echo.Context) error {
	var req models.UpdaterConfig
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	if req.Repo != "" {
		s.Repo = req.Repo
	}
	if len(req.Mirrors) > 0 {
		s.MirrorsJSON = string(req.Mirrors)
	}
	if req.AutoCheck != 0 {
		s.AutoCheck = req.AutoCheck
	}
	if req.CurrentVersion != "" {
		s.CurrentVersion = req.CurrentVersion
	}
	if err := h.db.Save(&s).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
	}
	if s.MirrorsJSON != "" {
		_ = jsonUnmarshal(s.MirrorsJSON, &s.Mirrors)
	}
	return ok(c, s)
}

// MirrorSpeed 处理 POST /admin/update/mirror-speed（并发测速所有镜像）
func (h *UpdaterHandler) MirrorSpeed(c echo.Context) error {
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	mirrors := h.mirrorList(s)
	results := updater.BenchmarkMirrors(context.Background(), mirrors, 5*time.Second)
	return ok(c, map[string]any{
		"results": results,
		"fastest": updater.Fastest(results),
	})
}

// Check 处理 POST /admin/update/check（检查远端版本）
func (h *UpdaterHandler) Check(c echo.Context) error {
	s, err := h.load()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	current := s.CurrentVersion
	if current == "" {
		current = h.version
	}
	info, err := updater.CheckLatest(context.Background(), s.Repo, current)
	if err != nil {
		return fail(c, http.StatusBadGateway, err.Error())
	}
	updater.Version = h.version
	return ok(c, info)
}

// Download 处理 POST /admin/update/download（一键下载并替换可执行文件）
func (h *UpdaterHandler) Download(c echo.Context) error {
	var req struct {
		Version   string `json:"version"`
		AssetsURL string `json:"assets_url"` // 下载基地址（Releases 下载目录）
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if req.AssetsURL == "" {
		return fail(c, http.StatusBadRequest, "assets_url 必填")
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	asset := assetFileName()
	res, err := updater.Install(context.Background(), exe, req.AssetsURL, asset)
	if err != nil {
		h.addLog(req.Version, "failed", err.Error())
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	h.addLog(req.Version, "success", res.Message)
	return ok(c, res)
}

// Logs 处理 GET /admin/update/logs（最近更新日志）
func (h *UpdaterHandler) Logs(c echo.Context) error {
	var list []models.UpdateLog
	if err := h.db.Order("id DESC").Limit(30).Find(&list).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, list)
}

func (h *UpdaterHandler) addLog(version, status, msg string) {
	if version == "" {
		version = h.version
	}
	_ = h.db.Create(&models.UpdateLog{Version: version, Status: status, Message: msg})
}

func (h *UpdaterHandler) mirrorList(s models.UpdaterConfig) []string {
	if s.MirrorsJSON == "" {
		return updater.DefaultMirrors()
	}
	var mirrors []string
	if jsonUnmarshal(s.MirrorsJSON, &mirrors) != nil || len(mirrors) == 0 {
		return updater.DefaultMirrors()
	}
	return mirrors
}

func (h *UpdaterHandler) load() (models.UpdaterConfig, error) {
	var s models.UpdaterConfig
	if err := h.db.First(&s, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s = models.DefaultUpdaterConfig(h.version)
			if err := h.db.Create(&s).Error; err != nil {
				return s, err
			}
			return s, nil
		}
		return s, err
	}
	return s, nil
}

// assetFileName 生成当前平台的 Release 资产文件名（与 .github/workflows 命名一致）
func assetFileName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return "mxgt-" + runtime.GOOS + "-" + runtime.GOARCH + ext
}