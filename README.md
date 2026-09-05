# MXGT

> 视频资源聚合 + 苹果 CMS v10 对接 + JSON 解析路由 + 在线播放页的综合后台
> 版本：v0.0.16

## ✨ 特性

- 🖥️ **在线播放页**：`?url=xxx` 动态渲染（DPlayer + hls.js），API 地址不硬编码（query / localStorage / 同域 三层兜底）
- 🛰️ **JSON 解析路由**：可配置**多条解析规则**（对接多个源站），支持 JSONPath / Regex / Custom 三种提取器，按优先级依次匹配
- 🔀 **多提取器**：`jsonpath` / `regex` / `custom` 接口化注册，新增提取器只需实现 `Extractor` 接口
- 📡 **多源采集器**：`api` / `html` / `custom` 采集器接口化注册，支持**对接多个采集源**；`POST /admin/sync` 一键采集 → 剧名模糊匹配 → 自动合并入库
- 🍎 **苹果 CMS v10 对接**：`/api.php/provide/vod/` 输出标准 CMS 采集接口（list / detail / search / play），多线路自动用 `$$$` 分隔
- 🎭 **前端设置伪装路径**：单行表配置播放页入口路径（如 `/mx.php`）与参数别名，后端动态注册伪装路由
- 📊 **仪表盘**：调用日志统计（overview / 趋势 / 规则TOP / 采集源TOP），ECharts 前端 /admin-ui
- 📋 **调用日志**：resolve / proxy / cms 自动记录（状态 / 耗时 / 缓存命中 / IP），定期自动清理
- 🗺️ **视频抓取映射**：预置腾讯/爱奇艺/优酷/芒果/搜狐/咪咕/B站七大站字段映射，JSONPath 提取可后台测试
- 🎯 **剧名匹配**：Levenshtein 相似度 + 别名表 + 年份/标点规范化；多源同剧自动合并到同一 vod，集数按来源区分
- 🧠 **匹配策略**：AI/规则双通道（rule / ai / auto）可配回退与相似度阈值；支持直接资源走去插播决策
- 🔌 **调用 Pipeline**：链式串联多节点（proxy / 去插播 / 去广告 / 自定义 HTTP），支持排序、独立启停、三种回退策略与链路测试
- 🔄 **自动更新**：GitHub 多镜像并发测速选最快源，检查更新（semver 对比）+ 一键下载安装（备份旧版 / 保留配置），更新日志记录
- 📦 **多存储**：数据库支持 SQLite（默认零配置）/ MySQL；缓存支持内存（默认）/ Redis，接口化可扩展
- 🔐 **管理后台**：JWT 登录 + 解析规则 CRUD + 采集源管理 CRUD（后台配置即可接入新源站）
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

**云端编译产物覆盖全部主流平台/框架**（GitHub Actions 自动编译，推 tag 即出）：

| 平台 | 架构 | 产物 |
|---|---|---|
| Linux（服务器主流） | amd64 / arm64 / 386 / armv7 | `mxgt-linux-*.zip` |
| Windows | amd64 / arm64 | `mxgt-windows-*.zip` |
| macOS | amd64 / arm64 | `mxgt-darwin-*.zip` |
| FreeBSD | amd64 | `mxgt-freebsd-amd64.zip` |

首次启动自动生成 `config.yaml` 和运行目录（`data/` SQLite、`cache/`、`logs/`、`uploads/`）。

### 方式二：源码运行

```bash
go run ./cmd/server
```

### 方式三：Docker（docker-compose）

```bash
docker compose up -d          # 后台启动（自动构建 + 映射 8080）
docker compose down           # 停止
```

数据持久化在命名卷 `mxgt-data`（`/app/data`，SQLite 在此），重建容器数据不丢失。管理员账号可用环境变量 `MXGT_ADMIN_USER` / `MXGT_ADMIN_PASSWORD` 覆盖。

