// Package ai AI 视频智能分析（M16）：m3u8 解析 → ts 分片 → MD5 指纹 → 去广告。
package ai

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Segment 一个 ts 分片
type Segment struct {
	Seq        int     `json:"seq"`
	Duration   float64 `json:"duration"`
	URL        string  `json:"url"`
	MD5        string  `json:"md5,omitempty"`
	SizeBytes  int64   `json:"size_bytes,omitempty"`
	Verdict    string  `json:"verdict,omitempty"` // 初始为空，分析后填充
	Confidence float64 `json:"confidence,omitempty"`
}

// Playlist m3u8 解析结果
type Playlist struct {
	Source    string    `json:"source"`
	Segments  []Segment `json:"segments"`
	TargetDur float64   `json:"target_duration"`
}

// ParseM3U8 下载并解析一个 m3u8 地址或原始文本，返回分片列表。
// raw 若以 http 开头则视为地址，否则视为 m3u8 文本。
func ParseM3U8(ctx context.Context, raw string) (*Playlist, error) {
	text := raw
	base := ""
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		b, err := fetch(ctx, raw)
		if err != nil {
			return nil, err
		}
		text = string(b)
		base = baseURL(raw)
	}
	p := &Playlist{Source: raw}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	seq := 0
	var curDur float64
	endlist := false
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		switch {
		case strings.HasPrefix(ln, "#EXT-X-TARGETDURATION:"):
			p.TargetDur, _ = strconv.ParseFloat(strings.TrimPrefix(ln, "#EXT-X-TARGETDURATION:"), 64)
		case strings.HasPrefix(ln, "#EXTINF:"):
			meta := strings.TrimPrefix(ln, "#EXTINF:")
			if i := strings.Index(meta, ","); i >= 0 {
				meta = meta[:i]
			}
			curDur, _ = strconv.ParseFloat(strings.TrimSpace(meta), 64)
		case strings.HasPrefix(ln, "#"):
			if ln == "#EXT-X-ENDLIST" {
				endlist = true
			}
			continue
		default:
			// 分片 URL
			uri := ln
			if !strings.HasPrefix(uri, "http") && base != "" {
				uri = resolveURL(base, uri)
			}
			p.Segments = append(p.Segments, Segment{Seq: seq, Duration: curDur, URL: uri})
			seq++
			curDur = 0
		}
	}
	if len(p.Segments) == 0 {
		return nil, fmt.Errorf("m3u8 中未解析到任何分片")
	}
	p.TargetDur = maxF(p.TargetDur, 10)
	_ = endlist
	return p, nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 MXGT-AI")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s 返回 %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func baseURL(u string) string {
	idx := strings.Index(u, "://")
	if idx < 0 {
		return u
	}
	rest := u[idx+3:]
	if i := strings.LastIndex(rest, "/"); i >= 0 {
		return u[:idx+3+i+1]
	}
	return u
}

func resolveURL(base, rel string) string {
	if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
		return rel
	}
	// 去掉 query
	path := rel
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	// 相对当前目录
	return base + path
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// MD5Bytes 计算数据 MD5（流式）
func MD5Bytes(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// MD5Stream 流式计算 MD5，data 被读完后返回哈希
func MD5Stream(r io.Reader) (string, int64, error) {
	h := md5.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, err
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum), n, nil
}