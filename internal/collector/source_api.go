package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PaesslerAG/jsonpath"
	"github.com/go-resty/resty/v2"
	"github.com/ssmhdssmhd/MXGT/internal/models"
)

// APICollector 通用 JSON API 源采集器
// 支持苹果 CMS v10 标准格式（vod_name / vod_play_url），也支持自定义 JSONPath 提取规则。
type APICollector struct {
	source *models.Source
	client *resty.Client
	rules  map[string]string // 提取规则（从 source.ExtractRules 解析）
}

// newAPICollector 创建 API 采集器
func newAPICollector(source *models.Source) *APICollector {
	rules := map[string]string{}
	_ = json.Unmarshal([]byte(source.ExtractRules), &rules)

	return &APICollector{
		source: source,
		client: resty.New().
			SetTimeout(15e9).
			SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MXGT/collector"),
		rules: rules,
	}
}

// Name 采集器类型名
func (c *APICollector) Name() string { return "api" }

// Fetch 拉取关键词资源列表
func (c *APICollector) Fetch(ctx context.Context, keyword string) ([]RawItem, error) {
	// 支持 {keyword} 占位符
	raw := strings.ReplaceAll(c.source.FetchURL, "{keyword}", url.QueryEscape(keyword))
	req := c.client.R().SetContext(ctx)
	if c.source.Method != "" && strings.ToUpper(c.source.Method) == "POST" {
		req = req.SetBody(map[string]string{"wd": keyword})
	}
	resp, err := req.Get(raw)
	if err != nil {
		return nil, fmt.Errorf("请求源站失败: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("源站返回状态码 %d", resp.StatusCode())
	}

	var doc any
	if err := json.Unmarshal(resp.Body(), &doc); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	listPath := c.rules["list_path"]
	if listPath == "" {
		listPath = "$.data.list"
	}

	v, err := jsonpath.Get(listPath, doc)
	if err != nil {
		// 兼容苹果 CMS 无 data.list 时返回整体数组
		return nil, fmt.Errorf("list_path 未匹配到列表: %w", err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return []RawItem{}, nil
	}

	items := make([]RawItem, 0, len(arr))
	for _, item := range arr {
		ri := RawItem{
			Name:     extractString(item, c.rules["name"], "vod_name"),
			Alias:    extractString(item, c.rules["alias"], "vod_actor"),
			Cover:    extractString(item, c.rules["cover"], "vod_pic"),
			Region:   extractString(item, c.rules["region"], "vod_area"),
			Category: extractString(item, c.rules["category"], "vod_class"),
			Remark:   extractString(item, c.rules["remark"], "vod_remarks"),
		}
		ri.Year = extractInt(item, c.rules["year"], "vod_year")
		ri.Episodes = extractEpisodes(item, c.rules["episodes"])
		if ri.Name != "" || len(ri.Episodes) > 0 {
			items = append(items, ri)
		}
	}
	return items, nil
}

// ---- 字段提取工具 ----

// extractString 从 item 提取字符串字段。
// 规则语法：jsonpath:$.x / regex:pattern / 固定值（空则用默认 jsonpath $.field）
func extractString(item any, rule, field string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		rule = "jsonpath:$." + field
	}
	switch {
	case strings.HasPrefix(rule, "jsonpath:"):
		v, err := jsonpath.Get(strings.TrimPrefix(rule, "jsonpath:"), item)
		if err != nil {
			return ""
		}
		return toString(v)
	case strings.HasPrefix(rule, "regex:"):
		re, err := regexp.Compile(strings.TrimPrefix(rule, "regex:"))
		if err != nil {
			return ""
		}
		m := re.FindStringSubmatch(toString(item))
		if len(m) > 1 {
			return m[1]
		}
		return ""
	default:
		return rule // 固定值
	}
}

// extractInt 提取整数字段
func extractInt(item any, rule, field string) int {
	s := extractString(item, rule, field)
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

// extractEpisodes 提取集数列表。
// 规则语法：
//
//	string:$.vod_play_url  → 字符串 "第1集$url#第2集$url"（默认，兼容苹果 CMS）
//	jsonpath:$.list        → JSON 数组 [{name,url}]
func extractEpisodes(item any, rule string) []RawEpisode {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		rule = "string:$.vod_play_url"
	}

	if strings.HasPrefix(rule, "string:") {
		v, err := jsonpath.Get(strings.TrimPrefix(rule, "string:"), item)
		if err != nil {
			return nil
		}
		return parsePlayString(toString(v))
	}

	if strings.HasPrefix(rule, "jsonpath:") {
		v, err := jsonpath.Get(strings.TrimPrefix(rule, "jsonpath:"), item)
		if err != nil {
			return nil
		}
		if arr, ok := v.([]any); ok {
			eps := make([]RawEpisode, 0, len(arr))
			for _, e := range arr {
				switch ev := e.(type) {
				case string:
					eps = append(eps, parsePlayString(ev)...)
				case map[string]any:
					name, url := toString(ev["name"]), toString(ev["url"])
					if url == "" {
						url = toString(ev["play_url"])
					}
					if url != "" {
						eps = append(eps, RawEpisode{Name: name, URL: url})
					}
				}
			}
			return eps
		}
	}
	return nil
}

// parsePlayString 解析苹果 CMS 播放串："第1集$url1#第2集$url2" 或 "url1,url2"
func parsePlayString(s string) []RawEpisode {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '#' || r == ',' || r == '，' || r == '\n'
	})
	eps := make([]RawEpisode, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if seg := strings.SplitN(p, "$", 2); len(seg) == 2 {
			eps = append(eps, RawEpisode{Name: strings.TrimSpace(seg[0]), URL: strings.TrimSpace(seg[1])})
		} else {
			eps = append(eps, RawEpisode{Name: p, URL: p})
		}
	}
	return eps
}

// toString 将 JSON 值转为字符串
func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