如需自定义端口，修改 `docker-compose.yml` 的 `ports: - "8080:8080"`。

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
| GET | `/admin/sources` | 采集源列表（多条） | JWT |
| POST | `/admin/sources` | 新增采集源 | JWT |
| PUT | `/admin/sources/:id` | 更新采集源 | JWT |
| DELETE | `/admin/sources/:id` | 删除采集源 | JWT |
| POST | `/admin/sync` | 触发多源采集→匹配→入库 | JWT |
| GET | `/admin/settings` | 前端设置读取（伪装路径等） | JWT |
| PUT | `/admin/settings` | 前端设置更新（伪装路径/参数别名/皮肤） | JWT |
| GET | `/api/settings` | 前端设置公开读取（播放页用） | 无 |
| GET | `/admin/mappings` | 站点映射列表（七大站预置+自定义） | JWT |
| POST | `/admin/mappings` | 新增自定义站点映射 | JWT |
| PUT | `/admin/mappings/:id` | 更新站点映射 | JWT |
| DELETE | `/admin/mappings/:id` | 删除站点映射（预置不可删） | JWT |
| POST | `/admin/mapping/test` | 测试 JSONPath 字段映射提取 | JWT |
| GET | `/admin/stats/overview` | 仪表盘总览（今日/累计/成功率/缓存命中） | JWT |
| GET | `/admin/stats/trends` | 调用趋势（按天聚合） | JWT |
| GET | `/admin/stats/rules-top` | 解析规则调用 TOP | JWT |
| GET | `/admin/stats/sources-top` | 采集源入库量 TOP | JWT |
| GET | `/admin/call-logs` | 最近调用明细（分页） | JWT |
| GET | `/admin/analysis/settings` | 分析引擎设置读取 | JWT |
| PUT | `/admin/analysis/settings` | 分析引擎设置更新（开关/优先级/未知处理） | JWT |
| GET | `/admin/matching/settings` | 匹配策略设置读取（AI/规则双通道） | JWT |
| PUT | `/admin/matching/settings` | 匹配策略设置更新（mode/回退/阈值/直接资源去插播） | JWT |
| POST | `/admin/matching/test` | 测试匹配策略（按剧名匹配已入库影片 / 直接资源去插播决策） | JWT |
| GET | `/admin/chain/nodes` | 调用 Pipeline 节点列表 | JWT |
| POST | `/admin/chain/nodes` | 新增 Pipeline 节点 | JWT |
| PUT | `/admin/chain/nodes/:id` | 更新 Pipeline 节点 | JWT |
| DELETE | `/admin/chain/nodes/:id` | 删除节点（内置不可删） | JWT |
| PUT | `/admin/chain/reorder` | 批量调整节点顺序 | JWT |
| POST | `/admin/chain/test` | 测试整条链路中间结果 | JWT |
| GET | `/admin/update/config` | 更新设置读取（仓库/镜像列表/自动检查） | JWT |
| PUT | `/admin/update/config` | 更新设置保存 | JWT |
| POST | `/admin/update/mirror-speed` | 并发测速所有镜像，返回最快源 | JWT |
| POST | `/admin/update/check` | 检查远端最新版本（GitHub API 对比） | JWT |
| POST | `/admin/update/download` | 一键下载并替换可执行文件（备份旧版） | JWT |
| GET | `/admin/update/logs` | 最近更新日志 | JWT |
| GET | `/admin-ui` | 管理后台仪表盘前端（ECharts） | - |
| GET | `/api.php/provide/vod/?ac=list` | 苹果 CMS v10 分类列表 | 无 |
| GET | `/api.php/provide/vod/?ac=detail&ids=1` | 苹果 CMS v10 详情 | 无 |
| GET | `/api.php/provide/vod/?ac=search&wd=关键词` | 苹果 CMS v10 搜索 | 无 |
| GET | `/api.php/provide/vod/?ac=play&id=1&ep=1` | 苹果 CMS v10 播放地址 | 无 |

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

## 📡 对接多个采集源（多源采集）

采集器支持 `api`（苹果 CMS / 通用 JSON）与 `html`（页面正则）等类型，**可同时配置多个源**。`POST /admin/sync` 会遍历所有启用的源采集，同一部剧自动按剧名匹配合并到同一 vod，集数按来源区分：

