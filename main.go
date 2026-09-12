// MXGT-Go v0.3.3 — M3U8 广告分析与去广告单文件服务
//
// 单文件、标准库零依赖：HTTP 服务接收 m3u8 链接，抓取-解析-保守广告检测-输出无广告 M3U8。
//
// 页面：
//   GET /mxadmin → 后台管理页（状态统计 + 解析测试 + 内嵌播放 + 远程更新）
//   GET /            → 简洁落地页（不进入后台，含通往 /mxadmin 的入口）
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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
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
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	AppVersion = "v0.6.23"
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
	Engine        string    `json:"engine,omitempty"`
	AiHits        int       `json:"ai_hits,omitempty"`
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
		// 死链提示：资源站/解析接口动态生成的 m3u8 有有效期，过期后源站文件被删即返回 404/410。
		// 这类地址无法本地复活，需从资源站重新解析/重新采集获取新地址。
		hint := ""
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			hint = "（该播放地址已失效/过期，源站返回404：请从资源站重新解析或重新采集获取新的有效地址）"
		}
		return "", fmt.Errorf("HTTP %d from %s%s", resp.StatusCode, u, hint)
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

// 增强检测使用的平台广告域/关键词补充表（中小资源站常见）。命中 URL 判定为广告段。
var enhancedAdURLRe = regexp.MustCompile(`(?i)(\.(baidu|bdstatic|aliyun|alicdn|bytedance|byteimg|toutiao|qiniucdn|upyun)\.[a-z]+/ad|/ad[s]{0,2}/|_ad\.|ad_insert|advert\.|\.adx|gdt_|tvc_|preroll|midroll|postroll)`)

// enhancedDetectAds 新版增强检测：仅新增高置信规则，不回改原已判段。
// 1. 增强 URL 关键词（平台广告域/前中后插播常见特征）
// 2. 边界短簇：开头/结尾连续超短视频（<2s）且成簇（>=3 段）判为广告，避免片头片尾 logo 簇误删正片
// 3. 中插短簇：片段中间被长正片包围的连续短段（<6s，1-3 段）判为广告（中插广告典型形态）
func enhancedDetectAds(segs []Segment) {
	if len(segs) == 0 {
		return
	}
	// 1. 增强 URL 关键词
	for i := range segs {
		if segs[i].IsAd {
			continue
		}
		if enhancedAdURLRe.MatchString(segs[i].URI) || enhancedAdURLRe.MatchString(segs[i].AbsURI) {
			segs[i].IsAd = true
			segs[i].AdReason = "enhanced_url_keyword"
		}
	}
	// 2. 边界短簇：开头连续短簇
	markBoundaryCluster(segs, true)
	// 3. 边界短簇：结尾连续短簇
	markBoundaryCluster(segs, false)
	// 4. 中插短簇：长正片包围的短段簇（中插广告/贴片最典型形态）
	markMidrollClusters(segs)
	// 5. 成对 DISCONTINUITY 广告块：插入点→短块→恢复点（广告常见），夹 1-12 段且总时长 ≤ 120s 判为广告
	markDiscontinuityBlocks(segs)
}

// applyAdSafetyRoof 播放安全兜底（「成功却播不了」的总开关）：
// 启发式去广告（短段簇/不连续广告块/边界短簇/聚类时长去重/超短视频）识别的是「形态」，对统一正片
// 会误判为广告一阵狂删，导致过滤后的播放列表被删空/只剩极少，轻则黑屏卡死、重则无法起播。
// 兜底原则：宁可不删广告，也要保证返回结果能正常播放。
// 规则：一旦启发式删除了整部片段过高比例（>50%），判定为「统一正片误伤」，
// 回退全部低置信启发式标记，仅保留高置信显式标记（URL 关键词 / 广告标签区间）；
// 若回退后仍为空，则保留全部片段（彻底放弃过滤，保证能播）。
func applyAdSafetyRoof(segs []Segment) {
	total := len(segs)
	if total == 0 {
		return
	}
	// 高置信显式标记：这些不常见且明确指向广告，不参与回退
	highConfidence := map[string]bool{
		"url_keyword":          true, // 广告型文件名
		"enhanced_url_keyword": true, // 广告域 / 前中后插播特征
		"ad_tag_range":         true, // 播放列表显式声明广告区间
	}
	kept := 0
	for i := range segs {
		if !segs[i].IsAd {
			kept++
		}
	}
	adCount := total - kept
	if adCount == 0 {
		return
	}
	overFilter := float64(adCount)/float64(total) > 0.5
	if kept == 0 {
		overFilter = true
	}
	if overFilter {
		// 回退低置信启发式标记
		for i := range segs {
			if segs[i].IsAd && !highConfidence[segs[i].AdReason] {
				segs[i].IsAd = false
				segs[i].AdReason = ""
			}
		}
		// 若仍为空（极少数全片都被判为 URL 关键词广告），彻底保留，保证能播
		stillKept := 0
		for i := range segs {
			if !segs[i].IsAd {
				stillKept++
			}
		}
		if stillKept == 0 {
			for i := range segs {
				segs[i].IsAd = false
				segs[i].AdReason = ""
			}
		}
	}
}

// markDiscontinuityBlocks 成对 DISCONTINUITY 广告块检测：
// 广告插入在 m3u8 中常表现为「插入点 DISCONTINUITY → 若干段 → 恢复点 DISCONTINUITY」成对出现。
// 两个 DISCONTINUITY 之间夹 1-12 段且总时长 ≤ 120s 时判为广告；
// 片头拼接/正片大段切分通常夹块远大于此（或仅单次 DISCONTINUITY），避免误伤。
func markDiscontinuityBlocks(segs []Segment) {
	n := len(segs)
	// 分隔符型播放列表保护：若 DISCONTINUITY 出现得极其频繁（>5% 的片段都带不连续标记），
	// 说明它被用作「分段分隔符」而非「广告插入点」（快车/闪电/牛牛/无水印等 CDN 每个 ~2s 段前都放一个），
	// 此时成对不连续间隙遍布全片，按块判广告会误删大半正片（曾导致整部视频 40%+ 被删、无法正常播放）。
	discont := 0
	for i := range segs {
		if segs[i].Discontinuity {
			discont++
		}
	}
	if discont > 0 && float64(discont)/float64(n) > 0.05 {
		return
	}
	i := 0
	for i < n {
		if !segs[i].Discontinuity {
			i++
			continue
		}
		start := i
		j := i + 1
		var sum float64
		for j < n && !segs[j].Discontinuity {
			sum += segs[j].Duration
			j++
		}
		if j >= n {
			break // 无成对恢复点（如片头单次拼接），跳过
		}
		cnt := j - start
		// 广告块需足够短于全片均长：按块平均时长显著短于全片均值才判，
		// 避免「全片都是 ~2s 短段」的统一正片被误删（分隔符型已在上面提前拦截，这里是双保险）
		avgBlock := sum / float64(cnt)
		avgAll := 0.0
		for k := range segs {
			avgAll += segs[k].Duration
		}
		avgAll /= float64(n)
		if cnt >= 1 && cnt <= 12 && sum > 0 && sum <= 120 && avgBlock < avgAll*0.6 {
			for k := start; k < j; k++ {
				if !segs[k].IsAd {
					segs[k].IsAd = true
					segs[k].AdReason = "enhanced_discontinuity_block"
				}
			}
		}
		i = j
	}
}

// markMidrollClusters 中插短簇检测：序列中间连续短段（<6s）组成的簇（1-3 段），
// 且两侧紧邻片段时长显著更长（平均 ≥ 2.2 倍）时判为广告。
// 避免误伤：片头/片尾 logo 簇由 markBoundaryCluster 处理；正片中间偶发短段需两侧邻居足够长才判。
func markMidrollClusters(segs []Segment) {
	n := len(segs)
	i := 0
	for i < n {
		if segs[i].IsAd || segs[i].Duration <= 0 || segs[i].Duration >= 6.0 {
			i++
			continue
		}
		// 收集连续短段簇
		j := i
		var sum float64
		for j < n && !segs[j].IsAd && segs[j].Duration > 0 && segs[j].Duration < 6.0 {
			sum += segs[j].Duration
			j++
		}
		cnt := j - i
		if cnt >= 1 && cnt <= 3 {
			avg := sum / float64(cnt)
			var sideSum float64
			var sideCnt int
			for k := i - 1; k >= i-2 && k >= 0; k-- {
				if segs[k].IsAd || segs[k].Duration <= 0 {
					continue
				}
				sideSum += segs[k].Duration
				sideCnt++
			}
			for k := j; k < j+2 && k < n; k++ {
				if segs[k].IsAd || segs[k].Duration <= 0 {
					continue
				}
				sideSum += segs[k].Duration
				sideCnt++
			}
			if sideCnt >= 2 && sideSum/float64(sideCnt) >= avg*2.2 {
				for k := i; k < j; k++ {
					segs[k].IsAd = true
					segs[k].AdReason = "enhanced_midroll_cluster"
				}
			}
		}
		i = j
	}
}

// markBoundaryCluster 标记开头(forward=true)/结尾(forward=false) 连续超短视频簇（>=3 段，<2s）为广告。
// 采用簇内多数+整体时长占比双条件，避免误伤片头 logo 短段。
func markBoundaryCluster(segs []Segment, forward bool) {
	n := len(segs)
	get := func(i int) int {
		if forward {
			return i
		}
		return n - 1 - i
	}
	// 收集边界连续短段下标
	var cluster []int
	for i := 0; i < n; i++ {
		idx := get(i)
		if segs[idx].IsAd {
			if len(cluster) > 0 {
				continue // 已判广告段不参与，但不停留簇
			}
			continue
		}
		if segs[idx].Duration > 0 && segs[idx].Duration < 2.0 {
			cluster = append(cluster, idx)
		} else {
			break
		}
	}
	if len(cluster) >= 3 {
		for _, idx := range cluster {
			if !segs[idx].IsAd {
				segs[idx].IsAd = true
				segs[idx].AdReason = "enhanced_boundary_cluster"
			}
		}
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

// ============================================================
// AI 去广告（独立子模块 ai/，独立版本、可单独更新）
// 配置：可执行文件旁 ai/config.json；缺失时用内置默认（enabled=false, mode=basic）
// ============================================================

type AIConfig struct {
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"` // basic|ai|auto
	Provider    string `json:"provider"`
	APIURL      string `json:"api_url"`
	APIKey      string `json:"api_key"`
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	MaxSegments int    `json:"max_segments"`
	Timeout     int    `json:"timeout"`
}

const defaultAIConfigJSON = `{"version":"v0.1.0","enabled":false,"mode":"basic","provider":"openai","api_url":"","api_key":"","model":"","prompt":"","max_segments":300,"timeout":25}`

func aiConfigFile() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "ai", "config.json")
	}
	return filepath.Join("ai", "config.json")
}

func aiVersionText() string {
	if exe, err := os.Executable(); err == nil {
		if b, e := os.ReadFile(filepath.Join(filepath.Dir(exe), "ai", "VERSION")); e == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return "v0.1.0"
}

func loadAIConfig() *AIConfig {
	cfg := &AIConfig{}
	_ = json.Unmarshal([]byte(defaultAIConfigJSON), cfg)
	if b, err := os.ReadFile(aiConfigFile()); err == nil {
		c2 := &AIConfig{}
		if json.Unmarshal(b, c2) == nil {
			return c2
		}
	}
	return cfg
}

func saveAIConfig(cfg *AIConfig) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(aiConfigFile(), b, 0o644)
}

// resolveEngine 根据用户 engine 参数与 AI 配置决定实际去广告引擎（basic / ai）
func resolveEngine(engine string, cfg *AIConfig) string {
	switch engine {
	case "ai":
		return "ai"
	case "basic":
		return "basic"
	case "":
		fallthrough
	default:
		switch cfg.Mode {
		case "ai":
			if cfg.Enabled {
				return "ai"
			}
		case "auto":
			if cfg.Enabled && cfg.APIURL != "" && cfg.APIKey != "" {
				return "ai"
			}
		}
		return "basic"
	}
}

