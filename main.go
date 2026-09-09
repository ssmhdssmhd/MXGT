// MXGT-Go v0.3.0 — M3U8 广告分析与去广告单文件服务
//
// 单文件、标准库零依赖：HTTP 服务接收 m3u8 链接，抓取-解析-保守广告检测-输出无广告 M3U8。
//
// 页面：
//   GET /            → 后台管理页（状态统计 + 解析测试 + 接口说明）
//
// 接口：
//   GET /api/clean?url=<m3u8>       → 过滤后的无广告 M3U8 纯文本（绝对地址）
//   GET /api/clean/json?url=<m3u8>  → JSON：统计 + 过滤后文本 + 广告片段明细
//   GET /api/stats                  → 运行统计（JSON，后台展示）
//   GET /healthz                    → 健康检查
//
// 广告检测（保守防误删，命中即高置信才删）：
//   1. URL 关键词（/ad/、_ad、ad0、300x250、tvc、promo 等）
//   2. 广告标签区间（EXT-X-DATERANGE / CUE-OUT / EXT-X-AD 声明）
//   3. 超短视频（duration < 1.0s）
//   4. opt=aggresive 时启用「同目录统一切片聚类」批量识别（可能误伤统一切片正片，默认关闭）
//
// 编译：CGO_ENABLED=0 go build -ldflags "-s -w" -o mxgt-go main.go（静态编译，旧系统 glibc 也能运行）
// 部署：./mxgt-go -addr :8080

package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AppVersion = "v0.3.0"
	UserAgent  = "MXGT-Go/" + AppVersion + " (+https://github.com/ssmhdssmhd/MXGT)"
)

