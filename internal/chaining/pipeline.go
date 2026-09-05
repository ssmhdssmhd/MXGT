// Package chaining 调用 Pipeline 引擎（M13）：按配置顺序串联多个节点，支持回退策略与中间结果提取。
package chaining

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 节点回退策略常量
const (
	FallbackSkip     = "skip"     // 出错跳过，继续下一节点
	FallbackAbort    = "abort"    // 出错终止整条链路
	FallbackFallback = "fallback" // 出错用兜底地址（fallback_to 或上一环节结果）
)

// Node 是 Pipeline 可执行节点的运行时视图（DB 记录 + 预处理）
type Node struct {
	ID         uint
	Name       string
	Type       string // proxy / custom / skip_ad / block_ad
	Endpoint   string
	Method     string
	Headers    map[string]string
	ResultPath string // 从响应提取结果的 JSONPath，如 $.data.url
	Fallback   string
	FallbackTo string
	Order      int
}

// StepResult 单节点执行结果（用于 /admin/chain/test 展示中间结果）
type StepResult struct {
	Order   int    `json:"order"`
	NodeID  uint   `json:"node_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Handled bool   `json:"handled"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// Result Pipeline 整条链路执行结果
type Result struct {
	Input   string       `json:"input"`
	Output  string       `json:"output"`
	OK      bool         `json:"ok"`
	Message string       `json:"message"`
	Steps   []StepResult `json:"steps"`
}

// Pipeline 执行引擎
type Pipeline struct {
	// httpDo 便于测试注入；nil 时用 http.DefaultClient
	httpDo func(ctx context.Context, req *http.Request) (*http.Response, error)
}

// New 创建 Pipeline 引擎
func New() *Pipeline {
	return &Pipeline{}
}

// ExactType 节点类型
const (
	NodeProxy   = "proxy"
	NodeCustom  = "custom"
	NodeSkipAd  = "skip_ad"
	NodeBlockAd = "block_ad"
)

// Execute 按顺序执行整条链路。
// input 为当前 URL/结果；nodes 需已按 Order 升序且 Enabled=1。
func (p *Pipeline) Execute(ctx context.Context, input string, nodes []Node) *Result {
	res := &Result{Input: input, Output: input, OK: true, Message: "success"}
	if strings.TrimSpace(input) == "" {
		res.OK = false
		res.Message = "input 为空"
		return res
	}

	current := input
	for _, n := range nodes {
		step := StepResult{Order: n.Order, NodeID: n.ID, Name: n.Name, Type: n.Type}
		out, err := p.runNode(ctx, current, n)
		if err != nil {
			// 按回退策略处理
			switch n.Fallback {
			case FallbackAbort:
				step.Error = err.Error()
				res.OK = false
				res.Message = "节点 " + n.Name + " 出错并中止整条链路"
				res.Steps = append(res.Steps, step)
				return res
			case FallbackFallback:
				if strings.TrimSpace(n.FallbackTo) != "" {
					out = n.FallbackTo
				} else {
					out = current // 用上一环节结果
				}
				step.Error = "回退： " + err.Error()
			default: // skip
				out = current
				step.Error = "跳过： " + err.Error()
			}
		}

		if out != "" {
			current = out
		}
		step.Handled = true
		step.Output = current
		res.Steps = append(res.Steps, step)
	}

	res.Output = current
	return res
}

// runNode 执行单个节点，返回新的输出结果。
func (p *Pipeline) runNode(ctx context.Context, current string, n Node) (string, error) {
	switch n.Type {
	case NodeProxy:
		// 代理节点：实际由后端 /api/proxy/stream 转发；这里输出原样（proxy 由播放端发起）
		return current, nil
	case NodeSkipAd, NodeBlockAd:
		// 去插播/去广告：依赖 M16 AI 智能分析模块，此处先透传
		return current, nil
	case NodeCustom:
		// 通用 HTTP 节点：替换 {input_url}，请求后按 result_path 提取
		if n.Endpoint == "" {
			return "", fmt.Errorf("custom 节点未配置 endpoint")
		}
		url := strings.ReplaceAll(n.Endpoint, "{input_url}", current)
		return p.httpCall(ctx, n, url)
	default:
		return "", fmt.Errorf("未知节点类型: %s", n.Type)
	}
}

// httpCall 发起 HTTP 请求并按 result_path 从 JSON 响应提取结果
func (p *Pipeline) httpCall(ctx context.Context, n Node, url string) (string, error) {
	method := n.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return "", err
	}
	for k, v := range n.Headers {
		req.Header.Set(k, v)
	}
	do := p.httpDo
	if do == nil {
		do = func(ctx context.Context, r *http.Request) (*http.Response, error) {
			client := &http.Client{Timeout: 15 * time.Second}
			return client.Do(r)
		}
	}
	resp, err := do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP 非200: %d %s", resp.StatusCode, string(data))
	}

	// 未配 result_path：返回原始响应文本（若 URL 可直接作为结果的场景）
	if n.ResultPath == "" {
		body := strings.TrimSpace(string(data))
		if body == "" {
			return "", fmt.Errorf("响应为空")
		}
		return body, nil
	}

	// 按 JSONPath 提取
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("响应不是合法 JSON: %w", err)
	}
	v, err := JSONPathGet(n.ResultPath, doc)
	if err != nil {
		return "", fmt.Errorf("JSONPath %s 提取失败: %w", n.ResultPath, err)
	}
	s, ok := v.(string)
	if !ok {
		b, _ := json.Marshal(v)
		s = string(b)
	}
	if s == "" {
		return "", fmt.Errorf("JSONPath %s 提取为空", n.ResultPath)
	}
	return s, nil
}