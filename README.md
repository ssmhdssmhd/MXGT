# MXGT

> 视频资源聚合 + 苹果 CMS v10 对接 + JSON 解析路由 + 在线播放页的综合后台
> 版本：v0.0.6

## ✨ 特性

- 🖥️ **在线播放页**：`?url=xxx` 动态渲染（DPlayer + hls.js），API 地址不硬编码（query / localStorage / 同域 三层兜底）
- 🛰️ **JSON 解析路由**：可配置**多条解析规则**（对接多个源站），支持 JSONPath / Regex / Custom 三种提取器，按优先级依次匹配
- 🔀 **多提取器**：`jsonpath` / `regex` / `custom` 接口化注册，新增提取器只需实现 `Extractor` 接口
- 📦 **多存储**：数据库支持 SQLite（默认零配置）/ MySQL；缓存支持内存（默认）/ Redis，接口化可扩展
- 🔐 **管理后台**：JWT 登录 + 解析规则 CRUD（多规则动态增删改，管理后台配置即可接入新源站）
- 🔌 **跨域 / 防盗链**：Echo CORS + `/api/proxy/stream` 代理转发（带 Referer，支持 Range 拖动进度条）
- ☁️ **GitHub Actions 云端编译**：push tag 自动编译多平台 Go 可执行文件并发布 Release
- 📦 **免配置运行**：单文件下载即用（go:embed 内嵌前端），无需 Go / MySQL / Redis / Docker

## 🚀 快速开始

### 方式一：直接运行（免配置）

```
1. 到 GitHub Releases 下载对应平台可执行文件
2. 放到任意目录（即「运行文件夹」）
3. ./mxgt 启动
4. 浏览器打开 http://localhost:8080
```

首次启动自动生成 `config.yaml` 和运行目录（`data/` SQLite、`cache/`、`logs/`、`uploads/`）。

### 方式二：源码运行

```bash
go run ./cmd/server
```

### 方式三：Docker

```bash
docker build -t mxgt .
docker run -p 8080:8080 mxgt
```

## 📡 API

| 方法 | 路径 | 说明 | 鉴权 |
|---|---|---|---|
| GET | `/api/health` | 健康检查（版本 / db / cache） | 无 |
| GET | `/api/resolve?url=xxx` | 解析源站 URL → 真实视频链接 | 无 |
| GET | `/api/proxy/stream?url=xxx` | 视频流代理（防盗链 / 跨域） | 无 |
| POST | `/admin/login` | 登录，返回 JWT（默认 admin/admin123，可用环境变量覆盖） | 无 |
| GET | `/admin/rules` | 解析规则列表（多条） | JWT |
| POST | `/admin/rules` | 新增解析规则 | JWT |
| PUT | `/admin/rules/:id` | 更新解析规则 | JWT |
| DELETE | `/admin/rules/:id` | 删除解析规则 | JWT |

`/api/resolve` 返回格式：

```json
{
  "code": 1,
  "msg": "ok",
  "data": {
    "url": "https://xxx/index.m3u8",
    "type": "hls",
    "proxy": false,
    "rule_id": 1,
    "cache_hit": false
  }
}
```

## 🔌 对接多个解析规则（管理后台配置）

解析路由按 `priority` 从高到低依次尝试所有启用规则，URL 正则命中后交给对应提取器。支持**同时配置多条规则**，接入新源站只需新增一条规则：

```bash
# 1. 登录拿 token
TOKEN=$(curl -s -X POST http://localhost:8080/admin/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq -r .data.token)

# 2. 新增 regex 规则（提取 m3u8 直链）
curl -s -X POST http://localhost:8080/admin/rules \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "regex-通用m3u8",
    "url_pattern": "example\\.com",
    "extractor_type": "regex",
    "rule_config": {"regex": "https?://[^\"]+\\.m3u8", "group": 0},
    "priority": 10,
    "need_proxy": 1
  }'

# 3. 新增 jsonpath 规则（页面内嵌 JSON 取值）
curl -s -X POST http://localhost:8080/admin/rules \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "jsonpath-测试",
    "url_pattern": "demo\\.org",
    "extractor_type": "jsonpath",
    "rule_config": {"jsonpath": "$.data.url"},
    "priority": 5
  }'
```

## 🧱 分层架构

```
┌─────────────────────────────────────────────┐
│  L3 前端层  web/player（DPlayer + hls.js）   │
│   ?url=xxx → fetch /api/resolve → 播放       │
├─────────────────────────────────────────────┤
│  L2 中间层  解析路由                          │
│   /api/resolve → 多规则匹配 → 多提取器提取    │
│   /api/proxy/stream → 防盗链 / 跨域转发       │
│   缓存（memory / redis）                      │
├─────────────────────────────────────────────┤
│  L1 后端核心  Echo + GORM                    │
│   config（免配置自动生成） + SQLite/MySQL     │
│   + JWT 管理后台（规则多配置 CRUD）           │
└─────────────────────────────────────────────┘
```

```
internal/
├── config/       # 配置加载（viper，首次运行自动生成 config.yaml）
├── database/     # 数据库（sqlite 默认 / mysql 可选）
├── cache/        # 缓存接口（memory 默认 / redis 可选）
├── models/       # 数据模型（vods / episodes / sources / extract_rules）
├── extractor/    # 提取器接口 + 注册表（jsonpath / regex / custom 可扩展）
├── service/      # 解析服务（多规则匹配 + 提取 + 缓存）
├── handler/      # HTTP 处理器（resolve / proxy / admin / rules / health）
├── middleware/   # JWT 鉴权
└── router/       # 路由注册（分层）
```

## 🔄 更新方式

| 方式 | 步骤 |
|---|---|
| 手动替换 | 下载新 Release 可执行文件 → 覆盖运行目录里的 `mxgt` → 重启 |
| 后台一键 | 后续版本在「更新设置」页一键更新 |

`config.yaml` / `data/` / `uploads/` 全在运行目录，替换执行文件即完成升级。

## 🧠 设计原则

1. 云端编译：所有构建在 GitHub Actions 完成，用户不接触编译
2. 免配置运行：单文件下载即用，首次运行自动建环境
3. 运行目录隔离：配置/数据/日志全在运行文件夹内，卸载即删文件夹
4. 接口化对接：提取器 / 数据库 / 缓存均为接口 + 注册表，可对接多个、方便迭代

## 📚 文档

- [KF思路.md](KF思路.md)：开发者思路文档（总体架构 / 管理后台 / AI 分析 / 部署分发 / 里程碑）

## 📝 更新日志

### v0.0.6
- 新增：Go 分层代码实现——L1 后端核心（config / database / cache / models / JWT 管理后台）
- 新增：L2 中间层解析路由——多规则匹配 + 多提取器（jsonpath / regex / custom）+ proxy + 缓存
- 新增：L3 前端播放页（DPlayer + hls.js，API 不硬编码）
- 新增：GitHub Actions 云端编译多平台可执行文件（build-release.yml）+ Dockerfile
- 新增：免配置单文件运行（首次启动自动建环境，SQLite 默认存储）
