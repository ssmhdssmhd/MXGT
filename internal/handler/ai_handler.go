package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/ai"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// AIHandler AI 智能视频分析处理器（M16）
type AIHandler struct {
	db *gorm.DB
}

// NewAIHandler 创建 AI 分析处理器
func NewAIHandler(db *gorm.DB) *AIHandler {
	return &AIHandler{db: db}
}

// ---------- 设置 ----------
func (h *AIHandler) GetSettings(c echo.Context) error {
	s, err := h.loadSettings()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, s)
}

func (h *AIHandler) UpdateSettings(c echo.Context) error {
	var req models.AiSetting
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	s, err := h.loadSettings()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	if req.Enabled != 0 {
		s.Enabled = req.Enabled
	}
	if req.AutoSkipAD != 0 {
		s.AutoSkipAD = req.AutoSkipAD
	}
	if req.Provider != "" {
		s.Provider = req.Provider
	}
	if req.APIKey != "" {
		s.APIKey = req.APIKey
	}
	if req.Endpoint != "" {
		s.Endpoint = req.Endpoint
	}
	if req.Model != "" {
		s.Model = req.Model
	}
	if req.SampleRatio != 0 {
		s.SampleRatio = req.SampleRatio
	}
	if req.Concurrency != 0 {
		s.Concurrency = req.Concurrency
	}
	if req.MaxSegments != 0 {
		s.MaxSegments = req.MaxSegments
	}
	if req.HeuristicOn != 0 {
		s.HeuristicOn = req.HeuristicOn
	}
	if err := h.db.Save(&s).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
	}
	return ok(c, s)
}

// ---------- 分析 ----------
// Analyze 处理 POST /admin/ai/analyze（输入 m3u8，全量 ts 分片 → MD5 + 启发式/指纹判定）
func (h *AIHandler) Analyze(c echo.Context) error {
	var req struct {
		M3U8    string `json:"m3u8"`
		Source  string `json:"source"`
		MaxSeg  int    `json:"max_segments"`
	}
	if err := c.Bind(&req); err != nil || req.M3U8 == "" {
		return fail(c, http.StatusBadRequest, "m3u8 必填")
	}
	s, err := h.loadSettings()
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}

	cfg := ai.Config{
		Concurrency: s.Concurrency,
		MaxSegments: req.MaxSeg,
		HeuristicOn: s.HeuristicOn == 1,
		MaxBytes:    8 << 20, // 8MB 上限
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}

	log := models.TsAnalysisLog{M3U8URL: req.M3U8, Status: "running"}
	_ = h.db.Create(&log)

	an := ai.NewAnalyzer(&dbFingerprintStore{db: h.db})
	res, err := an.Analyze(context.Background(), req.M3U8, req.Source, cfg, nil)
	if err != nil {
		log.Status = "failed"
		log.Message = err.Error()
		_ = h.db.Save(&log)
		return fail(c, http.StatusBadRequest, err.Error())
	}
	clean := len(res.Results) - res.Bad
	log.Status = "success"
	log.Total = res.Total
	log.ADs = res.Bad
	log.CleanCount = clean
	log.Analyzed = len(res.Results)
	log.Message = res.Message
	_ = h.db.Save(&log)

	return ok(c, map[string]any{
		"log_id":   log.ID,
		"source":   res.Source,
		"total":    res.Total,
		"analyzed": len(res.Results),
		"bad":      res.Bad,
		"clean":    clean,
		"target_duration": res.TargetDur,
		"segments": res.Results,
	})
}

// Result 处理 GET /admin/ai/ts?log_id=N（查看某次分析的 ts 列表）
func (h *AIHandler) Result(c echo.Context) error {
	logID := c.QueryParam("log_id")
	var logs []models.TsAnalysisLog
	q := h.db.Order("id DESC").Limit(50)
	if logID != "" {
		q = h.db.Where("id = ?", logID)
	}
	if err := q.Find(&logs).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, logs)
}

