// Package service 业务服务层
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ssmhdssmhd/MXGT/internal/ai"
	"github.com/ssmhdssmhd/MXGT/internal/analyzer"
	"github.com/ssmhdssmhd/MXGT/internal/chaining"
	"github.com/ssmhdssmhd/MXGT/internal/collector"
	"github.com/ssmhdssmhd/MXGT/internal/matcher"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// PlayResult 统一播放入口返回（两种核心功能的统一输出）
type PlayResult struct {
	URL       string   `json:"url"`                  // 最终可播放链接
	Type      string   `json:"type"`                 // hls / mp4 / flv
	Mode      string   `json:"mode"`                 // official / direct / unknown
	Source    string   `json:"source,omitempty"`     // 命中资源站的来源名
	Title     string   `json:"title,omitempty"`      // 匹配到的剧名
	Cleaned   bool     `json:"cleaned"`              // 是否 AI 去广告
	CleanM3U8 string   `json:"clean_m3u8,omitempty"` // 去广告后的干净 m3u8 文本（m3u8 且开启时）
	Steps     []string `json:"steps,omitempty"`      // 处理链路步骤（排查用）
}

// PlayService 统一播放入口（核心两种功能）
//
//	功能一（官方链接）：输入官方播放页 → 抓取剧名/集数 → 去配置的资源站搜索接口找对应资源
//	                 → 替换为资源站可播放链接 → 去插播/去广告 → 返回
//	功能二（直链）：   输入 .m3u8/.mp4/.flv → 去插播/去广告接口 → 返回最终链接
type PlayService struct {
	db      *gorm.DB
	resolve *ResolveService
}

// NewPlayService 创建统一播放入口服务
func NewPlayService(db *gorm.DB, resolve *ResolveService) *PlayService {
	return &PlayService{db: db, resolve: resolve}
}

//------ 统一入口 ------

// Play 根据输入 URL 自动分发到 官方 / 直链 / 未知 三种处理链路
func (s *PlayService) Play(ctx context.Context, rawURL, title string, ep int) (*PlayResult, error) {
	a := analyzer.Parse(rawURL)
	switch {
	case a.Matched && a.Type == analyzer.TypeDirect:
		return s.playDirect(ctx, rawURL, title, ep)
	case a.Type == analyzer.TypeOfficial:
		return s.playOfficial(ctx, rawURL, title, ep, a)
	default:
		return s.playUnknown(ctx, rawURL)
	}
}

//------ 功能二：直链资源（m3u8 / mp4 / flv）→ 去广告/去插播 → 返回 ------

func (s *PlayService) playDirect(ctx context.Context, rawURL, title string, ep int) (*PlayResult, error) {
	res := &PlayResult{URL: rawURL, Mode: "direct", Type: detectMediaType(rawURL)}
	res.Steps = []string{"识别为可播放直链: " + res.Type}

	// 去插播 / 去广告链路 + 可选 AI 去广告
	out, err := s.finish(ctx, res, rawURL)
	if err != nil {
		return res, err
	}
	_ = title
	_ = ep
	return out, nil
}

//------ 功能一：官方资源 → 抓剧名集数 → 搜资源替换 → 去广告去插播 → 返回 ------

