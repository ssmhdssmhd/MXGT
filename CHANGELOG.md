# 更新日志

## Go 分支 v0.3.3 (2026-09-09) — 更新面板显示当前/最新版本 + 修复 Failed to fetch

### 更新面板直接显示版本；修复检查更新远程连通慢导致的 Failed to fetch

> 用户诉求：更新时要显示当前版本和最新版本；检查失败提示 `Failed to fetch`。

#### 1. 服务端渲染版本（[main.go](file:///workspace/main.go) `renderUpdateBlock`）

- 后台「远程在线更新」面板打开即显示「**当前版本 → 最新版本** + 是否有新版」，由服务端 `handleAdmin()` 用 `renderUpdateBlock()` 直接注入 HTML，**不依赖客户端 fetch**——网络差/离线也能看到版本；
- 前端 `checkUpdate()` 仅做状态刷新，失败时不再覆盖已显示的版本信息。

#### 2. 修复 Failed to fetch（[main.go](file:///workspace/main.go)）

- 根因：`/api/update/check` 在请求内同步访问 `raw.githubusercontent.com`（国内常慢/被墙），原 `updateHTTP` 超时 60s，连通差时长时间阻塞导致浏览器等不到响应报 `Failed to fetch`；
- 修复：清单拉取改用独立 `manifestHTTP` 客户端，**超时 8 秒**；`/api/update/check` 实测约 0.1s 返回。

#### 3. 版本与验证

