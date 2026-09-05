// MXGT 服务入口：分层启动（配置 → 数据库 → 缓存 → 路由）
package main

import (
	"fmt"
	"log"

	"github.com/ssmhdssmhd/MXGT/internal/cache"
	"github.com/ssmhdssmhd/MXGT/internal/config"
	"github.com/ssmhdssmhd/MXGT/internal/database"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"github.com/ssmhdssmhd/MXGT/internal/router"
)

// version 程序版本（构建时可用 -ldflags "-X main.version=vX.Y.Z" 覆盖）
var version = "v0.0.11"

func main() {
	// 运行文件夹：所有用户环境（config / data / logs...）都建立在此目录内
	runDir := config.RunDir()
	log.Printf("运行文件夹: %s", runDir)

	// ① 配置（免配置：首次运行自动生成 config.yaml）
	cfg, err := config.Load(runDir)
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	// ② 数据库（多驱动：sqlite 默认 / mysql 可选）
	db, err := database.Open(cfg)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	if err := models.AutoMigrate(db); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	// 首次运行插入七大站预置映射（幂等）
	models.SeedSiteMappings(db)

	// ③ 缓存（多驱动：memory 默认 / redis 可选）
	c, err := cache.New(&cfg.Cache)
	if err != nil {
		log.Fatalf("缓存初始化失败: %v", err)
	}
	defer c.Close()

	// ④ 路由
	e := router.New(db, c, cfg, version)

	// ⑤ 启动
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("MXGT %s 启动成功 → http://localhost:%d", version, cfg.Server.Port)
	if err := e.Start(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
