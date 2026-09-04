package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PaesslerAG/jsonpath"
)

// JSONPathExtractor JSONPath 提取器：从页面内嵌 JSON 中取值
type JSONPathExtractor struct{}

// Name 返回类型名
func (e *JSONPathExtractor) Name() string { return "jsonpath" }

// Extract 按 ruleConfig["jsonpath"] 路径提取
func (e *JSONPathExtractor) Extract(_ context.Context, _ string, content string, ruleConfig map[string]any) (string, error) {
	path, ok := ruleConfig["jsonpath"].(string)
	if !ok || path == "" {
		return "", errors.New("jsonpath 提取器缺少 jsonpath 配置")
	}

	var doc any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return "", fmt.Errorf("jsonpath 提取器无法解析内容为 JSON: %w", err)
	}

	res, err := jsonpath.Get(path, doc)
	if err != nil {
		return "", fmt.Errorf("jsonpath 提取失败: %w", err)
	}

	switch v := res.(type) {
	case string:
		return v, nil
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s, nil
			}
		}
	case map[string]any:
		// 尝试常见的 url 字段
		for _, k := range []string{"url", "playurl", "src"} {
			if s, ok := v[k].(string); ok {
				return s, nil
			}
		}
	}
	return "", errors.New("jsonpath 提取结果不是字符串")
}