```bash
# 新增 API 采集源（苹果 CMS 格式，支持 {keyword} 占位符）
curl -s -X POST http://localhost:8080/admin/sources \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "源A-苹果CMS",
    "source_type": "api",
    "fetch_url": "https://example.com/api.php/provide/vod/?ac=detail&wd={keyword}",
    "extract_rules": {},
    "priority": 10
  }'

# 新增 HTML 采集源
curl -s -X POST http://localhost:8080/admin/sources \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "源B-页面",
    "source_type": "html",
    "fetch_url": "https://example.com/search?q={keyword}",
    "priority": 5
  }'

# 触发多源采集 → 匹配 → 入库
curl -s -X POST http://localhost:8080/admin/sync \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
  -d '{"keyword": "庆余年"}'
```

`/admin/sync` 返回每个源的结果统计：

```json
{
  "code": 1,
  "data": {
    "sources": [
      {"source_id": 1, "source_name": "源A-苹果CMS", "fetched": 2, "created": 2, "updated": 0, "episodes": 4, "errors": 0},
      {"source_id": 2, "source_name": "源B-页面", "fetched": 1, "created": 0, "updated": 1, "episodes": 2, "errors": 0}
    ],
    "total": {"fetched": 3, "created": 2, "episodes": 6}
  }
}
```

## 🍎 苹果 CMS v10 对外接口

`GET /api.php/provide/vod/` 兼容苹果 CMS v10 标准采集接口（`ac=list` / `detail` / `search` / `play`），配合多源采集可一键把「采集到的多源数据」输出为标准 CMS 数据，供任意支持该规范的站点 / 播放器对接：

```bash
# 分类列表（分页 ?pg=，分类筛选 ?t=）
curl "http://localhost:8080/api.php/provide/vod/?ac=list&pg=1"

# 详情（多 ids 用逗号分隔）
curl "http://localhost:8080/api.php/provide/vod/?ac=detail&ids=1,2"

# 搜索
curl "http://localhost:8080/api.php/provide/vod/?ac=search&wd=庆余年"

# 播放：ep 指定集数返回真实直链；不传 ep 返回完整播放串
curl "http://localhost:8080/api.php/provide/vod/?ac=play&id=1&ep=1"
```

返回遵循苹果 CMS v10 schema：`code/msg/page/pagecount/limit/total/list[]`，其中 `vod_play_from` 与 `vod_play_url` 支持多线路，线路间用 `$$$` 分隔、集间用 `#` 分隔：

