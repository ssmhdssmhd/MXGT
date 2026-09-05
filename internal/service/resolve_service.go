// Package service 业务服务层
package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/ssmhdssmhd/MXGT/internal/cache"
	"github.com/ssmhdssmhd/MXGT/internal/config"
	"github.com/ssmhdssmhd/MXGT/internal/extractor"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// ResolveResult 解析结果（前端直接消费）
type ResolveResult struct {
	URL      string `json:"url"`       // 真实播放链接
	Type     string `json:"type"`      // hls / mp4 / flv
	Proxy    bool   `json:"proxy"`     // 是否走后端 proxy
	RuleID   uint   `json:"rule_id"`   // 命中的解析规则 ID
	CacheHit bool   `json:"cache_hit"` // 是否缓存命中
}

// ResolveService 解析路由服务：路由匹配多个规则 → 多个提取器 → 缓存
type ResolveService struct {
	db     *gorm.DB
	cache  cache.Cache
	client *resty.Client
	ttl    time.Duration
}

// NewResolveService 创建解析服务
func NewResolveService(db *gorm.DB, c cache.Cache, cfg *config.Config) *ResolveService {
	timeout := time.Duration(cfg.Resolve.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &ResolveService{
		db:    db,
		cache: c,
		client: resty.New().
			SetTimeout(timeout).
			SetRetryCount(1).
			SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MXGT/"+version),
		ttl: time.Duration(cfg.Resolve.CacheTTL) * time.Second,
	}
}

// Resolve 解析源站 URL → 真实视频链接
func (s *ResolveService) Resolve(ctx context.Context, rawURL string) (*ResolveResult, error) {
	// ① 查缓存
	cacheKey := "resolve:" + hashURL(rawURL)
	if v, ok := s.cache.Get(ctx, cacheKey); ok {
		var r ResolveResult
		if err := json.Unmarshal([]byte(v), &r); err == nil {
			r.CacheHit = true
			return &r, nil
		}
	}

	// ② 加载所有启用规则（支持对接多个规则），按优先级尝试匹配
	var rules []models.ExtractRule
	if err := s.db.Where("enabled = ?", 1).Order("priority DESC").Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("加载解析规则失败: %w", err)
	}

	for i := range rules {
		rule := &rules[i]
		re, err := regexp.Compile(rule.URLPattern)
		if err != nil {
			continue // 规则正则无效，跳过
		}
		if !re.MatchString(rawURL) {
			continue
		}

		// ③ 请求源站页面内容
		content, err := s.fetch(ctx, rawURL)
		if err != nil {
			continue
		}

		// ④ 按类型分发到对应提取器（可对接多个提取器）
		ruleConfig, err := extractor.ParseRuleConfig(rule.RuleConfig)
		if err != nil {
			continue
		}
		ex, ok := extractor.Get(rule.ExtractorType)
		if !ok {
			continue
		}
		realURL, err := ex.Extract(ctx, rawURL, content, ruleConfig)
		if err != nil {
			continue
		}

		result := &ResolveResult{
			URL:    realURL,
			Type:   detectType(realURL),
			Proxy:  rule.NeedProxy == 1,
			RuleID: rule.ID,
		}

		// ⑤ 写缓存
		if b, err := json.Marshal(result); err == nil {
			_ = s.cache.Set(ctx, cacheKey, string(b), s.ttl)
		}
		return result, nil
	}

	return nil, errors.New("没有匹配到可用的解析规则")
}

// fetch 请求源站页面（统一 UA / 超时 / 重试）
func (s *ResolveService) fetch(ctx context.Context, url string) (string, error) {
	resp, err := s.client.R().SetContext(ctx).Get(url)
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("源站返回状态码 %d", resp.StatusCode())
	}
	return resp.String(), nil
}

// detectType 根据 URL 后缀推断视频类型
func detectType(url string) string {
	switch {
	case regexp.MustCompile(`(?i)\.m3u8`).MatchString(url):
		return "hls"
	case regexp.MustCompile(`(?i)\.flv`).MatchString(url):
		return "flv"
	case regexp.MustCompile(`(?i)\.(mp4|webm|ogg)`).MatchString(url):
		return "mp4"
	default:
		return "hls"
	}
}

// hashURL 生成缓存 key（URL 哈希）
func hashURL(url string) string {
	h := md5.Sum([]byte(url))
	return hex.EncodeToString(h[:])
}

// version 程序版本（与文档版本一致，由构建时 ldflags 覆盖）
var version = "v0.0.11"
