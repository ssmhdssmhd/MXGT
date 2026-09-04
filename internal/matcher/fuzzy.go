package matcher

import (
	"regexp"
	"strings"
)

// fuzzyRe 匹配剧名中的年份（用于规范化时去掉）
var yearRe = regexp.MustCompile(`(19|20)\d{2}`)

// Normalize 规范化剧名：去空白/标点/年份/（大陆）/国语等干扰词，转小写
func Normalize(s string) string {
	s = strings.ToLower(s)
	s = yearRe.ReplaceAllString(s, "")
	// 去常见后缀干扰词
	for _, w := range []string{"(大陆)", "（大陆）", "(内地)", "（内地）", "国语", "粤语", "高清", "全集"} {
		s = strings.ReplaceAll(s, w, "")
	}
	// 去空白和常见标点
	replacer := strings.NewReplacer(
		" ", "", "\t", "", "\n", "",
		":", "", "：", "", "-", "", "_", "",
		"《", "", "》", "", "【", "", "】", "",
		".", "", ",", "", "，", "",
	)
	return replacer.Replace(s)
}
