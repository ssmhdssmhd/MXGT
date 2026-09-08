// MXGT-Go v0.1.0 — M3U8 广告分析与去广告单文件服务
//
// 单文件、标准库零依赖：HTTP 服务接收 m3u8 链接，抓取-解析-保守广告检测-输出无广告 M3U8。
//
// 接口：
//   GET /api/clean?url=<m3u8>       → 过滤后的无广告 M3U8 纯文本（绝对地址）
//   GET /api/clean/json?url=<m3u8>  → JSON：统计 + 过滤后文本 + 广告片段明细
//   GET /healthz                    → 健康检查
//
// 广告检测（保守防误删，命中即高置信才删）：
//   1. URL 关键词（/ad/、_ad、ad0、300x250、tvc、promo 等）
//   2. 广告标签区间（EXT-X-DATERANGE / CUE-OUT / EXT-X-AD 声明）
//   3. 超短视频（duration < 1.0s）
//   4. opt=aggresive 时启用「同目录统一切片聚类」批量识别（可能误伤统一切片正片，默认关闭）
//
// 编译：go build -ldflags "-s -w" -o mxgt-go main.go
// 部署：./mxgt-go -addr :8080

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	AppVersion = "v0.1.0"
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

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "ok %s\n", AppVersion)
	})
	http.HandleFunc("/api/clean", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		aggr := r.URL.Query().Get("opt") == "aggresive"
		res := cleanOne(u, aggr)
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
		writeJSON(w, res)
	})
	log.Printf("MXGT-Go %s listening on %s (单文件 M3U8 去广告服务)", AppVersion, *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}
