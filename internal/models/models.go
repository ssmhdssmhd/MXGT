// Package models 核心数据模型：vods / episodes / sources / extract_rules。
// sources 支持对接多个采集源；extract_rules 支持对接多个解析规则。
package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Vod 影片主表
type Vod struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	VodID     string    `gorm:"size:64;uniqueIndex;not null" json:"vod_id"` // 外部源唯一标识
	Name      string    `gorm:"size:255;index;not null" json:"name"`
	Alias     string    `gorm:"size:512" json:"alias"` // 别名，逗号分隔
	Cover     string    `gorm:"size:512" json:"cover"`
	Year      int       `json:"year"`
	Region    string    `gorm:"size:64" json:"region"`
	Category  string    `gorm:"size:128;index" json:"category"`
	Remark    string    `gorm:"size:255" json:"remark"`
	Status    int8      `gorm:"default:1" json:"status"` // 1=启用 0=禁用
	Episodes  []Episode `gorm:"foreignKey:VodID" json:"episodes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Episode 集数表
type Episode struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	VodID       uint      `gorm:"index;not null" json:"vod_id"`
	EpisodeNo   int       `json:"episode_no"`
	EpisodeName string    `gorm:"size:255" json:"episode_name"`
	SourceURL   string    `gorm:"size:1024" json:"source_url"`   // 源站播放页 URL
	ResolvedURL string    `gorm:"size:1024" json:"resolved_url"` // 缓存的解析结果
	SourceName  string    `gorm:"size:128" json:"source_name"`   // 来源（可多源）
	PlayLine    string    `gorm:"size:128" json:"play_line"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Source 采集源配置（可对接多个源站）
type Source struct {
	ID           uint            `gorm:"primaryKey" json:"id"`
	Name         string          `gorm:"size:128;not null" json:"name"`
	SourceType   string          `gorm:"size:32;not null" json:"source_type"` // api / html / custom
	FetchURL     string          `gorm:"size:512;not null" json:"fetch_url"`  // 支持 {keyword} 占位符
	Method       string          `gorm:"size:8;default:GET" json:"method"`
	HeadersJSON  string          `gorm:"column:headers" json:"-"`
	Headers      json.RawMessage `gorm:"-" json:"headers,omitempty"`
	ExtractRules string          `gorm:"column:extract_rules;not null" json:"-"`
	Priority     int             `gorm:"default:0" json:"priority"`
	Enabled      int8            `gorm:"default:1" json:"enabled"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// ExtractRule 解析规则（中间层核心，可对接多个规则）
type ExtractRule struct {
	ID            uint            `gorm:"primaryKey" json:"id"`
	Name          string          `gorm:"size:128;not null" json:"name"`
	URLPattern    string          `gorm:"size:512;index;not null" json:"url_pattern"` // URL 匹配正则
	ExtractorType string          `gorm:"size:32;not null" json:"extractor_type"`     // jsonpath / regex / custom
	RuleConfig    string          `gorm:"type:text;not null" json:"-"`
	ConfigJSON    json.RawMessage `gorm:"-" json:"rule_config,omitempty"`
	TargetField   string          `gorm:"size:64;default:url" json:"target_field"`
	NeedProxy     int8            `gorm:"default:0" json:"need_proxy"` // 1=需要走 proxy
	Priority      int             `gorm:"default:0" json:"priority"`
	Enabled       int8            `gorm:"default:1" json:"enabled"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// TableName 指定表名
func (ExtractRule) TableName() string { return "extract_rules" }

// FrontendSetting 前端设置（单行表，id=1）：播放页伪装路径 / 参数别名 / 皮肤等
type FrontendSetting struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayPath    string    `gorm:"size:128;default:/" json:"play_path"`  // 播放页入口路径，如 /mx.php
	URLParam    string    `gorm:"size:64;default:url" json:"url_param"` // 主 URL 参数名
	AliasParams string    `gorm:"size:255" json:"alias_params"`         // 别名参数名，逗号分隔：video,src,link
	Skin        string    `gorm:"size:64;default:default" json:"skin"`
	PlayerType  string    `gorm:"size:32;default:dplayer" json:"player_type"` // dplayer / hls.js / flv.js
	LogoURL     string    `gorm:"size:512" json:"logo_url"`
	APIBase     string    `gorm:"size:255" json:"api_base"` // 强制注入后端 API 地址（空=自动同域）
	FooterText  string    `gorm:"size:255" json:"footer_text"`
	Beian       string    `gorm:"size:128" json:"beian"` // ICP 备案号
	CrossOrigin int8      `gorm:"default:1" json:"cross_origin"`
	CacheTTL    int       `gorm:"default:3600" json:"cache_ttl"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (FrontendSetting) TableName() string { return "frontend_settings" }

// SiteMapping 站点映射（预置腾讯/爱奇艺/优酷/芒果/搜狐/咪咕/B站七大站 + 自定义）
type SiteMapping struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	SiteCode       string    `gorm:"size:32;uniqueIndex;not null" json:"site_code"` // tencent/iqiyi/youku/mgtv/sohu/migu/bilibili/custom
	SiteName       string    `gorm:"size:128;not null" json:"site_name"`
	SiteDomain     string    `gorm:"size:512;not null" json:"site_domain"` // 域名正则
	SiteIcon       string    `gorm:"size:255" json:"site_icon"`
	NameField      string    `gorm:"size:255;not null" json:"name_field"` // 剧名提取：$.vod_name / regex:xxx
	AliasField     string    `gorm:"size:255" json:"alias_field"`
	CoverField     string    `gorm:"size:255" json:"cover_field"`
	YearField      string    `gorm:"size:255" json:"year_field"`
	RegionField    string    `gorm:"size:255" json:"region_field"`
	CategoryField  string    `gorm:"size:255" json:"category_field"`
	RemarkField    string    `gorm:"size:255" json:"remark_field"`
	EpisodesPath   string    `gorm:"size:255;not null" json:"episodes_path"` // 集数数组 JSONPath
	EpisodeNoRule  string    `gorm:"size:255" json:"episode_no_rule"`
	EpisodeURLRule string    `gorm:"size:255" json:"episode_url_rule"`
	ExtractRuleID  int       `gorm:"default:0" json:"extract_rule_id"` // 关联 extract_rules
	IsBuiltin      int8      `gorm:"default:0" json:"is_builtin"`      // 1=预置不可删
	Enabled        int8      `gorm:"default:1" json:"enabled"`
	Priority       int       `gorm:"default:0" json:"priority"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SiteMapping) TableName() string { return "site_mappings" }

