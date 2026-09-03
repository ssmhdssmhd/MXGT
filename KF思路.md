# MXGT — 开发者思路文档

> 项目定位：视频资源聚合 + 苹果 CMS v10 对接 + JSON 解析路由的综合后台
> 版本：v0.0.1
> 技术栈：Go + Echo + GORM + MySQL + Redis + Docker + GitHub Actions

---

## 一、核心业务链路

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

## 二、目录结构（分层架构）

```
mxgt/
├── cmd/server/
│   └── main.go                 # 程序入口
├── configs/
│   ├── config.yaml             # 运行时配置
│   └── config.example.yaml     # 配置模板
├── internal/                   # 私有包（不对外暴露）
│   ├── config/                 # 配置加载（viper）
│   ├── collector/              # 🆕 采集器（核心）
│   │   ├── collector.go        # Collector 接口：Fetch() → []RawItem
│   │   ├── factory.go          # 工厂：按配置选择采集器
│   │   ├── sources/            # 各种源站实现
│   │   │   ├── source_api.go       # 通用 JSON API 源
│   │   │   ├── source_html.go      # HTML 页面源（正则提取）
│   │   │   └── source_custom.go    # 自定义脚本源
│   │   └── models.go           # 采集原始结构体 RawItem
│   ├── matcher/                # 🆕 匹配映射器
│   │   ├── matcher.go          # Matcher 接口
│   │   ├── fuzzy.go            # 剧名模糊匹配（Levenshtein + 别名表）
│   │   ├── episode.go          # 集数提取正则（第xx集/EPxx/数字）
│   │   └── alias.go            # 剧名别名库
│   ├── extractor/              # 🆕 JSON 解析路由（核心）
│   │   ├── extractor.go        # Extractor 接口：URL → 真实视频链接
│   │   ├── jsonpath.go         # JSONPath 提取器
│   │   ├── regex_ext.go        # 正则提取器
│   │   ├── route.go            # 路由匹配：根据 URL 规则选解析器
│   │   └── rules.go            # 后台配置的解析规则集
│   ├── cms/                    # 🆕 苹果 CMS 适配层
│   │   ├── cms_v10.go          # v10 接口格式
│   │   └── models.go           # 苹果 CMS 输出结构体
│   ├── handler/                # HTTP handlers（按模块分）
│   │   ├── api_handler.go      # 对外 API（苹果 CMS 兼容格式输出）
│   │   ├── admin_handler.go    # 管理后台（配置采集源、解析规则）
│   │   ├── task_handler.go     # 手动触发采集/解析任务
│   │   └── proxy_handler.go    # 代理转发（某些源需要中转）
│   ├── service/                # 业务逻辑层
│   │   ├── sync_service.go     # 一键采集+映射+入库
│   │   ├── resolve_service.go  # 解析路由调度
│   │   └── cms_service.go      # 对外 CMS 接口数据组装
│   ├── repository/             # 数据访问层（GORM）
│   │   ├── vod_repo.go
│   │   ├── episode_repo.go
│   │   ├── source_repo.go
│   │   └── rule_repo.go
│   ├── middleware/             # 中间件（JWT、CORS、限流、日志）
│   └── router/                 # 路由注册
├── pkg/                        # 公共工具包（可被外部引用）
│   ├── httpclient/             # 封装 HTTP（超时、重试、UA、代理、压缩解压）
│   ├── jsonpath/               # JSONPath 实现
│   ├── response/               # 统一响应格式
│   ├── errors/                 # 自定义错误
│   └── utils/                  # 工具函数
├── migrations/                 # 数据库迁移
├── deployments/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   └── .env.example
├── .github/workflows/
│   └── ci.yml                  # GitHub Actions CI/CD
├── web/admin/                  # 管理后台静态资源（后补）
├── test/
├── go.mod
├── go.sum
└── README.md
```

---

## 三、数据库表设计（核心 4 张表）

### vods — 影片主表（统一后的标准数据）

