# M3U8 广告分析与去广告系统

> M3U8 播放列表广告分析与去广告工具 - 自动识别并移除插播广告片段，支持域名规则管理、自动学习、动态规则更新

## 分支说明

- **`main` 分支**：源码版本，核心代码明文，便于二次开发和审计
- **`jiami` 分支**：核心解析逻辑加密版本（Base64+乱码+自解码），防止被特征扫描，适合线上公开部署
  - 加密范围：`callOfficialReplaceDirect` / `findUrlInArray` / `isSafeVideoUrl` / `extractVideoUrl` 等 Bug 修复 + 官替优先核心逻辑
  - 功能与 main 完全一致，运行时自动解密，零性能感知差异
- **`go` 分支**：**Go 单文件版**（`main.go`），标准库零依赖，编译成单一静态可执行文件部署，无需 PHP/nginx；GitHub Actions 云端编译发布，详见下方「Go 单文件版说明」。

---

## Go 单文件版说明（branch `go`）

> M3U8 广告分析与去广告服务，用 Go 编写、**单文件**、**标准库零依赖**，编译后是一个可执行文件，下载即用。

### 快速开始

```bash
# 本地编译（Linux amd64，CGO_ENABLED=0 静态编译，旧系统 glibc 也能直接运行）
CGO_ENABLED=0 go build -o mxgt-go .

# 运行
./mxgt-go -addr :8080

# 或命令行模式（不进 HTTP，直接输出去广告结果）
go run main.go "https://示例.com/playlist.m3u8"
```

### HTTP 接口

| 接口 | 说明 |
|---|---|
| `GET /`（落地页） | 简洁落地页（显示品牌与开发者，入口指向 `/mxadmin`），**不进入后台** |
| `GET /mxadmin` | **后台管理页**：服务状态统计 + 解析测试 + 无广告播放器 + 远程在线更新 |
| `GET /api/clean?url=<m3u8>` | 返回过滤后的无广告 M3U8 纯文本（绝对地址，保留 KEY/MAP/不连续标签） |
| `GET /api/clean/json?url=<m3u8>` | 返回 JSON：统计 + 过滤后文本 + 每个片段的广告标记/原因 |
| `GET /api/clean?url=...&opt=aggresive` | 开启聚合聚类识别（同目录统一切片批量判广告，可能误伤统一节奏正片，默认关） |
| `GET /api/replace?url=<官方视频页>` | **官替链路**：识别平台→抓标题→资源站搜索→智能匹配→取集→经 `/api/clean` 去广告，返回 `ad_skip_url`/`play_url`（内置播放）/`external_url`（外置播放页） |
| `GET /player?url=<去广告直链>&title=<剧名>` | **独立外置播放页**（开放）：hls.js/原生播放，全站跨域 |
| `GET /api/maps` | 官替映射列表（`title_maps.json`：官方剧名 ↔ 资源站标准剧名） |
| `POST /api/maps/add` | 添加映射 `{from, to, platform?, note?}`（自动去重） |
| `POST /api/maps/delete?from=&to=` | 删除映射 |
| `GET /api/maps/fetch?url=<官方视频页>` | **从真实链接抓取官方平台/剧名/集数**，供一键填入映射表单 |
| `GET /api/platforms` | **官方平台自动更新配置列表**（`official_platforms.json`：顺序/平台/域名/URL正则/标题选择器/优先级，内置 7 平台默认） |
| `POST /api/platforms/add` | 添加官方平台配置 `{platform, domain, url_re?, title_selector?, priority, enabled, note?}` |
| `POST /api/platforms/update` | 编辑官方平台配置（按 `old_platform`+`old_domain` 定位，正则合法性校验） |
| `POST /api/platforms/delete?platform=&domain=` | 删除官方平台配置 |
| `GET /api/platforms/fetch?url=<真实官方链接>` | **自动更新官方**：按优先级匹配平台 → 标题选择器提取 → 解析「影视剧名+剧集集数」返回，供自动映射到映射表 |
| `GET /api/platforms/links` | 内置官方平台一键映射链接清单（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP） |
| `POST /api/platforms/oneclick?key=<内置key>` | **无脑映射**：内置官方链接实时抓取→解析剧名→自动映射到专区，无需输入链接（含反爬标题过滤） |
| `GET /api/jx?url=<m3u8/官方页/直链>&engine=basic/auto/ai` | **JSON 通用兼容接口**（供影视 / TVBox / 盒子等调用）：返回 `{code,success,msg,url(去广告可播),full,play,name,pic,header,format}`，已带跨域、URL 基于请求 Host 动态拼接不硬编码 |
| `GET/POST /api/ai/config` | 查看(仅GET,key打码)/更新(POST需登录) AI 去广告配置，返回 `ai_version`（见 `ai/` 独立目录） |
| `GET /api/sites` | 资源站列表（`resource_sites.json`，默认全部禁用，后台按需启用） |
| `POST /api/sites/toggle?name=<站名>&enabled=1/0` | 启用/禁用某个采集站并持久化 |
| `GET /api/sites/test?name=<站名>&kw=<词>` | 搜索测试单个资源站是否可用/命中 |
| `GET /api/stats` | 运行统计（JSON，后台展示） |
| `GET /api/update/check` | 检查远程是否有新版本（读取线上 `latest.json`） |
| `POST /api/update/apply` | 下载 GitHub Release 最新版并自动替换重启（远程在线更新） |
| `GET /healthz` | 健康检查 |

> 打开浏览器访问 `http://<IP>:8080/mxadmin` 进入后台（`/` 仅为落地页，不再直达后台）。后台**需登录**（默认 admin / admin123，可在后台「🔑 改密码」修改）。后台展示运行统计、M3U8 解析去广告测试、内嵌无广告播放器、官替链路、资源站管理与**远程在线更新**，页头显眼标注「开发者 · ssmhdssmhd」。

### 官替链路（Official Replace）

官替即“官方视频页 → 资源站无广告源替换”，参照 PHP 版 `gz/OfficialReplaceManager` 移植，流程：

1. **识别平台**：按域名识别腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP 视频；
2. **抓标题**：抓取官方页 `og:title` / `<title>`，腾讯无标题时用 `getinfo` 兜底取正式片名；
3. **解析剧名/集数**：`parseVideoTitle` 剥离「第X季/X集」、`S系E集`、画质词，得出 `base_title` 与 `episode_num`；
4. **搜索资源站**：对已启用采集站调 AppleCMS/maccms `?ac=videolist&wd=关键词`，解析 `list[]` 的 `vod_play_url`（支持 `$$$` 多线路 / `集数$url` 格式）；
5. **智能匹配**：按基础剧名相似度（包含/公共字）+ 集数命中 + 季数一致性打分，阈值 65；匹配时**双向展开映射变体**（`title_maps.json`），官方剧名/剧集与资源站表述不同也能命中（可在后台「🗺️ 官替映射专区」从真实链接抓取并添加映射）；
6. **取集**：按 `episode_num` 从剧集列表精准取对应 m3u8，否则回退列表首项（相对地址自动补全为绝对地址）；
7. **去广告**：把源 m3u8 交给 `/api/clean`（复用既有规则引擎），输出 `ad_skip_url` 无广告直链。

### 官替映射专区（Official Replace Mapping）

- 后台「🗺️ 官替映射专区」：**🧠 自动学习已开启**——只要官替解析成功命中资源站、且资源站剧名与官方剧名表述不同，就自动生成「官方剧名 → 资源站标准剧名」映射并保存，**无需手动逐个添加**；
- 手工补充入口仍可用：粘贴**真实官方视频页链接** →「🔍 从链接抓取」自动识别平台并抓取官方剧名/集数 →「📥 填入表单」→ 填**资源站标准剧名** →「➕ 添加映射」；
- 自动学习映射带有「自动学习」备注，与手动映射一样被匹配逻辑双向往回展开（`title_maps.json`），支持列表查看与逐条删除；
- 官替搜索关键词自动含「剧名+第N集 / 剧名+N / 剧名」三档；集数解析兼容「第01集 / EP01 / E1 / 01 / 独剑九天01 / 1」等多种表述，超过 9 集的多位数集数也能正确解析匹配。

### 自动更新官方（Official Platform Auto-Update）

- 后台「🛰️ 自动更新官方」面板：**顺序（优先级）/平台名称/域名/URL 匹配正则/标题选择器/优先级/启用/备注** 的配置列表，支持添加、编辑、删除、启停；
- **⚡ 一键映射**：面板内置各大官方平台默认链接，点击即**无脑映射**——后端实时抓取该官方链接标题 → 解析影视剧名 → 自动映射到专区，**无需输入链接**；命中反爬/验证页则拒绝映射并提示，避免脏数据；
- 配置持久化到可执行文件旁 `official_platforms.json`，内置 7 平台默认配置（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP）；
- `matchOfficialPlatform` 按**优先级顺序**匹配 URL（域名包含 + URL 正则可选），`detectPlatform` 优先使用用户配置，未配置回退内置平台提示；
- **标题选择器**：自定义正则（第 1 捕获组）从真实页面提取标题，空则回退默认 `og:title`/`<title>`；
- **自动映射**：粘贴真实官方链接 →「🔍 从链接自动获取」→ 自动解析「影视剧名 + 剧集集数」并**自动映射到「官替映射专区」表单**（from=影视剧名、平台自动填入），可直接「➕ 添加映射」落入映射表。

### JSON 兼容接口（影视 / TVBox / 盒子）

`GET /api/jx?url=<m3u8|mp4|官方视频页>&engine=basic|auto|ai`

- 供影视 App / TVBox / 电视盒子等调用；返回 JSON：
  ```json
  {"code":1,"success":true,"msg":"ok","url":"<去广告可播地址>","full":"<同>","play":"<源地址>","name":"","pic":"","header":"","format":"m3u8|direct"}
  ```
- `url` 为可直接播放的去广告地址（m3u8 经 `/api/clean`，mp4 直链透传；官方页走官替链路），基于**请求 Host 动态拼接**，无硬编码；
- 响应带全局 CORS 头（`Access-Control-Allow-Origin:*` 等）；
- `engine=ai` 可强制 AI 去广告（需先启用 AI 模块）。

### AI 去广告（独立模块 `ai/`）

