# MXGT — 开发者思路文档

> 项目定位：视频资源聚合 + 苹果 CMS v10 对接 + JSON 解析路由 + 在线播放页的综合后台
> 版本：v0.0.2
> 技术栈：Go + Echo + GORM + MySQL + Redis + Docker + GitHub Actions

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
│   └── ci.yml
├── web/
│   ├── player/                 # ⭐ 前端在线播放页（?url=xxx）
│   │   ├── index.html
│   │   ├── js/player.js
│   │   └── js/api.js
│   └── admin/                  # 管理后台（后补）
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

---

## 八、关键技术点

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

---

## 九、第三方依赖清单

```
github.com/labstack/echo/v4            # Web 框架
github.com/labstack/echo-contrib       # echo-session / basicauth 等
gorm.io/gorm                            # ORM
gorm.io/driver/mysql                    # MySQL 驱动
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
```

---

## 十、开发顺序（里程碑）

```
M1 搭架子
  └─ go.mod + 目录骨架 + Echo + MySQL + Redis 连接
     + 配置加载 + 路由注册 + CORS 中间件 + 健康检查
     + 前端 web/player/index.html 基础版（同域 API）

M2 数据层
  └─ 4 张核心表 gorm model + 自动迁移 + Repository CRUD

M3 前端在线播放页 MVP
  └─ 单文件 index.html + hls.js + DPlayer
     + ?url=xxx 参数解析
     + API_BASE 四层兜底（重点是不硬编码）
     + 调 /api/resolve 播放 m3u8

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

M8 管理后台 API
  └─ source / extract_rule / vod / sync / resolve/test CRUD

M9 部署
  └─ Dockerfile + docker-compose + GitHub Actions + README 更新
```

---

## 十一、设计原则

1. **内部包引用必须用绝对 module 路径**，严禁用相对路径
2. **接口优先**：Collector / Extractor / Matcher 都是 interface 先定义
3. **配置驱动**：采集源、解析规则全部入库可配
4. **错误统一返回**：`pkg/response` 输出标准 JSON（`{code, msg, data}`）
5. **版本号规则**：v0.0.1 → v0.0.99 → v0.1.0
6. **每次更新代码同步更新 README.md**
7. **跨域零硬编码**：前端 URL 不写死，后端 CORS 开全，proxy 兜底
8. **前端只一个文件**：单 HTML + CDN，部署简单

---

## 十二、待确认事项（TODO）

- [ ] 苹果 CMS v10 是 MXGT 作为上游数据源，还是主动调 CMS？
- [ ] 解析规则引擎是否需要后台灵活配置 JSONPath，还是固定几种类型就够？
- [ ] 预期接入多少个源站？1-5 / 6-10 / 10+
- [ ] Redis 用来做什么？解析结果缓存为主还是会话也要？
- [ ] 在线播放页是否需要多线路切换 UI？
- [ ] proxy 是否需要做流量/带宽统计？
- [ ] 是否需要解析结果的离线预热（定时跑一集集解析存入 resolved_url）？

---

*本文档随代码迭代同步更新。版本 v0.0.2 新增三层结构 + 前端播放页思路 + 跨域方案。*
