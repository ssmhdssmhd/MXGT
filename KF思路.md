# MXGT — 开发者思路文档

> 项目定位：视频资源聚合 + 苹果 CMS v10 对接 + JSON 解析路由 + 在线播放页的综合后台
> 版本：v0.0.9
> 技术栈：Go + Echo + GORM + MySQL + Redis + Docker + GitHub Actions（云端编译）
> 分发策略：GitHub Actions 云端编译多平台 Go 可执行文件 → 单文件免配置运行 → 更新只替换执行文件

---

# 🔝 总体思路（前端 / 中间 / 后端 三层）

```
┌─────────────────────────────────────────────────────────┐
│  🖥️ 前端（播放页）  域名/?url=xxx&title=xxx&ep=5       │
│  - 接收 query 参数动态渲染播放器                        │
│  - 不硬编码后端地址，自动同域/或读取 window.API_BASE    │
│  - 跨域播放器：hls.js / dplayer / flv.js              │
└─────────────────────────┬───────────────────────────────┘
                          │ fetch(`${API_BASE}/api/resolve?url=xxx`)
                          ▼
┌─────────────────────────────────────────────────────────┐
│  🛰️ 中间层（后端解析路由）                             │
│  - 接收任意源站播放页 URL                               │
│  - extract_rules 表匹配 url_pattern → 选 extractor     │
│  - 支持 JSONPath / Regex / 自定义三种提取器            │
│  - 处理跨域：后端 proxy 或前端跨域播放器                │
│  - 缓存解析结果（Redis）                                │
└─────────────────────────┬───────────────────────────────┘
                          │ REST / JSON
                          ▼
┌─────────────────────────────────────────────────────────┐
│  ⚙️ 后端核心                                           │
│  - 多源采集 → 自动匹配剧名/集数 → 入库                │
│  - 苹果 CMS v10 格式输出                                │
│  - 管理后台：采集源 / 解析规则 / 手动触发               │
└─────────────────────────────────────────────────────────┘
```

---

## 🖥️ 一、前端思路（在线播放页）

### 1. 访问方式

```
https://你的域名/?url=源站播放页URL&title=剧名&ep=集数&line=线路
```

**示例：**
```
https://play.mxgt.com/?url=https://v.qq.com/x/cover/abc123/e00123.html&title=庆余年第二季&ep=5
```

| 参数 | 必填 | 说明 |
|---|---|---|
| `url` | ✅ | 源站播放页 URL（URL encode），后端根据此选解析规则 |
| `title` | ❌ | 剧名，前端展示用 |
| `ep` | ❌ | 集数，前端展示用 |
| `line` | ❌ | 播放线路标签（如 `主线` / `备用`） |

### 2. URL 不硬编码

前端获取后端 API 地址的优先级：

```js
// 1. 从 query 参数 api_base 读取（部署时灵活切换）
const urlParams = new URLSearchParams(location.search);
const apiBase = urlParams.get('api_base')

// 2. 从 localStorage 读取（开发调试用）
    || localStorage.getItem('MXGT_API_BASE')

// 3. 默认同域（生产环境，前端页面和后端同部署）
    || `${location.protocol}//${location.host}`;

// 4. 全局注入（可选，Nginx/后端模板注入）
//    window.__MXGT_CONFIG__.API_BASE

window.MXGT_API_BASE = apiBase.replace(/\/+$/, '');
```

### 3. 跨域处理

**场景：** 前端页面在 `A域名`，后端 API 在 `B域名`，或 m3u8 视频源本身跨域。

| 跨域类型 | 解决方案 |
|---|---|
| **前端 → 后端 API 跨域** | 后端 Echo 开启 CORS 中间件（允许 `*` 或白名单域名），携带 `Access-Control-Allow-Origin` |
| **m3u8 视频流跨域** | 方案 A：后端 proxy `/api/proxy/m3u8?url=xxx` 转发；方案 B：用跨域播放器（hls.js 原生支持 CORS）；方案 C：Nginx 反向代理 |
| **视频流含 ts 分片 403/Referer** | 后端 proxy 转发，自动携带 Referer / UA，前端只请求自家 proxy |

**推荐组合：**

```
前端页面（同域或子域）
    └─ 请求 /api/resolve → 后端解析路由
         └─ 返回两种结果之一：
              a) 直接返回可跨域的 .m3u8 链接 → 前端 hls.js 直连
              b) 返回需要代理的链接 → 前端请求 /api/proxy/stream?token=xxx
                   └─ 后端 proxy 带 Referer 转发源站 → 返回给前端
```

### 4. 前端技术选型（单文件方案）

| 需求 | 方案 |
|---|---|
| 播放器核心 | **DPlayer**（B站开源，支持 hls / flv / mp4，广告少）或 **hls.js + plyr** |
| 页面框架 | 单文件 HTML + Vue3 CDN，打包后就是一个 index.html |
| 样式 | TailwindCSS CDN |
| 部署 | 后端 Echo 的 `StaticFS` 或 Nginx 直接 serve 静态文件 |

### 5. 前端核心交互流程

```
用户打开 ?url=xxx
    │
    ├─ 解析 query 参数 → 显示剧名/集数
    ├─ fetch(`${API_BASE}/api/resolve?url=${encodeURIComponent(url)}`)
    │      │
    │      ├─ 后端返回 { url: "https://xxx.m3u8", type: "hls", proxy: false }
    │      │      └─ hls.js.loadSource(url) → 播放
    │      │
    │      └─ 后端返回 { url: "/api/proxy/stream?token=abc", proxy: true }
    │             └─ hls.js.loadSource(`${API_BASE}${url}`) → 走后端代理
    │
    └─ 播放出错 → 自动切备用线路（如果有）
```

### 6. 前端核心文件结构

```
web/player/
├── index.html         # 播放页入口（单文件，CDN 引入）
├── js/
│   ├── player.js      # 播放器初始化、API 调用、跨域处理
│   └── api.js         # 封装 resolve / proxy 调用
└── css/
    └── style.css      # 基础样式
```

### 7. 安全注意事项

- `url` 参数必须 URL encode，防止 XSS
- 后端 `/api/resolve` 要校验调用频率（限流），防止被恶意爬取
- proxy 接口要做 token 签名（短时效），不能裸传目标 URL
- 前端不保存任何敏感信息，不走 cookie/session

---

## 🛰️ 二、中间层思路（后端解析路由核心）

中间层是前端和后端核心之间的桥梁，**同时也是解析引擎本身**。

### 1. 职责

1. **接收前端请求**：`GET /api/resolve?url=xxx`
2. **路由匹配**：根据 `url` 匹配 `extract_rules.url_pattern`（正则）
3. **解析提取**：用匹配到的 extractor 从源站 JSON/HTML 中提取真实视频 URL
4. **跨域处理**：返回直链 OR 返回 proxy token
5. **缓存**：Redis 缓存解析结果，减少重复请求

### 2. 解析流程

```
请求 /api/resolve?url=https://v.qq.com/x/cover/abc/e00123.html
    │
    ▼
① 查 Redis 缓存（key = "resolve:{url hash}"）
    │ hit → 直接返回
    │ miss ↓
    ▼
② GET extract_rules WHERE enabled=1 ORDER BY priority DESC
   依次尝试 url_pattern 正则匹配
    │ 匹配到 rule_id=5
    │ 无匹配 → 返回 404 + 提示添加规则
    ▼
③ 按 rule.extractor_type 分发：
    ├─ "jsonpath" → 先用 resty 请求源站播放页，
    │               提取页面里的内嵌 JSON → 用 jsonpath 取值
    ├─ "regex"   → resty 请求页面 → regex 匹配捕获组
    └─ "custom"  → 调用预注册的自定义解析函数（Go 代码实现）
    │
    ▼
④ 拿到真实视频 URL
    │
    ├─ 判断是否需要代理（看 rule_config 里标记 或 后端配置的域名黑名单）
    │      ├─ 不需要 → { url, proxy: false }
    │      └─ 需要   → 生成短期 token → { url: "/api/proxy/stream?token=xxx", proxy: true }
    │
    ▼
⑤ 写入 Redis 缓存（TTL 可配置，默认 1 小时）
    ▼
⑥ 返回给前端
```

### 3. Extractor 接口（Go）

```go
// internal/extractor/extractor.go
type Extractor interface {
    // Name 提取器名称
    Name() string
    // Extract 从目标 URL 页面内容中提取真实视频链接
    // pageURL: 原始播放页 URL
    // content: 页面内容（HTML 或 JSON 文本）
    // ruleConfig: 从数据库读出的 rule_config
    Extract(ctx context.Context, pageURL, content string, ruleConfig map[string]any) (string, error)
}
```

三种实现：

```
jsonpath  ──▶ 提取 page 中内嵌 JSON → JSONPath 取值（如 $.video_info.url）
regex     ──▶ 纯正则匹配 + 捕获组
custom    ──▶ Go 硬编码（如处理加密签名/多步跳转/需要 JS 执行的复杂源）
```

### 4. Proxy 接口（解决跨域/防盗链）

```
GET /api/proxy/stream?token=abc123
    │
    ├─ 从 token 解码出原始目标 URL + 过期时间
    ├─ 校验未过期
    ├─ resty 请求目标 URL（携带预设的 Referer / UA / 自定义 Header）
    ├─ 流式转发给前端（206 Partial Content 支持拖动进度条）
    └─ 设置 CORS Header 允许前端跨域访问
