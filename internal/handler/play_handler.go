package handler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/service"
)

// PlayHandler 统一播放入口（核心两种功能：官方链接搜资源替换 / 直链去广告去插播）
type PlayHandler struct {
	svc *service.PlayService
}

// NewPlayHandler 创建统一播放入口处理器
func NewPlayHandler(svc *service.PlayService) *PlayHandler {
	return &PlayHandler{svc: svc}
}

// Play 处理 GET /api/play?url=&title=&ep=
//   - url 必填：用户输入的链接（官方页 / m3u8 / mp4 / flv）
//   - title 可选：剧名（官方链接走资源替换时用于搜索）
//   - ep 可选：集数（默认第 1 集）
func (h *PlayHandler) Play(c echo.Context) error {
	rawURL := c.QueryParam("url")
	if rawURL == "" {
		return fail(c, http.StatusBadRequest, "url 必填")
	}
	title := c.QueryParam("title")
	ep, _ := strconv.Atoi(c.QueryParam("ep"))

	res, err := h.svc.Play(c.Request().Context(), rawURL, title, ep)
	if err != nil {
		return fail(c, http.StatusBadGateway, "处理失败: "+err.Error())
	}
	return ok(c, res)
}
