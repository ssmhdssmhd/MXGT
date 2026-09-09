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
)

const (
	AppVersion = "v0.4.8"
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

// cleanOne 主流程：抓取-解析-检测-输出
func cleanOne(rawURL string, aggresive bool, engine string) ParseResult {
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
  .siterow{display:flex;align-items:center;gap:8px;padding:6px 12px;border-bottom:1px solid #f6f6f6;font-size:13px}
  .siterow:last-child{border-bottom:0}
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
    <h2>🏢 资源站管理 <span class="muted">（默认全部禁用，按需启用；失效站自动隐藏）</span></h2>
    <div class="row" style="flex-wrap:wrap">
      <input id="nsName" placeholder="名称（必填）" style="width:150px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <input id="nsApi" placeholder="采集接口 https://…/api.php/provide/vod/（必填）" style="flex:1;min-width:260px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <input id="nsSite" placeholder="官网（可选）" style="width:190px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <input id="nsNote" placeholder="备注（可选）" style="width:150px;padding:8px 10px;border-radius:10px;border:1px solid #d1d5db">
      <button class="btn" style="padding:7px 14px;font-size:12px" onclick="addSite()">➕ 添加资源站</button>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <input id="siteSearch" placeholder="🔍 搜索站点/备注/接口…" style="flex:0 0 260px;padding:9px 12px;border-radius:10px;border:1px solid #d1d5db" onkeyup="renderSites()">
      <span class="muted" id="siteCount"></span>
    </div>
    <div class="row" style="flex-wrap:wrap">
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="loadSites()">⟳ 刷新</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="setAllSites(false)">全部折叠</button>
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="setAllSites(true)">全部展开</button>
      <label class="muted" style="display:flex;align-items:center;gap:4px"><input type="checkbox" id="hideFail" checked onchange="loadSites()"> 隐藏失效站</label>
      <span class="muted">测试词</span>
      <input id="siteKw" value="庆余年" style="width:110px;padding:6px;border-radius:8px;border:1px solid #d1d5db">
      <button class="btn ghost" style="padding:5px 12px;font-size:12px" onclick="checkSites()">🧹 检测并屏蔽失效站</button>
    </div>
    <div class="prog-wrap" id="siteProg" style="display:none"><div class="prog-bar"></div></div>
    <div id="siteList"></div>
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
    <h2>📚 HTTP 接口说明</h2>
    <table>
      <tr><th>接口</th><th>说明</th></tr>
      <tr><td><code>GET /api/clean?url=&lt;m3u8&gt;</code></td><td>返回过滤后的无广告 M3U8 纯文本（绝对地址）</td></tr>
      <tr><td><code>GET /api/clean/json?url=&lt;m3u8&gt;</code></td><td>返回 JSON：统计 + 过滤后文本 + 每个片段明细</td></tr>
      <tr><td><code>GET /api/clean?url=&lt;m3u8&gt;&amp;opt=aggresive</code></td><td>开启聚合聚类识别（可能误伤统一切片正片）</td></tr>
      <tr><td><code>GET /api/replace?url=&lt;官方视频页&gt;</code></td><td>官替链路：资源站匹配后返回无广告直链 ad_skip_url</td></tr>
      <tr><td><code>GET /api/sites</code> / <code>/toggle</code> / <code>/test</code></td><td>资源站列表（默认隐藏失效）/ 启停 / 搜索测试</td></tr>
      <tr><td><code>POST /api/sites/add</code> / <code>/delete</code> / <code>/check</code></td><td>添加 / 删除资源站 / 异步批量检测（并发）并屏蔽失效站</td></tr>
      <tr><td><code>GET /api/sites/check/progress?task=</code></td><td>查询批量检测任务进度（供进度条轮询）</td></tr>
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
refreshStats(); setInterval(refreshStats,5000); checkUpdate(); loadSites();

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
  if(hlsInst){try{hlsInst.destroy();}catch(e){}hlsInst=null;}
  // mp4 等直链：交给原生播放器，任何浏览器都支持
  if(isDirectVideo(src)){
    player.src=src;player.play().catch(function(){});
    return;
  }
  player.pause();player.removeAttribute('src');try{player.load();}catch(e){}
  // 原生支持 HLS（iOS Safari / 部分系统浏览器）优先
  if(player.canPlayType('application/vnd.apple.mpegurl')){
    player.src=src;player.play().catch(function(){});
    return;
  }
  // 其余用 hls.js（多 CDN 兜底）
  loadHls(function(){
    if(Hls&&Hls.isSupported()){
      hlsInst=new Hls({enableWorker:true,
        xhrSetup:function(xhr){xhr.withCredentials=false;xhr.setRequestHeader('Origin',location.origin);}});
      hlsInst.loadSource(src);hlsInst.attachMedia(player);
      player.play().catch(function(){});
    }else if(player.canPlayType('application/vnd.apple.mpegurl')){
      player.src=src;
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
    if(j.ad_skip_url){
      s+=' <a href="'+j.ad_skip_url+'" target="_blank">无广告直链 ↗</a>';
      s+=' <button class="btn" style="padding:4px 10px;font-size:12px" onclick="playURL(\''+j.ad_skip_url+'\')">▶ 播放</button>';
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
    const hide=el('hideFail')?el('hideFail').checked:true;
    const r=await fetch('/api/sites'+(hide?'':'?show=all'));
    if(r.status===401){location.href='/mxadmin/login';return;}
    const j=await r.json();
    sitesData=(j&&j.sites)?j.sites:[];
    sitesStats=j.stats||null;
    renderSites();
  }catch(e){el('siteList').innerHTML='<span class="muted">加载失败: '+esc(e.message)+'</span>';}
  showProg(false);
}
function renderSites(){
  if(!sitesData)return;
  const q=((el('siteSearch').value)||'').trim().toLowerCase();
  const s2=sitesData.filter(function(x){
    if(!q)return true;
    return (x.name+((x.note)||'')+((x.api_url)||'')).toLowerCase().indexOf(q)>=0;
  });
  const en=s2.filter(function(x){return x.enabled}).length;
  const st=sitesStats||{};
  el('siteCount').innerHTML='已启用 '+en+' / 显示 '+s2.length+' 个站点'+(q?'（筛选：'+esc(q)+'）':'')+
    (st.total?'　<span class="muted">共 '+st.total+' · 可用 '+st.active+' · 失效 '+st.failed+'</span>':'');
  const groups=[[esc('🟢 已启用'),s2.filter(x=>x.enabled)],[esc('⚪ 未启用'),s2.filter(x=>!x.enabled)]];
  let html='';
  groups.forEach(function(g){
    const label=g[0],arr=g[1];
    if(arr.length===0)return;
    html+='<details class="site" open><summary>'+label+'（'+arr.length+'）</summary>';
    html+=arr.map(function(x){
      return '<div class="siterow">'+
        '<label class="sw"><input type="checkbox" '+(x.enabled?'checked':'')+' onchange="toggleSite(\''+x.name.replace(/'/g,"\\'")+'\',this.checked)"></label>'+
        '<b>'+esc(x.name)+'</b>'+
        '<span class="muted" style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">'+esc(x.note)+'</span>'+
        '<button class="btn gh" onclick="siteDetail(\''+x.name.replace(/'/g,"\\'")+'\')">详情</button>'+
        '<button class="btn gh" style="color:#dc2626" onclick="deleteSite(\''+x.name.replace(/'/g,"\\'")+'\')">🗑 删除</button>'+
        '</div><div class="sitedtl" id="dtl_'+esc(x.name)+'"></div>';
    }).join('');
    html+='</details>';
  });
  el('siteList').innerHTML=html||'<span class="muted">无匹配站点</span>';
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
  if(!confirm('将对可用资源站并发检测（并发 '+((window.siteConc)||8)+'，失败站点自动标记失效并隐藏）。确定执行？'))return;
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
            return '<div class="siterow"><b>'+esc(x.name)+'</b>'+
              '<span style="'+(x.usable?'color:#16a34a':'color:#dc2626')+';font-weight:700">'+(x.usable?'✓ 可用':'✗ 失效')+'</span>'+
              '<span class="muted" style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">'+esc(x.message)+' · '+x.response_ms+'ms</span></div>';
          }).join('')||'<span class="muted">等待结果…</span>';
        if(d.finished){showProg(false);setTimeout(function(){loadSites();},600);return;}
        setTimeout(poll,600);
      }).catch(function(e){el('chkInfo').textContent='进度获取失败: '+esc(e.message);showProg(false);});
    })();
  }catch(e){el('siteList').innerHTML='<span class="muted">检测失败: '+esc(e.message)+'</span>';showProg(false);}
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
      const v0=j.videos[0];
      v+='<div class="muted" style="margin-top:6px">搜索「'+esc(kw)+'」命中 '+j.count+' 条，示例：'+esc(v0.name)+'</div>'+
         '<div style="margin-top:4px"><code>'+esc(v0.first_url)+'</code></div>'+
         '<button class="btn mini" onclick="copyText(this,decodeURIComponent(\''+encodeURIComponent(v0.first_url)+'\'))">复制播放链接</button>';
    }else{
      v+='<div class="muted" style="margin-top:6px">未命中：该站点无结果或已失效，可换测试词再点详情</div>';
    }
    box.innerHTML=v;
  }catch(e){box.innerHTML='<span class="muted">查询失败: '+esc(e.message)+'</span>';}
}
function copyText(btn,text){
  if(navigator.clipboard&&navigator.clipboard.writeText){
    navigator.clipboard.writeText(text).then(function(){
      btn.textContent='已复制 ✓';setTimeout(function(){btn.textContent='复制播放链接';},1500);
    }).catch(function(){fallbackCopy(text,btn);});
  }else{fallbackCopy(text,btn);}
}
function fallbackCopy(text,btn){
  const t=document.createElement('textarea');t.value=text;document.body.appendChild(t);t.select();
  try{document.execCommand('copy');btn.textContent='已复制 ✓';setTimeout(function(){btn.textContent='复制播放链接';},1500);}catch(e){}
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
  .api-meta{min-width:150px;flex-shrink:0}
  .api-meta b{color:#581c87;font-size:13.5px}
  .api-meta .d{font-size:11.5px;color:#9ca3af;margin-top:3px;line-height:1.5}
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
var APIS=[
  {n:'去广告 M3U8',d:'传入 m3u8 链接，返回过滤后无广告 M3U8 纯文本，可直接播放',p:'/api/clean?url=<m3u8链接>'},
  {n:'去广告 JSON',d:'同上去广告，返回 JSON（统计 + 过滤后文本 + 广告明细）',p:'/api/clean/json?url=<m3u8链接>'},
  {n:'官替链路',d:'官方视频页链接 → 资源站匹配 → 返回无广告直链',p:'/api/replace?url=<官方视频页链接>'},
  {n:'影视/TVBox 兼容',d:'影视 App / TVBox 等通用解析接口（JSON）',p:'/api/jx?url=<播放链接>'},
  {n:'运行统计',d:'接口调用次数 / 广告统计 / 运行时长（JSON）',p:'/api/stats'},
  {n:'健康检查',d:'服务存活状态与版本号',p:'/healthz'}
];
function renderApis(){
  var base=location.origin;
  el('apiList').innerHTML=APIS.map(function(a){
    var u=base+a.p;
    var c='curl -s "'+u+'"';
    return '<div class="api-row">'+
      '<div class="api-meta"><b>'+a.n+'</b><div class="d">'+a.d+'</div></div>'+
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
// 用短超时的独立客户端，避免连通性差时长时间阻塞 HTTP 请求导致前端 Failed to fetch
var manifestHTTP = &http.Client{Timeout: 8 * time.Second}

func fetchManifest() (*UpdateManifest, error) {
	resp, err := manifestHTTP.Get(manifestRawURL)
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
// 修复「更新后不自动重启」：
//   1) 先关闭当前 HTTP 服务释放端口，再启动新进程，避免新进程绑定失败(Address already in use)直接退出；
//   2) 新进程用 Setsid 脱离当前会话，避免老进程退出时 SIGHUP 把新进程一起带走（nohup/setsid 部署场景）。
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
	// 1) 停当前服务，释放监听端口（异步 Shutdown，避免阻塞正在处理本请求的连接）
	listenAddr := ""
	if httpServer != nil {
		listenAddr = httpServer.Addr
		if listenAddr != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			go httpServer.Shutdown(ctx) // 关闭 listener 并等待活跃连接结束
			waitPortFree(listenAddr, 8*time.Second)
			_ = cancel
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
	time.Sleep(200 * time.Millisecond)
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

func detectPlatform(raw string) string {
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

func fetchVideoTitle(raw, vidHint string) string {
	title := ""
	if body, err := newClient().fetch(raw); err == nil {
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
	// 腾讯无标题时用 getinfo 兜底取正式片名
	if title == "" && vidHint != "" {
		gURL := "https://vv.video.qq.com/getinfo?vid=" + url.QueryEscape(vidHint) +
			"&platform=101001&charge=0&otype=json&defn=shd&sdtfrom=v1010&host=v.qq.com"
		if body, err := newClient().fetch(gURL); err == nil {
			if m := regexp.MustCompile(`"ti"\s*:\s*"([^"]+)"`).FindStringSubmatch(body); len(m) > 1 {
				title = m[1]
			}
		}
	}
	return title
}

var epCNRe = regexp.MustCompile(`第\s*([0-9]+|[一二三四五六七八九十百千]+)\s*(集|期|话)`)
var seasonCNRe = regexp.MustCompile(`第\s*([0-9]+|[一二三四五六七八九十百千]+)\s*(季|部|篇|卷|番)`)
var sxxexxRe = regexp.MustCompile(`(?i)S(\d+)\s*E(\d+)`)
var qRe = regexp.MustCompile(`(?i)第[一-九零一二三四五六七八九十百千0-9]+[季部篇卷番集期话]|S\d+E\d+|全集|完结|高清|蓝光|4K|1080P|720P`)
var cleanTagRe = regexp.MustCompile(`[（(]?(第[一-九零一二三四五六七八九十百千0-9]+[季部篇卷番集期话]|S\d+E\d+|全集|完结)[）)]?`)

func cnToNum(s string) int {
	digits := map[rune]int{'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
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

func parseVideoTitle(title string) VideoInfo {
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
	VodID        string `json:"vod_id"`
	VodName      string `json:"vod_name"`
	VodPic       string `json:"vod_pic"`
	VodRemarks   string `json:"vod_remarks"`
	VodPlayURL   string `json:"vod_play_url"`
	VodPlayFrom  string `json:"vod_play_from"`
	Name         string `json:"name"`
	PlayURL      string `json:"play_url"`
	Pic          string `json:"pic"`
	Remarks      string `json:"remarks"`
	VodID2       string `json:"id"`
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

func httpGetBody(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", u)
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

func episodeNumOfPlayItem(name string) int {
	m := regexp.MustCompile(`第\s*(\d+)\s*[集期话]|EP?\s*(\d+)|E(\d+)\s*$`).FindStringSubmatch(name)
	for _, g := range m[1:] {
		if n, err := strconv.Atoi(g); err == nil && n > 0 {
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
	items := []maccmsItem{}
	var r maccmsResp
	if err := json.Unmarshal(b, &r); err == nil && (len(r.List) > 0 || len(r.Data) > 0) {
		items = append(items, r.List...)
		items = append(items, r.Data...)
	} else if x := parseStdXMLItems(b); x != nil {
		items = x
	} else if x := parseXgXMLItems(b); x != nil {
		items = x
	} else {
		return nil, fmt.Errorf("响应解析失败（非 JSON/XML）")
	}
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
			Site: s.Name, PlayFrom: it.VodPlayFrom, URLs: urls, FirstURL: urls[0].URL,
		})
	}
	if len(vs) == 0 {
		return nil, fmt.Errorf("无有效视频")
	}
	return vs, nil
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
	for _, v := range videos {
		cand := parseVideoTitle(v.Name)
		s := titleSim(vi.BaseTitle, cand.BaseTitle)
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
	UsedKeyword    string        `json:"used_keyword,omitempty"`
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
	if title == "" {
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

	// 生成搜索关键词：优先「基础剧名+集数」
	kws := []string{vi.BaseTitle}
	if vi.EpisodeNum > 0 {
		kws = []string{vi.BaseTitle + " 第" + strconv.Itoa(vi.EpisodeNum) + "集", vi.BaseTitle}
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
	steps = append(steps, replaceStep{"search", "资源站搜索", "ok",
		fmt.Sprintf("命中站点 %s · score=%.1f · 关键词 %s", best.Video.Site, best.Score, used)})

	m3u8, _ := pickEpisodeURL(best.Video, vi.EpisodeNum)
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
	res.ADSkipURL = playURL(r, "/api/clean", url.Values{"url": {m3u8}})
	steps = append(steps, replaceStep{"output", "组装输出", "ok", "ad_skip_url 已生成（经 /api/clean 去广告）"})

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
	})
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
	sr := searchSites(cfg, kw, 40)
	var vs []ResourceVideo
	for _, v := range sr.Videos {
		if v.Site == name {
			vs = append(vs, v)
		}
	}
	recordCall("/api/sites/test", name+" wd="+kw, len(vs) > 0, 0, fmt.Sprintf("命中 %d 条", len(vs)))
	writeJSON(w, map[string]interface{}{
		"success": len(vs) > 0, "site": name, "keyword": kw,
		"count": len(vs), "videos": vs,
		"site_ok": sr.SiteOK, "site_fail": sr.SiteFail, "searched": sr.Searched,
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
	Name       string `json:"name"`
	SiteURL    string `json:"site_url"`
	Usable     bool   `json:"usable"`
	Blocked    bool   `json:"blocked"`
	Message    string `json:"message"`
	ResponseMS int64  `json:"response_ms"`
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
	task := &siteCheckTask{ID: newTaskID(), Keyword: kw, StartTime: time.Now(), statusMap: map[string]bool{}, failMsg: map[string]string{}}
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
			t0 := time.Now()
			vs, err := searchSiteOne(s, kw)
			ms := time.Since(t0).Milliseconds()
			ok := err == nil && len(vs) > 0
			item := checkResultItem{Name: s.Name, SiteURL: s.SiteURL, ResponseMS: ms}
			if ok {
				item.Usable = true
				item.Message = "正常"
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
				if strings.Contains(cfg2.Sites[i].Note, "自动屏蔽") {
					cfg2.Sites[i].Note = ""
				}
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

// handleJX JSON 通用兼容接口（供影视 / TVBox / 盒子等调用）
//   GET /api/jx?url=<m3u8|mp4|官方视频页>&engine=basic|auto|ai
//   返回 {code:0/1, success, msg, url(可播放/去广告地址), full, play, name, pic, header, format}
//   url 用请求 Host 动态拼接，不硬编码；响应已带全局 CORS 头。
func handleJX(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	raw := r.URL.Query().Get("url")
	engine := r.URL.Query().Get("engine")
	resp := map[string]interface{}{
		"code": 0, "success": false, "msg": "参数缺失", "url": "",
		"play": "", "full": "", "name": "", "pic": "", "header": "", "format": "",
	}
	if raw == "" {
		writeJSON(w, resp)
		return
	}
	f := detectDirectFormat(raw)
	ok := false
	msg := ""
	switch f {
	case "m3u8":
		res := cleanOne(raw, false, engine)
		if res.Success {
			ad := playURL(r, "/api/clean", url.Values{"url": {raw}})
			resp["url"], resp["full"], resp["play"], resp["format"] = ad, ad, raw, "m3u8"
			resp["name"] = res.Message
			ok = true
		} else {
			msg = res.Message
		}
	case "direct":
		resp["url"], resp["full"], resp["play"], resp["format"] = raw, raw, raw, "direct"
		ok, msg = true, "ok"
	default:
		// 官方视频页 → 官替链路（自动得到基于本机 Host 的无广告直链）
		rr := replaceOne(r, raw)
		if rr.Success && rr.ADSkipURL != "" {
			resp["url"], resp["full"], resp["play"] = rr.ADSkipURL, rr.ADSkipURL, rr.M3U8URL
			resp["format"], resp["name"], resp["pic"], resp["remarks"] = "m3u8", rr.VideoName, rr.VideoPic, rr.VideoRemarks
			ok = true
		} else {
			msg = rr.Message
		}
	}
	if ok {
		resp["code"], resp["success"], resp["msg"] = 1, true, "ok"
	} else {
		resp["code"], resp["msg"] = 0, msg
	}
	recordCall("/api/jx", raw, ok, time.Since(start).Seconds()*1000, resp["msg"].(string))
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
	// AI 去广告配置查看/更新
	http.HandleFunc("/api/ai/config", handleAIConfig)
	// 官替链路：官方视频页 → 资源站 → 无广告 m3u8
	http.HandleFunc("/api/replace", handleReplace)
	// JSON 通用兼容接口（影视 / TVBox / 盒子等调用）
	http.HandleFunc("/api/jx", handleJX)
	// 资源站管理（需登录）
	http.HandleFunc("/api/sites", guard(handleSitesList))
	http.HandleFunc("/api/sites/toggle", guard(handleSiteToggle))
	http.HandleFunc("/api/sites/test", guard(handleSiteTest))
	http.HandleFunc("/api/sites/add", guard(handleSiteAdd))
	http.HandleFunc("/api/sites/delete", guard(handleSiteDelete))
	http.HandleFunc("/api/sites/check", guard(handleSiteCheck))
	http.HandleFunc("/api/sites/check/progress", guard(handleSiteCheckProgress))
	log.Printf("MXGT-Go %s listening on %s (M3U8 去广告 + 官替链路服务)", AppVersion, *addr)
	// 使用全局 httpServer 句柄：更新重启时可优雅关闭释放端口（修复更新后不自动重启）
	httpServer = &http.Server{Addr: *addr, Handler: withCORS(http.DefaultServeMux)}
	// 端口占用自动重试：避免「更新/重启后不能启动」（旧进程未退出 / TIME_WAIT / 端口被其它程序占用）
	const maxBindRetry = 30
	for i := 0; i < maxBindRetry; i++ {
		err := httpServer.ListenAndServe()
		if err == nil || err == http.ErrServerClosed {
			return // 被优雅关闭（如更新重启），退出
		}
		if strings.Contains(err.Error(), "address already in use") {
			log.Printf("端口 %s 被占用，2s 后重试(%d/%d)...", *addr, i+1, maxBindRetry)
			time.Sleep(2 * time.Second)
			continue
		}
		log.Fatal(err)
	}
	log.Fatalf("启动失败：端口 %s 在 %d 次重试后仍被占用，请先释放端口", *addr, maxBindRetry)
}
