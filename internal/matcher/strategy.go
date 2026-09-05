// Package matcher 匹配策略：AI 自动识别 + 指定规则匹配双通道，可配阈值与回退。
package matcher

// 匹配模式常量
const (
	ModeRule = "rule" // 指定规则匹配（模糊 + 别名）
	ModeAI   = "ai"   // AI 自动识别匹配
	ModeAuto = "auto" // 双通道：首选 + 回退
)

// 直接资源走去插播动作
const (
	DirectNone  = "none"    // 不处理，直接播放
	DirectSkip  = "skip_ad" // 去广告
	DirectBlock = "block_ad" // 去插播
)

// Settings 匹配策略设置（运行时副本，来自 matching_settings 单行表）
type Settings struct {
	Mode           string `json:"mode"`
	Fallback       string `json:"fallback"`
	FuzzyThreshold int    `json:"fuzzy_threshold"`
	AutoCreate     int8   `json:"auto_create"`
	DirectAction   string `json:"direct_action"`
}

// AIClient AI 匹配通道接口（可对接 openai / doubao / custom）
type AIClient interface {
	// Match 判断 srcName 是否即为 targetName（含别名），返回相似度 0-100 与是否匹配。
	Match(srcName, targetName string, aliases []string) (int, bool, error)
}

// MatchResult 匹配结果
type MatchResult struct {
	Matched    bool   `json:"matched"`
	Channel    string `json:"channel"` // rule / ai
	Score      int    `json:"score"`   // 相似度 0-100
	TargetName string `json:"target_name"`
	Reason     string `json:"reason,omitempty"`
}

// Strategy 双通道匹配策略执行器
type Strategy struct {
	Settings Settings
	AI       AIClient // 可选，nil 时跳过 AI 通道
}

// Match 按配置模式执行单目标匹配。
// srcName 为待匹配剧名，targetName 为目标剧名，aliases 为目标别名表。
func (s *Strategy) Match(srcName, targetName string, aliases []string) MatchResult {
	mode := s.Settings.Mode
	if mode == "" {
		mode = ModeRule
	}

	switch mode {
	case ModeAI:
		if s.AI != nil {
			return s.matchAI(srcName, targetName, aliases)
		}
		return s.matchRule(srcName, targetName, aliases)
	case ModeAuto:
		// 双通道：首选
		primary := s.Settings.Fallback
		if primary != ModeAI {
			primary = ModeRule
		}
		secondary := ModeAI
		if primary == ModeAI {
			secondary = ModeRule
		}
		for _, ch := range []string{primary, secondary} {
			var r MatchResult
			if ch == ModeAI && s.AI != nil {
				r = s.matchAI(srcName, targetName, aliases)
			} else if ch == ModeRule {
				r = s.matchRule(srcName, targetName, aliases)
			}
			if r.Matched {
				return r
			}
		}
		return MatchResult{Matched: false, Score: 0}
	default: // rule
		return s.matchRule(srcName, targetName, aliases)
	}
}

func (s *Strategy) matchRule(srcName, targetName string, aliases []string) MatchResult {
	th := s.Settings.FuzzyThreshold
	if th <= 0 {
		th = 85
	}
	ok := MatchName(srcName, targetName, aliases, th)
	score := 0
	if !ok {
		score = int(Similarity(Normalize(srcName), Normalize(targetName)) * 100)
	} else {
		score = 100
	}
	return MatchResult{Matched: ok, Channel: ModeRule, Score: score, TargetName: targetName}
}

func (s *Strategy) matchAI(srcName, targetName string, aliases []string) MatchResult {
	if s.AI == nil {
		return MatchResult{Matched: false, Channel: ModeAI, Reason: "AI 客户端未配置"}
	}
	score, ok, err := s.AI.Match(srcName, targetName, aliases)
	if err != nil {
		return MatchResult{Matched: false, Channel: ModeAI, Reason: err.Error()}
	}
	return MatchResult{Matched: ok, Channel: ModeAI, Score: score, TargetName: targetName}
}

// DecideDirectAction 对直接播放资源决定走去插播配置的动作（空则默认 none 直接播放）。
func DecideDirectAction(action string) string {
	if action == "" {
		return DirectNone
	}
	return action
}