```

**为什么要 proxy：**
- 很多源站视频有 Referer 校验，前端直连 403
- 部分域名需要特定 UA 或 Cookie
- 统一解决 CORS 问题，前端无需关心各源站差异

### 5. 跨域方案总览（后端视角）

```go
// CORS 中间件配置（Echo）
e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
    AllowOrigins: []string{"*"},   // 或白名单
    AllowMethods: []string{"GET", "POST", "OPTIONS"},
    AllowHeaders: []string{"Origin", "Content-Type", "Authorization"},
}))
```

三层跨域防线：
1. **Echo CORS 中间件** → 前端调 API 没问题
2. **Extractor 返回直链** → hls.js 直接播（前提是视频源本身允许 CORS）
3. **Proxy 兜底** → 任何复杂防盗链视频都能播，后端代请求

---

## ⚙️ 三、后端核心思路（原有内容，保持不变）

> 以下是完整的后端设计，含目录结构、数据库、接口、技术点、里程碑等。

---

## 四、核心业务链路（端到端总览）

```
┌─────────────┐     ┌──────────────┐     ┌─────────────────┐     ┌──────────────┐
│  多源采集    │────▶│  自动匹配映射  │────▶│  JSON 解析路由   │────▶│  对外输出     │
│  (爬虫/API)  │     │  剧名+集数     │     │  (提取真实URL)   │     │  苹果CMS v10  │
└─────────────┘     └──────────────┘     └─────────────────┘     └──────────────┘
```

| 环节 | 做什么 | 关键技术 |
|---|---|---|
| 采集 | 从 N 个源站/API 拉取影视剧数据（剧名、海报、集数列表、播放页 URL） | HTTP Client、正则/JSON 提取、并发 goroutine |
| 映射 | 把源站数据 → 统一的内部结构体，自动匹配剧名+集数（同名/别名/模糊匹配） | 字符串相似度、别名表、集数正则 |
| 解析路由 | 拿播放页 URL → 调后台配置的解析接口 JSON → 提取真实视频链接 | JSONPath / 自定义 JSON 提取规则、HTTP 代理转发 |
| 输出 | 按苹果 CMS v10 的 JSON 接口格式吐数据（/api.php/provide/vod/ 那套） | 固定 JSON 结构输出、源站切换 |

---

## 五、目录结构（分层架构）

```
mxgt/
├── cmd/server/
│   └── main.go                 # 程序入口
├── configs/
│   ├── config.yaml             # 运行时配置
│   └── config.example.yaml     # 配置模板
├── internal/                   # 私有包（不对外暴露）
│   ├── config/                 # 配置加载（viper）
│   ├── collector/              # 采集器
│   │   ├── collector.go        # Collector 接口：Fetch() → []RawItem
│   │   ├── factory.go          # 工厂：按配置选择采集器
│   │   ├── sources/
│   │   │   ├── source_api.go       # 通用 JSON API 源
│   │   │   ├── source_html.go      # HTML 页面源（正则提取）
│   │   │   └── source_custom.go    # 自定义脚本源
│   │   └── models.go
│   ├── matcher/                # 匹配映射器
│   │   ├── matcher.go
│   │   ├── fuzzy.go            # 剧名模糊匹配（Levenshtein + 别名表）
│   │   ├── episode.go          # 集数提取正则
│   │   └── alias.go
│   ├── extractor/              # ⭐ JSON 解析路由（前端 ↔ 后端 的桥）
│   │   ├── extractor.go        # Extractor 接口
│   │   ├── jsonpath.go         # JSONPath 实现
│   │   ├── regex_ext.go        # 正则实现
│   │   ├── custom.go           # 自定义实现（硬编码复杂解析）
│   │   ├── route.go            # 路由匹配：url_pattern → rule
│   │   ├── proxy.go            # ⭐ proxy 转发（解决跨域/防盗链）
│   │   └── cache.go            # Redis 缓存解析结果
│   ├── analyzer/               # 🧠 资源类型分析引擎（新增）
│   │   ├── analyzer.go         # Analyzer 接口：输入 URL → 判断 official / direct / unknown
│   │   ├── official.go         # 官方七大站域名正则匹配
│   │   ├── direct.go           # HEAD 请求探测 .m3u8 / .mp4 / .flv Content-Type
│   │   └── ai.go               # AI 辅助分析（可选，openai / doubao / custom）
│   ├── ai/                     # 🤖 AI 智能分析 ts / 广告 / 字幕（新增）
│   │   ├── ai.go               # AI 服务封装（openai / doubao / qwen / custom 多模态）
│   │   ├── m3u8.go             # m3u8 解析 → ts 分片列表（序号/时长/大小/URL）
│   │   ├── md5.go              # 分片 MD5 流式计算 + 指纹库 O(1) 比对
│   │   ├── tsanalyzer.go       # ts 分片分析主流程（并发下载 + 双通道判定）
│   │   ├── frame.go            # ts 解码抽帧（ffmpeg 可选 / 纯 Go mpegts 解析）
│   │   ├── subtitle.go         # 字幕 / 水印 / 台标检测
│   │   ├── clean.go            # 去广告 m3u8 动态生成（剔除广告 ts）
│   │   └── verdict.go          # 判定聚合：normal / ad / subtitle / interlude / unknown
│   ├── chaining/               # 🔌 调用 Pipeline（新增）
│   │   ├── pipeline.go         # Pipeline 引擎：链式执行 chain_nodes
│   │   ├── node.go             # 节点执行器（skip_ad / block_ad / proxy / custom）
│   │   ├── fallback.go         # 三种回退策略：skip / abort / fallback
│   │   └── templating.go       # {input_url} 占位符替换 + JSONPath 结果提取
│   ├── updater/                # 🔄 自动更新（新增）
│   │   ├── updater.go          # 更新主流程：测速 → 检查版本 → 下载 → 安装
│   │   ├── speedtest.go        # 并发测速 BuiltinMirrors → 选最快
│   │   ├── semver.go           # 版本号比较（v0.0.99 → v0.1.0）
│   │   ├── notice.go           # 公告.txt 解析
│   │   └── installer.go        # 下载 → 校验 → 备份 → 解压 → 覆盖
│   ├── cms/                    # 苹果 CMS 适配层
│   │   ├── cms_v10.go
│   │   └── models.go
│   ├── handler/
│   │   ├── resolve_handler.go  # ⭐ /api/resolve 接口（前端调用最多）
│   │   ├── proxy_handler.go    # ⭐ /api/proxy/stream 接口
│   │   ├── api_handler.go      # 对外苹果 CMS 兼容输出
│   │   ├── admin_handler.go    # 管理后台
│   │   └── task_handler.go     # 手动触发任务
│   ├── service/
│   │   ├── sync_service.go
│   │   ├── resolve_service.go
│   │   └── cms_service.go
│   ├── repository/
│   │   ├── vod_repo.go
│   │   ├── episode_repo.go
│   │   ├── source_repo.go
│   │   └── rule_repo.go
│   ├── middleware/             # CORS / JWT / 限流 / 日志
│   └── router/
├── pkg/
│   ├── httpclient/             # resty 封装（超时/重试/UA/压缩/Referer 注入）
│   ├── jsonpath/
│   ├── response/
│   ├── errors/
│   └── utils/
├── migrations/
├── deployments/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   └── .env.example
├── .github/workflows/
│   ├── ci.yml               # 代码检查 + 单测
│   └── build-release.yml    # ⭐ 云端编译：push tag → 多平台编译 → 发布 GitHub Release
├── web/
│   ├── player/              # ⭐ 前端在线播放页（?url=xxx）—— go:embed 打进可执行文件
│   │   ├── index.html
│   │   ├── js/player.js
│   │   └── js/api.js
│   └── admin/               # 管理后台
│       ├── ai/              # 🤖 AI 分析页（m3u8 分析 + 内置播放器 + 实时标注 + 指纹库）
│       └── ...              # 其余各模块页面（后补）
├── run/                     # ⭐ 运行目录（用户放可执行文件的地方，首次运行自动创建）
│   ├── mxgt                 # ← 可执行文件（更新时只替换这一个）
│   ├── config.yaml          # ← 用户配置（首次运行自动生成）
│   ├── data/                # ← SQLite 数据库（默认，免配置）
│   ├── cache/               # ← 本地缓存（无 Redis 时）
│   ├── logs/                # ← 日志
│   └── uploads/             # ← 上传
├── test/
├── go.mod
├── go.sum
└── README.md
```

---

## 六、数据库表设计（核心 4 张表）

### vods — 影片主表

```sql
CREATE TABLE vods (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    vod_id          VARCHAR(64)  NOT NULL UNIQUE COMMENT '外部源唯一标识',
    name            VARCHAR(255) NOT NULL COMMENT '标准剧名',
    alias           VARCHAR(512) DEFAULT '' COMMENT '别名，逗号分隔',
    cover           VARCHAR(512) DEFAULT '',
    year            SMALLINT,
    region          VARCHAR(64),
    category        VARCHAR(128),
    remark          VARCHAR(255) DEFAULT '',
    status          TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name),
    INDEX idx_category (category)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### episodes — 集数表

