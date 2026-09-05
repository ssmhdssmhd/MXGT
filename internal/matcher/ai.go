// Package matcher AI 匹配通道：通过 OpenAI 兼容的 chat/completions 接口判断剧名是否匹配。
package matcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient AI 匹配客户端（对接 openai / doubao 等 OpenAI 兼容接口的 chat/completions）
type OpenAIClient struct {
	APIKey   string
	Endpoint string
	Model    string
	Timeout  time.Duration
}

// ChatMessage 对话消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest / chatResponse OpenAI 兼容请求与响应
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// NewOpenAIClient 创建实测用 AI 客户端；endpoint 为空时用默认。
func NewOpenAIClient(apiKey, endpoint, model string) *OpenAIClient {
	return &OpenAIClient{
		APIKey:   apiKey,
		Endpoint: strings.TrimRight(endpoint, "/"),
		Model:    model,
		Timeout:  15 * time.Second,
	}
}

// Match 调用 AI 判断 srcName 是否即 targetName（含别名）。
// 返回相似度 0-100 和是否匹配。未配置 key / endpoint 时降级用规则比较，不报错。
func (c *OpenAIClient) Match(srcName, targetName string, aliases []string) (int, bool, error) {
	if c == nil || c.APIKey == "" || c.Endpoint == "" {
		// 降级：规则匹配兜底
		score := int(Similarity(Normalize(srcName), Normalize(targetName)) * 100)
		return score, MatchName(srcName, targetName, aliases, 85), nil
	}

	aliasText := ""
	if len(aliases) > 0 {
		aliasText = "\n别名：[" + strings.Join(aliases, ", ") + "]"
	}
	prompt := fmt.Sprintf("判断剧名「%s」是否与目标剧名「%s」为同一部作品%s。只回复一个JSON：{\"score\":0-100整数,\"matched\":true或false}", srcName, targetName, aliasText)

	req := chatRequest{
		Model: c.Model,
		Messages: []ChatMessage{
			{Role: "system", Content: "你是视频聚合平台的剧名匹配专家，只输出JSON，不要输出其他文字。"},
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, false, fmt.Errorf("构造AI请求失败: %w", err)
	}

	url := c.Endpoint
	if !strings.HasSuffix(url, "/chat/completions") {
		url = url + "/chat/completions"
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := &http.Client{Timeout: c.Timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, false, fmt.Errorf("AI请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("AI接口非200: %d %s", resp.StatusCode, string(data))
	}

	var chat chatResponse
	if err := json.Unmarshal(data, &chat); err != nil {
		return 0, false, fmt.Errorf("AI响应解析失败: %w", err)
	}
	if chat.Error != nil && chat.Error.Message != "" {
		return 0, false, fmt.Errorf("AI返回错误: %s", chat.Error.Message)
	}
	if len(chat.Choices) == 0 {
		return 0, false, fmt.Errorf("AI无返回内容")
	}
	content := strings.TrimSpace(chat.Choices[0].Message.Content)

	// 提取 JSON（可能被 ``` 包裹）
	content = trimCodeFence(content)
	var out struct {
		Score   int  `json:"score"`
		Matched bool `json:"matched"`
	}
	// 兜底：即便 JSON 解析失败，也尝试是否明确含 matched:true 字样
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		if strings.Contains(content, `"matched": true`) || strings.Contains(content, `"matched":true`) {
			out.Matched = true
			out.Score = 100
		}
	}
	if out.Score <= 0 {
		out.Score = map[bool]int{true: 100, false: 0}[out.Matched]
	}
	_ = content
	return out.Score, out.Matched, nil
}

func trimCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}