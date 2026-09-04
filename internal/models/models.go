// Package models 核心数据模型：vods / episodes / sources / extract_rules。
// sources 支持对接多个采集源；extract_rules 支持对接多个解析规则。
package models

import (
	"encoding/json"
	"time"
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

// AutoMigrate 自动建表/迁移
func AutoMigrate(db interface {
	AutoMigrate(dst ...interface{}) error
}) error {
	return db.AutoMigrate(
		&Vod{},
		&Episode{},
		&Source{},
		&ExtractRule{},
	)
}