```sql
CREATE TABLE episodes (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    vod_id          BIGINT UNSIGNED NOT NULL,
    episode_no      INT NOT NULL,
    episode_name    VARCHAR(255) DEFAULT '',
    source_url      VARCHAR(1024) NOT NULL COMMENT '源站播放页 URL（前端 ?url= 的就是这个）',
    resolved_url    VARCHAR(1024) DEFAULT '' COMMENT '缓存的解析结果（可空，按需解析）',
    source_name     VARCHAR(128) DEFAULT '',
    play_line       VARCHAR(128) DEFAULT '',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_vod_ep (vod_id, episode_no, source_name),
    INDEX idx_source_url (source_url(255)),
    CONSTRAINT fk_vod FOREIGN KEY (vod_id) REFERENCES vods(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### sources — 采集源配置

```sql
CREATE TABLE sources (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    source_type     VARCHAR(32)  NOT NULL COMMENT 'api / html / custom',
    fetch_url       VARCHAR(512) NOT NULL COMMENT '支持 {keyword} 占位符',
    method          VARCHAR(8)   DEFAULT 'GET',
    headers         JSON,
    params          JSON,
    extract_rules   JSON         NOT NULL COMMENT '字段提取规则',
    priority        INT DEFAULT 0,
    enabled         TINYINT DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### extract_rules — 解析规则（中间层核心配置）

```sql
CREATE TABLE extract_rules (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    url_pattern     VARCHAR(512) NOT NULL COMMENT 'URL 匹配正则',
    extractor_type  VARCHAR(32)  NOT NULL COMMENT 'jsonpath / regex / custom',
    rule_config     JSON         NOT NULL COMMENT '具体规则',
    target_field    VARCHAR(64)  DEFAULT 'url',
    need_proxy      TINYINT DEFAULT 0 COMMENT '1=需要走 proxy（防盗链域名）',
    proxy_headers   JSON         COMMENT 'proxy 请求时需要额外注入的 Header（Referer / UA / Cookie）',
    priority        INT DEFAULT 0,
    enabled         TINYINT DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_pattern (url_pattern(128))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**rule_config 示例：**

```json
// JSONPath 类型（页面内嵌 JSON 用这个）
{ "jsonpath": "$.video_info.playurl[0].url" }

// 正则类型（纯文本里找链接）
{ "regex": "player_url\\s*=\\s*[\"']([^\"']+\\.m3u8[^\"']*)[\"']", "group": 1 }

// Custom 类型（硬编码 Go 函数）
{ "func": "qq_video_extract", "params": { "api_key": "xxx" } }
```

---

## 七、核心接口

### ⭐ 中间层接口（前端直接调用）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/resolve?url=xxx` | 解析源站 URL → 返回真实视频链接（含是否需要 proxy 的标记） |
| GET | `/api/proxy/stream?token=xxx` | proxy 转发视频流（带防盗链 Header，跨域安全） |

**resolve 返回格式：**

```json
{
    "code": 1,
    "msg": "ok",
    "data": {
        "url": "https://xxx.com/20240901/abc123/index.m3u8",
        "type": "hls",
        "proxy": false,
        "rule_id": 5,
        "cache_hit": false
    }
}
```

```json
{
    "code": 1,
    "msg": "ok",
    "data": {
        "url": "/api/proxy/stream?token=eyJhbGciOi...",
        "type": "hls",
        "proxy": true,
        "rule_id": 3,
        "cache_hit": false
    }
}
```

### 对外（苹果 CMS v10 兼容）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api.php/provide/vod/?ac=list&t=1&pg=1` | 分类列表 |
| GET | `/api.php/provide/vod/?ac=detail&ids=123` | 影片详情（含集数） |
| GET | `/api.php/provide/vod/?ac=search&wd=xxx` | 搜索 |
| GET | `/api.php/provide/vod/?ac=play&id=123&ep=5` | 直接返回某集真实播放链接（苹果 CMS 调用） |

### 对内（管理后台）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/admin/login` | 登录 |
| CRUD | `/admin/source` | 采集源管理 |
| CRUD | `/admin/extract/rule` | 解析规则管理 |
| POST | `/admin/sync` | 触发全量/增量采集 |
| POST | `/admin/resolve/test` | 测试某 URL 能被哪条规则解析 |
| GET  | `/admin/vods` | 影片列表 |
| CRUD | `/admin/ai/settings` | 🤖 AI 分析配置 |
| POST | `/admin/ai/analyze` | 🤖 分析 m3u8（解析全部 ts → MD5 + AI 判定广告/字幕/插播） |
| GET  | `/admin/ai/ts` | 🤖 查看某次分析的 ts 列表（可按判定筛选） |
| GET  | `/admin/ai/result/m3u8` | 🤖 去广告后的干净 m3u8 |
| GET  | `/admin/ai/fingerprints` | 🤖 MD5 指纹特征库（广告/字幕/插播） |

---

## 八、管理后台设计（侧边栏 + 页面）

### 侧边栏完整菜单

```
📊 仪表盘（Dashboard）
│   ├── 调用统计图表（resolve / proxy / cms 各接口调用量、成功率、耗时）
│   ├── 今日调用量 / 近7日趋势
│   ├── 各解析规则命中率 TOP
│   ├── 各采集源入库量 TOP
│   ├── 系统状态（MySQL / Redis 连接、版本号）
│   └── 快捷入口（触发采集 / 测试解析）
│
🎨 前端设置（Frontend）
│   ├── 输出格式选择（用户调用时返回 JSON 接口 或 URL 网页播放器 两种模式）
│   ├── 播放页伪装路径（自定义 ?url= 前后缀，如 /mx.php?url=xxx）
│   ├── 播放器皮肤（DPlayer / hls.js 默认参数、主题色、LOGO）
│   ├── API_BASE 注入 / 跨域设置（前端 URL 不硬编码）
│   └── 页脚 / 版权 / 备案信息
│
🧠 分析设置（Analysis）
│   ├── 官方资源自动识别（输入 URL → 自动判断：腾讯 / 爱奇艺 / 优酷 / 芒果 / 搜狐 / 哔哩哔哩 / 咪咕）
│   ├── 从官方资源自动获取并映射 剧名 和 集数
│   ├── 可播放资源识别（输入直接 m3u8/mp4 链接 → 走专门的匹配规则）
│   ├── 分析引擎开关 / 优先级
│   └── 一键测试：粘贴 URL → 返回分析结果（官方站 / 直链）
│
🤖 AI 设置（AI Analysis）
│   ├── AI 服务配置（provider / api_key / endpoint / model，支持 openai / doubao / qwen / custom）
│   ├── m3u8 智能分析（输入 m3u8 → 自动解析全部 ts 分片）
│   │   ├── 实时查看每个 ts（序号 / 时长 / 大小 / MD5 / AI 判定：正常·广告·字幕·插播）
│   │   ├── 广告 / 字幕 / 插播自动识别（MD5 指纹命中 + AI 画面分析双通道）
│   │   ├── 视频内嵌广告检测（广告直接烧录在 ts 画面里，靠 MD5 特征库 + AI 抽帧识别）
│   │   └── MD5 指纹库（同一广告片段跨视频重复出现 → 指纹一致秒级命中）
│   ├── 内置播放器（管理后台内嵌，可预览 原始 m3u8 / 去广告 m3u8 / 单个 ts 片段，播放时实时标注当前 ts 的判定）
│   ├── 去插播 / 去广告（一键剔除广告 ts → 动态生成干净的 m3u8）
│   ├── 实时分析日志（SSE 推送分析进度与命中明细）
│   └── 分析参数（AI 抽样比例、是否自动去广告、指纹库管理 / 导入）
│
🎯 匹配设置（Matching）
│   ├── AI 自动识别匹配（可选，需配置 AI Key）
│   ├── 指定规则匹配（域名正则 / JSONPath / 字段映射）
│   ├── 匹配模式选择：
│   │   ├── 官方链接 → 走匹配模式（解析剧名 + 集数 + 播放页 URL）
│   │   └── 直接播放资源 → 走去插播配置（跳过剧名匹配，直接套广告/插播规则）
│   └── 匹配优先级：AI > 指定规则 > 去插播兜底
│
🔌 调用设置（Chaining）
│   ├── 多层接口串联（A → B → C，按顺序依次调用）
│   ├── 套去插播接口（匹配到资源后，自动调用去广告/去插播接口处理）
│   ├── 套去广告接口（同上，可与去插播叠加）
│   ├── 每个环节可独立启用 / 禁用 / 调整顺序
│   ├── 请求 Header / Body 透传配置
│   └── 出错回退策略（某层失败是否跳过继续 / 直接返回原始结果）
│
🗺️ 映射设置（Mapping）
│   ├── 官方七大站预置配置
│   │   ├── 🟦 腾讯视频 (v.qq.com)
│   │   ├── 🟩 爱奇艺 (iqiyi.com)
│   │   ├── 🟧 优酷 (youku.com)
│   │   ├── 🟥 芒果TV (mgtv.com)
│   │   ├── 🟪 搜狐视频 (tv.sohu.com)
│   │   ├── 🟨 咪咕视频 (miguvideo.com)
│   │   └── 🟫 哔哩哔哩 (bilibili.com)
│   ├── 剧名字段映射（name / vod_name / title / video_name ...）
│   ├── 集数字段映射（episodes / urls / play_list ...）
│   ├── 自定义字段映射 JSONPath（只提取需要的字段）
│   └── 一键测试：粘贴源站 URL → 返回映射结果
│
⚙️ 解析规则（Extract Rules）
│   ├── 新增 / 编辑 / 删除解析规则
│   ├── URL 正则匹配测试
│   ├── JSONPath / Regex / Custom 三种类型
│   └── need_proxy 标记
│
📼 影片管理（Vods）
│   ├── 影片列表 + 搜索 + 筛选
│   ├── 集数列表
│   ├── 手动增删改
│   └── 别名维护
│
🔄 更新设置（Updater）
│   ├── GitHub 仓库地址（默认：https://github.com/ssmhdssmhd/MXGT）
│   ├── 多条内置镜像（国内加速）：
│   │   ├── 官方 GitHub
│   │   ├── ghproxy.com
│   │   ├── mirror.ghproxy.cn
│   │   └── kkgithub.com
│   ├── 启动时 / 手动 测速所有镜像 → 自动选最快的下载源
│   ├── 检查更新（对比远程版本号 vs 当前版本号）
│   ├── 更新公告（从项目根目录公告.txt 读取 → 显示更新内容 + 版本号 + 发布时间）
│   ├── 一键下载安装更新
│   └── 更新日志查看
│
🔑 管理员（Admin）
│   ├── 修改密码
│   └── 操作日志
```

### 8.1 仪表盘（Dashboard）

**技术方案：** ECharts CDN 或 Chart.js CDN，数据由后端 `/admin/stats/*` 系列接口返回。

**统计维度：**

| 图表 | 数据源 | 类型 | 说明 |
|---|---|---|---|
| 调用总量趋势 | `call_logs` 表 | 折线图 | 近 7 天 / 30 天，按小时聚合 |
| 各接口调用占比 | `call_logs` | 饼图 | resolve / proxy / cms.play / cms.detail ... |
| 解析规则命中率 | `extract_rules` + `call_logs` | 柱状图 | 哪条规则被匹配最多 |
| 采集源入库量 | `sources` + `vods/episodes` | 柱状图 | 每个源站贡献了多少数据 |
| 成功率 / P95 耗时 | `call_logs` | 仪表盘 / 折线图 | 接口健康度 |
| 实时调用流 | WebSocket 或轮询 | 滚动列表 | 最近 20 条调用记录 |

**后端统计接口：**

```
GET /admin/stats/overview          → 今日调用量、缓存命中率、活跃规则数...
GET /admin/stats/trends?range=7d   → 按小时聚合的调用量 / 耗时 / 成功率
GET /admin/stats/rules-top?limit=10 → 解析规则 TOP N
GET /admin/stats/sources-top       → 采集源入库量 TOP
GET /admin/call-logs?page=1&size=20 → 最近调用明细（分页）
```

**日志表（新增一张）：**

```sql
CREATE TABLE call_logs (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    api         VARCHAR(64)  NOT NULL COMMENT '接口标识：resolve / proxy / cms.play / cms.detail ...',
    rule_id     INT          DEFAULT 0 COMMENT '命中的解析规则 ID',
    source_id   INT          DEFAULT 0 COMMENT '来源采集源 ID',
    call_status TINYINT      DEFAULT 0 COMMENT '1=成功 0=失败',
    duration_ms INT          DEFAULT 0 COMMENT '耗时毫秒',
    cache_hit   TINYINT      DEFAULT 0 COMMENT '1=命中缓存',
    client_ip   VARCHAR(64)  DEFAULT '',
    target_url  VARCHAR(512) DEFAULT '',
    error_msg   VARCHAR(512) DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_api_time (api, created_at),
    INDEX idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
  PARTITION BY RANGE (TO_DAYS(created_at)) (
      PARTITION p20260901 VALUES LESS THAN (TO_DAYS('2026-09-10')),
      PARTITION p20260910 VALUES LESS THAN (TO_DAYS('2026-09-20')),
      PARTITION p20260920 VALUES LESS THAN (TO_DAYS('2026-09-30')),
      PARTITION p_future VALUES LESS THAN MAXVALUE
  );
```

> 用分区表自动按月/按天归档，避免日志表无限增长。后台可以配"保留 N 天"定时清理。

### 8.2 前端设置（伪装路径）

**核心需求：** 用户访问播放页时，不想用默认的 `/?url=xxx`，可以自定义成任何路径，达到伪装 / 防扫描 / 个性化的目的。

**配置表（新增）：**

```sql
CREATE TABLE frontend_settings (
    id          TINYINT PRIMARY KEY COMMENT '单行表，id=1',
    play_path   VARCHAR(128) NOT NULL DEFAULT '/' COMMENT '播放页入口路径',
    url_param   VARCHAR(64)  NOT NULL DEFAULT 'url' COMMENT 'URL 参数名',
    alias_params VARCHAR(255) DEFAULT '' COMMENT '别名参数名，逗号分隔：video,src,link',
    skin        VARCHAR(64)  DEFAULT 'default' COMMENT '播放器皮肤/主题',
    player_type VARCHAR(32)  DEFAULT 'dplayer' COMMENT 'dplayer / hls.js / flv.js',
    logo_url    VARCHAR(512) DEFAULT '',
    api_base    VARCHAR(255) DEFAULT '' COMMENT '强制注入的后端 API 地址（空=自动同域）',
    footer_text VARCHAR(255) DEFAULT '',
    beian       VARCHAR(128) DEFAULT '' COMMENT 'ICP 备案号',
    cross_origin TINYINT DEFAULT 1 COMMENT '1=开启 CORS',
    cache_ttl   INT DEFAULT 3600 COMMENT '解析缓存 TTL（秒）',
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**路由设计（后端 Echo）：**

```go
// 默认路径 /
e.GET("/", playerHandler)

// 用户自定义伪装路径（支持多个，从 DB 配置读）
// 例如 play_path = "/mx.php" 时：
//   https://你的域名/mx.php?url=xxx   ← 和默认 / 完全等价
//   还支持别名参数：?video=xxx  ?src=xxx
e.GET(settings.PlayPath, playerHandler)
// 如果 play_path 带扩展名（.php / .html），Echo 会原样匹配，非常利于伪装
```

**前端参数别名读取逻辑：**

```js
// player.js —— 灵活读取目标 URL
const getTargetUrl = () => {
    const params = new URLSearchParams(location.search);
    const aliases = ['url', 'video', 'src', 'link', 'v', 'u'];  // 后端配置下发
    for (const key of aliases) {
        const v = params.get(key);
        if (v && v.startsWith('http')) return decodeURIComponent(v);
    }
    return null;
};
```

**伪装效果举例：**

| play_path 配置 | 访问地址 | 等价于 |
|---|---|---|
| `/` | `https://a.com/?url=xxx` | 默认播放页 |
| `/mx.php` | `https://a.com/mx.php?url=xxx` | 仿苹果 CMS 采集接口路径 |
| `/play.php` | `https://a.com/play.php?src=xxx` | 仿 PHP 动态页 |
| `/embed.html` | `https://a.com/embed.html?video=xxx` | 仿静态嵌入页 |
| `/live.m3u8` | `https://a.com/live.m3u8?link=xxx` | 仿 m3u8 文件（需要后端做 Content-Type 伪装） |

**后端伪装 Content-Type（高级）：**

```go
// 如果 play_path 以 .m3u8 / .mp4 结尾，实际返回的是解析后的视频流
// 前端可以直接 <video src="https://a.com/live.m3u8?link=xxx" />
e.GET("/live.m3u8", func(c echo.Context) error {
    targetURL := extractFromAliasParams(c)
    resolvedURL := resolveService.Resolve(c.Request().Context(), targetURL)
    c.Response().Header().Set("Content-Type", "application/vnd.apple.mpegurl")
    return c.Redirect(http.StatusFound, resolvedURL)
})
```

### 8.3 映射设置（Mapping / 官方七大站）

**核心需求：** 预置腾讯、爱奇艺、优酷、芒果、搜狐、咪咕、B站七大视频站的字段映射规则，用户可以一键启用/修改，新增源站只需填一下 JSONPath 映射即可。

**映射配置表（新增）：**

```sql
CREATE TABLE site_mappings (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    site_code   VARCHAR(32)  NOT NULL UNIQUE COMMENT 'tencent / iqiyi / youku / mgtv / sohu / migu / bilibili / custom',
    site_name   VARCHAR(128) NOT NULL COMMENT '展示名称：腾讯视频、爱奇艺...',
    site_domain VARCHAR(512) NOT NULL COMMENT '主域名正则匹配：(v\\.qq\\.com|v.qq.com)',
    site_icon   VARCHAR(255) DEFAULT '' COMMENT '小图标 URL',

    -- ⭐ 字段映射（JSONPath / Regex / 固定值）
    -- 原始数据来自采集源返回的 JSON 或 HTML
    name_field      VARCHAR(255) NOT NULL COMMENT '剧名提取路径：$.vod_name / $.data.title / regex:xxx',
    alias_field     VARCHAR(255) DEFAULT '',
    cover_field     VARCHAR(255) DEFAULT '',
    year_field      VARCHAR(255) DEFAULT '',
    region_field    VARCHAR(255) DEFAULT '',
    category_field  VARCHAR(255) DEFAULT '',
    remark_field    VARCHAR(255) DEFAULT '',

    -- ⭐ 集数列表提取
    episodes_path   VARCHAR(255) NOT NULL COMMENT '集数数组的 JSONPath：$.vod_play_from[0].vod_play_list[0].urls',
    episode_no_rule VARCHAR(255) DEFAULT '' COMMENT '从单条集数据提取集数的正则/路径',
    episode_url_rule VARCHAR(255) DEFAULT '' COMMENT '从单条集数据提取播放页 URL 的规则',

    -- ⭐ 播放页解析（可独立于 extract_rules 表，也可联动）
    extract_rule_id INT DEFAULT 0 COMMENT '关联 extract_rules 表',

    is_builtin      TINYINT DEFAULT 0 COMMENT '1=官方预置不可删 0=用户自定义',
    enabled         TINYINT DEFAULT 1,
    priority        INT DEFAULT 0,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_domain (site_domain(128))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**七大站预置数据（初始 INSERT）：**

```sql
-- 腾讯视频
INSERT INTO site_mappings (site_code, site_name, site_domain, name_field, episodes_path, extract_rule_id, is_builtin) VALUES
('tencent', '腾讯视频', '(v\\.|video\\.)qq\\.com', '$.vod_name', '$.vod_play_from[0].vod_play_list[0].urls', 1, 1),
('iqiyi',   '爱奇艺',   '(www\\.|pc\\.)iqiyi\\.com', '$.title', '$.data.episodes', 2, 1),
('youku',   '优酷',     '(v\\.|www\\.)youku\\.com', '$.title', '$.episodes', 3, 1),
('mgtv',    '芒果TV',   '(www\\.|h5\\.)mgtv\\.com', '$.vod_name', '$.data.episodes', 4, 1),
('sohu',    '搜狐视频', 'tv\\.sohu\\.com', '$.video_name', '$.episodes', 5, 1),
('migu',    '咪咕视频', 'www\\.miguvideo\\.com', '$.title', '$.episodes', 6, 1),
('bilibili','哔哩哔哩', '(www\\.|m\\.)bilibili\\.com', '$.title', '$.epList', 7, 1);
```

**抓取映射的工作流程：**

```
① 采集源拉到原始数据（JSON 或 HTML 内嵌 JSON）
    │
② 用 resty + 站点 domain 匹配 → 命中 site_code=tencent
    │
③ 读 site_mappings 的 name_field / episodes_path 等字段
    │
④ 用 JSONPath 从原始数据提取：
    ├─ 剧名   → rawItem.name    → 走 matcher 模糊匹配
    ├─ 集数数组 → 遍历每集，用 episode_no_rule / episode_url_rule 提取
    └─ 其他字段 → 封面、年份、分类...
    │
⑤ 走 matcher 集数提取 + 剧名模糊匹配
    │
⑥ 入库 vods / episodes
    │
⑦ 记录 call_logs → 仪表盘图表有数据
```

**管理后台 UI 交互：**

```
[编辑 site_mapping] 页面布局：

┌─ 基础信息 ─────────────────────────────┐
│ 站点名称：腾讯视频  [预置]               │
│ 域名正则：(v\.|video\.)qq\.com          │
└─────────────────────────────────────────┘

┌─ 字段映射（JSONPath / Regex）──────────┐
│ 剧名：    [ $.vod_name              ]  │
│ 别名：    [ $.vod_actor             ]  │
│ 封面：    [ $.vod_pic               ]  │
│ 年份：    [ $.vod_year              ]  │
│ 分类：    [ $.vod_class             ]  │
│ 备注：    [ $.vod_content  截断前50字]  │
└─────────────────────────────────────────┘

┌─ 集数提取 ──────────────────────────────┐
│ 集数数组路径：[ $.vod_play_from[0].vod_play_list[0].urls ] │
│ 单条集数号规则：[ regex:第(\d+)集 ]     │
│ 单条播放页URL规则：[ jsonpath:$.url ]   │
│ 关联解析规则：[ ▼ rule_id=3 qq_video ] │
└─────────────────────────────────────────┘

┌─ 🧪 一键测试 ──────────────────────────┐
│ 粘贴源站 URL / JSON 原始内容：          │
│ ┌─────────────────────────────────────┐ │
│ │ { "vod_name": "庆余年", ... }       │ │
│ └─────────────────────────────────────┘ │
│ [ 解析 → ]                              │
│ ┌─ 结果 ─────────────────────────────┐  │
│ │ ✓ 剧名：庆余年                      │  │
│ │ ✓ 集数 36 条已提取                  │  │
│ │   [ {"no":1,"url":"..."}, ... ]     │  │
│ └─────────────────────────────────────┘  │
└─────────────────────────────────────────┘
```

**后端测试接口：**

```
POST /admin/mapping/test
    body: { site_code: "tencent", raw_data: "{...}" }
    → 返回 { name, episodes_count, episodes: [...] }

GET  /admin/mappings          → 列出所有站点映射（含七大站预置）
GET  /admin/mappings/:code    → 查看/编辑单个站点
PUT  /admin/mappings/:code    → 修改（builtin 只能改 enabled / priority）
POST /admin/mappings          → 新增自定义映射
```

### 8.4 分析设置（Analysis Engine）

**核心需求：** 当用户输入一个 URL 时，系统自动分析它是什么类型的资源，选择对应的处理策略。

**分析分类：**

| 输入类型 | 判断依据 | 后续走的流程 |
|---|---|---|
| **官方资源** | URL 域名匹配 site_mappings 预置的七大站域名正则 | 提取剧名 + 集数 + 播放页 URL → 入库 → 匹配模式 |
| **可播放资源** | URL 后缀是 `.m3u8` / `.mp4` / `.flv` 且可直接返回视频流 | 跳过剧名匹配 → 走去插播配置 → 直接播放 |
| **未知类型** | 不匹配以上任何一种 | 返回 404 + 提示用户手动配置解析规则 |

**分析引擎流程：**

```
用户输入 URL →
    │
    ├─ ① 提取域名（net.Parse → hostname）
    │
    ├─ ② 匹配 site_mappings.site_domain 正则
    │      ├─ 命中 → type = "official", site_code = "tencent" ...
    │      └─ 未命中 ↓
    │
    ├─ ③ 判断 URL 后缀 / Content-Type（HEAD 请求探测）
    │      ├─ .m3u8 / application/vnd.apple.mpegurl → type = "direct"
    │      ├─ .mp4 / video/mp4                     → type = "direct"
    │      └─ .flv / video/x-flv                   → type = "direct"
    │
    └─ ④ 都不匹配 → type = "unknown"
```

**配置表（新增）：**

```sql
CREATE TABLE analysis_settings (
    id              TINYINT PRIMARY KEY COMMENT '单行表，id=1',
    enabled         TINYINT DEFAULT 1 COMMENT '分析引擎总开关',
    priority        VARCHAR(32) DEFAULT 'official_first' COMMENT 'official_first / direct_first / ai_first',
    ai_enabled      TINYINT DEFAULT 0 COMMENT '是否启用 AI 辅助分析',
    ai_provider     VARCHAR(32) DEFAULT '' COMMENT 'openai / doubao / custom',
    ai_api_key      VARCHAR(255) DEFAULT '',
    ai_endpoint     VARCHAR(512) DEFAULT '',
    unknown_mode    VARCHAR(32) DEFAULT 'reject' COMMENT 'reject 返回404 / direct 当直链处理 / rule 走解析规则',
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**后端接口：**

```
POST /admin/analysis/test
    body: { url: "https://v.qq.com/x/cover/abc123/e00123.html" }
    → 返回 { type: "official", site_code: "tencent", site_name: "腾讯视频", matched: true }

GET  /admin/analysis/settings    → 读取分析引擎配置
PUT  /admin/analysis/settings    → 修改配置
```

**内置七大站域名正则：**

```go
var OfficialSites = []struct{ code, name, pattern string }{
    {"tencent",   "腾讯视频",   `(v\.|video\.)qq\.com`},
    {"iqiyi",     "爱奇艺",     `(www\.|pc\.)iqiyi\.com`},
    {"youku",     "优酷",       `(v\.|www\.)youku\.com`},
    {"mgtv",      "芒果TV",     `(www\.|h5\.)mgtv\.com`},
    {"sohu",      "搜狐视频",   `tv\.sohu\.com`},
    {"migu",      "咪咕视频",   `www\.miguvideo\.com`},
    {"bilibili",  "哔哩哔哩",   `(www\.|m\.)bilibili\.com`},
}
```

---

### 8.5 AI 设置（AI Analysis：ts / 广告 / 字幕 智能识别）

**核心需求：** 分析 m3u8 视频流中的广告、字幕、插播，自动识别并剔除。很多广告 / 字幕 / 插播是**直接烧录在 ts 分片画面里**的（不是单独的 ad 接口），所以必须对 ts 分片**内容本身**做分析，靠 **MD5 指纹特征库 + AI 画面识别** 双通道判定。

**为什么需要 MD5 指纹：**
- 同一个广告片头 / 插播片段，会在**不同剧集、不同视频**里反复出现
- 这些 ts 分片的二进制内容完全一致 → **MD5 完全一致**
- 首次发现存入 `ad_fingerprints` 特征库，之后任何 m3u8 只要命中 MD5 → **秒级判定为广告**，无需再跑 AI（省成本、速度快）

**检测策略（双通道）：**

```
输入 m3u8 URL
    │
    ▼
① 解析 m3u8 → 得到全部 ts 分片列表（序号 / 时长 / 大小 / URL）
    │
    ▼
② MD5 快速命中（全部 ts 并发下载，流式计算 MD5）
    │   ├─ 命中 ad_fingerprints(md5)          → 判定 ad         （置信度 0.99）
    │   ├─ 命中 subtitle_fingerprints(md5)    → 判定 subtitle   （置信度 0.95）
    │   ├─ 命中 interlude_fingerprints(md5)   → 判定 interlude  （置信度 0.95）
    │   └─ 未命中 ↓
    │
    ▼
③ AI 画面抽样分析（只抽 analyze_ratio，默认 30%）
    │   对 ts 解码抽帧 → 送多模态 AI 判断画面类型
    │   ├─ 广告帧（品牌 logo / 促销文案 / 台标突变 / 黑场+大字幕）
    │   ├─ 字幕水印（底部硬字幕 / 台标 / 角标）
    │   ├─ 插播（内容断裂点 / 画面风格突变）
    │   └─ 正常内容
    │   判定结果 → 回写 ad_fingerprints 特征库（扩散命中）
    │
    ▼
④ 特征启发式辅助（可选，不依赖 AI）
    │   ├─ 时长异常短的 ts（常为拼接广告）
    │   ├─ 相邻 ts 画面哈希突变
    │   └─ 音频响度突变 / 静音段
    │
    ▼
⑤ 汇总生成分析报告
    │   ├─ 广告 ts 列表（一键剔除 → 动态生成干净 m3u8）
    │   ├─ 字幕 / 水印 ts 列表
    │   ├─ 插播位置
    │   └─ 置信度明细 + 判定原因
    │
    ▼
⑥ 写库 ts_analysis_logs + 更新 ad_fingerprints
```

**配置表（新增）：**

```sql
-- AI 服务配置（单行表）
CREATE TABLE ai_settings (
    id              TINYINT PRIMARY KEY COMMENT '单行表，id=1',
    enabled         TINYINT DEFAULT 0 COMMENT 'AI 分析总开关',
    provider        VARCHAR(32) DEFAULT '' COMMENT 'openai / doubao / qwen / custom',
    api_key         VARCHAR(255) DEFAULT '',
    endpoint        VARCHAR(512) DEFAULT '',
    model           VARCHAR(64)  DEFAULT '' COMMENT '多模态模型名',
    analyze_ratio   DECIMAL(3,2) DEFAULT 0.30 COMMENT 'AI 抽样分析比例（0-1）',
    md5_enabled     TINYINT DEFAULT 1 COMMENT '是否启用 MD5 指纹快速命中',
    auto_skip_ad    TINYINT DEFAULT 0 COMMENT '检测到广告 ts 是否自动剔除并生成干净 m3u8',
    concurrency     INT DEFAULT 8 COMMENT 'ts 并发下载数',
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ts 分片分析记录
CREATE TABLE ts_analysis_logs (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    task_id         VARCHAR(64)  NOT NULL COMMENT '一次分析的批次 id',
    m3u8_url        VARCHAR(1024) NOT NULL COMMENT '被分析的 m3u8',
    ts_index        INT NOT NULL COMMENT '分片序号（从0开始）',
    ts_url          VARCHAR(1024) NOT NULL,
    duration_sec    DECIMAL(8,3) DEFAULT 0,
    size_bytes      INT DEFAULT 0,
    md5             CHAR(32) DEFAULT '',
    ai_verdict      VARCHAR(16) DEFAULT 'normal' COMMENT 'normal / ad / subtitle / interlude / unknown',
    confidence      DECIMAL(4,2) DEFAULT 0 COMMENT '判定置信度 0-1',
    reason          VARCHAR(512) DEFAULT '' COMMENT '判定原因（md5_hit / ai_frame / heuristic）',
    analyzed_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_task (task_id),
    INDEX idx_m3u8 (m3u8_url(255)),
    INDEX idx_md5 (md5),
    INDEX idx_ai_verdict (ai_verdict)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 广告 / 字幕 / 插播 MD5 指纹特征库
CREATE TABLE ad_fingerprints (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    fingerprint_type VARCHAR(16) NOT NULL COMMENT 'ad / subtitle / interlude / watermark',
    md5             CHAR(32) NOT NULL UNIQUE,
    source_m3u8     VARCHAR(1024) DEFAULT '' COMMENT '首次发现来源',
    hit_count       INT DEFAULT 1 COMMENT '命中次数，越高越可信',
    last_hit_at     DATETIME DEFAULT NULL,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_type (fingerprint_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**内置播放器（去插播验证 + 实时标注）：**

```
管理后台内嵌播放器（复用 DPlayer + hls.js）
    │
    ├─ 播放原始 m3u8        → 前端 ?url=原始
    ├─ 播放去广告 m3u8      → /admin/ai/result/m3u8?task_id=xxx（auto_skip_ad 动态生成）
    └─ 预览单个 ts 片段     → /admin/ai/ts/:id/play

实时标注（关键交互）：
    监听 hls.js FRAG_LOADED 事件 → 拿到当前分片 SN
    → 映射到 ts_analysis_logs 的判定结果
    → 播放器下方显示：第 N 个 ts | MD5:xxxx | ⚠️ 广告 / 💬 字幕 / ✅ 正常
    → 广告片段红色进度条标记，可点击跳转预览
```

**后端接口：**

```
GET  /admin/ai/settings              → 读取 AI 配置
PUT  /admin/ai/settings              → 修改配置

POST /admin/ai/analyze               → 分析一个 m3u8  body: { url: "xxx" } → 返回 task_id
GET  /admin/ai/analyze/:task_id      → 查询分析进度 / 汇总结果（轮询）
GET  /admin/ai/ts                    → 某次分析的 ts 列表 ?task_id=&verdict=ad&page=
GET  /admin/ai/ts/:id/play           → 播放单个 ts（内置播放器预览）
GET  /admin/ai/result/m3u8           → 去广告后的干净 m3u8 ?task_id=
GET  /admin/ai/fingerprints          → 指纹库列表（按 hit_count 排序，可分类型筛选）
DELETE /admin/ai/fingerprints/:id    → 删除指纹
POST /admin/ai/fingerprints/import   → 批量导入已有 md5 特征（文本每行一个）
POST /admin/ai/fingerprints/export   → 导出特征库（备份 / 分享）
GET  /admin/ai/stream                → 实时推送分析日志（SSE 或 WebSocket）
```

**MD5 流式计算（Go 实现，边下边算不全量缓存）：**

```go
// internal/ai/md5.go
func ComputeMD5FromURL(url string, maxBytes int64) (string, error) {
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("User-Agent", randomUA())
    req.Header.Set("Referer", deriveReferer(url))
    resp, err := httpClient.Do(req)   // 支持 Range: bytes=0-maxBytes 只取头部即可近似
    if err != nil { return "", err }
    defer resp.Body.Close()

    h := md5.New()
    io.Copy(h, io.LimitReader(resp.Body, maxBytes))
    return hex.EncodeToString(h.Sum(nil)), nil
}

// 指纹比对：O(1) 命中
func MatchFingerprint(db *gorm.DB, md5 string) *AdFingerprint {
    var fp AdFingerprint
    if err := db.Where("md5 = ?", md5).First(&fp).Error; err != nil {
        return nil
    }
    db.Model(&fp).UpdateColumn("hit_count", gorm.Expr("hit_count + 1"))
    return &fp
}
```

**去广告 m3u8 动态生成：**

```
输入：原始 m3u8 + ts_analysis_logs（ad 判定列表）
输出：干净 m3u8（剔除广告 ts，保留 #EXTINF 时长信息）

#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.000,
https://xxx/seg_000.ts      ← 正常
#EXTINF:10.000,
https://xxx/seg_002.ts      ← 正常（广告 seg_001 已剔除）
...
#EXT-X-ENDLIST
```

**判定聚合（verdict.go）：**

```go
type Verdict string

const (
    VerdictNormal    Verdict = "normal"
    VerdictAd        Verdict = "ad"
    VerdictSubtitle  Verdict = "subtitle"
    VerdictInterlude Verdict = "interlude"
    VerdictUnknown   Verdict = "unknown"
)

// 多通道结果合并，取置信度最高的判定
func Merge(verdicts []Candidate) Verdict {
    best := VerdictUnknown
    bestConf := 0.0
    for _, v := range verdicts {
        if v.Confidence > bestConf {
            best, bestConf = v.Verdict, v.Confidence
        }
    }
    return best
}
```

---

### 8.6 匹配设置（Matching Strategy）

**核心需求：** 分析引擎判断出资源类型后，选择匹配策略将资源和内部数据关联。

**两种匹配模式：**

```
模式 A：官方链接 → 走「匹配模式」
───────────────────────────────
  输入：官方站播放页 URL
     │
     ├─ resty 请求官方 API / HTML
     ├─ 用 site_mappings 字段映射提取 剧名 + 集数
     ├─ matcher 模糊匹配内部 vods 表
     ├─ 找不到同名 → 新建 vod + episode
     └─ 关联成功 → 继续走调用设置

模式 B：直接播放资源 → 走「去插播配置」
─────────────────────────────────────────
  输入：m3u8 / mp4 直链
     │
     ├─ 跳过剧名匹配
     ├─ 直接进入调用设置链路（去插播接口 → 去广告接口）
     ├─ 可选：存为临时条目（不入库 vods）
     └─ 返回处理后的直链
```

**匹配优先级链：**

```
1. AI 自动识别（如果 analysis_settings.ai_enabled = 1）
   → 把 URL + 页面内容发给 AI，让 AI 判断剧名和集数
2. 指定规则匹配（默认）
   → site_mappings 字段映射 + matcher 模糊匹配
3. 去插播兜底
   → 以上都匹配不上时，当直链处理
```

**配置表（新增）：**

```sql
CREATE TABLE matching_settings (
    id              TINYINT PRIMARY KEY COMMENT '单行表，id=1',
    mode            VARCHAR(32) DEFAULT 'rule' COMMENT 'rule / ai / hybrid',
    fallback        VARCHAR(32) DEFAULT 'direct' COMMENT '匹配失败时：direct 当直链处理 / reject 拒绝',
    fuzzy_threshold INT DEFAULT 85 COMMENT '模糊匹配相似度阈值（0-100）',
    auto_create     TINYINT DEFAULT 1 COMMENT '匹配不到时是否自动新建 vods 条目',
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

### 8.7 调用设置（Chaining / Pipeline）

**核心需求：** 匹配成功后，资源经过多层接口串联处理（去插播 → 去广告 → proxy 等），每个环节可独立启停和调整顺序。

**链路模型：**

```
资源 URL 进入
    │
    ▼
┌─────────────────────────────────────────────┐
│  Pipeline（按顺序依次执行，每个节点可跳过）   │
│                                             │
│  [1] 去插播接口 ──┐                         │
│  [2] 去广告接口 ──┤──→ 结果传给下一层        │
│  [3] proxy 转发 ──┤                         │
│  [4] 自定义接口 ──┘                         │
│                                             │
│  出错回退：某层失败 → skip / abort / fallback│
└─────────────────────────────────────────────┘
    │
    ▼
最终输出（处理后的视频流）
```

**配置表（新增）：**

```sql
CREATE TABLE chain_nodes (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL COMMENT '节点名称：去插播 / 去广告 / proxy / 自定义',
    node_type       VARCHAR(32)  NOT NULL COMMENT 'skip_ad / block_ad / proxy / custom',
    order_no        INT NOT NULL COMMENT '执行顺序，越小越先执行',
    endpoint_url    VARCHAR(512) DEFAULT '' COMMENT '接口地址，支持 {input_url} 占位符',
    method          VARCHAR(8)   DEFAULT 'GET',
    headers         JSON,
    params          JSON,
    body_template   TEXT COMMENT '请求体模板（JSON 字符串，{input_url} 占位）',
    result_path     VARCHAR(255) DEFAULT '' COMMENT '从接口返回中提取结果的 JSONPath',
    fallback        VARCHAR(16)  DEFAULT 'skip' COMMENT '失败时：skip 跳过继续 / abort 终止返回错误 / fallback 返回原始',
    enabled         TINYINT DEFAULT 1,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_order (order_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**内置预置节点（初始数据）：**

```sql
INSERT INTO chain_nodes (name, node_type, order_no, endpoint_url, result_path, fallback, enabled) VALUES
('去插播接口', 'skip_ad',    1, 'https://example.com/api?url={input_url}', '$.data.url', 'skip', 1),
('去广告接口', 'block_ad',   2, '', '', 'skip', 0),
('proxy 转发', 'proxy',      3, '/api/proxy/stream', '', 'skip', 1);
```

**后端接口：**

```
GET  /admin/chain/nodes          → 列出所有链路节点（按 order_no 排序）
POST /admin/chain/nodes          → 新增节点
PUT  /admin/chain/nodes/:id      → 修改节点
DELETE /admin/chain/nodes/:id    → 删除节点
POST /admin/chain/reorder        → 调整顺序 body: { ids: [3, 1, 2] }
POST /admin/chain/test           → 测试整条链路 body: { input_url: "xxx" } → 返回每一层的中间结果
```

---

### 8.8 更新设置（Updater）

**核心需求：** 支持从 GitHub 仓库自动下载最新版本，内置多条镜像解决国内访问问题，自动测速选最快源，显示更新公告。

**镜像列表（内置多条）：**

```go
var BuiltinMirrors = []struct{ name, baseURL string }{
    {"官方 GitHub",     "https://github.com"},
    {"ghproxy.com",     "https://ghproxy.com"},
    {"gh-proxy.com",    "https://gh-proxy.com"},
    {"mirror.ghproxy.cn","https://mirror.ghproxy.cn"},
    {"kkgithub.com",    "https://kkgithub.com"},
    {"hub.fastgit.xyz", "https://hub.fastgit.xyz"},
}
```

**更新流程：**

```
用户点击「检查更新」或启动时自动检查
    │
    ├─ ① 并发测速所有内置镜像（HEAD 请求 + 计算耗时）
    │      └─ 选最快镜像作为本次更新源
    │
    ├─ ② 获取远程版本号
    │      ├─ GitHub: api.github.com/repos/ssmhdssmhd/MXGT/releases/latest
    │      └─ 镜像: 镜像前缀 + /ssmhdssmhd/MXGT/releases/latest
    │      └─ 解析 tag_name → remote_version
    │
    ├─ ③ 对比 remote_version vs 当前版本号（从 config.yaml / VERSION 文件读取）
    │      ├─ remote > current → 有更新
    │      └─ remote <= current → 已是最新
    │
    ├─ ④ 获取更新公告
    │      └─ 从项目根目录 公告.txt 读取（GitHub raw 或镜像 raw）
    │         公告.txt 格式：
    │         ┌──────────────────┐
    │         │ 版本：v0.0.4     │
    │         │ 日期：2026-09-04 │
    │         │ ─────────────── │
    │         │ 更新内容：      │
    │         │ 1. 新增分析设置 │
    │         │ 2. 新增匹配设置 │
    │         │ 3. 新增更新设置 │
    │         │ ─────────────── │
    │         │ 下载地址：...    │
    │         └──────────────────┘
    │
    ├─ ⑤ 用户点击「一键更新」
    │      ├─ 下载 zip 包（走最快镜像）
    │      ├─ 解压到临时目录
    │      ├─ 备份当前版本到 backup/
    │      ├─ 覆盖更新（保留 config.yaml）
    │      └─ 提示重启
    │
    └─ ⑥ 更新完成 → 记录到 update_logs
```

**配置表（新增）：**

```sql
CREATE TABLE updater_config (
    id              TINYINT PRIMARY KEY COMMENT '单行表，id=1',
    repo_url        VARCHAR(255) NOT NULL DEFAULT 'https://github.com/ssmhdssmhd/MXGT',
    auto_check      TINYINT DEFAULT 1 COMMENT '启动时是否自动检查',
    last_check_at   DATETIME DEFAULT NULL,
    last_version    VARCHAR(32) DEFAULT '',
    fastest_mirror  VARCHAR(64) DEFAULT '' COMMENT '上次测速选出的最快镜像名',
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE update_logs (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    from_version    VARCHAR(32) NOT NULL,
    to_version      VARCHAR(32) NOT NULL,
    mirror_used     VARCHAR(64) DEFAULT '',
    status          VARCHAR(16) DEFAULT 'success' COMMENT 'success / failed / partial',
    notice          TEXT COMMENT '更新公告内容',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**后端接口：**

```
GET  /admin/update/config          → 读取更新配置
PUT  /admin/update/config          → 修改配置

POST /admin/update/mirror-speed    → 手动触发所有镜像测速
    → 返回 [{ name: "官方 GitHub", latency_ms: 1200, reachable: false },
            { name: "ghproxy.com", latency_ms: 320, reachable: true }]

POST /admin/update/check           → 检查更新（对比远程版本 vs 当前）
    → 返回 { current: "v0.0.3", remote: "v0.0.4", has_update: true,
             notice: "公告.txt 内容", download_url: "...", mirror_used: "ghproxy.com" }

POST /admin/update/download        → 下载并安装更新
    → 返回 { success: true, backup_path: "backup/MXGT-v0.0.3-20260904/",
             extracted_to: "tmp/update/", next_step: "请手动重启服务" }

GET  /admin/update/logs            → 更新日志（分页）
```

**测速算法（Go 实现）：**

```go
// internal/updater/speedtest.go
func BenchmarkMirrors(mirrors []Mirror) (*Mirror, []MirrorResult) {
    results := make([]MirrorResult, len(mirrors))
    var mu sync.Mutex
    var wg sync.WaitGroup

    for i, m := range mirrors {
        wg.Add(1)
        go func(idx int, mirror Mirror) {
            defer wg.Done()
            start := time.Now()
            req, _ := http.NewRequest("HEAD", mirror.baseURL, nil)
            client := &http.Client{Timeout: 5 * time.Second}
            resp, err := client.Do(req)
            latency := time.Since(start)
            if err != nil {
                results[idx] = MirrorResult{mirror, 0, false, err.Error()}
                return
            }
            resp.Body.Close()
            mu.Lock()
            results[idx] = MirrorResult{mirror, latency.Milliseconds(), true, ""}
            mu.Unlock()
        }(i, m)
    }
    wg.Wait()

    // 选最快可达的
    var fastest *Mirror
    bestLatency := int64(1<<62)
    for _, r := range results {
        if r.reachable && r.latency_ms < bestLatency {
            bestLatency = r.latency_ms
            m := r.mirror
            fastest = &m
        }
    }
    return fastest, results
}
```

**公告.txt 格式规范（项目根目录）：**

```
==============================
MXGT 更新公告
==============================
版本：v0.0.4
日期：2026-09-04
作者：MXGT Team
------------------------------
【本次更新内容】

✨ 新增功能
  • 🧠 分析设置：自动识别官方资源 / 可播放资源
  • 🎯 匹配设置：AI + 指定规则 双通道匹配
  • 🔌 调用设置：多层接口串联（去插播 + 去广告）
  • 🗺️ 映射设置：七大站字段映射可配置
  • 🔄 更新设置：内置多镜像自动测速 + 一键更新

🐛 修复
  • 修复 proxy 接口在某些情况下无法正确转发 Referer 的问题
  • 修复 Redis 缓存 key 未做 hash 导致超长的问题

💡 优化
  • 仪表盘加载速度提升 30%
  • 解析规则匹配优先级更合理

------------------------------
下载地址：见 Release 页面
MD5：a1b2c3d4e5f6...
SHA256：...
==============================
```

---

### 8.9 侧边栏菜单树 → 路由权限（后端）

```go
// pkg/router/menu.go
type MenuItem struct {
    Key       string    // 唯一标识：dashboard / frontend / analysis / matching ...
    Label     string    // 显示名称
    Icon      string    // 图标名
    Path      string    // 前端路由路径
    Order     int
    Children  []MenuItem
}

var DefaultMenus = []MenuItem{
    {Key: "dashboard", Label: "仪表盘",     Icon: "chart",     Path: "/admin/dashboard",  Order: 1},
    {Key: "frontend",  Label: "前端设置",   Icon: "palette",   Path: "/admin/frontend",   Order: 2},
    {Key: "analysis",  Label: "分析设置",   Icon: "brain",     Path: "/admin/analysis",   Order: 3},
    {Key: "ai",        Label: "AI 设置",    Icon: "robot",     Path: "/admin/ai",         Order: 4},
    {Key: "matching",  Label: "匹配设置",   Icon: "crosshair", Path: "/admin/matching",   Order: 5},
    {Key: "chaining",  Label: "调用设置",   Icon: "link",      Path: "/admin/chaining",   Order: 6},
    {Key: "mapping",   Label: "映射设置",   Icon: "map",       Path: "/admin/mapping",    Order: 7},
    {Key: "rules",     Label: "解析规则",   Icon: "code",      Path: "/admin/rules",      Order: 8},
    {Key: "vods",      Label: "影片管理",   Icon: "film",      Path: "/admin/vods",       Order: 9},
    {Key: "updater",   Label: "更新设置",   Icon: "refresh",   Path: "/admin/updater",    Order: 10},
    {Key: "admin",     Label: "管理员",     Icon: "user",      Path: "/admin/account",    Order: 99},
}
```

---

## 九、关键技术点

| 难点 | 解决方案 |
|---|---|
| 前端 URL 硬编码 | `MXGT_API_BASE` 从 query / localStorage / 默认同域 / 后端模板注入四层兜底 |
| 前端 → 后端跨域 | Echo CORS 中间件，`AllowOrigins: *` 或白名单 |
| 视频源防盗链跨域 | extractor 返回直链（hls.js 跨域） 或 proxy 兜底（后端带 Referer 转发） |
| 剧名模糊匹配 | Levenshtein 距离 + 别名表 + 去年份去标点 |
| JSONPath 提取 | `github.com/PaesslerAG/jsonpath` |
| 集数正则 | `第\s*(\d+)\s*[集话期]` / `EP(\d+)` / `^(\d+)$` |
| 并发控制 | `errgroup` + 信号量限流 |
| HTTP 客户端 | resty：超时、gzip/brotli、随机 UA、失败重试、自动注入 Referer |
| 苹果 CMS 格式 | 严格对齐 v10 JSON schema |
| 解析路由匹配 | `url_pattern` 正则按 priority 排序依次尝试 |
| 解析缓存 | Redis，key = `md5(url) + rule_id`，TTL 可配置 |
| proxy token | JWT short-lived（5min），包含目标 URL + 需要注入的 Header |
| m3u8 拖动进度条 | proxy 必须支持 HTTP Range 请求（206 Partial Content） |
| 资源类型自动分析 | 域名正则匹配 + HEAD 请求探测 Content-Type 双重判断，并行请求不阻塞 |
| 多接口串联 Pipeline | 链式执行每个节点，支持 skip / abort / fallback 三种回退策略，中间结果 JSONPath 提取 |
| 国内用户 GitHub 加速 | 内置 6+ 镜像，启动时并发测速（HEAD + 5s timeout），选最快源，失败自动切换下一个 |
| 版本号对比 | 语义化版本比较器（semver.Parse），处理 v0.0.99 → v0.1.0 的跳跃 |
| 更新公告解析 | 项目根目录公告.txt 纯文本，用正则提取版本/日期/内容/下载地址，兼容 GBK/UTF-8 |
| 更新安全 | 下载后 SHA256 校验 + 备份旧版本到 backup/ + 保留 config.yaml 不被覆盖 |
| 分析引擎可配置 | analysis_settings 单行表，优先级顺序（official_first / direct_first / ai_first）运行时生效 |
| AI 辅助分析 | 可选，通过 analysis_settings.ai_provider 配置，把 URL + 页面内容压缩后发给 AI |
| 视频内嵌广告识别 | 广告烧录在 ts 画面里 → 靠 MD5 指纹库（跨视频重复广告秒级命中）+ AI 抽帧画面判定双通道 |
| MD5 指纹快速命中 | 全 ts 并发下载 + 流式计算 MD5（LimitReader 只取头部近似），ad_fingerprints 表 O(1) 命中 |
| AI 抽帧分析 | 对抽样 ts 解码抽帧（ffmpeg 可选 / 纯 Go mpegts 解析）→ 多模态模型判断广告/字幕/插播 |
| 内置播放器实时标注 | 复用 DPlayer + hls.js，监听 FRAG_LOADED 拿当前 SN → 映射判定结果实时标红 |
| 去广告 m3u8 生成 | 剔除广告 ts 后按 #EXTINF 重新拼 #EXTM3U，保留时长信息 |
| 判定聚合 | 多通道结果按置信度取最高，verdict.go 统一 normal / ad / subtitle / interlude / unknown |
| GitHub Actions 云端编译 | push tag 触发 build-release.yml，matrix 多平台编译（linux/windows/darwin × amd64/arm64），CGO_ENABLED=0 静态编译 + -trimpath + -ldflags "-s -w"，自动发布 Release |
| go:embed 单文件 | 编译期把 web/player + web/admin 打进可执行文件，单文件即完整程序，无需额外 web 目录 |
| 首次运行自建环境 | 检测 config.yaml 不存在则自动生成，创建 data/ cache/ logs/ uploads/ tmp/ 运行目录结构 |
| SQLite 零配置 | 默认内嵌 SQLite（glebarez/sqlite 纯 Go 无 CGO），config.yaml 配置了 MySQL/Redis 才自动切换 |
| 替换即更新 | 更新只替换可执行文件，config.yaml / data/ / uploads/ 全在运行目录不受影响，重启即完成升级 |
| 运行目录隔离 | 程序只使用运行文件夹内相对路径，不写系统其它位置，备份=复制文件夹，卸载=删除文件夹 |

---

## 十、第三方依赖清单

```
github.com/labstack/echo/v4            # Web 框架
github.com/labstack/echo-contrib       # echo-session / basicauth 等
gorm.io/gorm                            # ORM
gorm.io/driver/mysql                    # MySQL 驱动
gorm.io/driver/sqlite                   # SQLite 驱动（免配置默认存储，零数据库零 CGO）
github.com/glebarez/sqlite              # 纯 Go SQLite 实现（无 CGO，支持静态编译）
github.com/spf13/viper                  # 配置管理
github.com/golang-jwt/jwt/v5            # JWT
go.uber.org/zap                         # 日志
github.com/PaesslerAG/jsonpath          # JSONPath
github.com/go-resty/resty/v2            # HTTP Client
github.com/PuerkitoBio/goquery          # HTML 解析（可选）
github.com/swaggo/swag                  # API 文档（可选）
github.com/joho/godotenv                # .env
golang.org/x/sync/errgroup              # 并发
github.com/xrash/smetrics               # 字符串相似度
github.com/go-redis/redis/v9            # Redis 缓存
github.com/golang-jwt/jwt/v5            # proxy token 签名
github.com/Masterminds/semver/v3        # 版本号比较（更新设置）
golang.org/x/sync/errgroup              # 并发测速 / 并发分析
github.com/natefinch/lumberjack         # 日志 rotate（更新日志归档）
github.com/sashabaranov/go-openai       # 🤖 AI 多模态分析客户端（openai 兼容协议）
github.com/asticode/go-astits           # 🤖 纯 Go mpegts（ts 分片解复用，可选）
ffmpeg / ffprobe (系统命令，可选)      # 🤖 ts 解码抽帧（未安装时降级为纯 Go 解析）
github.com/gorilla/websocket            # SSE / WebSocket 实时推送分析日志
```

---

## 十一、开发顺序（里程碑）

```
M1 搭架子
  └─ go.mod + 目录骨架 + Echo + MySQL + Redis 连接
     + 配置加载 + 路由注册 + CORS 中间件 + 健康检查
     + 前端 web/player/index.html 基础版（同域 API）

M2 数据层
  └─ 15 张核心表 gorm model（vods / episodes / sources / extract_rules
     + call_logs / frontend_settings / site_mappings
     + analysis_settings / matching_settings / chain_nodes
     + updater_config / update_logs
     + ai_settings / ts_analysis_logs / ad_fingerprints）
     + 自动迁移 + Repository CRUD

M3 前端在线播放页 MVP
  └─ 单文件 index.html + hls.js + DPlayer
     + ?url=xxx 参数解析
     + API_BASE 四层兜底（重点是不硬编码）
     + 调 /api/resolve 播放 m3u8
     + 输出格式切换（JSON 接口 vs URL 网页播放器）

M4 中间层解析路由 MVP
  └─ /api/resolve + extract_rules 表
     + route 匹配 + jsonpath extractor + regex extractor
     + Redis 缓存
     + Echo CORS 中间件开启

M5 Proxy 接口
  └─ /api/proxy/stream + token 签发校验
     + Range 请求支持（拖动进度条）
     + Referer / UA 自动注入
     + extract_rules.need_proxy 字段联动

M6 采集器 MVP
  └─ Collector 接口 + source_api + source_html + Factory
     + matcher 模糊匹配 + 集数提取 + 入库

M7 苹果 CMS 适配
  └─ cms_v10 结构体 + 对外 4 个 ac 接口
     + play 接口走解析路由返回真实 URL

M8 管理后台 — 前端设置 & 视频抓取映射
  └─ frontend_settings 单行表 CRUD → 后端路由动态注册伪装路径（/mx.php /play.php）
     + site_mappings 七大站预置数据 INSERT
     + 字段映射 JSONPath 测试接口 /admin/mapping/test
     + 采集源 + 映射表联动：自动按域名匹配 site_mappings
     + 侧边栏菜单树配置

M9 管理后台 — 仪表盘
  └─ call_logs 表分区存储 + 自动清理
     + /admin/stats/overview / trends / rules-top / sources-top 接口
     + ECharts CDN 仪表盘前端（调用量趋势 / 接口占比饼图 / 规则命中率柱状图）
     + 实时调用流（WebSocket 或轮询滚动）

M10 部署
  └─ Dockerfile + docker-compose + GitHub Actions + README 更新

M11 分析引擎
  └─ internal/analyzer + OfficialSites 七大站域名正则
     + net.Parse + HEAD 请求探测 Content-Type
     + 三种分析结果：official / direct / unknown
     + analysis_settings 单行表 CRUD + 运行时热加载
     + /admin/analysis/test 接口（输入 URL → 返回分析结果）

M12 匹配策略
  └─ internal/matcher 增强：AI 自动识别 + 指定规则匹配双通道
     + matching_settings 配置（mode / fallback / fuzzy_threshold / auto_create）
     + 两种匹配模式：官方链接 → 匹配模式 / 直接资源 → 去插播配置
     + AI 可选接入（openai / doubao / custom）

M13 调用 Pipeline
  └─ internal/chaining Pipeline 引擎
     + chain_nodes 表 CRUD + 顺序调整 + 独立启停
     + 三种回退策略：skip / abort / fallback
     + 中间结果 JSONPath 提取（{input_url} 占位符替换）
     + 内置去插播 / 去广告 / proxy 预置节点
     + /admin/chain/test 接口（测试整条链路中间结果）

M14 更新设置
  └─ internal/updater
     + BuiltinMirrors 6+ 内置镜像（官方 GitHub + ghproxy + gh-proxy + mirror.ghproxy.cn + kkgithub + hub.fastgit.xyz）
     + 并发测速 BenchmarkMirrors（HEAD + 5s timeout → 选最快）
     + semver.Parse 版本号对比（v0.0.99 → v0.1.0 正确处理）
     + 公告.txt 解析（正则提取版本/日期/内容/下载地址）
     + 一键更新：下载 → 校验 → 备份 → 解压 → 覆盖（保留 config.yaml）
     + updater_config + update_logs 表
     + /admin/update/check / mirror-speed / download / logs 接口

M15 整体联调 + 侧边栏前端
  └─ 分析 → 匹配 → 调用 → 解析 全链路打通
     + 侧边栏前端 11 个模块全部可访问
     + 侧边栏菜单树权限联动（DefaultMenus）
     + 前端设置输出格式切换（JSON vs 网页播放器）
     + 最终端到端测试：官方站播放页 URL → 自动识别 → 匹配 → 解析 → 播放

M16 AI 视频智能分析（ts / 广告 / 字幕 / 插播）
  └─ internal/ai 包（ai / m3u8 / md5 / tsanalyzer / frame / subtitle / clean / verdict）
     + ai_settings 单行表 + ts_analysis_logs + ad_fingerprints MD5 指纹库
     + m3u8 解析 → 全量 ts 分片列表（并发下载 + MD5 流式计算）
     + MD5 指纹快速命中（ad / subtitle / interlude / watermark，O(1) 比对）
     + AI 多模态抽帧分析（openai / doubao / qwen / custom，抽样比例可配）
     + 特征启发式辅助（时长异常 / 画面突变 / 静音段，不依赖 AI）
     + 内置播放器（原始 / 去广告 / 单 ts 预览，hls.js FRAG_LOADED 实时标注判定）
     + 去广告 m3u8 动态生成（auto_skip_ad 一键剔除广告 ts）
     + /admin/ai/* 接口 + SSE 实时分析日志
     + 侧边栏 AI 设置页面（前后端 + 指纹库导入导出）

M17 分发与免配置（云端编译 + 单文件运行）
  └─ .github/workflows/build-release.yml 多平台云端编译（linux/windows/darwin × amd64/arm64）
     + CGO_ENABLED=0 静态编译 + go:embed 内嵌 web 前端 → 单文件即完整程序
     + 首次运行自动建环境（config.yaml / data/ / cache/ / logs/ / uploads/）
     + SQLite 默认存储（零数据库配置），检测到 MySQL/Redis 配置才连接
     + 单文件替换更新（后台「更新设置」一键更新 + 手动替换两种方式，配置数据保留）
     + push tag 自动发布 GitHub Release + README 快速开始文档
```

---

## 十二、部署与分发（云端编译 + 免配置运行）

### 1. 核心目标

| 目标 | 说明 |
|---|---|
| **云端编译** | 所有构建在 GitHub Actions 完成，用户**不接触任何编译过程** |
| **免配置运行** | 简单用户下载一个可执行文件就能用，无需 Go / MySQL / Redis / Docker |
| **运行目录自建环境** | 用户设置、数据库、日志等全部自动创建在**运行文件夹**内，与程序分离 |
| **替换即更新** | 更新只需替换打包好的 Go 执行文件，配置与数据全部保留 |

### 2. GitHub Actions 云端编译（多平台 Release）

推送版本 tag（如 `v0.0.6`）→ 自动触发 `.github/workflows/build-release.yml` → 云端编译并发布 GitHub Release。

```
workflow: build-release.yml
触发条件: push tags (v*)
    │
    ├─ 编译矩阵（matrix）
    │   ├─ linux   / amd64   → mxgt-linux-amd64
    │   ├─ linux   / arm64   → mxgt-linux-arm64   （树莓派 / ARM 服务器）
    │   ├─ windows / amd64   → mxgt-windows-amd64.exe
    │   ├─ darwin  / amd64   → mxgt-darwin-amd64
    │   └─ darwin  / arm64   → mxgt-darwin-arm64  （Apple Silicon）
    │
    ├─ 编译参数
    │   ├─ CGO_ENABLED=0          # 静态编译，不依赖系统动态库
    │   ├─ -trimpath              # 去除构建路径信息
    │   ├─ -ldflags "-s -w"       # 减小体积
    │   └─ go:embed 内嵌 web 前端  # 单文件即完整程序（含播放页 + 管理后台）
    │
    ├─ 打包 zip + 生成 sha256 校验文件
    │
    └─ 发布 GitHub Release（草稿 → 人工确认 → 发布）
```

### 3. 免配置直接使用（单文件）

```
1. 到 GitHub Releases 下载对应平台的可执行文件
2. 放到任意目录（该目录即「运行文件夹」）
3. 双击 或 ./mxgt 启动
4. 浏览器打开 http://localhost:8080
5. 完毕 ✅ —— 全程零配置
```

首次启动自动完成：
- 生成 `config.yaml`（带注释的默认配置）
- 自动创建运行目录结构（见下方）
- 默认内嵌 **SQLite** 数据库（零数据库配置）；若用户在 `config.yaml` 填了 MySQL / Redis 地址，则自动切换连接
- 打印访问地址 + 默认管理员账号提示

### 4. 运行目录自建环境（一切都在运行文件夹内）

```
run/  ← 运行文件夹（用户放可执行文件的目录）
├── mxgt                  ← 可执行文件（更新时只替换这一个）
├── config.yaml           ← 用户配置（首次运行自动生成，升级保留）
├── data/
│   └── mxgt.db           ← SQLite 数据库（默认）
├── cache/                ← 本地文件缓存（无 Redis 时降级用）
├── logs/                 ← 运行日志（按天滚动）
├── uploads/              ← 上传文件
└── tmp/                  ← 临时文件
```

- 程序**只使用运行文件夹内的相对路径**，不写系统其它位置
- 数据库、配置、日志、上传全部集中在运行目录 → **备份 = 复制整个文件夹**，**卸载 = 删除整个文件夹**

### 5. 更新方式（替换即更新）

| 方式 | 步骤 | 适用人群 |
|---|---|---|
| **后台一键更新** | 管理后台「更新设置」→ 检查更新 → 一键下载 → 自动备份旧版 → 替换可执行文件 → 提示重启 | 小白用户 |
| **手动替换** | 下载新 Release 可执行文件 → 覆盖运行目录里的 `mxgt` → 重启 | 进阶用户 |

更新后保留：`config.yaml`、`data/` 数据库、`uploads/`、`logs/` —— 全部在运行目录，不随可执行文件打包，因此**替换执行文件即完成升级**。

### 6. web 静态资源内嵌（go:embed）

```go
// cmd/server/main.go
//go:embed all:web/player web/admin
var webFS embed.FS
```

- 播放页 / 管理后台 / AI 分析页在编译期打进可执行文件
- 单文件 = 完整程序，**不需要额外放置 web 目录**
- 更新可执行文件 = 前端后端一起更新，杜绝版本不一致

### 7. 兼容降级（没有也能跑）

| 依赖 | 降级方案 |
|---|---|
| MySQL | 默认 SQLite（`glebarez/sqlite` 纯 Go 驱动，无 CGO） |
| Redis | 本地文件缓存 / 内存缓存（`cache/` 目录） |
| Docker | 不需要，单文件直接运行（Docker 仅作为可选高级部署） |

### 8. 后端接口（更新相关，复用 8.8 更新设置）

```
GET  /admin/update/config       → 读取更新配置（含 Release 下载地址模板）
POST /admin/update/check        → 检查远端版本 → 返回下载链接（多镜像测速选最快）
POST /admin/update/download     → 下载新可执行文件 → 校验 sha256 → 备份旧版 → 替换
GET  /admin/update/logs         → 更新日志
```

---

## 十三、设计原则

1. **内部包引用必须用绝对 module 路径**，严禁用相对路径
2. **接口优先**：Collector / Extractor / Matcher 都是 interface 先定义
3. **配置驱动**：采集源、解析规则全部入库可配
4. **错误统一返回**：`pkg/response` 输出标准 JSON（`{code, msg, data}`）
5. **版本号规则**：v0.0.1 → v0.0.99 → v0.1.0
6. **每次更新代码同步更新 README.md**
7. **跨域零硬编码**：前端 URL 不写死，后端 CORS 开全，proxy 兜底
8. **前端只一个文件**：单 HTML + CDN，部署简单
9. **云端编译**：所有构建在 GitHub Actions 完成，用户不接触编译
10. **免配置运行**：单文件下载即用，首次运行自动建环境
11. **运行目录隔离**：配置/数据/日志全在运行文件夹内，更新只换执行文件，卸载即删文件夹

---

## 十四、待确认事项（TODO）

- [ ] 苹果 CMS v10 是 MXGT 作为上游数据源，还是主动调 CMS？
- [ ] 解析规则引擎是否需要后台灵活配置 JSONPath，还是固定几种类型就够？
- [ ] 预期接入多少个源站？1-5 / 6-10 / 10+
- [ ] Redis 用来做什么？解析结果缓存为主还是会话也要？
- [ ] 在线播放页是否需要多线路切换 UI？
- [ ] proxy 是否需要做流量/带宽统计？
- [ ] 是否需要解析结果的离线预热（定时跑一集集解析存入 resolved_url）？

---

*本文档随代码迭代同步更新。版本 v0.0.9 新增：☁️ 云端编译矩阵扩展至全部主流平台/框架——Linux（amd64/arm64/386/armv7）+ Windows（amd64/arm64/386）+ macOS（amd64/arm64）+ FreeBSD（amd64），Release 打包改为动态遍历自动生成 zip 与校验和。*

*前一版本 v0.0.8 新增：🍎 M7 苹果 CMS v10 适配已落地实现——cms 包（CMSVod/ListResponse/PlayResponse 结构体 + 多线路 ToCMSVod/BuildPlayURL 组装）、CMS Handler（ac=list/detail/search/play 四个对外接口）、路由注册 GET /api.php/provide/vod/，打通「多源采集 → 匹配合并 → 入库 → 苹果 CMS 对外输出」闭环。*

*前一版本 v0.0.7 新增：🔌 M6 采集器已落地实现——Collector 接口 + 注册表（多源对接 api/html/custom）、matcher 剧名模糊匹配（Levenshtein + 别名）与集数提取、sync_service 多源采集→匹配→合并入库 vods/episodes、采集源管理 CRUD + POST /admin/sync 接口。*

*前一版本 v0.0.6 新增：☁️ 部署与分发章节（GitHub Actions 云端编译多平台 Go 可执行文件 + go:embed 单文件免配置运行 + 运行目录自建环境 + 替换即更新 + SQLite 零配置降级）、.github/workflows/build-release.yml 云端编译、M17 里程碑、4 条新设计原则（云端编译 / 免配置运行 / 运行目录隔离 / 替换即更新）。*

*前一版本 v0.0.5 新增：🤖 AI 设置（m3u8 智能分析 ts 分片、MD5 指纹库识别视频内嵌广告/字幕/插播、内置播放器实时标注判定、去广告 m3u8 动态生成、SSE 实时分析日志）、3 张新表（ai_settings / ts_analysis_logs / ad_fingerprints）、新 internal 包 ai/、里程碑扩展到 M16、侧边栏增至 11 个模块。*

*前一版本 v0.0.4 新增：🧠 分析设置（自动识别官方/直链资源）、🎯 匹配设置（AI+规则双通道）、🔌 调用设置（多层 Pipeline 串联）、🗺️ 映射设置（七大站字段映射）、🔄 更新设置（多镜像自动测速 + GitHub 一键更新 + 公告.txt 解析）、4 张新表（analysis_settings / matching_settings / chain_nodes / updater_config + update_logs）、3 个新 internal 包（analyzer / chaining / updater）、里程碑扩展到 M15。*

*前一版本 v0.0.3 新增管理后台侧边栏设计（仪表盘 / 前端设置伪装路径 / 视频抓取映射七大站）+ 3 张新表（call_logs / frontend_settings / site_mappings）+ 里程碑扩展。*
