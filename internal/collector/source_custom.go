package collector

import (
	"context"

	"github.com/ssmhdssmhd/MXGT/internal/models"
)

// CustomCollector 自定义采集器（示例）。
// 对接特殊源时，在此包新增更多实现并注册即可。
type CustomCollector struct {
	source *models.Source
}

// newCustomCollector 创建自定义采集器
func newCustomCollector(source *models.Source) *CustomCollector {
	return &CustomCollector{source: source}
}

// Name 采集器类型名
func (c *CustomCollector) Name() string { return "custom" }

// Fetch 自定义采集逻辑（示例：直接返回空，由用户扩展实现）
func (c *CustomCollector) Fetch(_ context.Context, _ string) ([]RawItem, error) {
	return []RawItem{}, nil
}