- 版本升级 `v0.3.2 → v0.3.3`；[README.md](file:///workspace/README.md) 同步 v0.3.3 更新日志。
- 验证：`/mxadmin` 直接渲染「当前版本 v0.3.3 → 最新版本 v0.3.2」；`/api/update/check` 0.1s 返回；`go vet`/静态编译通过。

---

## Go 分支 v0.3.2 (2026-09-09) — 后台入口改为 /mxadmin + 页头标注开发者

### 后台不再 / 直达，改为域名/mxadmin；后台显眼标注开发者

> 用户诉求：后台必须 `域名/mxadmin` 才能进入，不能 `http://IP:8080/` 直接进后台；后台显眼处显示开发者 ssmhdssmhd。

#### 1. 路由（[main.go](file:///workspace/main.go)）

- **后台入口 `/mxadmin`**：仅 `/mxadmin`、`/mxadmin/` 进入后台；原 `/admin`、`/admin/` 入口已移除（返回 404）；
- **首页 `/` 落地页**：`/` 不再进入后台，改为简洁落地页（品牌「MXGT-Go 去广告服务」+ 版本 + 开发者 ssmhdssmhd +「进入后台管理 →」链接）；
- 其余未知路径保持 404。

#### 2. 开发者署名片（[main.go](file:///workspace/main.go) admin）

- 后台**页头右上角显眼标注**「开发者 · ssmhdssmhd」+「品牌 MXGT」。

#### 3. 版本与验证

- 版本升级 `v0.3.1 → v0.3.2`；[README.md](file:///workspace/README.md) 同步接口表、访问方式与 v0.3.2 更新日志。
- 验证：`/` → 落地页(200，含开发者与后台入口)、`/mxadmin` → 后台(200，含「开发者 · ssmhdssmhd」)、`/admin` → 404；`go vet`/静态编译通过。

---

## Go 分支 v0.3.1 (2026-09-09) — 修复远程更新下载地址（Release tag 双 v 对齐）

### 下载 404 根因与修复

- **根因**：GitHub Actions 的 Release tag 形如 `go-vvX.Y.Z`（`VER` 变量本身含前导 `v`，`tag_name: go-v${{ env.VER }}` 得到 `go-vv0.3.0`）；而 [main.go](file:///workspace/main.go) 的 `updateInfo()` 下载地址拼的是单 v `go-vX.Y.Z` → 远程更新下载 **404**；
- **修复**：下载地址改为 `releaseBaseURL + "/go-vv" + version + "/" + zip`，与实际 tag 对齐；
- **验证**：`go vet`/静态编译通过；`/api/update/check` 返回 `download_url` 指向 `…/download/go-vv0.3.0/…zip`，`HEAD` 200（修复前 404）；
- 版本升级 `v0.3.0 → v0.3.1`；[README.md](file:///workspace/README.md) Go 章节同步 v0.3.1 更新日志。

---

## Go 分支 v0.3.0 (2026-09-09) — 新增远程在线更新

### 检查 GitHub Release → 下载 → 原子替换 → 自动重启

> 用户诉求：Go 版也要支持像 PHP 版那样的远程在线更新。

#### 1. 更新接口与清单（[main.go](file:///workspace/main.go) + [build-go.yml](file:///workspace/.github/workflows/build-go.yml)）

- **接口**：`GET /api/update/check`（检查是否有新版本）、`POST /api/update/apply`（下载并替换重启）；
- **版本清单 `latest.json`**：Actions 每次发布后自动写入并用 `git push` 回传 `go` 分支根目录（含最新版本号 + Release zip 文件名）；push 触发用 `paths-ignore: latest.json` 防止清单提交再次触发构建（死循环）；
- **规避限流**：服务从 `raw.githubusercontent.com/ssmhdssmhd/MXGT/go/latest.json` 读取，**不调用 GitHub API**（API 未认证有 60 次/小时限流，实测会 403）；
- **更新流程**：下载 Release zip → 解出与当前同名二进制 → 备份旧版为 `<exe>.bak` → 原子 `os.Rename` 替换 → 用 `exec.Command` 启动新进程（继承启动参数/端口/环境）→ 本进程 `os.Exit(0)`。

#### 2. 后台 UI（[main.go](file:///workspace/main.go) admin）

- 新增「🔄 远程在线更新」面板：「🔍 检查更新」「⬇ 下载并更新重启」按钮；
- 刷新页面自动调用 `check` 显示当前→最新版本与是否有新版。

#### 3. 版本与验证

- 版本升级 `v0.2.1 → v0.3.0`；[README.md](file:///workspace/README.md) Go 章节同步接口表、新增「远程在线更新（Go 版）」章节与 v0.3.0 更新日志；新增 `latest.json`。
- 验证：`go vet`/`go build` 通过；`/api/update/check` 在清单未发布时优雅返回 `manifest HTTP 404`（不崩溃）；后台更新面板正常渲染。

---

## Go 分支 v0.2.1 (2026-09-09) — 前端跨域 + 直接播放 + 无硬编码

### 后台补全跨域、新增内嵌播放器、播放地址不写死

> 用户诉求：解析结果跨域可播放、有画面、无广告、不硬编码服务器地址。

#### 1. 全局跨域（[main.go](file:///workspace/main.go) `withCORS`）

- 新增 `withCORS` 中间件包裹整个服务：所有响应统一补 `Access-Control-Allow-Origin/-Methods/-Headers/-Expose-Headers`，放行 `Range`（TS 分片片段请求），`Access-Control-Max-Age` 兜底；OPTIONS 预检直接返回 204；
- `Origin` 为空时回退 `*`，否则回显来源 Origin，并设 `Vary: Origin`。

#### 2. 内嵌播放器（后台页）

- 「解析测试」新增「▶ 直接播放无广告」按钮；点击后弹出 `video` 播放器（hls.js 多 CDN 兜底加载，动态设置请求 `Origin`），**就地播放过滤后的无广告画面**；
- 播放源地址由 `location.origin + buildCleanURL(...)` **动态拼接**，不硬编码任何 IP/域名；
- 新增后端 `playURL(r, path, query)` 按请求 Host 动态推导可播放地址的辅助函数。

#### 3. 版本与验证

- 版本升级 `v0.2.0 → v0.2.1`；[README.md](file:///workspace/README.md) Go 章节同步 v0.2.1 更新日志。
- 验证：OPTIONS 预检 204 + 全套 CORS 头；`/api/clean` m3u8 带 CORS；后台页、`/api/stats` 均带 `Access-Control-Allow-Origin`；浏览器实测后台渲染正常、解析输出过滤后 `#EXTM3U`、点击直接播放弹出播放器并显示动态播放源，控制台无致命报错。

---

## Go 分支 v0.2.0 (2026-09-09) — 新增后台管理页面

### 单文件服务新增玻璃拟态风格后台（运行统计 + 解析测试 + 接口说明）

> 打开浏览器访问服务地址即进后台：`GET /`（或 `/admin`）。全程内嵌单文件，仍零依赖、静态编译。

#### 1. 后台管理页（[main.go](file:///workspace/main.go)）

- **路由**：`GET /` 与 `GET /admin` 渲染内置后台 HTML（深紫→粉渐变 + 毛玻璃，与 PHP 版后台观感一致）；未知路径返回 404；
- **运行统计**：新增内存 `ServerStats` + `GET /api/stats`，展示版本、运行时长、清洗请求数/失败数、累计片段数/广告段/保留段、广告占比、最近解析结果与时间；后台每 5 秒自动刷新；
- **解析测试**：后台输入 m3u8 地址 + 聚合开关，一键调用 `/api/clean` 返回过滤后 M3U8 纯文本与统计；
- **接口说明**：后台内嵌 HTTP 接口清单；
- 原有 `/api/clean`、`/api/clean/json` 接入统计（成功/失败计数、片段统计、最近结果）；`/healthz` 保持不变。

#### 2. 版本

- 升级 `v0.1.1 → v0.2.0`；[README.md](file:///workspace/README.md) Go 章节同步更新接口表与 v0.2.0 更新日志。

#### 3. 验证

- `go vet` / `CGO_ENABLED=0 go build` 通过，静态版启动正常；
- 实测：`/`、`/admin` 返回 200 HTML；`/api/stats` 初值全 0 → 清洗 1 次后正确变为 `requests=1 / total_segs=64 / kept_segs=64` 且带「最近」消息；未知路径 404。

---

## Go 分支 v0.1.1 (2026-09-08) — 启动失败修复：静态编译消除 GLIBC 依赖

### 服务器启动报错「GLIBC_2.34/2.32 not found」根因与修复

> 部署报错：`/www/wwwroot/go/go1/mxgtgo/mxgt-go: /lib64/libc.so.6: version 'GLIBC_2.34' not found (required by .../mxgt-go)`，二进制无法启动。

#### 1. 根因（[.github/workflows/build-go.yml](file:///workspace/.github/workflows/build-go.yml)）

- 云端编译在 `ubuntu-latest` 上执行 `go build`，**默认启用 CGO** → 产物为**动态链接** ELF，依赖 runner 的新版 glibc（要求 `GLIBC_2.32` / `GLIBC_2.34`+）；
- 部署服务器 glibc 较旧，动态加载器找不到所需版本，**一启动就失败**；本地自编若在较新系统上编译同样会踩坑。

#### 2. 修复

- 编译命令改为 `CGO_ENABLED=0 go build -ldflags "-s -w" -o mxgt-go main.go`：**纯静态链接**（`ldd` → `not a dynamic executable`），不依赖任何宿主 libc，CentOS 7 等旧系统开箱即用；
- [main.go](file:///workspace/main.go) 版本升级 `v0.1.0 → v0.1.1`，头部编译注释同步标注静态编译；
- [README.md](file:///workspace/README.md) Go 章节：本地编译命令补 `CGO_ENABLED=0`，新增「部署注意事项（GLIBC 兼容）」与 v0.1.1 更新日志。

#### 3. 验证

- 本地对照：默认 `go build` → `file` 显示 `dynamically linked, libc.so.6`（复现原报错条件）；`CGO_ENABLED=0` → `statically linked` + `ldd` 报 `not a dynamic executable`（~6.6MB）；
- 静态版启动 `./mxgt-go -addr :8080` → 日志 `MXGT-Go v0.1.1 listening`，`GET /healthz` 返回 `ok v0.1.1`；`go vet` 通过，无任何报错。

---

## Go 分支 v0.1.0 (2026-09-08) — Go 单文件 M3U8 去广告服务首发

### 单文件、标准库零依赖、GitHub Actions 云端编译单二进制

- **Go 单文件服务**（[main.go](file:///workspace/main.go)）：HTTP 服务接收 m3u8 链接，抓取 → 解析（自动跟随 master playlist）→ 保守广告检测 → 输出无广告 M3U8（绝对地址、保留 EXT-X-KEY/MAP/不连续标签）；
- **接口**：`/api/clean?url=`（返回 M3U8 文本）、`/api/clean/json?url=`（JSON 统计+明细）、`/healthz`；同时支持命令行模式 `go run main.go <url>`；
- **广告检测（保守防误删）**：① URL 关键词（ad/ads/ad0/gdt/tvc/promo/300x250 等）② 广告标签区间（DATERANGE/CUE-OUT/EXT-X-AD）③ 超短视频(<1.0s，片头保护) ④ `opt=aggresive` 聚合聚类（默认关，防误伤统一切片正片）；
- **云端编译**（[.github/workflows/build-go.yml](file:///workspace/.github/workflows/build-go.yml)）：push 到 `go` 分支自动编译 Linux amd64 单文件二进制（~6.6MB），按规则打包 `MXGT_go_<版本>_<北京时间yyyyMMddHHmm>.zip` 上传 GitHub Release；
- **验证**：本地实测 12 段（含 3 段关键词广告 + 1 段超短占位）→ 识别 4 段广告、保留 8 段、输出绝对地址、TARGETDURATION 按实际时长计算；`go build`/`go vet` 通过。

---

# PHP 版更新日志（branch `main`）

## v5.15.11 (2026-09-08) — 学习 502 修复 + 后台全结果折叠

### HTTP 异步执行立即返回避免 nginx 502；后台所有结果区域可折叠、滚动条拖动查看

> 学习失败报「服务器返回非JSON响应: 502 Bad Gateway」的根因：AI 自动学习经 HTTP 回环触发时，学习任务（遍历全部资源站 + 深度 M3U8 解析）超过 nginx/PHP-FPM 超时被掐断，触发方收到 502 HTML。

#### 1. 学习 502 修复（[cron_ai_autolearn.php](file:///workspace/cron_ai_autolearn.php)）

- HTTP 模式（`cron_ai_autolearn.php?force=1` 等）在执行学习/清理前调用新增的 `aiHttpDetach()`；
- PHP-FPM 环境：输出 JSON 后 `fastcgi_finish_request()` 立即 flush 响应，nginx 不再等待；
- 其他环境：`Content-Length + Connection: close` 后 `flush()` 关闭连接；
- 触发方**立即收到 200**，任务在后台继续执行（`ignore_user_abort(true)` + `set_time_limit(0)`），不再 502。

#### 2. 后台全结果折叠（[mxadmin.php](file:///workspace/mxadmin.php)）

- 新增通用折叠组件 `makeFold()` / `initResultFolds()`，覆盖 **22 个结果容器**：视频分析 / 批量解析 / 资源站搜索 / 自动学习 / AI自动学习+日志 / 官替测试 / 嗅探测试 / 沫兮测试 / 数据库迁移 / 完整性检查 / 缓存清理 / 在线更新 / AI去广告 / 专业检测 / 插播识别 / 字幕分析 / 水印处理 / 接口选择；
- 点击标题栏折叠/展开；展开内容区**限高 420px + 自定义滚动条拖动查看**；双击标题栏**不限高**看全貌；折叠状态 `localStorage` 持久化；
- 纯前端渐进增强，不改任何后端渲染逻辑。

#### 3. 验证

- HTTP 模式实测 0ms 返回 `{"success":true,"async":true,"message":"AI 自动学习已提交后台执行"}`，不再 502；
- `php -l cron_ai_autolearn.php / mxadmin.php / version.php` 通过；主脚本块 `node --check` 通过；
- 浏览器实测 analyze / batch / ai_autolearn / api_picker / ai_skip 5 页：22 个折叠卡片全部渲染、点击折叠/展开往返正常、控制台 0 错误。

## v5.15.10 (2026-09-08) — AI自动学习长时间不动修复

### 懒触发 exec 禁用时不再静默失败、定时脚本 DB 模式适配、触发失败可自动重试

> AI 自动学习「配置长时间不动」（`last_run_time` 长期不更新）的根因：懒触发 `autoTriggerIfNeeded()` 内部只调用 `exec` 后台执行 `cron_ai_autolearn.php`，服务器禁用 `exec()` 时 `@exec` 静默失败、无任何回退 → 学习从未真正执行，状态页显示的时间永远不变。

#### 1. 根因（[AiAutoLearner.php](file:///workspace/gz/AiAutoLearner.php)）

- 懒触发（挂在 `info/version`，后台打开即检查）调用 private `triggerBackgroundRun()`，**只走 `exec`**；
- 生产服务器常禁用 `exec()`（`disable_functions`），`@exec` 静默失败且无回退，也不抛异常 → 懒触发"看似触发、实际从不执行"；
- `last_run_time` 永远停在旧值 → 后台「AI自动学习」显示长时间不动。

#### 2. 修复

- **懒触发回退**：`autoTriggerIfNeeded()` 改用带三级回退的 `triggerBackgroundRunAsync()`（exec → fsockopen 非阻塞 HTTP → curl 短超时），exec 禁用环境也能真正触发学习；失效规则清理触发同步增加 exec 禁用回退；
- **触发失败可重试**：`mx.php ai_autolearn/run` 不再预先更新 `last_run_time`（改由 cron 脚本实际执行 `run()` 成功后更新）——触发失败不会把 `last_run_time` 顶到未来导致数小时不再重试；
- **定时脚本 DB 适配**：`cron_ai_autolearn.php` 检测到 `db/db_config.php` 时使用 `DbResourceSiteManager` + `DbDomainRuleManager`（与 gx.php 的 `buildAiLearner()` 一致），DB 模式下定时学习正确写入 `domain_rules` 表。

#### 3. 验证

- `php -d disable_functions=exec` 实测：`autoTriggerIfNeeded()` 返回 `{"triggered":true,"reasons":["learn","cleanup"]}`（走回退通道），不再静默失败；
- `php -l gz/AiAutoLearner.php / cron_ai_autolearn.php / mx.php` 全部通过。

## v5.15.9 (2026-09-08) — 去广告监控数据异常修复

### 监控数据文件损坏自动自愈 + 原子写防并发写坏，接口不再报「获取监控数据异常」

> 播放开启 `mon=1` 后，多个请求并发写同一个监控数据文件（`gz/monitor_data.php`，无锁）会把文件写坏（截断/交错的不完整 PHP 数组）；下次读取时 `@include` 会抛 **ParseError**（`@` 无法抑制异常），导致 `monitor/list` 等接口整体异常，后台提示「获取监控数据异常」。

#### 1. 根因（[AdMonitor.php](file:///workspace/gz/AdMonitor.php)）

- 去广告监控数据落盘 `gz/monitor_data.php`（PHP 数组文件）；`record()` 在每次 `mxjx mon=1` 播放时被调用并整体重写文件；
- 旧实现 `file_put_contents` 直接覆盖同一文件、无锁 → 并发播放/多请求同时写入时产生**截断或交错**的不完整文件；
- 损坏文件被 `@include` 时抛 `ParseError`（PHP 8 中语法错误文件 include 抛异常，`@` 只抑制 warning），向上冒泡导致接口异常。

#### 2. 修复

- **自愈**：`load()` 捕获 `ParseError`/非数组，将损坏文件改名备份（`monitor_data.php.bak-时间戳`）并重建默认监控数据写回——接口立即恢复，不再持续报错；
- **防写坏**：`save()` 改为**原子写**：先写临时文件（`flock` 排他锁 + `fflush`），成功后 `rename` 原子覆盖；`rename` 失败（跨设备/权限受限）退化为直接写入。

#### 3. 验证

- 损坏文件场景实测：备份生成 + 数据重建 + `record()` 正常；
- 真实路径写读往返正常（监控版本 `5.15.8.0001`）；
- `monitor/list`、`monitor/status` 接口返回正常 JSON；
- `php -l gz/AdMonitor.php` 通过。

## v5.15.8 (2026-09-08) — AI自动学习优化 + 数据库自动保存 + 去广告监控修复

### 默认全部资源站按速度排序学习、规则自动入库启用定时任务、播放器去广告监控 mon=1 生效

> 本轮聚焦三条主线：① **AI 自动学习**默认覆盖全部启用资源站、按响应速度/健康排序择优学习；② **规则自动保存到数据库**并接入 gx.php 定时任务（task_ai_learn / task_ai_cleanup），DB 模式下直接写 `domain_rules` 表；③ 修复 **去广告监控长期不生效**——根因是播放器生成的播放链接从不带 `mon=1`，实时监控从未在真实播放中被记录。

#### 1. AI 自动学习优化（[AiAutoLearner.php](file:///workspace/gz/AiAutoLearner.php) + [ai_auto_learn_config.php](file:///workspace/gz/ai_auto_learn_config.php)）

- **默认全部资源站**：`max_sites_per_run` 默认 `0`（不限制），`target_mode` 默认 `all`，一次学习自动覆盖全部**启用（未暂停）**资源站，不再只测前 3 个；
- **按速度/健康排序**：新增 `sort_by_speed`（默认 `true`）——学习前按响应速度排序，快的优先学；复用 **24 小时新鲜测速缓存**，无缓存时实时 `checkSiteHealth` 并写回 `response_time/last_check`；健康优先、失败/超时排最后兜底；
- **自有测速写回**：`sortSitesBySpeed()` + `persistSiteSpeed()` 兼容 DB（`last_check_time`）与文件（`last_check`）两种管理器。

#### 2. 自动保存规则 + 定时任务（[AiAutoLearner.php](file:///workspace/gz/AiAutoLearner.php) + [gx.php](file:///workspace/gx.php)）

- **auto_save_rules**（默认 `true`）：DB 模式学习结果写 `domain_rules` 表、文件模式写 `rules_*.php`；关闭时仅分析不落库；
- **gx.php `buildAiLearner()`**：检测到 `db/db_config.php` 时使用 `DbResourceSiteManager` + `DbDomainRuleManager`，`task_ai_learn` / `task_ai_cleanup` 规则正确**入库**，异常自动回退文件版；
- **清理兼容 DB**：`cleanupStaleRules` 依赖的 `_filemtime` 在 DB 版由 `updated_at` 折算，DB 模式下失效规则清理同样生效。

#### 3. 去广告监控不生效修复（[player/index.php](file:///workspace/player/index.php)）

- **根因**：播放器入口生成的 `mxjx` 播放链接只带 `ph=1`、从不带 `mon=1`，`mxjx` 的实时去广告监控（`AdMonitor::record`）因此在真实播放中从未被触发；
- **修复**：播放链接补充 `mon=1`（普通 `mxjx` 入口 + 官替 fallback 深度入口），播放即记录去广告处理、自动识别高危/可疑删除；
- **验证**：`php -l` 六个文件全部通过。

## v5.15.7 (2026-09-07) — 后台全功能体检修复

### 全量语法 + 接口 + 页面实测，修复 AI自动去广告「MD5特征码分析」DOM id 引用失效与入口缺失

> 对后台做了系统性体检：全部 PHP 文件 `php -l` 通过；后台调用的所有 action 与 mx.php 的 case 一一对应（无未实现接口）；本地起服务实测列表/配置/保存接口均正常；浏览器逐页打开主要页面确认无 JS 报错。发现并修复 **1 个确定性缺陷**：AI自动去广告页的 MD5 特征码分析功能引用了两个页面不存在的 checkbox id，一旦触发会 `TypeError` 直接中断，且该功能没有任何触发按钮。

#### 1. 体检范围与结论（[mxadmin.php](file:///workspace/mxadmin.php) + [mx.php](file:///workspace/mx.php)）

- **语法**：所有 `.php` 文件 `php -l` 全部通过；
- **接口一致性**：子任务全量交叉比对，后台以 `?action=XXX` 引用的全部 action 在 mx.php 均有对应 `case`，无未实现接口；
- **运行时**：本地起服务冒烟，只读接口（info/version、sites/list、rules/list、announcement/list、official_sites/list、sniffer/player/fallback/api_picker/official_replace 等 config、monitor、db/status、proxies/list…）全部 ok；写回接口（ai_autolearn/fallback/api_picker/official_replace/sniffer config/save）原值写回均 success；
- **页面**：浏览器实测 概览/历史/批量/视频分析/规则/资源站/资源站规则/AI去广告/M3U8测试/接口选择/去广告监控 全部正常渲染、控制台 0 error。

#### 2. 修复：MD5 特征码分析（[mxadmin.php](file:///workspace/mxadmin.php)）

- **根因**：`aiMd5Analyze()` 读取 `aiSkipSaveMd5`、`aiSkipFastMode` 两个 checkbox 的 `.checked`，但页面根本没有这两个 id（`getElementById(...)` 为 `null` → `TypeError`），而且没有一个按钮能触发该分析；
- **修复**：AI自动去广告页「快捷操作」卡新增「**🔬 MD5特征码分析**」按钮，并补上「**⚡ 极速MD5（采样更少更快）** / **保存MD5特征码入库**」两个开关，MD5 分析功能恢复；配套 `renderMd5Stats()` 等已存在，直接可用；
- **验证**：修复后浏览器打开 ai_skip 页，`aiSkipFastMode`/`aiSkipSaveMd5` 均能正常读取，控制台无 "null" / "is null" 报错。

#### 3. 说明

- `resource_rules/*`（资源站规则）为**数据库模式专属**功能；无 DB（文件模式）时返回「数据库不可用」，属预期设计，需启用 DB 后使用；
- `player/config/save` 前端按顶层字段提交、后端正常，体检时误报为测试载荷问题，已用真实载荷复核为正常。

---

## v5.15.6 (2026-09-07) — 精简资源站·一键屏蔽不可搜索

### 全量检测所有启用资源站的搜索可用性，无法搜索或搜索返回不到结果的站点一键自动屏蔽，只保留可用站

> 针对资源站过多的问题，新增「检测并屏蔽不可搜索」：遍历**全部启用资源站**，用探测关键词逐个真实搜索，凡是无法搜索（接口失败/连不上/失效）或**搜索返回不到任何结果**的站点，自动置为暂停（屏蔽）并记录原因，退出活跃列表，只保留真正可用的资源站，列表随之精简。

#### 1. 全量搜索可用性检测（[ResourceSiteManager.php](file:///workspace/gz/ResourceSiteManager.php) + [DbResourceSiteManager.php](file:///workspace/db/DbResourceSiteManager.php)）

- 两个管理器各新增 `verifySearchCapability($probeKeyword, $maxSites, $limitPerSite)`：遍历全部启用站，逐个调用 `searchVideos` 真实搜索探测；
- **屏蔽条件**：搜索失败（接口不可用/连不上/解析失败）**或**搜索成功但返回 0 条结果——都自动调 `updateSiteStatus(..., 'paused', '自动屏蔽·不可搜索: 原因')`；
- 返回汇总：`checked` / `usable` / `blocked` / `blocked_sites`（被屏蔽站点名）与逐站明细（`usable`、`error`、`auto_blocked`）。

#### 2. 接口 + 后台入口（[mx.php](file:///workspace/mx.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- 新接口 **`sites/search_check`**：支持 `keyword`（探测词，默认高频词「爱情」，绝大多数资源站都有该题材）、`max`（限制检测数量，默认全部）；
- 资源站列表页工具栏新增「**🚫 检测并屏蔽不可搜索**」按钮：确认后逐个探测全部启用资源站，完成后弹窗提示 可用数/检测数/被屏蔽数，并自动刷新列表；被屏蔽站点显示「暂停」，勾选「显示已暂停」可查看并在后台恢复。

#### 3. 验证

- `php -l` 通过：`mx.php` / `mxadmin.php` / `gz/ResourceSiteManager.php` / `db/DbResourceSiteManager.php`；
- `verifySearchCapability` 方法在文件与 DB 管理器均存在；屏蔽时同时写状态与备注原因。

---

## v5.15.5 (2026-09-07) — 资源站优先级统一100 + 自动屏蔽不可搜索

### 资源站列表全部优先级统一为 100（默认 100，越小越优先按优先级排序）；搜索时自动屏蔽不能搜索的资源站

> 本次两项：① **优先级统一 100** —— 资源站列表 122 个站点 priority 全部统一为 100，新增/编辑默认值 100，后台按 priority **升序自动排序**（数字越小越优先），支持手动调低某站数值让其靠前匹配；② **自动屏蔽不可搜索** —— 搜索（searchAllSites）时若某个资源站搜索失败，自动将该站置为暂停（屏蔽），备注记录原因并退出活跃列表，不再反复请求无效站点。

#### 1. 优先级统一 100（[ResourceSiteManager.php](file:///workspace/gz/ResourceSiteManager.php) + [DbResourceSiteManager.php](file:///workspace/db/DbResourceSiteManager.php) + [sites_config.php](file:///workspace/gz/sites_config.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- `sites_config.php` 全部 **122 个站点 priority 统一改为 100**；
- `addSite` 新增默认优先级 **50 → 100**；列表/健康检查排序兜底 **99 → 100**（文件与 DB 两个管理器同步改）；
- 后台「添加资源站」表单优先级默认值 `value=100`（范围扩到 1~2000）、编辑回填/提交默认 `|| 100`；
- 排序规则不变：`getAllSites`（文件 usort）/DB `ORDER BY priority ASC` 均按 priority **升序**（越小越优先）自动排序，后台资源站列表按 priority 展示。

#### 2. 自动屏蔽不可搜索（[ResourceSiteManager.php](file:///workspace/gz/ResourceSiteManager.php) + [DbResourceSiteManager.php](file:///workspace/db/DbResourceSiteManager.php)）

- `searchAllSites` 搜索某站失败时自动调用 `updateSiteStatus(..., 'paused', '自动屏蔽·不可搜索: 原因')` 置为暂停（屏蔽），退出活跃资源站列表不再参与搜索；
- 返回新增 `auto_blocked`（本次屏蔽数）与 `blocked_sites`（被屏蔽站点名列表），每个结果的 `auto_blocked` 标记该站是否本次被屏蔽。

#### 3. 验证

- `sites_config.php` 122 站 priority 唯一值=100；`getAllSites` 98 个活跃全部 priority=100 并按升序；
- `php -l` 通过：`gz/ResourceSiteManager.php` / `db/DbResourceSiteManager.php` / `gz/sites_config.php` / `mxadmin.php`。

---

## v5.15.4 (2026-09-07) — 修复去广告整片误删黑屏

### 统一时长占比过高视为内容节奏，不再判为广告；修复影片被整片占位替换导致"没画面"

> 本次修复：`repetitive-duration` 规则把影片**统一时长（约 4s 一档）的正常码率切片**全量误判为广告，广告占比高达 58.5%，正片被整体替换为黑屏占位 TS，导致 `mxjx` 输出**进度条在走但没画面**（如 `v.lzcdn31.com` 源把 37.8% 的 4.0s 切片全删）。现已加入**内容节奏保护**——某时长桶占比 ≥35% 即为视频自身的统一切片刻度，直接放行。实测广告占比 **58.5% → 10.2%**，仅保留 DISCONTINUITY 边界与真正超短视频。

#### 1. 修复内容（[src/AdRuleEngine.php](file:///workspace/src/AdRuleEngine.php)）

- **根因**：`repetitive-duration` 规则权重 55 ≥ 判定阈值 50，单独命中即删；影片为统一码率输出时大量切片时长落在同一桶（本案例 4.0s 占 **37.8%**），全量触发 → 整片被替换黑屏；
- **修复**：判定前增加保护 —— 当前时长桶占总段数 **≥35%** 时视为内容切片节奏（非广告），直接放行，不再参与 repetitive-duration 判定；
- **效果**（2217 段回归）：广告判定 **58.5% → 10.2%**，正片片段 (con-前 4.0s 切片) 全部保留，仅保留 DISCONTINUITY 边界段与真正超短视频（≤1.5s）。

#### 2. 验证

- 对 v.lzcdn31.com 源（2217 段、37.8% 为 4.0s 统一切片）回归：修复前 AD 58.5%、修复后 10.2%；
- `php -l src/AdRuleEngine.php` 通过。

---

## v5.15.3 (2026-09-07) — 资源站规则自动获取 + 外置播放修复 + 接口选择器

### 输入资源站链接自动抓取分析配置规则（每 2 小时自动同步）；加密/fMP4 流保留 KEY/MAP 修复外置播放无画面；侧边栏新增接口选择（独立/组合调用）

> 本次三个重点：① **资源站规则自动获取** —— 输入资源站采集链接即可自动拉取视频、逐个解析 M3U8 深度分析广告特征，自动汇总 5 类规则写入规则库，每 2 小时自动同步更新，正确率随资源站变化持续优化；② **外置播放无画面修复** —— 根因是加密流（AES-128）与 fMP4/CMAF 流的 `EXT-X-KEY`/`EXT-X-MAP` 标签在去广告输出时被丢弃，播放器解密/解复用失败导致"进度条动但无画面"，现已完整保留并按密钥轮换正确输出；③ **接口选择器** —— 侧边栏可统一选择不同解析接口，独立调用或组合按顺序尝试，默认 `http://域名/api/clean/?url=`。

#### 1. 资源站规则自动获取（[gz/ResourceRuleAutoFetcher.php](file:///workspace/gz/ResourceRuleAutoFetcher.php) + [mx.php](file:///workspace/mx.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- 后台「资源站规则」页新增 **自动获取卡片**：输入资源站采集链接（苹果CMS/通用 `ac=detail` 接口）→ 自动拉取视频列表 → 逐个解析 M3U8 深度分析广告特征 → 自动汇总 **5 类规则**（时长 / 不连续 / 序列 / 文件名 / 关键词）写入 `resource_site_rules`；
- **智能去重**：跨视频累计命中 ≥2 次才写入（避免单次偶然误报），同站同类型同特征已存在自动跳过，二次运行 0 新增；
- **一键同步全部资源站**：遍历启用资源站的 `api_urls` 自动抓取分析更新规则；
- 新接口：`resource_rules/auto_fetch`、`resource_rules/sync`、`resource_rules/sync/status`、`resource_rules/sync/config/save`。

#### 2. 每 2 小时自动同步 + 一键更新维护（[gx.php](file:///workspace/gx.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- 后台打开「资源站规则」页时自动检测：距上次同步 **≥2 小时** 则后台异步触发一次同步（开关与间隔可调，默认 2 小时）；
- `gx.php` 新增 `resource_rules_sync` 任务：可单独执行（`php gx.php resource_rules_sync`），也可并入 `all` 一条龙（自动维护页任务下拉新增第 ⑧ 步）；
- 自动维护页新增「**⚡ 一键更新维护规则**」按钮：直接遍历资源站更新规则并输出到彩色日志，无需 gx 密钥。

#### 3. 外置播放无画面修复（[src/M3U8Parser.php](file:///workspace/src/M3U8Parser.php) + [src/OutputGenerator.php](file:///workspace/src/OutputGenerator.php)）

- **根因**：`#EXT-X-KEY`（AES-128 加密）与 `#EXT-X-MAP`（fMP4/CMAF init segment）此前解析时被忽略、输出时被丢弃，加密/CMAF 流经去广告后播放器无法解密/解复用 → **进度条在走但画面全黑**（外置播放器 APK/VLC 尤其明显）；
- **修复**：解析器逐片段记录 `key`（METHOD/URI/IV/KEYFORMAT）与 `map` 状态；输出器按**密钥轮换**重发 `EXT-X-KEY`、fMP4 流输出 `EXT-X-MAP`、占位黑屏 TS 显式 `METHOD=NONE`（未加密占位不再被旧 key 误解密）；
- 已验证 KEY 轮换（含 `METHOD=NONE` 显式终止）解析→输出往返一致，内置/外置播放器同步受益。

#### 4. 接口选择器（[mx.php](file:///workspace/mx.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- 后台侧边栏「接口工具」新增「**接口选择**」页：全局开关 + **独立调用 / 组合调用** 模式 + 接口列表；
- 接口列表：**自定义清洗接口**（默认 `http://域名/api/clean/?url=`，`{host}` 自动替换为本站地址，http/https 均可）置顶 + 内置 沫兮 / 官解（虾米）/ 官替（资源站匹配）/ 去广告（M3U8）按需启停与排序；
- 开启后 `parse` 等入口按配置顺序逐个尝试，**第一个成功即返回**并标注 `via` 接口与失败尝试明细；页面内置「按当前配置测试调用」；
- 新接口：`api_picker/config`、`api_picker/config/save`、`api_picker/run`。

#### 5. 验证

- `php -l` 全部通过：`mx.php` / `mxadmin.php` / `gx.php` / `gz/ResourceRuleAutoFetcher.php` / `src/M3U8Parser.php` / `src/OutputGenerator.php`；
- KEY 轮换解析输出往返一致（含 METHOD=NONE 显式终止）；规则写入去重二次运行 0 新增、命中过滤正确；
- 接口选择器实测：`single` 模式经「去广告（M3U8）」返回 `play_url` 与 `via` 标记；`api_picker/config` 读写、`resource_rules/sync/status` 正常；
- 前端内联 JS `node --check` 通过。

---

## v5.15.2 (2026-09-07) — 去广告实时监控防误删 + 占位模式 + 更新提示修复

### M3U8 去广告改用等时长黑屏占位不删段（解决卡顿/跳画面）；误删反馈进保护名单自动还原；新增实时监控与监控版本；版本更新提示同版本只弹一次

> 本次重点解决三件事：① **去广告后卡顿/跳画面** —— 广告段不再物理删除，改为等时长黑屏静音占位，时间轴连续不断档；② **误删正片** —— 新增实时监控系统，解析记录留痕、风险识别、人工反馈「误删」后相关片段进入保护名单自动还原，正确率随反馈持续优化；③ **版本更新重复弹窗** —— 同版本「稍后再说」后不再反复提示。

#### 1. 去广告实时监控 + 监控版本（[gz/AdMonitor.php](file:///workspace/gz/AdMonitor.php) + [mx.php](file:///workspace/mx.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- **记录留痕**：`parse_test` 每次解析、`mxjx` 带 `mon=1` 时自动记录（总片段/删除数/广告占比/守护是否触发/命中规则/删除片段索引与 URI 快照），最多保留 200 条；
- **风险识别**：自动标记可疑/高危 —— 广告占比≥50% 可疑、≥70% 高危、全部片段被删高危、删除零散（互不相邻散布全片）可疑、守护触发高危；
- **人工反馈**：后台「去广告监控」页每条记录可反馈「误删 / 正常」：标记误删后，该记录被删片段的 URI 快照进入**保护名单**，命中规则计入误报计数（`rule_fp`，用于持续优化提示）；
- **保护名单**：`parse_test` / `mxjx` 解析时自动加载保护名单，命中受保护 URI 的片段**自动还原保留**，不再重复误删；
- **监控版本**：= 应用版本 + 三位数字计数（如 `v5.15.2.0001`），应用升级自动跟随新版本前缀；后台「重置监控数据」按钮递增计数（0001→0002…）并清空记录/保护名单；
- **接口**：`monitor/status`、`monitor/list`、`monitor/feedback`、`monitor/protected`、`monitor/protected/add`、`monitor/protected/remove`、`monitor/reset` 共 7 个。

#### 2. 占位模式：广告段等时长黑屏占位，不删段（[mx.php](file:///workspace/mx.php) + [player/index.php](file:///workspace/player/index.php)）

- `mxjx` 新增 **`ph=1` 占位模式**：广告段不再从列表中删除，URI 替换为 `mx.php?action=placeholder_ts&d=时长` 的**等时长黑屏静音占位 TS**，片段总数与 EXTINF 时长完全不变；
- `parse_test` 过滤后 M3U8 默认输出占位模式（`placeholder_mode: true`），播放过滤后不再出现删段导致的**卡顿/跳画面/进度回跳**；
- 播放器页与解析测试的播放链接默认带 `ph=1`，无需手动开启；
- 保护名单命中的片段即使被判广告也**跳过占位**（保留原片段）。

#### 3. 防误删保护 + 缓存键修复（[src/M3U8AdSkipper.php](file:///workspace/src/M3U8AdSkipper.php)）

- 新增 `restoreProtectedSegments()`：过滤后按原始顺序重建片段，命中保护名单的被删片段自动还原（返回 `_restoredProtected` 计数）；
- **缓存键修复**：`mxjx` 缓存键加入 `ph`/`mon` 参数，占位/监控模式不命中旧缓存，`mon=1` 确保每次真实记录监控数据。

#### 4. 版本更新提示修复（[mxadmin.php](file:///workspace/mxadmin.php)）

- 更新弹窗「稍后再说」用 `localStorage` 记录已忽略版本（`mxadmin_ignored_update`）；
- 自动弹窗检查时同版本不再弹窗，**新版本发布后自动恢复**。

#### 5. 验证

- `php -l` 全部通过：`mx.php` / `mxadmin.php` / `src/M3U8AdSkipper.php` / `gz/AdMonitor.php` / `player/index.php`；
- AdMonitor 单元验证通过：高危识别（80% 占比→danger）、误删反馈入保护名单（+1）、规则误报计数（ad-uri-pattern×3）、`isProtected` 命中、`reset` 版本递增 0001→0002、统计正确率计算正常。

---

## v5.15.1 (2026-09-07) — 公告正文携带 README 更新内容 + M3U8 对比折叠

### 公告自动读取 README.md 当前版本更新内容作为正文；解析测试原始/过滤后 M3U8 默认折叠

> 公告接口此前只显示「最新版本 vX.Y.Z 发布：<一句话标题>」，本次让公告正文自动携带 README.md「当前版本」章节的完整更新内容；同时优化 M3U8 解析测试页，原始/过滤后 M3U8 长文本默认折叠，不再撑高页面。

#### 1. 公告正文携带 README 更新内容（[mx.php](file:///workspace/mx.php)）

- `announcement/list` 实时生成的最新版本公告，正文自动从 **README.md「当前版本 vX.Y.Z」章节**提取更新内容（截取到第一个「上一版」小标题为止，只保留当前版本）；
- `content` 字段 = `[日期] 标题 + \n\n + README 更新正文`，新增 `readme_content` 字段单独返回 README 正文；日期优先取 README 标题中的 `（YYYY-MM-DD）`；
- `text` 字段保持一句话标题不变，兼容旧版前端。

#### 2. M3U8 原始/过滤后对比折叠（[mxadmin.php](file:///workspace/mxadmin.php)）

- 「原始 M3U8」与「过滤后 M3U8」面板**默认折叠**（大文本不再撑高页面影响美观）；
- 标题栏新增「展开/收起」按钮一键切换；展开后限高 360px 内部滚动；
- 每次重新解析自动重置为折叠态。

#### 3. 验证

- `php -l mx.php / mxadmin.php` 全部通过；
- 本地实测 `announcement/list` 返回完整 README 更新正文（readme_content）；
- 内联 JS `node --check` 全部通过。

---

## v5.15.0 (2026-09-07) — 去插播兜底线路 + M3U8测试播放修复

### 可配置多条兜底清洗线路（默认沫兮兜底 1），官方资源优先走「访问→搜索→跑兜底接口」；解析测试过滤后播放与实时跟随修复

> 设置中新增「去插播兜底线路」，可增删多条接口地址模板；启用后输入官方资源自动先访问获取剧名/剧集、再资源站搜索、用搜索到的链接跑兜底接口返回清洗结果。同时修复 M3U8 解析测试「播放过滤后视频报错图片、片段不能跟随播放实时切换」两个问题。

#### 1. 去插播兜底线路（[mx.php](file:///workspace/mx.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- **设置入口**：后台「沫兮 API」页新增「去插播兜底线路」卡片：全局开关 + 线路列表（名称 / 接口地址模板 / 启停 / 删除 / ➕添加），默认内置「沫兮兜底 1 `https://mxqcb.ssmhd.com/api/clean/?url=`」；
- **配置存储**：DB 模式存 `sys_config`（`fallback_lines`），文件模式存 `gz/fallback_config.php`；新增接口 `fallback/config`（读）与 `fallback/config/save`（写）；
- **解析链路**：启用后输入官方资源（腾讯/爱奇艺/优酷/芒果/B站/搜狐/PP）自动走：
  1. **先访问**：通过 pt 平台适配器（TencentVideo/Iqiyi/Youku/Mgtv/Bilibili/Sohu Adapter）访问官方页面，提取**影视剧名**与**剧集**（失败回退 URL 推断）；
  2. **去搜索**：用剧名在资源站全站搜索，`similar_text` 取最佳匹配片源；
  3. **跑兜底接口**：取搜索到的 m3u8 链接，拼接到兜底线路接口（`接口地址 + url=` 参数）调用清洗；
  4. **返回结果**：兼容 JSON 返回 `url/play_url/m3u8_url` 等字段；业务错误码（如 `code=404 非本站资源`）自动识别并**优雅回退**到原有官替/官解链路；
- **接入入口**：`moxi`（沫兮API）、`parse`（统一解析入口）、`official_replace/resolve`、`official_replace/info` 四条入口全部支持。

#### 2. M3U8 解析测试播放修复（[mxadmin.php](file:///workspace/mxadmin.php)）

- **报错图片修复**：「播放过滤后」不再用片段手拼 M3U8（相对地址在 Blob 场景无法解析 → 报错黑屏），改用后端已生成的**绝对地址 `filtered_m3u8` 文本**播放，保留 `EXT-X-KEY` / `EXT-X-MAP` 等全部标签，加密 / 地图片段同样可播；
- **实时跟随修复**：新增 hls.js `FRAG_CHANGED` 事件**逐片段精确高亮**（比 timeupdate 更准）+ timeupdate 平滑补充；修复重复绑定监听器导致 timeupdate 多次触发的问题。

#### 3. 验证

- `php -l mx.php / mxadmin.php` 全部通过；
- 本地实测：`fallback/config` 读写正常；官方资源兜底链路在网络不通时优雅回退官替；`parse_test` 返回绝对地址与完整过滤文本（原相对 `uri` + 绝对 `absUri` 同时存在）；内联 JS `node --check` 全部通过。

---

## v5.14.9 (2026-09-07) — 沫兮去广告链接播放修复 + 官替优化

### 无广告链接可播放（JSON 守卫修复 + 跨域）、官替相对地址绝对化

> 沫兮 API 去广告完成后返回的 mxjx 无广告链接此前无法播放：最外层 JSON_OUTPUT_GUARD 的 ob 包裹层把 `#EXTM3U` 文本当 JSON 改写，且缺少跨域头。本次彻底修复，并优化官替相对播放地址。

#### 1. mxjx 输出 M3U8 播放修复（[mx.php](file:///workspace/mx.php)）

- **ob 包裹层彻底清理**：`mxjx` 输出去广告 M3U8 前 `while (ob_get_level() > 0) ob_end_clean()` 清掉最外层 JSON_OUTPUT_GUARD 包裹（此前 `ob_clean` 只清最内层，外层把 m3u8 文本改写成 JSON → 无法播放）；缓存命中（X-Cache: HIT）路径同步修复；
- **显式跨域**：输出 M3U8 时携带 `Access-Control-Allow-Origin: *` / Methods / Headers，跨端口、跨子域、跨域名播放不再被浏览器拦截 m3u8 与后续 TS 请求；
- **无硬编码确认**：沫兮 / 官替生成的 `mxjx` 链接 `selfUrl` 全部由 `$_SERVER` 动态推导（协议 + Host + 目录），`url` 参数为传入的真实地址，无硬编码域名 / IP。

#### 2. 官替优化（[db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php) + [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php)）

- 资源站偶尔返回**相对播放地址**（如 `/2026/09/07/xx/index.m3u8`），播放器无法直接播放 → 自动基于视频页域名补全为绝对地址；
- `m3u8_url` 与剧集列表 `all_urls`（数组与字符串两种形态）同步处理；
- 官替返回的 `ad_skip_url` 走 mxjx，直接受益于第 1 项修复。

#### 3. 验证

- `php -l` 全部通过；
- 本地实测 `mxjx`（公开测试流）：`Content-Type: application/vnd.apple.mpegurl`、`Access-Control-Allow-Origin: *`、BODY 为真实 `#EXTM3U` 内容、TS 重写为绝对地址；二次请求缓存 HIT 路径同样正常。

---

## v5.14.8 (2026-09-07) — M3U8测试播放修复 + 片段列表布局 + 侧边栏折叠

### 视频可播放、片段列表显示地址、侧边栏可折叠隐藏

> 修复 M3U8 解析测试页视频播放不了的问题（Chrome/Firefox 原生 video 不支持 HLS），优化片段列表布局，并为侧边栏增加折叠隐藏能力以便让图片/内容显示完整。

#### 1. 视频播放修复（[mxadmin.php](file:///workspace/mxadmin.php)）

- **改用 hls.js 播放 m3u8**：原生 `<video>` 在 Chrome/Firefox 不认 HLS，改用 hls.js 加载播放；
- **多 CDN 兜底**：hls.js 改为 bootcdn → jsdelivr → unpkg 依次尝试自动切换，网络不可达时也能加载；
- **实例生命周期**：停止播放、切换播放模式、重新解析时都先销毁旧 hls 实例再重建，避免内存泄漏与串流。

#### 2. 片段列表布局

- 新增「地址」列（绝对地址，超出省略 + 悬浮显示完整）；
- 固定各列宽，表格横向滚动不挤压内容区；
- 列表容器加边框与圆角。

#### 3. 侧边栏折叠（[mxadmin.php](file:///workspace/mxadmin.php)）

- 顶部栏新增「◧」折叠按钮：折叠后侧边栏收缩为 64px 窄条（只显示图标），主内容区/图片/播放器变宽显示完整；
- 折叠状态 `localStorage` 持久化，刷新后保持；
- 移动端（≤768px）自动隐藏折叠按钮，不影响原移动端横滑菜单。

#### 4. 验证

- `php -l mxadmin.php` 通过；
- 浏览器实测：折叠/展开/刷新持久化正常，M3U8 解析测试页加载无 JS 报错。

---

## v5.14.7 (2026-09-07) — 资源站多地址 + 自动换源

### 新增资源站支持多个采集地址，测速选最优，失败自动切换

> 资源站新增时可直接填写多条采集接口地址，系统抓取/搜索时按顺序自动切换，避免单一地址连接不到导致资源站不可用。

#### 1. 后端（[mx.php](file:///workspace/mx.php) + [db/DbResourceSiteManager.php](file:///workspace/db/DbResourceSiteManager.php) + [gz/ResourceSiteManager.php](file:///workspace/gz/ResourceSiteManager.php)）

- **多地址存储**：资源站新增 `api_urls` 数组字段（数据库版存于 `config` JSON），只填 `api_url` 的旧数据自动兼容回退为单地址；
- **多地址测速**：新增 `sites/test_urls` 接口，逐个检测地址可用性与响应时间，按「健康优先 + 速度升序」排序返回；
- **自动切换**：`fetchVideos` / `searchVideos` 依次尝试所有地址，首个成功即返回（带 `switched_source` 标记），全部失败返回各地址失败明细；
- **健康检测升级**：`checkSiteHealth` 对每个地址分别测速，返回最佳可用地址 `active_url` 与各地址明细 `urls_checked`；
- **调用链打通**：AiAutoLearner / OfficialReplaceManager / DbOfficialReplaceManager / gx.php 全部改为传站点对象搜索抓取，享受多地址自动切换。

#### 2. 前端（[mxadmin.php](file:///workspace/mxadmin.php)）

- 「资源站管理」与「域名发现」两个页面的采集接口表单均改为**多地址列表**（➕添加地址 / ✕删除行）；
- 新增「⚡ 测速排序」按钮：一键检测所有已填地址并**按最快可用源排序**；
- 资源站列表展示「N个源」徽标与主地址，健康检测返回每个地址的测速明细。

#### 3. 验证

- `php -l` 全部通过；
- 本地实测：坏地址 + 备用源自动切换成功（`switched=true`，取回 20 条视频）；`testApiUrls` 测速排序正常（可用源排前）。

---

## v5.14.6 (2026-09-07) — 自动学习失败修复

### M3U8Parser 自动跟随 Master playlist variant

- `src/M3U8Parser.php` `parse()` 检测到 Master playlist（`#EXT-X-STREAM-INF`）且无片段时，自动跟随最高带宽 variant 重新解析媒体流，拿到真实片段列表；
- 此前只解析出 0 片段导致学习链路报「Unsupported operand types: array * int」、自动学习/多线程学习全部失败；
- 保留 `isMaster` / `variants` 元信息，新增 `selectedVariant` / `selectedVariantUri` 字段；已有直接解析媒体流的场景不受影响。

## v5.14.5 (2026-09-07) — 公告实时化

### announcement/list 实时生成「最新版本公告」

- 基于 `version.php` 实时生成首条「最新版本 vX.Y.Z 发布：<变更标题>」公告（`is_latest_version` 标记），不再依赖手工维护的 `gg.txt` 过期问题；
- 本地 `gg.txt` 历史公告仍正常叠加返回。

## v5.14.4 (2026-09-07) — 在线更新源修复 + 健康检测卡死修复

### UpdateManager 更新源由 qcb 更正为 MXGT；修复 sites/health_check 长时间挂起

- `src/UpdateManager.php` 更新源由 `ssmhdssmhd/qcb` 更正为 `ssmhdssmhd/MXGT`，修正在线更新一直指向旧仓库而判定无更新的问题；
- `checkSiteHealth` 的 `$timeout` 真正透传给 `fetchVideos` / `httpGet`（此前 8s 超时参数从未生效）；`batchCheckHealth` 新增总时间预算（默认 20s），后台一键健康检测不再挂起。

## v5.14.3 (2026-09-07) — 接口去重 + API 文档补全

### 删除 notice/* 与 ad_signatures/* 重复别名，补全 API 文档接口索引

- 移除重复别名公告接口 `notice/list`、`notice/save`、`notice/add`、`notice/refresh`（保留规范名 `announcement/*`）；
- 移除重复别名特征码接口 `ad_signatures/*`（保留规范名 `signatures/*`）；
- `api_doc.php` 完整接口索引新增「资源站规则 / AI自动学习 / 公告管理 / 嗅探设置」四分类；
- 保留公有解析别名 `jx`、`parse/parse`、`moxi/api` 避免破坏外部引用。

---

## v5.14.2 (2026-09-07) — 版本标识

### 发布版本号提升至 v5.14.2

- `version_code` 由 51401 提升至 51402，`build` / `commit` 标记为 `v5.14.2-release`；
- 无业务逻辑改动，功能与 v5.14.1 一致。

---

## v5.14.1 (2026-09-07) — 新增功能页

### 后台新增「域名发现」与「资源站规则」两个独立页面

> 侧边栏「资源管理」分组新增两个独立功能页 + 新增一套独立的资源站规则库。

#### 1. 新页面（[mxadmin.php](file:///workspace/mxadmin.php)）

| 页面 | 功能 |
|------|------|
| 🕵️ **域名发现** `page-domain_discovery` | 添加/管理资源站域名与采集接口（名称、官网、采集接口、MacCMS/自定义类型、备注）；编辑、启停、删除、一键健康检测、名称搜索、收录统计 |
| 🗂️ **资源站规则** `page-site_rules` | 独立规则库，按资源站配置 时长/不连续/序列/文件名/关键词 5 类规则；增删改、启停、按站筛选、搜索、一键清空 |

- `handleNavClick` 增加 `domain_discovery` / `site_rules` 触发加载，进入即自动刷新；
- 资源站规则表单的资源站下拉自动填充已有资源站。

#### 2. 后端 独立规则库（[mx.php](file:///workspace/mx.php) + [db/Database.php](file:///workspace/db/Database.php) + schema）

- 新增数据库表 `resource_site_rules`（[schema_sqlite.sql](file:///workspace/db/schema_sqlite.sql) / [schema_mysql.sql](file:///workspace/db/schema_mysql.sql)），与域名规则（`rules_*.php` 文件式）完全独立；
- `Database.php::migrateTables()` 表清单加入 `resource_site_rules`；
- `mx.php` 初始化对老库缺失该表自动补建（`CREATE TABLE IF NOT EXISTS` 幂等）；
- 新增接口组 `resource_rules/*`：`list / get / add / update / delete / toggle / clear`，全部走数据库 + 占位符绑定 + `$useDb` 降级兜底。

#### 3. 验证

- `php -l mxadmin.php` / `mx.php` / `db/Database.php` → 全部 `No syntax errors detected`
- 本地 PHP 内置服务器实测 `resource_rules` 全链路 CRUD（add/get/update/toggle/delete/clear/list + 站点过滤）通过；
- `mxadmin.php` 新页面、菜单、JS 函数渲染正常。

---

## v5.14.0 (2026-09-07) — UI 重构

### 后台整体推倒重写视觉层：深紫→洋红渐变 + 玻璃拟态（Glassmorphism）

> 参照视觉稿对 `mxadmin.php` 后台外观做整体重写（皮肤层），业务逻辑零改动。

#### 1. 视觉层改动（[mxadmin.php](file:///workspace/mxadmin.php) 新增 GlassSkin v6 覆盖 <style>）

| 模块 | 效果 |
|------|------|
| 页面背景 | 浅色 → `#581c87→#7e22ce→#a21caf→#c026d3` 135° 渐变，`background-attachment: fixed` 固定滚动 |
| 侧边栏 `.sidebar` | 半透明白玻璃 `rgba(255,255,255,0.10)` + `backdrop-filter: blur(20px) saturate(160%)`，右缘半透明分隔线 |
| Logo `.sidebar-logo h2` | 白色→浅紫→紫 渐变文字；副标题白色 70% 透明度 |
| 菜单分组 `.menu-group` | 玻璃化 + 悬停紫色柔影；分组标题半透明白字 |
| 菜单项 `.nav-item` | 白字 85% 透明度；激活项 `#a855f7→#d946ef` 紫粉渐变胶囊 + 发光阴影 + 白左边线 |
| 顶部栏 `.header` | 毛玻璃 `blur(18px)` + 底部渐变光带，移除原实色渐变条 |
| 卡片 `.card` | 半透明白玻璃 `blur(16px)` + 圆角 18px + 紫色柔和投影；悬停抬升 |
| 数值卡 `.stat-card` | 同玻璃化 + 左侧色条改六色渐变（success/danger/warning/purple/info/pink） |
| 主按钮 `.btn-primary` | 紫粉渐变 + 紫色光晕 + 悬停增强 |
| 表格表头 `thead th` | 紫→粉渐变白字 |
| 输入框 / Toast | 玻璃化圆角、紫边聚焦、柔和阴影 |

#### 2. 兼容性保证

- 仅新增覆盖式 CSS，**不改动任何页面 DOM 结构与业务 JS**，21 个功能页面全部保留；
- 背景图模式（`body.bg-image-mode`）自动叠加紫色半透明蒙层，保持统一观感。

#### 3. 仓库清理（[.gitignore](file:///workspace/.gitignore)）

新增忽略规则：`.trae-html-share-packages/`、`*.bak`、`test_*`、`_diag*`、`_probe*`、`_e2e*`、`_build*`、`_run_orm*`、`_setup*` 等测试/诊断/备份临时文件，避免污染仓库。

#### 4. 验证

- `php -l mxadmin.php` / `php -l version.php` → 全部 `No syntax errors detected`
- 本地 `php -S` → `GET /mxadmin.php` HTTP 200，GlassSkin 皮肤层与页面结构正常加载

---

## v5.13.3 (2026-08-14) — Hotfix

### 虾米官解替换为新地址 https://jx.xmflv.cc/?url=&ref= + 新增 HTML播放器接口类型 + {url}/{ref}/{origin}/{ts}/{t} 占位符 + Cloudflare WAF 403 兼容

**用户原始诉求**：
> 虾米解析更换 `https://jx.xmflv.cc/?url=&ref=`，帮我修复（之前的 `114.134.184.91:9002` 2026-08-14 已加签名验证，必败）。

---

#### 1. D1 新接口探测定案（重要！与之前旧虾米 API 类型完全不同）

直接 curl 3 种组合命中：
- Case 1 裸UA：HTTP 200，`content-type:text/html`，`<title>虾米播放器-全网最稳定的视频播放器</title>`
- Case 2 Chrome126 UA + `&ref=https://v.youku.com/`：同上 HTML
- Case 3 禁止跳转：HTTP 200，没有重定向
- 8 条传统 JSON 子路径：`api.php / mx.php?action=api/v2 / api/v1 / jx.php / parse.php` 全部 **404**
- HTML 源码：`<div class="Xmflv" id="Xmflv"></div>` + 两段混淆 JS（base64+rot13+gzinflate 的 Xmflv 播放器构造器），无裸 `*.m3u8 / *.mp4` URL。

→ **结论**：`jx.xmflv.cc` 是**「浏览器端 HTML 播放器页面接口」**（播放器自己在前端 runtime 拉流），后端 cURL **无法**再从响应里抽 JSON 的 `play_url` 字段；
正确用法是：**把拼好的整段 URL（`https://jx.xmflv.cc/?url=ENC&ref=ENC`）本身作为最终 play_url 返回给客户端 → 客户端直接 302 跳转或 `<iframe src=>` 打开播放页即可**。

---

#### 2. D3 核心代码 3 项升级（[PerformanceOptimizer.php](file:///workspace/xt/PerformanceOptimizer.php)）

| 功能 | 实现 |
|------|------|
| ① 接口 URL 支持 {占位符} | `buildApiUrl()` 扩展 6 个占位符：`{url}`=`urlencode(原视频页)`、`{ref}` / `{referer}`=`guessPlatformReferer()`、`{origin}`=去掉 path 的 Referer、`{ts}`=秒级时间戳、`{t}`=毫秒时间戳；模板**不含任何 `{`** 时保留旧的「直接后缀拼 urlencode」——老配置 100% 兼容 |
| ② 精准 Referer 推断 | `guessPlatformReferer($videoUrl)`：优酷→`https://v.youku.com/`，爱奇艺→`https://www.iqiyi.com/`，腾讯视频→`https://v.qq.com/`，芒果→`https://www.mgtv.com/`，乐视→`https://www.le.com/`，B站→`https://www.bilibili.com/`，搜狐→`https://tv.sohu.com/`，PPTV→`https://v.pptv.com/`，其它→`scheme://host/`；被 `{ref}` 占位符 + HTTP `Referer` 头两处复用 |
| ③ HTML 播放器页 wrapper 识别（**关键**） | `extractVideoUrl()` 调用 jiami 闭包前前置 4 类命中：<br>1. `api.type ∈ {html_player,page,iframe}` 且响应含 `<!doctype html>`；<br>2. `<title>虾米播放器…</title>` 命中；<br>3. 含 `id="Xmflv"` / `class="Xmflv"`；<br>4. host 含 `xmflv.cc/jmflv/jx.xm*/xmplayer` + Xmflv 签名 JS；<br>命中后**直接把 `buildApiUrl` 拼好的整段 URL 作为 play_url 返回**，不再把不含裸m3u8的 HTML 丢进 jiami 闭包（之前会静默失败）。并发与串行两条链路统一走同一入口。 |

---

#### 3. D4 Cloudflare 403 兼容 + 5 处配置替换 + UA 升级

| 项 | 内容 |
|----|------|
| **请求头自动注入** | `createCurlHandle()` 检测 host 含 `xmflv.cc / jmflv / jx.*` 时自动追加：`Accept: text/html,application/xhtml+xml…`、`Origin: https://jx.xmflv.cc`、`Referer: <按平台>`、`sec-ch-ua` 三件套、`Upgrade-Insecure-Requests:1`（Cloudflare WAF 常见检测项） |
| **UA 默认升级为 Chrome 126** | [config.php](file:///workspace/xt/config.php#L159-L167) `http.user_agent` 从 Chrome 120 → Chrome 126；代码内兜底：若配置仍是空或 `Mozilla/5.0`，自动再替换成 Chrome 126 |
| **requestContextByHandle 上下文存盘** | 新增成员变量：每次 `createCurlHandle` 都把 `{url,api,video_url}` 存进 `$this->requestContextByHandle[(int)$ch]` + 引用备份到 `['last']`，wrapper 无需改 jiami 闭包签名就能拿到「我们请求 jx.xmflv.cc 时拼好的完整 URL」直接作为返回值 |
| **5 处默认配置全部 enabled=true → jx.xmflv.cc** | ✅ [config.php sniffer.official_apis](file:///workspace/xt/config.php#L22-L45)<br>✅ [config.php sniffer.official_api 单接口兼容](file:///workspace/xt/config.php#L46-L59)<br>✅ [config.php 顶层 official_apis fallback](file:///workspace/xt/config.php#L98-L117)<br>✅ [sniffer_config.php official_apis](file:///workspace/xt/sniffer_config.php#L19-L38)<br>✅ [sniffer_config.php official_api 单接口兼容](file:///workspace/xt/sniffer_config.php#L40-L57)<br>统一：`url=https://jx.xmflv.cc/?url={url}&ref={ref} type=html_player + 完整 Chrome126 headers` |
| **mxadmin banner 不变** | 仅对旧失效地址 `114.134.184.91 / :9002` 触发红色告警；`jx.xmflv.cc` 视作正常官解，**不会误弹 banner** |
| **parseVideoByOfficialChannel 完美承接** | 最终 play_url 返回的是整段 `jx.xmflv.cc` 链接（无 `.m3u8` 后缀）→ `parseVideoByOfficialChannel` 走 **else 分支**：直接 `setCache + buildResult(200,'解析成功',$playUrl,…)` 原样返回给客户端；CDN/广告逻辑由 jx.xmflv.cc 官方前端播放器 runtime 处理（和早年 iframe 虾米解析的用法完全一致，后端不介入） |

---

#### 4. 用户端现在怎么用？（20 秒恢复）

> **Option A：全新部署 / 没改过后台默认配置 → 无需任何操作**：
> v5.13.3 默认官解接口已替换为 `jx.xmflv.cc` + enabled=true + `type=html_player`，
> 进入首页直接解析你想要的 URL 即可（play_url 返回整段 jx.xmflv.cc 播放器链接，客户端直接 302/iframe 播放）。

> **Option B：已升级但之前仍存旧 114.134.184.91 配置 → 一键修复仍可复用**：
> 打开 `mxadmin.php → 🔍 嗅探设置` → 若顶部仍弹出红色 banner（说明旧配置还挂着）→ 点 **✅ 一键修复** → 底部保存 → 回到嗅探设置 → 点官解接口卡片 URL 改为 `https://jx.xmflv.cc/?url={url}&ref={ref}`、类型选 **html_player** → 保存即可。

---

#### 5. 回归 / 兼容性

```bash
php -l xt/PerformanceOptimizer.php  # No syntax errors ✅
php -l xt/config.php                # ✅
php -l xt/sniffer_config.php        # ✅
php -l xt/server.php                # ✅
php -l mxadmin.php                  # ✅
php -l version.php                  # ✅（v5.13.3 / version_code=51303）
```

- 对外 JSON API **老字段未改**：`success/code/message/play_url/video_name/debug_info/step_trace` 完全兼容旧客户端；
- 对纯前缀式旧官解 URL（无 `{xxx}` 占位符）→ 行为与 v5.13.2 **100% 一致**；
- `type=html_player` 为 v5.13.3 新增，旧 `type=json/redirect/text` 全部保留逻辑不碰。

---

## v5.13.2 (2026-08-14) — Hotfix

### 虾米官解 api/v2 「验证失败!」根因修复 + 官解静默失败可追溯 + 后台告警横幅一键修复

用户反馈：`mx.php?action=api/v2&type=parse&url=https://v.youku.com/v_show/id_XNjU0MjcxNTM1Ng==.html`
返回：`{"success":false,"code":500,"message":"❌<br>验证失败!","type_name":"虾米解析"}`，首页播放一直转圈，不知道哪里错。

---

#### 1. 根因定案（C1）：**不是我们代码 Bug，是第三方服务器改为签名+白名单验证**

用 5 种组合直接对 114.134.184.91:9002 发 curl：
- ① 裸请求（无UA、无header）
- ② 完整浏览器 UA（Chrome 126，附 Accept/Accept-Language）
- ③ 加 `Referer: https://v.youku.com/`（模拟从优酷跳转）
- ④ 加时间戳参数 `&t=1760860000`
- ⑤ 加 `X-Forwarded-For: 114.134.184.1`

**全部返回同一条**：`{"success":false,"code":500,"message":"❌<br>验证失败!"}`，HTTP 200。

→ 结论：2026-08-14 起虾米官方上游（`114.134.184.91:9002`）对 `api/v2` 接口新增了**签名 + 白名单 IP** 校验，未授权 IP 无论传什么 header/timestamp 都 100% 失败，无法绕过。

> 这解释了之前看到的「HTTP 200 但一直没拿到播放地址」：因为 old PerformanceOptimizer 只看 HTTP status，看到 200 就去提取视频 URL，然后失败时**把 `{success:false,message:"验证失败!"}` 静默当成「无法解析」丢弃**，前端完全看不到 `验证失败!` 这 5 个字的业务错误信息。

---

#### 2. C2 默认配置立即下线已失效的虾米官解（共 5 处 enabled=false）

| 位置 | 修改内容 |
|------|------|
| [config.php](file:///workspace/xt/config.php) → `sniffer.official_apis[]` | enabled=true → **false**，name 改为「虾米官解(已失效，2026-08-14起需签名验证…)」 |
| [config.php](file:///workspace/xt/config.php) → `sniffer.official_api` | enabled=true → **false**（单接口兼容保留字段） |
| [config.php](file:///workspace/xt/config.php) → 顶层 `official_apis[]` fallback 数组 | 整段注释掉改为示例模板「替换为你自己可用的官解接口」，避免后台未启用官解时仍走到必败的服务器 |
| [sniffer_config.php](file:///workspace/xt/sniffer_config.php) → `official_apis[]` | enabled=true → **false** |
| [sniffer_config.php](file:///workspace/xt/sniffer_config.php) → `official_api` | enabled=true → **false**；`update_date` 升级到 `2026-08-14` |

→ 新安装/后台保存后，官解通道默认**不再尝试** `114.134.184.91:9002`，官替本地直调立刻接管，解析速度**反而更快**（官替 URL 留空 = 不走 HTTP 回环）。

---

#### 3. C3 结束官解「静默失败」黑暗期：新增 `recordFailedApi` 全局失败明细

[PerformanceOptimizer.php](file:///workspace/xt/PerformanceOptimizer.php) 新增私有方法 `recordFailedApi($api,$httpCode,$response,$extraReason)`：

| 错误分类 | 识别方式 | 输出字段（写进全局变量） |
|---------|---------|---------------------|
| HTTP 非200 | `curl_getinfo(CURLINFO_HTTP_CODE) !== 200` | `reason="HTTP 502"`、`http_code=502` |
| 空响应/连接失败 | `curl_exec === false / ""`，附 `curl_error` | `reason="空响应(连接超时/上游502/服务器断开)"`、`http_code=0` |
| **业务级错误（本次痛点）** | HTTP 200 + JSON `{success:false, code!=200, status!=1}` | `reason="业务级错误：验证失败!"`、`biz_message="❌<br>验证失败!"`（精准透传上游 message/msg/ZT） |
| HTTP 200 + 业务成功但无视频字段 | `extractVideoUrl()=null` | `reason="HTTP 200 & 业务成功，但视频字段（play_url）为空/格式非法"` |

统一写入：`$GLOBALS['XT_FAILED_API_REQUESTS'][] = {name, url_prefix, http_code, response_len, reason, biz_message, ts_ms}`。

→ 之前的「官解失败，啥也不知道」痛点从此彻底消失，B4 嗅探诊断能把失败原因**逐条列出来**。

**两条请求链路都接入：**
- `callApiSingle()` 串行 fallback：5 类错误分别命中写盘
- `concurrentRaceRequest()` curl_multi 并发：`curl_multi_info_read` 每完成一条即检测 HTTP 码 + JSON success 判断，并发路径失败同样记录，不再是黑盒

---

#### 4. C4 后端 B4 嗅探诊断读出失败明细 + 自动生成修复建议

[server.php](file:///workspace/xt/server.php) `parseVideo` 失败分支读取 C3 写入的 `XT_FAILED_API_REQUESTS`：

```
🕵 嗅探通道诊断（全部失败，点击展开详情）
...
├─ 官解接口失败明细(1 条)：
│    1. 虾米官解 → 业务级错误：验证失败!；HTTP=200；resp_len=209；上游原消息=❌<br>验证失败!
├─ 最终返回通道：嗅探所有通道均未得到有效播放地址（见下）
└─ 修复建议：官解上游返回「验证失败!」，说明此服务器需要签名/白名单，
            未授权IP无法使用 → 切到官替 replace 模式 + 官替URL留空走本地直调即可。
```

同时新增 2 类**自动匹配的精准修复建议**（命中即顶到 `$failMsg` 首行，用户不再看到泛泛的「当前通道未能解析」）：
- 命中「biz_message 包含『验证失败』」 → 直接建议切 replace + 官替 URL 置空
- 命中「http_code=0 连接失败」 → 直接建议取消外部官解启用

`debug_info.sniffer_diagnostic` 和 `step_trace[].detail` 同步新增字段：`failed_api_requests[]`（含 biz_message/ts_ms/http_code 完整结构化数据，旧前端也能读到）。

---

#### 5. C4 后台嗅探设置红色告警 Banner + **一键修复按钮**（已上线用户秒级自愈）

即便用户是 v5.13.1 或更早版本且**仍然手动启用了这条已失效的官解**，升级到 v5.13.2 后打开后台 🔍 嗅探设置会**立即看到**概览卡底部的红色告警：

```
🚨 检测到已失效的「虾米官解 (114.134.184.91:9002)」接口仍处于启用状态

  上游服务器已于 2026-08-14 改为签名/白名单校验，未授权IP直接请求 100% 返回：
  {"success":false,"message":"❌ 验证失败!"}

  一分钟修复方案（推荐方案一，无需额外服务器）：
  1. 保持当前主路由为官替接口（replace）
  2. 取消官解接口的「启用此接口」勾选
  3. 确保官替接口启用 + URL 留空 = 走本地直调（最快）
  4. 点击底部「保存嗅探设置」，首页刷新即消失

  [✅ 一键修复：取消该官解启用 + 官替URL置空 + 切到 replace 主路由]
  [稍后自己改（隐藏此条）]
```

点击**一键修复**后：自动取消 114.134.184.91 的启用勾、切主路由到 replace、官替 URL 清空、标脏 + 滚动高亮 **💾 保存嗅探设置** 按钮 + 5 秒 Toast 说明。

---

#### 6. 对用户的「1 分钟修复操作清单」（不看文档也能做）

1. 进入后台 `mxadmin.php` → 左侧**接口工具 → 🔍 嗅探设置**
2. 若看到顶部红色告警横幅 → 直接点 **「✅ 一键修复」** → 滚到底点 **「💾 保存嗅探设置」**
3. 若没看到横幅（banner 已被之前点过隐藏），也手动做：
   - ① 把「1 官解接口」左上角「启用此接口」取消勾选
   - ② 确保「2 官替接口」启用 + **URL 留空**（留空=走本地直调，比远端官替快 30-70%）
   - ③ ①选择当前解析通道**选「官替接口」(replace)**
   - ④ 点保存
4. 回首页播放页面刷新，之前的「验证失败!」立即消失 ✅

---

#### 7. 回归 & 兼容性

- `php -l` 6 个修改文件：**全部 No syntax errors detected**
  - xt/config.php · xt/sniffer_config.php · xt/PerformanceOptimizer.php · xt/server.php · mxadmin.php · version.php
- 对外 JSON API 字段**完全前后向兼容**：失败 JSON 的老字段（code/message/play_url/video_name…）未改，新增 `debug_info.sniffer_diagnostic.failed_api_requests` 和 `step_trace[].detail.failed_api_requests` 属于附加信息，不影响旧版前端。
- 配置文件向后兼容：`sniffer_config.php` 的字段名和层级完全不变，只是把 `enabled` 默认从 true 改为 false + name 带说明。

---

## v5.13.1 (2026-08-17) — Hotfix

### 嗅探测试「502 Bad Gateway nginx」根因修复 + 报错 UI 美化 + 通道诊断时间线

用户截图（嗅探设置页点击「▶测试解析」→ 返回 502 HTML 整段裸贴）的完整链路与修复：

---

#### 1. 根因还原（双通道双 502 叠加）

```
mxadmin.php 嗅探测试「▶ 测试解析」
  ↓ fetch xt/api.php?url=视频页面
    ↓ parseVideo() 官替直调 callOfficialReplaceDirectV2
      ↓ 平台识别 → 资源站搜索 → AI 标题匹配（CPU密集）
        ↓ 执行时间 > 30s（Nginx fastcgi_read_timeout 默认）
          ↓ Nginx 等不到 FPM → 自己返回 502 Bad Gateway HTML ❌
同时 fallback 里：
  虾米官解 114.134.184.91:9002 也宕机，HTTP 502 ❌（双通道双 502 叠加）
```

**最终前端看到的就是**：`JSON.parse(text)` 失败 → 走老逻辑直接裸贴 502 HTML（整段 `Bad Gateway / nginx`）。

---

#### 2. B2 官替直调「预算时间保护」（xt/server.php parseVideo）

避免 PHP-FPM 长时间执行导致 Nginx 网关直接 502：

| 机制 | 说明 |
|------|------|
| 预算 `$directBudget` | `min(performance.timeout, 25s)`，至少留 5 秒给后续 HTTP fallback 通道 |
| `max_execution_time` 收紧 | 直调前 ini_set 把 FPM 最大执行时间调到 `budget+10s` 区间（不超过 nginx 的 30s） |
| `$GLOBALS['XT_REPLACE_DIRECT_DEADLINE']` | 全局 deadline 注入，OfficialReplaceManager 长循环可读取主动 return |
| 预算 99% 软中断 | 直调结束后若 `elapsed ≥ budget-0.5s` 且未成功 → 立即降级走 HTTP 官替，不再硬扛到 FPM 超时 kill |
| Throwable 全兜底 | 直调任何异常 catch → 写 `replace_direct_fail_reason` → fallback，绝不允许 FPM 崩溃 |

**结果**：后端一定能在 25s 内返回标准 JSON，前端不再看到 nginx 502 HTML。

---

#### 3. B3 非 JSON 报错 UI 全面美化（mxadmin.php testSniffer 分级诊断卡）

**修复前后对比**：

| 维度 | 修复前（截图里的丑态） | 修复后（干净美观） |
|------|----------------------|----------------|
| 错误标题 | 「返回非 JSON：」（灰色小字无情感区分） | 彩色大标题 + **status-pill**：502 红色「阻断级错误」· 504 红色「阻断级错误」· 500 红色「阻断级错误」· 403 橙「需要排查」· 空响应橙「需要排查」 |
| 关键元信息 | 没有 | 右上角：**HTTP 状态码 + 响应字节数** |
| 可能原因 | 全靠用户自己猜 | 502 自动匹配 3 条（官替直调 CPU / 虾米官解宕机 / FPM 满），按概率排序 |
| 修复操作建议 | 完全没有 | 502 自动匹配 3 条（切官替本地直调 / 停掉官解启用 / 运维侧调 fastcgi_read_timeout），直接可执行 |
| 原始 502 HTML | 直接贴 500 字 HTML 占满屏 **用户截图中的痛点** | 默认 **折叠**「📎 查看原始响应（非 JSON）」，点 ▸ 才展开（限 3KB、带滚动条） |

另外支持 6 档自动识别：**502 Bad Gateway / 504 Time-out / 500 PHP Fatal / 403 Forbidden / 空响应 / 其他异常**——不同档匹配不同原因清单 + 操作建议。

---

#### 4. B4 后端「嗅探诊断」时间线条目（失败 JSON 附带人类可读证据链）

所有通道（官替直调 → HTTP 官替 → 官解并发/串行 → fallback 旧数组）全部失败时，自动在 `step_trace` 末尾追加一条：

```
🕵 嗅探通道诊断（全部失败，点击展开详情）← 默认展开因为 status=fail
├─ 当前嗅探模式：官替接口(replace) ✅推荐
├─ 并发竞速模式：关（串行 fallback）
├─ 官解解析接口已启用 1 条：虾米官解→http://114.134.184.91:9002/mx.php?action=ap…
├─ 官替接口：✅启用，未填 URL → 走本地直调 OfficialReplaceManager
├─ 官替直调失败原因：官替直调临近超时(24.8s ≥ 预算25.0s)，已降级走 HTTP 官替
├─ 官替直调用时：24812.3ms（预算 25000ms）
└─ 修复建议：⚠ 检测到配置了虾米官解（114.134.184.91:9002），该服务器当前已宕机 502，
            请取消勾选该官解接口的「启用」改走官替本地直调
```

**宕机服务器自动识别**：B4 逻辑一旦扫到官解接口 URL 里带 `114.134.184.91` 或 `:9002`，就把该条 tip 直接顶到 `$failMsg` 首行返回给前端——而不是之前泛泛的「当前通道未能解析出视频地址」。

---

#### 5. 针对当前用户的「1 分钟修复操作清单」（直接照着点就行）

> 你的情况（截图）最可能是 **虾米官解挂了 + 官替直调 CPU 偶尔超时** 叠加。按下面 3 步操作，立即恢复：
>
> 1. 打开「接口工具 → 嗅探设置」
> 2. 在 **① 选择当前解析通道** 里，勾选 **「官替接口（replace）v5.11 推荐」**（就是截图里选中的那个，保持不动）
> 3. 在 **② 接口详细配置 → 1 官解解析接口** 里，**取消** 右上角「☑ 启用此接口（备用/主路由）」的勾选 —— **这是解决宕机服务器导致 fallback 一直等到 502 的关键**
> 4. ② 下方的 **2 官替接口**，「接口地址（URL 前缀）」**留空不填** → 这样走本地直调 OfficialReplaceManager（嗅探设置里绿色边框写着：留空比 HTTP 回环快 30-70%）
> 5. 点「💾 保存嗅探设置」→ 重新点「▶ 测试解析」，99% 的概率本次就正常了

---

#### 6. 回归验证

- **PHP lint**：`xt/server.php` / `mxadmin.php` / `version.php` → **全部 No syntax errors detected**
- **API 签名 0 改动**：B2/B4 只在 `parseVideo()` 内部新增局部变量和分支，`buildResult` 输出 JSON 的顶层键（code/msg/url/time/...）**完全不变**，旧客户端不会有任何兼容问题
- **新增字段只增不删**：`debug_info.sniffer_diagnostic`、`step_trace[]` 新增的「嗅探诊断条目」，旧前端读不到也会自动忽略

---

## v5.13.0 (2026-08-17)

### 后台全面美化升级：全模块与嗅探设置风格统一，干净美观

> **用户需求**：「后台优化和美化和嗅探设置里面一样，好看，干净美观」——即以用户已经好评的「嗅探设置」页面为视觉基准，把其余 19 个后台页面的组件、配色、排版、说明文案体系全面对齐，做到一处改风格全站生效。

---

#### 1. 设计令牌与通用组件规范（7 大类全站共享）

以嗅探设置页面的视觉风格为基准，新增 7 大类通用组件类，全部写在 `mxadmin.php` 内联 `<style>`：

| 组件类 | 用途 | 视觉特征 |
|--------|------|----------|
| **step-badge** | 步骤编号徽章 | info=蓝 / success=绿 / warning=橙 / danger=红 / primary=主蓝 / purple=紫 6 种背景，白字编号 ①②③ |
| **step-title** | 步骤标题行容器 | 左放 step-badge + 标题，右用 `.section-caption` 灰色小字补说明 |
| **overview-grid / overview-item** | 页面顶部「概览双栅格」 | 默认两列 `primary/success/warning/info/danger/purple` 6 色标题条，内部分 `overview-title`（加粗大标题）+ `overview-desc`（说明文案），窄屏自动单列 |
| **form-grid / inline-form-grid** | 表单双栅格 | 统一 `auto-fit + minmax` 响应式，inline 栅格每个字段独立 form-tip 小字号灰文说明 |
| **sub-card / sub-card-header** | 子分组卡片 | 灰描边 8px 圆角，header 带字母徽章(A/B/C) + 子组标题，可把页面大模块再细拆 N 个小组 |
| **action-bar (tight / with-top)** | 操作按钮栏 | flex-wrap 自动换行，gap=12px，tight=无顶间距，with-top=带上边距分隔线视觉 |
| **status-pill** + **form-tip / section-caption** | 状态徽章 & 说明文案 | pill 彩色圆角小标签(如「频繁更新规则专用」)；form-tip=表单底部提示；section-caption=卡片标题右侧灰色副说明 |

**CSS 设计令牌（CSS 变量）**：

```css
--primary/#409eff  --success/#67c23a  --warning/#e6a23c
--danger/#f56c6c   --info/#909399     --purple/#8b5cf6
--border-base:#dcdfe6  --radius-sm:4px/base:8px/lg:12px
--shadow-sm/base/lg    --text-primary/regular/secondary
```

---

#### 2. 覆盖范围：19 个后台页面全部升级（4 批完成）

| 批次 | 页面 | 主要美化点 |
|------|------|-----------|
| **A4-1** | `page-history` 播放记录 | 概览卡（概览双栅）+ 表格样式 + 批量操作 action-bar |
| 同上 | `page-batch` 批量解析 | 概览卡 + URL 输入区 + 处理选项 sub-card + 进度面板 |
| 同上 | `page-analyze` 视频广告分析 | 概览卡 + 6 项参数 inline-form-grid + 结果 6 色 overview-item 指标卡 |
| **A4-2** | `page-rules` 规则管理 | 概览卡 + 筛选区 sub-card + action-bar 批量按钮 + 规则表格 |
| 同上 | `page-sites` 资源站 | 概览卡 + 批量巡检 sub-card + 新增表单 inline-grid + 资源站表格 |
| 同上 | `page-ai_autolearn` AI 自动学习 | 概览双栅（primary+warning）+ 基础开关/样本过滤/资源站/附加选项 4 个 sub-card |
| 同上 | `page-official_sites` 官方资源站 | 概览卡 + 推荐站 sub-card + 参数配置 inline-form-grid |
| **A4-3** | `page-official_replace` 官替解析 | 概览卡 + 状态统计 sub-card + 核心参数/支持平台/API测试/在线播放/接口文档 7 个编号模块 |
| 同上 | `page-moxi_api` 魔西 API | 概览卡 + 字段说明 sub-card + 多模式测试 sub-card 带 step-badge |
| 同上 | `page-play` 播放器 | 概览卡 + 内核参数 sub-card + 播放测试带编号步骤 |
| 同上 | `page-database` 数据库 | 状态 + 表结构检查 + 配置 + 迁移 4 大编号模块 |
| 同上 | `page-update` 系统更新 | 版本信息/服务器/权限/缓存清理等 8 个运维卡片结构化 |
| 同上 | `page-autoupdate` 自动维护 | 概览卡（紫色渐变主色 pill）+ 任务参数 + 8 步详情 sub-card + 日志面板 |
| **A4-4** | `page-announcement` 公告管理 | 概览双栅 + 操作面板 + 公告源优先级 sub-card 列表 |
| 同上 | `page-auth` 授权中心 | 概览卡 + 4 指标 stats-grid + 本地/远程详情两个 sub-card + 授权配置 inline-form-grid |
| 同上 | `page-ai_skip` AI 智能去广告 | **保留紫色渐变横幅**，外加 7 个编号模块 + 开关栅格 sub-card + 结果链接对比 sub-card |
| 同上 | `page-ai_insert` AI 插播识别 | **保留粉紫渐变横幅**，4 个编号模块，5 类开关栅格化 toggle-label |
| 同上 | `page-ai_subtitle` 滚动字幕 | **保留青绿渐变横幅**，6 项指标用 6 色 overview-item，示例链接带 pill 边框样式 |
| 同上 | `page-ai_watermark` 水印处理 | **保留蓝青渐变横幅**，净化前后链接两张带 B/C 徽章 sub-card 并排对比 |

**关键设计决策：AI 四大模块保留品牌色渐变横幅** —— ai_skip（紫）/ ai_insert（粉紫）/ ai_subtitle（青绿）/ ai_watermark（蓝青）的渐变色信息横幅原本写在 `<div style="background:linear-gradient(...">`，我们不删除不替换，在外层统一叠加上「概览卡 → step-title → sub-card」统一骨架，既保留各模块辨识度又整体风格对齐，兼顾品牌与统一。

---

#### 3. 全面移除散乱内联样式，改一处全局生效

原先大量散乱写法：
```html
<div style="display:flex;gap:12px;margin-bottom:16px">
<p style="color:#606266;font-size:13px;margin-bottom:12px">
<label style="display:flex;align-items:center;gap:6px;color:#606266">
```
统一替换为：
```html
<div class="action-bar tight">                 ← flex+gap+换行
<div class="form-tip">                          ← 灰文小字说明
<label class="toggle-label">                    ← flex+gap+鼠标手型
```

**收益**：后续想要「所有按钮间距从 12px 调为 16px」或「form-tip 颜色改深一点」，只需要改一个 `.action-bar` 或 `.form-tip` 的 CSS 规则，全站 19 个页面同时生效，不再需要全局 grep 逐个改内联。

---

#### 4. 说明文案全面人性化，降低文档依赖

- 每个页面顶部放 **overview-grid 概览双栅**：左格写「模块怎么用/推荐参数」，右格写「常见坑/最佳实践」
- 每个表单字段下方放 **form-tip**：例如「授权服务器 IP」字段 tip 写「远程验证要连接的服务器地址，一般由授权方提供」，不用点进文档找
- 每个卡片标题右侧 **section-caption**：如「复制 / 新窗口 / 内置播放 / 下载 4 种入口」，一眼告知此模块可以做什么

---

#### 5. 回归验证

- **PHP lint**：`php -l mxadmin.php` → `No syntax errors detected`，0 Notice 0 Warning
- **零逻辑改动**：修改纯为 HTML 结构重排 + class 替换 + 少量 CSS 变量新增，不修改任何 `onclick=` 的函数名、不增删 JS 变量、不改后端接口调用，所有原 API 行为保持不变
- **响应式烟雾检查**：`@media (max-width: 768px)` 下 overview-grid / inline-form-grid 全部退化单列，移动端排版不乱

---

## v5.12.0 (2026-08-16)

### 6平台独立元数据解析器(策略模式) + 极简提取减轻服务器负担

#### 设计目标（用户需求）

> **用户原话**：完善剩下的各个平台，链接获取影视剧名和集数的方式，就行只需要获取到影视剧名和集数就好，其他不要，减轻服务器负担，剧名和去替换和集数，去非正片内容输出，确保无广告无插播等等影响观感的内容和不雅内容

**对应三项改造**：
1. **平台策略模式拆分**：fetchMeta_Youku/Tencent/Iqiyi/Mgtv/Bilibili/Generic — 6 个独立方法，各自维护
2. **极简提取 = 只取两个字段**：base_title(剧名) + episode_num(集数)，其余字段全空=归零内存负担
3. **非正片占位**：基于 v5.11 的 MD5 + 黑屏静音 TS 占位流程不变，广告段等时长占位不删除 → 不中断

---

#### 1. 通用提取引擎 `_extractQuickBaseAndEpisode`

三层提取优先级（从高到低，命中即 break，避免不必要的 preg 回溯）：

| 层级 | 来源 | 说明 |
|------|------|------|
| ① | **内联 JS 对象字面量/JSON** | 只扫 HTML 前 **260KB**（优酷/腾讯/B站 等平台的 usercfg/__NEXT_DATA__/__INITIAL_STATE__ 都在 head 里），按平台传入的 `inlineKeys` 顺序逐项试，长度 2-30 字符 + banWords 过滤即取 |
| ② | **meta 标签** | `og:video:series_name` / `tv:series_name` / `og:title` 等平台常见 property，正反两种属性顺序（property+content / content+property）都覆盖 |
| ③ | **og:title + \<title\> 兜底** | 从 title 截取 "第X集/话/期/部/季" **之前**的文本作为剧名，平台后缀(优酷/爱奇艺/腾讯视频/芒果tv/哔哩哔哩/bilibili/b站)和分类后缀(在线观看/高清/电视剧/电影/综艺/动漫/纪录片) 自动去除 |

**内联字段支持两种写法**（自动分发）：
- **扁平字段**：`"showName": "九门"` / `showName: "九门"`（裸键名，腾讯/B站/芒果对象字面量常见）
- **嵌套对象**：`partOfSeries: { name: "狂飙" }`（腾讯 ld+json schema.org 标准写法，regex 处理 `.name` 后缀自动取父对象 + name 字段）

**剧名强清洗 pipeline**：
```
raw_value → html_entity_decode(ENT_QUOTES|ENT_SUBSTITUTE UTF-8)
        → trim( \t\n\r《》<>"' )          // 书名号/引号全部脱壳
        → 去平台后缀( - 优酷 / _腾讯视频 / | 爱奇艺 ...)
        → 去分类后缀( -在线观看 / - 高清 / ...)
        → mb_strtolower banWords 黑名单比对（预告/花絮/速看等不雅/噪声词）
        → 长度校验 2~30 字符（电影兜底放宽到 40）
```

**集数多格式识别（从 og:title + \<title\> 拼接成 320 字符以内短文本，只扫一次）**：
- `第\s*\d+\s*[集话期部季]` → 中文写法（覆盖九门/莲花楼）
- `\bEP\s*\d+\b` → 欧美番剧 EPXX（i 大小写不敏感）
- `(\d{1,3})\s*/\s*\d{1,3}` → "2 / 24 全" 斜杠分数式（芒果常见）

---

#### 2. 各平台独立解析器的差异化字段策略

> **好维护 = 每个平台的优先字段写在自己的方法里，改一个平台不影响其他**

| 平台 | 方法 | 优先字段（inlineKeys） | 兜底 meta | 示例 | 正确 base | 正确 ep |
|------|------|----------------------|----------|------|-----------|---------|
| 🟡 **优酷** | `fetchMeta_Youku` | `usercfg.showName` → `videoShowName` → `albumName`（内联 JS 变量） | og:video:series_name | `九门 第2集 张启山和吴老狗达成合作` | **九门**（不会被副标题污染） | 2 |
| 🟢 **腾讯** | `fetchMeta_Tencent` | ld+json `partOfSeries.name`(schema.org 最稳) → `__NEXT_DATA__.seriesInfo.seriesName` → `seriesName` | tv:series_name | `狂飙 第39集 高启强终极对决` | **狂飙** | 39 |
| 🟢 **爱奇艺** | `fetchMeta_Iqiyi` | `og:video:series_name` meta(官方元数据) → `Q.playerInfo.albumName/seriesName/tvName` | og:video:series_name | `莲花楼 第20集 李莲花识破阴谋` | **莲花楼** | 20 |
| 🟠 **芒果TV** | `fetchMeta_Mgtv` | `__INIT__.showInfo.showName/seriesName/partOfSeries.name` | og:title | `乘风2024 第12期 成团夜` | **乘风2024** | 12 |
| 🔵 **B站番剧** | `fetchMeta_Bilibili` | `__INITIAL_STATE__.mediaInfo.season.title/seasonName/partOfSeries.name`（完整系列名） | og:title | `咒术回战 第二季 第24话 怀玉` | **咒术回战 第二季**（保留"第二季"利于资源站搜索） | 24 |
| ⚪ **B站UGC** | `fetchMeta_Bilibili` | `__INITIAL_STATE__.videoData.title`（完整保留括号信息） | og:title | `迈克杰克逊1995年MTV颁奖典礼现场(4K修复)` | **迈克杰克逊1995年MTV颁奖典礼现场(4K修复)** | null（UGC无集数概念） |
| ⬜ **通用兜底** | `fetchMeta_Generic` | showName/seriesName/albumName/partOfSeries.name 等所有平台常见字段并集 | og:title | `庆余年第二季 第36集 范闲回京` | **庆余年第二季** | 36 |

**关键设计决策**：
- 优酷故意**不用 og:title 作为 base**（因为官方 og:title 会写成 "九门 第2集 张启山和吴老狗达成合作"，如果通用逻辑截断"第2集"之前文本恰好没问题，但遇到类似"大江大河之岁月如歌 第1集"这种剧名本身带"之"的写法容易误截断），改为强制从 `usercfg.showName` 这个官方 API 级字段取值，此值就是纯剧名，零污染。
- B站番剧故意**保留 "第二季"**（不用 `partOfSeries.name` 的"咒术回战"）。原因：资源站常把"咒术回战"和"咒术回战 第二季"作为两个独立条目入库，去掉季信息反而匹配不上。
- B站UGC故意**不取集数**、**保留括号修饰词**（(4K修复)/(完整版)）。原因：UGC 视频本身就是单条视频，无"集"的概念；资源站转载时通常保留完整标题含修饰词，去括号反而搜不到。

---

#### 3. 轻负担验证（其余字段全空 = 内存归零）

每个 `fetchMeta_*` 返回的 9 个字段中，只填 2 个：

| 字段 | 值 | 原用途（现已停用） | 节省 |
|------|----|-------------------|------|
| `base_title` | ✅ 真实剧名（如"九门"） | - | 必须保留 |
| `episode_num` | ✅ int/null（如 2） | - | 必须保留 |
| `episode_name` | `''`（空串） | 分剧名/副标题（"张启山和吴老狗达成合作"） | 节省长字符串内存 |
| `subtitle_guess` | `''`（空串） | 猜测字幕 | 归零 |
| `cover` | `''`（空串） | 封面图 URL | 归零（无需爬 og:image） |
| `description` | `''`（空串） | 剧情简介，可能长达 200-1000 字 | **最大节省项** |
| `total_episodes` | `null` | 总集数（24/36 全） | 归零 |
| `raw_title` | `''`（空串） | 原始 og:title 备份 | 归零 |
| `hits` | `[]`（空数组） | 候选命中大对象（可能塞几十个搜索结果引用） | **第二大节省项** |

**Mock 测试断言（`_test_6platforms_fetchMeta.php`）：**
```
  [1/7] Youku                      | base:'九门' ✓ | ep:2      ✓ | 轻负担:✓
  [2/7] Tencent                    | base:'狂飙' ✓ | ep:39     ✓ | 轻负担:✓
  [3/7] Iqiyi                      | base:'莲花楼' ✓ | ep:20     ✓ | 轻负担:✓
  [4/7] Mgtv                       | base:'乘风2024' ✓ | ep:12     ✓ | 轻负担:✓
  [5/7] Bilibili-Bangumi           | base:'咒术回战 第二季' ✓ | ep:24     ✓ | 轻负担:✓
  [6/7] Bilibili-UGC               | base:'迈克杰克逊1995年MTV颁奖典礼现场(4K修复)' ✓ | ep:NULL   ✓ | 轻负担:✓
  [7/7] Generic-unknown-site       | base:'庆余年第二季' ✓ | ep:36     ✓ | 轻负担:✓

✅ 7 / 7 平台全部断言通过（base_title+episode_num 双正确 + 其余字段为空=减轻负担生效）
```

---

#### 4. Step Trace 调试可视化（mxadmin.php 嗅探测试区）

失败时现在能看到是**哪一步**出问题（时间线 UI，✓成功 / △警告 / ✕失败 / ℹ信息 四色标记）：
```
🕒 解析时间线（共 8 步）
  ✓ 平台识别        (12ms)  youku / id=XNjU0MjcxNTM1Ng==
  ✓ 官方页面抓取    (238ms) HTTP 200 / 184KB
  ✓ 元数据提取      (5ms)   base_title=九门 episode_num=2
  ℹ 资源站搜索      (412ms) 已搜 5 个站点 / 命中 3 个候选
  △ AI 匹配         (18ms)  分数 68（刚过阈值 65，建议降低阈值或添加资源站）
  ✓ 集数定位        (3ms)   第2集 / 匹配成功
  ✕ 去广告处理      (2800ms) m3u8 下载超时 → 建议检查代理或换源
  …
```

对应 `buildResult($extras=[])` 和 `callOfficialReplaceDirectV2()` 都已同步透传 `step_trace` 字段，HTTP 直调和本地直调都能看到。

---

#### 5. 非正片占位（不中断观感）

流程不变（v5.11 已上线）：**广告段 URI 替换为 `mx.php?action=placeholder_ts&d=X.X`（本地黑屏静音 TS），EXTINF 时长不变，段数不减少**。

保证：
- 播放器进度条不回跳/不卡住（TARGETDURATION 与段数一致）
- 解码器不中断（连续 10 段黑屏也正常通过，无需重新缓冲）
- 无广告/无插播/无不雅内容（原广告段被静音黑屏占位替代，视觉和听觉都感知不到内容）

---

## v5.10.9 (2026-08-13)

### P0 解析失败根因修复 + 官替优先智能识别新架构

#### 问题复现（用户反馈）

访问：`http://114.134.184.91:9002/jiexi.php?url=https://v.youku.com/v_show/id_XNjU0MjcxNTM1Ng==.html`

返回：
```json
{"code":200,"ZT":"解析成功","url":"https://v.youku.com/v_show/id_XNjU0MjcxNTM1Ng==.html","msg":"https://v.youku.com/...","time":"0s","KFZ":"超级嗅探|XT"}
```

**现象：** `code=200` + `ZT=解析成功`，但返回的 `url` 与输入的优酷视频页面地址**完全相同**，导致播放器接收到一个 HTML 页面链接而不是 `.m3u8/.mp4` 视频流，**无法播放**。

---

#### 根因分析（original_url 陷阱）

1. **虾米官解接口验证失败**：`parse_internal_xiami()` 调用的远程加密接口返回 `{"success":false,"code":500,"message":"验证失败!","original_url":"https://v.youku.com/...","play_url":""}` — 签名/加密机制已失效或服务端校验不通过

2. **findUrlInArray 递归误提取**：兜底的 `findUrlInArray($data)` 递归遍历 JSON 所有字段，找到 `original_url` 字段：其值 `https://v.youku.com/...` 满足 `filter_var(..., FILTER_VALIDATE_URL)` 为真 → **错误地把原始页面地址当作视频流地址返回**

3. **parseVideo 链条未校验**：
   - `$videoLink = "https://v.youku.com/..."` 非空不报错
   - 进入 `parseVideoByOfficialChannel()` → 不是 `.m3u8` → 直接走 `setCache + buildResult(200, 解析成功, $videoLink)`
   - → **code=200 假成功，实际是原始 HTML 页面 URL**

4. **官替通道默认关闭**：`sniffer.replace_api.enabled=false`，无法 fallback 到更可靠的资源站匹配

---

#### 修复方案（6 处代码同步加固）

**核心守卫：isSafeVideoUrl 三层校验模型**

| 层级 | 规则 | 作用 |
|------|------|------|
| 第一层 | `$candidate !== $videoUrl`（严格不等 + 末尾 / 归一化） | 完全阻止返回原始 URL |
| 第二层 | 同域名（优酷/腾讯等）必须以 `.m3u8/.mp4/.mkv/.flv/.avi/.ts/.webm` 等视频扩展名结尾 | 阻止原视频站点 HTML 页面 |
| 第三层 | 兜底递归扫描前排除 20+ 非视频字段 | `original_url/source_url/input_url/referer/page_url/msg/logo_url/pic/cover` 全部跳过 |

**修改文件清单：**

| 文件 | 修改 |
|------|------|
| `xt/server.php::findUrlInArray()` | 新增 $excludeDomainPattern 参数 + excludeKeys 黑名单 + videoExtPattern 扩展名强校验 |
| `xt/server.php::getVideoLinkFromApiEntry()` | 新增 isSafeVideoUrl 三层守卫；json 字段分 4 级信任度（url_field/success=true 字段 allowProxy，通用兜底 allowProxy=false）；调用 findUrlInArray 传入排除域名 |
| `xt/server.php::callSingleApi()` | 检测本地官替（URL 含 official_replace/info 或 name 含"官替"）→ 直接调 callOfficialReplaceDirect()，避免 HTTP 自请求 + original_url 陷阱 |
| `xt/server.php::callOfficialReplaceDirect()` | 【新函数】本地官替直调 OfficialReplaceManager，流程：识别平台→资源站搜索→AI 匹配→mxjx/deep 去广告代理→输出无广告 URL |
| `xt/PerformanceOptimizer::extractVideoUrl()` | 相同 isSafeVideoUrl 加固；findUrlInArray 传排除域名；新增 3 参签名（传 $videoUrl 进来） |
| `xt/PerformanceOptimizer::findUrlInArray()` | 同 server.php 加固；concurrentRace / callApiSingle 均传 videoUrl |
| `xt/PerformanceOptimizer::callApiSingle` / concurrentRace 内 extractVideoUrl 调用 | 新增第 3 参传当前 $videoUrl |
| `xt/config.php` | sniffer.mode=replace（官替优先）；replace_api.enabled=true；replace_api.url_field=ad_skip_url（优先取已去广告代理地址） |
| `gz/OfficialReplaceManager::getDefaultConfig()` | default_site=抖剧TV；match_threshold=75→65；platforms 新增 360kan.com（抖剧TV根源来源）；search_sites 首位=抖剧TV |
| `version.php` | v5.10.8→v5.10.9，build 20260810→20260813，新增完整 changelog |
| `README.md` | 新增分支说明 + v5.10.9 失败原因/修复链路 + 官替优先架构说明 |
| `CHANGELOG.md` | 新增 v5.10.9 完整问题根因 + 修复方案记录 |

---

#### 架构升级：官替优先智能识别流程

```
用户请求 jiexi.php?url=优酷/腾讯/爱奇艺...
   │
   ▼
[1] 平台识别 + 视频ID/标题提取（OfficialReplaceManager::resolve）
   │  detectPlatform → extractVideoId → fetchVideoInfo
   │
   ▼
[2] 资源站并发搜索（默认首站：抖剧TV → 量子 → 暴风 → ...）
   │  searchInSites(search_sites 数组) 多策略 ac=list/videolist/detail/...
   │
   ▼
[3] AI+规则智能匹配最佳视频
   │  AiSmartProcessor + TitleNormalizer + PtManager 交叉验证
   │  - 剧名/季/集匹配（阈值 65）
   │  - 年份 + 演员交叉加分
   │  - 非正片正则排除（预告/花絮/速看/解说/饭制 50+ 模式）
   │
   ▼
[4] 目标集数定位 + 去广告代理组装
   │  findEpisodeUrl → buildAdSkipUrl → mxjx/deep=1
   │
   ▼
[5] M3U8 下载 → 规则引擎 + AI 广告识别 → 去插播/去水印 → clean 输出
   │  AdFilter::process() 结合 EnhancedAdRuleEngine + ProfessionalAdDetector
   │
   ▼
最终输出：无广告、无插播、无水印的清洁 m3u8 播放链接
```

---

#### 验证

**Bug Fix 单元测试（PHP CLI）：**
```
Old behavior would pick: https://v.youku.com/v_show/id_XNjU0MjcxNTM1Ng==.html (WRONG)
NEW findUrlInArray result: NULL (CORRECT - no valid video URL in error response)
✅ PASS: original_url trap now correctly avoided
✅ PASS: correctly identifies local replace API
```

---

## v5.9.7 (2026-08-03)

### Bug 修复 - exec 被禁用时 AI 学习报错

#### 问题

- v5.9.6 改为异步执行后，部分服务器报错：`Call to undefined function exec()`
- 原因：服务器 `disable_functions` 禁用了 `exec()` / `popen()` 等进程控制函数

#### 修复 - 三层回退触发策略

重写 [AiAutoLearner.php#L844-L892](file:///workspace/gz/AiAutoLearner.php#L844-L892) `triggerBackgroundRunAsync()`：

1. **策略 1：exec()**（优先）
   - 检测 `function_exists('exec')` 且不在 `disable_functions` 列表中
   - 通过则 `exec('php cron_ai_autolearn.php force > /dev/null 2>&1 &')` 非阻塞
2. **策略 2：fsockopen 非阻塞 HTTP**（exec 不可用时回退）
   - 新增 [asyncHttpViaFsockopen()](file:///workspace/gz/AiAutoLearner.php#L901-L939)
   - 通过 `stream_socket_client` + `STREAM_CLIENT_ASYNC_CONNECT` 建立非阻塞连接
   - 发送 HTTP 请求后立即关闭 socket，**不等待响应**
   - 支持 HTTPS（SSL 协议）
3. **策略 3：curl 短超时 HTTP**（兜底）
   - 1 秒超时的 curl 请求（可能略阻塞但能工作）

#### 验证

模拟禁用 exec 测试：
```
$ php -d disable_functions=exec,popen -r "...triggerBackgroundRunAsync..."
exec 是否可用: NO
触发结果: {"method":"fsockopen","url":"http://ssmhd.com/cron_ai_autolearn.php?...","success":true}
```

#### 修改文件

| 文件 | 修改 |
|------|------|
| `gz/AiAutoLearner.php` | 重写 `triggerBackgroundRunAsync()`：exec 检测+三层回退；新增 `asyncHttpViaFsockopen()` |
| `mxadmin.php` | 公告列表添加 v5.9.7 |
| `version.php` | 版本号升级到 v5.9.7 |
| `CHANGELOG.md` | 记录 exec 禁用修复 |

## v5.9.6 (2026-08-03)

### Bug 修复 - AI 自动学习 502 Bad Gateway

#### 问题

- 点击「执行 AI 自动学习」报错 `502 Bad Gateway`（nginx 返回 HTML 错误页）
- 前端报：`❌ 服务器返回非JSON: <html><head><title>502 Bad Gateway</title>...</html>`

#### 原因

- `ai_autolearn/run` 同步阻塞执行学习（耗时 1-3 分钟）
- PHP-FPM `request_terminate_timeout` 或 nginx `fastcgi_read_timeout` 默认 60s 触发
- 后端被强杀 → nginx 返回 502

#### 修复 - 改为异步执行模式

- **后端** [mx.php#L3494-L3540](file:///workspace/mx.php#L3494-L3540) `ai_autolearn/run` 端点改造：
  - 立即返回 `{success:true, async:true, message:"已提交后台执行"}`
  - 通过 `triggerBackgroundRunAsync()` exec 非阻塞启动 `cron_ai_autolearn.php force`
  - 锁文件检测：已有任务运行时返回 `already_running:true`，避免重复触发
- **新增** [AiAutoLearner.php#L840-L871](file:///workspace/gz/AiAutoLearner.php#L840-L871) `triggerBackgroundRunAsync($options)`：
  - 主路径：`exec('php cron_ai_autolearn.php force > /dev/null 2>&1 &')` 非阻塞
  - 回退：异步 HTTP 触发 `cron_ai_autolearn.php?key=xxx&force=1`（1s 超时）
- **前端** [mxadmin.php runAiAutoLearn()](file:///workspace/mxadmin.php#L8731-L8856) 重写为两阶段：
  1. 提交任务（立即返回）→ 显示「任务已提交后台，开始监控进度...」
  2. 轮询日志（每 1 秒）→ 检测「完成/异常」关键字自动停止，最多轮询 5 分钟
  - 进度条根据日志变化动态推进（日志变化时 +3%，否则 +0.5%）
  - 完成后展示最近 10 条日志 + 「查看完整日志」按钮

#### 验证

- 接口响应耗时 < 5ms（之前会等到学习完成或 502）
- 返回 JSON：`{"success":true,"async":true,"triggered":{"method":"exec","success":true}}`
- 后台进程独立运行，不受 nginx/PHP-FPM 超时限制

#### 修改文件

| 文件 | 修改 |
|------|------|
| `mx.php` | `ai_autolearn/run` 改为异步：立即返回+锁检测+后台 exec 触发 |
| `gz/AiAutoLearner.php` | 新增 `triggerBackgroundRunAsync()`、`asyncHttpTriggerRaw()`；`updateLastRunTime()` 改为 public |
| `mxadmin.php` | `runAiAutoLearn()` 重写为异步两阶段：提交+日志轮询 |
| `version.php` | 版本号升级到 v5.9.6 |
| `CHANGELOG.md` | 记录 502 修复内容 |

## v5.9.5 (2026-08-03)

### UI 增强 - 所有「请稍后/加载中」提示升级为进度条

#### 新增通用进度条组件

- **CSS 组件**：`mxadmin.php` 新增 `.loading-wrapper` / `.progress-bar-container` / `.progress-bar-fill` / `.progress-bar-text`
  - **确定进度条**：传入 `total/current`，显示百分比 X % (N/Total)
  - **不确定进度条（动画）**：不传 `total`，使用 `@keyframes progress-indeterminate` 流动动画（蓝→绿渐变条）
- **JS 工具**：`showLoadingWithProgress(container, opts)` 返回 `update(state)` 闭包
  - `opts.label/extraText/total/current`：初始化
  - `update({ label, current, total, extraText, done })`：动态更新，`done=true` 清空容器

| 改造点 | 进度类型 | 说明 |
|--------|---------|------|
| `runAiAutoLearn()` AI 自动学习执行 | 确定进度 (N = videos_per_site * max_sites) | 1% / 秒渐进增长至 85%，完成后 100% |
| `runAutoLearn()` 自动学习执行 | 确定进度 (N = videos_per_site * max_sites) | 同上 |
| `batchLearnAll()` / `batchLearnFrontend()` 批量学习 | 确定进度 (N = videos.length) | 前端每完成 1 条实时更新进度+成功/失败计数 |
| `batchAnalyzeAll()` 批量分析 | 确定进度 (N = videos.length) | 后端批次渐进增长模拟，完成补 100% |
| `doUpdate()` 系统更新 | 确定进度 4 步骤 | 1/4 授权验证 → 2/4 下载更新 → 3/4 完整性检查 → 4/4 清理缓存 |
| `fetchSiteVideos()` 资源站视频列表 | 不确定进度动画条 | 文案「正在获取视频列表，请稍候...」 |
| `loadAiAutoLearnLogs()` 日志加载 | 不确定进度动画条 | 文案「加载日志中，请稍候...」 |
| `viewOfficialSiteVideos()` 官方资源站 | 不确定进度动画条 | 文案「加载中，请稍候...」 |
| `showOfficialVideoDetail()` 视频详情 | 不确定进度动画条 | 文案「获取视频详情中，请稍候...」 |
| `searchOfficialVideos()` 搜索 | 不确定进度动画条 | 文案「搜索中，请稍候...」 |

#### 修改文件

| 文件 | 修改 |
|------|------|
| `mxadmin.php` | 新增进度条 CSS/JS 组件，10+ 长耗时/loading 接口改造，v5.9.5 公告 |
| `version.php` | 版本号升级到 v5.9.5，version_code=50905 |
| `CHANGELOG.md` | 记录进度条改造内容 |

## v5.9.4 (2026-08-03)

### Bug 修复

#### AI 自动学习执行报错 `body stream already read`

- **问题**：点击"执行 AI 自动学习"按钮时报错 `Failed to execute 'text' on 'Response': body stream already read`
- **原因**：前端 `try { data = await res.json(); } catch { const text = await res.text(); }` 模式中，`res.json()` 解析失败时 body stream 已被消耗，再调 `res.text()` 会抛出此错误
- **修复**：改为先 `text = await res.text()` 再 `JSON.parse(text)` 的安全模式，避免 body 重复读取
- **影响文件**：`mxadmin.php`（AI 自动学习 run 按钮的响应处理）

## v5.9.3 (2026-08-03)

### 重大功能升级 - 自动成长系统

#### 1. 默认覆盖全部资源站

- **新增**：`target_mode` 配置字段
  - `all`（默认）：自动覆盖全部启用资源站，无需手动维护列表
  - `custom`：仅处理 `target_sites` 列表中的资源站
- **新增**：`resolveTargetSites()` 方法动态解析生效站点
- **新增**：`ai_autolearn/sites` 端点查看生效资源站列表
- 实测：自动识别 98 个启用资源站

#### 2. 广告规则自动更新（免手动）

- 学习成功后规则自动保存到 `gz/rules_*.php`
- M3U8 处理流程（lz.php/gzgx.php）自动读取最新规则
- 完全自动化：学习 → 保存 → 应用，无需任何手动干预

#### 3. 定期清理失效规则

- **新增**：`cleanupStaleRules()` 方法
  - 超过 `stale_rule_days`（默认 30 天）未更新的规则进入候选
  - 对候选域名做 HTTP 健康检查（HEAD/GET）
  - 不可达的域名规则自动删除，下次学习时重新获取
- **新增**：`ai_autolearn/cleanup` 端点（支持 `force=1` 强制清理）
- **新增**：cron 脚本 `cleanup` 模式（每天凌晨 3 点执行）
- 时间间隔保护：`cleanup_interval_hours`（默认 24 小时）避免频繁清理

#### 4. 部署即可自动成长

- **新增**：懒触发机制 `autoTriggerIfNeeded()`
  - 前端访问 `info/version` 时自动检查是否需要执行
  - 后台非阻塞触发（exec + & 或异步 HTTP），不影响当前请求
  - 同时触发学习任务和规则清理
- **新增**：`ai_autolearn/trigger` 端点手动触发
- **新增**：`install_cron.sh` 一键部署脚本
  - `./install_cron.sh` 安装定时任务（每 4 小时学习 + 每天 3 点清理）
  - `./install_cron.sh uninstall` 卸载
  - `./install_cron.sh status` 查看状态
- 即使不配置 cron，前端访问也能自动触发（懒触发机制）

#### 5. 配置项扩展

| 新增字段 | 默认值 | 说明 |
|---------|--------|------|
| `target_mode` | `all` | 资源站选择模式 |
| `auto_trigger_on_request` | `true` | 请求时懒触发 |
| `auto_cleanup_stale_rules` | `true` | 自动清理失效规则 |
| `stale_rule_days` | `30` | 规则过期天数 |
| `cleanup_health_timeout` | `6` | 健康检查超时秒数 |
| `cleanup_interval_hours` | `24` | 清理最小间隔小时 |
| `last_cleanup_time` | `null` | 最后清理时间 |

#### 修改文件

| 文件 | 修改 |
|------|------|
| `gz/AiAutoLearner.php` | 重构：新增 target_mode/清理/懒触发机制 |
| `gz/ai_auto_learn_config.php` | 新增自动成长相关配置 |
| `cron_ai_autolearn.php` | 新增 cleanup 模式 |
| `mx.php` | 新增 cleanup/trigger/sites 端点；info/version 集成懒触发 |
| `install_cron.sh` | 新增：一键部署定时任务脚本 |
| `version.php` | 版本号升级到 v5.9.3 |

## v5.9.2 (2026-08-03)

### Bug 修复

#### 1. AI 自动学习失败返回 HTTP 500

- **问题**：`ai_autolearn/run` 接口在未启用或业务逻辑失败时返回 HTTP 500，导致前端报错
- **修复**：业务逻辑失败（如"未启用"）统一返回 HTTP 200 + `success: false`，只有服务器异常才返回 500

#### 2. 版本号不递增/不显示

- **问题**：前端侧边栏调用 `action=info/version` 获取版本号，但 mx.php 中缺少此路由，请求落入 `default` 分支
- **修复**：新增 `case 'info/version'` 端点，返回 version/commit/version_code/build/updated_at
- **修复**：后台公告列表 `getLocalAnnouncements()` 缺少 v5.9.0、v5.9.1、v5.9.2 记录，已补充

#### 修改文件

| 文件 | 修改 |
|------|------|
| `mx.php` | 修复 `ai_autolearn/run` HTTP 状态码；新增 `info/version` 端点；路由列表补充 |
| `mxadmin.php` | 公告列表添加 v5.9.0、v5.9.1、v5.9.2 |
| `version.php` | 版本号升级到 v5.9.2，version_code=50902 |

## v5.9.1 (2026-08-03)

### 规则引擎优化 - 修复如意 rym3u8 误判问题

#### 问题分析

以 `https://cdn.ryplay12.com/20260622/37492_2184b2fa/index.m3u8` 为例，发现以下问题：

- ❌ 文件名模式规则 `/^/i` 错误匹配所有文件名（MD5 哈希文件名被提取前缀）
- ❌ DISCONTINUITY 权重过高（80），正常编码切换被误判为广告插播
- ❌ 序列号跳跃权重过高（90），加剧误判
- ❌ 广告判定阈值过低（45），导致大量正常片段被移除
- ❌ mxjx/info 接口缺少安全机制标记，100% 片段被移除时无法回退
- ❌ learnFromAnalysis 保护阈值过低（85%），误判结果仍被学习

#### 修复内容

| 修复项 | 修复前 | 修复后 |
|--------|--------|--------|
| 文件名模式提取 | 对所有文件名提取前8字符 | 跳过 MD5/哈希类文件名（纯十六进制≥6位、纯数字） |
| DISCONTINUITY 权重 | 80 | 40 |
| 序列号跳跃权重 | 90 | 50 |
| 默认广告阈值 | 50 | 60 |
| 阈值调整下限 | 30 | 45 |
| learnFromAnalysis 保护 | ≥85% 跳过 | ≥75% 跳过 |
| 保留内容保护 | <15% 跳过 | <20% 跳过 |
| 最小片段数 | 无限制 | <10 不学习 |
| mxjx/info 安全机制 | 无 | ≥90% 回退原始 M3U8 |
| 样本数上限 | 10 | 100 |

#### 验证结果

同一地址 `cdn.ryplay12.com` 修复前后对比：

| 指标 | 修复前 | 修复后 |
|------|--------|--------|
| 广告片段（analyze） | 53/92 (56.3%) | 23/92 (25.0%) |
| 保留片段（mxjx/info） | 0/92 (100% 误删) | 69/92 (75% 保留) |
| 安全机制触发 | ❌ 未触发 | ✅ 正常判断 |
| 文件名模式规则 | `/^/i`（错误） | 无（正确跳过 MD5） |

#### AI 自动学习配置优化

- 默认样本数从 5 提升到 **50**（支持 1-100）
- 获取视频列表上限从 `videosPerSite*4` 限制为 `min(500, videosPerSite*4)`
- 后台页面输入框上限从 10 改为 100

## v5.9.0 (2026-08-03)

### 新增 AI 自动学习功能（频繁更新规则专用）

#### 核心功能

- 🧠 **AI 自动学习引擎**：每隔几小时自动从指定资源站（默认如意资源站）获取热门/更新视频
- 🎯 **精准播放源过滤**：按 `play_from` 标识过滤（默认 `rym3u8`），只学习指定源的无广告地址
- 🔥 **热门视频优先**：按 `vod_remarks`（更新至xx集/完结等）智能排序，优先学习正在更新的热门影视
- 🚫 **视频去重机制**：已学习视频 7 天内不重复分析（可配置保留天数）
- 📊 **深度广告分析**：EnhancedAdRuleEngine + ProfessionalAdDetector 双引擎分析，识别广告/插播/水印
- ⚡ **按小时调度**：与原自动学习（按天）互补，支持 1-24 小时执行间隔
- 🔧 **按资源站单独配置**：可自由指定目标资源站列表和播放源标识

#### 新增文件

| 文件 | 说明 |
|------|------|
| `gz/AiAutoLearner.php` | AI 自动学习核心引擎类 |
| `gz/ai_auto_learn_config.php` | 配置文件（后台自动维护） |
| `cron_ai_autolearn.php` | 定时任务入口（CLI/HTTP/锁机制） |

#### 新增 API 端点

| 端点 | 说明 |
|------|------|
| `GET /mx.php?action=ai_autolearn/config` | 获取配置和状态 |
| `POST /mx.php?action=ai_autolearn/config/save` | 保存配置 |
| `GET /mx.php?action=ai_autolearn/status` | 获取运行状态 |
| `POST /mx.php?action=ai_autolearn/run` | 立即执行 AI 自动学习 |
| `GET /mx.php?action=ai_autolearn/logs` | 获取执行日志 |

#### 后台管理

- 资源管理 → AI自动学习（🧠 NEW 标签）
- 状态概览卡片（运行状态/上次执行/执行间隔/目标资源站）
- 完整配置表单（启用/间隔/站点/播放源/热门排序/去重/密钥等）
- 一键执行 + 执行结果详情展示 + 日志查看

#### 定时任务配置

```bash
# Crontab 每4小时执行（推荐）
0 0,4,8,12,16,20 * * * php /path/to/cron_ai_autolearn.php

# 或 URL 触发
curl "http://你的域名/cron_ai_autolearn.php?key=你的密钥"
```

#### 测试验证

- ✅ 从如意资源站成功获取 rym3u8 视频列表
- ✅ 成功分析并更新域名规则（cdn.ryplay11.com, svip.ryplay17.com, cdn7.ryplay7.com）
- ✅ 视频去重、热门排序、play_from 过滤均正常工作
- ✅ CLI 和 HTTP 两种触发方式均通过测试

## v5.8.4 (2026-07-23)

### 彻底修复自动学习502 Bad Gateway报错（二次修复）

#### 问题分析

v5.8.2 修复后仍然出现 502 错误，深入分析发现更多问题：

1. **遗漏接口未加固**：`sites/learn_batch`、`sites/analyze_batch` 等接口完全缺少超时设置和异常捕获
2. **多线程超时过长**：多线程模式超时设置为 120s/90s，超过 nginx 默认 fastcgi_read_timeout (60s)
3. **单次学习数量过多**：最多 10 个站点 × 10 个视频 = 100 个视频学习，执行时间远超 nginx 超时
4. **M3U8 下载超时硬编码**：`M3U8Parser` 超时硬编码为 60s，无法外部控制
5. **单个视频学习无超时保护**：`learnFromVideoUrl` 方法内部没有执行时间检查

#### 修复内容

**1. 全面加固所有学习/分析接口**

| 接口 | 新增保护 |
|------|---------|
| `sites/learn_batch` | 超时180s、内存384M、try-catch、数量限制20个 |
| `sites/analyze_batch` | 超时180s、内存384M、try-catch、数量限制20个 |
| `sites/search_and_learn` | 多线程超时 120s → 45s |
| `sites/auto_learn/run` | 多线程超时 90s → 45s、并发数 5 → 3 |

**2. 严格限制单次学习数量**

- 自动学习最大站点数：10 → **5**
- 自动学习每站点视频数：10 → **5**（默认 5 → 3）
- 批量学习最大视频数：无限制 → **20**
- 批量分析最大视频数：无限制 → **20**

**3. M3U8Parser 增加超时控制**

- 新增 `setTimeout($seconds)` 方法
- 新增 `setConnectTimeout($seconds)` 方法
- 下载超时从硬编码 60s 改为可配置

**4. learnFromVideoUrl 增加执行时间保护**

- 默认最大执行时间：30 秒
- 增加阶段性超时检查（解析前、解析后）
- 最大片段数：3000 → 1000
- 返回执行耗时统计

**5. 并发数进一步降低**

- 多线程并发：最高 5 → 最高 **3**
- 避免高并发导致 PHP-FPM 进程耗尽

#### 修改文件

- [mx.php](file:///workspace/mx.php) — 所有批量接口加固、超时和数量限制优化
- [src/M3U8Parser.php](file:///workspace/src/M3U8Parser.php) — 增加超时控制方法
- [gz/ResourceSiteManager.php](file:///workspace/gz/ResourceSiteManager.php) — learnFromVideoUrl 增加超时保护
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.8.4

---

## v5.8.3 (2026-07-23)

### 修复公告不能自动更新内容

#### 问题分析

公告系统存在以下问题：

1. **依赖外部服务器**：公告仅从 `http://114.134.184.91:9001/公告.txt` 单一外部服务器获取，服务器不可用时无法显示公告
2. **无本地存储**：本地 `gg.txt` 文件未被有效利用，无法通过后台管理
3. **无管理功能**：没有后台界面可以编辑和管理公告内容
4. **无降级机制**：远程获取失败时仅有静态内置公告，无法缓存上次获取的内容

#### 修复内容

**1. 新增公告 API 接口**

- `announcement/list` - 获取公告列表（从本地 gg.txt 读取）
- `announcement/save` - 保存公告列表
- `announcement/add` - 添加单条公告
- `announcement/refresh` - 从远程源同步公告

**2. 优化公告加载多级降级机制**

- 第一级：本地 API 接口（优先读取本地 gg.txt）
- 第二级：GitHub Raw（https://raw.githubusercontent.com/ssmhdssmhd/qcb/main/gg.txt）
- 第三级：jsDelivr CDN
- 第四级：备用服务器
- 第五级：localStorage 缓存
- 第六级：内置默认公告

**3. 新增后台公告管理页面**

- 公告列表可视化编辑
- 添加/删除/排序公告
- 从远程同步公告
- 显示公告总数和最后更新时间

**4. 多远程源自动切换**

- 配置 4 个远程公告源，按优先级尝试
- 支持 HTTPS 优先，HTTP 备用
- CDN 加速，国内访问更快

#### 修改文件

- [mx.php](file:///workspace/mx.php) — 新增公告相关 API 接口
- [mxadmin.php](file:///workspace/mxadmin.php) — 公告管理页面和加载逻辑优化
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.8.3

---

## v5.8.2 (2026-07-23)

### 修复自动学习502 Bad Gateway报错

#### 问题分析

后台自动学习时报错 "502 Bad Gateway"，错误原因：

1. **执行超时**：自动学习接口缺少 `set_time_limit` 设置，PHP 执行超时导致 PHP-FPM 进程挂掉
2. **缺少错误兜底**：部分接口未捕获异常，致命错误时 nginx 返回 502 错误页面而非 JSON
3. **并发过高**：多线程模式并发数和超时时间设置不合理
4. **HTTP状态码问题**：致命错误返回 500 状态码，部分 nginx 配置会拦截并用默认错误页替换

#### 修复内容

**1. 增加超时和内存限制**

- `sites/learn_video` 接口：增加 `set_time_limit(60)` 和 `memory_limit=256M`
- `sites/auto_learn/run` 接口：增加 `set_time_limit(300)` 和 `memory_limit=512M`
- `sites/search_and_learn` 接口：增加 `set_time_limit(180)` 和 `memory_limit=384M`

**2. 完善异常捕获**

- `sites/learn_video`：添加 try-catch，异常时返回 200 状态码的 JSON 错误响应
- `sites/auto_learn/run`：添加全局 try-catch，异常时优雅降级返回
- `sites/search_and_learn`：添加 try-catch，确保始终返回 JSON

**3. 优化并发和执行限制**

- 多线程模式并发数：最高 10 → 最高 5
- 多线程超时：120秒 → 90秒
- 单次自动学习最大站点数：限制最多 10 个
- 每站点视频数：限制最多 10 个
- 搜索学习并发数：最高 10 → 最高 5

**4. 修复致命错误处理**

- 全局致命错误处理器 `jsonFatalHandler`：状态码从 500 改为 200
- 确保即使 PHP 致命错误也返回 JSON 格式响应
- 避免 nginx 拦截 5xx 状态码并用 HTML 错误页替换

**5. 统一限制应用到所有管理器**

- 文件版 `ResourceSiteManager::runAutoLearn` 添加数量限制
- 数据库版 `DbResourceSiteManager::runAutoLearn` 添加数量限制

#### 修改文件

- `mx.php` - 所有学习相关接口增加超时、内存限制、异常捕获
- `gz/ResourceSiteManager.php` - 单线程自动学习增加数量限制
- `db/DbResourceSiteManager.php` - 数据库版增加数量限制
- `version.php` - 版本号 v5.8.1 → v5.8.2

---

## v5.8.1 (2026-07-23)

### 资源站深度分析与修复

#### 问题分析

对 27 个暂停/异常的资源站进行了深度检测分析，采用多维度诊断：
- **域名解析检测**：检查DNS是否可正常解析
- **多种协议测试**：HTTP/HTTPS 双协议尝试
- **URL变体测试**：每个资源站生成10-42个不同的URL变体进行测试
- **API可用性验证**：验证接口返回是否包含有效视频数据
- **响应时间测量**：记录各接口响应速度

#### 修复内容

**1. 恢复 3 个可修复资源站**

| 资源站 | 问题 | 修复方案 | 响应时间 |
|--------|------|----------|----------|
| 12官方 | 404失效，API子域名错误 | 修正API地址为 `www.gfzyw.com` | ~969ms |
| 淘片 | SSL连接失败 | 改用HTTP协议 | ~1123ms |
| 黑木耳 | 停更状态，API子域名错误 | 更换域名，恢复为活跃 | ~1065ms |

**2. 确认 24 个失效资源站**

失效分类统计：
- **SSL连接超时（8个）**：优质、八豆、牛牛、飞刀资源、无线、12鱼乐、360酷、九月
- **SSL握手失败（11个）**：10樱花(2个)、爱奇艺、新浪、极速、虎牙、ikun、乐视、好看、飞速、优优、13华为、蜂巢、15魔术
- **HTTP 404（2个）**：天空、八豆
- **API无有效数据（2个）**：360酷、九月
- **IP直连不可用（1个）**：12鱼乐

**3. 详细失效原因备注**

为所有确认失效的资源站更新了详细的失效原因备注，包含：
- 深度检测日期
- 具体错误类型
- 最终结论

#### 修改文件

- `gz/sites_config.php` - 资源站配置，v4.2.0 → v4.3.0
- `version.php` - 版本号，v5.8.0 → v5.8.1

#### 数据统计

| 指标 | 修复前 | 修复后 | 变化 |
|------|--------|--------|------|
| 资源站总数 | 122 | 122 | 0 |
| 活跃资源站 | 95 | **98** | +3 |
| 暂停资源站 | 27 | 24 | -3 |

---

## v5.8.0 (2026-07-23)

### 资源站列表大规模扩充

#### 修改内容

**1. 新增 61 个资源站**

从苹果CMS采集资源站完整列表中批量导入了61个新的资源站，涵盖以下类别：

- **官方推荐/活跃维护资源站（7个）：聚合资源、华为吧、黑木耳资源、ikun资源、旺旺短剧、1080zyku、卧龙资源
- **JSON格式采集接口（18个）**：海外看、360资源、刺桐资源、业余资源、华为吧2、小黄人、U酷资源、四九资源、快看资源、熊掌资源、飘花资源、天翼资源、虎牙资源、百度资源、飘零资源、速博资源、魔都资源、奇虎资源、快云资源、开放电影
- **补充资源站（24个）**：39影视、矢量资源、乐活影视、唐人街、酷点资源、酷点备用、森林资源、影库资源、探探资源、金鹰资源、奥斯卡资源、老鸭资源、北斗资源、快播资源、艾旦影视、飘花电影、网片电影、麒麟资源、番茄资源、8090资源、官网采集
- **萌芽合作资源站（13个）**：黑料资源网、奶香香资源、玉兔资源、CK资源网、888联盟、杏吧资源站、暴风资源站、155资源站、森林资源站、无水印资源站、九游联盟、合作共赢、98资源网

**2. 资源站统计**

- 资源站总数：从 61 个增加到 **122 个**
- 活跃资源站：从 34 个增加到 **95 个**
- 配置版本：v4.1.0 → v4.2.0

**3. 优先级分布**

- 优先级 1-3（高优先级）：官方推荐资源站
- 优先级 6-7（中优先级）：JSON接口资源站
- 优先级 8-9（低优先级）：补充资源站和合作站

## v5.7.9 (2026-07-22)

### 优化 jiexi.php 解析接口返回格式

#### 修改内容

**1. msg 字段返回播放地址 URL**

将 JSON 格式返回的 `msg` 字段从状态文字改为播放地址 URL，与 `url` 字段内容一致，方便部分只读取 msg 字段的播放器使用。

**2. 新增 time 字段（解析耗时）**

```json
"time": "0.123s"
```

从 `parseVideo()` 返回的 `time` 字段获取，真实计算解析耗时（秒）。

**3. 新增 KFZ 字段（开发者）**

```json
"KFZ": "超级嗅探|XT"
```

从配置 `developer.name` + `developer.author` 获取，标识开发者信息。

**4. 新增 ZT 字段（状态）**

```json
"ZT": "解析成功"
```

从 `parseVideo()` 返回的 `ZT` 字段获取，用于状态描述。

#### 返回格式示例

**成功：**
```json
{
  "code": 200,
  "ZT": "解析成功",
  "msg": "http://xxx/xt/clean.php?id=xxx",
  "url": "http://xxx/xt/clean.php?id=xxx",
  "time": "0.123s",
  "KFZ": "超级嗅探|XT",
  "info": "TVBox影视专用解析"
}
```

**失败：**
```json
{
  "code": 400,
  "ZT": "解析失败",
  "msg": "错误信息",
  "url": "",
  "time": "0.005s",
  "KFZ": "超级嗅探|XT"
}
```

#### 影响范围

- ✅ JSON 格式（默认）：新增 ZT / time / KFZ 字段，msg 改为 URL
- ✅ 302 / api / xml 格式：保持不变
- ✅ 向后兼容：url 字段未变，原有播放器不受影响

## v5.7.8 (2026-07-22)

### 修复推荐采集视频点击不显示播放地址问题

#### 问题现象

在后台「推荐采集」页面搜索视频后，点击视频卡片跳转到集数列表时，显示"为了防止爬虫，播放地址不再显示，如有需要请去采集"，无法获取真实播放地址。

#### 问题根因

MacCMS 资源站在列表接口（`ac=list` / `ac=detail&wd=keyword`）中返回的 `vod_play_url` 被替换为防爬提示文字，不包含真实播放地址。只有通过详情接口（`ac=detail&ids=vod_id`）才能获取完整的播放地址。

#### 修复内容

**1. 新增 `ResourceSiteManager::getVideoDetail($apiUrl, $vodId)`**

通过 `ac=detail&ids=vod_id` 接口获取视频详情，解析真实播放地址：

```php
$params = [
    'ac' => 'detail',
    'ids' => intval($vodId)
];
```

**2. 新增 `DbOfficialSiteManager::getVideoDetail($siteName, $vodId)`**

数据库版管理器封装，支持多域名自动切换和重试。

**3. 新增 API 接口 `official_sites/detail`**

```
GET mx.php?action=official_sites/detail&name=TW推荐采集&vod_id=12345
```

**4. 前端修改 `mxadmin.php`**

- `renderOfficialVideos()`：点击视频卡片调用 `showOfficialVideoDetail()`（而非直接学习）
- 新增 `showOfficialVideoDetail(vodId, videoName)`：调用详情接口获取真实播放地址
- 集数列表展示：集数名称 + 播放地址链接 + 复制按钮 + 学习按钮
- 支持返回视频列表

#### 使用流程

1. 在「推荐采集」页面搜索视频 → 显示视频卡片列表
2. 点击视频卡片 → 调用详情接口获取真实播放地址 → 显示集数列表
3. 每个集数显示：集数名称、可点击的播放地址链接、复制按钮、学习按钮
4. 点击"返回视频列表"回到视频卡片列表

#### 影响范围

- ✅ 推荐采集页面：视频点击后显示真实播放地址
- ✅ 资源站防爬限制：通过详情接口绕过
- ✅ 向后兼容：不影响现有功能

## v5.7.7 (2026-07-22)

### 新增 AI 智能解析公用 API（ai/sniff.php）

#### 功能说明

新增 `ai/` 目录，提供独立的智能解析公用 API：`ai/sniff.php`，支持从任意网页播放器（腾讯视频、爱奇艺、优酷、芒果TV等）获取真实播放地址链接。

#### 技术实现

- **签名算法**：AES-256-CBC + ZeroPadding，兼容 CryptoJS
  - key = MD5(timestamp + url) 的 hex 字符串
  - iv = `fUU9eRmkYzsgbkEK`
  - plaintext = keyHex（32字节，16字节对齐）
- **双 API 节点 fallback**：
  1. `https://cache.0567890.xyz:4433/Api`
  2. `https://cache.hls.one/Api`
  - 第一个节点失败时自动重试第二个
- **解密逻辑**：
  - 优先 ZeroPadding 模式（匹配 CryptoJS 默认）
  - 解密失败自动降级 PKCS7 模式
  - 自动去除 `tg:@xmflv` 水印
  - 支持 `vurl` / `url` 两种播放地址字段
- **响应格式**：`[{code, msg, type, label, url, time}]` JSON 数组
  - label 自动识别：HLS（m3u8/hls）、MP4

#### 使用方式

```
GET ai/sniff.php?url=https://v.youku.com/v_show/id_xxx.html
```

支持 CORS 跨域访问（`Access-Control-Allow-Origin: *`）。

---

### 优化 AI 匹配算法：配置驱动标准化 + 多维度评分重构

#### 背景

官替解析系统（`OfficialReplaceManager` → `AiVideoMatcher`）依赖 `TitleNormalizer` 进行视频标题标准化，原实现存在以下问题：

1. **同义词硬编码分散**：约 400+ 条同义词映射散落在多个独立正则数组中，难以维护
2. **链式副作用**：多组规则顺序敏感，如 `tv`→`TV版` 后又被 `TV`→`TV版` 重复命中；`;`→`:` 再被 `:`→`''` 移除依赖两步执行顺序
3. **跨源不一致**：同一剧集不同来源写法（`庆余年第二季 1080P 国语版` / `庆余年第2季1080P` / `庆余年S02 1080P 高清`）标准化结果不同，导致匹配失败
4. **季集解析覆盖不全**：`S01` 零填充形式、罗马数字 Ⅰ-Ⅸ 等未处理
5. **AiVideoMatcher 评分缺陷**：
   - 季数惩罚过弱（`seasonDiff*5`），跨季匹配仍可能命中
   - `levenshteinSimilarity` 实现为不准确的位置 diff（仅比较前 min(len) 个字符）
   - 缺少噪声候选排除（电影解说/预告片等可能误匹配）

#### 修改内容

##### 1. 新增 `gz/synonym_config.php`（核心配置文件）

集中管理用户提供的全部同义词映射，按类别分组返回数组：

| 类别 | 内容 | 取值约定 |
|------|------|---------|
| `season` | 第N季/部/番/卷、第一季、S1-S20（含 S01-S09 零填充）、罗马数字 Ⅰ-Ⅸ | `第1季`/`S1`/`Ⅰ` 置空；`第2季`/`第二季`/`S2`/`II` → `2` |
| `episode` | N集（1-100）、EP1-EP10、E01-E09 | 全部置空（由 `parseTitleInfo` 单独抽取） |
| `quality` | 4K/8K/2K/1080P/720P/蓝光/HD/HDR/杜比 等 | 规范化为统一形式 |
| `language` | 国语/粤语/英语/日语/双语/字幕 等 | 置空或规范化 |
| `version` | TV版/DVD版/剧场版/导演剪辑版/重制版 等 | 规范化或置空 |
| `region` | 英版/美版/日版/港版/台版 等 | 规范化（如 `英国版`→`英版`） |
| `symbol` | 全半角标点、特殊字符 | 置空或规范化（如 `〜`→`·`） |

**关键设计**：
- 使用循环批量生成 第N季/N集/EP\d+/S\d+ 等映射，避免手写 400 行
- 原词典 `;`→`:` 再 `:`→`''` 的两步链式替换简化为 `;`→`''` 单步等价处理
- `字幕` 单独置空（修正原仅处理 `字幕版` 变体导致 `某番剧字幕` 残留的问题）
- `S2`-`S20` 映射为对应数字（原词典全部置空会导致 `庆余年S2` 与 `庆余年第二季` 标准化不一致）

##### 2. 重构 `gz/TitleNormalizer.php`（配置驱动 + 单遍最长匹配）

**核心算法 `applyMap($title, $category)`**：
- 按 key 长度降序排序（同长按字典序倒序），保证最长匹配优先
- 编译为单个 alternation 正则，`preg_replace_callback` 一次扫描完成所有替换
- **替换结果不再被同组规则二次扫描**，消除链式副作用

**应用顺序**：`symbol → season → episode → quality → language → version → region → 折叠空白`

**公共 API（保持向后兼容）**：
- `normalize($title)` / `canonicalize($title)` 别名 — 标准化主入口
- `getBaseTitle($title)` — 基础剧名（剥离季/集/画质等后缀）
- `getSeasonInfo($title)` — 季数（int|null），覆盖 第N季/部/卷/番、S\d+、S\d+E\d+、罗马数字
- `getEpisodeInfo($title)` — 集数（int|null，新增），覆盖 N集、EP\d+、E\d+、S\d+E\d+
- `clearCache()` — 清空内部缓存

**性能优化**：
- 使用 `md5($title)` 作为缓存 key，避免对同一标题重复标准化
- 正则按类别编译一次后复用

##### 3. 优化 `gz/AiVideoMatcher.php` 评分算法

**标准化统一委托**：
- 所有标题标准化统一委托 `TitleNormalizer`（消费 `synonym_config.php`）
- 季/集解析委托 `TitleNormalizer::getSeasonInfo` / `getEpisodeInfo`

**新增噪声排除模式**：
- `private static $excludePatterns`：19 种噪声内容模式
  - 电影解说、预告片、片花、花絮、混剪、MV、OST、彩蛋、删减片段、幕后、采访、解说版、速看、5分钟、合集、名场面、补完、整活、二创
- 命中噪声模式的候选扣 `exclude_penalty = 50` 分

**`levenshteinSimilarity` 重写为基于 LCS 的真实实现**：
- 原实现为不准确的位置 diff（仅比较前 min(len) 个字符位置）
- 新实现：`(lcs / maxLen) * 100`，lcs 为最长公共子序列长度

**权重再平衡**：

| 维度 | 权重 | 说明 |
|------|------|------|
| `title_exact` | 35 | 标准化基础剧名完全一致 |
| `title_similarity` | 25 | 标题相似度（LCS + Jaccard + similar_text 均值） |
| `title_contains` | 12 | 一方为另一方前缀 |
| `semantic_similarity` | 10 | 同义词语义相似度 |
| `season_match` | 20 | 季数一致奖励 |
| `season_mismatch` | 25 | 季数不一致惩罚（按 diff×8 线性递增，封顶此值） |
| `episode_match` | 12 | 集数一致奖励 |
| `part_match` | 8 | 部数一致 |
| `version_match` | 5 | 版本标记一致 |
| `remarks_quality` | 3 | 备注质量信号 |
| `keyword_count` | 8 | 关键字符命中数 |
| `exclude_penalty` | 50 | 噪声内容惩罚 |

**缓存优化**：
- 新增 `private $normCache`：本次 `smartMatch` 调用生命周期内的标准化缓存
- 同义词语义相似度使用标准化后的标题，避免重复归一化

**强匹配奖励**：
- 当标准化基础剧名完全一致时，给予 `title_exact` 满分奖励，确保跨源同剧不同写法必命中

#### 测试验证

通过 4 个综合功能测试场景：

| 场景 | 输入 | 预期 | 实际 |
|------|------|------|------|
| 跨源匹配 | `庆余年第二季 1080P 国语版` + 3 候选 | 命中 `庆余年第2季`，排除 `庆余年电影解说` | ✅ 最佳 72.1 分，噪声 0 分 |
| 集数匹配 | `凡人修仙传第5集` + 3 候选 | 第5集/EP5 同分，第6集低分 | ✅ 第5集/EP5 72.1，第6集 63.41 |
| 噪声排除 | `三体` + `三体电影解说`/`三体预告片` | 噪声候选 0 分 | ✅ 正片 60.51 分，两者均 0 分 |
| 空候选 | 无候选 | 返回 null | ✅ 正确返回 null |

#### 影响范围

- ✅ 官替解析系统：跨源匹配一致性大幅提升，标准化驱动的匹配避免同剧不同写法漏配
- ✅ 噪声候选排除：电影解说/预告片等不再误匹配为正片
- ✅ 季集解析覆盖更全：S01 零填充、罗马数字、EP/E 集数标记均正确识别
- ✅ 性能优化：标准化结果缓存，重复请求零开销
- ✅ 向后兼容：`TitleNormalizer` 公共 API 保持不变，`AiVideoMatcher` 关键方法签名不变

## v5.7.6 (2026-07-19)

### 修复 jiexi.php 解析返回的 clean.php URL 路径错误导致不能播放

#### 问题现象

调用 `http://114.134.184.91:9002/jiexi.php?url=...` 解析腾讯视频，返回结果：

```json
{
  "code": 200,
  "msg": "解析成功",
  "url": "http://114.134.184.91:9002/clean.php?id=50a555ed8b0c08f1",
  "info": "TVBox影视专用解析"
}
```

URL `http://114.134.184.91:9002/clean.php?id=...` **不能播放**，访问 404。

#### 问题根因

`saveCleanM3u8()` 函数用 `dirname($_SERVER['SCRIPT_NAME'])` 推断 clean.php 的 URL 路径：

| 调用入口 | SCRIPT_NAME | dirname(SCRIPT_NAME) | 生成的 URL | 是否正确 |
|---------|------------|---------------------|-----------|---------|
| `/xt/api.php` | `/xt/api.php` | `/xt` | `http://host/xt/clean.php?id=xxx` | ✅ |
| `/jiexi.php`（根目录） | `/jiexi.php` | `/` | `http://host/clean.php?id=xxx` | ❌ |

**clean.php 实际位置始终在 `/xt/clean.php`**，但通过根目录的 jiexi.php 调用时，路径推断成了根目录，导致 404 无法播放。

#### 修复内容

`saveCleanM3u8()` 改用 `__DIR__`（server.php 所在目录，即 `xt/`）推断 clean.php 的 URL 路径：

1. 优先：用 `__DIR__` 相对 `DOCUMENT_ROOT` 的路径计算 URL 路径
   - `__DIR__ = /var/www/html/xt`，`DOCUMENT_ROOT = /var/www/html` → URL 路径 = `/xt`
2. 兜底：如果 DOCUMENT_ROOT 不可用或路径不匹配，用 SCRIPT_NAME 推断
   - 调用方在根目录时（如 jiexi.php），强制补 `/xt`
   - 调用方在子目录时，沿用该子目录

修复后，无论从根目录的 jiexi.php、mx.php，还是 xt/ 目录的 api.php 调用，都能正确生成 `/xt/clean.php?id=xxx` 的可播放 URL。

#### 影响范围

- ✅ jiexi.php 解析接口：返回的 clean.php URL 现在可以正常播放
- ✅ mx.php 后台解析：行为不变（已经在 xt/ 目录）
- ✅ xt/api.php：行为不变（已经在 xt/ 目录）
- ✅ TVBox / 影视App：解析结果可直接播放

## v5.7.5 (2026-07-19)

### 修复 jiexi.php 不能同时调用官解和官替，多线程高并发提速

#### 问题分析

之前 jiexi.php → parseVideo() → getVideoLinkBySnifferMode() 的调用链：
- 根据 `sniffer.mode` 选择走 official 或 replace 通道
- 当前通道失败时才 fallback 到另一通道
- 即使开启了 `race_mode`，也只是对 `official_apis` 数组内多个官解接口并发，**官解和官替之间是串行 fallback**，没有真正"同时调用"

#### 修复内容

**1. 新增 `getVideoLinkByConcurrentRace()` 函数（xt/server.php）**
- 把所有已启用的官解接口（`sniffer.official_apis` / `sniffer.official_api` / `official_apis`）和官替接口（`sniffer.replace_api`）合并到**同一个 curl_multi 并发池**
- 用 PHP 的 curl_multi 扩展同时发起多个 HTTP 请求，**真正实现多线程并发**
- 谁先返回有效结果就立即采用，自动取消其他正在进行的请求
- 自动识别命中的是 official 还是 replace 通道（通过 `_channel` 标记），后续 `parseVideoByOfficialChannel` / `parseVideoByReplaceChannel` 按通道分流处理
- 总耗时 ≈ 最快的那个接口的耗时，而非多个接口耗时之和

**2. 修改 `parseVideo()` 主流程**
- 新增 `concurrent_race_enabled` 开关判断分支
- 开启时调用 `getVideoLinkByConcurrentRace()`（并发模式）
- 关闭时维持原有 `getVideoLinkBySnifferMode()` 逻辑（向后兼容）

**3. 并发模式下的强制行为**
- 即使后台「嗅探设置」中 `replace_api.enabled = false`，并发模式也会**强制启用官替**（自动用本地官替接口 `mx.php?action=official_replace/info`）
- 确保两条通道同时跑，真正实现"同时调用官解和官替"
- `max_concurrent` 自动扩展为接口总数，避免某通道被排到剩余队列串行调用

**4. 新增配置项（xt/config.php）**
- `performance.concurrent_race_enabled`（默认 `true`）：是否同时调用官解和官替
- 与原有的 `race_mode`（官解数组内并发）配合，形成两级并发

#### 性能提升

| 场景 | 旧逻辑（串行 fallback） | 新逻辑（并发竞速） |
|------|------------------------|-------------------|
| 官解 2s 成功 | 2s | 2s |
| 官解失败，官替 3s 成功 | 5s+（官解超时后串行调官替） | 3s（同时并发，官替先成功） |
| 官解 4s，官替 1.5s 成功 | 4s（官解优先，先成功） | 1.5s（官替先成功，官解被取消） |
| 都失败 | 5s+（串行累加） | max(超时) 并发失败 |

#### 影响范围

- ✅ jiexi.php 解析接口：同时调用官解和官替，速度大幅提升
- ✅ mx.php 后台解析：同步受益
- ✅ TVBox / 影视App：解析响应更快，首屏等待更短
- ✅ 向后兼容：关闭 `concurrent_race_enabled` 即可回到旧逻辑

## v5.7.4 (2026-07-19)

### 优化 clean.php 播放器页面，移除多余 UI 元素

#### 修改内容

**简化播放器页面 UI**
- 移除顶部标题栏（"M3U8 无广告播放器" 标题和浏览器标签）
- 移除底部控制按钮（播放、暂停、复制链接按钮）
- 移除底部信息栏（缓存ID、去广告提示）
- 视频全屏显示，仅保留浏览器原生控制条

#### 影响范围

- xt/clean.php 浏览器访问时的播放器页面
- 不影响 TVbox 等软件播放器的 m3u8 内容返回

## v5.7.3 (2026-07-19)

### 优化更新备份功能，增加版本号和版本编号

#### 问题分析

之前的备份文件名格式为 `backup_YYYYMMDD_His.zip`，没有版本号信息，导致：
1. 用户无法从文件名判断备份对应的版本
2. 多个备份文件难以区分
3. 恢复时不知道恢复的是哪个版本

#### 修复内容

**1. 备份文件名增加版本号**
- 新格式：`backup_v{version}_{timestamp}.zip`
- 示例：`backup_v5.7.3_20260719_143000.zip`
- 从文件名即可直观看到版本号

**2. 备份文件内添加版本信息文件**
- 在备份根目录添加 `.backup_info.json` 文件
- 包含：version、commit、created_at、backup_type、platform、php_version
- 即使文件名被修改，也能从备份内容中获取版本信息

**3. getBackupList() 函数增强**
- 自动从文件名解析版本号
- 读取 `.backup_info.json` 获取详细信息
- 返回字段增加：version、commit、commit_short
- 兼容旧格式备份（无版本号时显示"未知版本"）

#### 影响范围

- 更新管理模块的备份功能
- 后台备份列表页面
- 自动更新时的备份操作

## v5.7.2 (2026-07-19)

### 修复 xt 文件夹中 clean.php 不能播放的问题

#### 问题根因

1. **浏览器检测逻辑过于宽泛**：`isBrowserRequest()` 函数通过 User-Agent 中的 "Mozilla/" 等关键词判断是否为浏览器请求，但 HLS.js 等播放器请求 m3u8 时也会携带浏览器 User-Agent（因为是在浏览器环境中运行），导致被误判为浏览器请求
2. **返回 HTML 而非 m3u8**：被误判为浏览器请求后，返回 HTML 播放器页面而不是 m3u8 内容，导致播放器无法解析，播放失败
3. **判断逻辑顺序错误**：原来的逻辑是先判断 User-Agent，再判断 Accept 头，导致即使 Accept 头明确请求 m3u8 类型，也会被 User-Agent 拦截

#### 修复内容

**1. 重写浏览器检测逻辑为 `shouldShowPlayerPage()`**
- 新增 `player=1` 参数显式控制：`clean.php?id=xxx&player=1` 强制显示播放器页面
- 优先检查 Accept 头：如果 Accept 不包含 `text/html`，直接返回 m3u8
- 排除 m3u8 类型请求：如果 Accept 包含 `application/vnd.apple.mpegurl` 或 `application/x-mpegurl`，返回 m3u8
- 排除播放器关键词：User-Agent 中包含 hls.js、videojs、exoplayer、vlc 等播放器标识时，返回 m3u8
- 排除 Range 请求：有 Range 头的请求（通常是视频分片请求），返回 m3u8
- 只有同时满足"Accept包含text/html"且"不是播放器请求"时，才显示播放器页面

**2. 优化播放器页面的 m3u8 URL 生成**
- 使用更可靠的方式构造 m3u8 URL，避免 URL 参数混乱
- 播放器页面中的 HLS.js 直接请求纯 m3u8 内容，不会再次触发播放器页面

**3. 移除重复的 header 设置**
- 原来代码中有两处设置 Content-Type 和缓存头，清理为一处

#### 影响范围

- xt/clean.php 去广告 m3u8 播放代理
- 所有通过 HLS.js、Video.js 等网页播放器播放的视频
- 所有调用 api.php 返回的播放链接

## v5.7.1 (2026-07-18)

### 修复所有用到代理的地方代理无法使用的问题

#### 问题根因

1. **代理管理器不一致**：DbOfficialReplaceManager 和 DbResourceSiteManager（数据库版）内部使用的是文件版 ProxyManager，而不是 DbProxyManager，导致数据库模式下代理配置不一致
2. **首次请求不使用代理**：所有使用代理的地方（M3U8Parser、OfficialReplaceManager、ResourceSiteManager等）都只在重试时（$attempt > 0）才使用代理，首次请求不经过代理，用户感觉代理没生效
3. **缺少依赖注入**：各个类内部自己实例化代理管理器，无法从外部统一注入和配置
4. **DbProxyManager排序逻辑不一致**：数据库版代理管理器的getProxy排序逻辑和文件版不一致，没有按响应时间优先排序

#### 修复内容

**1. 代理管理器依赖注入**
- 为 M3U8Parser 添加 `setProxyManager()` 和 `setUseProxyOnFirstTry()` 方法
- 为 OfficialReplaceManager 添加 `setProxyManager()` 和 `setUseProxyOnFirstTry()` 方法
- 为 ResourceSiteManager 添加 `setProxyManager()` 和 `setUseProxyOnFirstTry()` 方法
- 为 DbOfficialReplaceManager 添加 `setProxyManager()` 和 `setUseProxyOnFirstTry()` 方法
- 为 DbResourceSiteManager 添加 `setProxyManager()` 和 `setUseProxyOnFirstTry()` 方法
- 所有类优先使用注入的代理管理器，没有注入时才自己实例化（向后兼容）

**2. 首次请求使用代理**
- 所有类的 `$useProxyOnFirstTry` 默认值改为 `true`
- 只要代理池启用，首次请求就使用代理
- mx.php 中显式设置 `setUseProxyOnFirstTry(true)` 确保生效

**3. mx.php 统一注入代理管理器**
- 初始化 siteManager 和 officialReplaceMgr 后，自动注入 proxyManager
- 使用 method_exists 检查，确保向后兼容
- 数据库模式下注入 DbProxyManager，文件模式下注入 ProxyManager

**4. 统一 DbProxyManager 排序逻辑**
- getProxy() 方法排序逻辑与 ProxyManager 保持一致
- 按响应时间从快到慢排序（速度越快越优先）
- 有响应时间的优先，其次按失败次数少的优先，最后按优先级

#### 影响范围

- M3U8视频解析：代理立即生效
- 官替资源获取：代理立即生效
- 资源站接口调用：代理立即生效
- 数据库版和文件版均适用

## v5.7.0 (2026-07-18)

### 修复顶部统一接口不显示接口URL

#### 问题根因

全局CSS规则设置了 `select { width: 100% }`，导致顶部接口区域的下拉选择框（`.api-type-select`）占满了整个行的宽度，把右侧的URL显示区域（`.access-item`）挤得只剩复制按钮的宽度（30px），因此接口URL文字完全看不到。

#### 修复内容

- 为 `.api-type-select` 添加 `width: auto !important`，覆盖全局的 `width: 100%`
- 下拉框保持 `min-width: 180px` 的最小宽度，同时不会撑满整行
- URL显示区域正常占据剩余空间，接口地址完整可见

## v5.6.9 (2026-07-18)

### 修复顶部统一接口不显示接口信息

#### 问题根因

1. `.access-item code` 缺少显式的 `color: white`，在某些主题/浏览器下文字颜色不可见
2. `text-overflow: ellipsis` 需要配合 `display:block + white-space:nowrap + overflow:hidden` 才生效
3. base 路径计算在根目录部署时可能有问题

#### 修复内容

- 增加 `color: white !important` 确保 URL 文字可见
- 完善 ellipsis 样式：`display:block + white-space:nowrap + overflow:hidden + text-overflow:ellipsis`
- 优化 base 路径计算逻辑，兼容根目录和子目录部署

## v5.6.8 (2026-07-18)

### 修复接口URL不显示 + 移除管理后台卡片

#### 修改内容

- 修复顶部 V2 统一接口 URL 不显示的问题：
  - 原正则 `mxadmin\.php` 在不同入口文件名下失效
  - 改为 `lastIndexOf('/')` 取目录路径，兼容任意入口文件名
- 移除顶部右侧管理后台预览卡片，布局更简洁
- URL 超长时省略显示（`text-overflow: ellipsis`），防止溢出

## v5.6.7 (2026-07-18)

### 修复顶部统一接口区域右侧内容缺失

#### 问题根因

顶部 API 预览区域原本设计为左右双栏布局（左侧 V2 统一接口 + 右侧管理后台预览），但代码中右侧 admin-preview-card 缺失，导致 URL 文字溢出到右侧显示为竖排文字。

#### 修复内容

- 恢复左右双栏布局（2fr + 1fr）：左侧 V2 统一接口，右侧管理后台预览卡片
- 最新公告卡片从右栏移到底部，占满宽度，展示更充分
- 修复 URL 溢出问题：access-item 增加 min-width:0 防止 flex 子项溢出
- 响应式适配：平板（≤1024px）及以下自动切换为单列布局

## v5.6.6 (2026-07-18)

### 数据概览页面 UI 优化

#### 优化内容

- 统计卡片从 auto-fit 改为固定 3 列布局，6 个卡片两行整齐排列
- 卡片内部从上下布局改为左右布局（左侧图标 + 右侧内容）
- 顶部装饰条从 3px 横线改为左侧 4px 竖线，更现代简洁
- 快捷操作从 2 列改为 3 列，6 个操作两行整齐排列
- 完整响应式适配：
  - 桌面端：3 列统计 + 3 列快捷操作
  - 平板端：2 列统计 + 3 列快捷操作
  - 手机端：1 列统计 + 2 列快捷操作
  - 小屏手机：1 列统计 + 1 列快捷操作

## v5.6.5 (2026-07-18)

### 代理列表按速度排序 + 隐藏失败代理

#### 修改内容

- 后台代理列表页面：按响应时间从快到慢排序，不显示失败（inactive）的代理
- API `get_proxies` 接口：同样过滤失败代理 + 按速度排序
- `getProxy()` 方法：实际调用代理时优先选择响应时间最快的

#### 排序规则

1. 有响应时间的排前面，无响应时间的排后面
2. 都有响应时间：按快到慢排序（越小越快）
3. 都无响应时间：按失败次数少的优先，最后按优先级

## v5.6.4 (2026-07-18)

### 修复代理池不能正常使用的问题

#### 问题根因

1. **addProxy 无去重**：每次获取代理都重复添加，代理池膨胀导致性能下降
2. **addProxy 每次写文件**：批量添加100个代理写100次文件，性能极差
3. **代理池未自动启用**：获取到代理后 `enabled` 仍为 `false`，代理池不工作
4. **测试URL不稳定**：httpbin.org 在国内无法访问，导致验证全部失败
5. **验证成功判断过严**：只接受 HTTP 200，百度返回 30x 跳转会被误判为失败
6. **checkAllProxies 串行**：逐个测试代理，100个代理需要 100×8s = 800s
7. **proxy.scdn.io 解析过严**：强制要求 `code=200`，API 返回格式变化导致解析失败
8. **部分代理源失效**：ProxySpace、ProxyScan-API、sunny9577 等源已失效

#### 修复内容

##### ProxyManager 修复
- `addProxy()` 增加 host:port 去重检查
- 新增 `addProxiesBatch()` 批量添加方法（只写一次文件）
- `fetchProxiesFromWeb()` / `syncProxiesFast()` 改用批量添加
- 获取到代理后自动 `enabled = true`
- `checkAllProxies()` 改为 curl_multi 并发验证（10个一批）
- `testProxy()` 测试URL改为百度，HTTP 2xx/3xx 都算成功

##### ProxyFetcher 修复
- 测试URL：`https://httpbin.org/get` → `http://www.baidu.com/`
- 验证成功条件：`httpCode == 200` → `httpCode >= 200 && httpCode < 400`
- `parseScdnJson()` 兼容5种返回格式，不再强制要求 `code=200`
- 新增 `parseGeonodeJson()` 解析 Geonode API
- 更新代理源：移除失效源，新增 Geonode、monosans、clarketm、TheSpeedX-socks5

#### 版本号同步

- `version.php`：v5.6.3 → v5.6.4
- `xt/config.php`：5.6.3 → 5.6.4

## v5.6.3 (2026-07-18)

### 小版本更新 - 代理池并发优化 + 解析播放修复 + 多接口竞速

#### 更新内容

##### 1. 代理池并发获取优化

ProxyFetcher 重构为 curl_multi 并发请求所有代理源：
- 12 个代理源（proxy.scdn.io 等）并发请求，总耗时从 ~96s 降至 ~6s
- 代理验证改为并发执行（10个一批），验证速度提升 10 倍
- 新增 2 分钟本地缓存机制，避免频繁请求 proxy.scdn.io 导致延迟过高或获取不到
- 降低超时时间：单源 8s→6s，连接 5s→3s，验证 5s→4s，快速失败
- 新增 `syncProxiesFast()` 快速同步方法（不验证直接导入）
- 后台新增「⚡ 快速同步代理池」按钮

##### 2. 修复解析成功但不能播放的问题

优化官替通道 m3u8 处理逻辑：
- 修复官替接口 URL 为空时，自动使用本地 `official_replace/info` 接口
- 增加 m3u8 内容校验（`#EXTM3U` 标记检测），非 m3u8 格式直接返回原链接
- 增加多级 URL 提取和递归解析
- AdFilter 处理后内容为空或无有效 ts 时，回退到原始 m3u8
- 代理地址（mxjx/clean.php）识别和处理优化

##### 3. 多接口并发竞速 + AI 学习自动排序

新增 PerformanceOptimizer 性能优化器：
- 多接口并发竞速：curl_multi 并发请求多个官解接口，最快成功的立即返回
- AI 学习自动排序：记录每个接口的成功率、平均耗时、连续失败次数，评分算法动态调整优先级
  - 成功率权重 50%，平均耗时权重 40%，连续失败惩罚 10%
- 失败自动切换：一个接口被禁/失败，自动切换到下一个接口
- 性能统计持久化：JSON 文件存储，支持查看和重置
- 新增 API 端点：`sniffer/perf_stats`、`sniffer/perf_stats/reset`
- 后台支持多官解接口配置（`official_apis` 数组）

#### 版本号同步

- `version.php`：v5.6.0 → v5.6.3
- `xt/config.php`：5.2.0 → 5.6.3

#### 影响范围

- ✅ 代理池：从串行改为并发，获取速度提升 10 倍以上，解决 proxy.scdn.io 延迟过高问题
- ✅ 官替通道：修复解析成功但不能播放的问题，m3u8 处理逻辑更健壮
- ✅ 官解通道：多接口并发竞速 + AI 学习自动排序，响应速度和成功率大幅提升
- ✅ 后台：新增快速同步按钮、性能统计接口

## v5.6.0 (2026-07-18)

### 小版本更新 - 核心逻辑补充：官解走虾米接口，官替走 AI 去广告/去插播/去水印

#### 更新内容

##### 1. 官解通道 (official) - 调用虾米接口输出可播放链接

明确官解通道的核心逻辑：
- 调用虾米官解接口（`parse_internal_xiami`）→ 返回 m3u8/mp4 直链
- 下载 m3u8 内容 → 规则引擎 + AI 识别广告 → 生成去广告 m3u8
- 输出最终可播放的链接（clean.php 代理地址）

新增独立函数 `parseVideoByOfficialChannel()` 封装官解处理流程。

##### 2. 官替通道 (replace) - 从资源站匹配 + AI 去广告/去插播/去水印

明确官替通道的核心逻辑：
- 从资源站中匹配对应视频 → AI 自动失败重试 + 智能匹配 → 输出对应链接
- 下载 m3u8 内容 → AI 自动去广告 + 去插播 + 去水印 → 生成清洁 m3u8
- 输出最终播放链接（clean.php 代理地址）

新增独立函数 `parseVideoByReplaceChannel()` 封装官替处理流程：
- 步骤1：下载内容获取真正的 m3u8 直链（解析 mxjx 代理/资源站页面）
- 步骤2：解析 master playlist 获取真实 TS 播放列表
- 步骤3：判断是否为 m3u8 格式
- 步骤4：AI 自动去广告 + 去插播 + 去水印（强制启用 AI 增强模式）
- 步骤5：生成清洁 m3u8，输出最终播放链接

##### 3. AdFilter 增强 - 支持去插播和去水印识别

新增两条规则识别：
- **规则5：插播检测** - 单个分段超过 60s 且紧邻不连续标记，可能是片头/片尾插播（置信度 +0.25）
- **规则6：水印/角标检测** - URL 含 watermark/logo/burn/overlay 字样（置信度 +0.2）

AI 提示词增强：从只识别广告扩展为识别三类异常：
- 广告：URL含广告关键词、不同CDN域名、时长符合广告特征
- 插播：片头/片尾超长片段、不连续标记后的独立片段序列
- 水印：URL含水印/角标特征

##### 4. 配置增强

`xt/config.php` 新增两项规则配置：
- `insertion_check_enabled` - 是否启用插播检测（默认 true）
- `watermark_check_enabled` - 是否启用水印检测（默认 true）
- `watermark_keywords` - 水印/角标 URL 关键词列表

#### 版本号同步

- `version.php`：v5.5.9 → v5.6.0
- `xt/config.php`：5.1.8 → 5.2.0

#### 影响范围

- ✅ 官解通道：明确调用虾米接口 + xt 去广告流程，行为不变但代码结构更清晰
- ✅ 官替通道：增强 AI 去广告/去插播/去水印能力，输出最终清洁播放链接
- ✅ AdFilter：支持更多异常类型识别，提升官替通道的清洁度
- ✅ 配置：新增插播/水印检测开关，可独立控制

---

## v5.5.9 (2026-07-18)

### 小版本优化 - 官替通道返回直连播放地址

#### 问题现象

后台「嗅探设置」切到官替接口通道时，播放地址仍为 `http://114.134.184.91:9002/xt/clean.php?id=xxx` 代理地址，播放器无法播放。

#### 根因分析

官替接口（`official_replace/info`）返回的两个字段含义完全不同：

| 字段 | 含义 | 是否可直接播放 |
|------|------|---------------|
| `m3u8_url` | 资源站视频页面 URL（如 `https://xxx.com/video/abc.html`） | ❌ 播放器无法直接播放 |
| `ad_skip_url` | `mx.php?action=mxjx&deep=1&url=xxx` 代理地址 | ❌ 播放器无法加载 PHP 代理 |

之前代码优先取 `m3u8_url`，播放器无法播放；即使取 `ad_skip_url`，播放器也无法加载 PHP 代理地址。

#### 修复方案

1. **`getVideoLinkFromApiEntry()` 官替字段优先级调整**
   - 原：`m3u8_url` → `ad_skip_url`（取到的是页面 URL，不可播放）
   - 新：`ad_skip_url` → `m3u8_url`（优先取 mxjx 代理，后续内部解析）

2. **`parseVideo()` 官替通道处理逻辑重写**
   - 下载 mxjx 代理返回的 m3u8 内容
   - 通过 `resolveMultiLevelM3u8()` 解析 master playlist 获取真实 TS 播放列表
   - 通过 `extractVideoUrl()` 从内容中提取真正的 m3u8/mp4 直链
   - **直接返回直链**，不生成 `clean.php` 代理

3. **版本号同步**
   - `version.php`：v5.5.8 → v5.5.9
   - `xt/config.php`：5.1.7 → 5.1.8

#### 影响范围

- ✅ 官替通道：返回真正的 m3u8/mp4 直链，播放器可直接播放
- ✅ 官解通道：行为不变，仍走 xt 去广告流程
- ✅ Fallback：自动适配
- ✅ 旧 `official_apis` 数组：行为不变

---

## v5.5.8 (2026-07-18)

### 小版本优化 - 修复走官替接口时播放地址不可播放的问题

#### 问题现象

后台「嗅探设置」切到**官替接口**通道时，解析返回的播放地址为
`http://114.134.184.91:9002/xt/clean.php?id=b8e1cab38badd285`，播放器无法播放。

#### 根因

官替接口（`mx.php?action=official_replace/info&url=`）返回的 `m3u8_url` / `ad_skip_url`
**本身已经是去广告的播放地址**（由本项目 `mxjx` 代理生成）。
但 `parseVideo()` 没有区分通道来源，把它当成原始 m3u8 又走了一次 xt 的去广告流程：

```
官替返回 m3u8_url (已去广告)
   → fetchM3u8Content 下载
   → AdFilter 再次过滤
   → saveCleanM3u8 生成 clean.php?id=xxx 代理
   → 返回嵌套代理地址（不可播放）
```

代理地址嵌套 + 路径解析错乱，导致最终播放地址无法被播放器加载。

#### 修复

1. **`xt/server.php` - `getVideoLinkBySnifferMode()` 返回值改为结构化数组**
   - 原：`?string`（仅返回视频直链）
   - 新：`array{ url: string|null, source: 'official'|'replace'|null }`
   - 通过 `source` 字段告知调用方实际命中的是哪条通道（包含 fallback 后的真实通道）

2. **`xt/server.php` - `parseVideo()` 按通道分流处理**
   - `source === 'replace'`：官替返回的已是去广告地址，**直接透传**，写入缓存后返回
   - `source === 'official'`：官解返回的是原始 m3u8，继续走原有的 xt 去广告流程
     （fetchM3u8Content → AdFilter → saveCleanM3u8 → clean.php 代理）
   - fallback 场景自动正确：官替失败 fallback 到官解时走官解流程，反之亦然

3. **版本号同步**
   - `version.php`：v5.5.7 → v5.5.8
   - `xt/config.php`：5.1.6 → 5.1.7

#### 影响范围

- ✅ 走官解通道：行为不变，仍走 xt 去广告 + clean.php 代理
- ✅ 走官替通道：修复后直接返回官替的去广告 m3u8_url，可正常播放
- ✅ Fallback：自动适配，无需额外配置
- ✅ 旧 `official_apis` 数组：归为 official 通道，行为不变

---

## v5.5.7 (2026-07-18)

### 小版本更新 - 后台新增「嗅探设置」

1. **新增「嗅探设置」后台页面**
   - 位置：后台 → 接口工具 → 嗅探设置（🔍 图标）
   - 用于控制超级嗅探模块（`xt/`）走哪条解析通道
   - 支持两种解析通道，可任意切换：
     - **官解解析（official）**：调用官方解析 API 获取 m3u8/mp4 直链
     - **官替接口（replace）**：调用官替 API 获取资源站匹配后的 m3u8
   - 两个接口各配一个独立开关，再通过「当前通道」单选决定实际走哪一条
   - 当前通道失败时自动 fallback 到另一条已启用的通道
   - 内置测试入口，可直接在页面里输入视频链接验证当前嗅探设置效果

2. **后台页面交互细节**
   - 官解/官替各一个配置卡片：开关 + 接口名称 + 接口地址 + 接口类型（redirect/json/text）+ URL 字段名
   - 实时状态徽章：显示每个接口是「未启用 / 已启用 / 当前通道」
   - 切换开关或当前通道时徽章颜色实时变化
   - 官替接口地址留空时自动使用本项目官替接口 `mx.php?action=official_replace/info&url=`
   - 保存成功后自动重新加载并显示更新时间

3. **新增配置文件 `xt/sniffer_config.php`**
   - 由后台「嗅探设置」页面自动读写
   - 结构：`{ mode, official_api{enabled,name,url,type,url_field,headers}, replace_api{...}, update_date }`
   - 兼容旧版本：文件不存在时使用 `xt/config.php` 中的默认值

4. **`xt/server.php` 路由逻辑重构**
   - 新增 `getVideoLinkBySnifferMode()`：根据嗅探设置选择走官解还是官替
   - 抽取 `getVideoLinkFromApiEntry()` 为通用单接口调用函数（被旧逻辑和新嗅探路由复用）
   - `callSingleApi()` 包装单个接口配置后调用通用函数
   - JSON 类型解析增强：兼容官替接口返回结构 `{success, m3u8_url, ad_skip_url}`
   - 两个通道都未启用时自动 fallback 到旧的 `official_apis` 数组，保证向后兼容

5. **新增 API 端点**
   - `GET  /mx.php?action=sniffer/config`       — 获取嗅探设置（合并默认值）
   - `POST /mx.php?action=sniffer/config/save`  — 保存嗅探设置（白名单字段 + 写入 `xt/sniffer_config.php`）

6. **`xt/config.php` 同步更新**
   - 新增 `sniffer` 配置段（作为 `sniffer_config.php` 不存在时的兜底默认值）
   - 模块版本号 5.1.5 → 5.1.6

#### 影响文件

- 新增 [xt/sniffer_config.php](file:///workspace/xt/sniffer_config.php) — 嗅探设置配置文件（后台自动维护）
- 修改 [xt/config.php](file:///workspace/xt/config.php) — 新增 sniffer 默认配置段，版本号 5.1.6
- 修改 [xt/server.php](file:///workspace/xt/server.php) — 新增嗅探路由 + 抽取通用接口调用函数
- 修改 [mx.php](file:///workspace/mx.php) — 新增 sniffer/config 和 sniffer/config/save 两个 API 端点
- 修改 [mxadmin.php](file:///workspace/mxadmin.php) — 新增「嗅探设置」后台页面 + 侧边栏菜单 + JS 逻辑
- 修改 [version.php](file:///workspace/version.php) — 版本号升级到 v5.5.7
- 修改 [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志
- 修改 [README.md](file:///workspace/README.md) — 功能特性新增「嗅探设置」说明

---

## v5.5.6 (2026-07-18)

### Bug 修复 - 平台适配器方法可见性错误

1. **修复 `mx.php?action=api/v2&type=official` 报错问题**
   - 报错信息：`Access level to TencentVideoAdapter::chineseToNumber() must be protected (as in class AbstractPlatformAdapter) or weaker`
   - 原因：子类 `chineseToNumber()` 声明为 `private`，父类 `AbstractPlatformAdapter::chineseToNumber()` 是 `protected`，PHP 不允许子类把方法可见性改得更严格
   - 修复：将以下 4 个适配器的 `chineseToNumber()` 方法从 `private` 改为 `protected`，与父类保持一致

2. **影响文件**
   - [pt/TencentVideoAdapter.php](file:///workspace/pt/TencentVideoAdapter.php#L916) — `chineseToNumber()` private → protected
   - [pt/MgtvAdapter.php](file:///workspace/pt/MgtvAdapter.php#L420) — `chineseToNumber()` private → protected
   - [pt/BilibiliAdapter.php](file:///workspace/pt/BilibiliAdapter.php#L389) — `chineseToNumber()` private → protected
   - [pt/SohuAdapter.php](file:///workspace/pt/SohuAdapter.php#L397) — `chineseToNumber()` private → protected
   - [version.php](file:///workspace/version.php) — 版本号升级到 v5.5.6

---

## v5.5.5 (2026-07-17)

### 版本升级

1. **版本号升级**
   - `version.php` 版本号从 v5.1.5 升级到 v5.5.5
   - commit 标识更新为 `v5.5.5`
   - 更新时间更新为 2026-07-17

2. **功能汇总**
   - 浏览器适配功能：`xt/clean.php` 支持 Edge、Chrome、Firefox、Safari 等主流浏览器直接访问
   - TVBox/影视App专用解析接口 `jiexi.php`
   - 超级嗅探模块 `xt/`：官解接口对接、规则引擎 + AI 大模型双重广告识别
   - 修复去广告 m3u8 无法播放问题（ts 相对路径转绝对路径）

#### 影响文件

- [version.php](file:///workspace/version.php) — 版本号升级到 v5.5.5
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.1.5 (2026-07-17)

### 小版本更新 - 新增浏览器适配功能

- `xt/clean.php` 支持 Edge、Chrome、Firefox、Safari 等主流浏览器直接访问
- 浏览器访问时自动显示 HTML 播放器页面，支持在线播放 m3u8 视频
- 保留原有 m3u8 直链模式，播放器调用不受影响
- 新增浏览器检测和标识显示功能

---

## v5.1.0 (2026-07-16)

### 重大变更：移除视频嗅探模块

1. **移除文件**
   - 删除 `api.php`（视频解析API入口）
   - 删除 `server.php`（视频解析服务端）

2. **保留功能**
   - M3U8 广告分析与去广告系统（核心功能）
   - 后台管理页面（mxadmin.php）
   - 其他模块正常使用

#### 影响文件

- `api.php` — 已删除
- `server.php` — 已删除
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.1.0
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.10 (2026-07-16)

### 终极方案：JSONP方式绕过CORS（谁调用用谁IP）

1. **核心原理**：腾讯API支持JSONP回调（`QZOutputJson=xxx;`），通过 `<script>` 标签加载不受CORS限制
2. **工作流程**：
   - 阶段1：服务器生成腾讯API请求URL（含callback参数）
   - 阶段2：客户端浏览器用 `<script>` 标签直接加载腾讯API（出口IP=客户端国内IP）
   - 阶段3：客户端将API返回数据回传给服务器，服务器处理返回视频URL

3. **优势**：
   - 完全绕过CORS限制（`<script>` 标签不受同源策略约束）
   - 出口IP是客户端的真实国内IP，腾讯必然返回em=0
   - 用户直接访问即可，无需任何额外操作
   - 服务器只负责生成参数和处理结果，不参与API请求

4. **关键改动**：
   - `api.php`：完全重写，腾讯视频使用JSONP方式处理
   - `api.php`：新增 `handleTencentVideo()` 函数，生成JSONP请求页面
   - `api.php`：新增 `processTencentApiData()` 函数，处理阶段2回传的数据

#### 影响文件

- [api.php](file:///workspace/api.php) — JSONP方式处理腾讯视频
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.10
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.9 (2026-07-16)

### 终极方案：用户IP注入（解决CORS跨域问题）

1. **问题根因：CORS跨域阻止浏览器直接请求腾讯API**
   - v5.0.8 的客户端直连方案在浏览器中因CORS策略失败
   - 浏览器不允许直接跨域请求 `vv.video.qq.com`

2. **新方案：服务器代理转发 + 用户IP注入**
   - 核心原理：用户在国内访问海外服务器，服务器能获取到用户的真实国内IP
   - 服务器转发腾讯API请求时，将用户的国内IP注入到 `X-Forwarded-For/Client-IP/X-Real-IP` 请求头
   - 腾讯按注入的国内IP鉴权，返回 `em=0`

3. **工作流程**
   ```
   国内用户 → 海外服务器(api.php) → server.php → 提取用户国内IP → 注入X-Forwarded-For → 腾讯API(em=0) → 返回视频URL
   ```

4. **关键改动**
   - `server.php`: 新增 `getUserRealIp()` 函数，从 `REMOTE_ADDR/X-Forwarded-For/X-Real-IP/Client-IP` 提取用户真实IP
   - `server.php`: 新增 `curlGetWithUserIp()` 函数，转发请求时注入用户IP
   - `server.php`: 新增 `extractTencentVideoWithProxy()` 函数，使用用户IP注入模式解析腾讯视频
   - `server.php`: 新增 `proxyRequest()` 函数，提供API代理转发端点（`?action=proxy&url=xxx`）

5. **优势**
   - 用户无需任何操作，直接访问即可解析
   - 无需前端JS处理，纯服务器端完成
   - 支持所有平台（腾讯/爱奇艺/优酷/芒果TV）
   - 自动适配国内外服务器

#### 影响文件

- [server.php](file:///workspace/server.php) — 用户IP注入方案、代理转发功能
- [api.php](file:///workspace/api.php) — 简化为透传模式
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.9
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.8 (2026-07-16)

### 终极方案：客户端直连腾讯API（解决免费代理全部失效问题）

1. **问题根因：免费代理全部失效**
   - v5.0.7 的国内代理池轮询方案在靶机测试中所有代理请求失败（404/超时/连接拒绝）
   - 免费代理生命周期极短，且大部分已被滥用或封禁

2. **新方案：两阶段客户端直连**
   - **阶段1**：服务器生成腾讯API请求参数（URL、UA、referer、guid等），返回 `code:206`
   - **阶段2**：客户端（国内浏览器）直接调用腾讯API（出口IP为国内，必然返回 em=0），将结果回传给服务器
   - **阶段3**：服务器处理API响应，提取视频URL并返回

3. **工作流程**
   ```
   国内客户端 → api.php?url=xxx → server.php 返回阶段1任务
   国内客户端 → 直接调用腾讯API（em=0）→ api.php?phase=2&api_data=xxx → 返回视频URL
   ```

4. **关键改动**
   - `server.php`：新增 `generateTencentApiRequests()` 和 `processTencentApiData()` 函数
   - `server.php`：检测到腾讯视频时，返回 `code:206` + 任务参数（而非直接解析）
   - `server.php`：添加完整 CORS 响应头，允许客户端跨域调用腾讯API
   - `api.php`：支持 `phase=2` 参数透传

5. **前端集成**
   ```javascript
   // 前端需要处理 code:206 的响应
   async function parseVideo(url) {
       const resp = await fetch(`api.php?url=${encodeURIComponent(url)}`);
       const data = await resp.json();
       
       if (data.code === 206) {
           // 阶段2：客户端直接调用腾讯API
           for (const req of data.task.requests) {
               const apiResp = await fetch(req.url, {
                   headers: { 'User-Agent': req.ua, 'Referer': req.referer }
               });
               const text = await apiResp.text();
               const apiData = JSON.parse(text.replace(/^QZOutputJson=/, '').replace(/;$/, ''));
               
               if (apiData.em === 0) {
                   const result = await fetch(`${data.task.callback}&api_data=${btoa(JSON.stringify(apiData))}&guid=${data.task.guid}`);
                   return await result.json();
               }
           }
       }
       return data;
   }
   ```

#### 影响文件

- [server.php](file:///workspace/server.php) — 两阶段解析方案、CORS响应头
- [api.php](file:///workspace/api.php) — phase=2 参数透传、CORS响应头
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.8
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.7 (2026-07-16)

### 最终方案：国内 HTTP/SOCKS5 代理池轮询（解决 X-Forwarded-For 失效问题）

1. **问题根因：X-Forwarded-For 伪造被腾讯新版 API 检测**
   - v5.0.6 的 X-Forwarded-For 方案在靶机测试中仍返回 `em=80`，说明腾讯已升级检测机制：
     - 不再信任简单的请求头伪造
     - 可能检测真实 TCP 源 IP 或要求可信代理白名单
     - 结合 TLS 指纹等多维度判断

2. **新方案：真实国内代理池轮询**
   - 直接通过国内 HTTP/SOCKS5 代理访问腾讯 API，出口 IP 为国内
   - 代理来源：[proxy.scdn.io](https://proxy.scdn.io/?country=%E4%B8%AD%E5%9B%BD) 中国区免费代理
   - 内置 31 个国内代理（HTTP + SOCKS5），按响应时间排序轮询

3. **`curlGet()` 新增 `proxy` 选项**
   - 支持 `http://IP:PORT` 和 `socks5://IP:PORT` 两种格式
   - 自动检测协议类型，设置 `CURLOPT_PROXYTYPE`
   - 与现有 `spoof_ip`、`headers` 选项兼容

4. **代理池结构**
   ```
   第1批：响应时间 18-75ms（免费代理，可能失效）
   第2批：响应时间 335-500ms
   第3批：阿里云/腾讯云主机代理（相对稳定）
   SOCKS5：202.141.161.53:10808（更稳定）
   ```

5. **建议**
   - 免费代理稳定性差，建议使用**付费代理**或**自建国内 VPS 中转**
   - 如需稳定解析，可在国内 VPS 部署简单代理转发脚本

#### 解析流程（不变）

```
方案零：官方API + 国内代理池（出口IP为国内）
   ↓ 失败
方案一：第三方JSON解析接口
   ↓ 失败
方案二：第三方HTML解析接口
   ↓ 失败
方案三：Chrome Headless 嗅探
```

#### 影响文件

- [server.php](file:///workspace/server.php) — curlGet 新增 proxy 选项；腾讯解析改用国内代理池轮询
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.7
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.6 (2026-07-16)

### 关键修复：海外服务器腾讯视频 em=80 彻底解决（X-Forwarded-For 伪造国内IP）

1. **核心方案：HTTP 请求头注入国内 IP，绕过腾讯地域版权限制**
   - 问题根因：v5.0.5 的 CORS 代理方案在靶机对抗测试中全军覆没 —— 公共 CORS 代理（allorigins / corsproxy / proxy.cors.sh）的出口 IP 也都在海外，腾讯 API 对它们同样返回 `em=80`。
   - 新方案：直接在请求腾讯 API 时注入 `X-Forwarded-For` / `Client-IP` / `X-Real-IP` / `Forwarded` 四个请求头，让腾讯 API 按伪造的国内 IP 进行地域鉴权，返回 `em=0`。
   - 验证：本地 curl 测试，注入 `220.181.38.148` 后腾讯 API 返回 `em=0` 并正常下发 `fvkey`。

2. **国内 IP 池轮询机制**
   - 内置 10 个国内主流 IP（百度 / 电信 / 联通 / 移动 / 腾讯云骨干网）
   - 每次调用腾讯 API 轮换一个 IP，规避单 IP 被风控的可能
   - 涵盖北京、上海、广东、江苏等主要地域

3. **`curlGet()` 工具函数新增 `spoof_ip` 选项**
   - 通用化设计，所有调用方均可按需注入 IP 头
   - 自动校验 IP 格式（`filter_var`），非法 IP 不注入
   - 与现有 `headers` 选项合并，互不覆盖

4. **代码瘦身**
   - 移除已废弃的 `extractVideoByProxyApi()` 函数（方案零B）
   - 移除 `extractTencentVideo()` 的 `$useProxy` 参数和代理分支逻辑
   - 移除 CORS 代理列表（allorigins / corsproxy / proxy.cors.sh）
   - 删除调试用的临时文件 `test_decode.php`

#### 解析流程（4 层回退，简化结构）

```
方案零：官方API直连 + X-Forwarded-For 注入国内IP（绕过 em=80）
   ↓ 失败
方案一：第三方JSON解析接口（4个接口轮询）
   ↓ 失败
方案二：第三方HTML解析接口（5个接口轮询）
   ↓ 失败
方案三：Chrome Headless 嗅探（最后防线）
```

#### 影响文件

- [server.php](file:///workspace/server.php) — curlGet 新增 spoof_ip 选项；腾讯解析改用 X-Forwarded-For；移除代理方案
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.6
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.5 (2026-07-15)

### 重大更新：国内外服务器自动适配

1. **5 层自动回退机制，确保任何地区都能解析**
   ```
   方案零：官方API直连（国内IP最快，em=0直接成功）
      ↓ 失败(em=80)
   方案零B：CORS代理转发官方API（海外IP自动回退）
      代理列表：allorigins.win → corsproxy.io → proxy.cors.sh
      ↓ 失败
   方案一：第三方JSON解析接口（4个接口轮询）
      ↓ 失败
   方案二：第三方HTML解析接口（5个接口轮询）
      ↓ 失败
   方案三：Chrome Headless 嗅探（最后防线）
   ```

2. **重构代码架构**
   - 提取统一的 `curlGet()` 工具函数，消除重复代码
   - 腾讯解析函数支持 `直连/代理` 双模式参数
   - 第三方解析拆分为 JSON 接口和 HTML 接口两个独立方案
   - 每层方案独立记录调试日志，便于诊断

3. **代理模式工作原理**
   - 海外服务器直连腾讯API返回 `em=80`（版权限制）
   - 通过公共CORS代理转发请求，代理服务器在国内，em=0
   - 3个代理自动轮询，任一可用即成功

#### 影响文件

- [server.php](file:///workspace/server.php) — 完全重构，5层回退+代理模式
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.5
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.4 (2026-07-15)

### 深度修复

1. **根本性修复腾讯视频 em=80 版权限制**
   - 问题根因：腾讯 API 通过请求来源判断地域版权，缺少 `ehost` 参数导致返回 `em=80`
   - 解决方案：所有 API 请求添加 `ehost` 参数（PC端 `v.qq.com` / 移动端 `m.v.qq.com`）
   - 本地验证：添加 `ehost` 后 `em=0`，成功获取视频信息

2. **优化 vkey 获取流程**
   - 优先使用 `getinfo` 返回的 `fvkey`，无需再调 `getkey` 接口
   - `fvkey` 为空时自动回退到 `getkey` 接口
   - 减少一次 HTTP 请求，提升解析速度

3. **简化 CDN 验证逻辑**
   - 移除逐个 CDN 服务器 HEAD 验证（耗时且不必要）
   - 直接返回第一个服务器地址，由播放器处理

#### 影响文件

- [server.php](file:///workspace/server.php) — 添加 ehost 参数 + fvkey 优化
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.4
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.3 (2026-07-15)

### 修复

1. **修复服务器 IP 版权限制（em=80）导致解析失败**
   - 问题：腾讯 API 返回 `em=80`，提示"您所在区域暂无此内容版权"
   - 新增多 API 端点轮询：PC端 `vv.video.qq.com` + H5移动端 `h5vv.video.qq.com`
   - 使用移动端 UA 访问 H5 API，可能绕过部分地域限制
   - 新增多清晰度自动回退：shd → fhd → hd → sd → msd

2. **新增 JSON 直接返回的第三方解析接口**
   - 优先尝试 `jx.xmflv.com?type=json`、`yparse.ik9.cc?type=json` 等 JSON 接口
   - JSON 接口不依赖 JS 渲染，cURL 可直接获取视频地址
   - 支持多种 JSON 字段名解析（url/video/src/play/m3u8/mp4/data）

#### 影响文件

- [server.php](file:///workspace/server.php) — 多API端点轮询 + JSON解析接口
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.3
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.2 (2026-07-15)

### 修复

1. **修复腾讯视频解析失败问题**
   - 问题：第三方解析接口（jx.xmflv.com）使用 JavaScript 动态加载视频地址，cURL 无法执行 JS 导致解析失败
   - 新增**方案零：直接调用平台官方 API**，优先于第三方解析接口
   - 腾讯视频：通过 `getinfo` + `getkey` 两步 API 直接获取 MP4 视频直链
     - 自动提取视频 ID（支持 cover/page/iframe 多种 URL 格式）
     - 使用随机 GUID 生成鉴权 token
     - 遍历多个 CDN 服务器，自动验证 URL 可访问性
   - 爱奇艺：调用 `pcw-api` 获取视频播放地址
   - 优酷：调用 `ups.youku.com` 获取 M3U8/MP4 流地址
   - 芒果TV：调用 `pcweb.api.mgtv.com` 获取播放地址

2. **增加更多备用解析接口**
   - 新增 jx.m3u8.tv、jx.parwix.com、jx.jsonplayer.com 三个备用接口
   - 提升第三方解析回退成功率

#### 解析流程（三层策略）

```
方案零：平台官方 API（腾讯/爱奇艺/优酷/芒果）→ 最快最稳定
   ↓ 失败时
方案一：cURL + 正则解析（5个第三方接口轮询）→ 兜底
   ↓ 失败时
方案二：Chrome Headless 嗅探 → 最后防线
```

#### 影响文件

- [server.php](file:///workspace/server.php) — 新增平台直接 API 解析、增加备用解析接口
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.2
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v5.0.1 (2026-07-15)

### 修复

1. **管理后台顶部显示问题修复**
   - 修复桌面端顶部 header 显示异常的问题（v3 样式覆盖导致白色背景、文字过小）
   - 恢复渐变色顶部栏设计，与移动端风格保持一致
   - 修复背景图模式下顶部栏样式不一致的问题
   - 优化顶部栏布局，确保标题、主题切换按钮正确显示

#### 影响文件

- [mxadmin.php](file:///workspace/mxadmin.php) — 修复顶部 header 样式
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.1

---

## v5.0.0 (2026-07-15)

### 大版本更新 - 全面深度优化 & API 文档完善

#### 新增

1. **API 文档全面完善**
   - 在 [api_doc.php](file:///workspace/api_doc.php) 文档最前面添加**完整接口索引**，包含全部 95+ 个接口、20 个功能模块
   - 新增 **PT 引擎** 分类文档（pt/status、pt/test、pt/adskip）
   - 新增 **AI 智能** 分类文档（ai/smart_process、ai/pro_detect、ai/skip、ai/insert_detect、ai/subtitle_detect、ai/md5_analyze、ai/md5_detect 等 7 个接口）
   - 新增 **广告特征码** 分类文档（signatures/list、signatures/add、signatures/delete、signatures/stats、signatures/clean）
   - 新增 **官方站点** 分类文档（official_sites/status、official_sites/list、official_sites/search_all、official_sites/toggle）
   - 新增 **播放器** 分类文档（player/config/save）
   - 新增 **备份管理** 4 个接口文档（update/backup/list、update/backup/create、update/backup/restore、update/backup/delete）
   - 侧边栏导航同步更新，支持快速跳转到各分类

2. **parse/list 接口新增 cache 类型**
   - supported_types 数组中添加 `cache` 类型
   - 说明：缓存型 M3U8 解析（带 vkey 参数的缓存链接）

#### 修复

1. **pt/adskip 接口 M3U8 获取方式优化**
   - 将 `@file_get_contents` 改为 `curl`，增加超时控制
   - 设置 `CURLOPT_TIMEOUT = 15`（总超时）、`CURLOPT_CONNECTTIMEOUT = 5`（连接超时）
   - 增加 `CURLOPT_FOLLOWLOCATION` 支持重定向
   - 增加 SSL 证书验证跳过（兼容自签名证书）
   - 增加浏览器 User-Agent，避免被 CDN 拦截
   - 增加 HTTP 状态码检查，200 以外视为失败
   - 增加 curl 错误信息返回，便于排查问题

2. **mxjx/deep 接口保存空 MD5 问题修复**
   - 原代码 `saveMd5Signatures` 时传入 `'md5' => ''` 空值，导致特征码未实际保存
   - 修复：从 `tsAnalysis['md5_details']` 中构建 URI → MD5 映射表
   - 根据 `deepAdUris` 中的 URI 查找对应的 MD5 值后再保存
   - 没有对应 MD5 的跳过，避免保存无效数据

3. **mxjx/info 接口 file_get_contents 超时问题修复**
   - 将 `file_get_contents($url)` 改为 curl 请求
   - 设置 `CURLOPT_TIMEOUT = 10`、`CURLOPT_CONNECTTIMEOUT = 3`
   - 增加 HTTP 状态码检查，失败时回退到原始结果
   - 增加 SSL 跳过和 UA 设置，提高请求成功率

#### 优化

1. **版本号升级到 v5.0.0**
   - [version.php](file:///workspace/version.php) 版本从 v4.0.0 升级到 v5.0.0
   - commit 标识更新为 `v5-unified-api-pt-engine`

2. **API 文档结构优化**
   - 侧边栏增加"完整接口索引"（ALL 标签）作为第一项
   - 新增分类：完整接口索引、PT引擎、AI智能、广告特征码、官方站点、播放器
   - 接口搜索功能自动适配新增分类

#### 测试验证

- ✅ 所有 PHP 文件语法检查通过（无语法错误）
- ✅ version 接口正常返回 v5.0.0
- ✅ info 接口正常返回系统信息
- ✅ parse/list 接口正常返回，含 cache 类型
- ✅ rules/list 接口正常
- ✅ sites/list 接口正常
- ✅ player/config 接口正常
- ✅ official_replace/config 接口正常
- ✅ db/status 接口正常
- ✅ auth/info 接口正常
- ✅ api_doc.php 页面 200 OK，正常显示
- ✅ player/ 页面 200 OK，支持 18 种播放器切换
- ✅ mxadmin.php 后台页面 200 OK
- ✅ kz/cache.php 缓存解析页面 200 OK

#### 影响文件

- [mx.php](file:///workspace/mx.php) — 修复 pt/adskip、parse/list、mxjx/deep、mxjx/info
- [api_doc.php](file:///workspace/api_doc.php) — 全面完善 API 文档
- [version.php](file:///workspace/version.php) — 版本号升级到 v5.0.0
- [CHANGELOG.md](file:///workspace/CHANGELOG.md) — 更新日志

---

## v4.1.0 (2026-07-15)

### 新增

1. **创建 kz 扩展文件夹 - 缓存型 M3U8 解析器**
   - 新增 [kz/CacheM3u8Parser.php](file:///workspace/kz/CacheM3u8Parser.php) 核心解析类
   - 新增 [kz/cache.php](file:///workspace/kz/cache.php) 解析入口（可直接访问）
   - 专门解析带 `vkey` 鉴权参数的缓存型 M3U8 链接
   - 支持 `https://cache.xxx.xyz:4433/Cache/qq/xxx.m3u8?vkey=xxx` 格式

2. **缓存型 M3U8 解析功能**
   - 代理请求原始 M3U8（带浏览器 UA、防盗链头）
   - 自动重写分片 URL：相对路径→绝对路径
   - 支持多级 M3U8（master playlist / media playlist）
   - 支持 TS 分片代理（防盗链场景，`?proxy=1` 模式）
   - 支持 `#EXT-X-KEY` URI 重写
   - vkey 参数分析（hex 编码识别）

3. **集成到统一解析**
   - [mx.php](file:///workspace/mx.php) 统一解析自动识别缓存型 M3U8 链接
   - 识别规则：host 含 `cache` 或路径含 `/Cache/` 且有 `vkey=` 参数
   - 自动路由到 `kz/cache.php` 进行解析

### kz/cache.php 使用方式

```
# 解析并直接输出可播放 M3U8
kz/cache.php?url=https://cache.xxx.xyz/Cache/qq/xxx.m3u8?vkey=xxx

# 返回 JSON 信息（含分片数、类型等）
kz/cache.php?url=xxx&mode=json

# 代理模式（分片通过本PHP代理，防防盗链）
kz/cache.php?url=xxx&proxy=1

# TS 分片代理
kz/cache.php?ts=https://cache.xxx.xyz/Cache/qq/xxx.ts

# vkey 参数分析
kz/cache.php?vkey=xxx&mode=analyze
```

### 影响文件

- [kz/CacheM3u8Parser.php](file:///workspace/kz/CacheM3u8Parser.php) — 新增，核心解析类
- [kz/cache.php](file:///workspace/kz/cache.php) — 新增，解析入口
- [mx.php](file:///workspace/mx.php) — 集成到统一解析，添加 cache 类型识别
- [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php) → v4.1.0
- [pt/pt_config.php](file:///workspace/pt/pt_config.php) → v4.1.0
- [gz/sites_config.php](file:///workspace/gz/sites_config.php) → v4.1.0
- [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v4.0.4 (2026-07-15)

### 优化

1. **官替搜索关键词以 video_title 和 base_title 为准**
   - 搜索关键词顺序调整：`video_title`（完整标题，如"我只想要个公平 第2集"）作为第1优先搜索词
   - `base_title`（基础标题，如"我只想要个公平"）作为第2优先搜索词
   - 移除 `video_id` 作为搜索词（视频ID在资源站搜不到内容）
   - 季节变体、去标点版本、主标题提取等辅助搜索词保留，但排在 video_title/base_title 之后
   - 同步更新 [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php) 和 [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php)

### 效果示例

```
video_title = "我只想要个公平 第2集"
base_title  = "我只想要个公平"

搜索关键词顺序：
  1. 我只想要个公平 第2集    ← video_title（最优先）
  2. 我只想要个公平          ← base_title（次优先）
```

### 影响文件

- [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php#L415-L501)
- [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php#L525-L594)
- [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php)
- [pt/pt_config.php](file:///workspace/pt/pt_config.php)
- [gz/sites_config.php](file:///workspace/gz/sites_config.php)
- [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v4.0.3 (2026-07-15)

### 优化

1. **手机端蓝色区域（API预览区）适配优化**
   - 手机端（≤480px）蓝色区域 padding 从 24px 32px 减少到 12px 16px，节省垂直空间
   - API URL 行改为垂直排列，下拉选择框占满宽度
   - 公告卡片 padding 从 16px 18px 减少到 12px
   - 标题间距从 14px 减少到 10px

### 影响文件

- [mxadmin.php](file:///workspace/mxadmin.php)
- [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php)
- [pt/pt_config.php](file:///workspace/pt/pt_config.php)
- [gz/sites_config.php](file:///workspace/gz/sites_config.php)
- [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v4.0.2 (2026-07-15)

### 优化

1. **官替搜索结果只显示可用站点，失败站点不展示**
   - 搜索时记录每个资源站的成功/失败状态
   - 成功响应并有搜索结果的站点计入 `successful_sites`
   - 搜索失败/超时/无结果的站点计入 `failed_sites`，不显示在可用列表中
   - `site_matches` 保持只显示有匹配结果的站点（匹配度达标）
   - 新增字段：`successful_sites`（成功搜索的站点列表）、`failed_sites`（失败的站点及原因）、`searched_sites`（总搜索站点数）

2. **DbOfficialReplaceManager 搜索站点计数逻辑优化**
   - 原来 `searched_sites` 只统计有结果的站点，现改为统计实际发起搜索的站点数
   - `max_search_sites` 限制改为按成功站点数限制，避免过早退出
   - 新增 try/catch 捕获单站点搜索异常，不影响整体流程

3. **OfficialReplaceManager 并发搜索增强**
   - `searchSitesConcurrent` 从返回 videos 数组改为返回完整结果数组
   - 多线程模式下每个站点的失败原因都会被记录（HTTP错误/非JSON/无结果/请求失败）
   - 串行兜底模式同样记录成功/失败状态

### 影响文件

- [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php)
- [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php)
- [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php)
- [pt/pt_config.php](file:///workspace/pt/pt_config.php)
- [gz/sites_config.php](file:///workspace/gz/sites_config.php)
- [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v4.0.1 (2026-07-15)

### 优化

1. **官替搜索站点扩展为资源站列表全部 active 站点**
   - 搜索站点从 9 个增加到 34 个，覆盖资源站列表中所有 status=active 的站点
   - 按优先级排序：优先搜索高优先级站点（量子/暴风/非凡等），低优先级站点作为补充
   - max_search_sites 从 10 提升到 40，确保能遍历全部活跃站点
   - 新增站点：6度资源、豆包、快车、闪电、丫丫（鸭鸭）、无尽、速播、豪华、光速、蓝光、魔都、看看、樱花、好花、电影天堂、茅台、13大众、百度、爱奇艺资、牛牛6、蓝志、天逸、如意、天繁、西瓜

2. **同步更新所有配置文件**
   - [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php) 版本 4.0.1
   - [pt/pt_config.php](file:///workspace/pt/pt_config.php) 版本 4.0.1
   - [gz/sites_config.php](file:///workspace/gz/sites_config.php) 版本 4.0.1
   - [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php) 默认配置同步
   - [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php) 默认配置同步

### 影响文件

- [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php)
- [pt/pt_config.php](file:///workspace/pt/pt_config.php)
- [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php)
- [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php)
- [gz/sites_config.php](file:///workspace/gz/sites_config.php)
- [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v4.0.0 (2026-07-15)

### 大版本更新 - 平台官替深度优化

#### 新增

1. **新增 pt 模块化平台适配架构**
   - 创建 `/workspace/pt/` 目录，平台官替规则全部由 pt 模块统一调度
   - 定义 `PlatformAdapterInterface` 接口，规范各平台适配器契约
   - 抽象基类 `AbstractPlatformAdapter` 提供 httpGet/httpGetMobile/extractTitleFromHtml/cleanTitle/calculateBaseScore 等通用工具
   - 核心调度器 `PtManager` 单例模式，注册并管理所有平台适配器，统一调度 resolve/processAdSkip/detectAdapter/analyzeFailure/learnFromMatch
   - 配置文件 `pt/pt_config.php`，包含版本号、搜索站点、匹配阈值、AI 开关等

2. **6 个平台特定适配器，差异化算法**
   - `TencentVideoAdapter`（腾讯视频）：匹配 v.qq.com，提取 vid/cover_id，调用 float_vinfo2 API
   - `IqiyiAdapter`（爱奇艺）：匹配 iqiyi.com，调用 pcw-api baseinfo API
   - `YoukuAdapter.php`（优酷）：正则 `/youku\.com\/.*?id_([a-zA-Z0-9=]+)/i` 支持 `=` 字符
   - `MgtvAdapter`（芒果TV）：匹配 mgtv.com，调用 pcweb.api.mgtv.com
   - `BilibiliAdapter`（哔哩哔哩）：支持 BV 和 av 两种 ID 格式
   - `SohuAdapter`（搜狐视频）：匹配 tv.sohu.com（排除新闻频道），使用 videoinfo JSON API

3. **AI 自动化分析与优化引擎 `PtAIAnalyzer`**
   - 加权评分系统：title_exact(30) / title_similarity(25) / title_contains(15) / season_match(20) / year_match(10)
   - `smartMatch` 0-100 评分，阈值 50，自动排除解说/预告/花絮等非正片内容
   - `learnFromMatch` 根据用户反馈动态调整权重（正确 +0.5，错误 -0.3），学习数据持久化到 `pt/data/ai_learning.json`
   - `analyzeFailure` 在匹配失败时输出诊断建议（标题长度差异/季数不匹配/排除项等）

4. **M3U8 去广告引擎 `PtAdSkipEngine`**
   - 解析 M3U8 播放列表，识别广告分片并替换为空白分片
   - 识别规则：URI 模式（adjump/ad//advertisement 等）、关键词匹配、短时长检测（<2s）
   - 空白分片使用 `data:video/mp2t;base64,...` 最小 TS 数据，避免黑屏闪烁
   - 安全防护：广告占比 >40% 且无内容分片时保留原始 M3U8，避免误判清空正片
   - 通过 `mx.php?action=pt/adskip&url=...` API 调用，提供处理后内容

5. **新增 pt 管理 API 端点**（通过 `action` 参数访问）
   - `mx.php?action=pt/status` — 查看 pt 引擎状态、已注册适配器
   - `mx.php?action=pt/test&url=...` — 测试 pt 引擎识别与匹配
   - `mx.php?action=pt/adskip&url=...` — 调用去广告引擎处理 M3U8

#### 优化

1. **官替调用入口集成 pt 规则**
   - `DbOfficialReplaceManager` 和 `OfficialReplaceManager` 在 AI 匹配 + 规则兜底后，当无匹配或分数 <60 时调用 `PtManager::resolve()` 进行平台特定算法重匹配
   - pt 引擎异常时静默降级到原有匹配结果，保证稳定性
   - 匹配方法标记 `pt_<platform>`，便于追溯

2. **统一版本号到 4.0.0**
   - `official_replace_config.php` / `sites_config.php` / `pt_config.php` 版本号同步

#### 影响文件

- 新增 [pt/PlatformAdapterInterface.php](file:///workspace/pt/PlatformAdapterInterface.php)
- 新增 [pt/AbstractPlatformAdapter.php](file:///workspace/pt/AbstractPlatformAdapter.php)
- 新增 [pt/PtManager.php](file:///workspace/pt/PtManager.php)
- 新增 [pt/PtAIAnalyzer.php](file:///workspace/pt/PtAIAnalyzer.php)
- 新增 [pt/PtAdSkipEngine.php](file:///workspace/pt/PtAdSkipEngine.php)
- 新增 [pt/TencentVideoAdapter.php](file:///workspace/pt/TencentVideoAdapter.php)
- 新增 [pt/IqiyiAdapter.php](file:///workspace/pt/IqiyiAdapter.php)
- 新增 [pt/YoukuAdapter.php](file:///workspace/pt/YoukuAdapter.php)
- 新增 [pt/MgtvAdapter.php](file:///workspace/pt/MgtvAdapter.php)
- 新增 [pt/BilibiliAdapter.php](file:///workspace/pt/BilibiliAdapter.php)
- 新增 [pt/SohuAdapter.php](file:///workspace/pt/SohuAdapter.php)
- 新增 [pt/pt_config.php](file:///workspace/pt/pt_config.php)
- 修改 [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php)
- 修改 [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php)
- 修改 [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php)
- 修改 [gz/sites_config.php](file:///workspace/gz/sites_config.php)
- 修改 [mx.php](file:///workspace/mx.php)
- 修改 [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v3.2.19 (2026-07-15)

### 修复

1. **修复标题清理未生效导致官替无法匹配资源的问题**
   - 问题：extractPureTitle 中书名号正则缺少 `u` 修饰符，导致无法从《阿凡达：水之道》中提取纯标题
   - 问题：cleanTitle 字符类未包含书名号《》，无法清理"《阿凡达：水之道》充满创意和想象力"中的描述文字
   - 修复：
     - 为书名号正则添加 `u` 修饰符
     - 在 cleanTitle 字符类中添加《》
     - 将 extractPureTitle 提前到 cleanTitle 最开始执行
     - 在 resolve 中显式调用 cleanTitle 清理视频标题
     - 在 parseVideoTitle 中也调用 cleanTitle 作为防御性处理

2. **修复匹配阈值过高导致"搜索到但无法匹配"的问题**
   - 问题：默认匹配阈值 60，但数据库/配置中可能设置为 100，导致资源站能搜到但无法匹配
   - 修复：
     - 将默认匹配阈值从 60 调整为 75
     - 添加最佳努力匹配机制：当最佳匹配分数 >= 50 时即使未达阈值也返回
     - 改进 calculateBaseMatchScore：高字符相似度（>=80%）时直接给高分，避免"阿凡达2" vs "阿凡达"这类单个数字差异导致漏配

3. **优化搜索站点和关键词**
   - 默认搜索站点从 5 个增加到 9 个：量子、暴风、非凡、天影、猫眼、最大、索尼、OK资源、红牛
   - 最大搜索站点数从 5 增加到 10
   - buildSearchKeywords 增加去标点版本和主标题提取（如"阿凡达：水之道"会额外搜索"阿凡达水之道"和"阿凡达"）

4. **修复文件版官替默认配置中的优酷正则**
   - OfficialReplaceManager.php 默认配置中优酷 pattern 仍不支持 `=` 字符
   - 已同步修复为 `/youku\.com\/.*?id_([a-zA-Z0-9=]+)/i`

### 影响文件

- [db/DbOfficialReplaceManager.php](file:///workspace/db/DbOfficialReplaceManager.php)
- [gz/OfficialReplaceManager.php](file:///workspace/gz/OfficialReplaceManager.php)
- [gz/official_replace_config.php](file:///workspace/gz/official_replace_config.php)
- [gz/sites_config.php](file:///workspace/gz/sites_config.php)
- [CHANGELOG.md](file:///workspace/CHANGELOG.md)

---

## v3.2.18 (2026-07-15)

### 修复

1. **修复官替API返回404状态码导致nginx拦截问题**
   - 问题：官替接口在解析失败时返回 HTTP 404 状态码，nginx 配置了 `error_page 404` 后会用默认404 HTML页面替换JSON响应
   - 现象：前端显示"服务器返回非JSON响应"，响应内容为 nginx 404 页面
   - 修复：将业务逻辑失败的响应状态码统一改为 200，通过 JSON 中的 `success` 字段表示业务成功/失败
   - 影响文件：mx.php（official_replace/resolve、official_replace/info）、index.php（统一解析接口）

2. **同步DbOfficialReplaceManager标题处理逻辑**
   - 数据库版官替管理器的 cleanTitle 方法添加空值检查
   - 新增 extractPureTitle 方法，与文件版保持一致
   - 支持从书名号、引号、中文标点中提取纯标题

### 优化

- 更新版本号至 3.2.18

---

## v3.2.17 (2026-07-15)

### 修复

1. **修复优酷视频ID识别问题**
   - 配置文件中优酷正则表达式 `/youku\.com\/.*?id_([a-zA-Z0-9]+)/i` 不支持包含 `=` 字符的视频ID
   - 修复后正则表达式改为 `/youku\.com\/.*?id_([a-zA-Z0-9=]+)/i`，支持 Base64 编码的视频ID
   - 示例链接: `https://v.youku.com/v_show/id_XNTk1MjU3NzQ4NA==.html`

2. **改进标题提取和清理逻辑**
   - 新增 `extractPureTitle()` 方法，提取纯标题内容
   - 支持从书名号 `《》` 中提取标题
   - 支持从引号 `"` 中提取标题
   - 支持按中文标点（逗号、句号、感叹号、问号）分割标题
   - 支持按破折号分割标题
   - 修复了标题包含描述性文字影响搜索匹配的问题

3. **增强视频信息获取API备用方案**
   - 腾讯视频：新增多个API地址和HTML页面地址
   - 优酷：新增移动端页面和播放列表API
   - 提高视频信息获取成功率

4. **修复官替搜索站点配置**
   - 配置文件中 `search_sites` 为空数组，导致无法搜索资源站
   - 添加默认搜索站点: 量子、暴风、非凡、天影、猫眼、最大、索尼、OK资源

5. **修复正则表达式语法错误**
   - 修复 `extractPureTitle()` 方法中引号正则缺少结束分隔符的问题

### 优化

- 优化 `cleanTitle()` 方法，增加空值检查
- 优化搜索关键词构建逻辑，提高匹配准确性
- 更新版本号至 3.2.17

---

## v3.2.16 (2026-07-13)

- 修复官替资源广告插槽未去干净的问题

---

## 历史版本

- v3.2.x: 官替功能增强版本
- v3.1.x: 核心功能优化版本
- v3.0.x: 重构版本
