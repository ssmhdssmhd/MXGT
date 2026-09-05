package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ReleaseInfo 检查更新结果
type ReleaseInfo struct {
	Version    string `json:"version"`
	Name       string `json:"name"`
	Published  string `json:"published_at"`
	Body       string `json:"body"`
	DownloadURL string `json:"download_url"`
	HasUpdate  bool   `json:"has_update"`
	Current    string `json:"current"`
}

// CheckLatest 通过 GitHub API 获取最新 Release 并对比当前版本。
// repo 形如 https://github.com/owner/name 或 owner/name。
func CheckLatest(ctx context.Context, repo, current string) (*ReleaseInfo, error) {
	owner, name := parseRepo(repo)
	if owner == "" || name == "" {
		return nil, fmt.Errorf("无法解析仓库地址: %s", repo)
	}
	api := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, name)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MXGT-Updater/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var rel struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("解析 Release 失败: %w", err)
	}

	cur := semverOrZero(current)
	latest, err := Parse(rel.TagName)
	if err != nil {
		latest = cur
	}

	info := &ReleaseInfo{
		Version:   rel.TagName,
		Name:      rel.Name,
		Published: rel.PublishedAt,
		Body:      rel.Body,
		Current:   current,
	}
	if info.Version == "" {
		info.Version = rel.TagName
	}
	info.DownloadURL = releaseReleasesURL(owner, name, rel.TagName)
	info.HasUpdate = latest.GreaterThan(cur)
	return info, nil
}

// Notice 解析公告文本 → 提取版本号/日期/内容（简易实现）
type Notice struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Content string `json:"content"`
}

var noticeRe = regexp.MustCompile(`v?(\d+\.\d+(?:\.\d+)?)`)

// ParseNotice 从公告文本中解析出版本信息
func ParseNotice(text string) Notice {
	n := Notice{Content: strings.TrimSpace(text)}
	if m := noticeRe.FindString(text); m != "" {
		n.Version = strings.TrimPrefix(m, "v")
		if !strings.HasPrefix(m, "v") {
			n.Version = "v" + m
		}
	}
	// 尝试匹配日期 yyyy-mm-dd / yyyy/mm/dd
	dateRe := regexp.MustCompile(`\d{4}[-\/]\d{1,2}[-\/]\d{1,2}`)
	if d := dateRe.FindString(text); d != "" {
		n.Date = d
	}
	return n
}

func parseRepo(repo string) (string, string) {
	s := strings.TrimRight(strings.TrimSpace(repo), "/")
	// 去掉代理前缀（如 https://ghproxy.com/https://github.com/）
	if strings.Contains(s, "github.com/") {
		s = s[strings.Index(s, "github.com/")+len("github.com/"):]
	}
	parts := strings.Split(s, "/")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	parts = strings.Split(s, ":")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func releaseReleasesURL(owner, name, tag string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/", owner, name, tag)
}

func semverOrZero(s string) SemVer {
	v, err := Parse(s)
	if err != nil {
		return SemVer{}
	}
	return v
}