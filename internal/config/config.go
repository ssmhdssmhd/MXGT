// Package config 配置加载：支持多数据源（sqlite/mysql）、多缓存（memory/redis）。
// 首次运行在运行目录自动生成 config.yaml，简单用户零配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config 应用配置（分层：服务 / 数据库 / 缓存 / 解析）
type Config struct {
	Server   ServerConfig   `mapstructure:"server" yaml:"server"`
	Database DatabaseConfig `mapstructure:"database" yaml:"database"`
	Cache    CacheConfig    `mapstructure:"cache" yaml:"cache"`
	Resolve  ResolveConfig  `mapstructure:"resolve" yaml:"resolve"`
}

// ServerConfig 服务配置
type ServerConfig struct {
	Port int    `mapstructure:"port" yaml:"port"`
	Mode string `mapstructure:"mode" yaml:"mode"`
}

// DatabaseConfig 数据库配置：支持对接多个驱动（sqlite / mysql）
type DatabaseConfig struct {
	Driver   string `mapstructure:"driver" yaml:"driver"`           // sqlite / mysql
	SQLite   string `mapstructure:"sqlite_path" yaml:"sqlite_path"` // 默认 data/mxgt.db
	Host     string `mapstructure:"host" yaml:"host"`
	Port     int    `mapstructure:"port" yaml:"port"`
	User     string `mapstructure:"user" yaml:"user"`
	Password string `mapstructure:"password" yaml:"password"`
	DBName   string `mapstructure:"dbname" yaml:"dbname"`
}

// CacheConfig 缓存配置：支持对接多个驱动（memory / redis）
type CacheConfig struct {
	Driver   string `mapstructure:"driver" yaml:"driver"` // memory / redis
	Address  string `mapstructure:"address" yaml:"address"`
	Password string `mapstructure:"password" yaml:"password"`
	DB       int    `mapstructure:"db" yaml:"db"`
	TTL      int    `mapstructure:"ttl_seconds" yaml:"ttl_seconds"`
}

// ResolveConfig 解析路由配置
type ResolveConfig struct {
	CacheTTL int `mapstructure:"cache_ttl_seconds" yaml:"cache_ttl_seconds"`
	Timeout  int `mapstructure:"http_timeout_seconds" yaml:"http_timeout_seconds"`
}

// Default 返回内置默认值（免配置直接用）
func Default() *Config {
	return &Config{
		Server: ServerConfig{Port: 8080, Mode: "release"},
		Database: DatabaseConfig{
			Driver: "sqlite",
			SQLite: filepath.Join("data", "mxgt.db"),
		},
		Cache: CacheConfig{
			Driver: "memory",
			TTL:    3600,
		},
		Resolve: ResolveConfig{CacheTTL: 3600, Timeout: 15},
	}
}

// Load 从运行目录加载 config.yaml；不存在则自动生成默认配置（免配置运行）。
// runDir 为运行文件夹（可执行文件所在目录），所有用户环境都建立在此目录内。
func Load(runDir string) (*Config, error) {
	cfgPath := filepath.Join(runDir, "config.yaml")

	v := viper.New()
	v.SetConfigFile(cfgPath)
	v.SetConfigType("yaml")

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		// 首次运行：生成默认配置到运行目录
		cfg := Default()
		if err := Save(cfg, cfgPath); err != nil {
			return nil, fmt.Errorf("生成默认配置失败: %w", err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	cfg := Default()
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 默认值兜底
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "sqlite"
	}
	if cfg.Database.SQLite == "" {
		cfg.Database.SQLite = filepath.Join("data", "mxgt.db")
	}
	if cfg.Cache.Driver == "" {
		cfg.Cache.Driver = "memory"
	}
	if cfg.Resolve.CacheTTL == 0 {
		cfg.Resolve.CacheTTL = 3600
	}
	if cfg.Resolve.Timeout == 0 {
		cfg.Resolve.Timeout = 15
	}

	return cfg, nil
}

// Save 写入配置到指定路径
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeYAML(cfg, path)
}

// RunDir 返回运行文件夹（可执行文件所在目录，用户环境都建在这里）
func RunDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
