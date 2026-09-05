// Package analyzer 分析引擎：自动识别 URL 资源类型（官方 / 直链 / 未知）
package analyzer

import (
	"regexp"
	"strings"
)

// Type 分析结果类型
type Type string

const (
	TypeOfficial Type = "official" // 官方七大站资源
	TypeDirect   Type = "direct"   // 可直链播放的资源
	TypeUnknown  Type = "unknown"  // 未知类型
)

// NamedRegexp 带正则的域名匹配项（预编译）
type namedRegexp struct {
	name    string
	site    string
	pattern *regexp.Regexp
}

// Result 分析结果
type Result struct {
	Type      Type   `json:"type"`
	SiteCode  string `json:"site_code,omitempty"`
	SiteName  string `json:"site_name,omitempty"`
	Matched   bool   `json:"matched"`
	Directive string `json:"directive,omitempty"` // 直链类型 m3u8 / mp4 / flv
}

// OfficialSites 内置七大站域名正则（DB 未配置时的兜底，动态加载 site_mappings 后替换）
var OfficialSites = []namedRegexp{
	{name: "tencent", site: "腾讯视频", pattern: regexp.MustCompile(`(v\.|video\.)qq\.com`)},
	{name: "iqiyi", site: "爱奇艺", pattern: regexp.MustCompile(`(www\.|pc\.)iqiyi\.com`)},
	{name: "youku", site: "优酷", pattern: regexp.MustCompile(`(v\.|www\.)youku\.com`)},
	{name: "mgtv", site: "芒果TV", pattern: regexp.MustCompile(`(www\.|h5\.)mgtv\.com`)},
	{name: "sohu", site: "搜狐视频", pattern: regexp.MustCompile(`tv\.sohu\.com`)},
	{name: "migu", site: "咪咕视频", pattern: regexp.MustCompile(`www\.miguvideo\.com`)},
	{name: "bilibili", site: "哔哩哔哩", pattern: regexp.MustCompile(`(www\.|m\.)bilibili\.com`)},
}

// Parse 分析 URL 类型
func Parse(rawURL string) Result {
	host := Hostname(rawURL)

	// ① 官方七大站域名匹配
	for _, s := range OfficialSites {
		if s.pattern.MatchString(host) {
			return Result{Type: TypeOfficial, SiteCode: s.name, SiteName: s.site, Matched: true}
		}
	}

	// ② 直链类型判断（后缀）
	lower := strings.ToLower(rawURL)
	path := lower
	if i := strings.Index(lower, "?"); i >= 0 {
		path = lower[:i]
	}
	switch {
	case strings.HasSuffix(path, ".m3u8"):
		return Result{Type: TypeDirect, Directive: "m3u8", Matched: true}
	case strings.HasSuffix(path, ".mp4"):
		return Result{Type: TypeDirect, Directive: "mp4", Matched: true}
	case strings.HasSuffix(path, ".flv"):
		return Result{Type: TypeDirect, Directive: "flv", Matched: true}
	}

	// ③ 未知
	return Result{Type: TypeUnknown, Matched: false}
}

// Hostname 从 URL 提取主机名（去掉协议/端口/路径）
func Hostname(rawURL string) string {
	h := rawURL
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return h
}

// IsDirectMedia 判断 URL 是否为可直接播放的媒体后缀
func IsDirectMedia(rawURL string) bool {
	return Parse(rawURL).Type == TypeDirect
}