```sql
CREATE TABLE vods (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    vod_id          VARCHAR(64)  NOT NULL UNIQUE COMMENT '外部源唯一标识（可选）',
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

### episodes — 集数表（一部片子 → 多集）

```sql
CREATE TABLE episodes (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    vod_id          BIGINT UNSIGNED NOT NULL,
    episode_no      INT NOT NULL COMMENT '集数（1,2,3...）',
    episode_name    VARCHAR(255) DEFAULT '' COMMENT '第x集 / EP0x',
    source_url      VARCHAR(1024) NOT NULL COMMENT '源站播放页 URL',
    resolved_url    VARCHAR(1024) DEFAULT '' COMMENT '解析后的真实视频 URL（可空，按需解析）',
    source_name     VARCHAR(128) DEFAULT '' COMMENT '来源采集源名称',
    play_line       VARCHAR(128) DEFAULT '' COMMENT '播放线路标签（如：主线、备用）',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_vod_ep (vod_id, episode_no, source_name),
    INDEX idx_source_url (source_url(255)),
    CONSTRAINT fk_vod FOREIGN KEY (vod_id) REFERENCES vods(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### sources — 采集源配置（管理后台添加）

```sql
CREATE TABLE sources (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL COMMENT '采集源名称',
    source_type     VARCHAR(32)  NOT NULL COMMENT 'api / html / custom',
    fetch_url       VARCHAR(512) NOT NULL COMMENT '采集入口 URL（支持 {keyword} 占位符）',
    method          VARCHAR(8)   DEFAULT 'GET',
    headers         JSON         COMMENT 'HTTP Header 配置',
    params          JSON         COMMENT 'Query / Body 参数模板',
    extract_rules   JSON         NOT NULL COMMENT '这个源的字段提取规则（JSONPath 数组）',
    priority        INT DEFAULT 0 COMMENT '源的优先级，数值越大越优先',
    enabled         TINYINT DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**extract_rules 示例（JSON API 源）：**

```json
{
    "list_path": "$.data.list",
    "fields": {
        "name": "$.vod_name",
        "cover": "$.vod_pic",
        "year": "$.vod_year",
        "episodes_path": "$.vod_play_from[0].vod_play_list[0].urls"
    }
}
```

### extract_rules — JSON 解析规则（解析路由核心）

```sql
CREATE TABLE extract_rules (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    url_pattern     VARCHAR(512) NOT NULL COMMENT 'URL 匹配正则：决定哪些 URL 用这条规则',
    extractor_type  VARCHAR(32)  NOT NULL COMMENT 'jsonpath / regex / custom',
    rule_config     JSON         NOT NULL COMMENT '具体规则',
    target_field    VARCHAR(64)  DEFAULT 'url' COMMENT '提取哪个字段：url / playurl / m3u8',
    priority        INT DEFAULT 0,
    enabled         TINYINT DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_pattern (url_pattern(128))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**rule_config 示例：**

```json
// JSONPath 类型
{ "jsonpath": "$.data.url" }

// 正则类型
{ "regex": "url\\s*[:=]\\s*[\"']([^\"']+\\.m3u8[^\"']*)[\"']", "group": 1 }
```

---

## 四、核心接口

### 对外（苹果 CMS v10 兼容）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api.php/provide/vod/?ac=list&t=1&pg=1` | 分类列表 |
| GET | `/api.php/provide/vod/?ac=detail&ids=123` | 影片详情（含集数） |
| GET | `/api.php/provide/vod/?ac=search&wd=xxx` | 搜索 |
| GET | `/api.php/provide/vod/?ac=play&id=123&ep=5` | 获取某集真实播放链接（内部走解析路由） |

### 对内（管理后台）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/admin/login` | 管理后台登录 |
| POST | `/admin/source` | 新增采集源 |
| PUT  | `/admin/source/:id` | 修改采集源（含字段提取规则） |
| DELETE | `/admin/source/:id` | 删除采集源 |
| POST | `/admin/sync` | 触发全量/增量采集 |
| POST | `/admin/extract/rule` | 新增解析规则 |
| PUT  | `/admin/extract/rule/:id` | 修改解析规则 |
| POST | `/admin/resolve` | 测试某 URL 用哪条规则能解析出什么 |
| GET  | `/admin/vods` | 影片列表 |
| GET  | `/admin/vods/:id/episodes` | 某片的集数列表 |

---

## 五、关键技术点

| 难点 | 解决方案 |
|---|---|
| 剧名模糊匹配 | Levenshtein 距离 + 别名表（后台可维护别名）+ 去年份去标点后再比对 |
| JSONPath 提取 | 用 `github.com/PaesslerAG/jsonpath`，支持 `$.data.list[*].video_url` 语法 |
| 集数提取正则 | 统一一套：`第\s*(\d+)\s*[集话期]` / `EP(\d+)` / `^(\d+)$` 按优先级尝试 |
| 并发控制 | 采集用 `errgroup` + 信号量限流（避免被封 IP） |
| HTTP 客户端 | 统一超时、自动 gzip/brotli 解压、随机 UA、失败重试 3 次 |
| 苹果 CMS 格式 | 严格对齐 v10 的 JSON schema（`list[].vod_id / vod_name / vod_play_url` 等字段名） |
| 解析路由 | `source_url` 匹配 `url_pattern` → 找到优先级最高的规则 → 调 extractor → 拿到视频 URL |
| 配置化规则 | 采集源和解析规则全部后台可配，支持热生效 |

---

## 六、第三方依赖清单（初版）

```
github.com/labstack/echo/v4            # Web 框架
gorm.io/gorm                            # ORM
gorm.io/driver/mysql                    # MySQL 驱动
github.com/spf13/viper                  # 配置管理
github.com/golang-jwt/jwt/v5            # JWT
go.uber.org/zap                         # 日志
github.com/PaesslerAG/jsonpath          # JSONPath 实现
github.com/go-resty/resty/v2            # HTTP Client（比 net/http 好用）
github.com/PuerkitoBio/goquery          # HTML 解析（可选）
github.com/swaggo/swag                  # API 文档（可选）
github.com/joho/godotenv                # .env 支持
golang.org/x/sync/errgroup              # 并发控制
github.com/xrash/smetrics               # 字符串相似度（Levenshtein）
```

---

## 七、开发顺序（里程碑）

```
M1 搭架子
  └─ go.mod + 目录骨架 + Echo + MySQL 连接 + 配置加载 + 路由注册 + 健康检查接口

M2 数据层
  └─ 4 张核心表迁移 + Repository 层 CRUD + gorm 自动迁移脚本

M3 采集器 MVP
  └─ Collector 接口 + source_api + source_html + Factory + 并发控制 + 入库跑通

M4 匹配映射器
  └─ 集数正则提取 + 剧名模糊匹配 + 别名表 + 合并/去重逻辑

M5 JSON 解析路由
  └─ Extractor 接口 + JSONPath 提取 + regex 提取 + 路由匹配 + extract_rules 表联动

M6 苹果 CMS 适配
  └─ cms_v10 结构体 + 对外 4 个 ac 接口跑通 + play 接口走解析路由返回真实 URL

M7 管理后台 API
  └─ source / extract_rule / vod / sync / resolve 全部 CRUD 接口

M8 部署
  └─ Dockerfile + docker-compose + GitHub Actions + README 更新
```

---

## 八、设计原则

1. **内部包引用必须用绝对 module 路径**，例如 `github.com/mxgt/mxgt/internal/collector`，严禁用 `./collector` 相对路径
2. **接口优先**：Collector / Extractor / Matcher 都先定义 interface，再接实现，方便测试和扩展
3. **配置驱动**：采集源、解析规则全部入库可配，不硬编码
4. **错误统一返回**：handler 层捕获 → `pkg/response` 输出标准 JSON（`{code, msg, data}`）
5. **版本号规则**：v0.0.1 → v0.0.99 → v0.1.0，百位进一位
6. **每次更新代码同步更新 README.md**

---

## 九、苹果 CMS v10 对接约定（待最终确认）

**假设 MXGT 作为苹果 CMS 的上游采集接口源**：

苹果 CMS 在后台添加自定义接口 → 填 MXGT 的 URL → MXGT 按 v10 格式输出：

```json
{
    "code": 1,
    "msg": "采集成功",
    "list": [
        {
            "vod_id": "1001",
            "vod_name": "庆余年 第二季",
            "vod_pic": "https://xxx/cover.jpg",
            "vod_year": "2024",
            "vod_play_from": "主线$$$备用",
            "vod_play_url": "01$https://xxx/ep01.m3u8#02$https://xxx/ep02.m3u8",
            ...
        }
    ]
}
```

`vod_play_url` 格式：`集数标题$真实URL#集数标题$真实URL...`，多线路用 `$$$` 分隔。

> 若后续需要 MXGT 主动调苹果 CMS 的接口反向同步，另加一个 `internal/cms/pusher.go` 模块。

---

## 十、待确认事项（TODO）

- [ ] 苹果 CMS v10 是 MXGT 作为上游数据源，还是主动调 CMS？
- [ ] 解析规则引擎是否需要后台灵活配置 JSONPath，还是固定几种类型就够？
- [ ] 预期接入多少个源站？1-5 / 6-10 / 10+
- [ ] 管理后台前端是自己写（Vue/React）还是先用简单 HTML？
- [ ] Redis 用来做什么？会话缓存？解析结果缓存？还是先不引入？
- [ ] 是否需要 WebSocket 实时推送采集进度？

---

*本文档随代码迭代同步更新。*
