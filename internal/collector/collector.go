// Package collector 采集器：接口 + 注册表模式，可对接多个采集源
// （api / html / custom），新增源只需实现 Collector 接口并注册。
package collector

import (
	"context"
	"sync"
)

// RawEpisode 原始集数据
type RawEpisode struct {
	No   int    // 集数
	Name string // 集名称
	URL  string // 源站播放页 URL 或直链
	Line string // 播放线路标签（主线/备用）
}

// RawItem 采集到的一部影视（原始数据，未入库）
type RawItem struct {
	Name     string
	Alias    string
	Cover    string
	Year     int
	Region   string
	Category string
	Remark   string
	Episodes []RawEpisode
}

// Collector 采集器接口（可对接多个实现）
type Collector interface {
	// Name 采集器类型名（api / html / custom）
	Name() string
	// Fetch 按关键词拉取资源列表
	Fetch(ctx context.Context, keyword string) ([]RawItem, error)
}

// registry 全局注册表
type registry struct {
	mu    sync.RWMutex
	items map[string]Collector
}

var defaultRegistry = &registry{items: make(map[string]Collector)}

// Register 注册采集器实现
func Register(c Collector) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.items[c.Name()] = c
}

// Get 获取已注册的采集器（按类型名）
func Get(name string) (Collector, bool) {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	c, ok := defaultRegistry.items[name]
	return c, ok
}