// Segment 一个媒体片段
type Segment struct {
	Index         int      `json:"index"`
	Duration      float64  `json:"duration"`
	URI           string   `json:"uri"`
	AbsURI        string   `json:"abs_uri"`
	HasKey        bool     `json:"has_key,omitempty"`
	KeyURI        string   `json:"key_uri,omitempty"`
	KeyIV         string   `json:"key_iv,omitempty"`
	KeyMethod     string   `json:"key_method,omitempty"`
	MapURI        string   `json:"map_uri,omitempty"`
	Discontinuity bool     `json:"discontinuity,omitempty"`
	IsAd          bool     `json:"is_ad"`
	AdReason      string   `json:"ad_reason,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// ParseResult 解析结果
type ParseResult struct {
	Success       bool      `json:"success"`
	Message       string    `json:"message,omitempty"`
	MediaURL      string    `json:"media_url,omitempty"`
	IsMaster      bool      `json:"is_master"`
	Variants      []Variant `json:"variants,omitempty"`
	TotalSegments int       `json:"total_segments"`
	AdCount       int       `json:"ad_count"`
	KeptSegments  int       `json:"kept_segments"`
	AdRatio       float64   `json:"ad_ratio"`
	Segments      []Segment `json:"segments,omitempty"`
	FilteredM3U8  string    `json:"filtered_m3u8,omitempty"`
	DurationSec   float64   `json:"duration_sec"`
}

// Variant master playlist 备选流
type Variant struct {
	Bandwidth int    `json:"bandwidth"`
	URI       string `json:"uri"`
}

type client struct {
	http    *http.Client
	baseURL *url.URL
	keySeen map[string]bool
}

var adURLRe = regexp.MustCompile(`(?i)(^|[\/_\-\.])(ad|ads|advert|ad_v|ad0|ad_0|a0_|_ad|\.ad\.|gdt|bd_ad|sp_ad|tvc|promo|300x250|600x90|960x90|f_r0|zh_ad)([\/_\-\.]|$)`)

func newClient() *client {
	return &client{
		http:    &http.Client{Timeout: 30 * time.Second},
		keySeen: map[string]bool{},
	}
}

func (c *client) fetch(u string) (string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", c.base(u).Scheme+"://"+c.base(u).Host)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *client) base(ref string) *url.URL {
	if c.baseURL != nil {
		return c.baseURL
	}
	u, _ := url.Parse(ref)
	return u
}

// resolve 将相对/绝对 URI 基于 ref 解析为绝对地址
func resolve(ref, u string) string {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	base, err := url.Parse(ref)
	if err != nil {
		return u
	}
	rel, err := url.Parse(u)
	if err != nil {
		return u
	}
	return base.ResolveReference(rel).String()
}

// parseM3U8 解析播放列表文本为片段列表（自动跟随 master）
func parseM3U8(text, mediaURL string) ([]Segment, []Variant, bool, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	base := mediaURL
	keyMethod, keyURI, keyIV := "", "", ""
	mapURI := ""
	discont := false
	adRangeUntil := -1 // 广告区间结束段索引（DATERANGE/CUE 声明）
	var segs []Segment
	var variants []Variant
	var pendingEXTINF float64 = -1
	isMaster := false

	flushSegment := func(idx int) {
		if pendingEXTINF >= 0 {
			// 无 URI 的 EXTINF 忽略
			pendingEXTINF = -1
		}
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			isMaster = true
			// 下一行是 variant URI
			if i+1 < len(lines) {
				vURI := strings.TrimSpace(lines[i+1])
				if vURI != "" && !strings.HasPrefix(vURI, "#") {
					bw := 0
					if m := regexp.MustCompile(`BANDWIDTH=(\d+)`).FindStringSubmatch(line); len(m) > 1 {
						bw, _ = strconv.Atoi(m[1])
					}
					variants = append(variants, Variant{Bandwidth: bw, URI: vURI})
					i++
				}
			}
		case strings.HasPrefix(line, "#EXT-X-KEY"):
			keyMethod = parseAttr(line, "METHOD")
			keyURI = parseAttr(line, "URI")
			keyIV = parseAttr(line, "IV")
			if keyMethod == "" {
				keyMethod = "AES-128"
			}
		case strings.HasPrefix(line, "#EXT-X-MAP"):
			mapURI = parseAttr(line, "URI")
		case strings.HasPrefix(line, "#EXT-X-DISCONTINUITY"):
			discont = true
		case strings.HasPrefix(line, "#EXT-X-DATERANGE"):
			if d := parseDurationAttr(line, "DURATION"); d > 0 {
				// 声明区间的广告：标记后续约 duration 对应个片段
				// 简化：仅将紧邻其后 1 段标记为疑似广告（保守）
				adRangeUntil = len(segs)
			}
		case strings.HasPrefix(line, "#EXT-X-CUE-OUT") || strings.HasPrefix(line, "#EXT-X-AD"):
			adRangeUntil = len(segs)
		case strings.HasPrefix(line, "#EXTINF"):
			pendingEXTINF = parseEXTINF(line)
		case strings.HasPrefix(line, "#"):
			// 其余标签忽略
		default:
			// 片段 URI
			seg := Segment{
				Index:         len(segs),
				Duration:      pendingEXTINF,
				URI:           line,
				AbsURI:        resolve(base, line),
				HasKey:        keyMethod != "" && keyURI != "",
				KeyURI:        keyURI,
				KeyIV:         keyIV,
				KeyMethod:     keyMethod,
				MapURI:        mapURI,
				Discontinuity: discont,
			}
			if adRangeUntil >= 0 && len(segs) <= adRangeUntil+1 {
				seg.IsAd = true
				seg.AdReason = "ad_tag_range"
			}
			segs = append(segs, seg)
			discont = false
			pendingEXTINF = -1
		}
	}
	flushSegment(len(segs))
	return segs, variants, isMaster, nil
}

func parseAttr(line, key string) string {
	re := regexp.MustCompile(key + `="?([^",\s]+)"?`)
	m := re.FindStringSubmatch(line)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func parseDurationAttr(line, key string) float64 {
	v := parseAttr(line, key)
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func parseEXTINF(line string) float64 {
	// #EXTINF:5.000, -> 5.0
	body := strings.TrimPrefix(line, "#EXTINF:")
	if idx := strings.Index(body, ","); idx >= 0 {
		body = body[:idx]
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(body), 64)
	return f
}

// detectAds 保守广告检测：返回每个片段的广告标记（含原因）
func detectAds(segs []Segment, aggresive bool) {
	if len(segs) == 0 {
		return
	}
	// 1. URL 关键词
	for i := range segs {
		if segs[i].IsAd {
			continue
		}
		if adURLRe.MatchString(segs[i].URI) {
			segs[i].IsAd = true
			segs[i].AdReason = "url_keyword"
		}
	}
	// 2. 超短视频 < 1.0s（前 2 段为片头保护不判，避免误删片头 logo 段）
	for i := range segs {
		if segs[i].IsAd || i < 2 {
			continue
		}
		if segs[i].Duration > 0 && segs[i].Duration < 1.0 {
			segs[i].IsAd = true
			segs[i].AdReason = "ultra_short"
		}
	}
	// 3. 聚合识别（仅 aggresive）：同目录重复短时长桶批量标记
	if aggresive {
		detectClusters(segs)
	}
}

// detectClusters 同目录统一切片聚类：某 (目录, 时长桶) 出现占比≥35% 且片段普遍 <20s → 判为广告
// 默认关闭：统一切片的正片可能被误判（参考 PHP 版 v5.15.4 修复）
func detectClusters(segs []Segment) {
	type key struct {
		dir  string
		dura int
	}
	count := map[key]int{}
	dirTotal := map[string]int{}
	for i := range segs {
		if segs[i].IsAd || segs[i].Duration <= 0 {
			continue
		}
		u, err := url.Parse(segs[i].AbsURI)
		if err != nil {
			continue
		}
		dir := u.Path
		if idx := strings.LastIndex(dir, "/"); idx > 0 {
			dir = dir[:idx]
		}
		d := int(segs[i].Duration * 10)
		count[key{dir, d}]++
		dirTotal[dir]++
	}
	for i := range segs {
		if segs[i].IsAd || segs[i].Duration <= 0 || segs[i].Duration >= 20 {
			continue
		}
		u, err := url.Parse(segs[i].AbsURI)
		if err != nil {
			continue
		}
		dir := u.Path
		if idx := strings.LastIndex(dir, "/"); idx > 0 {
			dir = dir[:idx]
		}
		k := key{dir, int(segs[i].Duration * 10)}
		if dirTotal[dir] >= 5 && count[k] >= dirTotal[dir]*35/100 {
			segs[i].IsAd = true
			segs[i].AdReason = "cluster_repeat_duration"
		}
	}
}

// buildFilteredM3U8 输出过滤后 M3U8（绝对地址、保留 KEY/MAP/不连续标签）
func buildFilteredM3U8(segs []Segment, targetDuration int) string {
	if targetDuration <= 0 {
		targetDuration = 10
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	if targetDuration > 0 {
		b.WriteString("#EXT-X-TARGETDURATION:" + strconv.Itoa(targetDuration) + "\n")
	}
	b.WriteString("#EXT-X-VERSION:3\n")
	lastKey := ""
	lastMap := ""
	lastDiscont := false
	for _, s := range segs {
		if s.IsAd {
			continue
		}
		// 密钥轮换：与上一段不同才输出
		keySig := s.KeyURI + "|" + s.KeyIV + "|" + s.KeyMethod
		if s.HasKey && keySig != lastKey {
			method := s.KeyMethod
			if method == "" {
				method = "AES-128"
			}
			b.WriteString(fmt.Sprintf("#EXT-X-KEY:METHOD=%s,URI=\"%s\"", method, s.KeyURI))
			if s.KeyIV != "" {
				b.WriteString(fmt.Sprintf(",IV=%s", s.KeyIV))
			}
			b.WriteString("\n")
			lastKey = keySig
		} else if !s.HasKey {
			lastKey = ""
		}
		if s.MapURI != "" && s.MapURI != lastMap {
			b.WriteString(fmt.Sprintf("#EXT-X-MAP:URI=\"%s\"\n", s.MapURI))
			lastMap = s.MapURI
		}
		if s.Discontinuity && !lastDiscont {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		lastDiscont = s.Discontinuity
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n%s\n", s.Duration, s.AbsURI))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// cleanOne 主流程：抓取-解析-检测-输出
func cleanOne(rawURL string, aggresive bool) ParseResult {
	start := time.Now()
	res := ParseResult{}
	if rawURL == "" {
		res.Message = "缺少 url 参数"
		return res
	}
	mediaURL := rawURL
	body, err := newClient().fetch(rawURL)
	if err != nil {
		res.Message = "抓取失败: " + err.Error()
		return res
	}
	segs, variants, isMaster, err := parseM3U8(body, mediaURL)
	if err != nil {
		res.Message = "解析失败: " + err.Error()
		return res
	}
	// 跟随 master：选最高带宽 variant
	if isMaster && len(variants) > 0 {
		sort.Slice(variants, func(a, b int) bool { return variants[a].Bandwidth > variants[b].Bandwidth })
		mediaURL = resolve(rawURL, variants[0].URI)
		res.Variants = variants
		body2, err2 := newClient().fetch(mediaURL)
		if err2 != nil {
			res.Message = "media 抓取失败: " + err2.Error()
			return res
		}
		segs, _, _, _ = parseM3U8(body2, mediaURL)
	}
	res.IsMaster = isMaster
	res.MediaURL = mediaURL
	res.TotalSegments = len(segs)

	detectAds(segs, aggresive)

	adCount := 0
	for i := range segs {
		if segs[i].IsAd {
			adCount++
		}
	}
	res.AdCount = adCount
	res.KeptSegments = len(segs) - adCount
	if len(segs) > 0 {
		res.AdRatio = float64(adCount) / float64(len(segs)) * 100
	}
	res.Segments = segs
	res.FilteredM3U8 = buildFilteredM3U8(segs, maxTargetDuration(segs))
	res.DurationSec = time.Since(start).Seconds()
	res.Success = len(segs) > 0
	if res.Success {
		res.Message = fmt.Sprintf("解析 %d 段，识别广告 %d 段（%.1f%%），保留 %d 段",
			len(segs), adCount, res.AdRatio, res.KeptSegments)
	}
	return res
}

// maxTargetDuration 计算 TARGETDURATION：取保留片段最大时长向上取整（至少 10）
func maxTargetDuration(segs []Segment) int {
	maxD := 0.0
	for _, s := range segs {
		if s.IsAd {
			continue
		}
		if s.Duration > maxD {
			maxD = s.Duration
		}
	}
	t := int(maxD) + 1
	if t < 10 {
		t = 10
	}
	return t
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

// ServerStats 服务运行统计（后台展示用，内存统计）
type ServerStats struct {
	mu        sync.Mutex
	StartedAt time.Time
	Requests  int64 // 清洗接口请求数
	Errors    int64 // 失败请求数
	TotalSegs int64 // 累计解析片段数
	AdSegs    int64 // 累计标记广告片段数
	KeptSegs  int64 // 累计保留片段数
	LastMsg   string
	LastAt    time.Time
	LastURL   string
}

var stats = &ServerStats{StartedAt: time.Now()}

func (s *ServerStats) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ratio := 0.0
	if s.TotalSegs > 0 {
		ratio = float64(s.AdSegs) / float64(s.TotalSegs) * 100
	}
	return map[string]interface{}{
		"started_at": s.StartedAt.Format(time.RFC3339),
		"uptime":     fmtDuration(time.Since(s.StartedAt)),
		"version":    AppVersion,
		"requests":   s.Requests,
		"errors":     s.Errors,
		"total_segs": s.TotalSegs,
		"ad_segs":    s.AdSegs,
		"kept_segs":  s.KeptSegs,
		"ad_ratio":   math.Round(ratio*10) / 10,
		"last_msg":   s.LastMsg,
		"last_at":    s.LastAt.Format(time.RFC3339),
		"last_url":   s.LastURL,
	}
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d小时%02d分%02d秒", h, m, sec)
}

// recordClean 清洗一次解析结果，写入内存统计
func recordClean(res ParseResult, rawURL string) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.Requests++
	stats.TotalSegs += int64(res.TotalSegments)
	stats.AdSegs += int64(res.AdCount)
	stats.KeptSegs += int64(res.KeptSegments)
	stats.LastURL = rawURL
	stats.LastAt = time.Now()
	if res.Success {
		stats.LastMsg = res.Message
	} else {
		stats.Errors++
		stats.LastMsg = "失败: " + res.Message
	}
}

// adminPageHTML 内嵌单文件后台管理页面（玻璃拟态风格，与 PHP 版后台观感一致）
const adminPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MXGT-Go 后台</title>
<style>
  :root{--pri:#7e22ce;--pink:#c026d3;--card:rgba(255,255,255,.86);--line:rgba(255,255,255,.9)}
  *{box-sizing:border-box;margin:0;padding:0}
  body{min-height:100vh;font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;
       background:linear-gradient(135deg,#581c87,#7e22ce,#a21caf,#c026d3);color:#1f2937;padding:24px}
  .wrap{max-width:980px;margin:0 auto}
  .header{background:rgba(255,255,255,.18);backdrop-filter:blur(14px);border:1px solid var(--line);
          border-radius:18px;padding:20px 24px;color:#fff;display:flex;justify-content:space-between;align-items:center;margin-bottom:20px}
  .header h1{font-size:22px;font-weight:700}
  .header .ver{font-size:13px;opacity:.9;margin-top:4px}
  .grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:14px;margin-bottom:20px}
  .card{background:var(--card);backdrop-filter:blur(16px);border:1px solid var(--line);
        border-radius:18px;padding:16px 18px;box-shadow:0 8px 24px rgba(0,0,0,.12)}
  .card .lab{font-size:12px;color:#6b7280;margin-bottom:6px}
  .card .val{font-size:22px;font-weight:700;color:#581c87}
  .card .sub{font-size:12px;color:#9ca3af;margin-top:4px}
  .panel{background:var(--card);backdrop-filter:blur(16px);border:1px solid var(--line);
         border-radius:18px;padding:22px 24px;box-shadow:0 8px 24px rgba(0,0,0,.12);margin-bottom:20px}
  .panel h2{font-size:17px;color:#581c87;margin-bottom:16px}
  .row{display:flex;gap:10px;flex-wrap:wrap;align-items:center;margin-bottom:14px}
  input[type=url]{flex:1;min-width:240px;padding:11px 14px;border-radius:10px;border:1px solid #d1d5db;font-size:14px}
  .btn{background:linear-gradient(135deg,#7e22ce,#c026d3);color:#fff;border:0;border-radius:10px;
       padding:11px 18px;font-size:14px;cursor:pointer;box-shadow:0 4px 12px rgba(124,58,237,.35)}
  .btn:hover{filter:brightness(1.08)}
  .btn.ghost{background:#fff;color:#581c87;border:1px solid #d1d5db;box-shadow:none}
  label.sw{display:flex;align-items:center;gap:6px;font-size:13px;color:#4b5563;cursor:pointer}
  pre{margin-top:14px;background:#111827;color:#7ee787;border-radius:12px;padding:16px;font-size:12.5px;
      line-height:1.55;overflow:auto;max-height:430px;white-space:pre-wrap;word-break:break-all;display:none}
  .stat-line{font-size:13px;color:#374151;margin-top:10px;padding:10px 14px;background:#f3f4f6;border-radius:10px;display:none}
  table{width:100%;border-collapse:collapse;font-size:13.5px}
  th,td{text-align:left;padding:9px 10px;border-bottom:1px solid #eee}
  th{color:#581c87;font-size:12.5px}
  code{background:#f3f4f6;padding:2px 7px;border-radius:6px;color:#7e22ce;font-size:12px}
  .muted{color:#9ca3af;font-size:12px}
</style>
</head>
<body>
<div class="wrap">
  <div class="header">
    <div>
      <h1>🎬 MXGT-Go 后台</h1>
      <div class="ver">M3U8 广告分析与去广告 · 单文件服务 <span id="ver"></span></div>
    </div>
    <button class="btn ghost" onclick="refreshStats()">⟳ 刷新</button>
  </div>

  <div class="grid" id="statGrid"></div>

  <div class="panel">
    <h2>🔬 解析测试</h2>
    <div class="row">
      <input type="url" id="urlInput" placeholder="粘贴 M3U8 地址，如 https://example.com/playlist.m3u8"
             onkeydown="if(event.key==='Enter')runClean()">
      <label class="sw"><input type="checkbox" id="aggrOpt"> 聚合识别(opt=aggresive)</label>
      <button class="btn" onclick="runClean()">⚡ 解析并去广告</button>
      <button class="btn ghost" onclick="playClean()">▶ 直接播放无广告</button>
    </div>
    <div class="stat-line" id="resStats"></div>
    <pre id="resOut"></pre>
  </div>

  <div class="panel" id="playPanel" style="display:none">
    <h2>🎬 无广告播放</h2>
    <video id="player" controls playsinline style="width:100%;aspect-ratio:16/9;background:#000;border-radius:12px"></video>
    <div class="stat-line" id="playSrc" style="display:block"></div>
  </div>

  <div class="panel">
    <h2>🔄 远程在线更新</h2>
    <div class="row">
      <div class="stat-line" id="updInfo" style="display:block"></div>
    </div>
    <div class="row">
      <button class="btn" onclick="checkUpdate()">🔍 检查更新</button>
      <button class="btn ghost" onclick="applyUpdate()">⬇ 下载并更新重启</button>
    </div>
  </div>

  <div class="panel">
    <h2>📚 HTTP 接口说明</h2>
    <table>
      <tr><th>接口</th><th>说明</th></tr>
      <tr><td><code>GET /api/clean?url=&lt;m3u8&gt;</code></td><td>返回过滤后的无广告 M3U8 纯文本（绝对地址）</td></tr>
      <tr><td><code>GET /api/clean/json?url=&lt;m3u8&gt;</code></td><td>返回 JSON：统计 + 过滤后文本 + 每个片段明细</td></tr>
      <tr><td><code>GET /api/clean?url=&lt;m3u8&gt;&amp;opt=aggresive</code></td><td>开启聚合聚类识别（可能误伤统一切片正片）</td></tr>
      <tr><td><code>GET /api/stats</code></td><td>运行统计（JSON）</td></tr>
      <tr><td><code>GET /healthz</code></td><td>健康检查</td></tr>
    </table>
    <p class="muted" style="margin-top:12px">命令行模式：<code>./mxgt-go "&lt;m3u8地址&gt;"</code></p>
  </div>
</div>

<script>
function el(id){return document.getElementById(id)}
function statCard(lab,val,sub){
  return '<div class="card"><div class="lab">'+lab+'</div><div class="val">'+val+'</div>'+(sub?'<div class="sub">'+sub+'</div>':'')+'</div>';
}
async function getJSON(u){
  const r=await fetch(u); return r.json();
}
async function refreshStats(){
  try{
    const s=await getJSON('/api/stats');
    el('ver').textContent=s.version;
    const ratio=s.ad_ratio+'%';
    el('statGrid').innerHTML=
      statCard('版本',s.version,'运行 '+s.uptime)+
      statCard('清洗请求',s.requests,s.errors+' 次失败')+
      statCard('累计片段',s.total_segs,'广告 '+s.ad_segs)+
      statCard('广告占比',ratio,'保留 '+s.kept_segs)+
      statCard('最近',s.last_msg||'—',s.last_url||'');
    if(s.last_at){document.querySelector('#statGrid .val').title=s.last_at}
  }catch(e){el('statGrid').innerHTML=statCard('状态','ERR','统计加载失败')}
}
async function runClean(){
  const url=el('urlInput').value.trim();
  el('resStats').style.display='none'; el('resOut').style.display='none';
  if(!url){alert('请先粘贴 M3U8 地址');return;}
  el('resOut').style.display='block';el('resOut').textContent='请求中…';
  try{
    const opt=el('aggrOpt').checked?'&opt=aggresive':'';
    const r=await fetch('/api/clean?url='+encodeURIComponent(url)+opt);
    const ct=r.headers.get('content-type')||'';
    let data=await r.text();
    if(ct.indexOf('json')>=0){ const j=JSON.parse(data); el('resStats').style.display='block';el('resStats').textContent=(j.message||j.Message||'')+(j.success===false?'':'')+(j.filtered_m3u8?'  |  保留段 '+j.kept_segments+' / '+j.total_segments:''); data=j.filtered_m3u8||JSON.stringify(j,null,2); }
    el('resOut').textContent=data;
    refreshStats();
  }catch(e){el('resOut').textContent='解析失败: '+e.message}
}
function checkUpdate(){
  el('updInfo').textContent='正在检查更新…';
  fetch('/api/update/check').then(function(r){return r.json()}).then(function(d){
    let s='当前 '+d.current+' → 最新 '+d.latest;
    if(d.has_update){ s+='　⚠️ 存在新版本'; }
    if(d.message){ s+='　('+d.message+')'; }
    el('updInfo').textContent=s;
  }).catch(function(e){el('updInfo').textContent='检查失败: '+e.message});
}
function applyUpdate(){
  if(!confirm('确定下载并替换为新版本并重启服务吗？')){return;}
  el('updInfo').textContent='正在发起更新…';
  fetch('/api/update/apply',{method:'POST'}).then(function(r){return r.json()}).then(function(d){
    el('updInfo').textContent=(d.message||'处理中，请稍候刷新（会短暂断连）')+' → '+(d.to||'');
    setTimeout(function(){location.reload();},4000);
  }).catch(function(e){el('updInfo').textContent='发起失败: '+e.message});
}
refreshStats(); setInterval(refreshStats,5000); checkUpdate();

function buildCleanURL(url, aggr){
  return '/api/clean?url='+encodeURIComponent(url)+(aggr?'&opt=aggresive':'');
}
function loadHls(cb){
  if(window.Hls){return cb();}
  const cdn=['https://cdn.jsdelivr.net/npm/hls.js@1/dist/hls.min.js',
             'https://cdnjs.cloudflare.com/ajax/libs/hls.js/1.5.20/hls.min.js',
             'https://unpkg.com/hls.js@1/dist/hls.min.js'];
  let i=0;
  (function load(){
    if(i>=cdn.length){alert('hls.js 加载失败');return;}
    const s=document.createElement('script');
    s.src=cdn[i++];s.onload=cb;s.onerror=load;
    document.head.appendChild(s);
  })();
}
let hlsInst=null;
function playClean(){
  const url=(el('urlInput').value||'').trim();
  if(!url){alert('请先粘贴 M3U8 地址');return;}
  const aggr=el('aggrOpt').checked;
  const src=buildCleanURL(url,aggr);
  const player=el('player');
  // 动态拼绝对播放地址（基于当前站点 origin，不硬编码）
  el('playSrc').textContent='播放源: '+location.origin+src;
  el('playPanel').style.display='block';
  loadHls(function(){
    if(hlsInst){hlsInst.destroy();hlsInst=null;}
    if(Hls.isSupported()){
      hlsInst=new Hls({enableWorker:true,
        xhrSetup:function(xhr){
          xhr.withCredentials=false;
          xhr.setRequestHeader('Origin',location.origin);
        }});
      hlsInst.loadSource(src);hlsInst.attachMedia(player);
    }else if(player.canPlayType('application/vnd.apple.mpegurl')){
      player.src=src;
    }else{alert('当前浏览器不支持 HLS 播放');return;}
    player.play().catch(function(){});
  });
  refreshStats();
}
</script>
</body>
</html>
`

// handleAdmin 渲染后台页面
func handleAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, adminPageHTML)
}

// withCORS 全局跨域中间件：所有响应补 CORS 头（含播放分片所需的 Range 头放行），并处理 OPTIONS 预检
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Range, Accept, User-Agent, Referer")
		h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Content-Range")
		h.Set("Allow", "GET, POST, OPTIONS")
		h.Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			h.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// playURL 基于请求动态推导本服务可播放地址（不硬编码域名/IP，来源 request Host）
func playURL(r *http.Request, path string, query url.Values) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	u := url.URL{Scheme: scheme, Host: r.Host, Path: path, RawQuery: query.Encode()}
	return u.String()
}

// ============================================================
// 远程在线更新：检查 GitHub Releases → 下载替换 → 自动重启
// ============================================================

// 更新源配置。清单 latest.json 由 GitHub Actions 每次发布后自动写入并提交到 go 分支。
const (
	repoOwner      = "ssmhdssmhd"
	repoName       = "MXGT"
	repoBranch     = "go"
	manifestRawURL = "https://raw.githubusercontent.com/" + repoOwner + "/" + repoName + "/" + repoBranch + "/latest.json"
	releaseBaseURL = "https://github.com/" + repoOwner + "/" + repoName + "/releases/download"
	updateTimeout  = 60 * time.Second
)

// UpdateManifest 线上版本清单
type UpdateManifest struct {
	Version string `json:"version"` // 最新版本，如 v0.3.0
	Zip     string `json:"zip"`     // Release 资产文件名，如 MXGT_go_v0.3.0_202609091200.zip
}

var updateHTTP = &http.Client{Timeout: updateTimeout}

// parseVersion 解析 a.b.c 为 [a,b,c]
func parseVersion(v string) []int {
	t := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(t, ".")
	out := []int{}
	for _, p := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out = append(out, n)
	}
	for len(out) < 3 {
		out = append(out, 0)
	}
	return out[:3]
}

// compareVersion 返回 -1/0/1（a<b / a==b / a>b）
func compareVersion(a, b []int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// fetchManifest 拉取线上版本清单（raw.githubusercontent，无 GitHub API 限流）
func fetchManifest() (*UpdateManifest, error) {
	resp, err := updateHTTP.Get(manifestRawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
	}
	var m UpdateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, err
	}
	m.Version = strings.TrimPrefix(strings.TrimSpace(m.Version), "v")
	m.Zip = strings.TrimSpace(m.Zip)
	if m.Version == "" {
		return nil, fmt.Errorf("manifest 版本为空")
	}
	return &m, nil
}

// updateInfo 组装检查信息
func updateInfo() map[string]interface{} {
	out := map[string]interface{}{
		"current":      AppVersion,
		"latest":       AppVersion,
		"has_update":   false,
		"download_url": "",
		"message":      "",
	}
	m, err := fetchManifest()
	if err != nil {
		out["message"] = "检查更新失败: " + err.Error()
		return out
	}
	out["latest"] = "v" + m.Version
	out["download_url"] = releaseBaseURL + "/go-v" + m.Version + "/" + m.Zip
	if compareVersion(parseVersion(AppVersion), parseVersion(m.Version)) < 0 && m.Zip != "" {
		out["has_update"] = true
	}
	return out
}

// downloadZip 下载 Release 资产 zip 到本地临时文件
func downloadZip(url string) (string, error) {
	resp, err := updateHTTP.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 HTTP %d (%s)", resp.StatusCode, url)
	}
	f, err := os.CreateTemp("", "mxgt-update-*.zip")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// extractBinary 从 zip 中提取与当前可执行文件同名的二进制到临时文件
func extractBinary(zipPath, want string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		tmp, err := os.CreateTemp("", "mxgt-*.new")
		if err != nil {
			rc.Close()
			return "", err
		}
		if _, err := io.Copy(tmp, rc); err != nil {
			rc.Close()
			tmp.Close()
			os.Remove(tmp.Name())
			return "", err
		}
		rc.Close()
		tmp.Close()
		if err := os.Chmod(tmp.Name(), 0o755); err != nil {
			os.Remove(tmp.Name())
			return "", err
		}
		return tmp.Name(), nil
	}
	return "", fmt.Errorf("zip 中未找到可执行文件 %s", want)
}

// replaceAndRestart 用新二进制替换当前文件并重启进程
func replaceAndRestart(newBin string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// 备份当前二进制
	backup := exe + ".bak"
	_ = os.Remove(backup)
	if data, err := os.ReadFile(exe); err == nil {
		_ = os.WriteFile(backup, data, 0o755)
	}
	// 原子替换
	if err := os.Rename(newBin, exe); err != nil {
		return err
	}
	// 启动新进程（继承参数与工作目录）
	args := os.Args[1:]
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("已启动新进程 pid=%d，本进程即将退出", cmd.Process.Pid)
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
	return nil
}

// handleUpdateCheck GET /api/update/check
func handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, updateInfo())
}

// handleUpdateApply POST /api/update/apply
func handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	info := updateInfo()
	if !info["has_update"].(bool) {
		info["message"] = "当前已是最新版本"
		writeJSON(w, info)
		return
	}
	urlStr := info["download_url"].(string)
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "开始下载更新并重启，请稍候刷新页面…",
		"from":    info["current"],
		"to":      info["latest"],
	})
	go func() {
		zipPath, err := downloadZip(urlStr)
		if err != nil {
			log.Printf("更新下载失败: %v", err)
			return
		}
		defer os.Remove(zipPath)
		exe, _ := os.Executable()
		newBin, err := extractBinary(zipPath, filepath.Base(exe))
		if err != nil {
			log.Printf("更新解压失败: %v", err)
			return
		}
		if err := replaceAndRestart(newBin); err != nil {
			log.Printf("更新替换失败: %v", err)
			os.Remove(newBin)
		}
	}()
}

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	flag.Parse()
	if flag.NArg() > 0 {
		// CLI 模式：go run main.go <m3u8_url>
		res := cleanOne(flag.Arg(0), false)
		if !res.Success {
			fmt.Println("失败:", res.Message)
			os.Exit(1)
		}
		fmt.Println(res.Message)
		fmt.Println("=== 过滤后 M3U8 ===")
		fmt.Println(res.FilteredM3U8)
		return
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/admin", "/admin/":
			handleAdmin(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "ok %s\n", AppVersion)
	})
	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, stats.snapshot())
	})
	http.HandleFunc("/api/update/check", handleUpdateCheck)
	http.HandleFunc("/api/update/apply", handleUpdateApply)
	http.HandleFunc("/api/clean", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		aggr := r.URL.Query().Get("opt") == "aggresive"
		res := cleanOne(u, aggr)
		recordClean(res, u)
		if !res.Success {
			writeJSON(w, map[string]interface{}{"success": false, "message": res.Message})
			return
		}
		if r.URL.Query().Get("format") == "json" {
			res.Segments = nil // 减少 JSON 体积
			writeJSON(w, res)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, res.FilteredM3U8)
	})
	http.HandleFunc("/api/clean/json", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		aggr := r.URL.Query().Get("opt") == "aggresive"
		res := cleanOne(u, aggr)
		recordClean(res, u)
		writeJSON(w, res)
	})
	log.Printf("MXGT-Go %s listening on %s (单文件 M3U8 去广告服务)", AppVersion, *addr)
	if err := http.ListenAndServe(*addr, withCORS(http.DefaultServeMux)); err != nil {
		log.Fatal(err)
	}
}
