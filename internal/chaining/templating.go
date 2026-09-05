package chaining

import (
	"strings"

	"github.com/PaesslerAG/jsonpath"
)

// JSONPathGet 从已解析的 JSON 文档中按路径取值（复用 PaesslerAG/jsonpath）
func JSONPathGet(path string, doc any) (any, error) {
	return jsonpath.Get(path, doc)
}

// ReplaceInputURL 将占位符 {input_url} / {input} 替换为当前输入
func ReplaceInputURL(tmpl, current string) string {
	out := strings.ReplaceAll(tmpl, "{input_url}", current)
	out = strings.ReplaceAll(out, "{input}", current)
	return out
}