// CallLog 调用日志（仪表盘统计源，定期清理）
type CallLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	API        string    `gorm:"size:64;index" json:"api"` // resolve / proxy / cms.play / cms.detail ...
	RuleID     int       `gorm:"default:0;index" json:"rule_id"`
	SourceID   int       `gorm:"default:0;index" json:"source_id"`
	CallStatus int8      `gorm:"default:0" json:"call_status"` // 1=成功 0=失败
	DurationMS int       `gorm:"default:0" json:"duration_ms"`
	CacheHit   int8      `gorm:"default:0" json:"cache_hit"`
	ClientIP   string    `gorm:"size:64" json:"client_ip"`
	TargetURL  string    `gorm:"size:512" json:"target_url"`
	ErrorMsg   string    `gorm:"size:512" json:"error_msg"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

// TableName 指定表名
func (CallLog) TableName() string { return "call_logs" }

// AnalysisSetting 分析引擎设置（单行表，id=1）：自动识别 URL 资源类型
type AnalysisSetting struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Enabled      int8      `gorm:"default:1" json:"enabled"` // 总开关
	Priority     string    `gorm:"size:32;default:official_first" json:"priority"` // official_first / direct_first / ai_first
	AIEnabled    int8      `gorm:"default:0" json:"ai_enabled"`
	AIProvider   string    `gorm:"size:32" json:"ai_provider"` // openai / doubao / custom
	AIAPIKey     string    `gorm:"size:255" json:"ai_api_key"`
	AIEndpoint   string    `gorm:"size:512" json:"ai_endpoint"`
	UnknownMode  string    `gorm:"size:32;default:reject" json:"unknown_mode"` // reject / direct / rule
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 指定表名
func (AnalysisSetting) TableName() string { return "analysis_settings" }

// DefaultAnalysisSetting 默认分析设置
func DefaultAnalysisSetting() AnalysisSetting {
	return AnalysisSetting{ID: 1, Enabled: 1, Priority: "official_first", UnknownMode: "reject"}
}

// MatchingSetting 匹配策略设置（单行表，id=1）：AI 自动识别 + 指定规则匹配双通道
type MatchingSetting struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Mode           string    `gorm:"size:16;default:rule" json:"mode"`    // rule / ai / auto（auto=AI+规则双通道）
	Fallback       string    `gorm:"size:16;default:rule" json:"fallback"` // 首选通道失败后的回退：rule / ai
	FuzzyThreshold int       `gorm:"default:85" json:"fuzzy_threshold"`    // 模糊匹配相似度阈值 0-100
	AutoCreate     int8      `gorm:"default:1" json:"auto_create"`         // 匹配成功是否自动入库 vods/episodes
	DirectAction   string    `gorm:"size:16;default:none" json:"direct_action"` // 直接资源走去插播：none / skip_ad / block_ad
	AIEnabled      int8      `gorm:"default:0" json:"ai_enabled"`          // 是否启用 AI 自动识别匹配
	AIProvider     string    `gorm:"size:32" json:"ai_provider"`           // openai / doubao / custom
	AIAPIKey       string    `gorm:"size:255" json:"ai_api_key"`
	AIEndpoint     string    `gorm:"size:512" json:"ai_endpoint"`
	AIModel        string    `gorm:"size:64" json:"ai_model"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName 指定表名
