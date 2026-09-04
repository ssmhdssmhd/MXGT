package collector

import (
	"fmt"

	"github.com/ssmhdssmhd/MXGT/internal/models"
)

// New 按采集源配置创建采集器实例（多源对接：api / html / custom）
func New(source *models.Source) (Collector, error) {
	switch source.SourceType {
	case "api", "":
		return newAPICollector(source), nil
	case "html":
		return newHTMLCollector(source), nil
	case "custom":
		return newCustomCollector(source), nil
	default:
		return nil, fmt.Errorf("不支持的采集源类型: %s", source.SourceType)
	}
}

// RegisteredTypes 列出所有已注册的采集器类型（含内置）
func RegisteredTypes() []string {
	// 内置类型 + 注册表扩展类型
	seen := map[string]bool{"api": true, "html": true, "custom": true}
	types := []string{"api", "html", "custom"}
	for _, name := range []string{} {
		if !seen[name] {
			seen[name] = true
			types = append(types, name)
		}
	}
	return types
}
