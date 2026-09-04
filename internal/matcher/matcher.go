// Package matcher 匹配映射：剧名模糊匹配 + 别名 + 集数提取
package matcher

import (
	"strings"
)

// MatchName 判断采集到的剧名是否匹配目标剧名。
// threshold: 相似度阈值 0-100，默认 85。
func MatchName(name, target string, aliases []string, threshold int) bool {
	if threshold <= 0 {
		threshold = 85
	}
	name = Normalize(name)
	target = Normalize(target)
	if name == "" || target == "" {
		return false
	}
	// 1. 精确匹配
	if name == target {
		return true
	}
	// 2. 别名匹配（目标剧名的别名表）
	for _, a := range aliases {
		if a != "" && Normalize(a) == name {
			return true
		}
	}
	// 3. 包含关系（短名包含在长名中）
	if strings.Contains(target, name) || strings.Contains(name, target) {
		return true
	}
	// 4. 模糊匹配（Levenshtein 相似度）
	return int(Similarity(name, target)*100) >= threshold
}
