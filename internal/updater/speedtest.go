package updater

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// SpeedResult 镜像测速结果
type SpeedResult struct {
	URL        string `json:"url"`
	LatencyMS  int64  `json:"latency_ms"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"status_code,omitempty"`
}

// DefaultMirrors 内置镜像列表
func DefaultMirrors() []string {
	return []string{
		"https://github.com/ssmhdssmhd/MXGT",
		"https://ghproxy.com/https://github.com/ssmhdssmhd/MXGT",
		"https://gh-proxy.com/https://github.com/ssmhdssmhd/MXGT",
		"https://mirror.ghproxy.cn/https://github.com/ssmhdssmhd/MXGT",
		"https://kkgithub.com/ssmhdssmhd/MXGT",
		"https://hub.fastgit.org/ssmhdssmhd/MXGT",
	}
}

// BenchmarkMirrors 并发测速所有镜像，返回按延迟升序的结果（不可达排最后）
func BenchmarkMirrors(ctx context.Context, mirrors []string, timeout time.Duration) []SpeedResult {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if len(mirrors) == 0 {
		mirrors = DefaultMirrors()
	}
	results := make([]SpeedResult, len(mirrors))
	var wg sync.WaitGroup
	for i, m := range mirrors {
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			results[i] = speedTest(ctx, url, timeout)
		}(i, m)
	}
	wg.Wait()
	// 稳定排序：可达的按延迟升序，不可达放最后
	n := len(results)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			swap := false
			if results[j].Reachable && !results[i].Reachable {
				swap = true
			} else if results[i].Reachable == results[j].Reachable {
				if results[i].Reachable && results[j].LatencyMS < results[i].LatencyMS {
					swap = true
				}
			}
			if swap {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results
}

// Fastest 返回最快可达镜像，全部不可达时返回空串
func Fastest(results []SpeedResult) string {
	for _, r := range results {
		if r.Reachable {
			return r.URL
		}
	}
	return ""
}

func speedTest(ctx context.Context, url string, timeout time.Duration) SpeedResult {
	r := SpeedResult{URL: url}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodHead, url, nil)
	if err != nil {
		return r
	}
	req.Header.Set("User-Agent", "MXGT-Updater/"+Version)
	start := time.Now()
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return r
	}
	defer resp.Body.Close()
	r.LatencyMS = time.Since(start).Milliseconds()
	r.StatusCode = resp.StatusCode
	// HEAD 可能被禁用，回退 GET 部分检测
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		r.Reachable = true
	}
	return r
}

// Version 供 UA 使用（运行时由 handler 注入）
var Version = "v0.0.18"