func (s *PlayService) playOfficial(ctx context.Context, rawURL, title string, ep int, a analyzer.Result) (*PlayResult, error) {
	res := &PlayResult{Mode: "official"}
	res.Steps = []string{fmt.Sprintf("官方站点: %s(%s)", a.SiteName, a.SiteCode)}

	// 链接未携带剧名时无法可靠抓取官方页（官方站多为 JS 渲染），退化为解析原页保持可用
	keyword := strings.TrimSpace(title)
	if keyword == "" {
		res.Steps = append(res.Steps, "未提供剧名(title)，跳过资源搜索，退化为智能解析官方页")
		return s.playUnknown(ctx, rawURL)
	}
	res.Title = keyword

	// ① 遍历启用资源站，用其搜索接口找对应资源（替换播放链接）
	foundURL, sourceName, found := s.searchResource(ctx, keyword, ep)
	if found {
		res.Source = sourceName
		res.Steps = append(res.Steps, fmt.Sprintf("资源站 [%s] 命中剧名，替换播放链接", sourceName))

		// ② 命中链接若为页面而非直链，走解析路由取真实视频
		playable := foundURL
		if !isDirect(foundURL) {
			if rr, err := s.resolve.Resolve(ctx, foundURL); err == nil && rr.URL != "" {
				playable = rr.URL
				res.Steps = append(res.Steps, "解析路由: "+(linkType(rr.URL)))
			} else {
				res.Steps = append(res.Steps, "解析失败，回退使用资源站原始链接")
			}
		}

		// ③ 去插播 / 去广告链路
		return s.finish(ctx, res, playable)
	}

	// ③ 资源站未命中 → 退化为直接解析官方播放页
	res.Steps = append(res.Steps, "资源站未命中，退化为智能解析官方页")
	return s.playUnknown(ctx, rawURL)
}

// finish 对最终播放链接走去插播/去广告链路并返回
func (s *PlayService) finish(ctx context.Context, res *PlayResult, playable string) (*PlayResult, error) {
	res.Type = detectMediaType(playable)
	run, steps, err := s.buildChain()
	if err == nil {
		out := chaining.New().Execute(ctx, playable, run)
		res.Steps = append(res.Steps, steps...)
		if out.Output != "" {
			playable = out.Output
		}
	}
	res.URL = playable

	if res.Type == "hls" && s.aiSkipEnabled() {
		clean, skipped := s.aiCleanByFingerprint(ctx, playable)
		if skipped {
			res.Cleaned = true
			res.CleanM3U8 = clean
			res.Steps = append(res.Steps, "AI 去广告: 剔除广告分片，生成干净流")
		}
	}
	return res, nil
}

//------ 未知类型：走解析路由 / 直接透传 ------

func (s *PlayService) playUnknown(ctx context.Context, rawURL string) (*PlayResult, error) {
	// 若本身是直链媒体则按直链处理
	if isDirect(rawURL) {
		return s.playDirect(ctx, rawURL, "", 0)
	}
	// 尝试解析路由，失败则透传原链接
	if rr, err := s.resolve.Resolve(ctx, rawURL); err == nil && rr.URL != "" {
		res := &PlayResult{URL: rr.URL, Type: rr.Type, Mode: "unknown"}
		res.Steps = []string{"未知类型，按解析规则处理"}
		res.Steps = append(res.Steps, "命中解析规则 #"+fmt.Sprint(rr.RuleID))
		return res, nil
	}
	return &PlayResult{URL: rawURL, Type: detectMediaType(rawURL), Mode: "unknown"}, nil
}

//------ 资源站搜索（功能一核心）------

// searchResource 遍历启用的资源站，用搜索接口按剧名检索，返回匹配的剧集播放链接与来源名
func (s *PlayService) searchResource(ctx context.Context, keyword string, ep int) (url, sourceName string, found bool) {
	var sources []models.Source
	if err := s.db.Where("enabled = ?", 1).Order("priority DESC").Find(&sources).Error; err != nil {
		return "", "", false
	}
	for i := range sources {
		src := &sources[i]
		col, err := collector.New(src)
		if err != nil {
			continue
		}
		items, err := col.Fetch(ctx, keyword)
		if err != nil || len(items) == 0 {
			continue
		}
		for j := range items {
			item := &items[j]
			if !matcher.MatchName(item.Name, keyword, splitAlias(item.Alias), 85) {
				continue
			}
			// 从匹配的节目里取指定集（默认取第 1 集）
			if ev := pickEpisode(item, ep); ev != nil {
				return ev.URL, src.Name, true
			}
			if len(item.Episodes) > 0 {
				return item.Episodes[0].URL, src.Name, true
			}
		}
	}
	return "", "", false
}