// CleanM3U8 处理 POST /admin/ai/result/m3u8（用上次分析的 URL 重新解析并生成去广告 m3u8）
func (h *AIHandler) CleanM3U8(c echo.Context) error {
	var req struct {
		M3U8 string `json:"m3u8"`
	}
	if err := c.Bind(&req); err != nil || req.M3U8 == "" {
		return fail(c, http.StatusBadRequest, "m3u8 必填")
	}
	pl, err := ai.ParseM3U8(context.Background(), req.M3U8)
	if err != nil {
		return fail(c, http.StatusBadRequest, err.Error())
	}
	// 收集所有指纹中判定为 bad 的 MD5
	var fps []models.AdFingerprint
	if err := h.db.Find(&fps).Error; err == nil {
		skip := map[string]bool{}
		for _, f := range fps {
			if ai.Bad(f.Verdict) {
				skip[f.MD5] = true
			}
		}
		segs := make([]ai.Segment, 0, len(pl.Segments))
		segs = append(segs, pl.Segments...)
		clean := ai.GenerateCleanM3U8(segs, skip, pl.TargetDur)
		c.Response().Header().Set(echo.HeaderContentType, "application/vnd.apple.mpegurl")
		return c.String(http.StatusOK, clean)
	}
	return fail(c, http.StatusInternalServerError, "查询指纹库失败")
}

// Fingerprints 处理 GET /admin/ai/fingerprints 与 POST /admin/ai/fingerprints/import
func (h *AIHandler) Fingerprints(c echo.Context) error {
	var list []models.AdFingerprint
	if err := h.db.Order("hit_count DESC").Limit(200).Find(&list).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, list)
}

func (h *AIHandler) ImportFingerprints(c echo.Context) error {
	var req struct {
		Items []models.AdFingerprint `json:"items"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	n := 0
	for _, it := range req.Items {
		if it.MD5 == "" || it.Verdict == "" {
			continue
		}
		var f models.AdFingerprint
		if err := h.db.Where("md5 = ?", it.MD5).First(&f).Error; err == nil {
			continue // 已存在
		}
		nv := models.AdFingerprint{MD5: it.MD5, Verdict: it.Verdict, SourceName: it.SourceName}
		if err := h.db.Create(&nv).Error; err == nil {
			n++
		}
	}
	return ok(c, map[string]any{"imported": n})
}

// Logs 处理 GET /admin/ai/logs（最近分析日志）
func (h *AIHandler) Logs(c echo.Context) error {
	var list []models.TsAnalysisLog
	if err := h.db.Order("id DESC").Limit(30).Find(&list).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, list)
}

// ---------- 内部 ----------
func (h *AIHandler) loadSettings() (models.AiSetting, error) {
	var s models.AiSetting
	if err := h.db.First(&s, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s = models.DefaultAiSetting()
			if err := h.db.Create(&s).Error; err != nil {
				return s, err
			}
			return s, nil
		}
		return s, err
	}
	return s, nil
}

// dbFingerprintStore 基于 DB 的 MD5 指纹库
type dbFingerprintStore struct {
	db *gorm.DB
}

func (d *dbFingerprintStore) Lookup(md5 string) string {
	if md5 == "" {
		return ""
	}
	var f models.AdFingerprint
	if err := d.db.Where("md5 = ?", md5).First(&f).Error; err != nil {
		return ""
	}
	_ = d.db.Model(&f).Update("hit_count", f.HitCount+1)
	return f.Verdict
}

func (d *dbFingerprintStore) Record(src string, seg ai.Segment) error {
	if seg.MD5 == "" {
		return nil
	}
	var f models.AdFingerprint
	if err := d.db.Where("md5 = ?", seg.MD5).First(&f).Error; err == nil {
		return nil
	}
	v := seg.Verdict
	if v == "" {
		v = ai.VerdictNormal
	}
	return d.db.Create(&models.AdFingerprint{
		MD5: seg.MD5, Verdict: v, SourceName: src,
		SizeBytes: seg.SizeBytes, DurationSec: seg.Duration,
	}).Error
}