- 独立目录 [`ai/`](file:///workspace/ai/)：含 [`config.json`](file:///workspace/ai/config.json)、`VERSION`、`README.md`，**独立版本、可单独更新**（改配置/升级无需重编主程序）；
- 配置文件控 `enabled`、`mode`（basic/ai/auto）、AI 服务商/接口/key/模型/提示词；主程序启动时读取可执行文件旁 `ai/config.json`（缺失用内置默认=基础规则去广告，行为不变）；
- 使用：`/api/clean?engine=ai`、`/api/jx?engine=ai`，或在后台「解析测试」选择「去广告引擎=AI」；AI 调用失败自动回退基础规则；
- 查看/更新：`GET /api/ai/config`（key 打码）、`POST /api/ai/config`（需登录）。

### 播放器多浏览器兼容

- mp4/mkv/webm/flv 直链用**原生播放器**（任意浏览器）；
- iOS Safari 等原生支持 HLS 的直接用浏览器原生；
- 其余用 **hls.js**（4 个 CDN 自动兜底加载 + 失败提示），后台与前台播放均覆盖。

### 资源站管理

- 复用 PHP 版资源站列表（`sites_config.php` → [`resource_sites.json`](file:///workspace/resource_sites.json)，**默认全部禁用**，后台按需启用后再生效，避免误用失效源）；
- 后台 `/mxadmin` → **官替链路** 面板可一键解析官方视频页；**资源站管理** 面板可启用/禁用采集站并做搜索测试（启停持久化到可执行文件旁的 `resource_sites.json`）；
- 内置配置以常量打入二进制（`sites_static.go`），首次运行自动落盘供后台修改。

### 远程在线更新（Go 版）

服务内置远程自动更新：每次发布时 GitHub Actions 会把版本清单 `latest.json`（含最新版本号与 Release zip 文件名）自动提交到 `go` 分支根目录。

- 服务从 `raw.githubusercontent.com/ssmhdssmhd/MXGT/go/latest.json`（**不走 GitHub API，无未认证限流问题**）读取最新版本；
- 与当前 `AppVersion` 对比，若有新版（版本号更大且 zip 有效）→ 后台「远程在线更新」面板出现「检查更新 / 下载并更新重启」；
- 点更新：下载对应 Release zip → 解出二进制 → 备份旧版为 `mxgt-go.bak` → 原子替换 → 自动重启新进程（继承原启动参数与端口）；
- 运行目录需有写权限（能创建 `.bak` 并替换自身二进制）。



## 广告检测规则（保守防误删）

1. **URL 关键词**：`/ad/`、`_ad`、`ad0`、`ads`、`gdt`、`tvc`、`promo`、`300x250`、`600x90` 等；
2. **广告标签区间**：`EXT-X-DATERANGE` / `EXT-X-CUE-OUT` / `EXT-X-AD` 声明；
3. **超短视频**：`duration < 1.0s`（前 2 段为片头保护）；
4. **聚合聚类**（需 `opt=aggresive`）：同目录同短时长桶占比 ≥35% 批量判广告，默认关闭。

> 设计原则：与 PHP 版 v5.15.4 一致，**保守优先**——默认只用高置信规则（关键词/标签/超短 3 类），避免把统一切片的正片误删导致黑屏。

### 云端编译（GitHub Actions）

`.github/workflows/build-go.yml`：push 到 `go` 分支触发，Go 1.22 编译单文件二进制，打包为
`MXGT_go_<版本>_<北京时间yyyyMMddHHmm>.zip` 上传到 **GitHub Release**（tag `go-v<版本>`）。

产物：`mxgt-go`（~6.6MB，Linux amd64，静态，零依赖）+ `README-go.md`。

### 部署

```bash
# 下载 Release 里的 zip → 解压
unzip MXGT_go_*.zip
chmod +x mxgt-go
# 直接用，无需安装任何依赖
./mxgt-go -addr :8080
```

### Go 版来源与差异

- 从 PHP 版核心去广告逻辑移植，聚焦「M3U8 解析 + 广告检测 + 无广告输出」核心链路；
- 后续迭代方向（按需）：后台管理页、资源站抓取/搜索、规则自动学习、多平台二进制（macOS/Windows）等。

### 部署注意事项（GLIBC 兼容）

- GitHub Actions 在 `ubuntu-latest` 上**静态编译**（`CGO_ENABLED=0`），二进制不依赖宿主 glibc，旧系统（如 CentOS 7 / 低版本 glibc）也能直接运行；
- 若在本地自编，请务必使用 `CGO_ENABLED=0`。否则默认 `go build` 会动态链接当前系统的 glibc，部署到更旧系统的服务器会报 `GLIBC_2.3x not found` 无法启动。

---

## Go 版更新日志（branch `go`）

## v0.6.5 (2026-09-10) — 播放代理 + hls.js 抗卡顿优化，解决播放卡顿

> 播放器此前直连第三方 m3u8/分片：跨域 XHR 受限、源站限速/反爬、无失败重试 → 频繁卡顿/黑屏。本次新增**服务端播放代理** `/api/play`，所有播放统一走本服务：m3u8 内分片/密钥地址改写为本服务代理，分片带 UA/Referer/超时拉取并透传（支持 Range）；播放器侧加大缓冲 + 分片失败重试（指数退避）。

### 更新内容（[main.go](file:///workspace/main.go)）

- **🛰️ 播放代理 `/api/play?url=`**（开放，无需登录）：
  - m3u8 播放列表改写：分片/子列表/AES-128 密钥地址全部指向本服务代理（绝对/相对路径均处理）；
  - 分片/密钥/媒体字节流透传：支持 Range（206）、CORS 全放行、no-store；
  - 源站请求带浏览器 UA + 站域 Referer + 60s 超时，统一应对源站反爬/限速；
- **🎛️ hls.js 抗卡顿配置**（外置播放页 + 后台全部播放器）：`maxBufferLength 60s` + `maxMaxBufferLength 180s` + `backBufferLength 30s`、分片失败重试 5 次（指数退避 `400ms→4s`）、清单重试 3 次、`enableSoftwareAES` 软件解密兜底、自动码率（`startLevel:-1`）；
- **🔗 播放链路全走代理**：`/player` 外置页、后台「解析测试/官替/新版增强测试播放」的 m3u8 播放统一经 `/api/play` 代理（mp4/mkv 直链除外，原生播放），第三方分片不再直连；
- 版本升级 `v0.6.4 → v0.6.5`。

### 验证

- `go vet` / `go build` 通过；后台与外置播放页 JS `node --check` 通过；
- 实测代理：真实 m3u8 分片地址改写为 `/api/play?url=<绝对地址>`；分片透传 200（371KB）、Range 请求 206（1KB）、CORS 头齐全。

---

## v0.6.4 (2026-09-10) — 修复官方平台「一键映射」JS 渲染壳报错并给出解决指引

> 腾讯视频/PP视频等平台页面为 JS 渲染壳（静态 HTML 只有平台标题），此前一键映射直接报「未能从标题解析出剧名: 腾讯视频」且无下文。本次：腾讯增加 vid 自动提取 + getinfo 兜底取正式片名；失败时给出明确可操作的解决指引；无效内置链接替换。

### 更新内容（[main.go](file:///workspace/main.go)）

- **🔧 腾讯 vid 兜底**：`fetchVideoTitle` 自动从 URL 参数 `vid=` 或页面 JSON `"vid":"xxx"`（cover 页）提取 vid，调腾讯 `getinfo` 接口取正式片名（此前仅支持调用方显式传 vid）；
- **📋 报错指引可操作化**：`/api/platforms/oneclick` 与 `/api/platforms/fetch` 解析不出剧名时，明确提示「该平台页面为 JS 渲染壳」并给出 3 条解决路径：① 部署 `render-title/`（Node+Chromium 无头渲染，自动取真实剧名）② 在「官替映射专区」手动添加官方剧名 → 资源站剧名 ③ 配置该平台标题选择器后重试；
- **🔗 内置链接更新**：腾讯 cover 换成现役剧集（原链接对应视频已下线/审核中）；PP视频首页 `v.pptv.com/` → 具体剧集页（《冰锋》），避免点首页必然失败；
- 版本升级 `v0.6.3 → v0.6.4`。

### 验证

- `go vet` / `go build` 通过；实测 `oneclick?key=tencent` 返回完整诊断提示与 3 条解决路径，`oneclick?key=pptv` 明确提示渲染/反爬原因；
- 爱奇艺/优酷/哔哩哔哩等含 og:title 的平台一键映射不受影响。

---

## v0.6.3 (2026-09-10) — 资源站「测试采集」增强：视频信息列表 + 复制播放链接 + 测试播放

> 资源站面板的「详情」升级为「🔍 测试采集」按钮：展开显示该站搜索命中的**完整视频列表**（缩略图/剧名/备注/播放来源/播放地址），每条可一键**复制播放链接**或**测试播放**；接口改为**单站直测**（不再全量搜索 40+ 站再过滤，快且准确）。

### 更新内容（[main.go](file:///workspace/main.go)）

- **🔍 测试采集按钮**：资源站行按钮「详情」→「🔍 测试采集」，展开即测该站采集接口；
- **📺 视频信息列表**：展示全部命中视频卡片——封面缩略图、剧名、备注（如「更新至 30 集」）、播放来源（play_from）、播放地址（first_url）；
- **📋 复制播放链接**：每个视频独立「📋 复制播放链接」按钮（复制采集站返回的绝对播放地址）；
- **▶ 测试播放**：每个视频带「▶ 测试播放」按钮，新窗口打开 `/player` 独立播放页；
- **⚡ 单站直测**：`/api/sites/test` 改为只请求目标站采集接口（`searchSiteOne`），不再全量搜索后过滤，响应快、失败原因更明确（返回 `message` 字段）；
- 版本升级 `v0.6.2 → v0.6.3`。

### 验证

- `go vet` / `go build` 通过；后台 JS `node --check` 通过；登录后实测「测试采集」按钮页面渲染与 `playTest`/`copyText` 函数就绪；
- `/api/sites/test` 单站直测返回完整视频信息（name/remarks/pic/play_from/first_url）。

---

## v0.6.2 (2026-09-10) — 后台「HTTP 接口说明」补全

> 后台 `/mxadmin` 的接口说明表格此前只列出 9 个接口，本次补全为完整列表（含新版增强播放、jx、播放页、映射、平台、非正片标注、弹幕、远程更新、AI 配置等）。

### 更新内容（[main.go](file:///workspace/main.go)）

- **📚 后台接口说明补全**：`📚 HTTP 接口说明` 表格从 9 行扩充到 19 行，补齐：`/api/clean/enhanced[/json]`（新版增强测试播放）、`/api/jx`（影视/TVBox 兼容）、`/player`（外置播放页）、`/api/maps`（官替映射）、`/api/platforms`（官方平台自动更新）、`/api/skip`（非正片区间标注）、`/api/danmaku`（弹幕过滤规则库）、`/api/update/check` + `/api/update/apply`（远程更新）、`/api/ai/config` 等；
- 版本升级 `v0.6.1 → v0.6.2`。

### 验证

- `go vet` / `go build` 通过；`/mxadmin` 页面接口表格渲染完整。

---

## v0.6.1 (2026-09-10) — 远程更新多镜像回退，修复已部署客户检测不到新版本

> 已部署客户远程更新「获取不到最新版」根因：新版代码未推送到 `go` 分支，GitHub 上 `latest.json` 仍为旧版且无对应 Release。本次已推送并发布 v0.6.0；同时将更新检查改为**多镜像回退**，即使 GitHub 原生 CDN 缓存延迟也能拿到最新清单。

### 更新内容（[main.go](file:///workspace/main.go)）

- **🔧 远程更新多镜像回退**：版本清单 `latest.json` 读取改为三源按序回退——`raw.githubusercontent.com`（主源）→ `cdn.jsdelivr.net`（CDN 兜底）→ `api.github.com/contents`（API 终兜底，内容与 git 分支 HEAD 严格一致）；任一源成功即返回，避免单个源 CDN 缓存延迟/故障导致已部署客户检测不到新版本；
- **🚀 发布 v0.6.0 到 `go` 分支**：补齐本地未推送的提交（`be4ac25..81ce5f0`），触发 GitHub Actions 云端编译，创建 Release `go-vv0.6.0` 并更新 `latest.json → v0.6.0`，客户「检查更新」即可发现并下载；
- **📦 发行版保持两个包**：每个 Go Release 同时提供 **Go 单文件包**（`MXGT_go_*`，供远程在线更新）与 **PHP 发布包**（`MXGT_v*`），工作流构建时只清理历史 Go 产物、保留 PHP 包并上传前清理同名资产，避免更新后只剩一个包；
- 版本升级 `v0.6.0 → v0.6.1`。

### 验证

- `go vet` / `go build` 通过；`/api/update/check` 实测返回 `latest: v0.6.x`、下载地址命中 Release 资产；
- 三源一致性核对：raw / jsDelivr / API 均返回最新 `latest.json`。

---

## v0.6.0 (2026-09-10) — 新版增强测试播放 + 非正片区间标注 + 弹幕过滤规则库 + 资源站扩充(168)

> 后台新增 3 个独立面板：**🆕 新版增强测试播放**（独立增强引擎，不动原解析测试）、**⏱️ 非正片区间标注**（SponsorBlock 思路）、**💬 弹幕过滤规则库**（独立模块，将来接弹幕源即用）。资源站从「皮皮虾助手 + 萌芽采集资源」抓取扩充，站数 122 → 168。

### 更新内容（[main.go](file:///workspace/main.go) + [sites_static.go](file:///workspace/sites_static.go) + [resource_sites.json](file:///workspace/resource_sites.json)）

- **🆕 新版增强测试播放**：新增 `cleanEnhanced` 独立引擎（`/api/clean/enhanced[/json]`），在基础去广告之上叠加高置信增强检测——平台广告域关键词（百度/阿里/字节/CDN 广告路径、pre/mid/post-roll）+ 片头片尾超短簇（`#EXT-X-DISCONTINUITY` 边界连续 <2s 且成簇 ≥3 段）；**完全不动原 cleanOne / 原「解析测试」**，后台另起一块面板，带独立输入与「直接播放增强结果」；
- **⏱️ 非正片区间标注**（SponsorBlock 思路）：新增 `skip_ranges.json` + `/api/skip`（列表）、`/api/skip/add`、`/api/skip/delete`（需登录）；维护「片头/片尾/赞助/自我推广/互动三连/其他」时间戳区间，供播放器按区间跳过；后台「⏱️ 非正片区间标注」面板增删查；
- **💬 弹幕过滤规则库**（独立模块）：新增 `danmaku_rules.json` + `/api/danmaku`（列表）、`/api/danmaku/add`、`/api/danmaku/toggle`、`/api/danmaku/delete`、`/api/danmaku/test`（需登录）；关键词 + 正则双类型规则，命中计数，跑在服务端与播放器 DOM 解耦，未来接入弹幕源即用；后台「💬 弹幕过滤规则库」面板管理 + 单条测试；
- **🏢 资源站扩充 122 → 168**：经浏览器自动化登录苹果CMS后台，从「皮皮虾助手」及「萌芽采集资源」（推荐/切片/官采/自定义专区）抓取资源站，去重后**新增 46 个**（西瓜/无水印/闪电/金马资源网/精品98/山海/热播4k 等），`resource_sites.json` 与内置 `sites_static.go` 同步，默认全部禁用，后台按需启用即生效；
- **🔧 资源站检测修复（可用率 4 → 75/165）**：
  - 资源站请求改用**浏览器 UA + 站域 Referer**（`httpGetBody`），规避资源站 UA/Referer 反爬；
  - **宽松 JSON 解析**：多数站 `vod_id/id` 返回数字，标准 `string` 解析直接失败导致整批误判「非 JSON/XML」——新增 `maccmsFlex`（`json.Number`）兼容数字/字符串 id；
  - 深度去重（host/api/name 归一）168 → 165；
  - 全站启用后批量检测：**可用 75 个（45.5%）**，剔除 >3s 慢站后**最终启用 63 个**（`active+enabled`），其余 102 个失效/超时/慢自动 `paused` 屏蔽并记录原因；
- 版本升级 `v0.5.6 → v0.6.0`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build` 通过；资源站检测/搜索、官替、去广告接口回归正常；
- 三个新接口实测：skip 增/删/查、danmaku 增/删/启停/关键词/正则可命中测试全部通过；后台 JS `node --check` 通过；
- 资源站批量检测：165 站全量探测（并发 8），可用 75、屏蔽 90；端到端搜索「庆余年」启用站命中 14+ 站返回视频；
- 沙箱境外网络对部分站域名受限属网络环境问题，部署到国内服务器成功率会更高；被屏蔽站可后台「检测并屏蔽失效站」重新检测或手动恢复。

---

## v0.5.6 (2026-09-10) — 失效站可折叠 + 自动更新官方「一键映射」内置链接

> 资源站管理新增「❌ 已失效/暂停」可折叠区；自动更新官方内置各大官方链接，点击即**无脑映射**（无需输入链接），实时抓取并自动映射到专区。

### 更新内容（[main.go](file:///workspace/main.go)）

- **失效站可折叠**：资源站列表底部新增失效折叠区（默认折叠），主列表只显示可用站（启用/未启用分组 + 已启用/可用/失效统计）；
- **一键映射**：`GET /api/platforms/links`（内置 7 平台链接清单）+ `POST /api/platforms/oneclick?key=`（实时抓取→解析剧名→自动写入映射表）；反爬标题过滤拒绝脏数据；
- 版本升级 `v0.5.5 → v0.5.6`。

### 验证

- `CGO_ENABLED=0 go build -o mxgt-go .` 通过，`gofmt` 干净；冒烟测试 login→links→oneclick→maps 贯通。

---

## v0.5.5 (2026-09-09) — 资源站「搜索验证站」识别标注：不误判失效

> 资源站中部分站有**搜索验证**（关键词搜索接口需验证/未开放）。批量检测时若搜索无命中但列表接口连通，**仍判可用**（不误判失效），并在检测结果与站点列表标黄/备注提示，便于识别。

### 更新内容（[main.go](file:///workspace/main.go)）

- 检测项新增 `search_limited` 标记：探针词均无结果但列表连通即置位；检测列表橙黄标注「⚠️ 可用(搜索受限)」，靠接口连通性判可用、不屏蔽；
- 检测完成后该站 `note` 常驻「搜索受限/需验证·仅接口连通」，后续正常命中自动清除；
- 版本升级 `v0.5.4 → v0.5.5`。

### 验证

- `CGO_ENABLED=0 go build -o mxgt-go .` 通过，`gofmt` 干净。

---

## v0.5.4 (2026-09-09) — 官替映射「自动学习」：无需手动逐个添加

> 官替映射专区不再需要一条条手动添加：官替解析成功命中资源站、且资源站剧名与官方剧名表述不同时，自动生成「官方剧名 → 资源站标准剧名」映射并保存，被匹配逻辑双向往回展开，下次同类剧名直接命中。

### 更新内容（[main.go](file:///workspace/main.go)）

- **自动学习映射**：新增 `autoLearnTitleMap`，在 `replaceOne` 匹配成功后自动写入 `title_maps.json`（去重、同名不生成、拼接剧名自动取基础剧名）；返回 `auto_learn_map`/`auto_learn_count`；
- **前端**：官替结果状态行显示「🧠 自动生成映射…（累计 N 条）」+「🗺️ 查看映射表」；映射专区标题提示「已开启自动学习，手动表单仍可用」；
- 版本升级 `v0.5.3 → v0.5.4`。

### 验证

- `CGO_ENABLED=0 go build -o mxgt-go .` 通过；单测覆盖：表述不同→生成、同名→不生成、重复→不重复生成，全部通过。

---

## v0.5.3 (2026-09-09) — 资源站检测去误判 + 官替「自动更新官方」平台配置

> 修复资源站批量检测误判（单探针词导致 119 个站点被误判失效）；官替映射专区新增「🛰️ 自动更新官方」——平台配置（顺序/平台/域名/URL 正则/标题选择器/优先级）持久化到 `official_platforms.json`，从真实链接自动解析「影视剧名 + 剧集集数」并自动映射到映射表对应区域。

### 更新内容（[main.go](file:///workspace/main.go)）

- **资源站检测去误判**：新增 `probeSiteOne` 综合探测——多探针词（「爱情/庆余年/电视剧」）任一命中即判可用；搜索均无结果时探测列表接口连通性（`?ac=videolist&limit=5`），仅 HTTP 错误/超时/解析失败才置失效；
- **官方平台配置**：`OfficialPlatform` 数据结构 + `official_platforms.json` 持久化，内置 7 平台默认配置（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP）；`matchOfficialPlatform` 按优先级顺序匹配（域名包含 + URL 正则）；`detectPlatform` 优先用户配置，未匹配回退内置提示；
- **标题选择器**：`fetchVideoTitleWithSelector` 用自定义正则（第 1 捕获组）从真实页面提取标题，空则回退默认 `og:title`/`<title>`；
- **自动映射**：`GET /api/platforms/fetch?url=<真实官方链接>` 按优先级匹配平台 → 提取标题 → 解析「影视剧名 + 剧集集数」；后台「自动更新官方」面板支持添加/编辑（平台名称、域名、URL 匹配正则、标题选择器、优先级）/删除/启停，获取成功后自动填入「官替映射专区」表单并可一键添加映射；
- 版本升级 `v0.5.2 → v0.5.3`。

### 验证

- `CGO_ENABLED=0 go build -o mxgt-go .` 通过；API 冒烟测试（platforms 列表/添加/更新/删除/fetch 未匹配提示）全部通过。

---

## v0.5.2 (2026-09-09) — 修复剧名/集数拆分 + 内置 7 种官方平台字段提取

> 修复官方标题「【腾讯视频】 独剑九天 01」被整体当作剧名的问题：内置 7 种官方平台（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP）的剧名标签/后缀清理规则，自动拆分「剧名字段 + 剧集字段」，搜索关键词与映射专区 from 字段只含真实剧名。

### 更新内容（[main.go](file:///workspace/main.go)）

- **修复字段拆分**：新增 `platformTagRe`（7 平台标签/后缀清理：`【腾讯视频】`/`_爱奇艺`/`-优酷`/`芒果TV`/`哔哩哔哩`/`搜狐视频`/`PP视频` 等）；`parseVideoTitle` 支持「剧名+末尾数字→集数」拆分（`独剑九天 01`→剧名`独剑九天`+集数 1；紧跟 2 位数字次之，`流浪地球2` 类续作不误拆）；
- **内置 7 种官方映射**：7 平台字段提取规则内置代码；`title_maps.json` 首次创建自动落盘示例映射；
- 版本升级 `v0.5.1 → v0.5.2`。

### 验证

- 单测：`【腾讯视频】 独剑九天 01`→`独剑九天`+第1集、`独剑九天01`→第1集、`剑出九霄 第12集`→第12集、`独剑九天_爱奇艺`→`独剑九天`、`流浪地球2` 不误拆；`go vet` / `CGO_ENABLED=0 go build` 通过。

---

## v0.5.1 (2026-09-09) — 官替映射自动填充 + 内置/外置播放

> 映射专区输入官方链接即**自动映射剧名字段和剧集字段**（官方原始标题含当前剧集也能拆分）；官替结果支持「▶ 内置播放」（后台内嵌播放器）与「↗ 外置播放」（独立 `/player` 播放页新窗口），全站跨域、播放地址基于请求 Host 动态拼接不硬编码。

### 更新内容（[main.go](file:///workspace/main.go)）

- **映射自动填充**：`GET /api/maps/fetch` 返回 `title_raw`（原始标题）/`base_title`（剧名字段）/`episode_raw`+`episode_num`（剧集字段）；「从链接抓取」后自动填入 from 与平台，显示剧名/剧集拆分，「直接添加映射」时 to 留空默认等于官方剧名；
- **官替播放**：`GET /api/replace` 新增 `play_url`（内置播放地址）与 `external_url`（外置播放页地址）；
  - 内置播放：后台结果区「▶ 内置播放」按钮，页面内嵌播放器直接播放；
  - 外置播放：新增开放播放页 `GET /player?url=<去广告直链>&title=<剧名>`，hls.js 多 CDN 兜底 + iOS 原生 HLS + mp4 直链原生播放；
- **跨域**：`/player` 响应与 m3u8 分片请求均带 CORS 头（`withCORS` 全局），hls.js 请求分片携带 `Origin` 头；
- **URL 不硬编码**：`play_url` / `external_url` 由 `playURL` 基于请求 `Host` 动态生成；
- 版本升级 `v0.5.0 → v0.5.1`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过。

---

## v0.5.0 (2026-09-09) — 官替映射专区：解决官方剧名/剧集与资源站表述不同导致的匹配失败

> 新增「官替映射专区」：从真实官方视频页链接抓取官方平台/剧名/集数，建立官方剧名 ↔ 资源站标准剧名映射；官替搜索与匹配自动双向展开映射变体，解决官方剧名/剧集与资源站表述不同导致「搜不到 / 匹配不到正确剧集」（如「独剑九天 01」）的问题；同时修复多位数集数解析错误（「第12集」被误解析为 2）。

### 更新内容（[main.go](file:///workspace/main.go)）

- **映射专区**（映射持久化到可执行文件旁 `title_maps.json`）：
  - `GET /api/maps` 映射列表；`POST /api/maps/add` 添加 `{from, to, platform?, note?}`；`POST /api/maps/delete?from=&to=` 删除；
  - `GET /api/maps/fetch?url=<真实官方视频页>` **从链接抓取官方平台/剧名/集数**（识别腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP，抓 `og:title`/`<title>`，腾讯 `getinfo` 兜底），一键填入映射表单；
- **匹配增强**：`titleMapCandidates` 对官方剧名与资源站剧名双向展开映射变体；`findBestMatch` 变体两两比较取最高相似度；搜索关键词自动含「剧名+第N集 / 剧名+N / 剧名」三档；`episodeNumOfPlayItem` 兼容「第01集 / EP01 / E1 / 01 / 独剑九天01 / 1」；
- **修复**：`cnToNum` 纯数字逐字解析缺陷（「第12集」误解析为 2），现按整数直接转换；
- **后台 UI**：新增「🗺️ 官替映射专区」面板：粘贴链接→从链接抓取→填入表单→添加映射→列表管理（自动加载）；
- 版本升级 `v0.4.9 → v0.5.0`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过。

---

## v0.4.9 (2026-09-09) — 资源站支持编辑（接口/官网/备注/优先级/改名）

> 资源站管理补齐「编辑」能力（参考 PHP `updateSite`）：采集接口、官网、备注、优先级、启停可在线修改，支持改名；后台每行新增「✏️ 编辑」内联表单。

### 更新内容（[main.go](file:///workspace/main.go)）

- **新增接口 `POST /api/sites/update`**（参考 PHP `updateSite`）：`{name: 定位原名, api_url, site_url, note, priority, enabled, new_name?}`；
  - `name` 定位（精确 + 忽略大小写兜底），可更新采集接口 / 官网 / 备注 / 优先级 / 启停；
  - `new_name` 可选改名（自动查重，重名拒绝）；接口非空时校验 `http(s)://` 开头；
  - 修改实时落盘 `resource_sites.json`；
- **后台 UI**：站点每行新增「✏️ 编辑」按钮，行内展开编辑表单（名称/接口/官网/备注/优先级 + 保存），保存后自动刷新列表；
- 版本升级 `v0.4.8 → v0.4.9`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；
- 实测：编辑接口+备注成功并落盘；改名（西瓜→西瓜影视→西瓜）成功；非法接口（ftp://）拒绝；不存在站点返回「资源站不存在」；浏览器实测每行编辑按钮可见、表单正确预填、保存后备注更新、再次展开显示最新值、布局正常。

---

## v0.4.8 (2026-09-09) — 全局多并发 + 批量检测进度条

> 资源站批量操作（搜索/检测）全局多并发（默认 8，可 `-sites-conc` 调整）；「检测并屏蔽失效站」改为异步任务 + 真实百分比进度条 + 实时结果，长耗时任务不再阻塞页面。

### 更新内容（[main.go](file:///workspace/main.go)）

- **全局多并发**：
  - 新增启动参数 `-sites-conc N`（默认 8，建议 4~16）：资源站批量搜索与检测的全局并发数；
  - `searchSites`（官替链路多站搜索）与批量检测均改为并发 worker 池执行，显著提速；
- **批量检测异步化 + 进度条**：
  - `POST/GET /api/sites/check?kw=` 改为**异步启动**检测任务，立即返回 `task` 任务号；
  - `GET /api/sites/check/progress?task=<id>` 轮询进度：`total/done/usable/blocked/finished/results`（结果逐步返回）；
  - 检测并发执行，失败站点仍自动置 `paused` 屏蔽并记录原因，完成后统一落盘；
  - 后台「🧹 检测并屏蔽失效站」：显示真实百分比进度条（`.prog-fill` 按 `done/total` 增长）+ 计数文本 + 实时结果列表，完成后自动刷新列表并隐藏失效站；
- 版本升级 `v0.4.7 → v0.4.8`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；
- 实测：并发搜索西瓜 XML 命中 7 条；异步检测任务启动→进度接口返回 done/total/usable/blocked/results；浏览器实测进度条 0%→20%→100% 平滑增长、计数「检测 X/5（Y%）· 可用 N · 失效 M」实时更新、结果逐条追加、完成后自动刷新并隐藏失效站（慢站自动 `paused`）。

---

## v0.4.7 (2026-09-09) — 资源站管理：添加/删除/批量检测屏蔽失效站 + 西瓜 XML 接口

> 参考 PHP 版资源站管理：支持前台添加、删除资源站；批量检测可用性，失败站点自动标记为失效并在列表隐藏（不显示失败的）；新增西瓜资源站（XML 采集接口）并支持 XML 格式解析。

### 更新内容（[main.go](file:///workspace/main.go) + [sites_static.go](file:///workspace/sites_static.go)）

- **新增接口**：
  - `POST /api/sites/add`：添加资源站（名称+接口必填、重名拒绝、落盘持久化）；
  - `POST /api/sites/delete?name=`：删除资源站（精确 + 忽略大小写兜底）；
  - `GET /api/sites/check?kw=`：批量检测已启用站点（参考 PHP `verifySearchCapability`），探测词搜索失败/无结果自动置 `status=paused` 屏蔽并在备注记录原因，成功恢复 `active`；
- **列表过滤（不显示失败的）**：`GET /api/sites` 默认只返回 `status=active` 的可用站点（隐藏失效站），`?show=all` 显示全部；返回 `stats{total,active,failed,enabled,shown}`；
- **XML 采集接口支持**：`searchSiteOne` 兼容三种格式——maccms JSON、标准 AppleCMS XML（`vod_` 前缀）、自定义 XML（`<id>/<name>/<pic>/<dl><dd flag=>`，如西瓜）；
- **资源站客户端支持环境代理**：`siteHTTP` 增加 `ProxyFromEnvironment`，部署在需要代理的服务器也能采集；
- **新增/更新资源站**：西瓜接口更新为 `https://caiji.xgzyapi.com/api.php/provide/vod/at/xml/`（XML，实测搜索「庆余年」命中 7 条）；
- **后台 UI**：资源站面板新增「添加资源站」表单（名称/接口/官网/备注）、每行「🗑 删除」按钮、「隐藏失效站」开关（默认开）、「🧹 检测并屏蔽失效站」按钮与检测结果展示；
- 版本升级 `v0.4.6 → v0.4.7`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；
- 实测：西瓜 XML 搜索命中 7 条（庆余年）；列表默认隐藏 24 个失效站（显示 98/122）；添加/重名拒绝/空参拒绝/删除/删除不存在均符合预期；批量检测：西瓜→可用、失效站→自动 `paused` 屏蔽并记录原因；后台浏览器实测表单渲染、隐藏失效过滤、详情、删除按钮均正常。

---

## v0.4.6 (2026-09-09) — 前台显示 API 接口调用方式（支持一键复制）

> 前台（首页 `/`）新增「🔌 API 调用方式」面板：展示全部对外接口的调用地址（基于当前访问域名自动生成）与 curl 命令，每条可一键复制，方便接入影视 / TVBox / 盒子或脚本调用。

### 更新内容（[main.go](file:///workspace/main.go)）

- **前台 API 调用方式面板**：首页新增「🔌 API 调用方式」，列出 6 个接口——去广告 M3U8（`/api/clean?url=`）、去广告 JSON（`/api/clean/json`）、官替链路（`/api/replace`）、影视/TVBox 兼容（`/api/jx`）、运行统计（`/api/stats`）、健康检查（`/healthz`）；
- **动态地址 + 一键复制**：URL 基于当前访问域名 `location.origin` 自动拼接（不硬编码 IP/域名），每行提供「复制URL」「复制CURL」按钮，点击后显示「✓ 已复制」绿色反馈（含降级方案：无 Clipboard API 时用隐藏 textarea 复制）；
- 版本升级 `v0.4.5 → v0.4.6`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；
- 浏览器实测：`GET /` 正常渲染 6 行 API 面板；点击「复制URL」按钮变绿显示「✓ 已复制」并自动恢复；URL/CURL 深色代码块、紫色按钮样式正常，无横向溢出错位。

---

## v0.4.5 (2026-09-09) — 修复更新后/端口被占时不能启动

> 修复"更新好不能启动"：HTTP 端口被占用（旧进程未退出 / TIME_WAIT / 被其它程序占用）时不再直接 `log.Fatal` 退出，改为自动等待并重试绑定（最多 30 次，2s 一次），端口释放后自动接续启动。

### 更新内容（[main.go](file:///workspace/main.go)）

- **端口占用自动重试**：启动时若 `ListenAndServe` 返回 `address already in use`，打印「端口被占用，2s 后重试」并持续重试（`maxBindRetry=30`）；端口释放后自动绑定成功，避免在线更新重启或旧进程未退出导致服务起不来；
- 非端口类致命错误仍直接 `log.Fatal` 退出并给出明确日志；
- 优雅关闭（`http.ErrServerClosed`，如更新重启）正常退出；
- 版本升级 `v0.4.4 → v0.4.5`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；
- 实测复现旧 bug：起一个进程占用 `:8100` 后再起 v0.4.5 同端口 → 原来会 `bind: address already in use` 直接退出；现改为日志「端口被占用，2s 后重试」保持进程存活，释放端口后 `healthz` 返回 `ok v0.4.5`，接续启动成功。

---

## v0.4.4 (2026-09-09) — JSON 兼容接口 + AI 去广告独立模块 + 播放器多浏览器兼容

> 供影视/TVBox/盒子调用的 JSON 通用接口（跨域、url 动态不硬编码）；AI 去广告做成独立 `ai/` 目录可单独更新、引擎可选；播放器兼容各种浏览器。

### 更新内容（[main.go](file:///workspace/main.go) + [`ai/`](file:///workspace/ai/)）

- **JSON 通用兼容接口 `GET /api/jx`**：支持 m3u8（去广告）、mp4 直链（透传）、官方视频页（官替链路），返回 TVBox 风格 `{code,success,msg,url,full,play,name,pic,header,format}`；已带跨域；`url` 用请求 Host 动态拼接不硬编码；
- **AI 去广告独立模块**：新建 [`ai/`](file:///workspace/ai/)（`config.json`+`VERSION`+`README.md`），独立版本可单独更新；`enabled/mode(ai/auto/basic)`+服务商/接口/key/模型/提示词；主程序读可执行文件旁 `ai/config.json`，缺失用内置默认=基础规则；新增强制 `engine=ai`（`/api/clean`、`/api/jx`、后台「解析测试」可选），AI 失败自动回退规则；`GET/POST /api/ai/config` 查看/更新（key 打码）；
- **播放器多浏览器兼容**：mp4/mkv/webm/flv 原生播放；iOS Safari 原生 HLS；其余 hls.js（4 CDN 兜底+失败提示）；后台/前台播放均覆盖；
- 统计埋点到 `/api/jx`、`/api/ai/config`。
- 版本升级 `v0.4.3 → v0.4.4`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；实测 `/api/jx`：空参 code=0，mp4 直链返回 `format=direct/url=原地址/code=1`，m3u8（沙箱断网）失败分支返回结构化 code=0+msg；响应带 `Access-Control-Allow-Origin:*` 全套跨域头；`/api/ai/config` 返回 `ai_version=v0.1.0`、key 打码。

---

## v0.4.3 (2026-09-09) — 前台显示接口调用详细信息

> 前台（首页 `/`）由落地页升级为「接口调用详情」页：展示接口调用次数明细、最近调用记录（时间/接口/参数/耗时/成功失败/说明），每 3 秒自动刷新，开放无需登录。

### 更新内容（[main.go](file:///workspace/main.go)）

- **前台 `/` 页面**：改为接口调用详情页，含统计卡片（接口调用总数/失败数、累计片段、广告占比、运行时长）+「接口调用次数明细」表 +「最近接口调用记录」表（时间/接口/请求地址参数/耗时/成功失败/说明），每 3 秒轮询自动刷新；
- **调用明细统计**：`ServerStats` 新增按接口计数的 `Calls` 与最近调用队列 `Recent`（最多 30 条）；`/api/stats` 返回 `calls` 与 `recent`；
- **埋点**：`/api/clean`、`/api/replace`、`/api/sites`、`/api/sites/toggle`、`/api/sites/test` 均记录调用明细（含失败与耗时、说明）；
- 对外开放解析接口不变；后台复用同一 `/api/stats`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；实测：`GET /` 返回「接口调用详情」页；触发 clean/replace 后 `/api/stats` 返回 `calls`（`/api/clean`、`/api/replace`）与 `recent` 明细（时间/接口/成功失败/耗时/说明）。

---

## v0.4.2 (2026-09-09) — 后台登录鉴权 + 资源站折叠/搜索/详情/复制

> 新增后台登录（默认 admin / admin123，后台可改密码并持久化）；资源站长列表改为可折叠、可搜索、可查看站点详情并一键复制播放链接（带加载进度条）。

### 更新内容（[main.go](file:///workspace/main.go)）

- **后台登录**：新增登录页 `/mxadmin/login`；未登录访问 `/mxadmin` 或后台管理接口会自动跳转登录；默认账号 **admin / admin123**，凭据以随机盐+sha256 摘要持久化到可执行文件旁的 `auth.json`（[.gitignore](file:///workspace/.gitignore) 已忽略，不入库）；
- **改密码**：后台页头新增「🔑 改密码」（`POST /api/auth/password`，需登录，校验原密码），修改后持久化；
- **退出登录**：页头「⎋ 退出」清会话并回登录页；
- **接口保护**：`/api/sites`、`/api/sites/toggle`、`/api/sites/test`、`/api/update/apply` 需登录（401）；对外解析接口 `/api/clean`、`/api/replace` 保持开放；
- **资源站列表体验**：折叠面板（原生 `<details>` 按「已启用/未启用」分组）→ 大幅缩短页面；顶部搜索框即时过滤；「详情」展开显示站点官网/接口/备注并「复制播放链接」；「全部折叠/全部展开」一键切换；启停/详情加载显示**进度条动画**。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；实测：未登录 `/mxadmin`→302 `/mxadmin/login`；错密码拒绝；admin/admin123 登录成功设会话；带会话 `/api/sites` 正常（122站/0启用）、无会话 401；改密码→新密码重新登录→改回 admin123 均成功，`auth.json` 持久化。

---

## v0.4.1 (2026-09-09) — 修复更新后不会自动重启

> 修复 Go 版远程在线更新的「下载替换成功但服务未自动起来」问题。

### 更新内容（[main.go](file:///workspace/main.go) `replaceAndRestart`）

- **端口竞态**：原流程先启动新进程、300ms 后才让老进程退出，新进程启动即绑定端口、此时老进程仍占用 → 报 `bind: address already in use` 直接退出，服务起不来。修复：更新时先关闭当前 HTTP 服务（异步 `Shutdown` 释放监听端口）、轮询等待端口空闲后再启动新进程；
- **SIGHUP 误杀**：老进程退出时若为终端/session leader（nohup/setsid 部署），可能把同进程组的新进程一起 SIGHUP 带走。修复：新进程以 `Setsid` 脱离当前会话启动，老进程退出不影响新进程；
- 更新日志补充到 `v0.4.0 → v0.4.1`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过；本地起 `:8095` 后另起同端口进程复现旧 bug：`bind: address already in use` 退出；修复逻辑改为先停服务释放端口，新进程可正常绑定。

---

## v0.4.0 (2026-09-09) — 官替链路（官方视频页→资源站→无广告）

> 回退到 v0.3.3 后，对照 PHP 版补齐 Go 版缺失的**官替（官方替换）核心链路**：识别平台→抓标题→资源站搜索→智能匹配→取集→去广告输出可播直链。

### 更新内容（[main.go](file:///workspace/main.go) + [`resource_sites.json`](file:///workspace/resource_sites.json) + [`sites_static.go`](file:///workspace/sites_static.go)）

- **官替链路 `GET /api/replace?url=<官方视频页>`**：域名识别平台（腾讯/爱奇艺/优酷/芒果TV/哔哩哔哩/搜狐/PP）、抓取 `og:title`/`<title>`（腾讯 `getinfo` 兜底）、`parseVideoTitle` 剥离季/集/画质得出基础剧名与集数、生成搜索关键词；
- **资源站搜索**：复用 PHP 版 122 个采集站列表（`resource_sites.json`，**默认全部禁用**），调 AppleCMS `?ac=videolist&wd=` 解析 `list[]`，支持 `$$$` 多线路与 `集数$url` 集数格式（`parsePlayURL`）；
- **智能匹配**：按基础剧名相似度（包含/公共字）+ 集数命中 + 季数一致性打分（阈值 65），按 `episode_num` 精准取集、相对地址自动补全；
- **去广告输出**：源 m3u8 交给既有 `/api/clean` 规则引擎，返回 `ad_skip_url` 无广告直链；后台「官替链路」面板一键解析 + 直连播放；
- **资源站管理**：`GET /api/sites`、`POST /api/sites/toggle`、`GET /api/sites/test`；后台「资源站管理」面板启停采集站并做搜索测试，启停持久化到可执行文件旁的 `resource_sites.json`；
- 内置配置以常量打入二进制（`sites_static.go`），首次运行自动落盘供后台修改；`build-go.yml` 改为构建整个包（`.`）。
- 版本升级 `v0.3.3 → v0.4.0`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build -o mxgt-go .` 通过（静态 7MB 零依赖）；
- 实测：`/api/sites` 返回 122 个站点且全部禁用；`/api/sites/toggle?name=量子&enabled=1` 持久化生效；`/api/replace?url=...m.v.qq.com...` 识别为「腾讯视频」并抓到 `base_title`；沙箱网络受限时搜索走失败分支返回结构化错误，不崩溃。

---

## v0.3.3 (2026-09-09) — 更新面板显示当前/最新版本 + 修复 Failed to fetch

> 更新面板默认即显示「当前版本 → 最新版本」（服务端渲染，不依赖客户端 fetch）；修复检查更新因远程连通慢导致前端 `Failed to fetch` 的问题。

### 更新内容（[main.go](file:///workspace/main.go)）

- **服务端渲染版本**：后台「远程在线更新」面板打开即显示「当前版本 → 最新版本 + 是否有新版」，由服务端 `renderUpdateBlock()` 直接渲染，网络差/离线也能看到版本；
- **修复 Failed to fetch**：清单拉取改用独立 **8 秒短超时**客户端（原 60s），避免连通性差时长时间阻塞 HTTP 请求导致浏览器报 `Failed to fetch`；前端 `checkUpdate()` 失败时不再覆盖已显示版本。
- 版本升级 `v0.3.2 → v0.3.3`。

### 验证

- 实测 `/mxadmin` 直接渲染「当前版本 … → 最新版本 …」；`/api/update/check` 0.1s 返回；`go vet`/静态编译通过。

---

## v0.3.2 (2026-09-09) — 后台入口改为 /mxadmin + 页头标注开发者

> 后台不再通过 `/` 直达，改为 `http://<IP>:8080/mxadmin`；后台页头显眼标注「开发者 · ssmhdssmhd」。

### 更新内容（[main.go](file:///workspace/main.go)）

- **后台入口 `/mxadmin`**：只有 `/mxadmin`（或 `/mxadmin/`）进入后台；原 `/admin` 已移除（返回 404）；
- **首页 `/` 改为落地页**：显示品牌「MXGT-Go 去广告服务」、当前版本与「开发者 ssmhdssmhd」，并提供「进入后台管理 →」链接，不再直接进入后台；
- **后台页头显眼标注开发者**：右上角显示「开发者 · ssmhdssmhd」+「品牌 MXGT」；
- 版本升级 `v0.3.1 → v0.3.2`。

### 验证

- `go vet` / 静态编译通过；实测：`/` → 落地页(200，含开发者与后台入口，非后台)、`/mxadmin` → 后台(200，含「开发者 · ssmhdssmhd」)、`/admin` → 404。

---

## v0.3.1 (2026-09-09) — 修复远程更新下载地址（Release tag 双 v 对齐）

> 远程更新时间步检查到下载 404：GitHub Actions 的 Release tag 形如 `go-vvX.Y.Z`（`VER` 本身含前导 `v`），而 `updateInfo()` 下载地址拼的是单 v `go-vX.Y.Z`，导致 404 无法下载。已改为 `go-vv…`，下载地址实测返回 200。

### 更新内容（[main.go](file:///workspace/main.go)）

- `updateInfo()` 下载地址 `releaseBaseURL + "/go-vv" + version + "/" + zip`（对齐实际 tag `go-vvX.Y.Z`）；
- 版本升级 `v0.3.0 → v0.3.1`。

### 验证

- `go vet` / 静态编译通过；`/api/update/check` 返回的 `download_url` 已正确指向 `…/release/download/go-vv0.3.0/…zip`，该地址 `HEAD` 返回 **HTTP 200**（修复前为 404）。

---

## v0.3.0 (2026-09-09) — 新增远程在线更新

> 服务内置远程自动更新：检查 GitHub Release → 下载 → 原子替换 → 自动重启，后台一键完成。

### 新增内容（[main.go](file:///workspace/main.go) + [.github/workflows/build-go.yml](file:///workspace/.github/workflows/build-go.yml)）

- **更新接口**：`GET /api/update/check`（检查最新版本）、`POST /api/update/apply`（下载并替换重启）；
- **版本清单**：GitHub Actions 发布后自动写入并提交根目录 `latest.json`（含最新版本号 + Release zip 文件名），服务从 `raw.githubusercontent.com` 读取（**不走 GitHub API，规避未认证限流**）；
- **更新流程**：下载 Release zip → 解出与当前同名二进制 → 备份旧版为 `mxgt-go.bak` → 原子 `rename` 替换 → 启动新进程（继承参数与端口）→ 本进程退出；
- **后台 UI**：新增「🔄 远程在线更新」面板，一键「检查更新 / 下载更新重启」；
- 版本升级 `v0.2.1 → v0.3.0`。

### 验证

- `go vet` / `CGO_ENABLED=0 go build` 通过；`/api/update/check`、`/api/update/apply` 正常注册；
- 清单未发布时优雅返回「检查更新失败: manifest HTTP 404」，不崩溃；后台更新面板正常渲染。

---

## v0.2.1 (2026-09-09) — 前端跨域 + 直接播放 + 无硬编码

> 前端补全跨域（CORS + OPTIONS 预检 + Range 放行），后台新增内嵌播放器「直接播放无广告」，播放地址基于当前站点动态推导，不硬编码域名/IP。

### 更新内容（[main.go](file:///workspace/main.go)）

- **全局跨域**：新增 `withCORS` 中间件覆盖所有响应（含后台页），统一补 `Access-Control-Allow-Origin` / `-Methods` / `-Headers` / `-Expose-Headers`，并放行 `Range`（TS 分片片断请求）；OPTIONS 预检返回 204；
- **内嵌播放器**：后台「解析测试」新增「▶ 直接播放无广告」按钮，点击后弹出 `video` 播放器（hls.js 多 CDN 兜底加载），**就地播放过滤后的无广告画面**；
- **无硬编码**：播放源地址由 `location.origin + /api/clean?...` **动态拼接**（非写死 IP/域名），并提供 `playURL()` 后端按请求 Host 动态推导的辅助函数。
- 版本升级 `v0.2.0 → v0.2.1`。

### 验证

- OPTIONS 预检 → 204 且带全套 CORS 头；`/api/clean` 返回 m3u8 带 CORS；`/api/stats`、后台页均带 `Access-Control-Allow-Origin`；
- 浏览器实测：后台渲染正常、解析输出 `#EXTM3U` 过滤后文本、点击「直接播放无广告」弹出播放器并显示动态播放源，控制台无致命报错。

---

## v0.2.0 (2026-09-09) — 新增后台管理页面

> 单文件服务新增玻璃拟态风格后台：访问服务地址（`/` 或 `/admin`）即进入，含运行统计 + M3U8 解析去广告测试 + 接口说明。全程内嵌单文件，仍零依赖。

### 新增内容（[main.go](file:///workspace/main.go)）

- **后台页面**：`GET /` 与 `GET /admin` 渲染内置后台（深紫→粉渐变 + 毛玻璃，与 PHP 版观感一致）；
- **运行统计**：新增内存统计（`ServerStats`）+ `GET /api/stats`，展示版本、运行时长、清洗请求数/失败数、累计片段数/广告段/保留段、广告占比、最近解析结果与时间；后台每 5 秒自动刷新；
- **解析测试**：后台输入 m3u8 + 聚合开关，一键调用 `/api/clean` 返回过滤后 M3U8 纯文本与统计；
- **接口说明**：后台内嵌 HTTP 接口清单；
- 原有 `/api/clean`、`/api/clean/json` 现在会记录到统计；`/healthz` 不变；未知路径返回 404；
- 版本升级 `v0.1.1 → v0.2.0`。

### 验证

- `go vet` / `go build` 通过，静态版启动正常；
- 实测：`/`、`/admin` 返回 200 HTML；`/api/stats` 初值全 0 → 清洗 1 次后正确变为 `requests=1 / total_segs=64 / kept_segs=64` 并带「最近」消息；未知路径 404。

---

## v0.1.1 (2026-09-08) — 启动失败修复：静态编译消除 GLIBC 依赖

> 服务器启动报错 `mxgt-go: /lib64/libc.so.6: version 'GLIBC_2.34/2.32' not found` 的根因与修复。

### 根因

- 云端编译（[.github/workflows/build-go.yml](file:///workspace/.github/workflows/build-go.yml)）在 `ubuntu-latest` 上执行 `go build`，默认启用 CGO，产物**动态链接** runner 的新版 glibc（要求 GLIBC_2.32/2.34+）；
- 部署服务器 `/www/wwwroot/go/go1/mxgtgo/` 的 glibc 较旧，动态加载器找不到所需版本 → **启动即失败**，无法运行。

### 修复

- 编译命令改为 `CGO_ENABLED=0 go build -ldflags "-s -w" -o mxgt-go main.go`：**纯静态链接**，`ldd` 显示 `not a dynamic executable`，零依赖 glibc，任何 Linux（含 CentOS 7 等旧系统）开箱即用；
- README 本地编译命令同步标注 `CGO_ENABLED=0`；
- **验证**：本地动态版 `file/ldd` 显示 `dynamically linked → libc.so.6`（复现原报错条件）；静态版 `file/ldd` 显示 `statically linked / not a dynamic executable`（6.6MB）；静态版启动 `./mxgt-go -addr :8080` + `GET /healthz` 返回 `ok v0.1.1`，`go vet` 通过。

---

## PHP 版更新日志（branch `main`）

## 当前版本 v5.15.11（2026-09-08）

> 学习 502 修复 + 后台全结果折叠：HTTP 异步执行立即返回避免 nginx 502；后台所有结果区域可折叠、滚动条拖动查看。

### 🔥 学习 502 修复 + 🗂️ 后台全结果折叠（[cron_ai_autolearn.php](file:///workspace/cron_ai_autolearn.php) + [mxadmin.php](file:///workspace/mxadmin.php)）

- **学习 502 修复**：AI 自动学习经 HTTP 回环触发时，学习（遍历全部资源站 + 深度解析）超过 nginx/PHP-FPM 超时会被掐断返回「502 Bad Gateway / 服务器返回非JSON响应」。`cron_ai_autolearn.php` 新增 `aiHttpDetach()`：PHP-FPM 下 `fastcgi_finish_request()`、其他环境 `Content-Length + Connection: close`，**立即返回 200 并断连，任务继续后台执行**——触发方不再等长任务，永不 502；
- **后台全结果折叠**：新增通用折叠组件 `makeFold()`，覆盖 **22 个结果容器**（视频分析 / 批量解析 / 资源站搜索 / 自动学习 / AI自动学习+日志 / 官替测试 / 嗅探测试 / 沫兮测试 / 数据库迁移 / 完整性检查 / 缓存清理 / 在线更新 / AI去广告 / 专业检测 / 插播识别 / 字幕分析 / 水印处理 / 接口选择）：
  - 点击标题栏折叠/展开，展开内容区**限高 420px + 自定义滚动条拖动查看**；
  - 双击标题栏**不限高**看全貌；
  - 折叠状态 `localStorage` 持久化；
- **验证**：HTTP 模式实测 0ms 返回 `{"success":true,"async":true}`；`php -l` + 主脚本 `node --check` 通过；浏览器实测 5 页折叠渲染与交互正常、控制台 0 错误。

### ⏱️ AI自动学习「长时间不动」修复（上一版 v5.15.10）

- **根因**：懒触发 `autoTriggerIfNeeded()` 内部只走 `exec` 后台执行 cron 脚本；服务器**禁用 exec()** 时 `@exec` 静默失败、无任何回退 → `last_run_time` 永远不更新 → 后台「AI自动学习」显示长时间不动；
- **修复-懒触发**：改用带三级回退的 `triggerBackgroundRunAsync()`（exec → fsockopen 非阻塞 HTTP → curl 短超时），exec 禁用环境也能真正触发学习；失效规则清理触发同步加回退；
- **修复-可重试**：`ai_autolearn/run` 不再预先更新 `last_run_time`（由 cron 实际执行 `run()` 成功后更新），触发失败不会把时间顶到未来导致数小时不再重试；
- **修复-定时脚本 DB 适配**：`cron_ai_autolearn.php` 检测到数据库配置时使用 `DbResourceSiteManager` + `DbDomainRuleManager`，DB 模式下定时学习正确写入 `domain_rules` 表（与 gx.php 一致）；
- **验证**：`php -d disable_functions=exec` 实测懒触发返回 `triggered=true`（走回退通道）；三文件 `php -l` 通过。

### 🛠️ 去广告监控数据异常修复（上一版 v5.15.9）

- **根因**：去广告监控数据落盘 `gz/monitor_data.php`，播放开启 `mon=1` 后多个请求并发写同一文件且无锁 → 文件被写坏（不完整 PHP 数组）→ 下次读取时 `@include` 抛 ParseError（`@` 无法抑制异常）→ `monitor/list` 等接口整体异常 → 后台提示「获取监控数据异常」；
- **修复-自愈**：`load()` 捕获 ParseError/非数组，自动备份损坏文件（`monitor_data.php.bak-时间戳`）并重建默认数据，接口立即恢复；
- **修复-防写坏**：`save()` 改为**原子写**（临时文件 + `flock` 排他锁 + `fflush` + `rename`），多请求并发不再写坏数据文件；
- **验证**：损坏文件场景实测备份+重建+record 正常；`monitor/list`、`monitor/status` 返回正常 JSON。

### 🤖 AI自动学习优化 + 🗄️ 数据库自动保存 + 🎯 去广告监控修复（上一版 v5.15.8）

- **默认全部资源站**：AI 自动学习 `max_sites_per_run` 默认 `0`（不限制）、`target_mode` 默认 `all`，一次学习覆盖全部**启用（未暂停）**资源站，不再只测前 3 个；
- **按速度/健康排序**：新增 `sort_by_speed`（`sortSitesBySpeed()`），按响应速度排序择优学习，复用 24 小时新鲜测速缓存、无缓存实时测速写回 `response_time`；
- **规则自动保存到数据库**：新增 `auto_save_rules`（默认 `true`）——DB 模式写 `domain_rules` 表、文件模式写 `rules_*.php`；
- **定时任务入库**：`gx.php` 新增 `buildAiLearner()`，开启数据库模式时 `task_ai_learn` / `task_ai_cleanup` 使用 DB 管理器直接入库；
- **去广告监控生效修复**：根因是播放链接从不带 `mon=1`，实时去广告监控从未被记录；`player/index.php` 播放链接补齐 `mon=1`，播放即触发 `AdMonitor` 记录与可疑删除识别；
- **资源站暂停不展示**：学习与展示仅取启用站点，暂停站不参与。

### 🔧 后台全功能体检 + MD5特征码分析修复（上一版 v5.15.7）

- **体检**：全部 PHP 文件 `php -l` 通过；后台调用 action 与 mx.php 一一对应；本地起服务实测只读/写回接口正常；浏览器逐页实测主要页面全部正常渲染、控制台 0 error；
- **修复 MD5特征码分析**：AI自动去广告页 `aiMd5Analyze()` 引用的 `aiSkipSaveMd5`/`aiSkipFastMode` 两个 checkbox 页面缺失（触发会 TypeError），且无触发按钮。已在「快捷操作」卡补「🔬 MD5特征码分析」按钮与「⚡ 极速MD5 / 保存MD5特征码入库」两个开关，功能恢复；
- **说明**：`resource_rules/*` 为数据库模式专属功能，文件模式返回「数据库不可用」属预期设计，启用 DB 后可用。

### 🚫 一键检测并屏蔽不可搜索资源站（上一版 v5.15.6）

- **全量检测**：两个管理器新增 `verifySearchCapability()`，遍历全部启用资源站，用探测关键词逐个真实搜索；无法搜索（接口失败/连不上/失效）**或**搜索返回不到任何结果的站点自动置为**暂停（屏蔽）**并记录原因，退出活跃列表，只保留可用站；
- **接口**：`sites/search_check`（参数 `keyword` 默认高频词「爱情」、`max` 限制数量），返回 `checked/usable/blocked/blocked_sites` 与逐站明细；
- **后台按钮**：资源站列表工具栏新增「🚫 检测并屏蔽不可搜索」，确认后逐个探测、提示可用/屏蔽数、自动刷新列表，被屏蔽站可在「显示已暂停」中查看/恢复。

### 🗂️ 资源站优先级统一100（上一版 v5.15.5）

- **优先级统一 100**：资源站列表全部 **122 个站点 priority 统一改为 100**；新增/编辑默认值 **100**（addSite、后台表单默认 `value=100`、编辑回填/提交兜底 `||100`），排序兜底 **99→100**；后台列表与搜索均按 priority **升序自动排序**（数字越小越优先），支持手动调低某站数值让其靠前匹配；
- **自动屏蔽不可搜索**：`searchAllSites` 搜索某站失败时自动置为**暂停（屏蔽）**，备注记录「自动屏蔽·不可搜索: 原因」，退出活跃列表不再反复请求无效站点；返回新增 `auto_blocked`（本次屏蔽数）与 `blocked_sites`（被屏蔽站点名）。

### 🎬 去广告整片误删黑屏修复（上一版 v5.15.4）

- **根因**：`repetitive-duration` 规则把影片**统一时长的正常码率切片**全量误判为广告（权重 55≥50 单独即删），如 `v.lzcdn31.com` 源 **37.8%** 的切片都是 4.0s，广告占比达 **58.5%** → 正片被整体替换为黑屏占位 TS，`mxjx` 输出**进度条在走但没画面**；
- **修复**：该时长桶占总段数 **≥35%** 即为视频自身切片节奏（内容），直接放行，不再判为广告；
- **效果**（2217 段回归）：广告占比 **58.5% → 10.2%**，正片完整保留，仅保留 DISCONTINUITY 边界与真正超短视频。

### ⚡ 资源站规则自动获取 + 🎬 外置播放修复 + 🎛️ 接口选择器（上一版 v5.15.3）

- **规则自动获取**：后台「资源站规则」页输入资源站采集链接（苹果CMS/通用 `ac=detail` 接口），自动拉取视频列表 → 逐个解析 M3U8 深度分析广告特征 → 自动汇总 **5 类规则**（时长/不连续/序列/文件名/关键词）写入 `resource_site_rules`；跨视频命中≥2 次才写入、同站同类型去重。
- **每 2 小时自动同步**：后台打开规则页自动检测距上次同步≥2 小时即后台异步更新；`resource_rules/sync` 可一键同步全部资源站；`gx.php` 新增 `resource_rules_sync` 任务并入 all 一条龙；自动维护页新增「⚡ 一键更新维护规则」按钮。
- **外置播放无画面修复**：根因是 `EXT-X-KEY`（AES-128 加密）与 `EXT-X-MAP`（fMP4/CMAF）在去广告输出时被丢弃，导致播放器无法解密/解复用 → **进度条动但画面全黑**；现已逐片段保留并按**密钥轮换**正确输出（占位黑屏 TS 显式 `METHOD=NONE`）。
- **接口选择器**：侧边栏「接口选择」页 —— 全局开关 + **独立/组合调用**模式；接口列表默认 **自定义清洗接口 `http://域名/api/clean/?url=`**（`{host}` 自动替换，http/https 均可）置顶，内置 沫兮/官解/官替/去广告 按需启停排序；开启后 `parse` 按顺序逐个尝试，首个成功即返回（标注 `via`）；内置「按当前配置测试调用」。
- **新接口**：`resource_rules/auto_fetch`、`resource_rules/sync`、`resource_rules/sync/status`、`resource_rules/sync/config/save`、`api_picker/config`、`api_picker/config/save`、`api_picker/run`。
- **验证**：`php -l` 6 文件全部通过；KEY 轮换往返一致；规则写入去重二次 0 新增；接口选择器 single 模式实测经「去广告」返回 play_url 与 via；内联 JS `node --check` 通过。

### 🛡️ 去广告实时监控防误删 + 🎬 占位模式 + 🔕 更新提示修复（上一版 v5.15.2）

- **占位模式不删段**：`mxjx` 新增 `ph=1` 占位模式、`parse_test` 过滤后默认占位 —— 广告段不再删除，URI 替换为**等时长黑屏静音占位 TS**（`placeholder_ts`），片段总数与时长不变，时间轴连续，**不再卡顿/跳画面/进度回跳**；播放器页与解析测试播放链接默认带 `ph=1`。
- **实时监控防误删**：新增 `gz/AdMonitor.php` 与后台「去广告监控」页面 —— 每次解析/`mxjx mon=1` 自动记录（总段/删除/占比/守护/命中规则/URI 快照），自动识别**高危/可疑删除**（高占比、全部删除、零散删除、守护触发）；每条记录可反馈「误删 / 正常」。
- **保护名单自动还原**：标记误删后，被删片段 URI 快照进入**保护名单**，`parse_test`/`mxjx` 解析时命中受保护 URI 的片段**自动还原保留**，不再重复误删；命中规则计入误报计数用于持续优化。
- **监控版本**：= 应用版本 + 三位数字计数（`v5.15.2.0001`），应用升级自动跟随；后台「重置监控数据」递增 0001→0002…。
- **更新提示修复**：更新弹窗「稍后再说」localStorage 记录已忽略版本，**同版本不再重复弹窗**，新版本自动恢复。
- **接口**：`monitor/status`、`monitor/list`、`monitor/feedback`、`monitor/protected`、`monitor/protected/add`、`monitor/protected/remove`、`monitor/reset`。
- **验证**：`php -l` 6 文件全部通过；AdMonitor 单元验证（高危识别/入保护/误报计数/isProtected/reset 版本递增）全部正常。

### 📣 公告携带 README 更新内容 + 🧩 M3U8 对比折叠（上一版 v5.15.1）

- **公告正文升级**：`announcement/list` 最新版本公告正文自动从 **README.md「当前版本」章节**提取完整更新内容（`content` 携带全文，新增 `readme_content` 字段），不再只显示一句话标题；日期优先取 README 标题日期，`text` 保持短标题兼容旧前端。
- **M3U8 对比折叠**：解析测试「原始 M3U8 / 过滤后 M3U8」**默认折叠**，标题栏「展开/收起」一键切换，展开限高 360px 内滚动；重新解析自动重置折叠态。
- **验证**：`php -l` 全部通过；本地实测公告返回完整 README 正文；内联 JS `node --check` 通过。

### 🧩 去插播兜底线路 + 🎬 M3U8测试播放修复（上一版 v5.15.0）

- **兜底线路设置**：后台「沫兮 API」页新增「去插播兜底线路」卡片，可添加/删除多条线路（名称 + 接口地址模板 + 启停 + 全局开关），默认内置「沫兮兜底 1 `https://mxqcb.ssmhd.com/api/clean/?url=`」；配置 DB 模式存 `sys_config`、文件模式存 `gz/fallback_config.php`。
- **官方资源兜底链路**：启用后输入官方资源（腾讯/爱奇艺/优酷/芒果/B站/搜狐/PP）自动：①先访问官方页面提取**影视剧名+剧集**（pt 平台适配器）→ ②资源站搜索最佳片源 → ③用搜索到的链接跑兜底接口清洗 → ④返回清洗后播放地址；接口业务错误码（如 `code=404 非本站资源`）自动识别并优雅回退到原有官替/官解链路；`moxi / parse / official_replace/resolve / official_replace/info` 四条入口全部接入。
- **解析测试播放修复**：M3U8 解析测试「播放过滤后」改用后端生成的**绝对地址过滤 M3U8 文本**播放（此前用片段手拼相对地址，Blob 场景无法解析导致**报错黑屏**），加密/地图片段也能正常播放。
- **片段实时跟随修复**：新增 hls.js `FRAG_CHANGED` 逐片段精确高亮 + timeupdate 平滑补充，**片段跟随播放实时切换**；修复重复绑定监听器问题。
- **验证**：`php -l` 全部通过；本地实测 `fallback/config` 读写、官方资源兜底链路优雅回退、`parse_test` 绝对地址输出、内联 JS `node --check` 全部正常。

### 🎬 沫兮去广告链接播放修复 + 🔄 官替优化（上一版 v5.14.9）

- **播放修复**：`mxjx` 输出去广告 M3U8 时彻底清除最外层 JSON_OUTPUT_GUARD 的 ob 包裹层（此前外层把 `#EXTM3U` 文本当 JSON 改写，导致沫兮/官替去广告完成后的无广告链接无法播放），缓存命中路径同步修复。
- **加跨域**：`mxjx` 输出 M3U8 显式输出 `Access-Control-Allow-Origin: *` 等 CORS 头，播放器页面与接口跨域（不同端口/子域/域名）时不再被浏览器拦截 m3u8 与后续 TS 请求。
- **无硬编码**：沫兮/官替生成的 `mxjx` 链接 `selfUrl` 全部基于 `$_SERVER` 动态推导，`url` 参数使用传入的真实地址，无任何硬编码域名/IP。
- **官替优化**：资源站返回的相对播放地址（如 `/2026/xx/index.m3u8`）自动基于视频页域名补全为绝对地址，`m3u8_url` 与剧集列表 `all_urls` 同步处理，避免播放器拿到相对地址黑屏。
- **验证**：本地实测 `mxjx` 输出真实 `#EXTM3U` 内容、Content-Type 正确、CORS 头生效、TS 重写为绝对地址，缓存 HIT 路径正常。

### 📐 侧边栏折叠 + 🎬 M3U8测试播放修复（上一版 v5.14.8）

- **播放修复**：M3U8 解析测试页播放器改用 **hls.js** 播放 m3u8（原生 video 不支持 HLS）；hls.js 多 CDN 兜底加载；停止/切换播放正确销毁 hls 实例。
- **片段列表**：新增「地址」列（绝对地址、超出省略+悬浮显示完整），固定列宽、横向滚动不挤压。
- **侧边栏折叠**：顶部栏「◧」按钮折叠为 64px 窄条（只显示图标），让图片/内容区显示完整；localStorage 持久化；移动端自动隐藏。

### 🔄 资源站多地址 + 自动换源（上一版 v5.14.7）

- **多地址**：新增/编辑资源站时可填写**多条采集接口地址**（`api_urls`），数据库版与文件版管理器同步支持；只填单个 `api_url` 的旧数据自动兼容。
- **测速排序**：后台新增「⚡ 测速排序」按钮，一键检测所有已填地址的可用性与响应时间，按「健康优先 + 速度升序」把最快可用源排到最前（接口 `sites/test_urls`）。
- **自动切换**：抓取/搜索视频时依次尝试所有地址，**首个成功即返回**，失败自动换下一个源（`switched_source` 标记），全部失败返回各地址失败明细，避免单一地址连接不到导致资源站不可用。
- **健康检测升级**：对资源站的每个地址分别测速，返回最佳可用地址 `active_url` 与各地址明细。
- 入口：后台「资源站管理」与「域名发现」页，采集接口表单均支持多地址列表 + 测速排序。

### 🔧 自动学习失败修复（上一版 v5.14.6）

- **根因**：自动学习拿到的视频 URL 多为 Master playlist（`#EXT-X-STREAM-INF`），解析器此前不跟随 variant，只得到 0 片段，学习链路报 `Unsupported operand types: array * int`，导致多线程/自动学习全部失败。
- **修复**：`src/M3U8Parser.php` 的 `parse()` 检测到 Master 且无片段时，自动跟随最高带宽 variant 重新解析媒体流（保留 `isMaster`/`variants`，新增 `selectedVariantUri`）。
- **验证**：实测 master 链接解析 0→645 片段，广告占比 71.16% 正常可学习。

### 🔍 最新公告实时化（上一版 v5.14.5）

- **实时最新公告**：公告接口 `announcement/list` 自动基于 `version.php` 生成首条「最新版本 v5.14.5 发布：<变更标题>」，始终显示当前最新版本，不再受手工维护的 `gg.txt` 过期影响。
- **历史公告保留**：`gg.txt` 里的历史公告正常叠加返回，回复结构不变。

## 📥 发布包下载

| 项目 | 内容 |
|---|---|
| 发布包版本 | **v5.14.4**（最近一次打包上传，源码当前版本为 v5.14.5） |
| 构建时间 | 2026-09-08 03:03（北京时间） |
| 发布包 | `release/MXGT_v5.14.4_202609080303.zip`（5.8M，155 个文件） |
| 下载 | https://github.com/ssmhdssmhd/MXGT/releases/download/v5.14.4/MXGT_v5.14.4_202609080303.zip |
| 源码 | [ssmhdssmhd/MXGT](https://github.com/ssmhdssmhd/MXGT) `main` 分支 |

发布包为可部署源码（已排除 `db/data.db` 数据库文件及 `sq.php` / `db_config.php` 等敏感配置），解压覆盖上线即可，保留你自己的授权与数据库配置。

> 发布脚本：`./release.sh`（仅打包）或 `GITHUB_TOKEN=xxx ./release.sh -r`（打包并上传 GitHub Release）。自动生成 `release/MXGT_v<版本>_<北京时间戳>.zip`。

### 🔧 在线更新源修复 + 健康检测修复

- **在线更新源修复**：`src/UpdateManager.php` 更新源由 `ssmhdssmhd/qcb` 更正为 `ssmhdssmhd/MXGT`，与 `update.php` 统一。此前 `mx.php?action=update/check` 与 `update/download` 走 `UpdateManager`，仍指向旧仓库 `qcb`（最新仅 v5.13.8＜远程 v5.14.2），导致一直判定"无更新"、版本拉不上去。
- **健康检测卡死修复**：`gz/ResourceSiteManager.php` 将 `checkSiteHealth` 的 `$timeout=8` 真正透传给 `fetchVideos`/`httpGet`（原 8s 超时参数从未生效，默认 30s 兜底 + 重试放大，大量不可达采集源串行把请求拖到分钟级）；`fetchVideos` 新增 `$timeout` 参数。
- **总预算保护**：`batchCheckHealth` 默认 20s 总时间预算，超时即中断返回并带 `skipped` 统计，后台一键健康检测不再挂起。
- 验证：`php -l` 全部通过。

### 🔧 接口去重与 API 文档补全（上一版 v5.14.3）

- **接口去重**：移除重复别名公告接口（`notice/*`，保留规范名 `announcement/*`）与特征码接口（`ad_signatures/*`，保留规范名 `signatures/*`）；保留公有解析别名 `jx / parse/parse / moxi/api` 以免破坏外部已引用链接。
- **API 文档**：`api_doc.php`「完整接口索引」新增「资源站规则 / AI自动学习 / 公告管理 / 嗅探设置」四分类，并补充 `info/version`、`official/list`、`official/platforms`、`parse_test`、`placeholder_ts`，列全所有规范接口。
- 验证：`php -l mx.php / api_doc.php` 全部通过。

### 🆕 新增后台两个独立页面：域名发现 + 资源站规则

侧边栏「资源管理」分组新增两个独立功能页：

**🕵️ 域名发现**
- 专注**添加 / 管理资源站域名与采集接口**（名称、官网、MacCMS/自定义采集接口、类型、备注）；
- 支持编辑、启停、删除、一键健康检测、按名称搜索、收录统计速览。

**🗂️ 资源站规则（全新独立规则库）**
- 按资源站分组配置 **时长 / 不连续标记 / 序列跳变 / 文件名特征 / 关键词拦截** 5 类广告特征规则；
- 支持增删改、启停、按资源站筛选、关键词搜索、一键清空；
- 数据存数据库 `resource_site_rules` 表，与自动学习产生的域名规则（`rules_*.php` 文件式）**完全独立、互不干扰**。

**后端能力**
- 新增 `resource_rules/*` 接口组（list/get/add/update/delete/toggle/clear）；
- 新增 `db/resource_site_rules` 表（SQLite + MySQL 双向 schema）；
- 老库升级自动补建表，线上升级无需手动操作。

验证：`php -l` 全部通过；本地实测 `resource_rules` 全链路 CRUD 通过，新页面与菜单渲染正常。

### ✨ 后台整体推倒重写视觉层（Glassmorphism 玻璃拟态）（上一版 v5.14.0）

参照目标视觉稿，将后台外观从「浅色半透明」整体重写为**深紫→洋红渐变 + 毛玻璃**现代风格：

- **渐变背景**：`#581c87 → #7e22ce → #a21caf → #c026d3` 135° 渐变，固定滚动不随内容移动；
- **侧边栏**：半透明白色毛玻璃（`backdrop-filter: blur(20px)`），Logo 白色→浅紫渐变文字，激活菜单 紫→粉 渐变胶囊 + 发光阴影；
- **顶部栏**：毛玻璃 + 柔和径向高光，与渐变背景一体化；
- **卡片 / 数值卡 / 面板**：半透明白玻璃（`blur(16px)`）+ 圆角 18px + 紫色柔和投影；
- **按钮 / 表格 / 输入 / Toast**：主按钮紫粉渐变 + 光晕，表格表头紫→粉渐变白字，输入框与 Toast 玻璃化；
- **兼容性**：不改动任何页面结构和业务 JS，21 个功能页面全部保留；背景图模式自动叠加紫色蒙层保持统一观感。

同时更新 `.gitignore`：忽略 `.trae-html-share-packages/` 打包缓存、`*.bak` 备份及 `test_*` / `_diag*` / `_probe*` 等测试诊断临时文件，避免污染仓库。

lint：`mxadmin.php` / `version.php` → 全部 `No syntax errors detected`；本地 `php -S` 返回 HTTP 200，皮肤层与页面结构加载正常。

### 🎯 真链接广告拦截验证 + 无广告链接修复（上一版 v5.13.11）

使用**内置广告的真实链接**对解析工具做端到端拦截验证，并修复「过滤后无广告链接」无法直接播放的问题：

- **全部广告正确标记并完美拦截**：
  - 前置/中插/后置广告（关键词 + 文件名片段模式）全部命中移除（mixed.m3u8：89 段中 14 段广告全拦截，保留 75 段正片）；
  - `#EXT-X-AD-START/END`、`#EXT-X-DATERANGE`、`#EXT-X-CUE-OUT/IN` 广告标签段被识别，且过滤后输出中这些广告标签**全部剔除**；
  - **内置广告字幕**（`SUBTITLES`/`CLOSED-CAPTIONS` 媒体轨及变体上的字幕属性）可被正确剥离，仅保留音频与视频。
- **无广告链接修复（src/M3U8Parser.php）**：
  - 解析后为每个片段填充 `absoluteUri`（按媒体 URL 基地址解析），配合 `useAbsoluteUrls` 产出**绝对地址**的过滤后 M3U8——返回的无广告链接可直接交给播放器播放，不再因相对路径解析错源而黑屏；
  - 绝对地址补全端口号，自定义端口（如 `:8091`）不再丢失；
  - 修复 `#EXT-X-MEDIA` 解析偏移（15→13），此前 `TYPE` 读空导致字幕/广告字幕轨过滤失效。
- lint：`src/M3U8Parser.php` / `version.php` → 全部 `No syntax errors detected`。

### 🧪 新增：M3U8 解析测试工具页（v5.13.10）

后台侧边栏「接口工具」新增 **M3U8 解析测试** 页面：

- **输入解析**：输入 m3u8 地址 + 站点/代理选择，一键解析，实时展示总片段/广告段/保留段/耗时统计
- **播放模式**：播放过滤后 / 播放原始 / 整片 / 过滤后，Blob 直接播放规避 mxjx 通道 JSON 守卫改写问题
- **跟随定位**：播放时自动高亮对应片段行并滚动到可见，实时更新命中广告区域，支持上一处/下一处定位
- **片段列表**：全部/广告/正片/已标记 过滤 + 按段号/地址搜索 + 区间批量标记（疑/止/广告类型）+ 取消标记
- **对比视图**：原始 M3U8 与过滤后 M3U8 左右分栏展示，一键复制
- 新增后端接口：`mx.php?action=parse_test`（返回片段明细、原始/过滤后 M3U8 文本、统计）

> 注：v5.13.9 曾将在线更新源切到本仓库（MXGT），并与 curl_close 弃用修复同步发布。

### 🚀 在线更新源切换：qcb → MXGT

- **更新源切换**：`update.php` 的在线更新源由 `ssmhdssmhd/qcb`（main）改为当前仓库 `ssmhdssmhd/MXGT`（main），后续版本更新从当前仓库拉取。
- **PHP 8.5 兼容修复**：移除外层 `curl_close()`（PHP 8.0+ 自动释放句柄，8.5 已废弃），消除 `Deprecated` 警告，保证更新检查/下载逻辑无报错输出。
- lint：`update.php` / `version.php` → 全部 `No syntax errors detected`。

### 🚑 Hotfix：虾米官解新地址 `https://jx.xmflv.cc/?url=&ref=` 接入 + HTML播放器类型支持 + {url}/{ref}占位符 + Cloudflare 403 兼容

**用户诉求**：
> 虾米解析更换为 `https://jx.xmflv.cc/?url=&ref=`（原 `114.134.184.91:9002` 已于 2026-08-14 加签名+白名单验证，未授权IP必败）。

**D1 新接口探测定案**：
`jx.xmflv.cc` 返回的是**浏览器端 HTML 播放器页面**（`<title>虾米播放器…</title>` + `<div id=Xmflv>` + 混淆 Xmflv JS runtime 拉流），api/v2/mx.php/api.php/jx.php 等传统 JSON/Redirect 子路径**全部 404**，HTML 里也没有裸 m3u8 URL。
→ **最终 play_url 就返回**我们拼好的整段 `https://jx.xmflv.cc/?url=URLENCODED&ref=URLENCODED`，客户端直接 302 跳转或 `<iframe src=>` 即可播放。

---

#### ✅ 用户 20 秒立即可用

> **Option A：新安装 / 未改过后台默认配置 → 无需任何操作**：
> v5.13.3 默认已把 5 处官解配置（sniffer.official_apis / sniffer.official_api / 顶层 official_apis fallback / sniffer_config 同名 2 处）**enabled=true**，url=`https://jx.xmflv.cc/?url={url}&ref={ref}`，type=html_player，首页直接点击解析即生效。

> **Option B：曾改过后台配置仍挂旧 114.134.184.91 → 2 步立刻修复**：
> 1. 后台 `mxadmin.php → 🔍 嗅探设置` → 若弹出红色告警 → 点「✅ 一键修复」→ 保存；
> 2. 回到官解接口卡片：URL 改成 `https://jx.xmflv.cc/?url={url}&ref={ref}`，接口类型选 **html_player**，保存 → 回首页刷新 ✅。

---

#### 🛠 代码侧升级（D1~D5，6 文件 lint 全过）

| 项 | 落地位置 | 说明 |
|----|--------|------|
| D3-① 6 占位符 | [PerformanceOptimizer.php buildApiUrl](file:///workspace/xt/PerformanceOptimizer.php#L513-L551) | `{url} / {ref}/{referer} / {origin} / {ts} / {t}`；模板无占位符时保留纯后缀拼接老行为 |
| D3-② 精准 Referer 推断 | [PerformanceOptimizer.php guessPlatformReferer](file:///workspace/xt/PerformanceOptimizer.php#L494-L511) | 优酷/爱奇艺/腾讯视频/芒果/乐视/B站/搜狐/PPTV 返回官方 Referer；其余按原 host 推断 |
| D3-③ HTML 播放器页 wrapper 识别 | [PerformanceOptimizer.php extractVideoUrl](file:///workspace/xt/PerformanceOptimizer.php#L666-L710) | 调用 jiami 闭包前前置 4 类命中（type=html_player / 标题虾米播放器 / id=Xmflv / host=xmflv.cc 且特征JS），命中直接把 buildApiUrl 拼好的 xmflv.cc 整段 URL 作为 play_url 返回，无需抽 JSON 字段 |
| D4 Cloudflare 403 兼容 + UA Chrome126 | [PerformanceOptimizer.php createCurlHandle](file:///workspace/xt/PerformanceOptimizer.php#L420-L492) + [config.php http.user_agent](file:///workspace/xt/config.php#L159-L167) | host 含 xmflv.cc/jmflv/jx.* 自动补 Accept/Origin/Referer/sec-ch-ua三件套/Upgrade-Insecure-Requests；UA 默认 Chrome126（Mozilla/5.0或空会自动兜底） |
| D4 5 处配置启用新地址 | [config.php sniffer.official_apis](file:///workspace/xt/config.php#L22-L45) / [sniffer.official_api](file:///workspace/xt/config.php#L46-L59) / [顶层 official_apis fallback](file:///workspace/xt/config.php#L98-L117) / [sniffer_config.php official_apis](file:///workspace/xt/sniffer_config.php#L19-L38) / [sniffer_config.php official_api](file:///workspace/xt/sniffer_config.php#L40-L57) | 5 处 enabled=true + url=`https://jx.xmflv.cc/?url={url}&ref={ref}` + type=html_player + Chrome126 headers |
| D4 parseVideoByOfficialChannel 承接 | [server.php parseVideoByOfficialChannel](file:///workspace/xt/server.php#L413-L457) | 新 URL 无 `.m3u8` 后缀 → 走 else 分支：setCache + buildResult(200,解析成功,play_url=整段xmflv.cc URL)，CDN/广告处理直接交给 jx.xmflv.cc 官方播放器 |

```bash
php -l xt/PerformanceOptimizer.php  # ✅
php -l xt/config.php                # ✅
php -l xt/sniffer_config.php        # ✅
php -l xt/server.php                # ✅
php -l mxadmin.php                  # ✅
php -l version.php                  # ✅  v5.13.3  version_code=51303
```

📄 完整修复图文 + 操作清单见 **CHANGELOG.md → v5.13.3**。

---

## 上一 Hotfix 版本 v5.13.2（2026-08-14）

### 🚑 Hotfix：虾米官解 api/v2 「验证失败!」根因修复 + 静默失败可追溯 + 后台告警横幅一键修复

**用户反馈原始错误**：
> 调用 `http://114.134.184.91:9002/mx.php?action=api/v2&type=parse&url=https://v.youku.com/v_show/id_XNjU0MjcxNTM1Ng==.html`
> 返回 `{"success":false,"code":500,"message":"❌<br>验证失败!","type_name":"虾米解析"}` → 前端一直转圈，不知道错在哪。

**根因结论（非项目 Bug）**：第三方虾米官方服务器 `114.134.184.91:9002` 已于 **2026-08-14** 对 `api/v2` 接口新增签名 + 白名单 IP 校验，**未授权 IP 无论传什么 UA / Referer / 时间戳都 100% 返回验证失败**，无法通过补 header 绕过。

---

#### 👩🔧 用户端「1 分钟立即修复，嗅探报错立刻消失」

> **Option A（一键修复，强烈推荐）**
> 1. 进入后台 `mxadmin.php` → 左侧菜单「接口工具 → 🔍 嗅探设置」
> 2. 页面顶部会出现红色告警横幅，直接点击 **「✅ 一键修复：取消该官解启用 + 官替URL置空 + 切到 replace 主路由」**
> 3. 页面底部会自动滚动高亮 **「💾 保存嗅探设置」**，点它
> 4. 回首页刷新播放页 → 嗅探报错立即消失 ✅

> **Option B（手动做，等价结果）**
> 1. ① 选择当前解析通道 → 选 **官替接口（replace v5.11 推荐主路由）**
> 2. ② 接口详细配置 → 「1 官解接口」左上角 **取消**「启用此接口」（114.134.184.91:9002 已失效，启用只会白等）
> 3. ② 下方「2 官替接口」→ 保持「启用此接口」= ✅，并把 **接口地址 URL 留空**（留空 = 走本地 OfficialReplaceManager 直调，比 HTTP 回环快 30-70%，不会再 502/验证失败）
> 4. 💾 保存 → 回到首页播放页刷新

---

#### 🛠 代码侧修复（C1~C5，全部 lint 通过）

| 步骤 | 落地位置 | 做了什么 |
|------|--------|--------|
| **C3 结束静默失败黑暗期** | [PerformanceOptimizer.php](file:///workspace/xt/PerformanceOptimizer.php) | 新增 `recordFailedApi()`：HTTP 非200 / 空响应 / **HTTP 200 但 {success:false,code:500} 业务级错误** / 成功但视频字段空 → 四类错误全部写进 `$GLOBALS['XT_FAILED_API_REQUESTS']`，字段含 {reason,biz_message,http_code,response_len,ts_ms}；`callApiSingle` 和 `concurrentRaceRequest` 两条链路都接入 |
| **C2 默认配置下线 5 处失效官解** | [config.php](file:///workspace/xt/config.php) + [sniffer_config.php](file:///workspace/xt/sniffer_config.php) | `sniffer.official_apis / sniffer.official_api / 顶层 official_apis 兜底` + `sniffer_config 的 2 处` 共 5 条 `114.134.184.91:9002` 全部 **enabled=false**，name 明确标注「2026-08-14起需签名验证，请替换或改用官替本地直调」；sniffer_config.update_date=2026-08-14 |
| **C4 后端诊断失败明细** | [server.php](file:///workspace/xt/server.php) B4 嗅探诊断 | 失败 JSON 的 `step_trace.summary` 和 `debug_info.sniffer_diagnostic.failed_api_requests` 中逐条列出官解失败：`1. 虾米官解 → 业务级错误：验证失败!；HTTP=200；resp_len=209；上游原消息=❌<br>验证失败!`；新增两条自动修复建议（验证失败命中 → 切 replace；http_code=0 连接失败 → 停官解） |
| **C4 红色告警 Banner + 一键修复** | [mxadmin.php](file:///workspace/mxadmin.php) 嗅探设置页 | 加载完配置后检测 enabled=true 且 URL 含 `114.134.184.91 / :9002` → 弹出红色横幅，含 4 步操作清单 + `xiamiBannerOneClickFix()` 按钮（自动设参数 + 标脏 + 滚动高亮保存按钮 + 5 秒 Toast） |
| **C5 回归** | 6 个核心文件 + version | `php -l` 全部 **No syntax errors detected**；对外 JSON 字段前后向兼容；API 客户端无升级必要 |

📄 详细图文 & 每步修复前后对比见顶部 **CHANGELOG.md → v5.13.2**。

---

## 上一 Hotfix 版本 v5.13.1（2026-08-17）

### 🚑 嗅探测试 502 Bad Gateway 根因修复 + 报错 UI 美化 + 诊断时间线

**用户截图问题 1 分钟解决操作（立即恢复）**：
> 1. 嗅探设置 → ① 选择「官替接口 replace v5.11 推荐」（已选就不动）
> 2. ② 接口配置 → 「官解解析接口」**取消**右上角「启用此接口」（虾米 114.134.184.91:9002 已宕机，会让 fallback 白等 502）
> 3. ② 下方「官替接口」的接口地址 URL **留空** → 本地直调（比 HTTP 回环快 30-70%，不会再 502）
> 4. 💾 保存设置 → 重新「▶ 测试解析」

**代码侧修复（v5.13.1 新增 B1~B4 + UI 美化）**：
- B2 官替直调 CPU 过载 → 预算时间保护（25s 软中断降级），不再触发 Nginx FPM 超时吐 502 HTML
- B3 非 JSON/502 报错不再整段 HTML 裸贴 → 彩色分级诊断卡（6 档：502/504/500/403/空响应/异常，双栅格「可能原因 / 修复建议」+ 原始响应可折叠）
- B4 后端失败自动补一条「🕵 嗅探通道诊断」时间线条目（展示当前模式/启用了哪些接口/宕机服务器自动识别/针对性 fix_tips）

PHP lint 0 错误通过，API 签名兼容旧客户端。

---

## 上一重点版本 v5.13.0（2026-08-17）

### ✨ 重点更新：后台全面美化升级（与嗅探设置风格统一）

> **用户需求响应**：「后台优化和美化和嗅探设置里面一样，好看，干净美观」——以用户好评的「嗅探设置」页面为视觉基准，把其余 19 个后台页面全部对齐同一设计规范。

#### 🎨 v5.13 后台美化 5 大核心升级

| 升级项 | 说明 |
|--------|------|
| 🧱 **7 类通用组件** | step-badge（编号徽章）/ overview-grid（概览双栅）/ form-grid·inline-form-grid（表单双栅）/ sub-card（子分组卡）/ action-bar（按钮栏）/ status-pill（状态徽章）/ section-caption + form-tip（说明文案），全站共享，改一处全站生效 |
| 🎨 **设计令牌 CSS 变量** | 6 主色（primary/success/warning/danger/info/purple）+ 三级圆角 + 三级阴影 + 三级文本色，统一不漂移 |
| 🔢 **全局步骤编号系统** | 每页每个大模块 `step-title + step-badge` 编号 ①②③，小模块 `sub-card-header + 字母徽章` A/B/C，信息层级一目了然 |
| 🌈 **AI 四大页保留品牌色** | ai_skip（紫）/ ai_insert（粉紫）/ ai_subtitle（青绿）/ ai_watermark（蓝青）原渐变横幅完整保留，外层叠加统一骨架，既保持辨识度又统一风格 |
| 📱 **响应式全覆盖** | 所有栅格用 `auto-fit + minmax`，窄屏自动变单列；按钮 `flex-wrap` 自动换行，PC/平板/手机都好看 |

#### 📋 覆盖 19 个后台页面（4 批交付）

```
A4-1 3页  播放记录 / 批量解析 / 视频广告分析
A4-2 4页  规则管理 / 资源站 / AI自动学习 / 官方资源站
A4-3 6页  官替解析 / 魔西API / 播放器 / 数据库 / 系统更新 / 自动维护
A4-4 6页  公告 / 授权 / AI去广告 / AI插播识别 / AI字幕分析 / AI水印处理
```

#### ✅ 回归验证 100% 过

- **PHP lint**：`mxadmin.php` → No syntax errors detected
- **零逻辑改动**：纯 HTML/CSS class 重排，不改任何接口调用与 onclick 函数名，原 API 行为完全不变
- **去内联样式**：大量散乱 `style="display:flex;gap:12px;..."` 统一替换为 `.action-bar / .sub-card / .form-tip / .toggle-label` 类，后续好维护

---

## 上一重点版本 v5.12.0（2026-08-16）

**【6平台独立元数据解析器(策略模式) + 极简提取减轻服务器负担】**

> **用户需求响应**：完善剩下的各个平台，链接获取影视剧名和集数的方式，只需要获取到影视剧名和集数就好，其他不要，减轻服务器负担，剧名和去替换和集数，去非正片内容输出，确保无广告无插播等影响观感的内容和不雅内容。

#### ✨ v5.12 核心三项改造

| 改造项 | 说明 |
|--------|------|
| 🏗️ **策略模式拆分** | `fetchMeta_Youku / fetchMeta_Tencent / fetchMeta_Iqiyi / fetchMeta_Mgtv / fetchMeta_Bilibili / fetchMeta_Generic` — 6 个独立方法，各自维护互不干扰，改一个平台不影响其他，好维护 |
| ⚡ **极简提取 = 减轻负担** | **只提取 `base_title(剧名)` + `episode_num(集数)` 两个字段**，`description/cover/subtitle_guess/total_episodes/raw_title/hits` 其余 7 个字段全部固定为空/空数组，内存占用归零，单请求处理更快 |
| 🚫 **非正片占位不中断** | 基于 v5.11 的 MD5 + 黑屏静音 TS 占位流程保持不变：广告段 URI 替换为本地黑屏静音 TS，EXTINF 时长不变，段数不删除 → 进度条不回跳、解码器不缓冲中断、无广告/插播/不雅内容输出 |

#### 🧠 通用提取引擎 `_extractQuickBaseAndEpisode`（三层优先级）

```
① 内联 JS（前260KB即break）→ ② meta标签 → ③ og:title/<title>兜底
```

- **内联 JS 双写法自动分发**：扁平字段（`showName: "九门"`）+ 嵌套对象（`partOfSeries: { name: "狂飙" }`）
- **剧名强清洗**：脱壳《》<>引号 → 去平台/分类后缀（-优酷/-腾讯视频/-在线观看/-高清/-纪录片…）→ banWords 黑名单过滤 → 长度 2~30 校验
- **集数多格式识别**（一次扫描 320 字符短文本）：第X集/话/期/部/季、EPXX、2/24（分数斜杠式）

#### 📺 各平台差异化优先字段

| 平台 | 优先取 | 结果样例 |
|------|--------|---------|
| 🟡 优酷 | `usercfg.showName / videoShowName`（API级纯剧名，避免副标题污染） | 九门 / 第2集 |
| 🟢 腾讯 | `ld+json partOfSeries.name + episodeNumber`（schema.org 标准字段最稳） | 狂飙 / 第39集 |
| 🟢 爱奇艺 | `og:video:series_name` meta（官方元数据）→ `Q.playerInfo.albumName` | 莲花楼 / 第20集 |
| 🟠 芒果TV | `__INIT__.showInfo.showName/seriesName`（芒果内嵌对象） | 乘风2024 / 第12期 |
| 🔵 B站番剧 | `mediaInfo.season.title/seasonName`（**保留"第二季"**，利于资源站匹配） | 咒术回战 第二季 / 第24话 |
| ⚪ B站UGC | `videoData.title`（保留括号修饰词，UGC无集数概念） | 迈克杰克逊1995年MTV颁奖典礼现场(4K修复) / null |
| ⬜ 通用兜底 | `showName/seriesName/albumName/partOfSeries.name` 常见字段并集 | 庆余年第二季 / 第36集 |

#### 🔬 Step Trace 调试可视化（mxadmin.php 嗅探测试区）

失败时一眼定位是**哪一步**出问题：时间线 UI 按 `✓成功 △警告 ✕失败 ℹ信息` 四色标记展示「平台识别 → 官方页面抓取 → 元数据提取 → 资源站搜索 → AI匹配 → 集数定位 → 去广告处理」每一步的状态 / 耗时 / 详情，`buildResult` 和 `callOfficialReplaceDirectV2` 全链路透传 `step_trace`。

#### ✅ 回归测试 100% 通过

- **Mock 测试（7/7 全过）**：Youku / Tencent / Iqiyi / Mgtv / Bilibili-Bangumi / Bilibili-UGC / Generic，`base_title + episode_num` 双正确，且"轻负担"断言（其余字段全空）通过
- **PHP lint**：`gz/OfficialReplaceManager.php / xt/server.php / mx.php` 核心 3 文件 0 语法错误

---

### 上一版本重点（v5.11 → v5.10.9）

**【P0 解析失败根因修复 + 官替优先智能识别新架构 + AI+MD5非正片占位不中断】**

- `isSafeVideoUrl` 三层守卫：original_url 陷阱彻底修复（虾米官解验证失败不再把原始页面URL误当视频流地址返回假成功）
- 默认 `mode=replace`（官替优先），本地直调 `callOfficialReplaceDirect(V2)` 比 HTTP 回环快 30-70%
- v5.11 新增 **MD5+黑屏静音TS占位**：广告段不删除（避免进度条回跳/解码器中断），URL 替换为本地生成的黑屏静音 TS，观感 100% 干净（无广告/插播/水印/不雅内容）

## 功能特性

- 🎯 **多维度广告检测** - 支持关键词、文件名模式、时长范围、不连续标记、序列号跳跃、SCTE-35、CUE-OUT/IN 等多种检测规则
- 🧠 **智能聚类算法** - 自动识别广告片段集群，减少误判
- ⚡ **高性能解析** - 纯 PHP 实现，无需外部依赖，优化 CURL 请求和缓存机制
- 📊 **专业视频广告分析** - 详细的广告分析报告，支持可视化展示，专业级分析深度
  - 插播点分析（片头/片中/片尾插播识别）
  - 广告类型分类统计
  - 心理特征分析（注意力抓取、插播频率、可观看性评分）
  - 置信度评估系统
- 🔬 **广告标记全面支持** - 解析多种 M3U8 广告标记
  - EXT-X-DISCONTINUITY / DISCONTINUITY-SEQUENCE
  - EXT-X-CUE-OUT / EXT-X-CUE-IN
  - EXT-OATCLS-SCTE35 / EXT-X-SCTE35
  - EXT-X-AD 自定义广告标签
- 🎬 **内置播放器** - 集成 DPlayer + hls.js，支持无广告在线播放
- 🎥 **在线播放器页面** - 独立的 /player 播放器页面，支持直接输入链接播放
- 🔧 **域名规则管理** - 按域名管理广告规则，支持快速模式去广告
- 📚 **智能自动学习** - 每次分析自动学习优化规则，支持重复学习迭代
  - 插播模式学习
  - 广告类型学习
  - 心理画像构建
  - 置信度迭代优化
- 📦 **规则导入导出** - 支持 JSON 格式的规则导入导出，方便备份和分享
- 🌐 **资源站管理** - 内置 50+ 资源站，支持官网访问和视频采集
- 🔍 **搜索影视学习** - 搜索指定或热门影视名称，用 M3U8 域名进行学习
- 🔄 **官替解析系统** - 官方视频链接智能替换，自动匹配资源站无广告源
  - 支持腾讯视频、爱奇艺、芒果TV 等主流平台
  - 智能提取视频信息（剧名、季数、集数）
  - 多 API  fallback 策略，稳定获取视频元数据
  - 多关键词组合匹配，提高匹配成功率
  - 自动去广告集成，直接返回无广告播放链接
  - 完整日志记录，便于排查优化
- 🤖 **自动化规则更新** - 定时从资源站采集视频自动学习更新规则
- ⏰ **定时任务支持** - 支持宝塔/Cron/URL触发等多种定时任务方式
- 🧠 **AI 自动学习（频繁更新专用）** - 每隔几小时自动从指定资源站（默认如意）获取热门/更新视频，提取 rym3u8 地址进行深度广告分析并更新规则
  - 按小时调度（1-24h），与原自动学习（按天）互补
  - 按 `play_from` 过滤（rym3u8），精准定位无广告源
  - 热门视频优先排序，视频去重避免重复学习
  - 默认采集 50-100 个样本充分学习
  - EnhancedAdRuleEngine + ProfessionalAdDetector 双引擎深度分析
  - 智能跳过 MD5/哈希类文件名，避免误判
  - 安全机制：广告占比≥90% 自动回退原始 M3U8
  - 可单独配置目标资源站和播放源标识
- 🔄 **动态规则更新** - 提供 gzgx.php 接口，支持远程动态更新规则
- 🌐 **Web API 服务** - 支持通过 URL 参数直接调用，返回 JSON 或 M3U8
- 🔓 **CORS 支持** - 支持跨域访问，可直接在前端调用
- 📱 **响应式界面** - 美观的后台管理界面，支持移动端访问
- 📺 **TVBox / 影视App专用解析接口** - `jiexi.php` 专为电视盒子和影视App打造
  - 支持 TVBox、影视仓、喵影视、影迷大院等主流影视App
  - 兼容多种参数：url/wd/v/video/t/u/play/src
  - 支持 5 种返回格式：JSON / 影视CMS标准 / 302跳转 / XML / JSONP
  - CORS 跨域支持，开箱即用
- 🚀 **超级嗅探模块（xt/）** - 独立官解对接 + AI去广告轻量模块
  - 官解接口对接，支持 redirect/json/text 三种类型
  - 规则引擎 + AI 大模型双重广告识别
  - 解析结果缓存，重复请求毫秒级响应
  - 相对路径自动转绝对路径，直接播放
- 🔍 **后台嗅探设置** - 一处管控嗅探解析通道
  - 支持放置「官解解析」和「官替接口」两个接口
  - 每个接口独立开关 + 接口地址/类型/字段名配置
  - 通过「当前通道」单选决定走官解还是官替
  - 当前通道失败自动 fallback 到另一通道
  - 配置文件：`xt/sniffer_config.php`（后台自动维护）

## 快速开始

### 部署方式

将项目所有文件上传到你的 PHP 网站根目录或任意子目录即可使用。

### 目录结构

```
├── mx.php                 # API 接口入口
├── mxadmin.php            # 后台管理页面
├── jiexi.php              # TVBox/影视App专用解析接口
├── cron_ai_autolearn.php  # AI自动学习定时任务（频繁更新规则专用）
├── index.php              # 首页路由
├── router.php             # 路由配置
├── xt/                    # 超级嗅探模块（官解对接 + AI去广告）
│   ├── api.php            # 多端调用统一入口
│   ├── server.php         # 服务端核心解析
│   ├── AdFilter.php       # 广告识别引擎
│   ├── clean.php          # 去广告 m3u8 播放代理
│   ├── config.php         # 全局配置
│   ├── sniffer_config.php # 嗅探设置配置（后台自动维护）
│   └── README.md          # 模块文档
├── gz/
│   ├── DomainRuleManager.php   # 域名规则管理器
│   ├── EnhancedAdRuleEngine.php # 增强版广告规则引擎
│   ├── ResourceSiteManager.php # 资源站管理器
│   ├── sites_config.php        # 资源站配置列表
│   ├── gzgx.php                # 动态规则更新接口
│   └── rules_*.php             # 各域名规则文件
├── src/
│   ├── M3U8AdSkipper.php  # 主类 - M3U8 去广告
│   ├── M3U8Parser.php     # M3U8 解析器
│   ├── AdRuleEngine.php   # 广告规则引擎
│   ├── AdFilter.php       # 广告过滤器
│   ├── CacheManager.php   # 缓存管理器
│   ├── UpdateManager.php  # 系统更新管理器
│   ├── CryptoUtil.php     # 加密工具
│   ├── AuthConfig.php     # 授权配置
│   └── AuthValidator.php  # 授权验证器
├── cache/                 # 缓存目录
└── README.md              # 项目说明
```

## 后台管理

访问 `mxadmin.php` 进入后台管理页面，包含以下功能模块：

### 1. 视频分析

- 输入 M3U8 视频链接进行广告分析
- 详细的广告片段统计和可视化展示
- **快速模式**：已有域名规则时直接使用规则快速去广告
- **自动学习**：分析时自动学习并更新规则
- 支持无广告播放链接生成和复制
- 内置播放器测试

### 2. 规则管理

- 按域名管理广告规则
- 支持时长规则、DISCONTINUITY 规则、序列号跳跃规则、文件名模式
- 规则学习次数统计
- 单条规则导出
- 全部规则导出/导入

### 3. 资源站管理

- 内置 50+ 资源站配置（官网、采集接口、扩展备注）
- 资源站增删改查管理
- 一键访问资源站官网
- 在线获取资源站最新视频列表
- 视频链接一键复制和分析
- 支持按名称搜索过滤

### 4. 搜索影视学习

- 搜索指定或热门影视名称
- 支持单个资源站搜索或全部资源站批量搜索
- 显示搜索结果的 M3U8 视频链接和域名信息
- 查看多个播放源的视频链接
- 一键学习指定视频的广告规则（基于 M3U8 域名）
- 支持复制链接、分析视频等操作

### 5. 自动学习配置

- 可配置自动学习开关
- 可设置更新间隔天数
- 支持按指定影视名称搜索学习
- 可配置每站视频数和最大站点数
- 可设置最小片段数和最大广告占比
- 一键立即执行自动学习
- 详细的学习结果统计

### 7. 无广告播放

- 输入视频链接直接无广告播放
- 支持复制无广告链接
- 内置 DPlayer 播放器

### 8. 在线播放器页面

访问 `/player?url=<视频链接>` 进入在线播放器页面：

- 独立的播放器页面，支持直接分享
- 支持输入任意 M3U8 视频链接播放
- 自动调用解析接口进行去广告处理
- 支持播放/暂停、音量调节、进度控制
- 支持截图、全屏播放
- 响应式设计，适配移动端

### 9. 定时任务自动学习

- 支持宝塔面板 / Cron 定时任务
- 支持 URL 访问触发（适合虚拟主机）
- 支持命令行执行
- 自动判断学习间隔，避免重复执行
- 执行日志记录，方便排查问题
- 访问密钥保护，防止恶意调用

### 10. 嗅探设置

后台 → 接口工具 → 嗅探设置，管控超级嗅探模块（`xt/`）走哪条解析通道：

- 支持放置「官解解析」和「官替接口」两个接口
- 每个接口独立开关：启用 / 禁用
- 每个接口可配置：接口名称、接口地址、接口类型（redirect/json/text）、URL 字段名
- 通过「当前通道」单选决定实际走官解还是官替
- 当前通道失败时自动 fallback 到另一条已启用的通道
- 官替接口地址留空时自动使用本项目官替接口 `mx.php?action=official_replace/info&url=`
- 内置测试入口，可直接在页面里输入视频链接验证当前嗅探设置
- 配置文件：`xt/sniffer_config.php`（由后台自动维护，无需手动编辑）
- API 端点：`GET/POST /mx.php?action=sniffer/config[/save]`

### 8. 系统更新

- 在线检查更新
- 一键系统更新
- 缓存清理功能
- PHP 缓存自动清理

## Web API 使用

### 接口地址

```
http://你的域名/mx.php?action=<action>&...
```

### 接口列表

#### 1. 视频分析接口

**GET** `/mx.php?action=analyze&url=<m3u8地址>`

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | 是 | M3U8 播放列表地址 |
| `auto_learn` | bool | 否 | 是否自动学习，默认 true |

**响应示例（快速模式）：**
```json
{
  "success": true,
  "fastMode": true,
  "hasDomainRules": true,
  "learn_count": 5,
  "message": "检测到已有域名规则，使用规则快速去广告",
  "domain": "v.example.com",
  "stats": {
    "totalSegments": 695,
    "adSegments": 482,
    "keptSegments": 213,
    "adPercentage": 68.73
  }
}
```

#### 2. 去广告接口（M3U8输出）

**GET** `/mx.php?action=mxjx&url=<m3u8地址>`

直接返回去广告后的 M3U8 播放列表，可用于播放器直接播放。

#### 3. 去广告信息接口（JSON输出）

**GET** `/mx.php?action=mxjx/info&url=<m3u8地址>`

返回去广告结果的 JSON 格式信息。

#### 4. 规则列表

**GET** `/mx.php?action=rules/list`

#### 5. 获取单条规则

**GET** `/mx.php?action=rules/get&domain=<域名>`

#### 6. 保存规则

**POST** `/mx.php?action=rules/save`

#### 7. 自动生成规则

**GET** `/mx.php?action=rules/generate&url=<视频地址>`

#### 8. 规则学习

**GET** `/mx.php?action=rules/learn&url=<视频地址>`

#### 9. 规则导出

**GET** `/mx.php?action=rules/export[&domain=<域名>][&download=1]`

#### 10. 规则导入

**POST** `/mx.php?action=rules/import`

#### 11. 资源站列表

**GET** `/mx.php?action=sites/list[&include_paused=1]`

#### 12. 获取单个资源站

**GET** `/mx.php?action=sites/get&name=<资源站名称>`

#### 13. 添加资源站

**POST** `/mx.php?action=sites/add`

#### 14. 更新资源站

**POST** `/mx.php?action=sites/update`

#### 15. 删除资源站

**POST** `/mx.php?action=sites/delete`

#### 16. 获取资源站视频列表

**GET** `/mx.php?action=sites/fetch_videos&name=<资源站名称>[&page=1&limit=20]`

#### 17. 获取自动学习配置

**GET** `/mx.php?action=sites/auto_learn/config`

#### 18. 保存自动学习配置

**POST** `/mx.php?action=sites/auto_learn/config/save`

#### 19. 执行自动学习

**POST** `/mx.php?action=sites/auto_learn/run`

### AI 自动学习接口（频繁更新规则专用）

每隔几小时自动从指定资源站（默认如意）获取热门/更新视频，提取 rym3u8 地址进行深度广告分析。

#### 20. AI 自动学习配置

**GET** `/mx.php?action=ai_autolearn/config`

#### 21. 保存 AI 自动学习配置

**POST** `/mx.php?action=ai_autolearn/config/save`

#### 22. AI 自动学习状态

**GET** `/mx.php?action=ai_autolearn/status`

#### 23. 执行 AI 自动学习

**POST** `/mx.php?action=ai_autolearn/run`

#### 24. AI 自动学习日志

**GET** `/mx.php?action=ai_autolearn/logs`

### 定时任务

| 脚本 | 说明 | 推荐频率 |
|------|------|---------|
| `cron_autolearn.php` | 原自动学习（按天，全站） | 每天1次 |
| `cron_ai_autolearn.php` | AI自动学习（按小时，指定站） | 每4小时 |

```bash
# Crontab 配置示例
0 3 * * * php /path/to/cron_autolearn.php          # 每天3点执行原自动学习
0 0,4,8,12,16,20 * * * php /path/to/cron_ai_autolearn.php  # 每4小时执行AI自动学习
```

## 动态规则更新接口（gzgx.php）

访问 `/gz/gzgx.php` 进行动态规则管理：

| 接口 | 说明 |
|------|------|
| `?action=info` | 获取规则概览信息 |
| `?action=get&domain=xxx` | 获取指定域名规则 |
| `?action=update&domain=xxx` | 更新指定域名规则（POST） |
| `?action=learn&url=xxx` | 从视频链接学习更新规则 |
| `?action=export&domain=xxx` | 导出规则 |
| `?action=import` | 导入规则 |
| `?action=delete&domain=xxx` | 删除规则 |

## 广告检测规则

### 内置规则（v2.0 权重置信度系统）

所有规则均带权重值，累加达到阈值（默认50）判定为广告：

| 规则名称 | 说明 | 权重 | 默认状态 | 类别 |
|---------|------|------|---------|------|
| `short-duration` | 片段时长过短 | 30 | ✅ 启用 | duration |
| `long-duration` | 片段时长过长 | 30 | ❌ 禁用 | duration |
| `keyword-match` | 标题或文件名包含广告关键词 | 50 | ✅ 启用 | keyword |
| `filename-pattern` | 文件名匹配广告命名正则模式 | 60 | ✅ 启用 | pattern |
| `discontinuity` | 存在 EXT-X-DISCONTINUITY 标记 | 80 | 可配置 | marker |
| `repetitive-duration` | 重复出现相同时长的短片段 | 55 | 可配置 | pattern |
| `cue-marker` | CUE-OUT/CUE-IN 广告插播标记 | 95 | ✅ 启用 | marker |
| `scte35-marker` | SCTE-35 数字广告信令标记 | 95 | ✅ 启用 | marker |
| `ad-tag-marker` | EXT-X-AD 自定义广告标签 | 90 | ✅ 启用 | marker |
| `ad-cluster-boundary` | 广告簇边界（DISCONTINUITY + 时长突变） | 70 | ✅ 启用 | cluster |
| `pre-roll-position` | 前贴片广告位置检测 | 40 | ✅ 启用 | position |
| `post-roll-position` | 后贴片广告位置检测 | 40 | ✅ 启用 | position |
| `sequence-jump` | 序列号跳跃检测 | 90 | 可配置 | marker |

### 域名规则

按域名自定义规则，支持：

- **时长规则**：基于片段时长判断广告
- **DISCONTINUITY 规则**：基于不连续标记判断插播
- **序列号跳跃规则**：基于序列号跳跃判断广告段
- **文件名模式**：基于文件名正则匹配
- **插播模式**：学习并记录片头/片中/片尾插播规律
- **心理画像**：构建域名广告心理特征档案
- **置信度评分**：规则整体可信度评分

### 自动学习机制（v2.0 增强）

每次分析视频时自动学习优化规则：

- 统计广告片段时长分布，自动调整时长阈值
- 根据广告占比动态调整广告判定阈值
- 提取广告文件名前缀，生成文件名模式
- 学习插播模式（片头/片中/片尾插播规律）
- 统计广告类型分布（按位置、按检测方式）
- 构建心理画像（注意力抓取指数、插播频率、可观看性）
- 迭代优化置信度评分
- 累计统计广告标记出现次数
- 检测到标记时自动启用对应检测
- 记录学习次数和历史统计数据
- 最多保留最近 10 次学习历史

## 缓存机制

- M3U8 解析结果缓存 2 分钟
- 去广告结果缓存 2 分钟
- 支持 OPcache / APC 缓存
- 更新时自动清理所有缓存

## 环境要求

- PHP 7.0 或更高版本
- 启用 cURL 扩展（用于远程 URL 请求）
- 启用 mbstring 扩展（用于多字节字符串处理）
- 启用 json 扩展
- cache/ 目录有写入权限

## 注意事项

1. **广告检测准确率** - 广告检测基于规则匹配，可能存在误判或漏判。建议根据实际使用场景调整规则参数。
2. **加密流** - 当前版本不处理 DRM 加密的流。
3. **主播放列表** - 自动追踪 Master Playlist 到实际媒体播放列表。
4. **网络请求** - 处理远程 URL 时需要网络连接，支持 HTTP 和 HTTPS。
5. **规则学习** - 自动学习功能会根据分析结果优化规则，建议定期检查规则准确性。

## 许可证

MIT License

## 版本历史

### v5.7.9 (2026-07-22)

- 🔧 优化 jiexi.php 解析接口返回格式
- 📡 msg 字段返回播放地址 URL（与 url 一致）
- ⏱️ 新增 time 字段：解析耗时（秒）
- 👤 新增 KFZ 字段：开发者信息
- 📋 新增 ZT 字段：状态描述

### v5.7.8 (2026-07-22)

- 🐛 修复推荐采集视频点击不显示播放地址问题
- 📡 新增 `official_sites/detail` API：通过 vod_id 获取视频详情播放地址
- 🔍 点击视频卡片时调用详情接口获取真实播放地址（资源站列表接口隐藏了链接）
- 📋 集数列表支持复制播放地址、学习广告规则、返回视频列表

### v5.7.7 (2026-07-22)

- 🤖 新增 `ai/sniff.php` 公用 API：从任意网页播放器获取播放地址链接（虾米解析）
- 🔐 支持 AES-256-CBC 签名加密 + ZeroPadding 解密，兼容 CryptoJS
- 🌐 双 API 节点 fallback（cache.0567890.xyz / cache.hls.one），自动重试
- 🧠 优化 AI 匹配算法，配置驱动的标题标准化 + 多维度评分重构
- 📚 新增 `gz/synonym_config.php`：集中管理 400+ 同义词映射（季/部/番/卷/集/画质/语言/版本/地区/符号）
- ♻️ 重构 `gz/TitleNormalizer.php`：单遍最长匹配正则替换 + md5 缓存，消除链式副作用
- 🎯 重写 `gz/AiVideoMatcher.php` 评分算法：季集解析委托、噪声排除、LCS 相似度重写、权重再平衡
- 🔁 跨源一致性：`庆余年第二季 1080P 国语版` / `庆余年第2季1080P` / `庆余年S02 1080P 高清` 标准化结果完全一致
- 🚫 噪声候选排除：电影解说/预告片/片花/花絮/混剪/MV/OST 等 19 种模式 50 分惩罚
- 📊 季数不一致惩罚按 diff×8 线性递增，封顶 `season_mismatch`，避免弱惩罚误匹配
- ✅ 通过 4 个综合功能测试场景（跨源匹配/集数匹配/噪声排除/空候选）

### v5.7.6 (2026-07-19)

- 🐛 修复 jiexi.php 解析返回的 clean.php URL 路径错误导致不能播放
- 🔧 `saveCleanM3u8()` 改用 `__DIR__` 推断 clean.php 的 URL 路径
- 🐛 旧逻辑用 `dirname(SCRIPT_NAME)` 推断路径，jiexi.php 在根目录时生成 `/clean.php`（404）
- ✅ 修复后生成正确的 `/xt/clean.php?id=xxx`，无论从根目录还是 xt/ 调用都正确

### v5.7.5 (2026-07-19)

- 🚀 修复 jiexi.php 不能同时调用官解和官替的问题
- ⚡ 新增 `getVideoLinkByConcurrentRace()`：curl_multi 并发调用官解+官替，最快成功的立即返回
- 🧵 多线程高并发：合并所有已启用接口到同一并发池，真正同时请求
- 🎯 自动识别通道：通过 `_channel` 标记区分 official/replace，后续按通道分流处理
- 🔧 新增配置项 `performance.concurrent_race_enabled`（默认 `true`）
- 💡 并发模式下强制启用官替，即使后台开关关闭也会自动用本地官替接口
- ✅ 向后兼容：关闭开关即回到旧的串行 fallback 逻辑

### v5.7.4 (2026-07-19)

- 🎬 优化 clean.php 播放器页面，移除多余 UI 元素
- 🧹 移除顶部标题栏（"M3U8 无广告播放器" 标题和浏览器标签）
- 🧹 移除底部控制按钮（播放、暂停、复制链接按钮）
- 🧹 移除底部信息栏（缓存ID、去广告提示）
- 📺 视频全屏显示，仅保留浏览器原生控制条
- ✅ 不影响 TVbox 等软件播放器的 m3u8 内容返回

### v5.7.3 (2026-07-19)

- 🔧 优化更新备份功能，备份文件名增加版本号：`backup_v{version}_{timestamp}.zip`
- 📦 备份文件内添加 `.backup_info.json` 版本信息文件
- 📋 getBackupList() 返回版本号、commit 等详细信息
- ⚡ 兼容旧格式备份文件

### v5.7.2 (2026-07-19)

- 🐛 修复 xt 文件夹中 clean.php 不能播放的问题
- 🔧 重写浏览器检测逻辑，避免 HLS.js 等播放器请求被误判为浏览器
- ⚡ 优化判断顺序：优先检查 Accept 头，再检查 User-Agent
- 🎯 新增 `player=1` 参数显式控制播放器页面显示
- 📦 排除 Range 请求和播放器 UA 的误判

### v5.7.1 (2026-07-18)

- 🐛 修复所有用到代理的地方代理无法使用的问题
- 🔧 为所有使用代理的类添加 setProxyManager() 方法，支持依赖注入
- ⚡ 默认首次请求就使用代理（之前只在重试时使用）
- 📊 统一 DbProxyManager 排序逻辑，按响应时间从快到慢优先
- 🔗 mx.php 中统一注入代理管理器，确保配置一致

### v5.7.0 (2026-07-18)

- 🐛 修复顶部统一接口不显示接口URL：全局 select { width:100% } 导致下拉框占满整行，添加 width:auto 覆盖

### v5.6.9 (2026-07-18)

- 🐛 修复顶部统一接口不显示接口信息：文字颜色 + ellipsis + 路径计算

### v5.6.8 (2026-07-18)

- 🐛 修复顶部接口URL不显示：路径计算兼容任意入口文件名
- 🗑️ 移除顶部右侧管理后台卡片，布局更简洁

### v5.6.7 (2026-07-18)

- 🐛 修复顶部统一接口区域右侧内容缺失
- 恢复左右双栏布局：左侧 V2 统一接口 + 右侧管理后台预览
- 公告移到底部占满宽度，修复 URL 溢出竖排问题

### v5.6.6 (2026-07-18)

- 🎨 数据概览页面 UI 优化：统计卡片 3 列左右布局，快捷操作 3 列
- 📱 完整响应式适配：桌面/平板/手机各尺寸优化

### v5.6.5 (2026-07-18)

- 🚀 代理列表按速度排序：响应时间越快越靠前
- 🚫 不显示失败的代理：后台和API只返回 active 状态
- ⚡ getProxy() 优化：优先选择最快的代理

### v5.6.4 (2026-07-18)

- 🐛 修复代理池不能正常使用的问题（8个根因）
- 修复 addProxy 无去重、批量添加性能、代理池未自动启用
- 修复测试URL不稳定：httpbin.org → 百度
- 修复 checkAllProxies 串行 → 并发验证（速度提升10倍）
- 修复 proxy.scdn.io 解析：兼容5种返回格式
- 更新代理源：新增 Geonode、monosans、clarketm 等

### v5.6.3 (2026-07-18)

- 🔖 版本号升级到 v5.6.3（xt 模块 5.6.3）
- 🚀 代理池并发获取：ProxyFetcher 使用 curl_multi 并发请求所有代理源（proxy.scdn.io 等12个源）
- ⚡ 并发验证代理：验证速度提升 10 倍（10个一批并发）
- 💾 代理缓存机制：2分钟本地缓存，避免频繁请求 proxy.scdn.io
- 🐛 修复官替接口 URL 为空时默认使用本地官替接口
- 🐛 修复解析成功但不能播放的问题：优化官替通道 m3u8 处理逻辑
- 🔥 多接口并发竞速：curl_multi 并发请求多个官解接口，最快成功的立即返回
- 🧠 AI 学习自动排序：记录成功率/平均耗时，动态调整调用优先级
- 🔄 失败自动切换：一个接口被禁/失败，自动切换到下一个

### v5.6.0 (2026-07-18)

- 🔖 版本号升级到 v5.6.0（xt 模块 5.2.0）
- 🧠 核心逻辑补充：明确官解走虾米接口，官替走 AI 去广告/去插播/去水印
- ✨ 重构 `parseVideo()` 为两个独立函数：
  - `parseVideoByOfficialChannel()` - 官解通道：虾米接口 + xt 去广告
  - `parseVideoByReplaceChannel()` - 官替通道：资源站匹配 + AI 去广告/去插播/去水印
- ✨ AdFilter 新增插播检测（>60s 超长片段）和水印检测（URL 含 watermark/logo/burn/overlay）
- ✨ AI 提示词增强：从只识别广告扩展为识别广告/插播/水印三类异常
- ✨ `xt/config.php` 新增 `insertion_check_enabled` / `watermark_check_enabled` / `watermark_keywords` 配置项

### v5.5.9 (2026-07-18)

- 🔖 版本号升级到 v5.5.9
- 🐛 官替通道返回直连播放地址（不生成 clean.php 代理）
- 🐛 修复 `getVideoLinkFromApiEntry()` 字段优先级：`ad_skip_url` → `m3u8_url`

### v5.5.8 (2026-07-18)

- 🔖 版本号升级到 v5.5.8
- 🐛 修复走官替接口时播放地址不可播放的问题
- 🐛 `getVideoLinkBySnifferMode()` 返回值改为结构化数组，标识实际命中通道

### v5.5.7 (2026-07-18)

- 🔖 版本号升级到 v5.5.7
- 🔍 后台新增「嗅探设置」页面：管控超级嗅探模块走官解解析还是官替接口
- ✨ 支持放置官解/官替两个接口，各配独立开关 + 接口地址/类型/字段名
- ✨ 通过「当前通道」单选决定走哪条通道，当前通道失败自动 fallback 到另一通道
- ✨ 新增配置文件 `xt/sniffer_config.php`（后台自动维护）
- ✨ 新增 API 端点 `sniffer/config` 和 `sniffer/config/save`
- 🐛 `xt/server.php` 抽取通用接口调用函数，向后兼容旧 `official_apis` 配置

### v5.5.5 (2026-07-17)

- 🔖 版本号升级到 v5.5.5
- ✨ 浏览器适配功能：`xt/clean.php` 支持 Edge、Chrome、Firefox、Safari 等主流浏览器直接访问
- ✨ 新增 `jiexi.php` — TVBox / 影视App专用解析接口
- ✨ 超级嗅探模块 `xt/`：官解接口对接、规则引擎 + AI 大模型双重广告识别
- 🐛 修复去广告 m3u8 无法播放问题（ts 相对路径转绝对路径）

### v2.29.1 (2026-07-17)

- ✨ 新增浏览器适配功能：`xt/clean.php` 支持 Edge、Chrome、Firefox、Safari 等主流浏览器直接访问
- ✨ 浏览器访问时自动显示 HTML 播放器页面，支持在线播放 m3u8 视频
- ✨ 保留原有 m3u8 直链模式，播放器调用不受影响
- ✨ 新增浏览器检测和标识显示功能

### v2.29.0 (2026-07-16)

- ✨ 新增 `jiexi.php` — TVBox / 影视App专用解析接口
  - 支持 TVBox、影视仓、喵影视、影迷大院等主流影视App
  - 兼容 8 种参数名：url/wd/v/video/t/u/play/src
  - 支持 5 种返回格式：JSON / 影视CMS标准(code=1) / 302跳转 / XML / JSONP
  - CORS 跨域支持，开箱即用
- ✨ 新增超级嗅探模块 `xt/`（v5.1.4）
  - 官解接口对接，支持 redirect/json/text 三种类型
  - 规则引擎 + AI 大模型双重广告识别
  - 解析结果缓存，重复请求毫秒级响应
  - m3u8 相对路径自动转绝对路径，直接播放
  - 多级 m3u8 最高码率优选
  - 缓存自动清理（过期 + 文件数上限双重保护）
  - clean.php 支持 ETag / 304 协商缓存 + CORS 预检
- 🐛 修复去广告 m3u8 无法播放问题（ts 相对路径转绝对路径）

### v2.28.2 (2026-07-07)

- 🐛 修复内存耗尽问题：规则文件过大导致 `Allowed memory size exhausted`
- ✅ 新增大字段过滤机制，规则文件缩小 99.8%（2.6MB → 4KB）
- ✅ 规则懒加载优化，初始内存占用减少 95%+
- ✅ 内存限制提升至 512M，增加异常降级处理
- ✅ 19 项接口测试全通过，3 轮 10 并发压力测试全通过

### v2.28.1 (2026-07-07)

- 🚀 修复 moxi 接口 HTTP 状态码不一致问题
- ✅ 优化文件缓存并发安全性（临时文件 + rename 原子写入）
- ✅ 优化缓存目录创建并发安全
- ✅ 优化缓存读取容错
- ✅ 20 项接口测试全通过，并发压力测试全通过

### v2.28.0 (2026-07-07)

- 🚀 新增数据库自动迁移机制
- ✅ 自动检测并创建缺失的 12 张核心表
- ✅ 自动检测并添加缺失的列，支持 MySQL 和 SQLite
- ✅ 修复 schema_mysql.sql 缺失字段
- ✅ 修复 schema_sqlite.sql 缺失 4 张表
- ✅ 修复 splitSqlStatements() 括号深度追踪 bug
- ✅ 70 个数据库层单元测试通过率 98.6%

### v2.27.0 (2026-07-07)

- 🔧 修复 analyze 接口报错：durationDistribution 遍历逻辑错误
- ✅ 后台添加多个接口测试按钮
  - 测试解析（moxi）
  - 测试去广告（mxjx/info）
  - 测试分析（analyze）
  - 测试官替（official_replace/info）
- ✅ 庆余年第1季 M3U8 测试通过（移除40个广告片段）

### v2.26.0 (2026-07-07)

- ✅ 补充缺失的API接口
- ✅ 新增系统信息接口（info、version）
- ✅ 新增官替接口（official/list、official/platforms）
- ✅ 新增代理列表接口（proxies/list）
- 🔧 修复版本读取逻辑，兼容数组格式
- ✅ 所有API接口测试通过（11个数据库类测试 + 11个API接口测试）

### v2.25.0 (2026-07-07)

- ✅ 全面数据库化改造
- ✅ 新增分析缓存、广告特征码、官替缓存数据库表
- ✅ 在线播放从数据库加载广告规则
- ✅ 官替多线程抓取和搜索
- 🔧 修复Database PDO参数绑定错误

### v1.9.0 (2026-06-29)

- ✨ 新增资源站管理系统（50+内置资源站）
- ✨ 新增自动学习更新规则功能
- ✨ 新增资源站采集接口支持（MacCMS）
- ✨ 资源站管理前端页面
- ✨ 自动学习配置面板
- ✨ 资源站视频列表在线获取
- ✨ 视频链接一键复制和分析
- ⚡ 优化规则学习流程

### v1.8.0 (2026-06-29)

- ✨ 新增自动学习机制，每次分析自动优化规则
- ✨ 新增规则导入导出功能（JSON格式）
- ✨ 新增动态规则更新接口 gzgx.php
- ✨ 新增域名规则学习次数统计
- ✨ 视频分析页面新增学习状态显示
- ✨ 规则管理页面新增导入导出按钮
- ⚡ 优化快速模式，已有规则时直接去广告
- ⚡ 优化广告规则匹配算法

### v1.7.1 (2026-06-28)

- 🔧 修复播放地址404问题
- 🔧 优化接口地址展示，支持一键复制
- 🎨 改进后台界面用户体验

### v1.7.0 (2026-06-28)

- ⚡ 全面优化解析和播放速度
- 📦 新增缓存管理（CacheManager）
- 🔧 优化 CURL 请求参数
- 🚀 优化广告规则匹配算法（O(N²)→O(N)）

### v1.6.0 (2026-06-27)

- 🔧 修复播放黑屏问题
- ✨ 完善接口功能
- 📊 新增详细统计信息

### v1.5.0 (2026-06-27)

- ✨ 域名规则管理功能
- ✨ 后台管理界面
- ✨ 内置播放器

### v1.1.0 (2026-06-27)

- 移植到 PHP 版本
- 完整的 Web API 支持

### v1.0.0 (2026-06-27)

- 初始版本发布（Node.js）
- 实现 M3U8 解析器
- 实现多规则广告检测引擎
- 实现智能广告聚类过滤

## 许可证

本项目采用 [MIT License](LICENSE) 开源许可证。

- [English Version](LICENSE)（法律有效版本）
- [中文翻译版](LICENSE.zh-CN.md)（仅供参考）

你可以自由地使用、复制、修改、合并、发布、分发、再许可和/或出售本软件的副本，但需在所有副本或重要部分中包含上述版权声明和本许可声明。