func (MatchingSetting) TableName() string { return "matching_settings" }

// DefaultMatchingSetting 默认匹配设置（id=1 单行）
func DefaultMatchingSetting() MatchingSetting {
	return MatchingSetting{
		ID: 1, Mode: "rule", Fallback: "ai", FuzzyThreshold: 85,
		AutoCreate: 1, DirectAction: "none",
	}
}

// 七大站预置映射数据（官方站点，is_builtin=1）
var builtinSiteMappings = []SiteMapping{
	{SiteCode: "tencent", SiteName: "腾讯视频", SiteDomain: `(v\.|video\.)qq\.com`, NameField: "$.vod_name", AliasField: "$.vod_actor", CoverField: "$.vod_pic", YearField: "$.vod_year", RegionField: "$.vod_area", CategoryField: "$.vod_class", RemarkField: "$.vod_content", EpisodesPath: "$.vod_play_from[0].vod_play_list[0].urls", IsBuiltin: 1, Enabled: 1, Priority: 70},
	{SiteCode: "iqiyi", SiteName: "爱奇艺", SiteDomain: `(www\.|pc\.)iqiyi\.com`, NameField: "$.title", CoverField: "$.imageUrl", YearField: "$.year", RegionField: "$.area", CategoryField: "$.categories", EpisodesPath: "$.data.episodes", IsBuiltin: 1, Enabled: 1, Priority: 60},
	{SiteCode: "youku", SiteName: "优酷", SiteDomain: `(v\.|www\.)youku\.com`, NameField: "$.title", CoverField: "$.poster", YearField: "$.year", CategoryField: "$.showCategory", EpisodesPath: "$.episodes", IsBuiltin: 1, Enabled: 1, Priority: 50},
	{SiteCode: "mgtv", SiteName: "芒果TV", SiteDomain: `(www\.|h5\.)mgtv\.com`, NameField: "$.vod_name", CoverField: "$.img", EpisodesPath: "$.data.episodes", IsBuiltin: 1, Enabled: 1, Priority: 40},
	{SiteCode: "sohu", SiteName: "搜狐视频", SiteDomain: `tv\.sohu\.com`, NameField: "$.video_name", CoverField: "$.pic", EpisodesPath: "$.episodes", IsBuiltin: 1, Enabled: 1, Priority: 30},
	{SiteCode: "migu", SiteName: "咪咕视频", SiteDomain: `www\.miguvideo\.com`, NameField: "$.title", CoverField: "$.img", EpisodesPath: "$.episodes", IsBuiltin: 1, Enabled: 1, Priority: 20},
	{SiteCode: "bilibili", SiteName: "哔哩哔哩", SiteDomain: `(www\.|m\.)bilibili\.com`, NameField: "$.title", CoverField: "$.pic", YearField: "$.pubdate", CategoryField: "$.tname", EpisodesPath: "$.epList", IsBuiltin: 1, Enabled: 1, Priority: 10},
}

// SeedSiteMappings 首次运行时插入七大站预置映射（幂等：已有则不重复插入）
func SeedSiteMappings(db *gorm.DB) {
	var m SiteMapping
	if db.First(&m).Error == nil {
		return // 已有数据，不重复插入
	}
	for i := range builtinSiteMappings {
		_ = db.Create(&builtinSiteMappings[i])
	}
}

// AutoMigrate 自动建表/迁移
func AutoMigrate(db interface {
	AutoMigrate(dst ...interface{}) error
}) error {
	return db.AutoMigrate(
		&Vod{},
		&Episode{},
		&Source{},
		&ExtractRule{},
		&FrontendSetting{},
		&SiteMapping{},
		&CallLog{},
		&AnalysisSetting{},
		&MatchingSetting{},
	)
}
