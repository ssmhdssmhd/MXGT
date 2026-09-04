// Package extractor 解析提取器：接口优先 + 注册表模式，可对接多个提取器
// （jsonpath / regex / custom），后续新增提取器只需实现接口并注册。
package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Extractor 提取器接口：从源站页面内容中提取真实视频链接
type Extractor interface {
	// Name 提取器类型名（jsonpath / regex / custom）
	Name() string
	// Extract 从页面内容中提取真实视频 URL
	// pageURL: 原始播放页 URL；content: 页面内容（HTML 或 JSON 文本）
	// ruleConfig: 从数据库读出的规则配置（JSON 对象）
	Extract(ctx context.Context, pageURL, content string, ruleConfig map[string]any) (string, error)
}

// registry 全局注册表：按类型名管理多个提取器
type registry struct {
	mu         sync.RWMutex
	extractors map[string]Extractor
}

var defaultRegistry = &registry{
	extractors: make(map[string]Extractor),
}

// Register 注册一个提取器（对接多个）
func Register(e Extractor) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.extractors[e.Name()] = e
}

// Get 按类型名获取提取器
func Get(name string) (Extractor, bool) {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	e, ok := defaultRegistry.extractors[name]
	return e, ok
}

// List 列出所有已注册的提取器类型名
func List() []string {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	names := make([]string, 0, len(defaultRegistry.extractors))
	for n := range defaultRegistry.extractors {
		names = append(names, n)
	}
	return names
}

// ParseRuleConfig 解析数据库存储的规则配置字符串为 map
func ParseRuleConfig(raw string) (map[string]any, error) {
	cfg := make(map[string]any)
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("解析 rule_config 失败: %w", err)
	}
	return cfg, nil
}

// init 注册内置的多个提取器实现
func init() {
	Register(&JSONPathExtractor{})
	Register(&RegexExtractor{})
	Register(&CustomExtractor{})
}
