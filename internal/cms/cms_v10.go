// Package cms 苹果 CMS v10 适配：数据结构 + vod 组装（多线路输出）
package cms

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ssmhdssmhd/MXGT/internal/models"
)

// CMSVod 苹果 CMS v10 单条影片（JSON 字段对齐官方 schema）
type CMSVod struct {
	VodID       uint   `json:"vod_id"`
	VodName     string `json:"vod_name"`
	TypeName    string `json:"type_name"`
	VodYear     string `json:"vod_year"`
	VodArea     string `json:"vod_area"`
	VodActor    string `json:"vod_actor"`
	VodDirector string `json:"vod_director"`
	VodContent  string `json:"vod_content"`
	VodPic      string `json:"vod_pic"`
	VodRemarks  string `json:"vod_remarks"`
	VodPlayFrom string `json:"vod_play_from"` // 多线路来源，$$$ 分隔
	VodPlayURL  string `json:"vod_play_url"`  // 多线路播放串，$$$ 分隔
}

// ListResponse 列表/搜索/详情统一响应
type ListResponse struct {
	Code      int      `json:"code"`
	Msg       string   `json:"msg"`
	Page      int      `json:"page"`
	PageCount int      `json:"pagecount"`
	Limit     string   `json:"limit"`
	Total     int      `json:"total"`
	List      []CMSVod `json:"list"`
}

// PlayResponse play 接口响应
type PlayResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	URL  string `json:"url"`
}

// ToCMSVod 将内部 vod（含 episodes）组装为苹果 CMS v10 格式。
// 多源集数按 source_name 分组 → vod_play_from 与 vod_play_url 用 $$$ 对齐。
func ToCMSVod(v *models.Vod) CMSVod {
	// 按来源分组并保序
	groups := make(map[string][]models.Episode)
	order := make([]string, 0)
	for _, e := range v.Episodes {
		if _, ok := groups[e.SourceName]; !ok {
			order = append(order, e.SourceName)
		}
		groups[e.SourceName] = append(groups[e.SourceName], e)
	}

	froms := make([]string, 0, len(order))
	urls := make([]string, 0, len(order))
	for _, src := range order {
		eps := groups[src]
		sort.Slice(eps, func(i, j int) bool { return eps[i].EpisodeNo < eps[j].EpisodeNo })

		froms = append(froms, src)
		var sb strings.Builder
		for i, e := range eps {
			if i > 0 {
				sb.WriteString("#")
			}
			name := e.EpisodeName
			if name == "" {
				name = fmt.Sprintf("第%d集", e.EpisodeNo)
			}
			sb.WriteString(name)
			sb.WriteString("$")
			sb.WriteString(e.SourceURL)
		}
		urls = append(urls, sb.String())
	}

	return CMSVod{
		VodID:       v.ID,
		VodName:     v.Name,
		TypeName:    v.Category,
		VodYear:     strconv.Itoa(v.Year),
		VodArea:     v.Region,
		VodPic:      v.Cover,
		VodRemarks:  v.Remark,
		VodPlayFrom: strings.Join(froms, "$$$"),
		VodPlayURL:  strings.Join(urls, "$$$"),
	}
}

// BuildPlayURL 从单条播放串中提取某集（ep）的真实 URL。
// playURL 为单线路 "第1集$url1#第2集$url2"；ep<=0 返回整串。
func BuildPlayURL(playURL string, ep int) string {
	if ep <= 0 || playURL == "" {
		return playURL
	}
	for _, seg := range strings.Split(playURL, "#") {
		parts := strings.SplitN(seg, "$", 2)
		if len(parts) != 2 {
			continue
		}
		if containsEpisodeNo(parts[0], ep) {
			return parts[1]
		}
	}
	return playURL
}

// containsEpisodeNo 判断集名是否等于指定集数（第N集 / EP N / 纯数字）
func containsEpisodeNo(name string, ep int) bool {
	name = strings.TrimSpace(name)
	if name == strconv.Itoa(ep) {
		return true
	}
	trim := strings.TrimPrefix(name, "第")
	trim = strings.TrimSuffix(trim, "集")
	trim = strings.TrimSuffix(trim, "话")
	trim = strings.TrimSuffix(trim, "期")
	trim = strings.TrimSpace(trim)
	return trim == strconv.Itoa(ep)
}
