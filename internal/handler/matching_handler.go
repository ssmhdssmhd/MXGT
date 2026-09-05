package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/analyzer"
	"github.com/ssmhdssmhd/MXGT/internal/matcher"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// MatchingHandler 匹配策略处理器（M12）：AI/规则双通道 + 阈值配置
type MatchingHandler struct {
	db *gorm.DB
}

// NewMatchingHandler 创建匹配策略处理器
func NewMatchingHandler(db *gorm.DB) *MatchingHandler {
	return &MatchingHandler{db: db}
}

// GetSettings 读取匹配设置（单行，不存在返回默认）
func (h *MatchingHandler) GetSettings(c echo.Context) error {
	s, err := loadMatchingSetting(h.db)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	return ok(c, s)
}

// UpdateSettings 更新匹配设置（PUT /admin/matching/settings）
func (h *MatchingHandler) UpdateSettings(c echo.Context) error {
	var req models.MatchingSetting
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	s, err := loadMatchingSetting(h.db)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	if req.Mode != "" {
		s.Mode = req.Mode
	}
	if req.Fallback != "" {
		s.Fallback = req.Fallback
	}
	if req.FuzzyThreshold != 0 {
		s.FuzzyThreshold = req.FuzzyThreshold
	}
	if req.AutoCreate != 0 {
		s.AutoCreate = req.AutoCreate
	}
	if req.DirectAction != "" {
		s.DirectAction = req.DirectAction
	}
	if req.AIEnabled != 0 {
		s.AIEnabled = req.AIEnabled
	}
	if req.AIProvider != "" {
		s.AIProvider = req.AIProvider
	}
	if req.AIAPIKey != "" {
		s.AIAPIKey = req.AIAPIKey
	}
	if req.AIEndpoint != "" {
		s.AIEndpoint = req.AIEndpoint
	}
	if req.AIModel != "" {
		s.AIModel = req.AIModel
	}
	if err := h.db.Save(&s).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
	}
	return ok(c, s)
}

// Test 处理 POST /admin/matching/test（测试某个剧名在当前策略下能匹配到哪些已入库影片）
func (h *MatchingHandler) Test(c echo.Context) error {
	var req struct {
		Name        string   `json:"name"`
		Aliases     []string `json:"aliases"`
		Limit       int      `json:"limit"`
		ResourceURL string   `json:"resource_url"` // 可选：直接资源 URL，返回走去插播决策
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if req.Name == "" && req.ResourceURL == "" {
		return fail(c, http.StatusBadRequest, "name 或 resource_url 至少一个")
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	st, err := loadMatchingSetting(h.db)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}

	strategy := matcher.Strategy{
		Settings: matcher.Settings{
			Mode:           st.Mode,
			Fallback:       st.Fallback,
			FuzzyThreshold: st.FuzzyThreshold,
			AutoCreate:     st.AutoCreate,
			DirectAction:   st.DirectAction,
		},
		AI: buildAIClient(st),
	}

	// 直接资源：返回去插播决策
	if req.ResourceURL != "" {
		return ok(c, map[string]any{
			"is_direct":    analyzer.IsDirectMedia(req.ResourceURL),
			"direct_action": matcher.DecideDirectAction(st.DirectAction),
		})
	}

	// 匹配模式：遍历已入库 vods 找出命中项
	var vods []models.Vod
	if err := h.db.Limit(req.Limit).Find(&vods).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	matches := make([]map[string]any, 0, len(vods))
	for _, v := range vods {
		aliases := []string{}
		if v.Alias != "" {
			aliases = splitAliases(v.Alias)
		}
		r := strategy.Match(req.Name, v.Name, aliases)
		if r.Matched {
			matches = append(matches, map[string]any{
				"vod_id":   v.ID,
				"vod_name": v.Name,
				"channel":  r.Channel,
				"score":    r.Score,
				"reason":   r.Reason,
			})
		}
	}
	return ok(c, map[string]any{
		"mode":         st.Mode,
		"fuzzy_threshold": st.FuzzyThreshold,
		"total_scanned":  len(vods),
		"matched":        matches,
	})
}

// buildAIClient 根据设置构建 AI 匹配客户端
func buildAIClient(s models.MatchingSetting) matcher.AIClient {
	if s.AIEnabled == 0 || s.AIAPIKey == "" {
		return nil
	}
	endpoint := s.AIEndpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}
	return matcher.NewOpenAIClient(s.AIAPIKey, endpoint, s.AIModel)
}

// splitAliases 将逗号分隔的别名串拆成切片
func splitAliases(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' || r == '，' || r == '、' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// loadMatchingSetting 读取匹配设置，不存在则创建默认行
func loadMatchingSetting(db *gorm.DB) (models.MatchingSetting, error) {
	var s models.MatchingSetting
	if err := db.First(&s, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s = models.DefaultMatchingSetting()
			if err := db.Create(&s).Error; err != nil {
				return s, err
			}
			return s, nil
		}
		return s, err
	}
	return s, nil
}