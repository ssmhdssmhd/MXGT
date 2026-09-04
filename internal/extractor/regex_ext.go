package extractor

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// RegexExtractor 正则提取器：从页面文本中按正则捕获组取值
type RegexExtractor struct{}

// Name 返回类型名
func (e *RegexExtractor) Name() string { return "regex" }

// Extract 按 ruleConfig["regex"] 模式匹配，ruleConfig["group"] 指定捕获组
func (e *RegexExtractor) Extract(_ context.Context, _ string, content string, ruleConfig map[string]any) (string, error) {
	pattern, ok := ruleConfig["regex"].(string)
	if !ok || pattern == "" {
		return "", errors.New("regex 提取器缺少 regex 配置")
	}

	group := 1
	if g, ok := ruleConfig["group"].(float64); ok {
		group = int(g)
		if group == 0 {
			group = 1
		}
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("regex 提取器规则编译失败: %w", err)
	}

	matches := re.FindStringSubmatch(content)
	if len(matches) > group {
		return matches[group], nil
	}
	return "", errors.New("regex 提取器未匹配到捕获组")
}
