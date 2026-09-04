package extractor

import (
	"context"
	"errors"
	"regexp"
)

// CustomExtractor 自定义提取器：演示可扩展的兜底实现。
// 对接多个复杂源时，可在此包新增更多 custom 实现（如处理加密签名 / 多步跳转），
// 只需实现 Extractor 接口并在 init 中 Register。
type CustomExtractor struct{}

// Name 返回类型名
func (e *CustomExtractor) Name() string { return "custom" }

// Extract 兜底逻辑：在页面内容中搜索任意 m3u8 直链
func (e *CustomExtractor) Extract(_ context.Context, _ string, content string, _ map[string]any) (string, error) {
	re := regexp.MustCompile(`https?://[^"'\s<>]+\.m3u8[^"'\s<>]*`)
	if m := re.FindString(content); m != "" {
		return m, nil
	}
	return "", errors.New("custom 提取器未找到可播放链接")
}
