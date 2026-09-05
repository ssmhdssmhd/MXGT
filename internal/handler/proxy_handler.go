package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// ProxyHandler /api/proxy/stream 视频流代理（解决跨域 / 防盗链 Referer）
type ProxyHandler struct {
	client *resty.Client
	db     *gorm.DB
}

// NewProxyHandler 创建代理处理器
func NewProxyHandler(db *gorm.DB) *ProxyHandler {
	return &ProxyHandler{
		db: db,
		client: resty.New().
			SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MXGT/"+proxyVersion),
	}
}

// Stream 处理 GET /api/proxy/stream?url=xxx&referer=xxx
// 流式转发（支持 Range 拖动进度条），自动注入 Referer / UA
func (h *ProxyHandler) Stream(c echo.Context) error {
	target := c.QueryParam("url")
	if target == "" {
		return c.String(http.StatusBadRequest, "缺少 url 参数")
	}

	start := time.Now()
	req := h.client.R().
		SetContext(c.Request().Context()).
		SetDoNotParseResponse(true)

	// 注入 Referer（解决防盗链），默认取目标域名
	if ref := c.QueryParam("referer"); ref != "" {
		req.SetHeader("Referer", ref)
	}

	// 透传 Range 请求头（m3u8 拖动进度条关键）
	if r := c.Request().Header.Get("Range"); r != "" {
		req.SetHeader("Range", r)
	}

	resp, err := req.Get(target)
	dur := int(time.Since(start).Milliseconds())
	if err != nil {
		RecordCall(h.db, "proxy", 0, 0, 0, dur, 0, c.RealIP(), target, err.Error())
		return c.String(http.StatusBadGateway, "代理请求失败: "+err.Error())
	}
	status := int8(1)
	if resp.StatusCode() != 200 {
		status = 0
	}
	RecordCall(h.db, "proxy", 0, 0, status, dur, 0, c.RealIP(), target, "")
	defer resp.RawBody().Close()

	// CORS + 透传 Content-Type
	hdr := c.Response().Header()
	hdr.Set("Access-Control-Allow-Origin", "*")
	hdr.Set("Cache-Control", "no-store")
	if ct := resp.Header().Get("Content-Type"); ct != "" {
		hdr.Set("Content-Type", ct)
	}
	if status := resp.StatusCode(); status != 200 {
		c.Response().WriteHeader(status)
	}
	_, _ = io.Copy(c.Response(), resp.RawBody())
	return nil
}

// proxyVersion 版本号占位（构建时由 ldflags 注入，见 cmd/server）
const proxyVersion = "v0.0.13"
