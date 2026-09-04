package matcher

import (
	"regexp"
	"strconv"
	"strings"
)

// 集数提取正则
var (
	epCNRe  = regexp.MustCompile(`第\s*(\d+)\s*[集话期]`)    // 第1集 / 第2话 / 第3期
	epENRe  = regexp.MustCompile(`(?i)(?:ep|e)\s*(\d+)`) // EP1 / ep 2 / E12
	epNumRe = regexp.MustCompile(`^(\d+)$`)              // 纯数字
)

// ExtractEpisodeNo 从集名称提取集数；提取失败返回 0
func ExtractEpisodeNo(name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0
	}
	if m := epCNRe.FindStringSubmatch(name); len(m) > 1 {
		return toInt(m[1])
	}
	if m := epENRe.FindStringSubmatch(name); len(m) > 1 {
		return toInt(m[1])
	}
	if m := epNumRe.FindStringSubmatch(name); len(m) > 1 {
		return toInt(m[1])
	}
	return 0
}

func toInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
