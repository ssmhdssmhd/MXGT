// Package database 数据库连接：支持对接多个驱动（sqlite 默认零配置 / mysql 可选）。
// 简单用户无需安装任何数据库即可使用（默认 SQLite）。
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite" // 纯 Go SQLite，无 CGO，支持静态编译
	"github.com/ssmhdssmhd/MXGT/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open 按配置创建数据库连接（多驱动对接：sqlite / mysql）
func Open(cfg *config.Config) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	var db *gorm.DB
	var err error

	switch cfg.Database.Driver {
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.Database.User, cfg.Database.Password,
			cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName)
		db, err = gorm.Open(mysql.Open(dsn), gormCfg)
	case "sqlite", "":
		// SQLite 文件路径：确保目录存在
		sqlitePath := cfg.Database.SQLite
		if sqlitePath == "" {
			sqlitePath = filepath.Join("data", "mxgt.db")
		}
		if dir := filepath.Dir(sqlitePath); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("创建数据库目录失败: %w", err)
			}
		}
		db, err = gorm.Open(sqlite.Open(sqlitePath), gormCfg)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", cfg.Database.Driver)
	}

	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}
