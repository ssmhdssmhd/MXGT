package service

import (
	"context"
	"strings"

	"github.com/ssmhdssmhd/MXGT/internal/collector"
	"github.com/ssmhdssmhd/MXGT/internal/matcher"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// SyncResult 单个采集源的同步统计
type SyncResult struct {
	SourceID   uint   `json:"source_id"`
	SourceName string `json:"source_name"`
	Fetched    int    `json:"fetched"`  // 拉取条数
	Created    int    `json:"created"`  // 新建影片数
	Updated    int    `json:"updated"`  // 命中更新数
	Episodes   int    `json:"episodes"` // 写入集数
	Errors     int    `json:"errors"`   // 失败数
	ErrorMsg   string `json:"error_msg,omitempty"`
}

// SyncService 采集同步服务：多源采集 → 匹配 → 入库
type SyncService struct {
	db *gorm.DB
}

// NewSyncService 创建同步服务
func NewSyncService(db *gorm.DB) *SyncService {
	return &SyncService{db: db}
}

// Sync 遍历所有启用采集源（对接多个），按关键词采集并入库
func (s *SyncService) Sync(ctx context.Context, keyword string) ([]SyncResult, error) {
	var sources []models.Source
	if err := s.db.Where("enabled = ?", 1).Order("priority DESC").Find(&sources).Error; err != nil {
		return nil, err
	}

	results := make([]SyncResult, 0, len(sources))
	for i := range sources {
		src := &sources[i]
		result := SyncResult{SourceID: src.ID, SourceName: src.Name}

		col, err := collector.New(src)
		if err != nil {
			result.Errors++
			result.ErrorMsg = err.Error()
			results = append(results, result)
			continue
		}

		items, err := col.Fetch(ctx, keyword)
		if err != nil {
			result.Errors++
			result.ErrorMsg = err.Error()
			results = append(results, result)
			continue
		}
		result.Fetched = len(items)

		for j := range items {
			item := &items[j]
			if err := s.syncItem(ctx, src, item, &result); err != nil {
				result.Errors++
				continue
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// syncItem 单条影视：匹配/新建 vod + 写入集数
func (s *SyncService) syncItem(ctx context.Context, src *models.Source, item *collector.RawItem, result *SyncResult) error {
	aliases := splitAlias(item.Alias)

	// 匹配库内已有 vod（多源对接：同一部剧多源入库时合并到同一 vod）
	vod, isNew, err := s.matchOrCreateVod(ctx, item, aliases)
	if err != nil {
		return err
	}
	if isNew {
		result.Created++
	} else {
		result.Updated++
	}

	// 写入集数（按 vod + 集数 + 来源 upsert）
	for k := range item.Episodes {
		ep := &item.Episodes[k]
		no := matcher.ExtractEpisodeNo(ep.Name)
		if no == 0 && k+1 > 0 {
			no = k + 1 // 无集数信息时按顺序补位
		}
		ep.No = no

		var existing models.Episode
		err := s.db.WithContext(ctx).
			Where("vod_id = ? AND episode_no = ? AND source_name = ?", vod.ID, no, src.Name).
			First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			s.db.WithContext(ctx).Create(&models.Episode{
				VodID:       vod.ID,
				EpisodeNo:   no,
				EpisodeName: ep.Name,
				SourceURL:   ep.URL,
				SourceName:  src.Name,
				PlayLine:    ep.Line,
			})
			result.Episodes++
		} else if err == nil {
			existing.SourceURL = ep.URL
			existing.EpisodeName = ep.Name
			s.db.WithContext(ctx).Save(&existing)
			result.Episodes++
		}
	}
	return nil
}

// matchOrCreateVod 匹配或新建影片
func (s *SyncService) matchOrCreateVod(ctx context.Context, item *collector.RawItem, aliases []string) (*models.Vod, bool, error) {
	// 1. 精确/模糊匹配库内 vods
	var candidates []models.Vod
	s.db.WithContext(ctx).Find(&candidates)
	for i := range candidates {
		if matcher.MatchName(item.Name, candidates[i].Name, aliases, 85) {
			return &candidates[i], false, nil
		}
	}

	// 2. 未匹配 → 新建
	vod := &models.Vod{
		VodID:    genVodID(item.Name, item.Year),
		Name:     item.Name,
		Alias:    item.Alias,
		Cover:    item.Cover,
		Year:     item.Year,
		Region:   item.Region,
		Category: item.Category,
		Remark:   item.Remark,
		Status:   1,
	}
	if err := s.db.WithContext(ctx).Create(vod).Error; err != nil {
		return nil, false, err
	}
	return vod, true, nil
}

// splitAlias 别名逗号分隔
func splitAlias(alias string) []string {
	parts := strings.Split(alias, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// genVodID 生成外部唯一标识（name+year 哈希，保证多源同剧合并）
func genVodID(name string, year int) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "_")) + "_" + itoa(year)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
