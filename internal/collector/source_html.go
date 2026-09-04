package collector

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/ssmhdssmhd/MXGT/internal/models"
)

// HTMLCollector HTML 页面源采集器（正则提取）
type HTMLCollector struct {
	source *models.Source
	client *resty.Client
}

// newHTMLCollector 创建 HTML 采集器
func newHTMLCollector(source *models.Source) *HTMLCollector {
	return &HTMLCollector{
		source: source,
		client: resty.New().
			SetTimeout(15e9).
			SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MXGT/collector"),
	}
}

// Name 采集器类型名
func (h *HTMLCollector) Name() string { return "html" }

// Fetch 拉取 HTML 页面，正则提取标题 + m3u8 直链
func (h *HTMLCollector) Fetch(ctx context.Context, keyword string) ([]RawItem, error) {
	raw := strings.ReplaceAll(h.source.FetchURL, "{keyword}", keyword)
	resp, err := h.client.R().SetContext(ctx).Get(raw)
	if err != nil {
		return nil, fmt.Errorf("请求页面失败: %w", err)
	}
	content := resp.String()

	// 提取页面标题
	titleRe := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	title := ""
	if m := titleRe.FindStringSubmatch(content); len(m) > 1 {
		title = strings.TrimSpace(m[1])
	}

	// 提取所有 m3u8 直链
	urlRe := regexp.MustCompile(`https?://[^"'\s<>]+\.m3u8[^"'\s<>]*`)
	matches := urlRe.FindAllString(content, -1)

	eps := make([]RawEpisode, 0, len(matches))
	for i, m := range matches {
		eps = append(eps, RawEpisode{Name: fmt.Sprintf("第%d集", i+1), URL: m})
	}

	item := RawItem{
		Name:     title,
		Episodes: eps,
	}
	if item.Name == "" && len(eps) > 0 {
		item.Name = keyword
	}
	if len(eps) == 0 {
		return []RawItem{}, nil
	}
	return []RawItem{item}, nil
}
