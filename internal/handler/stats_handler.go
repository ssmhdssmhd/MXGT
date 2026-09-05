package handler

import (
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// StatsHandler 仪表盘统计
type StatsHandler struct {
	db *gorm.DB
}

// NewStatsHandler 创建统计处理器
func NewStatsHandler(db *gorm.DB) *StatsHandler {
	return &StatsHandler{db: db}
}

// Overview 处理 GET /admin/stats/overview（今日/累计调用、成功率、缓存命中、活跃规则/源）
func (h *StatsHandler) Overview(c echo.Context) error {
	today := time.Now().Truncate(24 * time.Hour)

	var totalCalls, todayCalls, successCalls, todaySuccess, cacheHits int64
	_ = h.db.Model(&models.CallLog{}).Count(&totalCalls).Error
	_ = h.db.Model(&models.CallLog{}).Where("created_at >= ?", today).Count(&todayCalls).Error
	_ = h.db.Model(&models.CallLog{}).Where("call_status = ?", 1).Count(&successCalls).Error
	_ = h.db.Model(&models.CallLog{}).Where("created_at >= ? AND call_status = ?", today, 1).Count(&todaySuccess).Error
	_ = h.db.Model(&models.CallLog{}).Where("cache_hit = ?", 1).Count(&cacheHits).Error

	var rules, sources, vods, episodes int64
	_ = h.db.Model(&models.ExtractRule{}).Where("enabled = ?", 1).Count(&rules).Error
	_ = h.db.Model(&models.Source{}).Where("enabled = ?", 1).Count(&sources).Error
	_ = h.db.Model(&models.Vod{}).Count(&vods).Error
	_ = h.db.Model(&models.Episode{}).Count(&episodes).Error

	return ok(c, map[string]any{
		"total_calls":   totalCalls,
		"today_calls":   todayCalls,
		"success_rate":  percent(successCalls, totalCalls),
		"today_success": percent(todaySuccess, todayCalls),
		"cache_hit_rate": percent(cacheHits, totalCalls),
		"active_rules":  rules,
		"active_sources": sources,
		"vods":          vods,
		"episodes":      episodes,
	})
}

// Trends 处理 GET /admin/stats/trends?range=7d（按天聚合调用量/成功率/平均耗时）
func (h *StatsHandler) Trends(c echo.Context) error {
	days := atoi(c.QueryParam("range"), 7)
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	start := time.Now().AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)

	// 取区间内日志，按天在内存聚合（数据量可控）
	var logs []struct {
		CreatedAt   time.Time
		CallStatus  int8
		CacheHit    int8
		DurationMS  int
	}
	_ = h.db.Model(&models.CallLog{}).
		Select("created_at, call_status, cache_hit, duration_ms").
		Where("created_at >= ?", start).
		Find(&logs).Error

	// 生成连续日期
	points := make([]map[string]any, 0, days)
	dayIdx := make(map[string]int)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		dayIdx[key] = i
		points = append(points, map[string]any{
			"date": key, "calls": 0, "success": 0, "total_ms": 0, "cache_hits": 0,
		})
	}
	for _, l := range logs {
		key := l.CreatedAt.Format("2006-01-02")
		idx, ok := dayIdx[key]
		if !ok {
			continue
		}
		p := points[idx]
		p["calls"] = p["calls"].(int) + 1
		p["total_ms"] = p["total_ms"].(int) + l.DurationMS
		if l.CallStatus == 1 {
			p["success"] = p["success"].(int) + 1
		}
		if l.CacheHit == 1 {
			p["cache_hits"] = p["cache_hits"].(int) + 1
		}
	}
	// 计算派生指标
	for i := range points {
		calls := points[i]["calls"].(int)
		success := points[i]["success"].(int)
		totalMS := points[i]["total_ms"].(int)
		points[i]["success_rate"] = percentInt(success, calls)
		points[i]["avg_ms"] = avg(totalMS, calls)
	}
	return ok(c, points)
}

// RulesTop 处理 GET /admin/stats/rules-top?limit=10（解析规则调用 TOP）
func (h *StatsHandler) RulesTop(c echo.Context) error {
	limit := atoi(c.QueryParam("limit"), 10)
	type row struct {
		RuleID int   `json:"rule_id"`
		Calls  int64 `json:"calls"`
	}
	var rows []row
	_ = h.db.Model(&models.CallLog{}).
		Select("rule_id, COUNT(*) AS calls").
		Where("rule_id > ?", 0).
		Group("rule_id").
		Order("calls DESC").
		Limit(limit).
		Scan(&rows).Error

	// 补规则名称
	type out struct {
		RuleID int    `json:"rule_id"`
		Name   string `json:"name"`
		Calls  int64  `json:"calls"`
	}
	result := make([]out, 0, len(rows))
	for _, r := range rows {
		name := ""
		var rule models.ExtractRule
		if err := h.db.Select("name").First(&rule, r.RuleID).Error; err == nil {
			name = rule.Name
		}
		result = append(result, out{RuleID: r.RuleID, Name: name, Calls: r.Calls})
	}
	return ok(c, result)
}

// SourcesTop 处理 GET /admin/stats/sources-top（采集源入库量 TOP：按 episodes.source_name）
func (h *StatsHandler) SourcesTop(c echo.Context) error {
	limit := atoi(c.QueryParam("limit"), 10)
	type row struct {
		SourceName string `json:"source_name"`
		Episodes   int64  `json:"episodes"`
		Vods       int64  `json:"vods"`
	}
	var rows []row
	_ = h.db.Model(&models.Episode{}).
		Select("source_name, COUNT(*) AS episodes").
		Where("source_name <> ''").
		Group("source_name").
		Order("episodes DESC").
		Limit(limit).
		Scan(&rows).Error
	// 补每个源的影片数
	for i := range rows {
		var vods int64
		_ = h.db.Model(&models.Episode{}).Distinct("vod_id").Where("source_name = ?", rows[i].SourceName).Count(&vods).Error
		rows[i].Vods = vods
	}
	return ok(c, rows)
}

// CallLogs 处理 GET /admin/call-logs?page=1&size=20（最近调用明细）
func (h *StatsHandler) CallLogs(c echo.Context) error {
	page := atoi(c.QueryParam("page"), 1)
	if page < 1 {
		page = 1
	}
	size := atoi(c.QueryParam("size"), 20)
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	var total int64
	_ = h.db.Model(&models.CallLog{}).Count(&total).Error
	var logs []models.CallLog
	_ = h.db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&logs).Error
	return ok(c, map[string]any{
		"page": page, "size": size, "total": total, "list": logs,
	})
}

// percent 计算成功率百分比（百分比整数）
func percent(a, b int64) float64 {
	if b <= 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func percentInt(a, b int) float64 {
	if b <= 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func avg(totalMS, calls int) int {
	if calls <= 0 {
		return 0
	}
	return totalMS / calls
}