// pickEpisode 从节目集数中取指定集（按 No 精确匹配；ep<=0 取第 1 集）
func pickEpisode(item *collector.RawItem, ep int) *collector.RawEpisode {
	if ep <= 0 {
		return nil
	}
	for k := range item.Episodes {
		e := &item.Episodes[k]
		if e.No == ep {
			return e
		}
	}
	// 集数编号不对齐时回退第一个
	if len(item.Episodes) > 0 {
		return &item.Episodes[0]
	}
	return nil
}

//------ 调用 Pipeline 构建 ------

func (s *PlayService) buildChain() ([]chaining.Node, []string, error) {
	var nodes []models.ChainNode
	if err := s.db.Where("enabled = ?", 1).Order("sort_order ASC, id ASC").Find(&nodes).Error; err != nil {
		return nil, nil, err
	}
	run := make([]chaining.Node, 0, len(nodes))
	var steps []string
	for _, n := range nodes {
		hdrs := map[string]string{}
		if n.HeadersJSON != "" {
			_ = json.Unmarshal([]byte(n.HeadersJSON), &hdrs)
		}
		run = append(run, chaining.Node{
			ID: n.ID, Name: n.Name, Type: n.NodeType,
			Endpoint: n.Endpoint, Method: n.Method, Headers: hdrs,
			ResultPath: n.ResultPath, Fallback: n.Fallback, FallbackTo: n.FallbackTo, Order: n.Order,
		})
		steps = append(steps, "调用节点["+n.Name+"]")
	}
	return run, steps, nil
}

//------ AI 去广告 ------

func (s *PlayService) aiSkipEnabled() bool {
	var a models.AiSetting
	if err := s.db.First(&a, 1).Error; err != nil {
		return false
	}
	return a.Enabled == 1 && a.AutoSkipAD == 1
}

// aiCleanByFingerprint 基于 MD5 指纹库剔除广告分片，返回干净 m3u8 文本
func (s *PlayService) aiCleanByFingerprint(ctx context.Context, m3u8 string) (string, bool) {
	pl, err := ai.ParseM3U8(ctx, m3u8)
	if err != nil || len(pl.Segments) == 0 {
		return "", false
	}
	var fps []models.AdFingerprint
	if err := s.db.Find(&fps).Error; err != nil {
		return "", false
	}
	skip := map[string]bool{}
	for _, f := range fps {
		if ai.Bad(f.Verdict) {
			skip[f.MD5] = true
		}
	}
	if len(skip) == 0 {
		return "", false
	}
	segs := make([]ai.Segment, 0, len(pl.Segments))
	segs = append(segs, pl.Segments...)
	clean := ai.GenerateCleanM3U8(segs, skip, pl.TargetDur)
	// 未剔除任何分片则视为无去广告必要
	if !strings.Contains(clean, "#EXTINF") {
		return "", false
	}
	return clean, skippedAny(segs, skip)
}

func skippedAny(segs []ai.Segment, skip map[string]bool) bool {
	for _, sg := range segs {
		if skip[sg.MD5] {
			return true
		}
	}
	return false
}

//------ 工具 ------

// isDirect 判断是否为可直接播放的媒体链接
func isDirect(u string) bool {
	return analyzer.Parse(u).Type == analyzer.TypeDirect
}

// detectMediaType 按 URL 后缀推断视频类型
func detectMediaType(u string) string {
	switch {
	case regexp.MustCompile(`(?i)\.m3u8`).MatchString(u):
		return "hls"
	case regexp.MustCompile(`(?i)\.flv`).MatchString(u):
		return "flv"
	case regexp.MustCompile(`(?i)\.(mp4|webm|ogg)`).MatchString(u):
		return "mp4"
	default:
		return "hls"
	}
}

// linkType 仅用于步骤日志展示
func linkType(u string) string {
	return detectMediaType(u)
}