func shortURL(u string, n int) string {
	if len(u) <= n {
		return u
	}
	return u[:n] + "…"
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// aiDetectAdIndexes 调用外部 AI（OpenAI Chat Completions 兼容）识别广告片段索引
func aiDetectAdIndexes(rawURL string, segs []Segment, cfg *AIConfig) ([]int, error) {
	if !cfg.Enabled || cfg.APIURL == "" || cfg.APIKey == "" {
		return nil, fmt.Errorf("AI 未启用或未配置")
	}
	if len(segs) == 0 || len(segs) > cfg.MaxSegments {
		return nil, fmt.Errorf("片段数 %d 不在可送审范围(≤%d)", len(segs), cfg.MaxSegments)
	}
	lines := make([]string, 0, len(segs))
	for i, s := range segs {
		dur := s.Duration
		if dur <= 0 {
			dur = 0
		}
		lines = append(lines, fmt.Sprintf("%d|%.1f|%s", i, dur, shortURL(s.AbsURI, 100)))
	}
	prompt := cfg.Prompt
	if prompt == "" {
		prompt = "识别以下视频片段中的广告，返回广告片段索引 JSON 数组，仅返回数组。"
	}
	if codeHint := regexp.MustCompile(`[【}]`).MatchString(prompt); codeHint {
		// 已有返回格式引导则保留原文
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"model":       cfg.Model,
		"messages":    []map[string]string{{"role": "system", "content": prompt + " 只输出 JSON 数组，如 [2,5,8]，不含其它文字。"}, {"role": "user", "content": strings.Join(lines, "\n")}},
		"temperature": 0,
		"max_tokens":  1000,
	})
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 25
	}
	req, err := http.NewRequest("POST", cfg.APIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("AI HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if len(r.Choices) == 0 {
		return nil, fmt.Errorf("AI 无返回")
	}
	content := stripCodeFence(r.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("AI 返回为空")
	}
	var idx []int
	if err := json.Unmarshal([]byte(content), &idx); err != nil {
		// 尝试提取数字
		re := regexp.MustCompile(`\d+`)
		for _, m := range re.FindAllString(content, -1) {
			if n, e := strconv.Atoi(m); e == nil {
				idx = append(idx, n)
			}
		}
	}
	return idx, nil
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

// cleanOne 主流程：抓取-解析-检测-输出（保持原版本行为不变，仅供既有接口复用）
func cleanOne(rawURL string, aggresive bool, engine string) ParseResult {
	return runClean(rawURL, aggresive, engine, nil)
}

// cleanEnhanced 新版增强测试播放引擎：在基础去广告之外叠加增强检测（边界短簇 + 平台广告域前缀）
// 独立实现、不改动原 cleanOne，供「新版增强测试播放」后台与 /api/clean/enhanced 接口使用。
func cleanEnhanced(rawURL, engine string) ParseResult {
	return runClean(rawURL, true, engine, enhancedDetectAds)
}

// runClean 去广告公共流程：抓取-解析-检测-输出。enhanced 为空则行为与 cleanOne 完全一致，
// 非空则在 detectAds 后追加一次增强检测（不改动原检测结果，只新加标记）。
func runClean(rawURL string, aggresive bool, engine string, enhanced func(segs []Segment)) ParseResult {
	start := time.Now()
	res := ParseResult{}
	if rawURL == "" {
		res.Message = "缺少 url 参数"
		return res
	}
	// 解析去广告引擎（basic/ai）
	aiCfg := loadAIConfig()
	eng := resolveEngine(engine, aiCfg)
	res.Engine = eng
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

	// 增强检测：仅当调用方传入时叠加（新版增强测试播放引擎：平台广告域 + 边界短簇 + 中插短簇）
	if enhanced != nil {
		enhanced(segs)
	}

	// AI 去广告：规则检测后追加 AI 识别标记（失败自动回退到规则结果，不影响播放）
	if eng == "ai" && len(segs) > 0 {
		if idx, err := aiDetectAdIndexes(rawURL, segs, aiCfg); err == nil {
			for _, i := range idx {
				if i >= 0 && i < len(segs) && !segs[i].IsAd {
					segs[i].IsAd = true
					segs[i].AdReason = "ai_model"
					res.AiHits++
				}
			}
		}
	}
	// 播放安全兜底：启发式把整部误删过大比例时回退，宁可不删广告也要能正常播放
	applyAdSafetyRoof(segs)

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
		msg := fmt.Sprintf("解析 %d 段，识别广告 %d 段（%.1f%%），保留 %d 段", len(segs), adCount, res.AdRatio, res.KeptSegments)
		if eng == "ai" {
			msg += " · 引擎=AI"
		}
		res.Message = msg
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
	Requests  int64            // 接口调用总数
	Errors    int64            // 失败请求数
	TotalSegs int64            // 累计解析片段数
	AdSegs    int64            // 累计标记广告片段数
	KeptSegs  int64            // 累计保留片段数
	Calls     map[string]int64 // 按接口累计调用次数
	Recent    []CallRecord     // 最近调用记录（环形，最多 30 条）
	LastMsg   string
	LastAt    time.Time
	LastURL   string
}

// CallRecord 一次接口调用明细
type CallRecord struct {
	Time string  `json:"time"`
	API  string  `json:"api"`
	URL  string  `json:"url"`
	OK   bool    `json:"ok"`
	Ms   float64 `json:"ms"`
	Msg  string  `json:"msg,omitempty"`
}

var stats = &ServerStats{StartedAt: time.Now(), Calls: map[string]int64{}}

func (s *ServerStats) snapshot() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ratio := 0.0
	if s.TotalSegs > 0 {
		ratio = float64(s.AdSegs) / float64(s.TotalSegs) * 100
	}
	// 拷贝，避免调用方持有内部切片指针
	recent := make([]CallRecord, len(s.Recent))
	copy(recent, s.Recent)
	calls := make(map[string]int64, len(s.Calls))
	for k, v := range s.Calls {
		calls[k] = v
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
		"calls":      calls,
		"recent":     recent,
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

// recordClean 清洗一次解析结果，写入片段统计 + 调用明细
func recordClean(res ParseResult, rawURL string) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.Requests++
	stats.Calls["/api/clean"]++
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
	stats.Recent = append(stats.Recent, CallRecord{
		Time: time.Now().Format("15:04:05"), API: "/api/clean", URL: rawURL,
		OK: res.Success, Ms: math.Round(res.DurationSec * 1000), Msg: stats.LastMsg,
	})
	if len(stats.Recent) > 30 {
		stats.Recent = stats.Recent[len(stats.Recent)-30:]
	}
}

// recordCall 记录一次通用接口调用明细（用于官替 / 资源站管理 / 统计等）
func recordCall(api, reqURL string, ok bool, ms float64, msg string) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.Requests++
	stats.Calls[api]++
	stats.LastURL = reqURL
	stats.LastAt = time.Now()
	stats.LastMsg = msg
	if !ok {
		stats.Errors++
	}
	stats.Recent = append(stats.Recent, CallRecord{
		Time: time.Now().Format("15:04:05"), API: api, URL: reqURL,
		OK: ok, Ms: math.Round(ms), Msg: msg,
	})
	if len(stats.Recent) > 30 {
		stats.Recent = stats.Recent[len(stats.Recent)-30:]
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
  .prog-wrap{height:6px;background:#eee;border-radius:4px;overflow:hidden;margin:10px 0;position:relative}
  .prog-bar{height:100%;width:40%;background:linear-gradient(90deg,#7e22ce,#c026d3);border-radius:4px;animation:prog 1s ease-in-out infinite}
  @keyframes prog{0%{margin-left:-40%}100%{margin-left:100%}}
  .prog-track{height:10px;background:#eee;border-radius:6px;overflow:hidden;margin:8px 0 4px}
  .prog-fill{height:100%;width:0;background:linear-gradient(90deg,#7e22ce,#c026d3);border-radius:6px;transition:width .35s ease}
  details.site{margin:6px 0;border:1px solid #eee;border-radius:10px;background:#fff}
  details.site summary{cursor:pointer;padding:8px 12px;font-size:13.5px;font-weight:600;color:#581c87;list-style:none}
  details.site summary::before{content:"▸ ";color:#c026d3}
  details.site[open] summary::before{content:"▾ "}
  details.site summary::-webkit-details-marker{display:none}
  details.site[open] summary{border-bottom:1px solid #f0f0f0}
  .siterow{display:flex;align-items:center;gap:8px;padding:6px 12px;border-bottom:1px solid #f6f6f6;font-size:13px;min-width:0;flex-wrap:wrap}
  .siterow:last-child{border-bottom:0}
  .siterow label.sw{flex:0 0 auto}
  .siterow b{flex:0 1 auto;min-width:0;max-width:34%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .siterow .note{flex:1 1 120px;min-width:80px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .siterow .btn{flex-shrink:0;white-space:nowrap}
  #siteList{max-width:100%;overflow:hidden}
  .sitedtl{display:none;padding:8px 12px 12px;background:#fafafa;font-size:12.5px;color:#374151;word-break:break-all}
  .sitedtl code{background:#f3f4f6;padding:2px 6px;border-radius:6px;color:#7e22ce}
  .btn.gh{background:#fff;color:#581c87;border:1px solid #d1d5db;box-shadow:none;padding:3px 10px;font-size:12px}
  .btn.mini{padding:5px 12px;font-size:12px}
</style>
</head>
<body>
<div class="wrap">
  <div class="header">
    <div>
      <h1>🎬 MXGT-Go 后台</h1>
      <div class="ver">M3U8 广告分析与去广告 · 单文件服务 <span id="ver"></span></div>
    </div>
    <div style="text-align:right">
      <div style="font-size:14px;font-weight:700;color:#ffe4f1">开发者 · ssmhdssmhd</div>
      <div class="ver" style="font-size:11px;margin-top:2px">品牌 MXGT</div>
    </div>
    <button class="btn ghost" onclick="openPwd()">🔑 改密码</button>
    <button class="btn ghost" onclick="doLogout()">⎋ 退出</button>
    <button class="btn ghost" onclick="refreshStats()">⟳ 刷新</button>
  </div>

  <div class="grid" id="statGrid"></div>

  <div class="panel">
    <h2>🔬 解析测试</h2>
    <div class="row">
      <input type="url" id="urlInput" placeholder="粘贴 M3U8 地址，如 https://example.com/playlist.m3u8"
             onkeydown="if(event.key==='Enter')runClean()">
      <label class="sw"><input type="checkbox" id="aggrOpt"> 聚合识别(opt=aggresive)</label>
      <label class="sw">去广告引擎
        <select id="engOpt" style="padding:6px 8px;border-radius:8px;border:1px solid #d1d5db">
          <option value="">基础(默认)</option><option value="ai">AI 审核</option>
        </select></label>
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
    <h2>🔁 官替链路（官方视频页 → 资源站 → 无广告）</h2>
    <div class="row">
      <input type="url" id="repInput" placeholder="粘贴官方视频页，如 https://m.v.qq.com/x/m/play?cid=..&vid=.."
             onkeydown="if(event.key==='Enter')runReplace()">
      <button class="btn" onclick="runReplace()">⚡ 官替解析</button>
    </div>
    <div class="stat-line" id="repStats" style="display:block"></div>
    <pre id="repOut"></pre>
  </div>

  <div class="panel">
    <h2>🗺️ 官替映射专区 <span class="muted">（🧠 已开启自动学习：官替解析成功后自动生成「官方剧名→资源站标准剧名」映射，无需手动逐个添加；手动抓取表单仍可用）</span></h2>
    <div class="row" style="flex-wrap:wrap">
      <input id="mapUrl" placeholder="粘贴真实官方视频页链接，如腾讯/爱奇艺/优酷…，自动抓取剧名与集数" style="flex:1;min-width:280px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="fetchMap()">🔍 从链接抓取</button>
      <span class="muted">自定义提取字段</span>
      <input id="mapSel" placeholder="可选：页面字段提取正则（第1捕获组），如 \"name\":\"([^\"]+)\"、<h1>([^<]+)</h1>" style="flex:1;min-width:240px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db" title="页面结构变化时，可自定义正则从真实页面提取标题/剧名（第 1 捕获组），留空自动识别">
      <span class="muted" id="mapFetchInfo" style="width:100%"></span>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <span class="muted">官方剧名(from)</span>
      <input id="mFrom" placeholder="官方剧名，如：独剑九天" style="width:170px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">→ 标准剧名(to)</span>
      <input id="mTo" placeholder="资源站标准剧名，如：独剑九天" style="width:170px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">平台</span>
      <input id="mPlat" placeholder="如：腾讯视频（可选）" style="width:140px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="addMap()">➕ 添加映射</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="loadMaps()">⟳ 刷新</button>
    </div>
    <table>
      <thead><tr><th>平台</th><th>官方剧名 (from)</th><th>→ 标准剧名 (to)</th><th>备注</th><th>操作</th></tr></thead>
      <tbody id="mapList"></tbody>
    </table>
  </div>

  <div class="panel">
    <h2>🛰️ 自动更新官方 <span class="muted">（平台匹配规则：按优先级顺序 → 域名 + URL正则 → 标题选择器提取 → 自动映射剧名/集数到映射表）</span></h2>
    <div class="row" style="flex-wrap:wrap;margin-bottom:4px">
      <span class="muted" style="line-height:2">⚡ <b>一键映射</b>（内置各官方平台链接，点击即实时抓取并自动映射到专区，无需输入链接）：</span>
      <span id="ocLinks"></span>
      <span class="muted" id="ocInfo" style="width:100%"></span>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <input id="pfUrl" placeholder="粘贴真实官方链接，自动匹配平台并提取「影视剧名 + 剧集集数」" style="flex:1;min-width:280px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="pfFetch()">🔍 从链接自动获取</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="loadPlatforms()">⟳ 刷新</button>
      <span class="muted" id="pfInfo" style="width:100%"></span>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <span class="muted">平台名称</span>
      <input id="pfPlatform" placeholder="如：腾讯视频" style="width:110px;padding:7px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">域名</span>
      <input id="pfDomain" placeholder="如：v.qq.com" style="width:130px;padding:7px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">URL匹配正则</span>
      <input id="pfUrlRe" placeholder="可选，如 (?i)play\?cid=([0-9]+)" style="flex:1;min-width:200px;padding:7px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">标题选择器</span>
      <input id="pfTitleSel" placeholder="可选，HTML提取标题正则（第1捕获组）" style="flex:1;min-width:200px;padding:7px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">优先级</span>
      <input id="pfPriority" type="number" value="10" style="width:64px;padding:7px 8px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="pfSave()">💾 保存配置</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="pfResetForm()">清空</button>
    </div>
    <table>
      <thead><tr><th>顺序</th><th>平台</th><th>域名</th><th>URL匹配正则</th><th>标题选择器</th><th>优先级</th><th>启用</th><th>备注</th><th>操作</th></tr></thead>
      <tbody id="pfList"></tbody>
    </table>
  </div>

  <div class="panel">
    <h2>🏢 资源站管理 <span class="muted">（默认全部禁用，按需启用；失效站自动归入下方「❌ 已失效/暂停」折叠区查看）</span></h2>
    <div class="row" style="flex-wrap:wrap">
      <input id="nsName" placeholder="名称（必填）" style="width:150px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <input id="nsApi" placeholder="采集接口 https://…/api.php/provide/vod/（必填）" style="flex:1;min-width:260px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <input id="nsSite" placeholder="官网（可选）" style="width:190px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <input id="nsNote" placeholder="备注（可选）" style="width:150px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="addSite()">➕ 添加资源站</button>
    </div>
    <div class="row" style="flex-wrap:wrap;align-items:center">
      <input id="impUrl" placeholder="⚡ 从链接自动导入：粘贴含资源站列表的网址（含 https://），点一下自动抓取并批量添加" style="flex:1;min-width:320px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="importSites(false)">⚡ 一键从链接导入</button>
      <span class="muted">或</span>
      <button class="btn ghost" style="padding:7px 14px;font-size:12px" onclick="toggleImportText(this)">📋 粘贴文本</button>
    </div>
    <div class="row" id="impTextWrap" style="flex-wrap:wrap;display:none">
      <textarea id="impText" rows="6" placeholder="粘贴资源站文本（每行一个，格式：名称：官网 采集：接口 备注；支持 kdocs 表格文本「序号|名：官网 | 采集：接口 | 备注」）" style="flex:1 1 100%;min-width:100%;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db;box-sizing:border-box;font-family:inherit"></textarea>
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="importSites(true)">⚡ 从粘贴文本导入</button>
    </div>
    <div class="muted" id="impMsg" style="margin-top:6px;word-break:break-all"></div>
    <div class="row" style="flex-wrap:wrap">
      <input id="siteSearch" placeholder="🔍 搜索站点/备注/接口…" style="flex:0 0 260px;padding:9px 12px;border-radius:10px;border:1px solid #d1d5db" onkeyup="renderSites()">
      <span class="muted" id="siteCount"></span>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="loadSites()">⟳ 刷新</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="setAllSites(false)">全部折叠</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="setAllSites(true)">全部展开</button>
      <label class="sw"><input type="checkbox" id="siteM3U8" onchange="toggleSiteM3U8(this.checked)"> 仅 m3u8 播放地址</label>
      <span class="muted" id="siteKwLabel">失效站见下方折叠区</span>
      <span class="muted">测试词</span>
      <input id="siteKw" value="庆余年" style="width:110px;padding:6px;border-radius:8px;border:1px solid #d1d5db">
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="checkSites()">🧹 检测并屏蔽失效站</button>
    </div>
    <div class="prog-wrap" id="siteProg" style="display:none"><div class="prog-bar"></div></div>
    <div id="siteList"></div>
  </div>

  <div class="panel">
    <h2>🆕 新版增强测试播放 <span class="muted">（独立引擎 /api/clean/enhanced，不改动原有解析测试；在基础去广告上叠加「平台广告域关键词 + 片头片尾超短簇」高置信增强检测）</span></h2>
    <div class="row">
      <input type="url" id="enhInput" placeholder="粘贴 M3U8 地址，走增强检测引擎"
             onkeydown="if(event.key==='Enter')runEnhanced()">
      <label class="sw">去广告引擎
        <select id="enhEngOpt" style="padding:6px 8px;border-radius:8px;border:1px solid #d1d5db">
          <option value="">基础(默认)</option><option value="ai">AI 审核</option>
        </select></label>
      <button class="btn" onclick="runEnhanced()">⚡ 增强解析</button>
      <button class="btn ghost" onclick="playEnhanced()">▶ 直接播放增强结果</button>
    </div>
    <div class="stat-line" id="enhStats"></div>
    <div class="stat-line" id="enhPlaySrc" style="display:none"></div>
    <pre id="enhOut"></pre>
  </div>

  <div class="panel">
    <h2>⏱️ 非正片区间标注 <span class="muted">（SponsorBlock 思路：维护片头/片尾/赞助/三连/其他 时间戳区间，存 skip_ranges.json，供播放器按区间跳过）</span></h2>
    <div class="row" style="flex-wrap:wrap">
      <span class="muted">视频标识(空=全局)</span>
      <input id="skKey" placeholder="剧名 第N集 / 留空对所有视频生效" style="width:180px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">起始(s)</span>
      <input id="skStart" type="number" min="0" step="0.1" placeholder="0" style="width:90px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">结束(s)</span>
      <input id="skEnd" type="number" min="0" step="0.1" placeholder="30" style="width:90px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <span class="muted">类型</span>
      <select id="skType" style="padding:8px;border-radius:10px;border:1px solid #d1d5db">
        <option value="intro">片头</option><option value="outro">片尾</option><option value="sponsor">赞助</option>
        <option value="selfpromo">自我推广</option><option value="interaction">互动/三连</option><option value="other">其他</option>
      </select>
      <input id="skNote" placeholder="备注（可选）" style="width:160px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="addSkip()">➕ 添加区间</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="loadSkips()">⟳ 刷新</button>
      <span class="muted" id="skInfo"></span>
    </div>
    <table>
      <thead><tr><th>视频</th><th>起始</th><th>结束</th><th>类型</th><th>备注</th><th>来源</th><th>操作</th></tr></thead>
      <tbody id="skList"></tbody>
    </table>
  </div>

  <div class="panel">
    <h2>💬 弹幕过滤规则库 <span class="muted">（独立模块，存 danmaku_rules.json；关键词/正则规则过滤广告/刷屏/剧透等弹幕，未来接弹幕源即用）</span></h2>
    <div class="row" style="flex-wrap:wrap">
      <input id="dmPattern" placeholder="规则内容（必填）：keyword=包含词，regex=正则表达式" style="flex:1;min-width:240px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <select id="dmType" style="padding:8px;border-radius:10px;border:1px solid #d1d5db">
        <option value="keyword">关键词</option><option value="regex">正则</option>
      </select>
      <select id="dmCategory" style="padding:8px;border-radius:10px;border:1px solid #d1d5db">
        <option value="ad">广告</option><option value="spam">垃圾刷屏</option><option value="spoiler">剧透</option>
        <option value="attack">人身攻击</option><option value="nsfw">低俗</option><option value="other">其他</option>
      </select>
      <input id="dmNote" placeholder="备注（可选）" style="width:160px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="addDanmaku()">➕ 添加规则</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="loadDanmaku()">⟳ 刷新</button>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <span class="muted">测试弹幕文本</span>
      <input id="dmTestText" placeholder="输入一条弹幕，测试是否命中规则" style="flex:1;min-width:240px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="testDanmaku()">🔍 测试</button>
      <span class="muted" id="dmTestInfo"></span>
    </div>
    <table>
      <thead><tr><th>类型</th><th>规则内容</th><th>分类</th><th>命中</th><th>备注</th><th>启用</th><th>操作</th></tr></thead>
      <tbody id="dmList"></tbody>
    </table>
  </div>

  <div class="panel">
    <h2>🔍 广告核查 <span class="muted">（列出每一个「不连贯」片段：拼接点/时长突变/偏短/规则已删，逐段播放核查，确认广告一键写入跳过区间——规则漏检的兜底，播放时自动跳过）</span></h2>
    <div class="row" style="flex-wrap:wrap">
      <input id="adInput" placeholder="粘贴 M3U8 地址，核查不连贯片段是否有广告" style="flex:1;min-width:300px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db"
             onkeydown="if(event.key==='Enter')runAudit()">
      <input id="adKey" placeholder="视频标识（剧名 第N集，空=全局）" style="width:200px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" onclick="runAudit()">🔎 开始核查</button>
    </div>
    <div class="stat-line" id="adStats"></div>
    <div class="stat-line" id="adPlayerTip" style="display:none"></div>
    <table>
      <thead><tr><th>时间轴(过滤后)</th><th>时长</th><th>可疑</th><th>片段</th><th>操作</th></tr></thead>
      <tbody id="adList"></tbody>
    </table>
    <details><summary style="cursor:pointer;color:#94a3b8;margin-top:8px">🗑 规则已删广告段（共 <span id="adDelCnt">0</span> 段）</summary>
      <table><thead><tr><th>原始序号</th><th>时长</th><th>原因</th><th>片段</th></tr></thead><tbody id="adDeleted"></tbody></table>
    </details>
  </div>

  <div class="panel">
    <h2>🔄 远程在线更新</h2>
    <div class="row">
      <div class="stat-line" id="updInfo" style="display:block"><!--UPD_BLOCK--></div>
    </div>
    <div class="row">
      <button class="btn" onclick="checkUpdate()">🔍 检查更新</button>
      <button class="btn ghost" onclick="applyUpdate()">⬇ 下载并更新重启</button>
    </div>
  </div>

  <div class="panel">
    <h2>📚 HTTP 接口说明 <span class="muted">（类型：🟢客户端 / 🟣服务端 / 🟠后台管理 / ⚪通用）</span></h2>
    <table>
      <tr><th>类型</th><th>接口</th><th>说明</th></tr>
      <tr><td>⚪ 通用</td><td><code>GET /</code> / <code>GET /mxadmin</code></td><td>落地页（不进入后台）/ 后台管理页（本页）</td></tr>
      <tr><td>⚪ 通用</td><td><code>GET /api/clean?url=&lt;m3u8&gt;</code></td><td>返回过滤后的无广告 M3U8 纯文本（绝对地址）——客户端可直接喂播放器，服务端可抓取无广告地址</td></tr>
      <tr><td>🟣 服务端</td><td><code>GET /api/clean/json?url=&lt;m3u8&gt;</code></td><td>返回 JSON：统计 + 过滤后文本 + 每个片段明细——服务端二次分析/审计</td></tr>
      <tr><td>🟣 服务端</td><td><code>GET /api/clean?url=&lt;m3u8&gt;&amp;opt=aggresive</code></td><td>开启聚合聚类识别（可能误伤统一切片正片）</td></tr>
      <tr><td>🟣 服务端</td><td><code>GET /api/clean/enhanced[/json]?url=&lt;m3u8&gt;</code></td><td>🆕 新版增强测试播放：独立引擎，叠加平台广告域关键词 + 片头片尾超短簇高置信检测</td></tr>
      <tr><td>🟣 服务端</td><td><code>GET /api/replace?url=&lt;官方视频页&gt;</code></td><td>官替链路：资源站匹配后返回无广告直链 ad_skip_url——服务端从官方页解析直链供下发</td></tr>
      <tr><td>🟢 客户端</td><td><code>GET /api/jx?url=&lt;链接&gt;&amp;engine=basic/auto/ai</code></td><td>影视 App / TVBox 等通用兼容接口（JSON，带跨域）</td></tr>
      <tr><td>🟢 客户端</td><td><code>GET /api/jx/client?url=&lt;链接&gt;&amp;engine=basic/auto/ai</code></td><td>🆕 客户端调用接口：精简播放字段，msg=url 可播放地址，体积小响应快</td></tr>
      <tr><td>🟣 服务端</td><td><code>GET /api/jx/server?url=&lt;链接&gt;&amp;engine=basic/auto/ai</code></td><td>🆕 服务器调用 API：附带 detail 完整明细（去广告统计 / 官替全过程）</td></tr>
      <tr><td>🟢 客户端</td><td><code>GET /api/play?url=&lt;m3u8/分片/密钥&gt;</code></td><td>播放代理：服务端内置增强去广告 + 分片同源代理（解决跨域/限速卡顿）</td></tr>
      <tr><td>🟣 服务端</td><td><code>GET /api/audit?url=&lt;m3u8&gt;</code></td><td>广告核查：列出所有不连贯片段（拼接点/时长突变/偏短），供人工逐段核对</td></tr>
      <tr><td>🟢 客户端</td><td><code>GET /player?url=&lt;去广告直链&gt;&amp;title=&lt;剧名&gt;</code></td><td>独立外置播放页（开放，hls.js/原生播放，全站跨域）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/maps</code> / <code>/add</code> / <code>/delete</code> / <code>/fetch</code></td><td>官替映射列表 / 添加 / 删除 / 从真实链接抓取剧名集数（需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/platforms</code> / <code>/add</code> / <code>/update</code> / <code>/delete</code> / <code>/fetch</code> / <code>/links</code> / <code>/oneclick</code></td><td>官方平台自动更新配置：列表 / 增 / 改 / 删 / 抓取 / 内置一键映射 / 无脑映射（需登录）</td></tr>
      <tr><td>⚪ 通用</td><td><code>GET /api/skip</code> / <code>/add</code> / <code>/delete</code></td><td>⏱️ 非正片区间标注：客户端播放器读取跳过区间，服务端增删标注（增删需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/danmaku</code> / <code>/add</code> / <code>/toggle</code> / <code>/delete</code> / <code>/test</code></td><td>💬 弹幕过滤规则库：列表 / 添加 / 启停 / 删除 / 单条命中测试（需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/sites</code> / <code>/toggle</code> / <code>/test</code></td><td>资源站列表（默认隐藏失效）/ 启停 / 搜索测试（需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>POST /api/sites/add</code> / <code>/update</code> / <code>/delete</code> / <code>/check</code></td><td>添加 / 编辑 / 删除资源站 / 异步批量检测（并发）并屏蔽失效站（需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>POST /api/sites/import</code></td><td>🆕 一键自动导入资源站：从链接抓取或粘贴文本（支持 kdocs 表格文本）解析并批量添加（需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/sites/m3u8</code> / <code>POST /api/sites/m3u8</code></td><td>🆕 资源站搜索「仅 m3u8 播放地址」开关：读取 / 设置（设置需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/sites/check/progress?task=</code></td><td>查询批量检测任务进度（供进度条轮询，需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/update/check</code></td><td>检查远程是否有新版本（读取线上 latest.json，需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>POST /api/update/apply</code></td><td>下载新版本 zip 并自动替换重启（需登录）</td></tr>
      <tr><td>🟠 后台管理</td><td><code>GET /api/ai/config</code> / <code>POST /api/ai/config</code></td><td>查看（key 打码）/ 更新 AI 去广告配置（更新需登录）</td></tr>
      <tr><td>⚪ 通用</td><td><code>GET /api/stats</code></td><td>运行统计（JSON）——监控服务状态与调用量</td></tr>
      <tr><td>⚪ 通用</td><td><code>GET /healthz</code></td><td>健康检查——探活/负载均衡健康检测</td></tr>
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
  // 版本（当前/最新）已由服务端渲染；这里仅刷新状态备注，失败不覆盖版本信息
  el('updInfo').innerHTML='正在检查更新… <span class="upd-note"></span>';
  fetch('/api/update/check').then(function(r){return r.json()}).then(function(d){
    let s='当前版本 '+d.current+' → 最新版本 '+d.latest;
    if(d.has_update){ s+='　<span style="color:#dc2626;font-weight:700">⚠️ 存在新版本</span>'; }
    else { s+='　<span style="color:#16a34a">已是最新</span>'; }
    if(d.message){ s+='　<span class="upd-note" style="color:#9ca3af">('+d.message+')</span>'; }
    el('updInfo').innerHTML=s;
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
refreshStats(); setInterval(refreshStats,5000); checkUpdate(); loadSites(); loadMaps(); loadPlatforms(); loadOneClickLinks(); loadSkips(); loadDanmaku(); initSkipAuto();

// —— 新版增强测试播放（/api/clean/enhanced）——
function enhBuildURL(url, eng){
  return '/api/clean/enhanced/json?url='+encodeURIComponent(url)+(eng?'&engine='+eng:'');
}
async function runEnhanced(){
  const url=el('enhInput').value.trim();
  el('enhStats').style.display='none'; el('enhOut').style.display='none';
  if(!url){alert('请先粘贴 M3U8 地址');return;}
  el('enhOut').style.display='block';el('enhOut').textContent='请求中…';
  const eng=el('enhEngOpt').value;
  try{
    const r=await fetch(enhBuildURL(url,eng));
    const j=await r.json();
    if(!j.success){el('enhOut').textContent=j.message||'解析失败';el('enhStats').style.display='block';el('enhStats').textContent='✕ '+ (j.message||'');return;}
    el('enhStats').style.display='block';
    el('enhStats').textContent=(j.message||'')+'　引擎='+(j.engine||'-');
    let txt='总 '+j.total_segments+' 段 · 广告 '+j.ad_count+' 段 ('+(j.ad_ratio||0).toFixed(1)+'%) · 保留 '+j.kept_segments+' 段\n\n';
    const rows=(j.segments||[]).map(function(s){
      return (s.is_ad?'[AD '+(s.ad_reason||'')+'] ':'[OK] ')+'#'+s.index+'  '+(s.duration||0).toFixed(2)+'s  '+(s.abs_uri||s.uri||'');
    });
    txt+=rows.join('\n');
    el('enhOut').textContent=txt;
  }catch(e){el('enhOut').textContent='解析失败: '+e.message}
}
async function playEnhanced(){
  const url=el('enhInput').value.trim();
  if(!url){alert('请先粘贴 M3U8 地址');return;}
  // /api/play 播放代理已内置增强去广告（平台广告域 + 边界短簇 + 中插短簇），分片同源防卡顿
  el('enhPlaySrc').style.display='block';
  el('enhPlaySrc').textContent='播放地址: '+proxyPlayURL(url)+'　（服务端已内置增强去广告）';
  playURLWith(url, function(){ el('playSrc').textContent='（新版增强）已尝试播放'; });
}
// 播放走本服务代理 /api/play（分片同源 + 服务端 UA/Referer/超时），解决第三方 m3u8 跨域/限速卡顿
// 已是本服务地址（/api/play、/api/clean 等）不再二次代理，避免本服务请求本服务多一层往返
function isLocalProxy(u){return u.indexOf('/api/play')===0||u.indexOf('/api/clean')===0||/^https?:\/\/[^/]*\/api\/(play|clean)/.test(u);}
function proxyPlayURL(u){return isLocalProxy(u)?u:'/api/play?url='+encodeURIComponent(u);}
// —— 非正片区间自动跳过（内嵌/占位广告：播放器按区间 seek 跳过，SponsorBlock 思路）——
let __skipRanges=[],__skipBound=false,__lastSkip=0;
function setSkipRanges(rs){__skipRanges=(rs||[]).filter(function(r){return r&&r.start<r.end;});}
function bindSkipAuto(v){
  if(!v||__skipBound)return;__skipBound=true;
  v.addEventListener('timeupdate',function(){
    const t=v.currentTime,now=Date.now();
    if(now-__lastSkip<300)return; // 防连续 seek 死循环
    for(let i=0;i<__skipRanges.length;i++){
      const rg=__skipRanges[i];
      if(t>rg.start&&t<rg.end-0.3){__lastSkip=now;v.currentTime=rg.end+0.01;break;}
    }
  });
}
function initSkipAuto(){ // 默认加载全局区间（所有视频通用的片头片尾等），官替结果会覆盖
  fetch('/api/skip').then(function(r){return r.json()}).then(function(d){if(d.ranges)setSkipRanges(d.ranges);}).catch(function(){});
}
function isDirectURL(u){return /\.(mp4|mkv|webm|flv)(\?|$)/i.test(u);}
// hls.js 抗卡顿配置：加大缓冲 + 分片/清单失败重试（指数退避）+ 软件解密兜底
function hlsConfig(){return {
  enableWorker:true,
  maxBufferLength:60,maxMaxBufferLength:180,backBufferLength:30,startLevel:-1,maxBufferSize:60*1000*1000,
  fragLoadPolicy:{default:{maxNumRetry:5,retryDelay:400,backoff:'exponential',maxRetryDelay:4000}},
  manifestLoadPolicy:{default:{maxNumRetry:3,retryDelay:300}},
  levelLoadPolicy:{default:{maxNumRetry:3,retryDelay:300}},
  enableSoftwareAES:true,
  xhrSetup:function(xhr){xhr.withCredentials=false;xhr.setRequestHeader('Origin',location.origin);}
};}
function playURLWith(url, cb){
  const v=el('player');
  const panel=el('playPanel'); if(panel)panel.style.display='block';
  const srcEl=el('playSrc'); if(srcEl){srcEl.style.display='block';srcEl.textContent='播放源: '+url;}
  const src=isDirectURL(url)?url:proxyPlayURL(url);
  bindSkipAuto(v); // 非正片区间自动跳过（内嵌/占位广告）
  function destroy(){ if(window.__hls){window.__hls.destroy();window.__hls=null;} }
  destroy();
  if(navigator.userAgent.indexOf('Safari')>=0 && window.Hls===undefined){
    v.src=src; v.play().catch(function(){});
  } else {
    loadHls(function(){
      if(!window.Hls.isSupported()){v.src=src;v.play().catch(function(){});return;}
      const hls=new Hls(hlsConfig()); window.__hls=hls;
      hls.loadSource(src); hls.attachMedia(v);
      hls.on(Hls.Events.MANIFEST_PARSED,function(){v.play().catch(function(){});});
    });
  }
  if(cb)cb();
}
// —— 非正片区间标注（/api/skip）——
const skipTypeMap={'intro':'片头','outro':'片尾','sponsor':'赞助','selfpromo':'自我推广','interaction':'互动/三连','other':'其他'};
async function loadSkips(){
  try{
    const d=await getJSON('/api/skip');
    el('skInfo').textContent='共 '+d.total+' 个区间';
    el('skList').innerHTML=(d.ranges||[]).map(function(r){
      return '<tr><td>'+(r.key||'🌐 全局')+'</td><td>'+r.start.toFixed(1)+'s</td><td>'+r.end.toFixed(1)+'s</td><td>'+(skipTypeMap[r.type]||r.type)+'</td><td>'+(r.note||'')+'</td><td>'+(r.source||'')+'</td>'+
        '<td><button class="btn gh" onclick="delSkip(\''+r.id+'\')">🗑 删除</button></td></tr>';
    }).join('')||'<tr><td colspan="7" class="muted">暂无区间</td></tr>';
  }catch(e){el('skInfo').textContent='加载失败: '+e.message}
}
async function addSkip(){
  const start=parseFloat(el('skStart').value); const end=parseFloat(el('skEnd').value);
  if(!(start>=0)||!(end>start)){alert('请输入有效起始/结束（0 ≤ 起始 < 结束）');return;}
  const d=await fetch('/api/skip/add',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({key:el('skKey').value.trim(),start:start,end:end,type:el('skType').value,note:el('skNote').value.trim()})}).then(function(r){return r.json()});
  if(d.success){el('skKey').value='';el('skStart').value='';el('skEnd').value='';el('skNote').value='';loadSkips();}
  else{alert(d.message||'添加失败');}
}
async function delSkip(id){
  if(!confirm('确定删除该区间吗？'))return;
  const d=await fetch('/api/skip/delete?id='+encodeURIComponent(id),{method:'POST'}).then(function(r){return r.json()});
  if(d.success){loadSkips();}else{alert(d.message||'删除失败');}
}
// —— 广告核查（/api/audit）：逐段核查不连贯片段，确认广告写入跳过区间 ——
async function runAudit(){
  const url=el('adInput').value.trim();
  el('adStats').style.display='none';
  if(!url){alert('请先粘贴 M3U8 地址');return;}
  el('adStats').style.display='block'; el('adStats').textContent='核查中…';
  el('adList').innerHTML=''; el('adDeleted').innerHTML=''; el('adDelCnt').textContent='0';
  el('adPlayerTip').style.display='none';
  try{
    const r=await fetch('/api/audit?url='+encodeURIComponent(url));
    const j=await r.json();
    if(!j.success){el('adStats').textContent='✕ '+(j.message||'核查失败');return;}
    el('adStats').textContent=j.message;
    const rows=(j.segments||[]).filter(function(s){return s.suspect&&s.suspect.length;});
    el('adList').innerHTML=rows.map(function(s){
      const tag=(s.suspect||[]).map(function(t){return '<span style="color:#d97706;font-weight:600">'+t+'</span>'}).join(' ');
      return '<tr data-idx="'+s.index+'"><td>'+s.start.toFixed(1)+'–'+s.end.toFixed(1)+'s</td><td>'+s.duration.toFixed(2)+'s</td>'+
        '<td>'+tag+'</td><td style="max-width:280px;word-break:break-all;font-size:11px;color:#94a3b8">'+(s.abs_uri||s.uri||'')+'</td>'+
        '<td><button class="btn gh" style="padding:3px 8px;font-size:12px" onclick="adPlay('+s.start.toFixed(3)+','+s.end.toFixed(3)+')">▶ 播放核对</button> '+
        '<button class="btn gh" style="padding:3px 8px;font-size:12px;background:#dc2626;color:#fff" onclick="adMark('+s.start.toFixed(3)+','+s.end.toFixed(3)+')">⛔ 标广告</button> '+
        '<button class="btn gh" style="padding:3px 8px;font-size:12px" onclick="adIgnore(\''+s.index+'\')">✅ 正常</button></td></tr>';
    }).join('')||'<tr><td colspan="5" class="muted">未发现可疑片段，可展开下方查看规则已删广告</td></tr>';
    el('adDelCnt').textContent=(j.ads||[]).length;
    el('adDeleted').innerHTML=(j.ads||[]).map(function(s){
      return '<tr><td>#'+s.index+'</td><td>'+s.duration.toFixed(2)+'s</td><td>'+(s.ad_reason||'')+'</td><td style="max-width:280px;word-break:break-all;font-size:11px;color:#94a3b8">'+(s.abs_uri||s.uri||'')+'</td></tr>';
    }).join('')||'<tr><td colspan="4" class="muted">无</td></tr>';
  }catch(e){el('adStats').textContent='核查失败: '+e.message}
}
function adPlay(start,end){ // 用主播放器加载该 m3u8（经 /api/play 代理），定位到可疑段时间轴核对上下文
  const url=el('adInput').value.trim();
  if(!url){alert('缺少 M3U8 地址');return;}
  el('adPlayerTip').style.display='block';
  el('adPlayerTip').textContent='主播放器已加载该片，定位到 '+start.toFixed(1)+'s–'+end.toFixed(1)+'s（可前后拖动核对上下文）…';
  playURLWith(url, function(){
    const v=el('player');
    const go=function(){ v.currentTime=start; v.play().catch(function(){}); };
    if(v.readyState>=1){go();}else{v.addEventListener('loadedmetadata',go,{once:true});}
  });
}
function adMark(start,end){ // 确认广告 → 写入 skip 区间（过滤后时间轴，与播放器一致）
  const key=el('adKey').value.trim();
  fetch('/api/skip/add',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({key:key,start:start,end:end,type:'sponsor',note:'人工核查确认广告'})})
    .then(function(r){return r.json()})
    .then(function(d){
      if(d.success){loadSkips();alert('✅ 已加入跳过区间 '+(key?('「'+key+'」'):'（全局）')+' '+start+'–'+end+'s，该视频播放时自动跳过');}
      else{alert(d.message||'标记失败（需登录后台）');}
    })
    .catch(function(e){alert('标记失败: '+e.message)});
}
function adIgnore(idx){ // 确认正常 → 本次核查会话隐藏
  const row=document.querySelector('#adList tr[data-idx="'+idx+'"]');
  if(row)row.style.display='none';
}
// —— 弹幕过滤规则库（/api/danmaku）——
const dmCatMap={'ad':'广告','spam':'垃圾刷屏','spoiler':'剧透','attack':'人身攻击','nsfw':'低俗','other':'其他'};
async function loadDanmaku(){
  try{
    const d=await getJSON('/api/danmaku');
    el('dmList').innerHTML=(d.rules||[]).map(function(r){
      return '<tr><td>'+r.type+'</td><td>'+(r.type==='regex'?'<code>'+r.pattern+'</code>':r.pattern)+'</td><td>'+(dmCatMap[r.category]||r.category)+'</td><td>'+r.hits+'</td><td>'+(r.note||'')+'</td>'+
        '<td>'+(r.enabled?'✅':'⛔')+'</td>'+
        '<td><button class="btn gh" onclick="toggleDanmaku(\''+r.id+'\')">启停</button> <button class="btn gh" onclick="delDanmaku(\''+r.id+'\')">🗑 删除</button></td></tr>';
    }).join('')||'<tr><td colspan="7" class="muted">暂无规则</td></tr>';
  }catch(e){el('dmList').innerHTML='<tr><td colspan="7">加载失败</td></tr>';}
}
async function addDanmaku(){
  const pattern=el('dmPattern').value.trim();
  if(!pattern){alert('请输入规则内容');return;}
  const d=await fetch('/api/danmaku/add',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({type:el('dmType').value,pattern:pattern,category:el('dmCategory').value,note:el('dmNote').value.trim()})}).then(function(r){return r.json()});
  if(d.success){el('dmPattern').value='';el('dmNote').value='';loadDanmaku();}
  else{alert(d.message||'添加失败');}
}
async function toggleDanmaku(id){
  await fetch('/api/danmaku/toggle?id='+encodeURIComponent(id),{method:'POST'}).then(function(r){return r.json()});
  loadDanmaku();
}
async function delDanmaku(id){
  if(!confirm('确定删除该规则吗？'))return;
  const d=await fetch('/api/danmaku/delete?id='+encodeURIComponent(id),{method:'POST'}).then(function(r){return r.json()});
  if(d.success){loadDanmaku();}else{alert(d.message||'删除失败');}
}
async function testDanmaku(){
  const text=el('dmTestText').value.trim();
  if(!text){alert('请输入弹幕文本');return;}
  const d=await fetch('/api/danmaku/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:text})}).then(function(r){return r.json()});
  el('dmTestInfo').textContent=d.blocked?('⛔ 已过滤 ['+(dmCatMap[d.category]||d.category)+'] 命中:「'+d.matched+'」'):'✅ 未命中规则';
}

function buildCleanURL(url, aggr){
  const eng=el('engOpt')?el('engOpt').value:'';
  return '/api/clean?url='+encodeURIComponent(url)+(aggr?'&opt=aggresive':'')+(eng?'&engine='+eng:'');
}
function loadHls(cb,onFail){
  if(window.Hls){return cb();}
  const cdn=['https://cdn.jsdelivr.net/npm/hls.js@1/dist/hls.min.js',
             'https://cdnjs.cloudflare.com/ajax/libs/hls.js/1.5.20/hls.min.js',
             'https://unpkg.com/hls.js@1/dist/hls.min.js',
             'https://fastly.jsdelivr.net/npm/hls.js@1/dist/hls.min.js'];
  let i=0;
  (function load(){
    if(i>=cdn.length){if(onFail)onFail();else alert('hls.js 加载失败，请检查网络后重试');return;}
    const s=document.createElement('script');
    s.src=cdn[i++];s.onload=cb;s.onerror=load;
    document.head.appendChild(s);
  })();
}
// 兼容多浏览器：mp4 直链原生播放；iOS/Safari 原生 HLS；其余用 hls.js（多 CDN 兜底）
function isDirectVideo(src){
  return /\.(mp4|mkv|webm|flv)(\?|$)/i.test(src);
}
let hlsInst=null;
function playURL(src){
  const player=el('player');
  el('playSrc').textContent='播放源: '+src;
  el('playPanel').style.display='block';
  bindSkipAuto(player); // 非正片区间自动跳过（内嵌/占位广告）
  if(hlsInst){try{hlsInst.destroy();}catch(e){}hlsInst=null;}
  // mp4 等直链：交给原生播放器，任何浏览器都支持
  if(isDirectVideo(src)){
    player.src=src;player.play().catch(function(){});
    return;
  }
  player.pause();player.removeAttribute('src');try{player.load();}catch(e){}
  // 原生支持 HLS（iOS Safari / 部分系统浏览器）优先，走代理同源
  if(player.canPlayType('application/vnd.apple.mpegurl')){
    player.src=proxyPlayURL(src);player.play().catch(function(){});
    return;
  }
  // 其余用 hls.js（多 CDN 兜底），走本服务播放代理 + 抗卡顿配置
  loadHls(function(){
    if(Hls&&Hls.isSupported()){
      hlsInst=new Hls(hlsConfig());
      hlsInst.loadSource(proxyPlayURL(src));hlsInst.attachMedia(player);
      player.play().catch(function(){});
    }else if(player.canPlayType('application/vnd.apple.mpegurl')){
      player.src=proxyPlayURL(src);
    }else{alert('当前浏览器不支持 HLS 播放（已尝试自动加载播放组件）');return;}
  });
}
async function runReplace(){
  const url=el('repInput').value.trim();
  el('repOut').style.display='block';el('repOut').textContent='请求中…';
  el('repStats').style.display='none';
  if(!url){alert('请先粘贴官方视频页');return;}
  try{
    const r=await fetch('/api/replace?url='+encodeURIComponent(url));
    const j=await r.json();
    let s=j.message||'';
    if(j.success){
      s+=' | '+ (j.site||'?') +' · 第'+(j.episode_num||'?')+'集 · score='+Math.round(j.match_score||0);
      s+=' <a href="'+j.m3u8_url+'" target="_blank">源 m3u8 ↗</a>';
    }
    if(j.auto_learn_map){
      s+=' <span style="color:#16a34a;font-weight:700">🧠 自动生成映射「'+esc(j.auto_learn_map)+'」（累计 '+j.auto_learn_count+' 条）</span>';
    }
    s+=' <a href="#" onclick="loadMaps();return false;" style="font-size:12px">🗺️ 查看映射表</a>';
    if(j.ad_skip_url){
      const safe=j.ad_skip_url.replace(/'/g,"\\'");
      s+=' <a href="'+j.ad_skip_url+'" target="_blank">无广告直链 ↗</a>';
      s+=' <button class="btn" style="padding:4px 10px;font-size:12px" onclick="playURL(\''+safe+'\')">▶ 内置播放</button>';
      if(j.external_url){
        s+=' <button class="btn" style="padding:4px 10px;font-size:12px" onclick="window.open(\''+j.external_url.replace(/'/g,"\\'")+'\',\'_blank\')">↗ 外置播放</button>';
      }
      if(j.skip_ranges&&j.skip_ranges.length){
        setSkipRanges(j.skip_ranges); // 官替视频专属+全局区间，内置播放自动跳过内嵌/占位广告
        s+=' <span style="color:#d97706;font-weight:600">⏭️ 已加载 '+j.skip_ranges.length+' 个跳过区间</span>';
      }
    }
    el('repStats').style.display='block';el('repStats').innerHTML=s;
    el('repOut').textContent=JSON.stringify(j,null,2);
    refreshStats();
  }catch(e){el('repOut').textContent='官替失败: '+e.message}
}
let sitesData=null;
const esc=function(s){return String(s==null?'':s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;')};
function showProg(on){el('siteProg').style.display=on?'block':'none';}
async function loadSites(){
  showProg(true);
  try{
    // 始终 show=all：失效站在前端单独折叠展示（「显示失效」可折叠）
    const r=await fetch('/api/sites?show=all');
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    sitesData=(j&&j.sites)?j.sites:[];
    sitesStats=j.stats||null;
    const m3=el('siteM3U8'); if(m3)m3.checked=!!(j&&j.only_m3u8);
    renderSites();
  }catch(e){el('siteList').innerHTML='<span class="muted">加载失败: '+esc(e.message)+'</span>';}
  showProg(false);
}
async function toggleSiteM3U8(v){
  try{
    const r=await fetch('/api/sites/m3u8',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({only_m3u8:!!v})});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    if(!j.success)alert(j.message||'保存失败');
  }catch(e){alert('网络错误: '+e.message);}
}
function isFailedSite(x){return !x||x.status!=='active'}
function renderSites(){
  if(!sitesData)return;
  const q=((el('siteSearch').value)||'').trim().toLowerCase();
  const s2=sitesData.filter(function(x){
    if(!q)return true;
    return (x.name+((x.note)||'')+((x.api_url)||'')).toLowerCase().indexOf(q)>=0;
  });
  // 主列表排除失效站；失效站归入下方「显示失效」可折叠区
  const okSites=s2.filter(function(x){return !isFailedSite(x)});
  const failSites=s2.filter(isFailedSite);
  const en=okSites.filter(function(x){return x.enabled}).length;
  const st=sitesStats||{};
  el('siteCount').innerHTML='已启用 '+en+' / 可用 '+okSites.length+' 个站点'+(q?'（筛选：'+esc(q)+'）':'')+
    (st.total?'　<span class="muted">共 '+st.total+' · 失效 '+failSites.length+'</span>':'');
  const groups=[[esc('🟢 已启用'),okSites.filter(x=>x.enabled)],[esc('⚪ 未启用'),okSites.filter(x=>!x.enabled)]];
  let html='';
  groups.forEach(function(g){
    const label=g[0],arr=g[1];
    if(arr.length===0)return;
    html+='<details class="site" open><summary>'+label+'（'+arr.length+'）</summary>';
    html+=arr.map(function(x){
      return '<div class="siterow">'+
        '<label class="sw"><input type="checkbox" '+(x.enabled?'checked':'')+' onchange="toggleSite(\''+x.name.replace(/'/g,"\\'")+'\',this.checked)"></label>'+
        '<b>'+esc(x.name)+'</b>'+
        '<span class="muted note">'+esc(x.note)+'</span>'+
        '<button class="btn gh" onclick="siteDetail(\''+x.name.replace(/'/g,"\\'")+'\')">🔍 测试采集</button>'+
        '<button class="btn gh" onclick="editSite(\''+x.name.replace(/'/g,"\\'")+'\')">✏️ 编辑</button>'+
        '<button class="btn gh" style="color:#dc2626" onclick="deleteSite(\''+x.name.replace(/'/g,"\\'")+'\')">🗑 删除</button>'+
        '</div><div class="sitedtl" id="dtl_'+esc(x.name)+'"></div>'+
        '<div class="sitedtl" id="edt_'+esc(x.name)+'" style="display:none"></div>';
    }).join('');
    html+='</details>';
  });
  // 失效站折叠区（默认折叠，点击展开查看被屏蔽/失效站）
  if(failSites.length){
    html+='<details class="site" id="failFold"><summary>❌ 已失效 / 暂停（'+failSites.length+'）－ 点击展开查看</summary>';
    html+=failSites.map(function(x){
      return '<div class="siterow">'+
        '<label class="sw"><input type="checkbox" '+(x.enabled?'checked':'')+' onchange="toggleSite(\''+x.name.replace(/'/g,"\\'")+'\',this.checked)"></label>'+
        '<b style="color:#dc2626">'+esc(x.name)+'</b>'+
        '<span class="muted note">'+esc(x.note||'')+'</span>'+
        '<button class="btn gh" onclick="editSite(\''+x.name.replace(/'/g,"\\'")+'\')">✏️ 编辑</button>'+
        '<button class="btn gh" style="color:#dc2626" onclick="deleteSite(\''+x.name.replace(/'/g,"\\'")+'\')">🗑 删除</button>'+
        '</div><div class="sitedtl" id="edt_'+esc(x.name)+'" style="display:none"></div>';
    }).join('');
    html+='</details>';
  }
  el('siteList').innerHTML=html||'<span class="muted">无匹配站点</span>';
}
function toggleImportText(btn){
  const w=document.getElementById('impTextWrap');
  if(!w)return;
  w.style.display=w.style.display==='none'?'block':'none';
}
async function importSites(fromText){
  const url=(el('impUrl').value||'').trim();
  const text=fromText?((el('impText').value||'').trim()):'';
  if(!url && !text){alert('请填写链接或粘贴文本');return;}
  const msg=el('impMsg');showProg(true);msg.innerHTML='导入中…';
  try{
    const r=await fetch('/api/sites/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:url,text:text})});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    if(j.success){msg.innerHTML='<span style="color:#16a34a">✅ '+esc(j.message)+'</span><div class="muted" style="font-size:12px">如需启用在「已启用/未启用」里勾选开启；失效站与需停用站可点 🗑 删除。</div>';await loadSites();}
    else{msg.innerHTML='<span style="color:#dc2626">❌ '+esc(j.message)+'</span>';}
  }catch(e){msg.innerHTML='<span style="color:#dc2626">❌ 网络错误: '+esc(e.message)+'</span>';}
  showProg(false);
}
async function addSite(){
  const name=(el('nsName').value||'').trim();
  const api=(el('nsApi').value||'').trim();
  if(!name||!api){alert('名称和采集接口不能为空');return;}
  showProg(true);
  try{
    const r=await fetch('/api/sites/add',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({name:name,api_url:api,site_url:(el('nsSite').value||'').trim(),note:(el('nsNote').value||'').trim()})});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'添加成功':'添加失败'));
    if(j.success){el('nsName').value='';el('nsApi').value='';el('nsSite').value='';el('nsNote').value='';await loadSites();}
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
async function editSite(name){
  const box=document.getElementById('edt_'+esc(name));
  if(!box)return;
  if(box.style.display==='block'){box.style.display='none';return;}
  const s=sitesData.filter(function(x){return x.name===name})[0]||{};
  box.style.display='block';
  box.innerHTML='<div class="row" style="flex-wrap:wrap;align-items:center">'+
    '<span class="muted">名称</span><input id="edName" value="'+esc(s.name||'')+'" style="width:140px;padding:7px 9px;border-radius:8px;border:1px solid #d1d5db">'+
    '<span class="muted">接口</span><input id="edApi" value="'+esc(s.api_url||'')+'" style="flex:1;min-width:260px;padding:7px 9px;border-radius:8px;border:1px solid #d1d5db">'+
    '<span class="muted">官网</span><input id="edSite" value="'+esc(s.site_url||'')+'" style="width:190px;padding:7px 9px;border-radius:8px;border:1px solid #d1d5db">'+
    '<span class="muted">备注</span><input id="edNote" value="'+esc(s.note||'')+'" style="flex:1;min-width:160px;padding:7px 9px;border-radius:8px;border:1px solid #d1d5db">'+
    '<span class="muted">优先级</span><input id="edPri" type="number" value="'+(s.priority||100)+'" style="width:70px;padding:7px 9px;border-radius:8px;border:1px solid #d1d5db">'+
    '<button class="btn" style="padding:7px 14px;font-size:12px" onclick="saveSite(\''+name.replace(/'/g,"\\'")+'\')">💾 保存</button>'+
    '</div>';
}
async function saveSite(name){
  const payload={name:name,
    new_name:(el('edName').value||'').trim(),
    api_url:(el('edApi').value||'').trim(),
    site_url:(el('edSite').value||'').trim(),
    note:(el('edNote').value||'').trim(),
    priority:parseInt(el('edPri').value||'0',10)||0};
  if(!payload.new_name){alert('名称不能为空');return;}
  if(!payload.api_url){alert('采集接口不能为空');return;}
  showProg(true);
  try{
    const r=await fetch('/api/sites/update',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'更新成功':'更新失败'));
    if(j.success)await loadSites();
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
async function deleteSite(name){
  if(!confirm('确定删除资源站「'+name+'」吗？'))return;
  showProg(true);
  try{
    const r=await fetch('/api/sites/delete?name='+encodeURIComponent(name),{method:'POST'});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'删除成功':'删除失败'));
    if(j.success)await loadSites();
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
async function checkSites(){
  const kw=el('siteKw')?el('siteKw').value:'爱情';
  if(!confirm('将对可用资源站并发检测（多探针词「爱情/庆余年/电视剧」任一命中即可用，搜索无命中时自动探测接口连通性，仅真失效才屏蔽）。确定执行？'))return;
  showProg(true);
  try{
    const r=await fetch('/api/sites/check?kw='+encodeURIComponent(kw),{method:'POST'});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    if(!j.success){el('siteList').innerHTML='<span class="muted">启动失败: '+esc(j.message)+'</span>';showProg(false);return;}
    const task=j.task;
    // 真实百分比进度条 + 计数
    const prog=el('siteProg');
    prog.style.display='block';
    prog.innerHTML='<div class="prog-track"><div class="prog-fill" id="chkFill"></div></div>'+
      '<div class="muted" id="chkInfo" style="font-size:12px">准备中…</div>';
    el('siteList').innerHTML='<span class="muted">正在并发检测…</span>';
    (function poll(){
      fetch('/api/sites/check/progress?task='+encodeURIComponent(task)).then(function(r){return r.json()}).then(function(d){
        if(!d.success){el('chkInfo').textContent=d.message;showProg(false);return;}
        const pct=d.total>0?Math.round(d.done/d.total*100):0;
        const fill=document.getElementById('chkFill');
        if(fill)fill.style.width=pct+'%';
        el('chkInfo').textContent='检测 '+d.done+'/'+d.total+'（'+pct+'%）· ✅ 可用 '+d.usable+' · ⛔ 失效 '+d.blocked+(d.finished?'　— 完成':'…');
        const rs=d.results||[];
        const tail=rs.slice(-60).reverse();
        el('siteList').innerHTML='<div class="muted" style="margin-bottom:6px">已完成 '+d.done+' / 共 '+d.total+' 个站点：</div>'+
          tail.map(function(x){
            const st=(x.usable?(x.search_limited?'#d97706':'#16a34a'):'#dc2626');
            const lab=x.usable?(x.search_limited?'⚠️ 可用(搜索受限)':'✓ 可用'):'✗ 失效';
            return '<div class="siterow" title="搜索受限：此站关键词搜索需验证/未开放，靠列表接口连通判定可用，官替搜索可能搜不到，但不影响其他站"><b>'+esc(x.name)+'</b>'+
              '<span style="color:'+st+';font-weight:700">'+lab+'</span>'+
              '<span class="muted" style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">'+esc(x.message)+' · '+x.response_ms+'ms</span></div>';
          }).join('')||'<span class="muted">等待结果…</span>';
        if(d.finished){showProg(false);setTimeout(function(){loadSites();},600);return;}
        setTimeout(poll,600);
      }).catch(function(e){el('chkInfo').textContent='进度获取失败: '+esc(e.message);showProg(false);});
    })();
  }catch(e){el('siteList').innerHTML='<span class="muted">检测失败: '+esc(e.message)+'</span>';showProg(false);}
}
// —— 官替映射专区 ——
async function loadMaps(){
  try{
    const r=await fetch('/api/maps');
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    const ms=(j&&j.name_maps)||[];
    el('mapList').innerHTML=ms.length?ms.map(function(m){
      return '<tr><td>'+(m.platform?esc(m.platform):'—')+'</td>'+
        '<td><code>'+esc(m.from)+'</code></td>'+
        '<td>→ <code>'+esc(m.to)+'</code></td>'+
        '<td class="muted">'+esc(m.note||'')+'</td>'+
        '<td><button class="btn gh" style="color:#dc2626;padding:3px 10px;font-size:12px" onclick="delMap(\''+m.from.replace(/'/g,"\\'")+'\',\''+m.to.replace(/'/g,"\\'")+'\')">🗑 删除</button></td></tr>';
    }).join(''):'<tr><td colspan="5" class="muted">暂无映射，可从上方「从链接抓取」添加</td></tr>';
  }catch(e){el('mapList').innerHTML='<tr><td colspan="5" class="muted">加载失败: '+esc(e.message)+'</td></tr>';}
}
async function fetchMap(){
  const url=(el('mapUrl').value||'').trim();
  if(!url){alert('请先粘贴真实官方视频页链接');return;}
  const sel=(el('mapSel').value||'').trim();
  el('mapFetchInfo').textContent='抓取中…';
  try{
    let q='/api/maps/fetch?url='+encodeURIComponent(url);
    if(sel)q+='&selector='+encodeURIComponent(sel);
    const r=await fetch(q);
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    if(!j.success){el('mapFetchInfo').textContent='抓取失败: '+(j.message||'');return;}
    // 自动映射：剧名字段（from=官方剧名）+ 平台，直接填入表单
    window._fetchedBase=j.base_title||'';
    window._fetchedPlat=j.platform||'';
    fillMap();
    const epRaw=j.episode_raw||'';
    let epTxt=j.episode_num>0?('剧集字段「<b>'+esc(epRaw||('第'+j.episode_num+'集'))+'</b>('+j.episode_num+')'):'单集/未识别集数';
    el('mapFetchInfo').innerHTML='自动映射成功：平台 <b>'+esc(j.platform)+'</b> ｜ 剧名字段「<b>'+esc(j.base_title)+'</b>」 ｜ '+epTxt+
      '　<button class="btn" style="padding:3px 10px;font-size:12px" onclick="el(\'mTo\').focus()">✏️ 填资源站剧名(to)</button>'+
      '　<button class="btn" style="padding:3px 10px;font-size:12px" onclick="addMap()">➕ 直接添加映射</button>';
  }catch(e){el('mapFetchInfo').textContent='抓取失败: '+esc(e.message);}
}
function fillMap(){
  el('mFrom').value=window._fetchedBase||'';
  el('mPlat').value=window._fetchedPlat||'';
  el('mTo').focus();
}
async function addMap(){
  const from=(el('mFrom').value||'').trim();
  let to=(el('mTo').value||'').trim();
  if(!from){alert('官方剧名(from)不能为空');return;}
  if(!to){to=from;} // to 留空时默认同名，支持「直接添加映射」一键添加
  showProg(true);
  try{
    const r=await fetch('/api/maps/add',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({from:from,to:to,platform:(el('mPlat').value||'').trim(),note:'后台添加'})});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'映射添加成功':'添加失败'));
    if(j.success){el('mFrom').value='';el('mTo').value='';el('mPlat').value='';await loadMaps();}
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
async function delMap(from,to){
  if(!confirm('确定删除映射「'+from+' → '+to+'」吗？'))return;
  showProg(true);
  try{
    const r=await fetch('/api/maps/delete?from='+encodeURIComponent(from)+'&to='+encodeURIComponent(to),{method:'POST'});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'删除成功':'删除失败'));
    if(j.success)await loadMaps();
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
// —— 自动更新官方：官方平台配置（顺序/平台/域名/URL正则/标题选择器/优先级）——
let pfEditKey=null; // 编辑状态：{platform,domain}

// 一键映射：加载内置官方链接按钮（用户无需输入链接）
async function loadOneClickLinks(){
  try{
    const r=await fetch('/api/platforms/links');
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    const lks=(j&&j.links)||[];
    el('ocLinks').innerHTML=lks.length?lks.map(function(l){
      return '<button class="btn" data-lab="⚡ '+esc(l.label)+'" style="padding:6px 12px;font-size:12px;margin:3px" title="'+esc(l.url)+'" onclick="oneClickMap(\''+l.key+'\',this)">⚡ '+esc(l.label)+'</button>';
    }).join(''):'<span class="muted">无内置链接</span>';
  }catch(e){el('ocLinks').textContent='加载失败: '+esc(e.message);}
}
async function oneClickMap(key,btn){
  const lab=btn?btn.getAttribute('data-lab')||'重试':null;
  if(btn){btn.disabled=true;btn.textContent='映射中…';}
  el('ocInfo').textContent='正在从官方链接实时抓取剧名并自动映射…';
  try{
    const r=await fetch('/api/platforms/oneclick?key='+encodeURIComponent(key),{method:'POST'});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    el('ocInfo').innerHTML=j.success
      ?'<span style="color:#16a34a">✅ '+esc(j.message)+'</span>'
      :'<span style="color:#dc2626">❌ '+esc(j.message||'失败')+'</span>';
    if(j.success){await loadMaps();loadPlatforms();}
  }catch(e){el('ocInfo').textContent='失败: '+esc(e.message);}
  if(btn){btn.disabled=false;btn.textContent=lab||'重试';}
}

async function loadPlatforms(){
  try{
    const r=await fetch('/api/platforms');
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    const ps=((j&&j.platforms)||[]).slice().sort(function(a,b){
      return (a.priority-b.priority)||(a.platform>b.platform?1:-1);
    });
    window._pfData=ps;
    el('pfList').innerHTML=ps.length?ps.map(function(p,i){
      return '<tr>'+
        '<td class="muted">'+(i+1)+'</td>'+
        '<td><b>'+esc(p.platform)+'</b></td>'+
        '<td><code>'+esc(p.domain)+'</code></td>'+
        '<td class="muted" style="max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(p.url_re||'')+'">'+esc(p.url_re||'—')+'</td>'+
        '<td class="muted" style="max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(p.title_selector||'')+'">'+esc(p.title_selector||'—')+'</td>'+
        '<td>'+p.priority+'</td>'+
        '<td><label style="cursor:pointer"><input type="checkbox" '+(p.enabled?'checked':'')+' onclick="pfToggle(\''+esc(p.platform).replace(/'/g,"\\'")+'\',\''+esc(p.domain).replace(/'/g,"\\'")+'\',this.checked)"> '+(p.enabled?'启用':'停用')+'</label></td>'+
        '<td class="muted">'+esc(p.note||'')+'</td>'+
        '<td><button class="btn gh" style="padding:3px 10px;font-size:12px" onclick="pfEdit(\''+esc(p.platform).replace(/'/g,"\\'")+'\',\''+esc(p.domain).replace(/'/g,"\\'")+'\')">✏️ 编辑</button> '+
        '<button class="btn gh" style="color:#dc2626;padding:3px 10px;font-size:12px" onclick="pfDelete(\''+esc(p.platform).replace(/'/g,"\\'")+'\',\''+esc(p.domain).replace(/'/g,"\\'")+'\')">🗑 删除</button></td></tr>';
    }).join(''):'<tr><td colspan="9" class="muted">暂无配置，可在上方填写后「保存配置」添加</td></tr>';
  }catch(e){el('pfList').innerHTML='<tr><td colspan="9" class="muted">加载失败: '+esc(e.message)+'</td></tr>';}
}
function pfResetForm(){
  pfEditKey=null;
  ['pfPlatform','pfDomain','pfUrlRe','pfTitleSel'].forEach(function(id){el(id).value='';});
  el('pfPriority').value='10';
  el('pfInfo').textContent='';
}
function pfEdit(platform,domain){
  const ps=window._pfData||[];
  const p=ps.filter(function(x){return x.platform===platform&&x.domain===domain;})[0];
  if(!p){return;}
  pfEditKey={platform:platform,domain:domain};
  el('pfPlatform').value=p.platform;
  el('pfDomain').value=p.domain;
  el('pfUrlRe').value=p.url_re||'';
  el('pfTitleSel').value=p.title_selector||'';
  el('pfPriority').value=p.priority||10;
  el('pfInfo').innerHTML='正在编辑：<b>'+esc(platform)+' @ '+esc(domain)+'</b>（保存将更新该条配置）';
  el('pfPlatform').focus();
}
async function pfSave(){
  const platform=(el('pfPlatform').value||'').trim();
  const domain=(el('pfDomain').value||'').trim();
  const url_re=(el('pfUrlRe').value||'').trim();
  const title_selector=(el('pfTitleSel').value||'').trim();
  const priority=parseInt(el('pfPriority').value||'10',10)||10;
  if(!platform||!domain){alert('平台名称和域名不能为空');return;}
  showProg(true);
  try{
    const body=pfEditKey?{old_platform:pfEditKey.platform,old_domain:pfEditKey.domain,platform:platform,domain:domain,url_re:url_re,title_selector:title_selector,priority:priority,enabled:true,note:'后台配置'}:{platform:platform,domain:domain,url_re:url_re,title_selector:title_selector,priority:priority,enabled:true,note:'后台配置'};
    const r=await fetch(pfEditKey?'/api/platforms/update':'/api/platforms/add',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?(pfEditKey?'更新成功':'添加成功'):'保存失败'));
    if(j.success){pfResetForm();await loadPlatforms();}
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
async function pfToggle(platform,domain,enabled){
  const p=(window._pfData||[]).filter(function(x){return x.platform===platform&&x.domain===domain;})[0];
  const body={old_platform:platform,old_domain:domain,platform:platform,domain:domain,
    url_re:(p&&p.url_re)||'',title_selector:(p&&p.title_selector)||'',priority:(p&&p.priority)||10,enabled:enabled,note:(p&&p.note)||''};
  try{
    const r=await fetch('/api/platforms/update',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
    if(r.status===401){location.href='/mxadmin/login';return;}
    await loadPlatforms();
  }catch(e){await loadPlatforms();}
}
async function pfDelete(platform,domain){
  if(!confirm('确定删除官方平台配置「'+platform+' @ '+domain+'」吗？'))return;
  showProg(true);
  try{
    const r=await fetch('/api/platforms/delete?platform='+encodeURIComponent(platform)+'&domain='+encodeURIComponent(domain),{method:'POST'});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'删除成功':'删除失败'));
    if(j.success)await loadPlatforms();
  }catch(e){alert('网络错误: '+e.message);}
  showProg(false);
}
async function pfFetch(){
  const url=(el('pfUrl').value||'').trim();
  if(!url){alert('请先粘贴真实官方链接');return;}
  el('pfInfo').textContent='匹配平台并提取剧名/集数中…';
  try{
    const r=await fetch('/api/platforms/fetch?url='+encodeURIComponent(url));
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    if(!j.success){el('pfInfo').innerHTML='<span style="color:#dc2626">获取失败: '+esc(j.message||'')+'</span>';return;}
    // 自动映射到对应区域：把「影视剧名/剧集集数」填入官替映射表单（映射表区域）
    window._fetchedBase=j.base_title||'';
    window._fetchedPlat=j.platform||'';
    el('mFrom').value=j.base_title||'';
    el('mPlat').value=j.platform||'';
    el('mTo').value='';
    const epRaw=j.episode_raw||'';
    const epTxt=j.episode_num>0?('剧集字段「<b>'+esc(epRaw||('第'+j.episode_num+'集'))+'</b>('+j.episode_num+')'):'单集/未识别集数';
    el('pfInfo').innerHTML='自动获取成功：平台 <b>'+esc(j.platform)+'</b> @ <code>'+esc(j.domain)+'</code> ｜ 影视剧名「<b>'+esc(j.base_title)+'</b>」 ｜ '+epTxt+
      '　（已自动填入上方「官替映射专区」表单）'+
      '　<button class="btn" style="padding:3px 10px;font-size:12px" onclick="addMap()">➕ 直接添加映射</button>'+
      '　<button class="btn" style="padding:3px 10px;font-size:12px" onclick="el(\'mTo\').focus()">✏️ 填资源站剧名(to)</button>';
  }catch(e){el('pfInfo').textContent='获取失败: '+esc(e.message);}
}

async function toggleSite(name,onValue){
  showProg(true);
  try{
    const r=await fetch('/api/sites/toggle?name='+encodeURIComponent(name)+'&enabled='+(onValue?1:0));
    if(r.status===401){location.href='/mxadmin/login';return;}
    await loadSites();
  }catch(e){}
  showProg(false);
}
async function siteDetail(name){
  const box=document.getElementById('dtl_'+esc(name));
  if(!box)return;
  if(box.innerHTML!==''){box.style.display=box.style.display==='block'?'none':'block';return;}
  box.style.display='block';box.innerHTML='<span class="muted">查询中…</span>';
  const kw=el('siteKw')?el('siteKw').value:'庆余年';
  try{
    const r=await fetch('/api/sites/test?name='+encodeURIComponent(name)+'&kw='+encodeURIComponent(kw));
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    const s=sitesData.filter(function(x){return x.name===name})[0]||{};
    let v='<div><b>站点：</b><code>'+esc(name)+'</code></div>'+
          '<div><b>官网：</b><code>'+esc(s.site_url||'')+'</code></div>'+
          '<div><b>接口：</b><code>'+esc(s.api_url||'')+'</code></div>'+
          '<div class="muted">状态：'+(s.enabled?'已启用':'已禁用')+(s.note?(' · '+esc(s.note)):'')+'</div>';
    if(j.count>0){
      v+='<div class="muted" style="margin-top:8px;margin-bottom:4px">搜索「'+esc(kw)+'」命中 '+j.count+' 条：</div>'+
         '<div style="display:flex;flex-direction:column;gap:6px">'+
         (j.videos||[]).map(function(x){
           const pic=x.pic?'<img src="'+esc(x.pic)+'" alt="" style="width:52px;height:72px;object-fit:cover;border-radius:6px;flex:0 0 auto" referrerpolicy="no-referrer" onerror="this.style.display=\'none\'">':'';
           return '<div style="display:flex;gap:8px;align-items:flex-start;border:1px solid #e5e7eb;border-radius:8px;padding:6px 8px;background:#fff">'+
             pic+
             '<div style="flex:1;min-width:0">'+
               '<div><b>'+esc(x.name)+'</b> <span class="muted" style="color:#16a34a">'+esc(x.remarks||'')+'</span>'+
               '<span class="muted" style="margin-left:6px;font-size:12px">来源：'+esc(x.play_from||'')+'</span></div>'+
               '<div class="muted" style="font-size:12px;word-break:break-all;margin-top:2px"><code>'+esc(x.first_url)+'</code></div>'+
               '<div style="margin-top:4px">'+
                 '<button class="btn mini" onclick="copyText(this,decodeURIComponent(\''+encodeURIComponent(x.first_url)+'\'),\'📋 复制播放链接\')">📋 复制播放链接</button> '+
                 '<button class="btn mini" onclick="playTest(\''+encodeURIComponent(x.first_url)+'\',\''+encodeURIComponent(x.name)+'\')">▶ 测试播放</button>'+
               '</div></div></div>';
         }).join('')+
         '</div>';
    }else{
      v+='<div class="muted" style="margin-top:6px">未命中：该站点无结果或已失效，可换测试词（资源站面板右上输入框）再点测试采集</div>';
    }
    box.innerHTML=v;
  }catch(e){box.innerHTML='<span class="muted">查询失败: '+esc(e.message)+'</span>';}
}
// playTest 用采集站返回的播放地址打开独立外置播放页（新窗口）
function playTest(urlEnc,titleEnc){
  const u=decodeURIComponent(urlEnc),t=decodeURIComponent(titleEnc);
  window.open('/player?url='+encodeURIComponent(u)+'&title='+encodeURIComponent(t),'_blank');
}
function copyText(btn,text,label){
  label=label||'复制播放链接';
  if(navigator.clipboard&&navigator.clipboard.writeText){
    navigator.clipboard.writeText(text).then(function(){
      btn.textContent='已复制 ✓';setTimeout(function(){btn.textContent=label;},1500);
    }).catch(function(){fallbackCopy(text,btn,label);});
  }else{fallbackCopy(text,btn,label);}
}
function fallbackCopy(text,btn,label){
  label=label||'复制播放链接';
  const t=document.createElement('textarea');t.value=text;document.body.appendChild(t);t.select();
  try{document.execCommand('copy');btn.textContent='已复制 ✓';setTimeout(function(){btn.textContent=label;},1500);}catch(e){}
  document.body.removeChild(t);
}
function setAllSites(openState){
  document.querySelectorAll('#siteList details.site').forEach(function(d){d.open=openState;});
}
async function openPwd(){
  const old=prompt('请输入原密码：');
  if(old==null)return;
  const nw=prompt('请输入新密码（至少 4 位）：');
  if(nw==null)return;
  const nw2=prompt('请再次输入新密码：');
  if(nw!==nw2){alert('两次新密码不一致');return;}
  try{
    const r=await fetch('/api/auth/password',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({old:old,new:nw})});
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    alert(j.message||(j.success?'密码已修改':'修改失败'));
  }catch(e){alert('网络错误: '+e.message);}
}
async function doLogout(){
  await fetch('/api/auth/logout',{method:'POST'});
  location.href='/mxadmin/login';
}
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
      hlsInst=new Hls(hlsConfig());
      hlsInst.loadSource(proxyPlayURL(src));hlsInst.attachMedia(player);
    }else if(player.canPlayType('application/vnd.apple.mpegurl')){
      player.src=proxyPlayURL(src);
    }else{alert('当前浏览器不支持 HLS 播放');return;}
    player.play().catch(function(){});
  });
  refreshStats();
}
</script>
</body>
</html>
`

// renderUpdateBlock 服务端渲染「当前/最新版本」区块，避免依赖客户端 fetch（离线/慢时仍可显示版本）
func renderUpdateBlock() string {
	info := updateInfo()
	cur, latest := info["current"].(string), info["latest"].(string)
	s := `当前版本 ` + cur + ` &nbsp;→&nbsp; 最新版本 ` + latest
	if has, _ := info["has_update"].(bool); has {
		s += `　<span style="color:#dc2626;font-weight:700">⚠️ 存在新版本</span>`
	} else {
		s += `　<span style="color:#16a34a">已是最新</span>`
	}
	s += `　<span style="color:#9ca3af" class="upd-note"></span>`
	return s
}

// handleAdmin 渲染后台页面（需登录，未登录跳转登录页）
func handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !isAuthed(r) {
		http.Redirect(w, r, "/mxadmin/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.Replace(adminPageHTML, `<!--UPD_BLOCK-->`, renderUpdateBlock(), 1)
	io.WriteString(w, page)
}

// loginPageHTML 后台登录页
const loginPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MXGT-Go 后台登录</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{min-height:100vh;font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;
       background:linear-gradient(135deg,#581c87,#7e22ce,#a21caf);display:flex;align-items:center;justify-content:center;padding:24px}
  .card{width:100%;max-width:360px;background:rgba(255,255,255,.92);backdrop-filter:blur(16px);
        border-radius:18px;padding:30px 28px;box-shadow:0 16px 40px rgba(0,0,0,.28);text-align:center}
  h1{font-size:20px;color:#581c87;margin-bottom:4px}
  .sub{font-size:12.5px;color:#9ca3af;margin-bottom:22px}
  label{display:block;text-align:left;font-size:12.5px;color:#4b5563;margin:12px 0 6px}
  input{width:100%;padding:11px 14px;border-radius:10px;border:1px solid #d1d5db;font-size:14px}
  .btn{width:100%;margin-top:20px;background:linear-gradient(135deg,#7e22ce,#c026d3);color:#fff;border:0;
       border-radius:10px;padding:12px;font-size:15px;cursor:pointer;box-shadow:0 6px 16px rgba(124,58,237,.35)}
  .btn:hover{filter:brightness(1.08)}
  .err{color:#dc2626;font-size:12.5px;margin-top:12px;min-height:18px}
  .foot{margin-top:18px;font-size:11px;color:#9ca3af}
</style>
</head>
<body>
<div class="card">
  <h1>🎬 MXGT-Go 后台</h1>
  <div class="sub">登录后进入后台管理 · 开发者 ssmhdssmhd</div>
  <label>账号</label>
  <input id="user" value="admin" autocomplete="username">
  <label>密码</label>
  <input id="pass" type="password" placeholder="请输入密码" autocomplete="current-password"
         onkeydown="if(event.key==='Enter')doLogin()">
  <button class="btn" onclick="doLogin()">登 录</button>
  <div class="err" id="err"></div>
  <div class="foot">默认账号 admin / admin123（可在后台修改密码）</div>
</div>
<script>
async function doLogin(){
  const user=document.getElementById('user').value.trim();
  const pass=document.getElementById('pass').value;
  const err=document.getElementById('err');
  if(!user||!pass){err.textContent='请输入账号和密码';return;}
  err.textContent='登录中…';
  try{
    const r=await fetch('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({username:user,password:pass})});
    const j=await r.json();
    if(j.success){location.href='/mxadmin';}
    else{err.textContent=j.message||'登录失败';}
  }catch(e){err.textContent='网络错误: '+e.message;}
}
</script>
</body>
</html>
`

// handleLogin 渲染登录页
func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, loginPageHTML)
}

// frontPageHTML 前台：展示接口调用详细信息（开放，无需登录）
const frontPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MXGT-Go 接口调用详情</title>
<style>
  :root{--pri:#7e22ce;--pink:#c026d3;--card:rgba(255,255,255,.86);--line:rgba(255,255,255,.9)}
  *{box-sizing:border-box;margin:0;padding:0}
  body{min-height:100vh;font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;
       background:linear-gradient(135deg,#581c87,#7e22ce,#a21caf,#c026d3);color:#1f2937;padding:24px}
  .wrap{max-width:1000px;margin:0 auto}
  .header{background:rgba(255,255,255,.18);backdrop-filter:blur(14px);border:1px solid var(--line);
          border-radius:18px;padding:20px 24px;color:#fff;display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;gap:10px;flex-wrap:wrap}
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
  .panel h2{font-size:17px;color:#581c87;margin-bottom:14px}
  table{width:100%;border-collapse:collapse;font-size:13.5px}
  th,td{text-align:left;padding:8px 10px;border-bottom:1px solid #eee}
  th{color:#581c87;font-size:12.5px}
  code{background:#f3f4f6;padding:2px 7px;border-radius:6px;color:#7e22ce;font-size:12px}
  .ok{color:#16a34a;font-weight:700}.fail{color:#dc2626;font-weight:700}
  .muted{color:#9ca3af;font-size:12px}
  .url{max-width:420px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:inline-block;vertical-align:bottom}
  a{color:#fff;background:#c026d3;padding:9px 18px;border-radius:10px;text-decoration:none}
  .api-row{display:flex;align-items:flex-start;gap:10px;padding:10px 0;border-bottom:1px solid #eee}
  .api-row:last-child{border-bottom:none}
  .api-meta{min-width:200px;flex-shrink:0}
  .api-meta b{color:#581c87;font-size:13.5px}
  .api-badge{display:inline-block;font-size:10.5px;font-weight:700;color:#fff;border-radius:999px;
             padding:1px 8px;margin-left:6px;vertical-align:1px}
  .api-badge.client{background:#0891b2}
  .api-badge.server{background:#7e22ce}
  .api-badge.admin{background:#d97706}
  .api-badge.both{background:#16a34a}
  .api-meta .d{font-size:11.5px;color:#9ca3af;margin-top:3px;line-height:1.5}
  .api-meta .use{font-size:11px;color:#b45309;margin-top:2px;line-height:1.5}
  .api-cmd{flex:1;background:#1f2937;color:#e5e7eb;border-radius:8px;padding:8px 11px;font-size:12px;
           overflow-x:auto;white-space:nowrap;word-break:break-all;min-width:0}
  .api-cmd .muted{color:#9ca3af}
  .copy-btn{background:#c026d3;color:#fff;border:none;border-radius:8px;padding:6px 13px;cursor:pointer;
            font-size:12px;flex-shrink:0;font-weight:600}
  .copy-btn:hover{opacity:.85}
  .copy-btn.copied{background:#16a34a}
</style>
</head>
<body>
<div class="wrap">
  <div class="header">
    <div>
      <h1>🎬 MXGT-Go 接口调用详情</h1>
      <div class="ver">M3U8 去广告 + 官替链路服务 <span id="ver"></span> · 开发者 ssmhdssmhd</div>
    </div>
    <a href="/mxadmin">进入后台管理 →</a>
  </div>

  <div class="grid" id="statGrid"></div>

  <div class="panel">
    <h2>🔌 API 调用方式 <span class="muted">（地址基于当前访问域名自动生成，点击复制）</span></h2>
    <div id="apiList"></div>
  </div>

  <div class="panel">
    <h2>📊 接口调用次数明细</h2>
    <table><thead><tr><th>接口</th><th>调用次数</th></tr></thead><tbody id="calls"></tbody></table>
  </div>

  <div class="panel">
    <h2>🕒 最近接口调用记录 <span class="muted" id="recentHint"></span></h2>
    <table>
      <thead><tr><th>时间</th><th>接口</th><th>请求地址/参数</th><th>耗时</th><th>结果</th><th>说明</th></tr></thead>
      <tbody id="recent"></tbody>
    </table>
  </div>
</div>

<script>
function el(id){return document.getElementById(id)}
function esc(s){return String(s==null?'':s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;')}
function card(lab,val,sub){return '<div class="card"><div class="lab">'+lab+'</div><div class="val">'+val+'</div>'+(sub?'<div class="sub">'+sub+'</div>':'')+'</div>';}
// —— API 调用方式（地址基于当前访问域名动态生成，支持一键复制）——
// t 标注接口用途：client=客户端调用（播放器/影视App/TVBox）、server=服务端调用（服务器间/二次处理）、
// admin=后台管理（需登录）、both=客户端与服务端通用
var APIS=[
  {n:'去广告 M3U8',t:'both',d:'传入 m3u8 链接，返回过滤后无广告 M3U8 纯文本，可直接播放',u:'客户端：直接喂播放器；服务端：抓取过滤后的无广告地址',p:'/api/clean?url=<m3u8链接>'},
  {n:'去广告 JSON',t:'server',d:'同上去广告，返回 JSON（统计 + 过滤后文本 + 广告明细）',u:'服务端：二次分析广告片段、统计与审计',p:'/api/clean/json?url=<m3u8链接>'},
  {n:'增强去广告',t:'server',d:'独立引擎：平台广告域关键词 + 片头片尾超短簇高置信检测',u:'服务端：更高召回的去广告解析（/json 返回结构化结果）',p:'/api/clean/enhanced?url=<m3u8链接>'},
  {n:'播放代理',t:'client',d:'服务端代理 m3u8/分片/密钥，内置增强去广告，解决跨域与限速卡顿',u:'客户端：播放器直接播放本服务拼接的代理地址',p:'/api/play?url=<m3u8链接>'},
  {n:'广告核查',t:'server',d:'列出所有不连贯片段（拼接点/时长突变/偏短），供人工逐段核对广告',u:'服务端/人工：审核可疑片段，辅助广告标记',p:'/api/audit?url=<m3u8链接>'},
  {n:'官替链路',t:'server',d:'官方视频页链接 → 资源站匹配 → 返回无广告直链',u:'服务端：从官方视频页解析出无广告直链供下发',p:'/api/replace?url=<官方视频页链接>'},
  {n:'影视/TVBox 兼容',t:'client',d:'影视 App / TVBox 等通用解析接口（JSON，带跨域）',u:'客户端：影视App/TVBox/盒子配置解析接口',p:'/api/jx?url=<播放链接>'},
  {n:'客户端调用',t:'client',d:'播放器/盒子调用：精简播放字段，msg=url 可播放地址',u:'客户端：专用精简接口，体积小响应快',p:'/api/jx/client?url=<播放链接>'},
  {n:'服务器调用',t:'server',d:'服务端二次处理：附带 detail 完整明细（去广告统计/官替全过程）',u:'服务端：需要完整明细做二次处理时调用',p:'/api/jx/server?url=<播放链接>'},
  {n:'非正片区间',t:'both',d:'非正片区间标注列表（SponsorBlock 思路，播放器按区间跳过）',u:'客户端：播放器读取跳过区间；服务端：增删标注',p:'/api/skip'},
  {n:'弹幕规则库',t:'admin',d:'弹幕过滤规则库列表',u:'后台管理：查看/增删弹幕过滤规则（需登录）',p:'/api/danmaku'},
  {n:'官替映射',t:'admin',d:'官替映射列表（官方剧名 ↔ 资源站标准剧名）',u:'后台管理：官替映射专区数据（需登录）',p:'/api/maps'},
  {n:'官方平台配置',t:'admin',d:'官方平台自动更新配置列表',u:'后台管理：官方平台自动更新配置（需登录）',p:'/api/platforms'},
  {n:'资源站列表',t:'admin',d:'资源站列表（默认隐藏失效站）',u:'后台管理：资源站启停与状态（需登录）',p:'/api/sites'},
  {n:'资源站搜索测试',t:'admin',d:'搜索测试单个资源站是否可用/命中',u:'后台管理：验证采集站可用性（需登录）',p:'/api/sites/test?name=<站名>&kw=<词>'},
  {n:'检查更新',t:'admin',d:'检查远程是否有新版本（读取线上 latest.json）',u:'后台管理：版本检查（需登录应用更新）',p:'/api/update/check'},
  {n:'AI 去广告配置',t:'admin',d:'查看 AI 去广告配置（key 打码）',u:'后台管理：AI 引擎参数查看/更新（需登录）',p:'/api/ai/config'},
  {n:'运行统计',t:'both',d:'接口调用次数 / 广告统计 / 运行时长（JSON）',u:'通用：监控服务状态与调用量',p:'/api/stats'},
  {n:'健康检查',t:'both',d:'服务存活状态与版本号',u:'通用：探活/负载均衡健康检测',p:'/healthz'}
];
function badgeOf(t){
  var m={client:['客户端','api-badge client'],server:['服务端','api-badge server'],admin:['后台管理','api-badge admin'],both:['通用','api-badge both']};
  var b=m[t]||m.both;
  return '<span class="'+b[1]+'">'+b[0]+'</span>';
}
function renderApis(){
  var base=location.origin;
  el('apiList').innerHTML=APIS.map(function(a){
    var u=base+a.p;
    var c='curl -s "'+u+'"';
    return '<div class="api-row">'+
      '<div class="api-meta"><b>'+a.n+badgeOf(a.t)+'</b><div class="d">'+a.d+'</div>'+(a.u?'<div class="use">用途：'+a.u+'</div>':'')+'</div>'+
      '<div class="api-cmd"><span class="muted">URL </span>'+esc(u)+'<br><span class="muted">CURL </span>'+esc(c)+'</div>'+
      '<div style="display:flex;flex-direction:column;gap:6px;flex-shrink:0">'+
      '<button class="copy-btn" onclick="copyApi(this)" data-t="'+esc(u)+'">复制URL</button>'+
      '<button class="copy-btn" onclick="copyApi(this)" data-t="'+esc(c)+'">复制CURL</button>'+
      '</div></div>';
  }).join('');
}
function copyApi(btn){
  var txt=btn.dataset.t;
  var ok=function(){btn.textContent='✓ 已复制';btn.classList.add('copied');setTimeout(function(){btn.textContent=btn.dataset.label;btn.classList.remove('copied');},1600);};
  btn.dataset.label=btn.textContent;
  if(navigator.clipboard&&navigator.clipboard.writeText){
    navigator.clipboard.writeText(txt).then(ok,function(){fbCopy(txt,btn,ok);});
  }else{fbCopy(txt,btn,ok);}
}
function fbCopy(t,btn,ok){
  var ta=document.createElement('textarea');ta.value=t;ta.style.position='fixed';ta.style.opacity='0';
  document.body.appendChild(ta);ta.select();
  try{document.execCommand('copy');ok();}catch(e){btn.textContent='失败';}
  document.body.removeChild(ta);
}
renderApis();
async function refresh(){
  try{
    const r=await fetch('/api/stats');
    const j=await r.json();
    el('ver').textContent=j.version;
    el('statGrid').innerHTML=
      card('接口调用',j.requests,j.errors+' 次失败')+
      card('累计片段',j.total_segs,'广告 '+j.ad_segs)+
      card('广告占比',j.ad_ratio+'%','保留 '+j.kept_segs)+
      card('运行时长',j.uptime,(j.last_msg||''));
  
    // 接口调用次数
    const cl=j.calls||{};
    const keys=Object.keys(cl);
    el('calls').innerHTML=keys.length?keys.map(function(k){return '<tr><td><code>'+esc(k)+'</code></td><td>'+cl[k]+'</td></tr>';}).join('')
      :'<tr><td colspan="2" class="muted">暂无接口调用</td></tr>';
    
    // 最近调用
    const rc=j.recent||[];
    el('recentHint').textContent='（最近 '+rc.length+' 条，每 3 秒自动刷新）';
    el('recent').innerHTML=rc.slice().reverse().map(function(c){
      return '<tr><td class="muted">'+esc(c.time)+'</td>'+
        '<td><code>'+esc(c.api)+'</code></td>'+
        '<td><span class="url" title="'+esc(c.url)+'">'+esc(c.url||'—')+'</span></td>'+
        '<td>'+(c.ms>0?(Math.round(c.ms)+'ms'):'—')+'</td>'+
        '<td class="'+(c.ok?'ok':'fail')+'">'+(c.ok?'✓ 成功':'✗ 失败')+'</td>'+
        '<td class="muted">'+esc(c.msg||'')+'</td></tr>';
    }).join('')||'<tr><td colspan="6" class="muted">暂无调用记录</td></tr>';
  }catch(e){el('recent').innerHTML='<tr><td colspan="6" class="muted">加载失败: '+esc(e.message)+'</td></tr>';}
}
refresh(); setInterval(refresh,3000);
</script>
</body>
</html>
`

// playerPageHTML 独立外置播放页（/player?url=<m3u8|mp4>&title=…）：hls.js 多 CDN 兜底，mp4 直链原生播放
// 响应带全站 CORS 头（withCORS），页面内 hls.js 请求分片时携带 Origin 头，跨域播放无障碍
const playerPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MXGT-Go 播放器</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{min-height:100vh;background:#0f172a;color:#e2e8f0;font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;padding:20px;display:flex;flex-direction:column;align-items:center}
  h1{font-size:18px;margin-bottom:6px;color:#f5f3ff}
  .sub{font-size:12px;color:#94a3b8;margin-bottom:14px;text-align:center;word-break:break-all;max-width:900px}
  video{width:100%;max-width:960px;aspect-ratio:16/9;background:#000;border-radius:14px;box-shadow:0 10px 40px rgba(0,0,0,.5)}
  .tip{font-size:12px;color:#64748b;margin-top:12px}
  a{color:#c084fc;text-decoration:none}
</style>
</head>
<body>
  <h1>🎬 MXGT-Go 无广告播放器</h1>
  <div class="sub" id="src"></div>
  <video id="player" controls playsinline autoplay></video>
  <div class="tip">来源：资源站 → 官替去广告直链（支持跨域）。m3u8 自动走 hls.js，mp4/mkv 直链原生播放。</div>
<script>
function g(n){return new URLSearchParams(location.search).get(n)||''}
var src=g('url'),title=g('title'),key=g('key');
var video=document.getElementById('player');
if(title){document.title=title+' - MXGT-Go 播放器';}
document.getElementById('src').textContent=src||'缺少 url 参数';
function isDirect(u){return /\.(mp4|mkv|webm|flv)(\?|$)/i.test(u);}
// 播放走本服务代理 /api/play：分片同源 + 服务端带 UA/Referer/超时，解决第三方 m3u8 跨域与限速卡顿；
// 已是本服务地址（/api/play、/api/clean 等）不再二次代理
function isLocalProxy(u){return u.indexOf('/api/play')===0||u.indexOf('/api/clean')===0||/^https?:\/\/[^/]*\/api\/(play|clean)/.test(u);}
function proxyPlayURL(u){return isLocalProxy(u)?u:'/api/play?url='+encodeURIComponent(u);}
// 非正片区间自动跳过（内嵌/占位广告）：按视频 key 加载区间（key 为空只取全局区间），播放时 seek 跳过
var skipRanges=[],lastSkip=0;
fetch('/api/skip'+(key?'?key='+encodeURIComponent(key):'')).then(function(r){return r.json()}).then(function(d){skipRanges=(d.ranges||[]).filter(function(r){return r&&r.start<r.end;});}).catch(function(){});
video.addEventListener('timeupdate',function(){
  var t=video.currentTime,now=Date.now();
  if(now-lastSkip<300)return;
  for(var i=0;i<skipRanges.length;i++){
    var rg=skipRanges[i];
    if(t>rg.start&&t<rg.end-0.3){lastSkip=now;video.currentTime=rg.end+0.01;break;}
  }
});
// hls.js 抗卡顿配置：加大缓冲 + 分片/清单失败重试（指数退避）+ 软件解密兜底
function hlsConfig(){return {
  enableWorker:true,
  maxBufferLength:60,maxMaxBufferLength:180,backBufferLength:30,startLevel:-1,maxBufferSize:60*1000*1000,
  fragLoadPolicy:{default:{maxNumRetry:5,retryDelay:400,backoff:'exponential',maxRetryDelay:4000}},
  manifestLoadPolicy:{default:{maxNumRetry:3,retryDelay:300}},
  levelLoadPolicy:{default:{maxNumRetry:3,retryDelay:300}},
  enableSoftwareAES:true,
  xhrSetup:function(xhr){xhr.withCredentials=false;xhr.setRequestHeader('Origin',location.origin);}
};}
function loadHls(cb){
  if(window.Hls){return cb();}
  var cdn=['https://cdn.jsdelivr.net/npm/hls.js@1/dist/hls.min.js',
           'https://cdnjs.cloudflare.com/ajax/libs/hls.js/1.5.20/hls.min.js',
           'https://unpkg.com/hls.js@1/dist/hls.min.js',
           'https://fastly.jsdelivr.net/npm/hls.js@1/dist/hls.min.js'];
  var i=0;
  (function load(){
    if(i>=cdn.length){alert('hls.js 加载失败，请检查网络后重试');return;}
    var s=document.createElement('script');
    s.src=cdn[i++];s.onload=cb;s.onerror=load;
    document.head.appendChild(s);
  })();
}
if(src){
  if(isDirect(src)){
    video.src=src;video.play().catch(function(){});
  }else if(video.canPlayType('application/vnd.apple.mpegurl')){
    video.src=proxyPlayURL(src);video.play().catch(function(){});
  }else{
    loadHls(function(){
      if(Hls&&Hls.isSupported()){
        var hls=new Hls(hlsConfig());
        hls.loadSource(proxyPlayURL(src));hls.attachMedia(video);
        video.play().catch(function(){});
      }else if(video.canPlayType('application/vnd.apple.mpegurl')){
        video.src=proxyPlayURL(src);
      }else{alert('当前浏览器不支持 HLS 播放');}
    });
  }
}
</script>
</body>
</html>
`

// handlePlayer 渲染独立外置播放页（开放，无需登录；播放地址来自 /api/replace 的 external_url，基于请求 Host 动态生成）
func handlePlayer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, playerPageHTML)
}

// ============================================================
// 播放代理 /api/play?url=<m3u8|分片|密钥>：解决播放卡顿
//   播放器请求本服务 → 代拉源站 m3u8/分片（带浏览器 UA + 站域 Referer + 超时），
//   m3u8 内分片/密钥/子列表地址改写为本服务代理地址。
//   收益：① 第三方分片无需 CORS（hls.js XHR 跨域不再失败导致黑屏/卡顿）
//         ② 源站限速/反爬由服务端统一应对（UA/Referer/重试）
//         ③ 播放器与分片同源，走本服务稳定链路
// ============================================================

// playHTTP 播放代理专用客户端：更长超时（分片可能慢）+ 关闭证书校验（部分源 http/自签）
var playHTTP = &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	Proxy:           http.ProxyFromEnvironment,
}}

// playlistCache 播放代理去广告结果缓存：同一 m3u8 清单播放器会重复请求，避免反复抓源站+过滤（慢/触发反爬）
var playlistCache sync.Map // key=URL → cachedPlaylist
const playlistCacheTTL = 60 * time.Second

type cachedPlaylist struct {
	ts   time.Time
	body []byte
}

func getCachedPlaylist(key string) ([]byte, bool) {
	v, ok := playlistCache.Load(key)
	if !ok {
		return nil, false
	}
	c := v.(cachedPlaylist)
	if time.Since(c.ts) > playlistCacheTTL {
		playlistCache.Delete(key)
		return nil, false
	}
	return c.body, true
}

func setCachedPlaylist(key string, body []byte) {
	playlistCache.Store(key, cachedPlaylist{ts: time.Now(), body: body})
}

// cleanPlaylistOnce 对已抓取的 m3u8 body 做增强去广告，返回过滤后的播放列表文本。
// master 列表（仅码率分叉、无分片）或解析失败返回 ok=false，由调用方原样改写（其子列表请求仍会走本代理过滤）。
// 供 /api/play 播放代理复用，保证「播放即去广告」且与「新版增强测试播放」引擎一致。
func cleanPlaylistOnce(body []byte, mediaURL string) (string, bool) {
	segs, _, isMaster, err := parseM3U8(string(body), mediaURL)
	if err != nil || isMaster || len(segs) == 0 {
		return "", false
	}
	detectAds(segs, false) // aggresive=false：禁用同目录聚类去重（统一正片会被误判为广告团，导致播放列表被删空）
	enhancedDetectAds(segs)
	applyAdSafetyRoof(segs) // 播放安全兜底：宁可少删广告，不可删到播不了
	return buildFilteredM3U8(segs, maxTargetDuration(segs)), true
}

// isPlaylistURL 判断请求目标是否为 m3u8 播放列表（按 URL 后缀或响应 Content-Type）
func isPlaylistURL(raw, ct string) bool {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, ".m3u8") || strings.HasSuffix(lower, ".m3u") {
		return true
	}
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "mpegurl") || strings.Contains(ct, "m3u8") || strings.Contains(ct, "x-mpegurl")
}

// playProxyURL 生成本服务播放代理地址（相对路径，页面同源）
func playProxyURL(u string) string {
	return "/api/play?url=" + url.QueryEscape(u)
}

// playProxyAbsURL 生成本服务播放代理绝对地址（基于请求 Host），供外部播放器（TVBox 等）使用
func playProxyAbsURL(proxyBase, u string) string {
	return proxyBase + "/api/play?url=" + url.QueryEscape(u)
}

// rewritePlaylist 改写 m3u8 播放列表：分片/密钥/子列表地址全部指向本服务代理（解决跨域与源站限速卡顿）
// proxyBase 非空时输出绝对代理地址（外部播放器可播）；为空输出相对路径（同源页面）
func rewritePlaylist(body []byte, baseURL, proxyBase string) []byte {
	base := baseURL
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[:i+1]
	}
	prox := func(u string) string {
		if proxyBase != "" {
			return playProxyAbsURL(proxyBase, u)
		}
		return playProxyURL(u)
	}
	abs := func(u string) string {
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			return u
		}
		return base + u
	}
	lines := strings.Split(string(body), "\n")
	var out []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") {
			if strings.HasPrefix(t, "#EXT-X-KEY:") && strings.Contains(t, "URI=") {
				// 改写 AES-128 密钥地址（URI 可能为相对路径）
				t = regexp.MustCompile(`URI="([^"]+)"`).ReplaceAllStringFunc(t, func(m string) string {
					inner := regexp.MustCompile(`^URI="([^"]+)"$`).FindStringSubmatch(m)
					if len(inner) < 2 {
						return m
					}
					return `URI="` + prox(abs(inner[1])) + `"`
				})
			}
			out = append(out, t)
			continue
		}
		if t == "" {
			out = append(out, ln)
			continue
		}
		out = append(out, prox(abs(t)))
	}
	return []byte(strings.Join(out, "\n"))
}

// ============================================================
// 广告核查 /api/audit：把每一个「不连贯」片段列出来，供人工逐段核查是否有广告
// 覆盖规则漏检兜底：规则引擎判不了的（时长与正片相同、无 URL/标签特征），
// 人工确认后一键写入 skip 区间（按视频标识），播放时自动跳过 → 实际播放不再出现。
// 时间轴为「过滤后时间轴」，与 /api/play 播放器时间轴一致，标记区间不错位。
// ============================================================

// AuditSegment 核查输出段（过滤后时间轴）
type AuditSegment struct {
	Index    int      `json:"index"`
	Start    float64  `json:"start"`
	End      float64  `json:"end"`
	Duration float64  `json:"duration"`
	URI      string   `json:"uri"`
	AbsURI   string   `json:"abs_uri"`
	IsAd     bool     `json:"is_ad"`
	AdReason string   `json:"ad_reason,omitempty"`
	Suspect  []string `json:"suspect,omitempty"`
}

// AuditResult 核查结果
type AuditResult struct {
	Success  bool           `json:"success"`
	Message  string         `json:"message,omitempty"`
	URL      string         `json:"url"`
	MediaURL string         `json:"media_url,omitempty"`
	TotalRaw int            `json:"total_raw"` // 原始段数
	AdCount  int            `json:"ad_count"`  // 规则已判广告数
	Segments []AuditSegment `json:"segments"`  // 保留段（过滤后时间轴，含可疑标记）
	Ads      []AuditSegment `json:"ads"`       // 规则已删广告段（原始序号）
}

// auditParse 解析 + 增强检测 + 生成核查清单（与 /api/play 同一套检测）
func auditParse(rawURL string) AuditResult {
	res := AuditResult{Success: false, URL: rawURL}
	body, err := newClient().fetch(rawURL)
	if err != nil {
		res.Message = "抓取失败: " + err.Error()
		return res
	}
	mediaURL := rawURL
	segs, variants, isMaster, err := parseM3U8(body, mediaURL)
	if err != nil {
		res.Message = "解析失败: " + err.Error()
		return res
	}
	if isMaster && len(variants) > 0 {
		sort.Slice(variants, func(a, b int) bool { return variants[a].Bandwidth > variants[b].Bandwidth })
		mediaURL = resolve(rawURL, variants[0].URI)
		body2, err2 := newClient().fetch(mediaURL)
		if err2 != nil {
			res.Message = "media 抓取失败: " + err2.Error()
			return res
		}
		segs, _, _, _ = parseM3U8(body2, mediaURL)
	}
	res.MediaURL = mediaURL
	res.TotalRaw = len(segs)
	if len(segs) == 0 {
		res.Message = "无有效片段"
		return res
	}
	detectAds(segs, false) // 禁用聚类去重，避免统一正片被误判为广告团导致核查清单被删空
	enhancedDetectAds(segs)
	applyAdSafetyRoof(segs) // 播放安全兜底：宁可不删广告，不可删到播不了

	var kept []Segment
	for _, s := range segs {
		if s.IsAd {
			res.Ads = append(res.Ads, AuditSegment{Index: s.Index, Duration: s.Duration, URI: s.URI, AbsURI: s.AbsURI, IsAd: true, AdReason: s.AdReason})
			res.AdCount++
		} else {
			kept = append(kept, s)
		}
	}
	// 过滤后时间轴（跳过已删广告段累计）
	acc := 0.0
	for _, s := range kept {
		as := AuditSegment{
			Index: s.Index, Start: acc, End: acc + s.Duration,
			Duration: s.Duration, URI: s.URI, AbsURI: s.AbsURI,
		}
		// 不连贯/可疑标记
		if s.Discontinuity {
			as.Suspect = append(as.Suspect, "拼接点")
		}
		if s.Duration < 2.0 {
			as.Suspect = append(as.Suspect, "偏短")
		}
		res.Segments = append(res.Segments, as)
		acc += s.Duration
	}
	// 时长突变：与保留段前后邻段均值比较（>2 倍或 <1/2）
	for i := range res.Segments {
		as := &res.Segments[i]
		var sum float64
		var cnt int
		for j := i - 1; j <= i+1; j++ {
			if j < 0 || j >= len(res.Segments) || j == i {
				continue
			}
			sum += res.Segments[j].Duration
			cnt++
		}
		if cnt >= 1 {
			avg := sum / float64(cnt)
			if as.Duration < avg/2 || as.Duration > avg*2 {
				as.Suspect = append(as.Suspect, "时长突变")
			}
		}
	}
	sus := 0
	for i := range res.Segments {
		sus += len(res.Segments[i].Suspect)
	}
	res.Success = true
	res.Message = fmt.Sprintf("原始 %d 段 · 规则已删广告 %d 段 · 保留 %d 段 · 可疑 %d 处", res.TotalRaw, res.AdCount, len(res.Segments), sus)
	return res
}

// handleAudit GET /api/audit?url=<m3u8>：广告核查（开放，播放器/核查面板使用）
func handleAudit(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSpace(r.URL.Query().Get("url"))
	if u == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少 url 参数"})
		return
	}
	res := auditParse(u)
	recordCall("/api/audit", u, res.Success, 0, res.Message)
	writeJSON(w, res)
}

// handlePlayProxy GET /api/play?url=<http(s)地址>：m3u8 播放列表改写返回；分片/密钥/媒体字节流透传（支持 Range）
func handlePlayProxy(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" || (!strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://")) {
		http.Error(w, "url 参数需为 http(s) 地址", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", raw, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", siteFetchUA)
	if base, e := url.Parse(raw); e == nil {
		req.Header.Set("Referer", base.Scheme+"://"+base.Host+"/")
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := playHTTP.Do(req)
	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Content-Range, Accept-Ranges")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, Range, Accept, User-Agent, Referer")
	w.Header().Set("Cache-Control", "no-store")
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		http.Error(w, "upstream "+resp.Status, http.StatusBadGateway)
		return
	}
	if isPlaylistURL(raw, ct) {
		var b []byte
		if cb, ok := getCachedPlaylist(raw); ok {
			b = cb
		} else {
			b, _ = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		}
		// 基于请求 Host 生成绝对代理前缀：分片/密钥/子列表地址输出为绝对地址，外部播放器（TVBox 等）可直接播放
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		proxyBase := scheme + "://" + r.Host
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
		// 增强去广告：输出过滤后清单，分片仍改走本代理（不卡顿 + 无广告）
		if filtered, ok := cleanPlaylistOnce(b, raw); ok {
			setCachedPlaylist(raw, []byte(filtered))
			w.Write(rewritePlaylist([]byte(filtered), raw, proxyBase))
			return
		}
		// master 列表 / 解析失败：原样改写（子列表与分片仍走本代理过滤）
		setCachedPlaylist(raw, b)
		w.Write(rewritePlaylist(b, raw, proxyBase))
		return
	}
	// 分片/密钥/媒体字节流直传
	if resp.StatusCode == http.StatusPartialContent {
		w.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
		w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
		w.WriteHeader(http.StatusOK)
	}
	_, _ = io.Copy(w, resp.Body)
}

// handleFront 渲染前台接口调用详情页
func handleFront(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, frontPageHTML)
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

// manifestURLs 版本清单候选源（按序回退）：
//  1. raw.githubusercontent.com 主源（部署方默认源）
//  2. jsDelivr CDN（gh/仓库@分支/latest.json，GitHub 原生 CDN 缓存偶有延迟时兜底）
//  3. GitHub API contents 接口（内容为 base64，始终与 git 分支 HEAD 一致，最终兜底）
//
// 任一源成功即视为最新清单，避免单个源缓存/故障导致已部署客户检测不到新版本。
var manifestURLs = []string{
	manifestRawURL,
	"https://cdn.jsdelivr.net/gh/" + repoOwner + "/" + repoName + "@" + repoBranch + "/latest.json",
	"https://api.github.com/repos/" + repoOwner + "/" + repoName + "/contents/latest.json?ref=" + repoBranch,
}

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

// manifestHTTP 用短超时的独立客户端，避免连通性差时长时间阻塞 HTTP 请求导致前端 Failed to fetch
var manifestHTTP = &http.Client{Timeout: 8 * time.Second}

// fetchManifest 拉取线上版本清单，按 manifestURLs 多镜像回退，任一路径成功即返回
func fetchManifest() (*UpdateManifest, error) {
	var lastErr error
	for _, u := range manifestURLs {
		m, err := fetchManifestFrom(u)
		if err == nil {
			return m, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// fetchManifestFrom 从单个源拉取并解析版本清单；GitHub API contents 源的内容为 base64
func fetchManifestFrom(u string) (*UpdateManifest, error) {
	resp, err := manifestHTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest HTTP %d (%s)", resp.StatusCode, u)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	raw := string(body)
	if strings.Contains(u, "/contents/") {
		var enc struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &enc); err != nil || enc.Content == "" {
			return nil, fmt.Errorf("manifest API 内容为空或解析失败")
		}
		b, err := base64.StdEncoding.DecodeString(enc.Content)
		if err != nil {
			return nil, err
		}
		raw = string(b)
	}
	var m UpdateManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
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
	// 注意：Release tag 为 go-vvX.Y.Z（VER 本身含前导 v），下载地址必须用双 v 才能命中
	out["download_url"] = releaseBaseURL + "/go-vv" + m.Version + "/" + m.Zip
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

// httpServer 全局 HTTP 服务句柄（更新重启时可优雅关闭并释放端口）
var httpServer *http.Server

// waitPortFree 阻塞轮询直到 addr 上无法建立连接（即端口已释放），最久 timeout
func waitPortFree(addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return // 连接拒绝 = 端口已释放
		}
		conn.Close()
		time.Sleep(150 * time.Millisecond)
	}
}

// replaceAndRestart 用新二进制替换当前文件并重启进程。
// 修复「更新后不自动重启」+「重启等待时间过长」：
//  1. 先快速关闭当前 HTTP 服务释放端口（Shutdown 短超时 + 端口轮询限时），
//     避免长时间阻塞（Shutdown 立即关闭 listener，活跃连接最多等 2s）；
//  2. 新进程用 Setsid 脱离当前会话，避免老进程退出时 SIGHUP 把新进程一起带走（nohup/setsid 部署场景）；
//  3. 新进程端口占用时 300ms 快速重试（见 main 内绑定循环），旧进程释放端口后新进程几乎立即绑定成功。
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
	// 1) 停当前服务，释放监听端口（短超时 Shutdown：listener 立即关闭，活跃连接最多等 2s）
	listenAddr := ""
	if httpServer != nil {
		listenAddr = httpServer.Addr
		if listenAddr != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			go httpServer.Shutdown(ctx) // 关闭 listener 并等待活跃连接结束（最多 2s）
			waitPortFree(listenAddr, 3*time.Second)
			cancel()
		}
	}
	// 2) 启动新进程（继承参数与工作目录；Setsid 脱离会话防 SIGHUP）
	args := os.Args[1:]
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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

// ============================================================
// 官替链路（Official Replace）：官方视频页 → 资源站搜索 → 匹配 → 取集 → 去广告
// 参照 PHP gz/OfficialReplaceManager：识别平台→抓标题→搜索→匹配→取集 m3u8→清广输出
// ============================================================

// Site 一个采集资源站（resource_sites.json，与 PHP 版同一份列表，默认全部禁用）
type Site struct {
	Name     string `json:"name"`
	SiteURL  string `json:"site_url"`
	APIURL   string `json:"api_url"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Enabled  bool   `json:"enabled"`
	Note     string `json:"note"`
	Priority int    `json:"priority"`
}

// SitesConfig 资源站配置
type SitesConfig struct {
	Version    string `json:"version"`
	UpdateDate string `json:"update_date"`
	Sites      []Site `json:"sites"`
	// OnlyM3U8 资源站搜索/采集返回时只保留 m3u8 播放地址（过滤 mp4 等其它格式），后台「资源站管理」可开关
	OnlyM3U8 bool `json:"only_m3u8"`
}

var sitesMu sync.Mutex

// sitesConfigPath 配置落盘位置（与可执行文件同目录，便于后台启停后持久化）
func sitesConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "resource_sites.json")
	}
	return "resource_sites.json"
}

// loadSites 读取资源站配置：优先磁盘文件（保留用户启停），否则用内置常量并落盘
func loadSites() *SitesConfig {
	cfg := &SitesConfig{}
	_ = json.Unmarshal([]byte(defaultSitesJSON), cfg)
	p := sitesConfigPath()
	if b, err := os.ReadFile(p); err == nil {
		c2 := &SitesConfig{}
		if json.Unmarshal(b, c2) == nil && len(c2.Sites) > 0 {
			return c2
		}
	}
	// 首次运行：内置配置落盘
	_ = os.WriteFile(p, []byte(defaultSitesJSON), 0o644)
	return cfg
}

func saveSites(cfg *SitesConfig) error {
	b, _ := json.MarshalIndent(cfg, "", "    ")
	return os.WriteFile(sitesConfigPath(), b, 0o644)
}

// enabledSites 返回已启用的站点（按优先级）
func enabledSites(cfg *SitesConfig) []Site {
	out := []Site{}
	for _, s := range cfg.Sites {
		if s.Enabled && strings.TrimSpace(s.APIURL) != "" {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// ============ 官方平台识别 ============

type platformHint struct{ Name, Hint string }

var platformHints = []platformHint{
	{"腾讯视频", "v.qq.com"}, {"腾讯视频", "m.v.qq.com"},
	{"爱奇艺", "iqiyi.com"}, {"优酷", "youku.com"},
	{"芒果TV", "mgtv.com"}, {"哔哩哔哩", "bilibili.com"},
	{"搜狐视频", "sohu.com"}, {"PP视频", "pptv.com"},
}

// detectPlatform 识别官方平台：优先用官方平台配置（按优先级+域名+URL正则），未配置时回退内置平台提示
func detectPlatform(raw string) string {
	if p := matchOfficialPlatform(raw); p != nil {
		return p.Platform
	}
	host := ""
	if u, err := url.Parse(raw); err == nil {
		host = strings.ToLower(u.Host)
	}
	for _, p := range platformHints {
		if strings.Contains(host, p.Hint) {
			return p.Name
		}
	}
	return ""
}

// ============ 标题抓取与解析 ============

type VideoInfo struct {
	Title      string
	BaseTitle  string
	SeasonNum  int
	EpisodeNum int
	Episode    string
}

var ogTitleRe = regexp.MustCompile(`(?i)<meta[^>]+(?:property|name)=["'](?:og:title|twitter:title)["'][^>]+content=["']([^"']+)["']`)
var titleTagRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
var tencentVidRe = regexp.MustCompile(`(?i)[?&]vid=([A-Za-z0-9]+)`)
var tencentVidJSONRe = regexp.MustCompile(`(?i)"vid"\s*:\s*"([A-Za-z0-9]+)"`)
// tencentVidPathRe 腾讯链接从路径提取当前集 vid：
//   - 剧集页  /x/page/{vid}.html                → vid
//   - 封面页  /x/cover/{album_id}/{vid}.html     → 最后一段 vid
//   - 旧式      /x/cover/{vid}.html              → vid
var tencentVidPathRe = regexp.MustCompile(`(?i)/x/(?:page/|cover/(?:[^/.]+/)?)([A-Za-z0-9]+)(?:\.html)?(?:\?|$)`)
var iqiyiPageIDRe = regexp.MustCompile(`(?i)v_([A-Za-z0-9]+)\.html`)

// bvidRe 哔哩哔哩视频 ID（BV 号）：页面 412 反爬，但公开 API 可直接取标题
var bvidRe = regexp.MustCompile(`(?i)/video/(BV[0-9A-Za-z]{10,12})`)

// iqiyiJSONNameRe 爱奇艺 /adv/ SEO 静态页内嵌 JSON 的视频名（第一个 name 即视频名，形如「生逢其时第1集」）
var iqiyiJSONNameRe = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)

// mojibakeRe 匹配 GBK 被当 UTF-8 解码产生的替换字符 U+FFFD（乱码标题标记）
var mojibakeRe = regexp.MustCompile(`\x{FFFD}`)

// fetchIqiyiTitle 用爱奇艺无需登录的公开接口从 v_xxx.html 页面 id 解析真实剧名：
//  1. 爱奇艺《视频页》是纯 JS 渲染壳 —— 静态 HTML 里 <title> 恒为平台通用标题、无 og:title、无内嵌剧名；
//     直接抓页面永远拿不到真实剧名（这是「官替映射专区」爱奇艺无法获取的根因）。
//  2. 方案：pcw-api.iq.com/api/decode/<页面id> 把页面 id 解码成真实 tvid，
//     再请求 pcw-api.iqiyi.com/video/video/baseinfo/<tvid> 取 data.name（如「利剑·玫瑰第1集」）。
func fetchIqiyiTitle(raw string) string {
	m := iqiyiPageIDRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	pageID := m[1]
	// ① 爱奇艺 /adv/ SEO 静态页（搜索引擎收录页）：真实剧名以内嵌 JSON「"name":"剧名第N集"」输出，
	//    非 JS 壳、无编码问题、无需登录。PC 播放页为 JS 渲染壳（<title> 只有平台名），此页是静态的。
	if body, err := newClient().fetch("https://www.iqiyi.com/adv/v_" + pageID + ".html"); err == nil {
		if name := iqiyiJSONNameRe.FindStringSubmatch(body); len(name) > 1 {
			t := strings.TrimSpace(name[1])
			if t != "" && !mojibakeRe.MatchString(t) {
				return t
			}
		}
	}
	// ② 兜底：decode 接口 page id → tvid，再用 baseinfo 取真实剧名（部分视频 decode 失败/无数据）
	tvid := ""
	body, err := newClient().fetch("https://pcw-api.iq.com/api/decode/" + url.QueryEscape(pageID) +
		"?platformId=3&modeCode=intl&langCode=sg")
	if err == nil {
		var d struct {
			Code string          `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if e := json.Unmarshal([]byte(body), &d); e == nil && d.Code == "0" && len(d.Data) > 0 {
			// data 可能为数字字符串或纯数字
			var num json.Number
			if e2 := json.Unmarshal(d.Data, &num); e2 == nil {
				tvid = num.String()
			} else {
				var s string
				if e2 := json.Unmarshal(d.Data, &s); e2 == nil {
					tvid = s
				}
			}
		}
	}
	if tvid == "" {
		return ""
	}
	// ③ tvid → 视频基础信息（剧名）
	bi, err := newClient().fetch("https://pcw-api.iqiyi.com/video/video/baseinfo/" + url.QueryEscape(tvid))
	if err != nil {
		return ""
	}
	var b struct {
		Data struct {
			Name        string `json:"name"`
			ShortTitle  string `json:"shortTitle"`
			AlbumName   string `json:"albumName"`
			Subtitle    string `json:"subtitle"`
			EpisodeNum  int    `json:"episode"`
			AllTimeName string `json:"allTimeName"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(bi), &b); e != nil {
		return ""
	}
	if b.Data.Name != "" {
		return strings.TrimSpace(b.Data.Name)
	}
	if b.Data.ShortTitle != "" {
		return strings.TrimSpace(b.Data.ShortTitle)
	}
	if b.Data.AlbumName != "" {
		return strings.TrimSpace(b.Data.AlbumName)
	}
	return ""
}

// fetchTencentVid 提取腾讯视频当前集 vid：优先 URL 参数 vid=，其次路径（/x/page/xxx、/x/cover/专辑id/xxx.html），最后页面 JSON "vid":"xxx"
func fetchTencentVid(raw, body string) string {
	if m := tencentVidRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	if m := tencentVidPathRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	if body != "" {
		if m := tencentVidJSONRe.FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// fetchTencentTitleByVid 用腾讯 getinfo 接口按 vid 取正式片名（返回空=无有效 ti，如视频审核中/需登录）
func fetchTencentTitleByVid(vid string) string {
	if vid == "" {
		return ""
	}
	gURL := "https://vv.video.qq.com/getinfo?vid=" + url.QueryEscape(vid) +
		"&platform=101001&charge=0&otype=json&defn=shd&sdtfrom=v1010&host=v.qq.com"
	if body, err := newClient().fetch(gURL); err == nil {
		if m := regexp.MustCompile(`"ti"\s*:\s*"([^"]+)"`).FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// fetchBilibiliTitle 用 B 站公开 API 按 BV 号取真实标题：
// 视频页对数据中心 IP 反爬（412 或「出错啦!」壳），但 x/web-interface/view API 无需登录即可返回 title。
func fetchBilibiliTitle(bvid string) string {
	if bvid == "" {
		return ""
	}
	body, err := newClient().fetch("https://api.bilibili.com/x/web-interface/view?bvid=" + url.QueryEscape(bvid))
	if err != nil {
		return ""
	}
	var res struct {
		Code int `json:"code"`
		Data struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &res) == nil && res.Code == 0 && strings.TrimSpace(res.Data.Title) != "" {
		return strings.TrimSpace(res.Data.Title)
	}
	return ""
}

func fetchVideoTitle(raw, vidHint string) string {
	// 爱奇艺：页面是纯 JS 渲染壳，静态只能拿到平台通用标题 → 优先用公开接口解析真实剧名（无需登录）
	if isIqiyiURL(raw) {
		if t := fetchIqiyiTitle(raw); t != "" {
			return t
		}
	}
	// 哔哩哔哩：视频页对数据中心 IP 反爬（412/「出错啦!」），但公开 API 可直接取标题
	if m := bvidRe.FindStringSubmatch(raw); len(m) > 1 {
		if t := fetchBilibiliTitle(m[1]); t != "" {
			return t
		}
	}
	title := ""
	body := ""
	if b, err := fetchPageBytes(raw); err == nil {
		body = string(b)
		if m := ogTitleRe.FindStringSubmatch(body); len(m) > 1 {
			title = strings.TrimSpace(m[1])
		}
		if title == "" {
			if m := titleTagRe.FindStringSubmatch(body); len(m) > 1 {
				title = strings.TrimSpace(m[1])
				// 去掉 "xxx_腾讯视频" 类冗余后缀
				if i := strings.LastIndex(title, "_"); i > 0 {
					title = title[:i]
				}
			}
		}
	}
	// 乱码兜底：GBK 页面被当 UTF-8 解码时标题会含替换字符（�），尝试按页面字符集重新解码
	if title != "" && mojibakeRe.MatchString(title) {
		if b, err := fetchPageBytesDecoded(raw); err == nil {
			if t := parseTitleFromBytes(b); t != "" {
				title = t
			}
		}
	}
	// 腾讯无标题时用 getinfo 兜底取正式片名：优先调用方传入 vid，其次自动从 URL/页面 JSON 提取。
	// 壳标题（静态只有平台名的 JS 渲染壳页，title 非空但非真实剧名）同样要走 getinfo，否则会短路为平台名。
	isTX := strings.Contains(raw, "v.qq.com") || strings.Contains(raw, "m.v.qq.com")
	if isTX {
		if vidHint == "" {
			vidHint = fetchTencentVid(raw, body)
		}
		if vidHint != "" && (title == "" || isShellTitle(title)) {
			if t := fetchTencentTitleByVid(vidHint); t != "" {
				title = t
			}
		}
	}
	// 无头渲染兜底：静态/接口都只拿到壳标题或拿不到标题时，用 render-title/（Node+Chromium）执行 JS
	// 取真实剧名（腾讯/爱奇艺等 JS 渲染壳页的可靠解法）。未部署 render-title/ 时立即返回空，零开销。
	if title == "" || isShellTitle(title) {
		if t := renderTitleViaNode(raw); t != "" && !suspiciousTitle(t) && !isShellTitle(t) {
			title = t
		}
	}
	return title
}

// isIqiyiURL 判断是否为爱奇艺视频页链接
func isIqiyiURL(raw string) bool {
	host := ""
	if u, err := url.Parse(raw); err == nil {
		host = strings.ToLower(u.Host)
	}
	return strings.Contains(host, "iqiyi.com") && iqiyiPageIDRe.MatchString(raw)
}

// fetchVideoTitleWithSelector 按官方平台配置的「标题选择器」从真实页面提取标题：
// selector 为正则（第 1 捕获组为标题），为空时回退默认 og:title/<title> 提取
func fetchVideoTitleWithSelector(raw, selector string) string {
	if selector != "" {
		re, err := regexp.Compile(selector)
		if err == nil {
			if body, err2 := fetchPageBytes(raw); err2 == nil {
				if m := re.FindStringSubmatch(string(body)); len(m) > 1 {
					t := strings.TrimSpace(m[1])
					if t != "" && !mojibakeRe.MatchString(t) {
						return t
					}
				}
			}
		}
	}
	return fetchVideoTitle(raw, "")
}

// ---- 编码感知抓取：解决搜狐等 GBK/GB2312 页面被当 UTF-8 读导致的乱码 ----

// detectCharset 从 HTTP Content-Type 与 HTML <meta charset> 探测页面字符集（返回小写名，如 gbk/utf-8）
func detectCharset(header string, body []byte) string {
	// 优先 HTTP 头
	if header != "" {
		lower := strings.ToLower(header)
		if i := strings.Index(lower, "charset="); i >= 0 {
			cs := strings.TrimSpace(lower[i+len("charset="):])
			if j := strings.IndexAny(cs, " \t;"); j >= 0 {
				cs = cs[:j]
			}
			cs = strings.Trim(cs, `"'`)
			if cs != "" && cs != "utf-8" && cs != "utf8" && cs != "us-ascii" && cs != "ascii" {
				return cs
			}
		}
	}
	// 其次 <meta charset>
	if m := regexp.MustCompile(`(?i)<meta[^>]+charset\s*=\s*["']?\s*([a-z0-9\-]+)`).FindSubmatch(body); len(m) > 1 {
		cs := strings.ToLower(string(m[1]))
		if cs != "utf-8" && cs != "utf8" && cs != "us-ascii" && cs != "ascii" {
			return cs
		}
	}
	return ""
}

// decodeBytesByCharset 按字符集把字节流转 UTF-8：优先系统 iconv 命令（Linux 一般自带），
// 无 iconv 时返回原样（调用方自行用 mojibakeRe 判定丢弃乱码标题）
func decodeBytesByCharset(b []byte, charset string) []byte {
	if len(b) == 0 || charset == "" {
		return b
	}
	cmd := exec.Command("iconv", "-f", charset, "-t", "UTF-8//IGNORE")
	cmd.Stdin = bytes.NewReader(b)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return b
	}
	return out
}

// fetchPageBytes 抓取页面原始字节（保持原始编码，不做转换）
func fetchPageBytes(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", siteFetchUA)
	if base, e := url.Parse(u); e == nil {
		req.Header.Set("Referer", base.Scheme+"://"+base.Host+"/")
	}
	resp, err := siteHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// fetchPageBytesDecoded 抓取页面并按探测到的字符集解码为 UTF-8（GBK/GB2312/Big5 页面不乱码）
func fetchPageBytesDecoded(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", siteFetchUA)
	if base, e := url.Parse(u); e == nil {
		req.Header.Set("Referer", base.Scheme+"://"+base.Host+"/")
	}
	resp, err := siteHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	cs := detectCharset(resp.Header.Get("Content-Type"), b)
	if cs != "" {
		return decodeBytesByCharset(b, cs), nil
	}
	// 无 charset 声明但字节不是合法 UTF-8 → 按 GBK 试解（搜狐旧页面常见）
	if !utf8.Valid(b) {
		return decodeBytesByCharset(b, "GBK"), nil
	}
	return b, nil
}

// parseTitleFromBytes 从（已解码的）页面字节提取标题（og:title 优先，其次 <title>）
func parseTitleFromBytes(b []byte) string {
	s := string(b)
	if m := ogTitleRe.FindStringSubmatch(s); len(m) > 1 {
		if t := strings.TrimSpace(m[1]); t != "" {
			return t
		}
	}
	if m := titleTagRe.FindStringSubmatch(s); len(m) > 1 {
		t := strings.TrimSpace(m[1])
		if i := strings.LastIndex(t, "_"); i > 0 {
			t = t[:i]
		}
		return strings.TrimSpace(t)
	}
	return ""
}

// ---- 可选增强抓取：无头 Chromium 渲染真实剧名（一次性探测 Node + 脚本可用性）----

var (
	nodeRenderOnce sync.Once
	nodeRenderOK   bool
	nodeRenderPath string // node 可执行文件绝对路径
	nodeRenderJS   string // fetch-title.js 绝对路径
)

// renderTitleScriptPath 定位 render-title/fetch-title.js（可执行文件旁目录）
func renderTitleScriptPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "render-title", "fetch-title.js")
}

// initNodeRender 探测 node 与脚本是否可用（仅探测一次）。缺 Node/脚本 → 标记不可用，调用方回退静态抓取。
func initNodeRender() {
	nodeRenderJS = renderTitleScriptPath()
	if nodeRenderJS == "" {
		return
	}
	if _, err := os.Stat(nodeRenderJS); err != nil {
		nodeRenderJS = ""
		return
	}
	nodeRenderPath = "node"
	if p, err := exec.LookPath("node"); err == nil && p != "" {
		nodeRenderPath = p
	} else {
		nodeRenderJS = ""
		return
	}
	// 脚本存在即视为"已启用"（依赖/Chromium 是否就绪交给真正调用时判失败容错）
	nodeRenderOK = true
}

// renderTitleViaNode 调用 Node 渲染取真实剧名。主程序在「一键映射」抓取时优先使用，
// 缺 Node / 脚本缺失 / 未装 Chromium / 执行超时 → 返回空，由调用方回退到静态抓取。
// 期待脚本 stdout 单行 JSON { "title": "...", "og_title": "...", "err": "" }。
func renderTitleViaNode(raw string) string {
	if raw == "" {
		return ""
	}
	nodeRenderOnce.Do(initNodeRender)
	if !nodeRenderOK || nodeRenderJS == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nodeRenderPath, nodeRenderJS, raw)
	cmd.Env = append(os.Environ(), "PLAYWRIGHT_BROWSERS_PATH=") // 用默认缓存目录
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return ""
	}
	if err != nil {
		return ""
	}
	var r struct {
		Title   string `json:"title"`
		OgTitle string `json:"og_title"`
		Err     string `json:"err"`
	}
	if json.Unmarshal(out, &r) != nil {
		return ""
	}
	if r.Err != "" {
		return ""
	}
	t := strings.TrimSpace(r.Title)
	if t == "" {
		t = strings.TrimSpace(r.OgTitle)
	}
	return t
}

var epCNRe = regexp.MustCompile(`第\s*([0-9]+|[一二三四五六七八九十百千]+)\s*(集|期|话)`)
var seasonCNRe = regexp.MustCompile(`第\s*([0-9]+|[一二三四五六七八九十百千]+)\s*(季|部|篇|卷|番)`)
var sxxexxRe = regexp.MustCompile(`(?i)S(\d+)\s*E(\d+)`)
var qRe = regexp.MustCompile(`(?i)第[一-九零一二三四五六七八九十百千0-9]+[季部篇卷番集期话]|S\d+E\d+|全集|完结|高清|蓝光|4K|1080P|720P`)
var cleanTagRe = regexp.MustCompile(`[（(]?(第[一-九零一二三四五六七八九十百千0-9]+[季部篇卷番集期话]|S\d+E\d+|全集|完结)[）)]?`)

// platformTagRe 内置 7 种官方平台（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP）的剧名标签与后缀清理规则，
// 用于从官方标题中提取真实剧名字段（如「【腾讯视频】 独剑九天 01」「独剑九天_爱奇艺」→ 剧名「独剑九天」）
var platformTagRe = regexp.MustCompile(`(?i)(【[^】]{1,20}】|〔[^〕]{1,20}〕|\[[^\]]{1,20}\])|[-—_·\s]*(腾讯视频|爱奇艺|优酷视频|优酷|芒果TV|芒果tv|哔哩哔哩|bilibili|搜狐视频|搜狐|PP视频|pptv|PPTV)\s*$`)

// epTailSpaceRe 剧名 + 空格/分隔符 + 数字（如「独剑九天 01」「独剑九天-1」）→ 末尾数字为集数
var epTailSpaceRe = regexp.MustCompile(`^(.+?)[\s·\-_]+(\d{1,4})$`)

// epTailAttachRe 剧名直接跟 2 位数字（如「独剑九天01」「独剑九天25」）→ 末尾数字为集数（限 2 位，避免误拆「流浪地球2」类续作剧名）
var epTailAttachRe = regexp.MustCompile(`^(.{3,}?)(\d{2})$`)

func cnToNum(s string) int {
	// 纯阿拉伯数字直接转换（"01"→1、"12"→12、"100"→100），避免逐字解析导致多位数错误
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	digits := map[rune]int{'零': 0, '一': 1, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	units := map[rune]int{'十': 10, '百': 100, '千': 1000}
	n, temp := 0, 0
	for _, ch := range s {
		if v, ok := digits[ch]; ok {
			temp = v
		} else if u, ok2 := units[ch]; ok2 {
			if temp == 0 && u == 10 {
				temp = 1
			}
			n += temp * u
			temp = 0
		}
	}
	n += temp
	return n
}

// siteSuffixRe 匹配末尾「分隔符+一段站点词」，供 trimSiteSuffix 逐段剥掉重复后缀
var siteSuffixRe = regexp.MustCompile(`[-—_·\s]+([^\-—_·\s]{1,14})$`)

// siteSuffixWords 官方 <title> 常见的站点/频道/剧集冗余词（末位段命中即剥掉）。
// 命中词本身不作为剧名（如「电视剧」「完整版」「高清完整版」「在线观看」「爱奇艺」「芒果TV」「腾讯视频」…）。
var siteSuffixWords = map[string]bool{
	"电视剧": true, "连续剧": true, "全集": true, "综艺": true, "动漫": true, "电影": true, "纪录片": true,
	"完整版": true, "高清完整版": true, "完整视频版": true, "抢先看": true, "正版": true, "高清": true,
	"在线观看": true, "在线播放": true, "免费观看": true, "高清视频在线观看": true,
	"爱奇艺": true, "腾讯视频": true, "芒果TV": true, "芒果tv": true, "天生青春": true, "优酷": true,
	"优酷视频": true, "哔哩哔哩": true, "bilibili": true, "搜狐视频": true, "PP视频": true,
	"电视剧频道": true, "其他": true, "海外": true, "片花": true, "预告片": true, "花絮": true,
	"音乐视频": true, "聚力视频": true, "pptv聚力视频": true, "原pptv聚力视频": true, "蓝光": true, "超清": true,
}

// trimSiteSuffix 从标题末尾按分隔符逐段剥掉「站点/频道冗余词」，保留真正剧名主体。
// 例：交锋_01_电视剧_完整版视频在线观看_腾讯视频 → 交锋_01；生逢其时-电视剧-完整版视频在线观看 → 生逢其时；
//
//	云游纪 (10)-其他-完整正版视频在线观看 → 云游纪 (10)；御廷谣 - 芒果TV-天生青春 → 御廷谣。
//
// 末端段整体命中站点词，或段内包含站点词且以站点词收尾（如「完整版视频在线观看」→ 含「在线观看」）→ 剥掉该段。
func trimSiteSuffix(s string) string {
	s = strings.TrimSpace(s)
	for {
		m := siteSuffixRe.FindStringSubmatch(s)
		if len(m) < 2 {
			break
		}
		seg := strings.TrimSpace(m[1])
		if seg == "" {
			break
		}
		lower := strings.ToLower(seg)
		hit := siteSuffixWords[lower]
		if !hit {
			for w := range siteSuffixWords {
				if strings.HasSuffix(lower, strings.ToLower(w)) {
					hit = true
					break
				}
			}
		}
		if hit {
			s = strings.TrimSpace(strings.TrimSuffix(s, m[0]))
			continue
		}
		break
	}
	return s
}

func parseVideoTitle(title string) VideoInfo {
	// 内置 7 种官方平台标签/后缀清理：先从原始标题提取真实剧名（如「【腾讯视频】 独剑九天 01」「独剑九天_爱奇艺」）
	title = platformTagRe.ReplaceAllString(strings.TrimSpace(title), "")
	title = strings.TrimSpace(title)
	// 清理官方 <title> 常见的站点/频道冗余后缀（如「交锋_01_电视剧_完整版视频在线观看」→ 保留「交锋」；
	// 「生逢其时-电视剧-完整版视频在线观看」「云游纪 (10)-其他-完整正版视频在线观看」「御廷谣 - 芒果TV-天生青春」）。
	// 规则：从末尾按分隔符(－-_ ·)逐段剥掉「站点频道词」，保留真正的剧名主体。
	title = trimSiteSuffix(title)
	vi := VideoInfo{Title: title}
	if m := seasonCNRe.FindStringSubmatch(title); len(m) > 1 {
		vi.SeasonNum = cnToNum(m[1])
	}
	if m := epCNRe.FindStringSubmatch(title); len(m) > 1 {
		vi.EpisodeNum = cnToNum(m[1])
		vi.Episode = m[0]
	}
	if m := sxxexxRe.FindStringSubmatch(title); len(m) > 2 {
		if vi.SeasonNum == 0 {
			vi.SeasonNum, _ = strconv.Atoi(m[1])
		}
		vi.EpisodeNum, _ = strconv.Atoi(m[2])
		vi.Episode = m[0]
	}
	base := cleanTagRe.ReplaceAllString(title, "")
	base = qRe.ReplaceAllString(base, "")
	// 去掉开头/结尾的【标签】/〔标签〕 类包围块（如【腾讯视频】）
	base = regexp.MustCompile(`(?:^|^[\s·])(?:【[^】]{1,16}】|〔[^〕]{1,16}〕|\[[^\]]{1,16}\])\s*`).ReplaceAllString(strings.TrimSpace(base), " ")
	base = regexp.MustCompile(`\s*(?:【[^】]{1,16}】|〔[^〕]{1,16}〕)$`).ReplaceAllString(base, "")
	base = strings.TrimSpace(base)
	// 剧名 + 末尾数字 → 拆分集数字段（官方标题直接包含当前剧集，如「独剑九天 01」「独剑九天01」）
	if vi.EpisodeNum == 0 {
		if m := epTailSpaceRe.FindStringSubmatch(base); len(m) > 2 {
			if n, err := strconv.Atoi(m[2]); err == nil && n > 0 && n <= 9999 {
				vi.EpisodeNum, vi.Episode = n, m[2]
				base = strings.TrimSpace(m[1])
			}
		} else if m := epTailAttachRe.FindStringSubmatch(base); len(m) > 2 {
			// 剧名直接跟数字：剧名部分不能以数字开头（排除纯数字串）
			if !regexp.MustCompile(`^\d`).MatchString(m[1]) {
				if n, err := strconv.Atoi(m[2]); err == nil && n > 0 && n <= 99 {
					vi.EpisodeNum, vi.Episode = n, m[2]
					base = strings.TrimSpace(m[1])
				}
			}
		}
	}
	vi.BaseTitle = strings.TrimSpace(base)
	return vi
}

// ============ 资源站搜索（AppleCMS / maccms） ============

type PlayItem struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ResourceVideo struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Pic      string     `json:"pic"`
	Remarks  string     `json:"remarks"`
	Site     string     `json:"site_name"`
	PlayFrom string     `json:"play_from"`
	URLs     []PlayItem `json:"urls"`
	FirstURL string     `json:"first_url"`
}

type maccmsItem struct {
	VodID       string `json:"vod_id"`
	VodName     string `json:"vod_name"`
	VodPic      string `json:"vod_pic"`
	VodRemarks  string `json:"vod_remarks"`
	VodPlayURL  string `json:"vod_play_url"`
	VodPlayFrom string `json:"vod_play_from"`
	Name        string `json:"name"`
	PlayURL     string `json:"play_url"`
	Pic         string `json:"pic"`
	Remarks     string `json:"remarks"`
	VodID2      string `json:"id"`
}

// maccmsFlex 宽松版采集接口响应：vod_id/id 兼容数字或字符串（部分站返回数字 id 导致标准解析失败）
type maccmsFlex struct {
	List []maccmsItemFlex `json:"list"`
	Data []maccmsItemFlex `json:"data"`
	Msg  string           `json:"msg"`
}

type maccmsItemFlex struct {
	VodID       json.Number `json:"vod_id"`
	VodName     string      `json:"vod_name"`
	VodPic      string      `json:"vod_pic"`
	VodRemarks  string      `json:"vod_remarks"`
	VodPlayURL  string      `json:"vod_play_url"`
	VodPlayFrom string      `json:"vod_play_from"`
	Name        string      `json:"name"`
	PlayURL     string      `json:"play_url"`
	Pic         string      `json:"pic"`
	Remarks     string      `json:"remarks"`
	VodID2      json.Number `json:"id"`
}

type maccmsResp struct {
	List []maccmsItem `json:"list"`
	Data []maccmsItem `json:"data"`
	Msg  string       `json:"msg"`
}

// siteHTTP 资源站专用客户端：短超时 + 关闭证书校验（多数采集站自签/http）+ 支持环境代理（HTTP(S)_PROXY）
var siteHTTP = &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	Proxy:           http.ProxyFromEnvironment,
}}

// siteConcurrency 资源站批量操作（搜索/检测）全局并发数，-sites-conc 可调，默认 8
var siteConcurrency = 8

// siteFetchUA 资源站/采集接口请求用的浏览器 UA（很多资源站有 UA/Referer 反爬，冷 UA 会被拒或返回非标准内容）
const siteFetchUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

func httpGetBody(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", siteFetchUA)
	// Referer 设为站点根域，避免被当爬虫（部分资源站校验 Referer 需与站同域）
	if base, e := url.Parse(u); e == nil {
		req.Header.Set("Referer", base.Scheme+"://"+base.Host+"/")
	}
	resp, err := siteHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func buildSearchURL(apiURL, kw string, pg, limit int) string {
	u, err := url.Parse(apiURL)
	if err != nil {
		return apiURL
	}
	q := u.Query()
	q.Set("ac", "videolist")
	q.Set("wd", kw)
	q.Set("pg", strconv.Itoa(pg))
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()
	return u.String()
}

func stripFragment(u string) string {
	if i := strings.Index(u, "#"); i >= 0 {
		return u[:i]
	}
	return u
}

// episodeNumOfPlayItem 从播放项名称提取集数：兼容「第01集/EP01/E1/01/独剑九天01/1」等官方与资源站不同表述
func episodeNumOfPlayItem(name string) int {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "http") {
		return 0
	}
	m := regexp.MustCompile(`第\s*0*(\d+)\s*[集期话]|EP?\s*0*(\d+)|E\s*0*(\d+)\s*$`).FindStringSubmatch(name)
	for _, g := range m[1:] {
		if n, err := strconv.Atoi(g); err == nil && n > 0 {
			return n
		}
	}
	// 整串为纯数字（含前导零，如 "01"、"1"）→ 视为集数
	if regexp.MustCompile(`^\d{1,4}$`).MatchString(name) {
		if n, err := strconv.Atoi(name); err == nil && n > 0 {
			return n
		}
	}
	// 中文数字「第X集」
	if m2 := regexp.MustCompile(`第\s*([一二三四五六七八九十百千]+)\s*[集期话]`).FindStringSubmatch(name); len(m2) > 1 {
		return cnToNum(m2[1])
	}
	// 末尾数字（如 "独剑九天01"）
	if m3 := regexp.MustCompile(`0*(\d{1,3})$`).FindStringSubmatch(name); len(m3) > 1 {
		if n, err := strconv.Atoi(m3[1]); err == nil && n > 0 && n <= 999 {
			return n
		}
	}
	return 0
}

// parsePlayURL 解析 vod_play_url（支持 $$$ 多线路、多行、name$url 集数格式）
func parsePlayURL(full string) []PlayItem {
	body := strings.ReplaceAll(strings.ReplaceAll(full, "\r\n", "\n"), "$$$", "\n")
	var items []PlayItem
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(line, "$") {
			if !seen[stripFragment(line)] {
				seen[stripFragment(line)] = true
				items = append(items, PlayItem{Name: "第1集", URL: stripFragment(line)})
			}
			continue
		}
		prev := ""
		for _, p := range strings.Split(line, "$") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if strings.HasPrefix(p, "http") {
				u := stripFragment(p)
				name := prev
				if name == "" {
					name = "第" + strconv.Itoa(len(items)+1) + "集"
				}
				if !seen[u] {
					seen[u] = true
					items = append(items, PlayItem{Name: name, URL: u})
				}
			} else {
				prev = p
			}
		}
	}
	return items
}

type searchOut struct {
	Videos   []ResourceVideo
	SiteOK   []string
	SiteFail []string
	Searched int
}

// maccms 响应若为 JSON 返回 json 解析，否则按 AppleCMS XML（at/xml/）解析
type xmlVideoItem struct {
	VodID       string `xml:"vod_id"`
	VodName     string `xml:"vod_name"`
	VodPic      string `xml:"vod_pic"`
	VodRemarks  string `xml:"vod_remarks"`
	VodPlayURL  string `xml:"vod_play_url"`
	VodPlayFrom string `xml:"vod_play_from"`
}

type xmlVideoList struct {
	Videos []xmlVideoItem `xml:"video"`
}

type xmlMaccmsResp struct {
	List xmlVideoList `xml:"list"`
}

// 西瓜等站点自定义 XML：<id>/<name>/<pic>/<note>/<dl><dd flag="...">
type xgVideoItem struct {
	ID   string `xml:"id"`
	Name string `xml:"name"`
	Pic  string `xml:"pic"`
	Note string `xml:"note"`
	DL   struct {
		DD []struct {
			Flag string `xml:"flag,attr"`
			URL  string `xml:",chardata"`
		} `xml:"dd"`
	} `xml:"dl"`
}

type xgMaccmsResp struct {
	List struct {
		Videos []xgVideoItem `xml:"video"`
	} `xml:"list"`
}

// parseStdXMLItems 尝试按标准 AppleCMS XML（vod_ 前缀）解析；字段无效返回 nil
func parseStdXMLItems(b []byte) []maccmsItem {
	var x xmlMaccmsResp
	if err := xml.Unmarshal(b, &x); err != nil || len(x.List.Videos) == 0 {
		return nil
	}
	var items []maccmsItem
	for _, v := range x.List.Videos {
		if v.VodName == "" && v.VodPlayURL == "" {
			continue // 非标准 XML（如西瓜 <id>/<name>），跳过
		}
		items = append(items, maccmsItem{
			VodID: v.VodID, VodName: v.VodName, VodPic: v.VodPic,
			VodRemarks: v.VodRemarks, VodPlayURL: v.VodPlayURL, VodPlayFrom: v.VodPlayFrom,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// parseXgXMLItems 尝试按西瓜等自定义 XML（<id>/<name>/<pic>/<dl><dd flag=..>）解析
func parseXgXMLItems(b []byte) []maccmsItem {
	var xg xgMaccmsResp
	if err := xml.Unmarshal(b, &xg); err != nil || len(xg.List.Videos) == 0 {
		return nil
	}
	var items []maccmsItem
	for _, v := range xg.List.Videos {
		playURL := ""
		playFrom := ""
		for _, dd := range v.DL.DD {
			if strings.TrimSpace(dd.URL) != "" {
				playURL = strings.TrimSpace(dd.URL)
				playFrom = dd.Flag
				break
			}
		}
		if v.Name == "" && playURL == "" {
			continue
		}
		items = append(items, maccmsItem{
			VodID: v.ID, VodName: v.Name, VodPic: v.Pic,
			VodRemarks: v.Note, VodPlayURL: playURL, VodPlayFrom: playFrom,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// searchSiteOne 在单个资源站搜索关键词，返回该站命中的视频列表。
// 兼容三种采集接口格式：maccms JSON、标准 AppleCMS XML（vod_ 前缀）、自定义 XML（id/name/pic/dl）。
func searchSiteOne(s Site, kw string) ([]ResourceVideo, error) {
	b, err := httpGetBody(buildSearchURL(s.APIURL, kw, 1, 20))
	if err != nil {
		return nil, err
	}
	items, err := parseMaccmsItems(b)
	if err != nil {
		return nil, err
	}
	vs := itemsToVideos(items, s.Name)
	return filterM3U8Only(vs, loadSites().OnlyM3U8), nil
}

// filterM3U8Only 资源站搜索返回过滤：开启「仅 m3u8」后，丢弃 mp4 等非 m3u8 播放地址；
// 某条视频没有任何 m3u8 地址则整条丢弃。仅过滤播放地址，不改变匹配/去广告逻辑。
func filterM3U8Only(vs []ResourceVideo, only bool) []ResourceVideo {
	if !only {
		return vs
	}
	var out []ResourceVideo
	for _, v := range vs {
		var urls []PlayItem
		for _, it := range v.URLs {
			if strings.Contains(strings.ToLower(it.URL), "m3u8") {
				urls = append(urls, it)
			}
		}
		if len(urls) == 0 {
			continue
		}
		v.URLs = urls
		v.FirstURL = urls[0].URL
		out = append(out, v)
	}
	return out
}

// parseMaccmsItems 解析采集接口响应为通用条目列表（JSON / 标准 XML / 自定义 XML）
func parseMaccmsItems(b []byte) ([]maccmsItem, error) {
	items := []maccmsItem{}
	// 1. 宽松 JSON：vod_id/id 兼容数字或字符串（多数站 id 为数字，标准 string 解析会失败）
	var flex maccmsFlex
	if err := json.Unmarshal(b, &flex); err == nil {
		for _, it := range append(append([]maccmsItemFlex{}, flex.List...), flex.Data...) {
			items = append(items, maccmsItem{
				VodID:       it.VodID.String(),
				VodName:     it.VodName,
				VodPic:      it.VodPic,
				VodRemarks:  it.VodRemarks,
				VodPlayURL:  it.VodPlayURL,
				VodPlayFrom: it.VodPlayFrom,
				Name:        it.Name,
				PlayURL:     it.PlayURL,
				Pic:         it.Pic,
				Remarks:     it.Remarks,
				VodID2:      it.VodID2.String(),
			})
		}
		if len(items) > 0 {
			return items, nil
		}
	}
	// 2. 标准 JSON（兼容既有逻辑）
	var r maccmsResp
	if err := json.Unmarshal(b, &r); err == nil && (len(r.List) > 0 || len(r.Data) > 0) {
		items = append(items, r.List...)
		items = append(items, r.Data...)
		return items, nil
	}
	if x := parseStdXMLItems(b); x != nil {
		return x, nil
	}
	if x := parseXgXMLItems(b); x != nil {
		return x, nil
	}
	return nil, fmt.Errorf("响应解析失败（非 JSON/XML）")
}

// itemsToVideos 通用条目 → ResourceVideo（过滤无播放地址项）
func itemsToVideos(items []maccmsItem, siteName string) []ResourceVideo {
	var vs []ResourceVideo
	for _, it := range items {
		playURL := it.VodPlayURL
		if playURL == "" {
			playURL = it.PlayURL
		}
		if playURL == "" {
			continue
		}
		urls := parsePlayURL(playURL)
		if len(urls) == 0 {
			continue
		}
		name := it.VodName
		if name == "" {
			name = it.Name
		}
		id := it.VodID
		if id == "" {
			id = it.VodID2
		}
		pic := it.VodPic
		if pic == "" {
			pic = it.Pic
		}
		remarks := it.VodRemarks
		if remarks == "" {
			remarks = it.Remarks
		}
		vs = append(vs, ResourceVideo{
			ID: id, Name: name, Pic: pic, Remarks: remarks,
			Site: siteName, PlayFrom: it.VodPlayFrom, URLs: urls, FirstURL: urls[0].URL,
		})
	}
	return vs
}

// probeSiteKeywords 资源站检测探针词：任一命中即判可用，避免单词语义/覆盖率导致的整批误判失效
var probeSiteKeywords = []string{"爱情", "庆余年", "电视剧"}

// probeSiteOne 综合探测单个资源站可用性：
//  1. 依次用多个探针词搜索，任一命中 → 可用；
//  2. 搜索词均无结果时，探测不带关键词的列表接口连通性，接口返回有效数据 → 可用（无命中）；
//  3. 仅当 HTTP 错误/超时/解析失败才判失效。
func probeSiteOne(s Site) (ms int64, msg string, vs []ResourceVideo, err error) {
	t0 := time.Now()
	var lastErr error
	for _, kw := range probeSiteKeywords {
		v, e := searchSiteOne(s, kw)
		if e == nil && len(v) > 0 {
			return time.Since(t0).Milliseconds(), "搜索命中「" + kw + "」", v, nil
		}
		if e != nil {
			lastErr = e
		}
	}
	// 搜索无结果：探测列表接口连通性
	if v, e := probeSiteList(s); e == nil && len(v) > 0 {
		return time.Since(t0).Milliseconds(), "接口正常（搜索无命中）", v, nil
	} else if e != nil {
		lastErr = e
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("搜索与列表均无数据")
	}
	return time.Since(t0).Milliseconds(), "", nil, lastErr
}

// probeSiteList 不带搜索词探测资源站接口（?ac=videolist&limit=5），验证接口连通与解析
func probeSiteList(s Site) ([]ResourceVideo, error) {
	u, err := url.Parse(s.APIURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("ac", "videolist")
	q.Set("limit", "5")
	u.RawQuery = q.Encode()
	b, err := httpGetBody(u.String())
	if err != nil {
		return nil, err
	}
	items, err := parseMaccmsItems(b)
	if err != nil {
		return nil, err
	}
	return itemsToVideos(items, s.Name), nil
}

// searchSites 在所有已启用站点搜索关键词，汇总返回（多站点并发，并发数 siteConcurrency）
func searchSites(cfg *SitesConfig, kw string, maxSites int) searchOut {
	sites := enabledSites(cfg)
	if maxSites > 0 && len(sites) > maxSites {
		sites = sites[:maxSites]
	}
	out := searchOut{Searched: len(sites)}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, siteConcurrency)
	for _, s := range sites {
		wg.Add(1)
		go func(s Site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			vs, err := searchSiteOne(s, kw)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.SiteFail = append(out.SiteFail, s.Name)
				return
			}
			out.Videos = append(out.Videos, vs...)
			out.SiteOK = append(out.SiteOK, s.Name)
		}(s)
	}
	wg.Wait()
	return out
}

// ============ 标题匹配 ============

// ===== 官替映射专区：解决官方剧名/剧集与资源站表述不同导致匹配失败 =====

// TitleMap 一条剧名映射：from（官方剧名/别名）→ to（资源站标准剧名）
type TitleMap struct {
	Platform string `json:"platform,omitempty"`
	From     string `json:"from"`
	To       string `json:"to"`
	Note     string `json:"note,omitempty"`
}

// TitleMaps 映射配置（title_maps.json，可执行文件旁持久化）
type TitleMaps struct {
	Version    string     `json:"version"`
	UpdateDate string     `json:"update_date"`
	NameMaps   []TitleMap `json:"name_maps"`
}

// OfficialPlatform 官方平台自动更新配置：
// 按优先级顺序匹配 URL（域名 + URL 匹配正则），用标题选择器从真实页面提取标题，
// 自动解析出「影视剧名 + 剧集集数」两个映射字段并映射到对应区域
type OfficialPlatform struct {
	Platform      string `json:"platform"`       // 平台名称（如：腾讯视频）
	Domain        string `json:"domain"`         // 域名（如：v.qq.com，包含匹配）
	URLRe         string `json:"url_re"`         // URL 匹配正则（可选，空=仅域名匹配）
	TitleSelector string `json:"title_selector"` // 标题选择器（可选，HTML 中提取标题的正则，第 1 捕获组为标题；空=默认 og:title/<title>）
	Priority      int    `json:"priority"`       // 优先级（数值越小越先匹配）
	Enabled       bool   `json:"enabled"`        // 是否启用
	Note          string `json:"note,omitempty"` // 备注
}

// OfficialPlatforms 官方平台配置（official_platforms.json，可执行文件旁持久化）
type OfficialPlatforms struct {
	Version    string             `json:"version"`
	UpdateDate string             `json:"update_date"`
	Platforms  []OfficialPlatform `json:"platforms"`
}

// defaultOfficialPlatforms 内置 7 种官方平台默认配置（可后台增删改，优先级控制顺序）
var defaultOfficialPlatforms = []OfficialPlatform{
	{Platform: "腾讯视频", Domain: "v.qq.com", Priority: 1, Enabled: true, Note: "内置默认"},
	{Platform: "腾讯视频", Domain: "m.v.qq.com", Priority: 2, Enabled: true, Note: "内置默认"},
	{Platform: "爱奇艺", Domain: "iqiyi.com", Priority: 3, Enabled: true, Note: "内置默认"},
	{Platform: "优酷", Domain: "youku.com", Priority: 4, Enabled: true, Note: "内置默认"},
	{Platform: "芒果TV", Domain: "mgtv.com", Priority: 5, Enabled: true, Note: "内置默认"},
	{Platform: "哔哩哔哩", Domain: "bilibili.com", Priority: 6, Enabled: true, Note: "内置默认"},
	{Platform: "搜狐视频", Domain: "sohu.com", Priority: 7, Enabled: true, Note: "内置默认"},
	{Platform: "PP视频", Domain: "pptv.com", Priority: 8, Enabled: true, Note: "内置默认"},
}

var platformCfgMu sync.Mutex

// builtinOfficialLink 内置官方平台一键映射链接（用户无需输入链接，点「⚡ 一键映射」即自动抓取+映射到专区）
type builtinOfficialLink struct {
	Key      string `json:"key"`
	Platform string `json:"platform"`
	Label    string `json:"label"`
	URL      string `json:"url"`
}

// builtinOfficialLinks 各大官方平台默认链接清单，用于自动更新官方「无脑映射」
// 这些链接在选择渲染（render-title/）启用时能解析出「真实剧名」；若未启用渲染则回退静态抓取（部分平台是 JS 壳）。
// 注：腾讯/爱奇艺/搜狐/PP 播页均为 JS 渲染壳或反爬（静态只有平台标题），一键映射首选无头渲染；
//
//	哔哩哔哩走公开 API；搜狐/PP 已换成当前可访问的具体视频页。
var builtinOfficialLinks = []builtinOfficialLink{
	{Key: "tencent", Platform: "腾讯视频", Label: "腾讯视频", URL: "https://v.qq.com/x/cover/mzc002001tyxcdm.html"},
	{Key: "iqiyi", Platform: "爱奇艺", Label: "爱奇艺", URL: "https://www.iqiyi.com/v_2bkbhy2gi2w.html"},
	{Key: "youku", Platform: "优酷", Label: "优酷", URL: "https://v.youku.com/v_show/id_XNTA3MjQ5NDY4NA="},
	{Key: "mgtv", Platform: "芒果TV", Label: "芒果TV", URL: "https://www.mgtv.com/b/781917.html"},
	{Key: "bilibili", Platform: "哔哩哔哩", Label: "哔哩哔哩", URL: "https://www.bilibili.com/video/BV1GJ411x7h7"},
	{Key: "sohu", Platform: "搜狐视频", Label: "搜狐视频", URL: "https://tv.sohu.com/v/dXMvMzU4MTE2MDYvMzE4MTYwMDUwLnNodG1s.html"},
	{Key: "pptv", Platform: "PP视频", Label: "PP视频", URL: "https://v.pptv.com/show/2zzxb9c9retOzDQ.html"},
}

func builtinLinkByKey(key string) *builtinOfficialLink {
	for i := range builtinOfficialLinks {
		if builtinOfficialLinks[i].Key == key {
			return &builtinOfficialLinks[i]
		}
	}
	return nil
}

// antiBotTitleWords 反爬/验证页标题黑名单：命中视为未抓到真实剧名，避免把脏标题映射进专区
var antiBotTitleWords = []string{"验证码", "安全验证", "访问异常", "访问出错", "请输入", "页面不存在", "页面丢失", "出错了", "出错啦", "暂时无法观看", "暂时无法播放", "403", "404", "error", "出错", "域名不正确"}

// siteGenericTitleWords 平台通用首页/壳标题特征词：命中且整体过短/无剧集痕迹时视为平台壳页，
// 避免把「搜狐视频-领先的综合视频网站,正版高清视频在线观看…」这类平台首页标题当作剧名映射进专区
var siteGenericTitleWords = []string{"综合视频网站", "正版高清视频在线观看", "高清视频在线观看", "海量正版", "在线视频网站", "原创视频上传", "全网视频搜索", "高清电影", "电视剧,电影", "视频在线观看", "客户端下载", "网络电视"}

// suspiciousTitle 判断标题是否为反爬/占位/乱码内容（过短、含验证词、GBK 乱码、平台壳页标题）
// platformDisplayNames 官方平台展示名集合（作为页面 <title> 壳标题出现时不视为真实剧名）
var platformDisplayNames = map[string]bool{
	"腾讯视频": true, "爱奇艺": true, "优酷": true, "优酷视频": true,
	"芒果TV": true, "芒果tv": true, "哔哩哔哩": true, "bilibili": true,
	"搜狐视频": true, "搜狐": true, "PP视频": true, "pptv": true, "PPTV": true,
	"聚力视频": true,
}

// isShellTitle 判断标题是否为「平台壳标题」：整体剥离后只剩平台名/平台通用词，无任何剧名痕迹。
// JS 渲染壳页（腾讯/爱奇艺等）静态抓取的 <title> 恒为平台名（如「腾讯视频」），这类标题绝不能被当作
// 剧名参与匹配或写入映射，否则不同视频页会因同一壳标题→同一映射得到「不同链接返回相同结果」。
func isShellTitle(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return true
	}
	// 整串只是平台展示名（大小写不敏感）→ 壳标题
	if platformDisplayNames[strings.ToLower(t)] {
		return true
	}
	// 「平台名 + 站点通用词」组合，且不含剧名痕迹（如「腾讯视频-高清视频在线观看」）
	hit := 0
	for _, w := range siteGenericTitleWords {
		if strings.Contains(t, w) {
			hit++
		}
	}
	if hit >= 2 && !epCNRe.MatchString(t) && !sxxexxRe.MatchString(t) && len([]rune(t)) < 10 {
		return true
	}
	return false
}

func suspiciousTitle(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" || len([]rune(t)) < 2 {
		return true
	}
	// GBK 被当 UTF-8 解码产生的替换字符（�）→ 乱码，未抓到有效标题
	if mojibakeRe.MatchString(t) {
		return true
	}
	for _, w := range antiBotTitleWords {
		if strings.Contains(t, w) {
			return true
		}
	}
	// 平台壳页标题（如搜狐首页「搜狐视频-领先的综合视频网站,正版高清视频在线观看…」）：
	// 命中 ≥2 个平台通用词，且标题不含任何剧集痕迹（第N集/S01E01/末尾数字）→ 视为平台首页/壳页标题，
	// 避免把平台首页标题当作剧名映射进专区。真实剧集标题（如「金色_01_电视剧_高清完整版视频在线观看_腾讯视频」）命中不足 2 词，不受影响。
	hit := 0
	for _, w := range siteGenericTitleWords {
		if strings.Contains(t, w) {
			hit++
		}
	}
	if hit >= 2 {
		hasEpisode := epCNRe.MatchString(t) || sxxexxRe.MatchString(t) || seasonCNRe.MatchString(t) ||
			regexp.MustCompile(`[_-]\s*\d{1,4}$`).MatchString(t) || len([]rune(t)) < 8
		if !hasEpisode {
			return true
		}
	}
	return false
}

// addOfficialOneClickMap 一键映射：把官方剧名写入映射表（from=官方剧名, to=官方剧名，note=官方一键自动映射），自动去重
func addOfficialOneClickMap(platform, officialName string) bool {
	if platform == "" || officialName == "" {
		return false
	}
	titleMapsMu.Lock()
	defer titleMapsMu.Unlock()
	cfg := loadTitleMaps()
	for _, m := range cfg.NameMaps {
		if m.Platform == platform && m.From == officialName && m.To == officialName {
			return false // 已存在
		}
	}
	cfg.NameMaps = append(cfg.NameMaps, TitleMap{Platform: platform, From: officialName, To: officialName, Note: "官方一键自动映射"})
	_ = saveTitleMaps(cfg)
	return true
}

func officialPlatformsPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "official_platforms.json")
	}
	return "official_platforms.json"
}

func loadOfficialPlatforms() *OfficialPlatforms {
	cfg := &OfficialPlatforms{Version: "1.0", UpdateDate: time.Now().Format("2006-01-02")}
	if b, err := os.ReadFile(officialPlatformsPath()); err == nil {
		c2 := &OfficialPlatforms{}
		if json.Unmarshal(b, c2) == nil && len(c2.Platforms) > 0 {
			return c2
		}
	}
	// 首次运行/文件缺失：落盘内置 7 平台默认配置
	cfg.Platforms = append([]OfficialPlatform(nil), defaultOfficialPlatforms...)
	_ = saveOfficialPlatforms(cfg)
	return cfg
}

func saveOfficialPlatforms(c *OfficialPlatforms) error {
	c.UpdateDate = time.Now().Format("2006-01-02")
	b, _ := json.MarshalIndent(c, "", "    ")
	return os.WriteFile(officialPlatformsPath(), b, 0o644)
}

// matchOfficialPlatform 按优先级顺序匹配官方平台：域名包含 + URL 正则（可选）
func matchOfficialPlatform(raw string) *OfficialPlatform {
	host := ""
	if u, err := url.Parse(raw); err == nil {
		host = strings.ToLower(u.Host)
	}
	platformCfgMu.Lock()
	cfg := loadOfficialPlatforms()
	platformCfgMu.Unlock()
	list := append([]OfficialPlatform(nil), cfg.Platforms...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Priority != list[j].Priority {
			return list[i].Priority < list[j].Priority
		}
		return list[i].Platform < list[j].Platform
	})
	for i := range list {
		p := &list[i]
		if !p.Enabled || p.Domain == "" {
			continue
		}
		if !strings.Contains(host, strings.ToLower(p.Domain)) {
			continue
		}
		if p.URLRe != "" {
			re, err := regexp.Compile(p.URLRe)
			if err != nil || !re.MatchString(raw) {
				continue
			}
		}
		return p
	}
	return nil
}

var titleMapsMu sync.Mutex

// defaultTitleMaps 内置默认官替映射：7 种官方平台（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP）的
// 剧名/集数字段提取规则已内置在代码中（platformTagRe 平台标签后缀清理 + parseVideoTitle 剧名集数拆分 +
// episodeNumOfPlayItem 集数表述兼容），此处为 title_maps.json 首次创建时的示例映射（用户可改/删）
var defaultTitleMaps = []TitleMap{
	{Platform: "腾讯视频", From: "独剑九天", To: "独剑九天", Note: "内置示例：官方剧名→资源站标准剧名"},
}

func titleMapsPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "title_maps.json")
	}
	return "title_maps.json"
}

func loadTitleMaps() *TitleMaps {
	cfg := &TitleMaps{Version: "1.0", UpdateDate: time.Now().Format("2006-01-02")}
	if b, err := os.ReadFile(titleMapsPath()); err == nil {
		c2 := &TitleMaps{}
		if json.Unmarshal(b, c2) == nil && len(c2.NameMaps) > 0 {
			// 清洗历史脏映射：删除「壳标题」作键的映射（如某次把「腾讯视频」自动学成某剧名），
			// 避免不同视频页因同一壳标题→同一映射得到「不同链接返回相同结果」，并避免后台继续展示失效映射。
			orig := len(c2.NameMaps)
			clean := c2.NameMaps[:0]
			for _, m := range c2.NameMaps {
				if !isShellTitle(m.From) && !isShellTitle(m.To) {
					clean = append(clean, m)
				}
			}
			c2.NameMaps = clean
			if len(clean) != orig {
				_ = saveTitleMaps(c2)
			}
			if len(c2.NameMaps) > 0 {
				return c2
			}
		}
	}
	// 首次运行/文件缺失：落盘内置默认映射
	cfg.NameMaps = append([]TitleMap(nil), defaultTitleMaps...)
	_ = saveTitleMaps(cfg)
	return cfg
}

func saveTitleMaps(c *TitleMaps) error {
	c.UpdateDate = time.Now().Format("2006-01-02")
	b, _ := json.MarshalIndent(c, "", "    ")
	return os.WriteFile(titleMapsPath(), b, 0o644)
}

// autoLearnTitleMap 官替匹配成功后自动学习映射：官方剧名 → 资源站标准剧名，持久化到 title_maps.json。
// 无需前端一个一个添加：只要官替成功命中资源站、且资源站剧名与官方剧名表述不同（非同名），就自动生成一条映射，
// 映射被 titleMapCandidates 双向往回展开，下次同类剧名直接命中。返回生成的「官方剧名 → 资源站标准剧名」，已存在则返回空。
func autoLearnTitleMap(platform string, vi VideoInfo, matched ResourceVideo) string {
	f := strings.TrimSpace(vi.BaseTitle)
	rvi := parseVideoTitle(matched.Name)
	t := strings.TrimSpace(rvi.BaseTitle)
	// 脏映射防护：官方剧名解析为空、为平台壳标题、或过于可疑（反爬/乱码）时绝不自动学习，
	// 否则会把「腾讯视频」这类壳标题学成某剧名，导致不同视频页匹配到同一资源（不同链接返回相同结果）。
	if f == "" || t == "" || f == t {
		return ""
	}
	if isShellTitle(f) || isShellTitle(t) || suspiciousTitle(f) {
		return ""
	}
	// 资源站剧名可能是多条拼接（如「独剑九天|HD|国语」），只取首个分隔后的基础剧名
	if i := strings.IndexAny(t, "|｜/／ "); i > 0 {
		t = strings.TrimSpace(t[:i])
		if t == "" || t == f {
			return ""
		}
	}
	titleMapsMu.Lock()
	defer titleMapsMu.Unlock()
	cfg := loadTitleMaps()
	for _, m := range cfg.NameMaps {
		if m.From == f && m.To == t {
			return "" // 已存在
		}
	}
	cfg.NameMaps = append(cfg.NameMaps, TitleMap{Platform: platform, From: f, To: t, Note: "自动学习（官替解析成功自动生成）"})
	_ = saveTitleMaps(cfg)
	return f + " → " + t
}

// titleMapCandidates 返回名称 + 所有映射变体（官方↔资源站双向），用于匹配时提高命中
func titleMapCandidates(name string) []string {
	seen := map[string]bool{}
	out := []string{name}
	seen[name] = true
	titleMapsMu.Lock()
	cfg := loadTitleMaps()
	titleMapsMu.Unlock()
	for _, m := range cfg.NameMaps {
		// 跳过「壳标题」键的脏映射（如某次把「腾讯视频」自动学成某剧名的映射），
		// 否则不同视频页会因同一壳标题→同一映射得到「不同链接返回相同结果」。
		if isShellTitle(m.From) || isShellTitle(m.To) {
			continue
		}
		for _, cand := range []string{m.From, m.To} {
			if cand == "" {
				continue
			}
			if strings.Contains(name, cand) || strings.Contains(cand, name) || name == cand {
				other := m.To
				if cand == m.To {
					other = m.From
				}
				if other != "" && !seen[other] {
					seen[other] = true
					out = append(out, other)
				}
			}
		}
	}
	return out
}

func compactTitle(s string) string {
	s = strings.ToLower(s)
	return regexp.MustCompile(`[\s·,，。.:：!！?？\-_/\\"'|【】\[\]()（）]`).ReplaceAllString(s, "")
}

func titleSim(a, b string) float64 {
	ca, cb := compactTitle(a), compactTitle(b)
	if ca == "" || cb == "" {
		return 0
	}
	if strings.Contains(ca, cb) || strings.Contains(cb, ca) {
		sh, lg := len(ca), len(cb)
		if sh > lg {
			sh, lg = lg, sh
		}
		return 60 + float64(sh)/float64(lg)*40
	}
	ra := map[rune]bool{}
	for _, c := range []rune(ca) {
		ra[c] = true
	}
	rc := map[rune]bool{}
	for _, c := range []rune(cb) {
		rc[c] = true
	}
	inter, union := 0, 0
	for c := range rc {
		if ra[c] {
			inter++
		}
		if ra[c] || rc[c] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union) * 100 * 0.5
}

type matchItem struct {
	Video ResourceVideo
	Score float64
}

func findBestMatch(vi VideoInfo, videos []ResourceVideo) (matchItem, bool) {
	best := matchItem{}
	ok := false
	// 官方剧名的映射变体（含映射表双向展开），提高不同表述的命中
	viNames := titleMapCandidates(vi.BaseTitle)
	for _, v := range videos {
		cand := parseVideoTitle(v.Name)
		// 资源站剧名同样做映射展开，取最高相似度
		s := 0.0
		for _, a := range viNames {
			for _, b := range titleMapCandidates(cand.BaseTitle) {
				if sc := titleSim(a, b); sc > s {
					s = sc
				}
			}
		}
		score := s * 0.8
		epMatch := false
		if vi.EpisodeNum > 0 {
			if cand.EpisodeNum > 0 && vi.EpisodeNum == cand.EpisodeNum {
				epMatch = true
			}
			for _, it := range v.URLs {
				if episodeNumOfPlayItem(it.Name) == vi.EpisodeNum {
					epMatch = true
					break
				}
			}
		}
		if epMatch {
			score += 20
		}
		// 季数不一致扣分
		if vi.SeasonNum > 0 && cand.SeasonNum > 0 && vi.SeasonNum != cand.SeasonNum {
			score -= 30
		}
		if score > best.Score {
			best = matchItem{Video: v, Score: score}
			ok = true
		}
	}
	return best, ok
}

// m3u8Reachable 快速探测 m3u8 是否可达：用 Range 请求头只取 1 字节，源站返回 2xx=可用，4xx=死链/已过期。
// 短超时；网络异常（超时/被墙）按「不可用」处理，由调用方决定是否换线路。
func m3u8Reachable(u string) bool {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", UserAgent)
	if ru, err2 := url.Parse(u); err2 == nil {
		req.Header.Set("Referer", ru.Scheme+"://"+ru.Host)
	}
	cl := &http.Client{Timeout: 8 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// pickReachablePlayURL 选一个「可达」的播放地址（死链自动切换）：
// 优先级=当前命中视频的记录（先目标集数、再任意）、其它匹配视频的同类记录。全部失效则回退首个候选（与原行为一致，不引入回归）。
func pickReachablePlayURL(best ResourceVideo, others []ResourceVideo, ep int) string {
	var cand, first string
	for i, it := range best.URLs {
		if first == "" {
			first = it.URL
		}
		if ep > 0 && episodeNumOfPlayItem(it.Name) == ep {
			if cand == "" {
				cand = it.URL
			}
			if i < 12 { // 同视频内有限探测，避免请求过多
				if m3u8Reachable(it.URL) {
					return it.URL
				}
			}
		}
	}
	if cand == "" {
		cand = first
	}
	if cand != "" && m3u8Reachable(cand) {
		return cand
	}
	// 主视频全部失效 → 尝试其它搜索命中的视频（其它站点/线路），最多探测 4 条
	tried := 0
	for _, v := range others {
		for _, it := range v.URLs {
			if ep > 0 && episodeNumOfPlayItem(it.Name) != ep {
				continue
			}
			if tried >= 4 {
				return cand
			}
			tried++
			if m3u8Reachable(it.URL) {
				return it.URL
			}
		}
	}
	return cand
}

func pickEpisodeURL(v ResourceVideo, ep int) (string, string) {
	if ep > 0 {
		for _, it := range v.URLs {
			if episodeNumOfPlayItem(it.Name) == ep {
				return it.URL, it.Name
			}
		}
	}
	if len(v.URLs) > 0 {
		return v.URLs[0].URL, v.URLs[0].Name
	}
	return "", ""
}

// ============ 官替返回结构 ============

type replaceStep struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type ReplaceResult struct {
	Success        bool          `json:"success"`
	Message        string        `json:"message,omitempty"`
	Channel        string        `json:"channel"`
	Platform       string        `json:"platform,omitempty"`
	OriginalURL    string        `json:"original_url,omitempty"`
	VideoTitle     string        `json:"video_title,omitempty"`
	BaseTitle      string        `json:"base_title,omitempty"`
	EpisodeNum     int           `json:"episode_num,omitempty"`
	Episode        string        `json:"episode,omitempty"`
	MatchScore     float64       `json:"match_score,omitempty"`
	Site           string        `json:"site,omitempty"`
	VideoName      string        `json:"video_name,omitempty"`
	VideoPic       string        `json:"video_pic,omitempty"`
	VideoRemarks   string        `json:"video_remarks,omitempty"`
	M3U8URL        string        `json:"m3u8_url,omitempty"`
	ADSkipURL      string        `json:"ad_skip_url,omitempty"`
	PlayURL        string        `json:"play_url,omitempty"`     // 内置播放地址（= ad_skip_url，经 /api/play 增强去广告+代理）
	ExternalURL    string        `json:"external_url,omitempty"` // 外置播放页（/player?url=…，基于请求 Host 动态拼接）
	SkipKey        string        `json:"skip_key,omitempty"`     // 该视频的区间匹配标识（剧名 第N集）
	SkipRanges     []SkipRange   `json:"skip_ranges,omitempty"`  // 该视频应跳过的非正片区间（含全局区间）
	UsedKeyword    string        `json:"used_keyword,omitempty"`
	AutoLearnMap   string        `json:"auto_learn_map,omitempty"` // 本次官替成功自动生成的映射「官方剧名 → 资源站标准剧名」
	AutoLearnCount int           `json:"auto_learn_count"`         // 本次自动生成映射累计条数
	SearchKeywords []string      `json:"search_keywords,omitempty"`
	SearchedSites  int           `json:"searched_sites"`
	SiteOK         []string      `json:"site_ok,omitempty"`
	SiteFail       []string      `json:"site_fail,omitempty"`
	Steps          []replaceStep `json:"steps,omitempty"`
	TotalMS        float64       `json:"total_ms"`
}

// replaceOne 官替主流程
func replaceOne(r *http.Request, raw string) ReplaceResult {
	res := ReplaceResult{Channel: "official_replace"}
	t0 := time.Now()

	if raw == "" {
		res.Message = "缺少 url 参数"
		return res
	}
	platform := detectPlatform(raw)
	if platform == "" {
		res.Message = "不支持的视频平台"
		res.Steps = []replaceStep{{"detect", "识别视频平台", "fail", "域名未匹配到支持的平台"}}
		res.TotalMS = time.Since(t0).Seconds() * 1000
		return res
	}
	res.Platform = platform
	res.OriginalURL = raw
	steps := []replaceStep{{"detect", "识别视频平台", "ok", "识别为 " + platform}}

	vidHint := ""
	if m := tencentVidRe.FindStringSubmatch(raw); len(m) > 1 {
		vidHint = m[1]
	}
	title := fetchVideoTitle(raw, vidHint)
	if title == "" || isShellTitle(title) {
		res.Message = "无法获取视频信息"
		steps = append(steps, replaceStep{"fetch_meta", "获取官方页面信息", "fail", "未能从页面/Meta获取标题"})
		res.Steps = steps
		res.TotalMS = time.Since(t0).Seconds() * 1000
		return res
	}
	vi := parseVideoTitle(title)
	res.VideoTitle = title
	res.BaseTitle = vi.BaseTitle
	res.EpisodeNum = vi.EpisodeNum
	res.Episode = vi.Episode
	steps = append(steps, replaceStep{"fetch_meta", "获取官方页面信息", "ok",
		fmt.Sprintf("title=%s · base=%s · 第%d集", title, vi.BaseTitle, vi.EpisodeNum)})

	// 生成搜索关键词：官方剧名 + 映射变体（兼容资源站不同表述），优先「基础剧名+集数」
	kws := []string{}
	seenKW := map[string]bool{}
	addKW := func(k string) {
		k = strings.TrimSpace(k)
		if k != "" && !seenKW[k] {
			seenKW[k] = true
			kws = append(kws, k)
		}
	}
	for _, n := range titleMapCandidates(vi.BaseTitle) {
		if vi.EpisodeNum > 0 {
			addKW(n + " 第" + strconv.Itoa(vi.EpisodeNum) + "集")
			addKW(n + " " + strconv.Itoa(vi.EpisodeNum))
		}
		addKW(n)
	}
	res.SearchKeywords = kws

	cfg := loadSites()
	var allVideos []ResourceVideo
	used := ""
	seenOK, seenFail := map[string]bool{}, map[string]bool{}
	best := matchItem{}
	matched := false

	for _, kw := range kws {
		if kw == "" {
			continue
		}
		sr := searchSites(cfg, kw, 8)
		res.SearchedSites = sr.Searched
		for _, s := range sr.SiteOK {
			if !seenOK[s] {
				seenOK[s] = true
				res.SiteOK = append(res.SiteOK, s)
			}
		}
		for _, s := range sr.SiteFail {
			if !seenFail[s] {
				seenFail[s] = true
				res.SiteFail = append(res.SiteFail, s)
			}
		}
		allVideos = append(allVideos, sr.Videos...)
		if len(sr.Videos) > 0 {
			if m, ok := findBestMatch(vi, sr.Videos); ok && m.Score >= 65 {
				best, matched, used = m, true, kw
				break
			}
		}
	}
	if !matched && len(allVideos) > 0 {
		if m, ok := findBestMatch(vi, allVideos); ok && m.Score >= 65 {
			best, matched = m, true
			used = kws[0]
		}
	}
	if !matched {
		res.Message = "未找到匹配度足够的资源（可能未启用该资源站，请在后台启用后再试）"
		steps = append(steps, replaceStep{"match", "资源站匹配", "fail", "所有候选分数低于阈值 65"})
		res.Steps = steps
		res.TotalMS = time.Since(t0).Seconds() * 1000
		return res
	}

	res.VideoName = best.Video.Name
	res.VideoPic = best.Video.Pic
	res.VideoRemarks = best.Video.Remarks
	res.Site = best.Video.Site
	res.MatchScore = best.Score
	res.UsedKeyword = used
	// 自动学习映射：官替成功后自动生成「官方剧名 → 资源站标准剧名」并保存，无需前端一个一个添加
	if learned := autoLearnTitleMap(platform, vi, best.Video); learned != "" {
		res.AutoLearnMap = learned
		learnCount := 0
		for _, m := range loadTitleMaps().NameMaps {
			if strings.Contains(m.Note, "自动学习") {
				learnCount++
			}
		}
		res.AutoLearnCount = learnCount
	}
	steps = append(steps, replaceStep{"search", "资源站搜索", "ok",
		fmt.Sprintf("命中站点 %s · score=%.1f · 关键词 %s", best.Video.Site, best.Score, used)})

	// 死链自动切换：选择「可达」的播放地址（当前线路源站已删/过期 404 时，自动探测并切换到该资源其它线路或其它命中站点；全部失效才回退原地址）
	m3u8 := pickReachablePlayURL(best.Video, allVideos, vi.EpisodeNum)
	if m3u8 == "" {
		res.Message = "匹配到的视频没有可用播放地址"
		res.Steps = steps
		res.TotalMS = time.Since(t0).Seconds() * 1000
		return res
	}
	if !strings.HasPrefix(m3u8, "http") {
		// 相对地址兜底：用官方页 host 补全
		if u, err := url.Parse(raw); err == nil {
			m3u8 = u.Scheme + "://" + u.Host + "/" + strings.TrimLeft(m3u8, "/")
		}
	}
	res.M3U8URL = m3u8
	// 统一走播放代理 /api/play：服务端内置增强去广告（平台广告域+边界短簇+中插短簇）+ 分片代理防卡顿，
	// 不再单独请求 /api/clean（避免二次过滤与源站分片直连）
	res.ADSkipURL = playURL(r, "/api/play", url.Values{"url": {m3u8}})
	// 内置播放：直接播放去广告直链；外置播放：独立播放页（跨域由 withCORS 覆盖，URL 基于请求 Host 动态拼接不硬编码）
	res.PlayURL = res.ADSkipURL
	// 该视频的区间匹配标识 + 应跳过的非正片区间（含全局区间），播放器据此自动跳过内嵌/占位广告
	res.SkipKey = vi.BaseTitle + " 第" + strconv.Itoa(vi.EpisodeNum) + "集"
	res.SkipRanges = rangesForVideo(res.SkipKey)
	res.ExternalURL = playURL(r, "/player", url.Values{"url": {m3u8}, "title": {vi.BaseTitle}, "key": {res.SkipKey}})
	steps = append(steps, replaceStep{"output", "组装输出", "ok", "ad_skip_url 已生成（经 /api/play 增强去广告+代理）；内置/外置播放均可，非正片区间自动跳过"})

	res.Success = true
	res.Message = "官替解析成功，请播放 ad_skip_url（无广告）"
	res.Steps = steps
	res.TotalMS = time.Since(t0).Seconds() * 1000
	return res
}

// ============ 官替 / 资源站 HTTP 处理器 ============

func handleReplace(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	res := replaceOne(r, raw)
	recordCall("/api/replace", raw, res.Success, res.TotalMS, res.Message)
	res.Steps = nil // 减小返回体积
	writeJSON(w, res)
}

func handleSitesList(w http.ResponseWriter, r *http.Request) {
	cfg := loadSites()
	showAll := r.URL.Query().Get("show") == "all"
	sites := cfg.Sites
	if !showAll {
		// 参考 PHP getAllSites(false)：默认不显示失败的（status != active 的暂停/失效站）
		kept := []Site{}
		for _, s := range sites {
			if s.Status == "active" {
				kept = append(kept, s)
			}
		}
		sites = kept
	}
	stats := map[string]int{"total": len(cfg.Sites), "shown": len(sites), "active": 0, "failed": 0, "enabled": 0}
	for _, s := range cfg.Sites {
		if s.Status == "active" {
			stats["active"]++
		} else {
			stats["failed"]++
		}
		if s.Enabled {
			stats["enabled"]++
		}
	}
	recordCall("/api/sites", "", true, 0, fmt.Sprintf("站点 %d 个（显示 %d，隐藏失效 %d）", stats["total"], len(sites), stats["failed"]))
	writeJSON(w, map[string]interface{}{
		"version": cfg.Version, "update_date": cfg.UpdateDate,
		"sites": sites, "stats": stats, "hiding_failed": !showAll,
		"only_m3u8": cfg.OnlyM3U8,
	})
}

// handleSiteM3U8 GET/POST /api/sites/m3u8：读取/设置「资源站搜索仅返回 m3u8 播放地址」（写需登录）
func handleSiteM3U8(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if !isAuthed(r) {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]interface{}{"success": false, "message": "请先登录"})
			return
		}
		var in struct {
			OnlyM3U8 bool `json:"only_m3u8"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "参数错误"})
			return
		}
		sitesMu.Lock()
		cfg := loadSites()
		cfg.OnlyM3U8 = in.OnlyM3U8
		err := saveSites(cfg)
		sitesMu.Unlock()
		if err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
			return
		}
		recordCall("/api/sites/m3u8", fmt.Sprintf("%v", in.OnlyM3U8), true, 0, "仅m3u8 开关已保存")
		writeJSON(w, map[string]interface{}{"success": true, "only_m3u8": in.OnlyM3U8})
		return
	}
	writeJSON(w, map[string]interface{}{"success": true, "only_m3u8": loadSites().OnlyM3U8})
}

// ================================
// 资源站一键自动导入（从链接自动抓取 / 粘贴文本解析，自动批量添加）
// ================================

var importURLRe = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
var importTagRe = regexp.MustCompile(`<[^>]{1,300}>`)
var importSeqRe = regexp.MustCompile(`(?m)^\s*(?:\d+|[一二三四五六七八九十]+)\s*[、.．\)\)】\s:-]\s*`)
var importKwRe = regexp.MustCompile(`采集|接口|官网|备注|接口号|playurl|url|：|:`)

// isImportAPIURL 判断某 URL 更像「采集接口」而非「官网」
func isImportAPIURL(u string) bool {
	lu := strings.ToLower(u)
	return strings.Contains(lu, "provide/vod") || strings.Contains(lu, "inc/api") || strings.Contains(lu, "api.php")
}

func importHost(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimSpace(u)
}

// importGroupToSkip 解析到「停更/移除以下」分组标题后，其后站点行不再导入（避免把死链/待移除站加进来）
func importGroupToSkip(line string) (skip bool, isHeader bool) {
	noURL := !importURLRe.MatchString(line)
	if !noURL {
		return false, false
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "移除"), strings.Contains(lower, "停更"), strings.Contains(lower, "丢弃"):
		return true, true
	case strings.Contains(lower, "推荐"), strings.Contains(lower, "多合一"),
		strings.Contains(lower, "大水印"), strings.Contains(lower, "同类"):
		return false, true
	}
	return false, false
}

// extractResourceSitesFromContent 解析文本/HTML 中的资源站
// 支持 kdocs 表格文本：「序号|名：官网 | 采集：接口 | 备注」以及平铺「名：官网 采集：接口 备注」。
func extractResourceSitesFromContent(content string) []Site {
	text := importTagRe.ReplaceAllString(content, " ") // 若为网页则去掉标签
	out := []Site{}
	seen := map[string]bool{}
	skip := false

	trimClean := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.Trim(s, " -—–·|:：,，。")
		return strings.TrimSpace(s)
	}
	token := func(s string) []string {
		// 去掉 kdocs 竖线分隔（｜/|）后再按关键词/空白分词，避免「|」成为首段污染名称
		s = strings.NewReplacer("|", " ", "｜", " ").Replace(s)
		s = importKwRe.ReplaceAllString(s, " ")
		return strings.Fields(s)
	}
	hostSeen := map[string]bool{}
	add := func(name, siteURL, apiURL, note string) {
		name = trimClean(name)
		apiURL = strings.TrimSpace(apiURL)
		if apiURL == "" {
			return
		}
		if name == "" {
			name = importHost(apiURL)
			if name == "" {
				name = "采集站"
			}
		}
		name = trimClean(name)
		if len([]rune(name)) > 24 {
			name = string([]rune(name)[:24])
		}
		if len([]rune(note)) > 80 {
			note = string([]rune(note)[:80])
		}
		note = trimClean(note)
		key := strings.ToLower(name)
		if seen[key] {
			key += strings.ToLower(apiURL)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		host := importHost(apiURL)
		if host != "" {
			if hostSeen[host] {
				// 官网/接口同源（可能是同一站多个线路），允许同名不同接口，但同源同名跳过
				key2 := strings.ToLower(name) + host
				if seen[key2] {
					return
				}
				seen[key2] = true
			} else {
				hostSeen[host] = true
			}
		}
		out = append(out, Site{Name: name, SiteURL: trimClean(siteURL), APIURL: apiURL,
			Type: "maccms", Status: "active", Enabled: true, Note: note, Priority: 60})
	}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		line = strings.TrimSpace(importSeqRe.ReplaceAllString(line, " "))
		if line == "" {
			continue
		}
		// 分组标题（无 URL 的短行）：识别「移除/停更」分组则其后站点跳过
		if s, isHeader := importGroupToSkip(line); isHeader {
			skip = s
			continue
		}
		if skip {
			continue
		}
		urls := importURLRe.FindAllString(line, -1)
		if len(urls) == 0 {
			continue
		}
		api, site := "", ""
		for _, u := range urls {
			if isImportAPIURL(u) {
				if api == "" {
					api = u
				}
			} else if site == "" {
				site = u
			}
		}
		if api == "" {
			// 只有一个 URL 或全部不像接口：把第一个当接口，官网留空
			api = urls[0]
			if len(urls) > 1 {
				site = urls[1]
			}
		}
		tmp := line
		for _, u := range urls {
			tmp = strings.ReplaceAll(tmp, u, " ")
		}
		parts := token(tmp)
		name := ""
		note := ""
		if len(parts) > 0 {
			name = parts[0]
		}
		if len(parts) > 1 {
			note = parts[len(parts)-1]
		}
		add(name, site, api, note)
	}
	return out
}

// handleSiteImport POST /api/sites/import：一键自动导入资源站。
// body: { "url": "https://… 含资源站列表的链接", "text": "粘贴的文本（二选一；url 为空时用 text）" }
// 自动抓取→解析→追加；已存在同名/同接口则跳过。返回新增/跳过/解析数。
func handleSiteImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL  string `json:"url"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数错误: " + err.Error()})
		return
	}
	content := strings.TrimSpace(in.Text)
	src := "text"
	if content == "" && in.URL != "" {
		src = "url:" + in.URL
		body, err := fetchImportBytes(in.URL)
		if err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "抓取链接失败: " + err.Error()})
			return
		}
		content = string(body)
	}
	if strings.TrimSpace(content) == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "链接或文本为空"})
		return
	}
	found := extractResourceSitesFromContent(content)
	added, skipped := 0, 0
	sitesMu.Lock()
	cfg := loadSites()
	byName := map[string]*Site{}
	for i := range cfg.Sites {
		byName[strings.ToLower(cfg.Sites[i].Name)] = &cfg.Sites[i]
	}
	for _, s := range found {
		key := strings.ToLower(s.Name)
		if ex, ok := byName[key]; ok {
			// 同名已存在：仅当原接口为空时补齐接口（不覆盖用户已有配置）
			if strings.TrimSpace(ex.APIURL) == "" {
				ex.APIURL = s.APIURL
				ex.SiteURL = s.SiteURL
				ex.Status = "active"
				ex.Enabled = true
				added++
			} else {
				skipped++
			}
			continue
		}
		cfg.Sites = append(cfg.Sites, s)
		byName[strings.ToLower(s.Name)] = &cfg.Sites[len(cfg.Sites)-1]
		added++
	}
	err := saveSites(cfg)
	sitesMu.Unlock()
	if err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	msg := fmt.Sprintf("解析 %d 条，新增 %d，跳过 %d（已存在同名/同接口）", len(found), added, skipped)
	recordCall("/api/sites/import", src, added > 0, 0, msg)
	writeJSON(w, map[string]interface{}{"success": true, "message": msg, "parsed": len(found), "added": added, "skipped": skipped})
}

// fetchImportBytes 抓取资源站列表源（浏览器 UA + 编码兜底）。返回原始字节（HTML 则交给解析器去标签）
func fetchImportBytes(u string) ([]byte, error) {
	if !strings.HasPrefix(u, "http") {
		u = "https://" + u
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", siteFetchUA)
	resp, err := siteHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	cs := detectCharset(resp.Header.Get("Content-Type"), b)
	if cs != "" {
		return decodeBytesByCharset(b, cs), nil
	}
	return b, nil
}

func handleSiteToggle(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	en := r.URL.Query().Get("enabled") == "1"
	sitesMu.Lock()
	defer sitesMu.Unlock()
	cfg := loadSites()
	for i := range cfg.Sites {
		if cfg.Sites[i].Name == name {
			cfg.Sites[i].Enabled = en
			if err := saveSites(cfg); err != nil {
				recordCall("/api/sites/toggle", name, false, 0, "保存失败: "+err.Error())
				writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
				return
			}
			recordCall("/api/sites/toggle", name, true, 0, fmt.Sprintf("站点 %s -> %v", name, en))
			writeJSON(w, map[string]interface{}{"success": true, "name": name, "enabled": en})
			return
		}
	}
	recordCall("/api/sites/toggle", name, false, 0, "站点不存在: "+name)
	writeJSON(w, map[string]interface{}{"success": false, "message": "站点不存在"})
}

func handleSiteTest(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	kw := r.URL.Query().Get("kw")
	if kw == "" {
		kw = "庆余年"
	}
	cfg := loadSites()
	// 单站直测：只请求目标站点的采集接口，避免全量搜索 40+ 站再过滤导致慢/误判
	var target *Site
	for i := range cfg.Sites {
		if cfg.Sites[i].Name == name {
			target = &cfg.Sites[i]
			break
		}
	}
	if target == nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "未找到资源站: " + name})
		return
	}
	vs, err := searchSiteOne(*target, kw)
	count := len(vs)
	ok := err == nil && count > 0
	recordCall("/api/sites/test", name+" wd="+kw, ok, 0, fmt.Sprintf("命中 %d 条", count))
	writeJSON(w, map[string]interface{}{
		"success": ok, "site": name, "keyword": kw,
		"count": count, "videos": vs,
		"site_ok": err == nil, "searched": 1,
		"message": func() string {
			if err != nil {
				return "采集接口请求失败: " + err.Error()
			}
			return ""
		}(),
	})
}

// handleSiteAdd 添加资源站（参考 PHP addSite：名称+接口必填、重名拒绝）
func handleSiteAdd(w http.ResponseWriter, r *http.Request) {
	var req Site
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.APIURL = strings.TrimSpace(req.APIURL)
	req.SiteURL = strings.TrimSpace(req.SiteURL)
	if req.Name == "" || req.APIURL == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "名称和采集接口不能为空"})
		return
	}
	if !strings.HasPrefix(req.APIURL, "http://") && !strings.HasPrefix(req.APIURL, "https://") {
		writeJSON(w, map[string]interface{}{"success": false, "message": "采集接口需以 http(s):// 开头"})
		return
	}
	if req.Type == "" {
		req.Type = "maccms"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Priority == 0 {
		req.Priority = 100
	}
	sitesMu.Lock()
	defer sitesMu.Unlock()
	cfg := loadSites()
	for _, s := range cfg.Sites {
		if s.Name == req.Name {
			recordCall("/api/sites/add", req.Name, false, 0, "资源站名称已存在")
			writeJSON(w, map[string]interface{}{"success": false, "message": "资源站名称已存在"})
			return
		}
	}
	cfg.Sites = append(cfg.Sites, req)
	if err := saveSites(cfg); err != nil {
		recordCall("/api/sites/add", req.Name, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/sites/add", req.Name, true, 0, "添加成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "添加成功", "site": req})
}

// siteUpdateReq 编辑资源站请求（参考 PHP updateSite：name 定位，可更新接口/官网/备注等，可选改名）
type siteUpdateReq struct {
	Name     string `json:"name"`     // 定位原名（必填）
	NewName  string `json:"new_name"` // 可选：改名
	SiteURL  string `json:"site_url"`
	APIURL   string `json:"api_url"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Note     string `json:"note"`
	Priority int    `json:"priority"`
	Enabled  *bool  `json:"enabled"`
}

// handleSiteUpdate 编辑资源站（接口/官网/备注/优先级/启停/改名），落盘持久化
func handleSiteUpdate(w http.ResponseWriter, r *http.Request) {
	var req siteUpdateReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少站点名称"})
		return
	}
	req.APIURL = strings.TrimSpace(req.APIURL)
	req.SiteURL = strings.TrimSpace(req.SiteURL)
	req.NewName = strings.TrimSpace(req.NewName)
	if req.APIURL != "" && !strings.HasPrefix(req.APIURL, "http://") && !strings.HasPrefix(req.APIURL, "https://") {
		writeJSON(w, map[string]interface{}{"success": false, "message": "采集接口需以 http(s):// 开头"})
		return
	}
	sitesMu.Lock()
	defer sitesMu.Unlock()
	cfg := loadSites()
	idx := -1
	for i := range cfg.Sites {
		if cfg.Sites[i].Name == name || strings.EqualFold(strings.TrimSpace(cfg.Sites[i].Name), name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		recordCall("/api/sites/update", name, false, 0, "资源站不存在")
		writeJSON(w, map[string]interface{}{"success": false, "message": "资源站不存在: " + name})
		return
	}
	s := &cfg.Sites[idx]
	if req.NewName != "" && req.NewName != s.Name {
		for i := range cfg.Sites {
			if i != idx && cfg.Sites[i].Name == req.NewName {
				recordCall("/api/sites/update", name, false, 0, "新名称已存在")
				writeJSON(w, map[string]interface{}{"success": false, "message": "资源站名称已存在: " + req.NewName})
				return
			}
		}
		s.Name = req.NewName
	}
	if req.APIURL != "" {
		s.APIURL = req.APIURL
	}
	if req.SiteURL != "" {
		s.SiteURL = req.SiteURL
	}
	if req.Note != "" {
		s.Note = strings.TrimSpace(req.Note)
	}
	if req.Type != "" {
		s.Type = req.Type
	}
	if req.Status != "" {
		s.Status = req.Status
	}
	if req.Priority > 0 {
		s.Priority = req.Priority
	}
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	if err := saveSites(cfg); err != nil {
		recordCall("/api/sites/update", name, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/sites/update", name, true, 0, fmt.Sprintf("更新 %s -> %s", name, s.Name))
	writeJSON(w, map[string]interface{}{"success": true, "message": "更新成功", "site": *s})
}

// handleSiteDelete 删除资源站（参考 PHP deleteSite：精确 + 忽略大小写兜底）
func handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		name = strings.TrimSpace(req.Name)
	}
	if name == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少站点名称"})
		return
	}
	sitesMu.Lock()
	defer sitesMu.Unlock()
	cfg := loadSites()
	kept := []Site{}
	found := false
	for _, s := range cfg.Sites {
		if s.Name == name || strings.EqualFold(strings.TrimSpace(s.Name), name) {
			found = true
			continue
		}
		kept = append(kept, s)
	}
	if !found {
		recordCall("/api/sites/delete", name, false, 0, "资源站不存在")
		writeJSON(w, map[string]interface{}{"success": false, "message": "资源站不存在: " + name})
		return
	}
	cfg.Sites = kept
	if err := saveSites(cfg); err != nil {
		recordCall("/api/sites/delete", name, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/sites/delete", name, true, 0, "删除成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "删除成功"})
}

// ============ 资源站批量检测（并发 + 异步任务 + 进度条） ============

// checkResultItem 单个站点检测结果
type checkResultItem struct {
	Name          string `json:"name"`
	SiteURL       string `json:"site_url"`
	Usable        bool   `json:"usable"`
	Blocked       bool   `json:"blocked"`
	SearchLimited bool   `json:"search_limited"` // 搜索受限/需验证但列表接口连通（仍可用，靠接口连通兜底）
	Message       string `json:"message"`
	ResponseMS    int64  `json:"response_ms"`
}

// siteCheckTask 一次批量检测任务的进度状态
type siteCheckTask struct {
	ID        string            `json:"task"`
	Keyword   string            `json:"probe_keyword"`
	Total     int               `json:"total"`
	Done      int               `json:"done"`
	Usable    int               `json:"usable"`
	Blocked   int               `json:"blocked"`
	Finished  bool              `json:"finished"`
	StartTime time.Time         `json:"-"`
	Results   []checkResultItem `json:"results"`
	statusMap map[string]bool   `json:"-"` // siteName -> usable
	failMsg   map[string]string `json:"-"`
	limited   map[string]bool   `json:"-"` // siteName -> 搜索受限/需验证但接口连通
}

var (
	checkTaskMu    sync.Mutex
	checkTasks     = map[string]*siteCheckTask{}
	checkTaskOrder = []string{} // 用于简单清理过期任务
)

func newTaskID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// startSiteCheck 启动一次异步批量检测（并发执行，失败站点自动置 paused 屏蔽）
func startSiteCheck(kw string) *siteCheckTask {
	task := &siteCheckTask{ID: newTaskID(), Keyword: kw, StartTime: time.Now(), statusMap: map[string]bool{}, failMsg: map[string]string{}, limited: map[string]bool{}}
	checkTaskMu.Lock()
	checkTasks[task.ID] = task
	checkTaskOrder = append(checkTaskOrder, task.ID)
	// 清理已完成且超过 10 分钟的旧任务，避免无限增长
	cut := time.Now().Add(-10 * time.Minute)
	for _, id := range checkTaskOrder {
		t, ok := checkTasks[id]
		if !ok {
			continue
		}
		if t.Finished && t.StartTime.Before(cut) {
			delete(checkTasks, id)
		}
	}
	checkTaskMu.Unlock()
	go runSiteCheck(task, kw)
	return task
}

// runSiteCheck 并发执行检测：探测词搜索失败/无结果 → 标记 paused（屏蔽）+ 记录原因；成功 → active
func runSiteCheck(task *siteCheckTask, kw string) {
	cfg := loadSites()
	var targets []Site
	for _, s := range cfg.Sites {
		if s.Enabled || s.Status == "active" {
			targets = append(targets, s)
		}
	}
	task.Total = len(targets)

	var wg sync.WaitGroup
	sem := make(chan struct{}, siteConcurrency)
	for _, s := range targets {
		wg.Add(1)
		go func(s Site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// 多探针词 + 接口连通性综合探测：避免「单词无结果」整批误判失效
			ms, msg, vs, err := probeSiteOne(s)
			ok := err == nil && len(vs) > 0
			item := checkResultItem{Name: s.Name, SiteURL: s.SiteURL, ResponseMS: ms}
			if ok {
				item.Usable = true
				if strings.Contains(msg, "搜索无命中") {
					// 搜索受限/需验证：探针词均无结果，但列表接口连通且返回有效数据 → 仍可用，靠接口连通性兜底
					item.SearchLimited = true
					item.Message = "可用 · 搜索受限/需验证，仅接口连通（" + msg + "）"
				} else {
					item.Message = "正常 · " + msg
				}
			} else {
				item.Blocked = true
				msg := err.Error()
				if len(msg) > 60 {
					msg = msg[:60]
				}
				item.Message = msg
			}
			checkTaskMu.Lock()
			task.Done++
			if ok {
				task.Usable++
				task.statusMap[s.Name] = true
				if item.SearchLimited {
					task.limited[s.Name] = true
				}
			} else {
				task.Blocked++
				task.statusMap[s.Name] = false
				task.failMsg[s.Name] = err.Error()
			}
			task.Results = append(task.Results, item)
			checkTaskMu.Unlock()
		}(s)
	}
	wg.Wait()

	// 统一落盘状态（成功→active；失败→paused + 自动屏蔽原因）
	sitesMu.Lock()
	cfg2 := loadSites()
	changed := false
	for i := range cfg2.Sites {
		usable, ok := task.statusMap[cfg2.Sites[i].Name]
		if !ok {
			continue
		}
		if usable {
			if cfg2.Sites[i].Status != "active" {
				cfg2.Sites[i].Status = "active"
				if strings.Contains(cfg2.Sites[i].Note, "自动屏蔽") || strings.Contains(cfg2.Sites[i].Note, "搜索受限") {
					cfg2.Sites[i].Note = ""
				}
				changed = true
			}
			// 搜索受限/需验证站：常驻备注提示，便于列表一眼识别（不误判失效，靠接口连通性兜底）
			const limitedNote = "搜索受限/需验证·仅接口连通"
			if task.limited[cfg2.Sites[i].Name] {
				if cfg2.Sites[i].Note != limitedNote {
					cfg2.Sites[i].Note = limitedNote
					changed = true
				}
			} else if cfg2.Sites[i].Note == limitedNote {
				cfg2.Sites[i].Note = ""
				changed = true
			}
		} else {
			reason := "自动屏蔽·不可搜索: " + task.failMsg[cfg2.Sites[i].Name]
			if len(reason) > 80 {
				reason = reason[:80]
			}
			if cfg2.Sites[i].Status != "paused" || cfg2.Sites[i].Note != reason {
				cfg2.Sites[i].Status = "paused"
				cfg2.Sites[i].Note = reason
				changed = true
			}
		}
	}
	if changed {
		_ = saveSites(cfg2)
	}
	sitesMu.Unlock()

	checkTaskMu.Lock()
	task.Finished = true
	checkTaskMu.Unlock()
	recordCall("/api/sites/check", "wd="+kw, task.Usable > 0, 0, fmt.Sprintf("检测 %d 个：可用 %d / 屏蔽 %d", task.Total, task.Usable, task.Blocked))
}

// handleSiteCheck POST/GET /api/sites/check 启动批量检测任务（异步，配合 /check/progress 轮询进度）
func handleSiteCheck(w http.ResponseWriter, r *http.Request) {
	kw := strings.TrimSpace(r.URL.Query().Get("kw"))
	if kw == "" {
		kw = "爱情"
	}
	task := startSiteCheck(kw)
	writeJSON(w, map[string]interface{}{
		"success": true, "task": task.ID, "probe_keyword": kw, "total": task.Total,
	})
}

// handleSiteCheckProgress GET /api/sites/check/progress?task=<id> 查询检测任务进度
func handleSiteCheckProgress(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("task"))
	checkTaskMu.Lock()
	task := checkTasks[id]
	if task != nil {
		task.Results = append([]checkResultItem(nil), task.Results...) // 拷贝快照，避免轮询期间并发写
	}
	checkTaskMu.Unlock()
	if task == nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "任务不存在或已过期"})
		return
	}
	writeJSON(w, map[string]interface{}{
		"success": true, "task": task.ID, "probe_keyword": task.Keyword,
		"total": task.Total, "done": task.Done, "usable": task.Usable, "blocked": task.Blocked,
		"finished": task.Finished, "results": task.Results,
	})
}

// ============ 官替映射专区（官方剧名 ↔ 资源站剧名，从真实链接抓取添加） ============

func handleMapsList(w http.ResponseWriter, r *http.Request) {
	cfg := loadTitleMaps()
	recordCall("/api/maps", "", true, 0, fmt.Sprintf("映射 %d 条", len(cfg.NameMaps)))
	writeJSON(w, cfg)
}

func handleMapsAdd(w http.ResponseWriter, r *http.Request) {
	var req TitleMap
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	req.From = strings.TrimSpace(req.From)
	req.To = strings.TrimSpace(req.To)
	req.Note = strings.TrimSpace(req.Note)
	if req.From == "" || req.To == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "官方剧名(from)和目标剧名(to)不能为空"})
		return
	}
	titleMapsMu.Lock()
	defer titleMapsMu.Unlock()
	cfg := loadTitleMaps()
	for _, m := range cfg.NameMaps {
		if m.From == req.From && m.To == req.To {
			recordCall("/api/maps/add", req.From, false, 0, "映射已存在")
			writeJSON(w, map[string]interface{}{"success": false, "message": "该映射已存在"})
			return
		}
	}
	cfg.NameMaps = append(cfg.NameMaps, req)
	if err := saveTitleMaps(cfg); err != nil {
		recordCall("/api/maps/add", req.From, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/maps/add", req.From+"→"+req.To, true, 0, "添加成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "映射添加成功", "map": req})
}

func handleMapsDelete(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" {
		var req struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		from, to = strings.TrimSpace(req.From), strings.TrimSpace(req.To)
	}
	if from == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少映射来源剧名"})
		return
	}
	titleMapsMu.Lock()
	defer titleMapsMu.Unlock()
	cfg := loadTitleMaps()
	kept := []TitleMap{}
	found := false
	for _, m := range cfg.NameMaps {
		if m.From == from && (to == "" || m.To == to) {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		recordCall("/api/maps/delete", from, false, 0, "映射不存在")
		writeJSON(w, map[string]interface{}{"success": false, "message": "映射不存在: " + from})
		return
	}
	cfg.NameMaps = kept
	if err := saveTitleMaps(cfg); err != nil {
		recordCall("/api/maps/delete", from, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/maps/delete", from, true, 0, "删除成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "映射删除成功"})
}

// handleMapsFetch POST /api/maps/fetch?url=<真实官方视频页>[&selector=<自定义标题提取正则>] 抓取官方平台/剧名/集数，供添加映射
func handleMapsFetch(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少 url 参数"})
		return
	}
	// 自定义字段提取：官替映射专区支持自定义「标题选择器」（正则，第 1 捕获组为标题/字段值），
	// 页面结构变化时无需改代码即可按真实页面字段提取，如 "name":"([^"]+)" 取 JSON 内嵌剧名。
	selector := strings.TrimSpace(r.URL.Query().Get("selector"))
	if selector != "" {
		if _, err := regexp.Compile(selector); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "自定义选择器正则无效: " + err.Error()})
			return
		}
	}
	platform := detectPlatform(raw)
	if platform == "" {
		recordCall("/api/maps/fetch", raw, false, 0, "不支持的平台")
		writeJSON(w, map[string]interface{}{"success": false, "message": "不支持的视频平台"})
		return
	}
	vidHint := ""
	if m := tencentVidRe.FindStringSubmatch(raw); len(m) > 1 {
		vidHint = m[1]
	}
	var title string
	if selector != "" {
		title = fetchVideoTitleWithSelector(raw, selector)
	} else {
		title = fetchVideoTitle(raw, vidHint)
	}
	if title == "" || suspiciousTitle(title) {
		recordCall("/api/maps/fetch", raw, false, 0, "疑似反爬/JS壳/平台首页标题")
		writeJSON(w, map[string]interface{}{"success": false, "message": "该链接返回疑似反爬/JS 渲染壳/平台首页标题（「" + title + "」），未映射以避免脏数据；可换其它链接、配置平台标题选择器、使用「自定义提取字段」，或部署 render-title/ 无头渲染"})
		return
	}
	vi := parseVideoTitle(title)
	if strings.TrimSpace(vi.BaseTitle) == "" {
		recordCall("/api/maps/fetch", raw, false, 0, "JS壳页未解析出剧名")
		writeJSON(w, map[string]interface{}{"success": false, "message": "未能从标题解析出剧名: " + title + "。该页面为 JS 渲染壳（静态抓取只有平台标题），建议部署 render-title/（Node+Chromium 无头渲染）或配置该平台标题选择器后重试"})
		return
	}
	recordCall("/api/maps/fetch", raw, true, 0, fmt.Sprintf("%s · %s · 第%d集", platform, vi.BaseTitle, vi.EpisodeNum))
	// title_raw：官方原始标题（可能直接包含当前剧集，如「独剑九天01」「独剑九天 第01集」）
	// base_title：拆出的剧名字段；episode_raw/episode_num：拆出的剧集字段
	writeJSON(w, map[string]interface{}{
		"success": true, "platform": platform,
		"title_raw": title, "title": title,
		"base_title":  vi.BaseTitle,
		"episode_raw": vi.Episode, "episode": vi.Episode,
		"episode_num": vi.EpisodeNum,
	})
}

// ============ 官方平台自动更新（official_platforms.json：顺序/平台/域名/URL正则/标题选择器/优先级） ============

func handlePlatformsList(w http.ResponseWriter, r *http.Request) {
	platformCfgMu.Lock()
	cfg := loadOfficialPlatforms()
	platformCfgMu.Unlock()
	recordCall("/api/platforms", "", true, 0, fmt.Sprintf("官方平台 %d 条", len(cfg.Platforms)))
	writeJSON(w, cfg)
}

// handlePlatformsAdd POST /api/platforms/add 添加官方平台配置
func handlePlatformsAdd(w http.ResponseWriter, r *http.Request) {
	var req OfficialPlatform
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	req.Platform = strings.TrimSpace(req.Platform)
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.URLRe = strings.TrimSpace(req.URLRe)
	req.TitleSelector = strings.TrimSpace(req.TitleSelector)
	req.Note = strings.TrimSpace(req.Note)
	if req.Platform == "" || req.Domain == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "平台名称和域名不能为空"})
		return
	}
	if req.URLRe != "" {
		if _, err := regexp.Compile(req.URLRe); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "URL 匹配正则无效: " + err.Error()})
			return
		}
	}
	if req.TitleSelector != "" {
		if _, err := regexp.Compile(req.TitleSelector); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "标题选择器正则无效: " + err.Error()})
			return
		}
	}
	platformCfgMu.Lock()
	defer platformCfgMu.Unlock()
	cfg := loadOfficialPlatforms()
	for _, p := range cfg.Platforms {
		if p.Domain == req.Domain && p.Platform == req.Platform {
			recordCall("/api/platforms/add", req.Domain, false, 0, "配置已存在")
			writeJSON(w, map[string]interface{}{"success": false, "message": "该平台域名配置已存在"})
			return
		}
	}
	cfg.Platforms = append(cfg.Platforms, req)
	if err := saveOfficialPlatforms(cfg); err != nil {
		recordCall("/api/platforms/add", req.Domain, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/platforms/add", req.Platform+"@"+req.Domain, true, 0, "添加成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "官方平台配置添加成功", "platform": req})
}

// handlePlatformsUpdate POST /api/platforms/update 编辑官方平台配置（按 platform+domain 定位）
func handlePlatformsUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPlatform string `json:"old_platform"`
		OldDomain   string `json:"old_domain"`
		OfficialPlatform
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	req.OldPlatform = strings.TrimSpace(req.OldPlatform)
	req.OldDomain = strings.ToLower(strings.TrimSpace(req.OldDomain))
	req.Platform = strings.TrimSpace(req.Platform)
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if req.OldPlatform == "" || req.OldDomain == "" || req.Platform == "" || req.Domain == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "定位(旧平台/旧域名)和新配置均不能为空"})
		return
	}
	if req.URLRe != "" {
		if _, err := regexp.Compile(req.URLRe); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "URL 匹配正则无效: " + err.Error()})
			return
		}
	}
	if req.TitleSelector != "" {
		if _, err := regexp.Compile(req.TitleSelector); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "标题选择器正则无效: " + err.Error()})
			return
		}
	}
	platformCfgMu.Lock()
	defer platformCfgMu.Unlock()
	cfg := loadOfficialPlatforms()
	idx := -1
	for i := range cfg.Platforms {
		if cfg.Platforms[i].Platform == req.OldPlatform && strings.EqualFold(cfg.Platforms[i].Domain, req.OldDomain) {
			idx = i
			break
		}
	}
	if idx < 0 {
		recordCall("/api/platforms/update", req.OldDomain, false, 0, "配置不存在")
		writeJSON(w, map[string]interface{}{"success": false, "message": "配置不存在: " + req.OldPlatform + "@" + req.OldDomain})
		return
	}
	// 改名/改域后查重（排除自身）
	for i := range cfg.Platforms {
		if i != idx && cfg.Platforms[i].Platform == req.Platform && strings.EqualFold(cfg.Platforms[i].Domain, req.Domain) {
			recordCall("/api/platforms/update", req.Domain, false, 0, "目标配置已存在")
			writeJSON(w, map[string]interface{}{"success": false, "message": "目标配置已存在: " + req.Platform + "@" + req.Domain})
			return
		}
	}
	cfg.Platforms[idx] = req.OfficialPlatform
	if err := saveOfficialPlatforms(cfg); err != nil {
		recordCall("/api/platforms/update", req.Domain, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/platforms/update", req.Platform+"@"+req.Domain, true, 0, "更新成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "官方平台配置更新成功", "platform": cfg.Platforms[idx]})
}

// handlePlatformsDelete POST /api/platforms/delete?platform=&domain= 删除官方平台配置
func handlePlatformsDelete(w http.ResponseWriter, r *http.Request) {
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if platform == "" || domain == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少平台名称或域名"})
		return
	}
	platformCfgMu.Lock()
	defer platformCfgMu.Unlock()
	cfg := loadOfficialPlatforms()
	kept := []OfficialPlatform{}
	found := false
	for _, p := range cfg.Platforms {
		if p.Platform == platform && strings.EqualFold(p.Domain, domain) {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		recordCall("/api/platforms/delete", domain, false, 0, "配置不存在")
		writeJSON(w, map[string]interface{}{"success": false, "message": "配置不存在: " + platform + "@" + domain})
		return
	}
	cfg.Platforms = kept
	if err := saveOfficialPlatforms(cfg); err != nil {
		recordCall("/api/platforms/delete", domain, false, 0, "保存失败: "+err.Error())
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/platforms/delete", platform+"@"+domain, true, 0, "删除成功")
	writeJSON(w, map[string]interface{}{"success": true, "message": "官方平台配置删除成功"})
}

// handlePlatformsFetch GET /api/platforms/fetch?url=<真实官方链接> 自动更新官方：
// 按优先级匹配平台配置（域名+URL正则+标题选择器）→ 从真实链接提取「影视剧名 + 剧集集数」→ 返回供自动映射
func handlePlatformsFetch(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少 url 参数"})
		return
	}
	p := matchOfficialPlatform(raw)
	if p == nil {
		recordCall("/api/platforms/fetch", raw, false, 0, "未匹配到官方平台配置")
		writeJSON(w, map[string]interface{}{"success": false, "message": "未匹配到官方平台配置（请先添加对应平台的域名/URL 正则）"})
		return
	}
	title := fetchVideoTitleWithSelector(raw, p.TitleSelector)
	if title == "" || suspiciousTitle(title) {
		recordCall("/api/platforms/fetch", raw, false, 0, "疑似反爬/JS壳/平台首页标题")
		writeJSON(w, map[string]interface{}{"success": false, "message": "该链接返回疑似反爬/JS 渲染壳/平台首页标题（「" + title + "」），未映射以避免脏数据；可换其它链接、配置平台标题选择器或部署 render-title/ 无头渲染"})
		return
	}
	vi := parseVideoTitle(title)
	if strings.TrimSpace(vi.BaseTitle) == "" {
		recordCall("/api/platforms/fetch", raw, false, 0, "JS壳页未解析出剧名")
		writeJSON(w, map[string]interface{}{"success": false, "message": "未能从标题解析出剧名: " + title + "。该页面为 JS 渲染壳（静态抓取只有平台标题），建议部署 render-title/（Node+Chromium 无头渲染）或配置该平台标题选择器"})
		return
	}
	recordCall("/api/platforms/fetch", raw, true, 0, fmt.Sprintf("%s · %s · 第%d集", p.Platform, vi.BaseTitle, vi.EpisodeNum))
	writeJSON(w, map[string]interface{}{
		"success": true, "platform": p.Platform, "domain": p.Domain,
		"priority": p.Priority, "title_raw": title,
		"base_title":  vi.BaseTitle,                             // 影视剧名字段
		"episode_raw": vi.Episode, "episode_num": vi.EpisodeNum, // 剧集集数字段
	})
}

// handlePlatformsLinks GET /api/platforms/links 内置官方平台一键映射链接清单（用户无需输入链接）
func handlePlatformsLinks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"success": true, "links": builtinOfficialLinks,
		"hint": "点击「⚡ 一键映射」将从对应官方链接实时抓取剧名并自动映射到专区，无需输入链接",
	})
}

// handlePlatformsOneClick POST /api/platforms/oneclick?key=<内置链接key> 无脑映射：
// 内置官方链接 → 实时抓取标题 → 解析官方剧名 → 自动写入映射表（全程无需用户输入链接）
func handlePlatformsOneClick(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	link := builtinLinkByKey(key)
	if link == nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "未找到该官方链接"})
		return
	}
	p := matchOfficialPlatform(link.URL)
	if p == nil {
		recordCall("/api/platforms/oneclick", link.URL, false, 0, "未匹配平台配置")
		writeJSON(w, map[string]interface{}{"success": false, "message": "未匹配到官方平台配置（' + link.Platform + '）请先配置对应平台域名"})
		return
	}
	// 首选：无头 Chromium 渲染取真实剧名（解决平台 JS 渲染空壳问题）。
	// 若未部署 render-title/（缺 node/Chromium）或渲染失败，则回退到现有静态抓取。
	// 渲染结果若为反爬/出错页（如哔哩哔哩对数据中心 IP 返回「出错啦!」壳），同样回退静态+公开 API（B 站 API 可取标题）。
	title := renderTitleViaNode(link.URL)
	if title == "" || suspiciousTitle(title) {
		title = fetchVideoTitleWithSelector(link.URL, p.TitleSelector)
	}
	if title == "" {
		recordCall("/api/platforms/oneclick", link.URL, false, 0, "无法获取视频信息")
		writeJSON(w, map[string]interface{}{"success": false, "message": "该官方页面未能获取到视频信息（可能被反爬或为 JS 渲染页，可部署 render-title/ 无头渲染、换链接或配标题选择器）"})
		return
	}
	if suspiciousTitle(title) {
		recordCall("/api/platforms/oneclick", link.URL, false, 0, "疑似反爬页: "+title)
		writeJSON(w, map[string]interface{}{"success": false, "message": "该链接返回疑似验证/反爬页（「" + title + "」），未映射以避免脏数据；可换内置其它链接或配标题选择器"})
		return
	}
	vi := parseVideoTitle(title)
	official := strings.TrimSpace(vi.BaseTitle)
	if official == "" {
		recordCall("/api/platforms/oneclick", link.URL, false, 0, "JS壳页未解析出剧名")
		writeJSON(w, map[string]interface{}{"success": false, "message": "未能从标题解析出剧名: " + title + "。平台「" + p.Platform + "」页面为 JS 渲染壳，静态抓取只有平台标题。解决办法：① 部署 render-title/（Node+Chromium 无头渲染，自动取真实剧名）；② 在「官替映射专区」手动添加官方剧名 → 资源站剧名；③ 配置该平台标题选择器后重试"})
		return
	}
	added := addOfficialOneClickMap(p.Platform, official)
	we := "（已在映射表中，未重复添加）"
	if added {
		we = "（已自动映射到专区）"
	}
	recordCall("/api/platforms/oneclick", link.URL, true, 0, fmt.Sprintf("%s · %s%s", p.Platform, official, we))
	writeJSON(w, map[string]interface{}{
		"success": true, "platform": p.Platform, "base_title": official,
		"episode_num": vi.EpisodeNum, "added": added, "message": "平台「" + p.Platform + "」· 影视剧名「" + official + "」" + we,
	})
}

// ============================================================
// 非正片区间标注（SponsorBlock 思路）：维护「片头/片尾/赞助/三连/其他」时间戳区间，
// 持久化 skip_ranges.json；/api/skip 在去广告结果上叠加区间清单，供播放器按区间跳过。
// ============================================================

// SkipRange 一个非正片时间戳区间（秒）
// Key 为视频标识（如「剧名 第N集」，可空=全局区间，所有视频生效），用于按视频跳过内嵌/占位广告
type SkipRange struct {
	ID     string  `json:"id"`
	Key    string  `json:"key,omitempty"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Type   string  `json:"type"` // intro/outro/sponsor/selfpromo/interaction/other
	Note   string  `json:"note,omitempty"`
	Source string  `json:"source,omitempty"` // manual/auto
}

// SkipRangesConfig 区间配置（可执行文件旁 skip_ranges.json）
type SkipRangesConfig struct {
	Version    string      `json:"version"`
	UpdateDate string      `json:"update_date"`
	Ranges     []SkipRange `json:"ranges"`
}

var skipMu sync.Mutex

func skipRangesPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "skip_ranges.json")
	}
	return "skip_ranges.json"
}

func loadSkipRanges() *SkipRangesConfig {
	cfg := &SkipRangesConfig{Version: "1.0", UpdateDate: time.Now().Format("2006-01-02")}
	if b, err := os.ReadFile(skipRangesPath()); err == nil {
		c2 := &SkipRangesConfig{}
		if json.Unmarshal(b, c2) == nil {
			return c2
		}
	}
	return cfg
}

func saveSkipRanges(cfg *SkipRangesConfig) error {
	cfg.UpdateDate = time.Now().Format("2006-01-02")
	b, _ := json.MarshalIndent(cfg, "", "    ")
	return os.WriteFile(skipRangesPath(), b, 0o644)
}

func newSkipID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// rangesForVideo 返回指定视频（key）应跳过的非正片区间：全局区间 + 该视频专属区间（按 start 升序）
func rangesForVideo(key string) []SkipRange {
	cfg := loadSkipRanges()
	var out []SkipRange
	for _, rg := range cfg.Ranges {
		if rg.Key == "" || rg.Key == key {
			out = append(out, rg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// handleSkipList GET /api/skip?key=<视频标识> → 区间（按 start 升序）
// key 为空：只返回全局区间（Key 为空）；key=剧名 第N集：返回全局区间 + 该视频专属区间
func handleSkipList(w http.ResponseWriter, r *http.Request) {
	skipMu.Lock()
	defer skipMu.Unlock()
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	cfg := loadSkipRanges()
	var out []SkipRange
	for _, rg := range cfg.Ranges {
		// key 为空：只返回全局区间；key 指定：全局区间 + 该视频专属区间
		if rg.Key == "" || (key != "" && rg.Key == key) {
			out = append(out, rg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	writeJSON(w, map[string]interface{}{"success": true, "total": len(out), "ranges": out})
}

// handleSkipAdd POST /api/skip/add {key,start,end,type,note} → 新增区间（自动校验）
func handleSkipAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key   string  `json:"key"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Type  string  `json:"type"`
		Note  string  `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	if in.Start < 0 || in.End <= in.Start {
		writeJSON(w, map[string]interface{}{"success": false, "message": "区间无效：需要 0 ≤ start < end"})
		return
	}
	valid := map[string]bool{"intro": true, "outro": true, "sponsor": true, "selfpromo": true, "interaction": true, "other": true}
	if in.Type == "" {
		in.Type = "other"
	}
	if !valid[in.Type] {
		writeJSON(w, map[string]interface{}{"success": false, "message": "type 需为 intro/outro/sponsor/selfpromo/interaction/other"})
		return
	}
	skipMu.Lock()
	defer skipMu.Unlock()
	cfg := loadSkipRanges()
	cfg.Ranges = append(cfg.Ranges, SkipRange{
		ID: newSkipID(), Key: strings.TrimSpace(in.Key), Start: in.Start, End: in.End,
		Type: in.Type, Note: strings.TrimSpace(in.Note), Source: "manual",
	})
	if err := saveSkipRanges(cfg); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/skip/add", in.Key, true, 0, fmt.Sprintf("添加非正片区间 %0.1f-%0.1fs (%s)", in.Start, in.End, in.Type))
	writeJSON(w, map[string]interface{}{"success": true, "message": "已添加区间", "total": len(cfg.Ranges)})
}

// handleSkipDelete POST /api/skip/delete?id= → 删除区间
func handleSkipDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少 id"})
		return
	}
	skipMu.Lock()
	defer skipMu.Unlock()
	cfg := loadSkipRanges()
	out := cfg.Ranges[:0]
	found := false
	for _, rg := range cfg.Ranges {
		if rg.ID == id {
			found = true
			continue
		}
		out = append(out, rg)
	}
	cfg.Ranges = out
	if !found {
		writeJSON(w, map[string]interface{}{"success": false, "message": "区间不存在"})
		return
	}
	if err := saveSkipRanges(cfg); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/skip/delete", id, true, 0, "删除非正片区间")
	writeJSON(w, map[string]interface{}{"success": true, "message": "已删除区间", "total": len(cfg.Ranges)})
}

// ============================================================
// 弹幕过滤规则库（独立模块，后续可接弹幕源）：关键词/正则规则 → 过滤弹幕文案。
// 持久化 danmaku_rules.json；API 只提供规则管理与单条文本过滤，不依赖播放器 DOM。
// ============================================================

// DanmakuRule 一条弹幕过滤规则
type DanmakuRule struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // keyword / regex
	Pattern  string `json:"pattern"`
	Category string `json:"category"` // ad/spam/spoiler/attack/nsfw/other
	Note     string `json:"note,omitempty"`
	Enabled  bool   `json:"enabled"`
	Hits     int64  `json:"hits,omitempty"`
}

// DanmakuRulesConfig 弹幕规则配置（可执行文件旁 danmaku_rules.json）
type DanmakuRulesConfig struct {
	Version    string        `json:"version"`
	UpdateDate string        `json:"update_date"`
	Rules      []DanmakuRule `json:"rules"`
}

var dmMu sync.Mutex

func danmakuRulesPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "danmaku_rules.json")
	}
	return "danmaku_rules.json"
}

func loadDanmakuRules() *DanmakuRulesConfig {
	cfg := &DanmakuRulesConfig{Version: "1.0", UpdateDate: time.Now().Format("2006-01-02")}
	if b, err := os.ReadFile(danmakuRulesPath()); err == nil {
		c2 := &DanmakuRulesConfig{}
		if json.Unmarshal(b, c2) == nil {
			return c2
		}
	}
	return cfg
}

func saveDanmakuRules(cfg *DanmakuRulesConfig) error {
	cfg.UpdateDate = time.Now().Format("2006-01-02")
	b, _ := json.MarshalIndent(cfg, "", "    ")
	return os.WriteFile(danmakuRulesPath(), b, 0o644)
}

var danmakuRegexCache = map[string]*regexp.Regexp{}
var danmakuRegexMu sync.Mutex

// compileDanmakuRegex 编译（带缓存）正则规则
func compileDanmakuRegex(pattern string) *regexp.Regexp {
	danmakuRegexMu.Lock()
	defer danmakuRegexMu.Unlock()
	if re, ok := danmakuRegexCache[pattern]; ok {
		return re
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil
	}
	danmakuRegexCache[pattern] = re
	return re
}

// danmakuFilterText 用当前启用规则过滤一条弹幕文本；返回是否应隐藏 + 命中的规则类型
func danmakuFilterText(text string) (bool, string, string) {
	dmMu.Lock()
	defer dmMu.Unlock()
	cfg := loadDanmakuRules()
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if !r.Enabled {
			continue
		}
		hit := false
		switch r.Type {
		case "regex":
			re := compileDanmakuRegex(r.Pattern)
			if re != nil && re.MatchString(text) {
				hit = true
			}
		default: // keyword：子串匹配
			if strings.Contains(text, r.Pattern) {
				hit = true
			}
		}
		if hit {
			r.Hits++
			_ = saveDanmakuRules(cfg)
			return true, r.Category, r.Pattern
		}
	}
	return false, "", ""
}

// handleDanmakuRulesList GET /api/danmaku → 规则列表（含命中数）
func handleDanmakuRulesList(w http.ResponseWriter, r *http.Request) {
	dmMu.Lock()
	defer dmMu.Unlock()
	cfg := loadDanmakuRules()
	writeJSON(w, map[string]interface{}{"success": true, "total": len(cfg.Rules), "rules": cfg.Rules})
}

// handleDanmakuRulesAdd POST /api/danmaku/add {type,pattern,category,note,enabled} → 新增规则
func handleDanmakuRulesAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type     string `json:"type"`
		Pattern  string `json:"pattern"`
		Category string `json:"category"`
		Note     string `json:"note"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败: " + err.Error()})
		return
	}
	in.Pattern = strings.TrimSpace(in.Pattern)
	if in.Pattern == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "规则内容不能为空"})
		return
	}
	if in.Type == "" {
		in.Type = "keyword"
	}
	if in.Type != "keyword" && in.Type != "regex" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "type 需为 keyword 或 regex"})
		return
	}
	if in.Type == "regex" {
		if compileDanmakuRegex(in.Pattern) == nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "正则不合法"})
			return
		}
	}
	if in.Category == "" {
		in.Category = "other"
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	dmMu.Lock()
	defer dmMu.Unlock()
	cfg := loadDanmakuRules()
	for _, r := range cfg.Rules {
		if r.Type == in.Type && r.Pattern == in.Pattern {
			writeJSON(w, map[string]interface{}{"success": false, "message": "规则已存在"})
			return
		}
	}
	cfg.Rules = append(cfg.Rules, DanmakuRule{
		ID: newSkipID(), Type: in.Type, Pattern: in.Pattern,
		Category: in.Category, Note: strings.TrimSpace(in.Note), Enabled: enabled,
	})
	if err := saveDanmakuRules(cfg); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	recordCall("/api/danmaku/add", in.Pattern, true, 0, "新增弹幕过滤规则")
	writeJSON(w, map[string]interface{}{"success": true, "message": "已添加规则", "total": len(cfg.Rules)})
}

// handleDanmakuRulesToggle POST /api/danmaku/toggle?id= → 启停规则
func handleDanmakuRulesToggle(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少 id"})
		return
	}
	dmMu.Lock()
	defer dmMu.Unlock()
	cfg := loadDanmakuRules()
	for i := range cfg.Rules {
		if cfg.Rules[i].ID == id {
			cfg.Rules[i].Enabled = !cfg.Rules[i].Enabled
			_ = saveDanmakuRules(cfg)
			recordCall("/api/danmaku/toggle", id, true, 0, "启停弹幕规则")
			writeJSON(w, map[string]interface{}{"success": true, "message": "已切换规则状态", "enabled": cfg.Rules[i].Enabled})
			return
		}
	}
	writeJSON(w, map[string]interface{}{"success": false, "message": "规则不存在"})
}

// handleDanmakuRulesDelete POST /api/danmaku/delete?id= → 删除规则
func handleDanmakuRulesDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, map[string]interface{}{"success": false, "message": "缺少 id"})
		return
	}
	dmMu.Lock()
	defer dmMu.Unlock()
	cfg := loadDanmakuRules()
	out := cfg.Rules[:0]
	found := false
	for _, rl := range cfg.Rules {
		if rl.ID == id {
			found = true
			continue
		}
		out = append(out, rl)
	}
	cfg.Rules = out
	if !found {
		writeJSON(w, map[string]interface{}{"success": false, "message": "规则不存在"})
		return
	}
	_ = saveDanmakuRules(cfg)
	recordCall("/api/danmaku/delete", id, true, 0, "删除弹幕过滤规则")
	writeJSON(w, map[string]interface{}{"success": true, "message": "已删除规则", "total": len(cfg.Rules)})
}

// handleDanmakuTest POST /api/danmaku/test {text} → 测试单条文本是否被过滤（不落库）
func handleDanmakuTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数解析失败"})
		return
	}
	blocked, cat, pat := danmakuFilterText(strings.TrimSpace(in.Text))
	writeJSON(w, map[string]interface{}{"success": true, "blocked": blocked, "category": cat, "matched": pat})
}

// ============================================================
// 后台登录鉴权：账号密码（默认 admin / admin123，后台可改，持久化 auth.json）
// ============================================================

const cookieName = "mxgt_token"

type AuthConfig struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Salt         string `json:"salt"`
}

var (
	authMu       sync.Mutex
	authUsername = "admin"
	authSalt     = ""
	authPwHash   = ""
	sessions     = map[string]int64{} // token -> expiry(ms)
	sessionTTL   = 7 * 24 * time.Hour
)

func authPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "auth.json")
	}
	return "auth.json"
}

func hashPw(salt, pw string) string {
	sum := sha256.Sum256([]byte("mxgt:" + salt + ":" + pw))
	return hex.EncodeToString(sum[:])
}

// loadAuth 读取/初始化账号密码。默认 admin/admin123，首次运行生成随机盐并落盘。
func loadAuth() {
	authMu.Lock()
	defer authMu.Unlock()
	p := authPath()
	if b, err := os.ReadFile(p); err == nil {
		var c AuthConfig
		if json.Unmarshal(b, &c) == nil && c.Username != "" && c.Salt != "" && c.PasswordHash != "" {
			authUsername, authSalt, authPwHash = c.Username, c.Salt, c.PasswordHash
			return
		}
	}
	// 默认 admin/admin123
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	authSalt = hex.EncodeToString(buf)
	authUsername = "admin"
	authPwHash = hashPw(authSalt, "admin123")
	_ = saveAuthLocked()
}

func saveAuthLocked() error {
	c := AuthConfig{Username: authUsername, Salt: authSalt, PasswordHash: authPwHash}
	b, _ := json.MarshalIndent(c, "", "    ")
	return os.WriteFile(authPath(), b, 0o600)
}

func newToken() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func isAuthed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	now := time.Now().UnixMilli()
	authMu.Lock()
	defer authMu.Unlock()
	exp, ok := sessions[c.Value]
	if !ok {
		return false
	}
	if now > exp {
		delete(sessions, c.Value)
		return false
	}
	return true
}

func setSession(w http.ResponseWriter) string {
	tok := newToken()
	authMu.Lock()
	sessions[tok] = time.Now().Add(sessionTTL).UnixMilli()
	authMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true,
		MaxAge: int(sessionTTL / time.Second),
	})
	return tok
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		authMu.Lock()
		delete(sessions, c.Value)
		authMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
}

// handleAuthLogin POST /api/auth/login {username,password}
func handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数错误"})
		return
	}
	authMu.Lock()
	ok := in.Username == authUsername && hashPw(authSalt, in.Password) == authPwHash
	name := authUsername
	authMu.Unlock()
	if !ok {
		writeJSON(w, map[string]interface{}{"success": false, "message": "账号或密码错误"})
		return
	}
	setSession(w)
	writeJSON(w, map[string]interface{}{"success": true, "username": name})
}

// handleAuthLogout POST /api/auth/logout
func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	clearSession(w, r)
	writeJSON(w, map[string]interface{}{"success": true})
}

// handleAuthMe GET /api/auth/me
func handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if isAuthed(r) {
		authMu.Lock()
		n := authUsername
		authMu.Unlock()
		writeJSON(w, map[string]interface{}{"success": true, "username": n})
		return
	}
	writeJSON(w, map[string]interface{}{"success": false})
}

// handleAuthPassword POST /api/auth/password {old,new}（需已登录）
func handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if !isAuthed(r) {
		writeJSON(w, map[string]interface{}{"success": false, "message": "未登录"})
		return
	}
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "参数错误"})
		return
	}
	if len(in.New) < 4 {
		writeJSON(w, map[string]interface{}{"success": false, "message": "新密码至少 4 位"})
		return
	}
	authMu.Lock()
	defer authMu.Unlock()
	if hashPw(authSalt, in.Old) != authPwHash {
		writeJSON(w, map[string]interface{}{"success": false, "message": "原密码错误"})
		return
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	authSalt = hex.EncodeToString(buf)
	authPwHash = hashPw(authSalt, in.New)
	if err := saveAuthLocked(); err != nil {
		writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"success": true, "message": "密码已更新"})
}

// guard 需要登录的接口包装：未登录返回 401 JSON
func guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuthed(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请先登录 (/mxadmin/login)"})
			return
		}
		next(w, r)
	}
}

// detectDirectFormat 判断 url 是否为直链（m3u8 / mp4 等）而非视频页面
func detectDirectFormat(raw string) string {
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, ".m3u8"):
		return "m3u8"
	case strings.Contains(l, ".mp4"), strings.Contains(l, ".mkv"), strings.Contains(l, ".flv"), strings.Contains(l, ".m3u8?"):
		return "direct"
	}
	return ""
}

// jxResolve 调用接口公共解析核心：按输入类型返回基础播放信息与完整明细
//   - base：客户端/服务端共用的可播字段（url/full/play/name/pic/format 等）
//   - detail：服务端扩展明细（m3u8 为去广告统计；官方页为官替完整信息；直链为 nil）
func jxResolve(r *http.Request, raw, engine string) (base map[string]interface{}, detail interface{}, ok bool, msg string) {
	base = map[string]interface{}{
		"code": 0, "success": false, "msg": "参数缺失", "url": "",
		"play": "", "full": "", "name": "", "pic": "", "header": "", "format": "",
	}
	if raw == "" {
		return base, nil, false, "参数缺失"
	}
	switch detectDirectFormat(raw) {
	case "m3u8":
		res := cleanOne(raw, false, engine)
		if !res.Success {
			return base, nil, false, res.Message
		}
		// 统一走播放代理：/api/play 内置增强去广告 + 分片代理（无广告且不卡顿）
		ad := playURL(r, "/api/play", url.Values{"url": {raw}})
		base["url"], base["full"], base["play"], base["format"] = ad, ad, raw, "m3u8"
		base["name"] = res.Message
		res.Segments = nil // 逐段明细仅 /api/clean/json 提供，此处减小体积
		detail = &res
		return base, detail, true, "ok"
	case "direct":
		base["url"], base["full"], base["play"], base["format"] = raw, raw, raw, "direct"
		return base, nil, true, "ok"
	default:
		// 官方视频页 → 官替链路（自动得到基于本机 Host 的无广告直链）
		rr := replaceOne(r, raw)
		if !rr.Success || rr.ADSkipURL == "" {
			return base, nil, false, rr.Message
		}
		base["url"], base["full"], base["play"] = rr.ADSkipURL, rr.ADSkipURL, rr.M3U8URL
		base["format"], base["name"], base["pic"], base["remarks"] = "m3u8", rr.VideoName, rr.VideoPic, rr.VideoRemarks
		detail = &rr
		return base, detail, true, "ok"
	}
}

// jxWrite 统一组装响应并记录调用
func jxWrite(w http.ResponseWriter, r *http.Request, api string, resp map[string]interface{}, detail interface{}, ok bool, msg string) {
	if ok {
		resp["code"], resp["success"] = 200, true
		// msg 与 url 一致：部分影视/TVBox 调用方取 msg 作为可播放地址
		resp["msg"] = resp["url"]
		if detail != nil {
			resp["detail"] = detail
		}
	} else {
		resp["code"], resp["msg"] = 0, msg
	}
	recordCall(api, r.URL.Query().Get("url"), ok, 0, resp["msg"].(string))
	writeJSON(w, resp)
}

// handleJX JSON 通用兼容接口（供影视 / TVBox / 盒子等调用，保留向后兼容，不附加 detail）
//
//	GET /api/jx?url=<m3u8|mp4|官方视频页>&engine=basic|auto|ai
//	返回 {code:0/200, success, msg, url(可播放/去广告地址), full, play, name, pic, header, format}
//	code=200 表示成功（与 HTTP 语义一致），0 表示失败；成功时 msg 与 url 一致（同为可播放地址）
//	url 用请求 Host 动态拼接，不硬编码；响应已带全局 CORS 头。
func handleJX(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	engine := r.URL.Query().Get("engine")
	resp, _, ok, msg := jxResolve(r, raw, engine)
	if ok {
		resp["code"], resp["success"] = 200, true
		// msg 与 url 一致：部分影视/TVBox 调用方取 msg 作为可播放地址
		resp["msg"] = resp["url"]
	} else {
		resp["code"], resp["msg"] = 0, msg
	}
	recordCall("/api/jx", raw, ok, 0, resp["msg"].(string))
	writeJSON(w, resp)
}

// handleJXServer GET /api/jx/server?url=...&engine=...：服务器调用 API 接口
// 返回客户端全部字段 + detail 完整明细（去广告统计 / 官替全过程），供服务端二次处理
func handleJXServer(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	engine := r.URL.Query().Get("engine")
	resp, detail, ok, msg := jxResolve(r, raw, engine)
	jxWrite(w, r, "/api/jx/server", resp, detail, ok, msg)
}

// handleJXClient GET /api/jx/client?url=...&engine=...：客户端调用接口（播放器/盒子）
// 仅返回播放所需精简字段（code/success/msg/url/name/pic/format），msg=url 可播放地址，体积更小响应更快
func handleJXClient(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	engine := r.URL.Query().Get("engine")
	resp, _, ok, msg := jxResolve(r, raw, engine)
	if ok {
		resp["code"], resp["success"] = 200, true
		resp["msg"] = resp["url"]
		delete(resp, "remarks")
		delete(resp, "header")
	} else {
		resp["code"], resp["msg"] = 0, msg
	}
	recordCall("/api/jx/client", raw, ok, 0, resp["msg"].(string))
	writeJSON(w, resp)
}

// handleAIConfig 查看(GET)/更新(POST,需登录) AI 去广告配置
func handleAIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if !isAuthed(r) {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]interface{}{"success": false, "message": "请先登录"})
			return
		}
		var in AIConfig
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "参数错误"})
			return
		}
		if in.Version == "" {
			in.Version = loadAIConfig().Version
		}
		if err := saveAIConfig(&in); err != nil {
			writeJSON(w, map[string]interface{}{"success": false, "message": "保存失败: " + err.Error()})
			return
		}
		recordCall("/api/ai/config", "POST", true, 0, "更新 AI 去广告配置")
		writeJSON(w, map[string]interface{}{"success": true, "message": "AI 去广告配置已保存", "version": in.Version})
		return
	}
	cfg := loadAIConfig()
	masked := *cfg
	if masked.APIKey != "" {
		masked.APIKey = "<set>"
	}
	writeJSON(w, map[string]interface{}{"success": true, "ai_version": aiVersionText(), "config": masked})
}

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	sitesConc := flag.Int("sites-conc", 8, "资源站批量操作（搜索/检测）并发数，建议 4~16")
	flag.Parse()
	siteConcurrency = *sitesConc
	if siteConcurrency < 1 {
		siteConcurrency = 1
	}
	if flag.NArg() > 0 {
		// CLI 模式：go run main.go <m3u8_url>
		res := cleanOne(flag.Arg(0), false, "")
		if !res.Success {
			fmt.Println("失败:", res.Message)
			os.Exit(1)
		}
		fmt.Println(res.Message)
		fmt.Println("=== 过滤后 M3U8 ===")
		fmt.Println(res.FilteredM3U8)
		return
	}
	loadAuth() // 初始化后台账号密码（默认 admin/admin123）
	titleMapsMu.Lock()
	loadTitleMaps() // 启动时即清洗历史脏映射（壳标题作键的映射），无需等首次访问
	titleMapsMu.Unlock()
	// 登录页
	http.HandleFunc("/mxadmin/login", handleLogin)
	// 首页固定进了后台；后台入口为 /mxadmin
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mxadmin" || r.URL.Path == "/mxadmin/" {
			handleAdmin(w, r)
			return
		}
		if r.URL.Path == "/" {
			handleFront(w, r)
			return
		}
		http.NotFound(w, r)
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "ok %s\n", AppVersion)
	})
	http.HandleFunc("/player", handlePlayer)      // 独立外置播放页（开放，供官替 external_url 新窗口播放）
	http.HandleFunc("/api/play", handlePlayProxy) // 播放代理：m3u8 改写 + 分片透传（解决跨域/限速卡顿）
	http.HandleFunc("/api/audit", handleAudit)    // 广告核查：列出不连贯片段供人工核查（开放）
	http.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, stats.snapshot())
	})
	// 鉴权
	http.HandleFunc("/api/auth/login", handleAuthLogin)
	http.HandleFunc("/api/auth/logout", handleAuthLogout)
	http.HandleFunc("/api/auth/me", handleAuthMe)
	http.HandleFunc("/api/auth/password", guard(handleAuthPassword))
	http.HandleFunc("/api/update/check", handleUpdateCheck)
	http.HandleFunc("/api/update/apply", guard(handleUpdateApply))
	http.HandleFunc("/api/clean", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		aggr := r.URL.Query().Get("opt") == "aggresive"
		eng := r.URL.Query().Get("engine")
		res := cleanOne(u, aggr, eng)
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
		eng := r.URL.Query().Get("engine")
		res := cleanOne(u, aggr, eng)
		recordClean(res, u)
		writeJSON(w, res)
	})
	// 新版增强测试播放引擎：/api/clean/enhanced（独立实现，不影响原 /api/clean）
	// 默认返回过滤后 m3u8（可直接喂播放器），format=json 返回结构化结果
	http.HandleFunc("/api/clean/enhanced", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		eng := r.URL.Query().Get("engine")
		res := cleanEnhanced(u, eng)
		recordClean(res, u)
		if !res.Success {
			writeJSON(w, map[string]interface{}{"success": false, "message": res.Message})
			return
		}
		if r.URL.Query().Get("format") == "json" {
			res.Segments = nil
			writeJSON(w, res)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, res.FilteredM3U8)
	})
	http.HandleFunc("/api/clean/enhanced/json", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		eng := r.URL.Query().Get("engine")
		res := cleanEnhanced(u, eng)
		recordClean(res, u)
		writeJSON(w, res)
	})
	// 非正片区间标注（SponsorBlock 思路）：列表开放（播放器读取按区间跳过），增删需登录
	http.HandleFunc("/api/skip", handleSkipList)
	http.HandleFunc("/api/skip/add", guard(handleSkipAdd))
	http.HandleFunc("/api/skip/delete", guard(handleSkipDelete))
	// 弹幕过滤规则库（独立模块，需登录）：/api/danmaku
	http.HandleFunc("/api/danmaku", guard(handleDanmakuRulesList))
	http.HandleFunc("/api/danmaku/add", guard(handleDanmakuRulesAdd))
	http.HandleFunc("/api/danmaku/toggle", guard(handleDanmakuRulesToggle))
	http.HandleFunc("/api/danmaku/delete", guard(handleDanmakuRulesDelete))
	http.HandleFunc("/api/danmaku/test", guard(handleDanmakuTest))
	// AI 去广告配置查看/更新
	http.HandleFunc("/api/ai/config", handleAIConfig)
	// 官替链路：官方视频页 → 资源站 → 无广告 m3u8
	http.HandleFunc("/api/replace", handleReplace)
	// JSON 通用兼容接口（影视 / TVBox / 盒子等调用）
	http.HandleFunc("/api/jx", handleJX)
	// 独立调用接口：服务器调用 API（返回完整明细 detail）/ 客户端调用（仅返回精简播放字段）
	http.HandleFunc("/api/jx/server", handleJXServer)
	http.HandleFunc("/api/jx/client", handleJXClient)
	// 资源站管理（需登录）
	http.HandleFunc("/api/sites", guard(handleSitesList))
	http.HandleFunc("/api/sites/toggle", guard(handleSiteToggle))
	http.HandleFunc("/api/sites/test", guard(handleSiteTest))
	http.HandleFunc("/api/sites/add", guard(handleSiteAdd))
	http.HandleFunc("/api/sites/update", guard(handleSiteUpdate))
	http.HandleFunc("/api/sites/delete", guard(handleSiteDelete))
	http.HandleFunc("/api/sites/check", guard(handleSiteCheck))
	http.HandleFunc("/api/sites/check/progress", guard(handleSiteCheckProgress))
	http.HandleFunc("/api/sites/m3u8", guard(handleSiteM3U8)) // 资源站搜索仅返回 m3u8 播放地址 开关（读写需登录）
	http.HandleFunc("/api/sites/import", guard(handleSiteImport)) // 一键自动导入（从链接抓取/粘贴文本解析批量添加）
	// 官替映射专区（需登录）
	http.HandleFunc("/api/maps", guard(handleMapsList))
	http.HandleFunc("/api/maps/add", guard(handleMapsAdd))
	http.HandleFunc("/api/maps/delete", guard(handleMapsDelete))
	http.HandleFunc("/api/maps/fetch", guard(handleMapsFetch))
	// 官方平台自动更新配置（需登录）：顺序/平台/域名/URL正则/标题选择器/优先级
	http.HandleFunc("/api/platforms", guard(handlePlatformsList))
	http.HandleFunc("/api/platforms/add", guard(handlePlatformsAdd))
	http.HandleFunc("/api/platforms/update", guard(handlePlatformsUpdate))
	http.HandleFunc("/api/platforms/delete", guard(handlePlatformsDelete))
	http.HandleFunc("/api/platforms/fetch", guard(handlePlatformsFetch))
	http.HandleFunc("/api/platforms/links", guard(handlePlatformsLinks))
	http.HandleFunc("/api/platforms/oneclick", guard(handlePlatformsOneClick))
	log.Printf("MXGT-Go %s listening on %s (M3U8 去广告 + 官替链路服务)", AppVersion, *addr)
	// 使用全局 httpServer 句柄：更新重启时可优雅关闭释放端口（修复更新后不自动重启）
	httpServer = &http.Server{Addr: *addr, Handler: withCORS(http.DefaultServeMux)}
	// 端口占用自动重试：避免「更新/重启后不能启动」（旧进程未退出 / TIME_WAIT / 端口被其它程序占用）
	// 300ms 快速重试：更新重启时旧进程释放端口后新进程几乎立即绑定成功，避免 2s/次 的长等待
	const maxBindRetry = 100
	for i := 0; i < maxBindRetry; i++ {
		err := httpServer.ListenAndServe()
		if err == nil || err == http.ErrServerClosed {
			return // 被优雅关闭（如更新重启），退出
		}
		if strings.Contains(err.Error(), "address already in use") {
			log.Printf("端口 %s 被占用，300ms 后重试(%d/%d)...", *addr, i+1, maxBindRetry)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		log.Fatal(err)
	}
	log.Fatalf("启动失败：端口 %s 在 %d 次重试后仍被占用，请先释放端口", *addr, maxBindRetry)
}
