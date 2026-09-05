package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Config 分析参数
type Config struct {
	Concurrency int   // 并发下载数
	MaxSegments int   // 最多分析分片（0=不限）
	HeuristicOn bool  // 是否启用启发式
	MaxBytes    int64 // 单分片最大下载字节（防止超大广告段拖慢）
}

// FingerprintStore MD5 指纹库（由 handler 用 DB 实现）
type FingerprintStore interface {
	// Lookup 返回该 MD5 的已知判定；未命中返回 ""
	Lookup(md5 string) string
	// Record 记录新指纹
	Record(src string, seg Segment) error
}

// SegmentResult 单个分片分析结果
type SegmentResult struct {
	Segment
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason,omitempty"`
}

// AnalysisResult 整次分析结果
type AnalysisResult struct {
	Source    string          `json:"source"`
	Total     int             `json:"total"`
	Bad       int             `json:"bad"`
	OK        bool            `json:"ok"`
	Message   string          `json:"message"`
	Results   []SegmentResult `json:"results"`
	SkipSet   map[string]bool `json:"-"` // 需剔除的 MD5 集合
	TargetDur float64         `json:"target_duration"`
}

// Analyzer 分析器
type Analyzer struct {
	Store FingerprintStore
	HTTP  *http.Client
}

// NewAnalyzer 创建分析器
func NewAnalyzer(store FingerprintStore) *Analyzer {
	return &Analyzer{Store: store, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Analyze 解析并分析 m3u8。onProgress 可选，回调进度。
func (a *Analyzer) Analyze(ctx context.Context, m3u8, sourceName string, cfg Config, onProgress func(done, total int)) (*AnalysisResult, error) {
	pl, err := ParseM3U8(ctx, m3u8)
	if err != nil {
		return nil, err
	}
	total := len(pl.Segments)
	res := &AnalysisResult{Source: m3u8, Total: total, TargetDur: pl.TargetDur, SkipSet: map[string]bool{}}
	if total == 0 {
		res.Message = "无分片"
		return res, nil
	}

	// 平均时长
	var durSum float64
	for _, s := range pl.Segments {
		durSum += s.Duration
	}
	avgDur := durSum / float64(total)

	limit := total
	if cfg.MaxSegments > 0 && cfg.MaxSegments < limit {
		limit = cfg.MaxSegments
	}
	conc := cfg.Concurrency
	if conc <= 0 {
		conc = 4
	}

	var mu sync.Mutex
	var done int32
	var wg sync.WaitGroup
	sem := make(chan struct{}, conc)
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(seg Segment) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := SegmentResult{Segment: seg}
			body, _, herr := a.download(ctx, seg.URL, cfg.MaxBytes)
			if herr == nil {
				seg.MD5 = MD5Bytes(body)
				if hit := a.Store.Lookup(seg.MD5); hit != "" {
					r.Skipped = Bad(hit)
					r.Verdict = hit
					r.Reason = "指纹命中 " + hit
				} else if cfg.HeuristicOn {
					if v := ClassifyHeuristic(seg.Duration, avgDur); v != VerdictUnknown {
						r.Skipped = Bad(v)
						r.Verdict = v
						r.Reason = "启发式 " + v
					} else {
						r.Verdict = VerdictNormal
					}
				} else {
					r.Verdict = VerdictNormal
				}
				_ = a.Store.Record(sourceName, seg)
			} else {
				r.Verdict = VerdictUnknown
			}

			mu.Lock()
			r.MD5 = seg.MD5
			if r.Skipped {
				res.Bad++
				res.SkipSet[seg.MD5] = true
			}
			res.Results = append(res.Results, r)
			mu.Unlock()
			n := int(atomicAddInt32(&done, 1))
			if onProgress != nil {
				onProgress(n, total)
			}
		}(pl.Segments[i])
	}
	wg.Wait()

	res.OK = true
	res.Message = "分析完成"
	return res, nil
}

func atomicAddInt32(p *int32, d int32) int32 {
	return atomic.AddInt32(p, d)
}

func (a *Analyzer) download(ctx context.Context, url string, maxBytes int64) ([]byte, int64, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 MXGT-AI")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("下载 %s 返回 %d", url, resp.StatusCode)
	}
	var reader io.Reader = resp.Body
	if maxBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBytes)
	}
	buf, err := io.ReadAll(reader)
	if err != nil {
		return nil, 0, err
	}
	return buf, int64(len(buf)), nil
}