```json
{
  "code": 1,
  "msg": "数据列表",
  "page": 1,
  "pagecount": 1,
  "limit": "20",
  "total": 1,
  "list": [
    {
      "vod_id": 1,
      "vod_name": "测试剧集A",
      "vod_year": "2026",
      "vod_area": "中国大陆",
      "vod_play_from": "测试API源A$$$测试API源B",
      "vod_play_url": "第1集$http://.../1.m3u8#第2集$http://.../2.m3u8$$$第3集$http://.../3.m3u8"
    }
  ]
}
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
├── collector/    # 采集器接口 + 多源实现（api / html / custom）
├── matcher/      # 剧名模糊匹配（Levenshtein + 别名）+ 集数提取
├── cms/          # 苹果 CMS v10 结构体 + vod 组装（多线路 play_from/play_url）
├── extractor/    # 提取器接口 + 注册表（jsonpath / regex / custom 可扩展）
├── service/      # 业务服务（解析路由 / 多源采集同步）
├── handler/      # HTTP 处理器（resolve / proxy / admin / rules / sources / sync / cms / health）
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

### v0.0.16
- 新增：🔄 M14 更新设置——internal/updater（semver 版本比较 v0.0.99→v0.1.0 / 镜像并发测速选最快 / GitHub API 检查更新 / 公告解析 / 下载→备份→覆盖）
- 新增：updater_config + update_logs 表，`/admin/update/config · mirror-speed · check · download · logs` 接口

### v0.0.15
- 新增：🔌 M13 调用 Pipeline——internal/chaining（Pipeline 引擎 + `{input_url}` 占位符 + JSONPath 结果提取）
- 新增：chain_nodes 表（sort_order 排序 / 独立启停 / fallback 三种回退策略 skip·abort·fallback / 内置 proxy·skip_ad·block_ad 预置节点）
- 新增：`/admin/chain/nodes` CRUD + `/admin/chain/reorder` 排序 + `/admin/chain/test` 链路测试（返回每步中间结果）

### v0.0.14
- 新增：🎯 M12 匹配策略——matching_settings 单行表（mode=rule/ai/auto 双通道 + fallback 回退 + fuzzy_threshold 模糊阈值 + auto_create 自动入库 + direct_action 直接资源去插播）
- 新增：matcher 双通道策略引擎（规则匹配 + AI 自动识别），OpenAI 兼容 AI 匹配客户端（openai/doubao/custom 可配）
- 新增：`POST /admin/matching/test` 一键测试（按剧名匹配已入库影片 / 直接资源去插播决策）+ `/admin/matching/settings` CRUD

### v0.0.13
- 新增：🐳 M10 部署——docker-compose.yml（一键 `docker compose up -d`，命名卷持久化 SQLite 数据，环境变量覆盖管理员账号），Dockerfile 多阶段构建

### v0.0.12
- 新增：📊 M9 仪表盘——call_logs 调用日志表 + resolve/proxy/cms 自动记录（状态/耗时/缓存命中/IP，定期清理）
- 新增：统计接口 overview / trends / rules-top / sources-top / call-logs（JWT）
- 新增：📊 ECharts 仪表盘前端 `/admin-ui`
- 新增：管理后台静态资源 `web/admin` 内嵌

### v0.0.11
- 新增：🎭 前端设置伪装路径——frontend_settings 单行表（play_path / url_param / 参数别名 / 皮肤 / 备案号），后端动态注册伪装路由（如 `/mx.php`），默认 `/` 与伪装路径同时可用
- 新增：🗺️ 视频抓取映射——site_mappings 表 + 腾讯/爱奇艺/优酷/芒果/搜狐/咪咕/B站七大站预置数据
- 新增：`POST /admin/mapping/test` 字段映射 JSONPath 提取测试接口
- 新增：`GET/PUT /admin/settings` 与公开 `GET /api/settings`

### v0.0.10
- 修复：移除不支持的 windows/386（modernc.org/sqlite 不支持 32 位 Windows），云端编译矩阵全部平台编译成功

### v0.0.9
- 新增：GitHub Actions 云端编译矩阵扩展至全部主流平台/框架（Linux amd64/arm64/386/armv7 + Windows amd64/arm64 + macOS amd64/arm64 + FreeBSD amd64）
- 优化：Release 打包改为动态遍历，自动为每个平台生成 zip 与校验和

### v0.0.8
- 新增：🍎 苹果 CMS v10 适配——`cms` 包（CMSVod / ListResponse / PlayResponse 结构体 + 多线路 vod 组装）
- 新增：`GET /api.php/provide/vod/` 对外接口（ac=list / detail / search / play），输出标准 CMS 采集数据
- 新增：多线路输出——同一剧多源合并后 `vod_play_from` / `vod_play_url` 自动用 `$$$` 分隔
- 打通：多源采集 → 剧名匹配合并 → 入库 → 苹果 CMS 对外输出的完整闭环

### v0.0.7
- 新增：📡 多源采集器——Collector 接口 + 注册表（api / html / custom 可扩展，对接多个采集源）
- 新增：🎯 剧名模糊匹配（Levenshtein 相似度 + 别名 + 规范化）与集数提取（第N集 / EP N / 纯数字）
- 新增：`POST /admin/sync` 多源采集 → 匹配合并 → 入库 vods/episodes（同剧多源自动合并）
- 新增：采集源管理 CRUD（`/admin/sources`）

### v0.0.6
- 新增：Go 分层代码实现——L1 后端核心（config / database / cache / models / JWT 管理后台）
- 新增：L2 中间层解析路由——多规则匹配 + 多提取器（jsonpath / regex / custom）+ proxy + 缓存
- 新增：L3 前端播放页（DPlayer + hls.js，API 不硬编码）
- 新增：GitHub Actions 云端编译多平台可执行文件（build-release.yml）+ Dockerfile
- 新增：免配置单文件运行（首次启动自动建环境，SQLite 默认存储）
