package handler

import (
	"sync/atomic"
	"time"

	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// 调用日志计数：每 N 条触发一次过期日志清理，避免表无限增长
const logCleanThreshold = 1000
const logRetainDays = 30

var callCounter atomic.Int64

// RecordCall 记录一条调用日志（resolve / proxy / cms.* 等），并定期清理过期数据
func RecordCall(db *gorm.DB, api string, ruleID uint, sourceID int, status int8, durationMS int, cacheHit int8, clientIP, targetURL, errMsg string) {
	if db == nil {
		return
	}
	log := models.CallLog{
		API:        api,
		RuleID:     int(ruleID),
		SourceID:   sourceID,
		CallStatus: status,
		DurationMS: durationMS,
		CacheHit:   cacheHit,
		ClientIP:   clientIP,
		TargetURL:  truncate(targetURL, 500),
		ErrorMsg:   truncate(errMsg, 500),
		CreatedAt:  time.Now(),
	}
	_ = db.Create(&log).Error

	// 定期清理：每 logCleanThreshold 条执行一次
	if callCounter.Add(1)%logCleanThreshold == 0 {
		cutoff := time.Now().AddDate(0, 0, -logRetainDays)
		_ = db.Where("created_at < ?", cutoff).Delete(&models.CallLog{}).Error
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
