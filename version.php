<?php
return array (
  'version' => 'v5.15.11',
  'branch' => 'main',
  'build' => '20260908-v5-15-11-502-fold-results',
  'version_code' => 51511,
  'commit' => 'v5.15.11-release',
  'updated_at' => '2026-09-08',
  'changelog' =>
  array (
    'v5.15.11' =>
    array (
      'date' => '2026-09-08',
      'title' => '【学习 502 修复 + 后台全结果折叠】HTTP 异步执行立即返回避免 nginx 502，后台所有结果区域可折叠/滚动查看',
      'changes' =>
      array (
        0 => '【修复-学习 502】cron_ai_autolearn.php HTTP 模式在学习/清理前调用 aiHttpDetach()：PHP-FPM 用 fastcgi_finish_request()、其他环境用 Content-Length+Connection:close 立即返回 200 并断开连接，任务继续后台执行——避免遍历全部资源站+深度解析超过 nginx/PHP-FPM 超时被掐断返回「502 Bad Gateway / 服务器返回非JSON响应」',
        1 => '【新增-结果折叠】后台 mxadmin.php 新增通用折叠组件 makeFold()/initResultFolds()，覆盖 22 个结果容器（视频分析/批量解析/资源站搜索/自动学习/AI自动学习+日志/官替测试/嗅探测试/沫兮测试/数据库迁移/完整性/缓存清理/在线更新/AI去广告/专业检测/插播/字幕/水印/接口选择）：点击标题栏折叠/展开、展开内容区限高 420px + 自定义滚动条拖动查看、双击标题不限高看全貌、折叠状态 localStorage 持久化；纯前端渐进增强，不改原渲染逻辑',
        3 => '【验证】HTTP 模式实测仅 0ms 即返回 {"success":true,"async":true} 不再 502；php -l cron_ai_autolearn.php / mxadmin.php / 主脚本块 node --check 均通过；浏览器实测 analyze/batch/ai_autolearn/api_picker/ai_skip 5 页折叠卡片渲染与折叠/展开交互全部正常、控制台 0 错误',
      ),
    ),
    'v5.15.10' =>
    array (
      'date' => '2026-09-08',
      'title' => '【AI自动学习长时间不动修复】懒触发 exec 禁用时不再静默失败，定时脚本 DB 模式适配，触发失败可自动重试',
      'changes' =>
      array (
        0 => '【根因】AI 自动学习的懒触发 autoTriggerIfNeeded() 内部只调用 exec 后台执行 cron_ai_autolearn.php，服务器禁用 exec() 时 @exec 静默失败且无回退 → last_run_time 永远不更新，配置页显示「长时间不动」',
        1 => '【修复-懒触发】autoTriggerIfNeeded() 改用带三级回退的 triggerBackgroundRunAsync()（exec → fsockopen 非阻塞 HTTP → curl 短超时），exec 禁用环境也能真正触发；失效规则清理触发同步增加 exec 禁用回退',
        2 => '【修复-可重试】mx.php ai_autolearn/run 不再预先更新 last_run_time（改为 cron 脚本实际执行 run() 成功后更新），触发失败不会把 last_run_time 顶到未来导致数小时不再重试',
        3 => '【修复-定时脚本DB适配】cron_ai_autolearn.php 检测到 db_config.php 时使用 DbResourceSiteManager + DbDomainRuleManager（与 gx.php 一致），DB 模式下定时学习正确写入 domain_rules 表',
        4 => '【验证】exec 禁用（php -d disable_functions=exec）下 autoTriggerIfNeeded 返回 triggered=true（走回退通道）不再静默失败；php -l gz/AiAutoLearner.php / cron_ai_autolearn.php / mx.php 全部通过',
      ),
    ),
    'v5.15.9' =>
    array (
      'date' => '2026-09-08',
      'title' => '【去广告监控数据异常修复】监控数据文件损坏自愈 + 原子写防并发写坏，接口不再报「获取监控数据异常」',
      'changes' =>
      array (
        0 => '【根因】去广告监控数据落盘 gz/monitor_data.php 由多请求并发写且无锁：播放开启 mon=1 后并发 record() 同时写同一文件，产生截断/交错 → 文件变成不完整 PHP 数组 → 下次 @include 抛 ParseError（@ 无法抑制异常）→ monitor/list 等接口整体异常 → 后台提示「获取监控数据异常」',
        1 => '【修复-自愈】AdMonitor::load() 捕获 ParseError/非数组，自动将损坏文件改名备份（monitor_data.php.bak-时间戳）并重建默认监控数据写回，接口立即恢复，不再持续报错',
        2 => '【修复-防写坏】AdMonitor::save() 改为原子写：临时文件 + flock 排他锁 + fflush + rename 覆盖，多请求并发也不会再写坏数据文件；rename 失败时退化直接写入',
        3 => '【验证】损坏文件场景实测：备份生成 + 数据重建 + record 正常；真实路径写读往返正常（版本 5.15.8.0001）；monitor/list、monitor/status 接口均返回正常 JSON；php -l gz/AdMonitor.php 通过',
      ),
    ),
    'v5.15.8' =>
    array (
      'date' => '2026-09-08',
      'title' => '【AI自动学习优化 + 数据库自动保存 + 去广告监控修复】默认全部资源站按速度排序学习，规则自动入库启用定时任务，播放器去广告监控 mon=1 生效',
      'changes' =>
      array (
        0 => '【AI自动学习-默认全部资源站】AiAutoLearner 的 max_sites_per_run 默认改为 0（不限制站点数），target_mode 默认 all，一次学习自动覆盖全部启用（未暂停）资源站，不再只测前 3 个',
        1 => '【AI自动学习-按速度/健康排序】新增 sort_by_speed 配置（默认 true）：学习前按资源站响应速度排序，快的优先学；复用 24 小时新鲜测速缓存，无缓存时实时测速并写回 response_time/last_check，健康站点优先、失败/超时排最后兜底',
        2 => '【自动保存规则-数据库】新增 auto_save_rules 配置（默认 true）：DB 模式学习结果自动写入 domain_rules 表、文件模式写 rules_*.php；关闭时仅分析不落库',
        3 => '【定时任务-DB模式适配】gx.php 新增 buildAiLearner()：检测到 db_config 时使用 DbResourceSiteManager + DbDomainRuleManager，task_ai_learn / task_ai_cleanup 规则正确入库；清理逻辑按 updated_at 折算 _filemtime 兼容 DB 模式',
        4 => '【去广告监控不生效修复】root cause：播放器入口生成的 mxjx 播放链接只带 ph=1 从不带 mon=1，实时去广告监控从未在真实播放中记录。player/index.php 播放链接补充 mon=1（普通 mxjx 入口 + 官替 fallback 深度入口），播放即触发 AdMonitor 记录与可疑删除识别',
        5 => '【资源站暂停不展示】AI 自动学习 resolveTargetSites 与 getAllSites(false) 只取启用站点，暂停资源站不参与学习与展示',
        6 => '【验证】php -l gx.php/gz/AiAutoLearner.php/gz/ai_auto_learn_config.php/player/index.php/db/DbResourceSiteManager.php/db/DbDomainRuleManager.php 全部通过；DB 管理器的 getSiteByName/updateSite/getAllRules/learnFromAnalysis/deleteRules 等与 AiAutoLearner 调用逐一对应',
      ),
    ),
    'v5.15.7' =>
    array (
      'date' => '2026-09-07',
      'title' => '【后台全功能体检修复】全量语法+接口+页面实测，修复 AI自动去广告「MD5特征码分析」入口缺失与 DOM id 引用失效',
      'changes' =>
      array (
        0 => '【后台体检】对 mxadmin 全部 25 个页面、mx.php 约166个 case 做全量校验：全部 PHP 文件 php -l 通过；后台调用的 action 与 mx.php 一一对应（无未实现接口）；启动本地服务实测只读/写回接口（列表/配置/保存）均正常',
        1 => '【修复-MD5特征码分析失效】AI自动去广告页 aiMd5Analyze() 引用了不存在于页面的 aiSkipSaveMd5 / aiSkipFastMode 两个 checkbox id（运行时会 getElementById(...) 为 null 报 TypeError），且该功能无触发按钮。修复：快捷操作卡新增「🔬 MD5特征码分析」按钮与「⚡ 极速MD5 / 保存MD5特征码入库」两个开关，MD5 分析功能恢复可用',
        2 => '【验证】浏览器实测 mxadmin 主要页面（概览/历史/批量/分析/规则/资源站/资源站规则/AI去广告/M3U8测试/接口选择/去广告监控）全部正常渲染、控制台 0 error；ai_skip 页 aiSkipFastMode/aiSkipSaveMd5 均可正常读取；player/config/save 经前端实际载荷验证正常',
      ),
    ),
    'v5.15.6' =>
    array (
      'date' => '2026-09-07',
      'title' => '【精简资源站·一键屏蔽不可搜索】全量检测所有启用资源站的搜索可用性，无法搜索或搜索返回不到结果的站点一键自动屏蔽，只保留可用站',
      'changes' =>
      array (
        0 => '【新增-搜索可用性检测】两个资源站管理器（文件/DB）新增 verifySearchCapability()：遍历全部启用资源站，用探测关键词逐个真实搜索，无法搜索（接口失败/连不上）或搜索返回不到任何结果的站点自动置为暂停（屏蔽）并记录原因，退出活跃列表，只保留可用资源站',
        1 => '【接口】mx.php 新增 sites/search_check 接口：支持 keyword（探测词，默认高频词「爱情」）与 max（限制检测数量）参数，返回 checked/usable/blocked/blocked_sites 与逐站明细；全量检测后返回统计并自动刷新列表',
        2 => '【后台按钮】资源站列表页工具栏新增「🚫 检测并屏蔽不可搜索」按钮：确认后逐个探测全部启用资源站，弹窗提示可用数/被屏蔽数，被屏蔽站点在列表显示暂停并可在「显示已暂停」中查看/恢复',
        3 => '【验证】php -l mx.php / mxadmin.php / gz/ResourceSiteManager.php / db/DbResourceSiteManager.php 全部通过；verifySearchCapability 方法在文件与 DB 管理器均存在；站点写入时同时更新 status 与屏蔽原因备注',
      ),
    ),
    'v5.15.5' =>
    array (
      'date' => '2026-09-07',
      'title' => '【资源站优先级统一100 + 自动屏蔽不可搜索】资源站列表全部优先级统一为 100（默认 100，越小越优先按优先级排序）；搜索时自动屏蔽不能搜索的资源站',
      'changes' =>
      array (
        0 => '【优先级统一100】资源站列表全部站点 priority 统一改为 100；新增/编辑默认值 100（addSite、后台表单、JS 提交/回填默认）+ 排序兜底 99→100（文件与 DB 两个管理器）；后台列表与搜索均按 priority 升序自动排序（数字越小越优先），支持手动调低某站优先级让其靠前',
        1 => '【自动屏蔽不可搜索】searchAllSites 搜索时若某资源站搜索失败，自动将该站点置为暂停（屏蔽），备注记录「自动屏蔽·不可搜索: 原因」，退出活跃列表不再参与搜索；返回结果新增 auto_blocked 计数与 blocked_sites 列表',
        2 => '【验证】sites_config.php 122 站全部 priority=100（唯一值）；getAllSites 98 活跃按优先级升序；php -l gz/ResourceSiteManager.php / db/DbResourceSiteManager.php / gz/sites_config.php / mxadmin.php 全部通过',
      ),
    ),
    'v5.15.4' =>
    array (
      'date' => '2026-09-07',
      'title' => '【修复-去广告整片误删黑屏】统一时长占比过高视为内容节奏不再误判为广告，修复影片被整片占位替换导致"没画面"',
      'changes' =>
      array (
        0 => '【修复-整片误删黑屏】根因：repetitive-duration 规则把影片「统一时长（约 4s 一档）的正常码率切片」全量误判为广告（权重 55≥阈值 50 单独命中即删），广告占比高达 58.5%，正片被整体替换为黑屏占位 TS，导致 mxjx 输出"进度条在走但没画面"。修复：src/AdRuleEngine.php 的 repetitive-duration 增加内容节奏保护——某时长桶占比 ≥35% 即为视频自身的统一切片刻度（内容），直接放行，不再判为广告。实测广告占比 58.5% → 10.2%，仅保留 DISCONTINUITY 边界与真正超短视频，正片完整保留',
        1 => '【验证】对 v.lzcdn31.com 源（2217 段、37.8% 为 4.0s 统一切片）回归：修复前 AD 58.5%、修复后 10.2%；php -l src/AdRuleEngine.php 通过',
      ),
    ),
    'v5.15.3' =>
    array (
      'date' => '2026-09-07',
      'title' => '【资源站规则自动获取 + 外置播放修复 + 接口选择器】输入资源站链接自动抓取分析配置规则，每 2 小时自动同步；加密/fMP4 流保留 EXT-X-KEY/MAP 修复外置播放无画面；侧边栏新增接口选择（独立/组合调用）',
      'changes' =>
      array (
        0 => '【新增-规则自动获取】新建 gz/ResourceRuleAutoFetcher.php：输入资源站采集链接（如 https://www.example.com/api.php/provide/vod/），自动拉取视频列表→逐个解析 M3U8 深度分析广告特征→自动汇总 5 类规则（时长/不连续/序列/文件名/关键词）写入 resource_site_rules（跨视频命中≥2 次才写入，同站同类型去重）；后台「资源站规则」页新增「自动获取」卡片（链接+站名+分析数+结果明细）与「一键同步全部资源站」',
        2 => '【新增-每2小时自动同步】resource_rules/sync 支持一键同步全部启用资源站；后台打开规则页时检测距上次同步≥2 小时自动后台异步触发；新增 sync/status 与 sync/config/save（开关+间隔可调，默认 2 小时）；gx.php 新增 resource_rules_sync 任务（可单跑/并入 all），自动维护页新增「⚡ 一键更新维护规则」按钮',
        3 => '【修复-外置播放无画面】M3U8Parser 新增 #EXT-X-KEY（METHOD/URI/IV/KEYFORMAT）与 #EXT-X-MAP 解析，逐片段记录密钥/init segment 状态；OutputGenerator 输出时按密钥轮换重发 EXT-X-KEY、fMP4 流输出 EXT-X-MAP、占位黑屏 TS 显式 METHOD=NONE（未加密不再被旧 key 解密导致黑屏）。修复加密流/CMAF 流经去广告后外置播放器「进度条动但不显示画面」',
        4 => '【新增-接口选择器】后台侧边栏「接口工具」新增「接口选择」页：全局开关 + 独立/组合调用模式 + 接口列表（自定义清洗接口默认 http://域名/api/clean/?url= 置顶，{host} 自动替换本站地址，http/https 均可；内置沫兮/官解/官替/去广告按需启停排序）+ 按当前配置测试调用；开启后 parse 入口按配置顺序逐个尝试，返回首个成功并标注 via 接口',
        5 => '【接口】新增 resource_rules/auto_fetch、resource_rules/sync、resource_rules/sync/status、resource_rules/sync/config/save、api_picker/config、api_picker/config/save、api_picker/run',
        6 => '【验证】php -l mx.php/mxadmin.php/gx.php/gz/ResourceRuleAutoFetcher.php/src/M3U8Parser.php/src/OutputGenerator.php 全部通过；KEY 轮换解析输出往返一致；自动规则写入去重（二次运行 0 新增）；接口选择器实测 single 模式经 mxjx 返回 play_url 与 via 标记',
      ),
    ),
    'v5.15.2' =>
    array (
      'date' => '2026-09-07',
      'title' => '【去广告实时监控防误删 + 占位模式 + 更新提示修复】监控版本 v5.15.2.0001；M3U8 去广告改用等时长黑屏占位不删段，解决卡顿跳画面；误删反馈进保护名单自动还原；版本更新提示同版本只弹一次',
      'changes' =>
      array (
        0 => '【新增-实时监控】新增 gz/AdMonitor.php 去广告实时监控：记录每次去广告处理（总段/删除/占比/守护/命中规则/删除索引与URI快照），自动识别高危/可疑删除（高占比/全部删除/零散删除/守护触发）；新增 monitor/status、monitor/list、monitor/feedback、monitor/protected、monitor/protected/add、monitor/protected/remove、monitor/reset 7 个接口；后台新增「去广告监控」页面（统计概览+记录列表+误删反馈+规则误报+保护名单管理）',
        1 => '【新增-监控版本】监控版本 = 应用版本 + 三位数字计数（v5.15.2.0001），应用升级自动跟随前缀；后台「重置监控数据」递增计数（0001→0002…），保护名单与记录同步清空',
        2 => '【新增-占位模式】mxjx 新增 ph=1 占位模式、parse_test 过滤后输出默认占位：广告段不再删除，改为等时长黑屏静音占位 TS（复用 placeholder_ts），保持时间轴连续，解决删除广告后卡顿/跳画面；player 播放页与解析测试播放链接默认带 ph=1',
        3 => '【新增-防误删保护】parse_test / mxjx 解析自动加载保护名单（monitor 误删反馈的片段快照），命中受保护 URI 的片段自动还原保留不再删除；缓存键加入 ph/mon 参数，监控模式（mon=1）不命中旧缓存，确保每次真实记录',
        4 => '【修复-版本更新提示】后台更新弹窗「稍后再说」用 localStorage 记录已忽略版本，同一版本不再重复弹窗提示；新版本发布后自动恢复弹窗',
        5 => '【验证】php -l mx.php/mxadmin.php/src/M3U8AdSkipper.php/gz/AdMonitor.php/player/index.php 全部通过；AdMonitor 单元验证：高危识别、误删反馈入保护名单、规则误报计数、isProtected 命中、reset 版本递增 0001→0002 全部正常',
      ),
    ),
    'v5.15.1' =>
    array (
      'date' => '2026-09-07',
      'title' => '【公告正文携带 README 更新内容 + M3U8 对比折叠】公告自动读取 README.md 当前版本更新内容作为正文；解析测试原始/过滤后 M3U8 默认折叠可展开',
      'changes' =>
      array (
        0 => '【新增-公告】announcement/list 实时公告正文自动从 README.md「当前版本」章节提取更新内容（截取到第一个「上一版」小标题为止），content 字段携带完整更新说明，新增 readme_content 字段，日期优先取 README 标题中的日期',
        1 => '【优化-界面】M3U8 解析测试「原始 M3U8 / 过滤后 M3U8」改为默认折叠（避免大文本撑高页面影响美观），标题栏新增「展开/收起」按钮，展开后限高 360px 内滚动；每次重新解析自动重置为折叠态',
        2 => '【验证】php -l mx.php / mxadmin.php 通过；本地实测 announcement/list 返回完整 README 更新正文；内联 JS node --check 通过',
      ),
    ),
    'v5.15.0' =>
    array (
      'date' => '2026-09-07',
      'title' => '【去插播兜底线路 + M3U8测试播放修复】设置可配置多条兜底清洗线路（默认沫兮兜底 1），官方资源优先走 访问→搜索→跑兜底接口 链路；解析测试过滤后播放改用绝对地址 M3U8 文本并逐片段实时跟随',
      'changes' =>
      array (
        0 => '【新增-兜底线路】后台「沫兮API」页新增「去插播兜底线路」设置：可增删多条线路（名称+接口地址模板+启停开关+全局开关），默认内置「沫兮兜底 1 https://mxqcb.ssmhd.com/api/clean/?url=」，DB 模式存 sys_config(fallback_lines)、文件模式存 gz/fallback_config.php，新增 fallback/config 与 fallback/config/save 接口',
        1 => '【新增-解析链路】启用兜底后，输入官方资源（腾讯/爱奇艺/优酷/芒果/B站/搜狐/PP）自动走：①先访问官方页面通过 pt 平台适配器获取剧名+剧集（失败回退 URL 推断）→ ②资源站搜索匹配最佳片源 → ③用搜索到的链接调用兜底接口清洗 → ④返回清洗后播放地址；接口兼容 json 返回 url/play_url/m3u8_url 等字段，业务错误码（如 code=404 非本站资源）自动识别并优雅回退到原有官替/官解链路；已接入 moxi / parse(统一入口) / official_replace resolve+info 四条入口',
        3 => '【修复-解析测试播放】M3U8 解析测试页「播放过滤后」不再用片段手拼 M3U8（相对地址在 Blob 场景无法解析导致报错黑屏），改用后端已生成的绝对地址 filtered_m3u8 文本播放，保留 EXT-X-KEY/EXT-X-MAP 等全部标签，加密/地图片段也能正常播放',
        4 => '【修复-实时跟随】片段跟随播放改为双通道：hls.js FRAG_CHANGED 逐片段精确高亮（比 timeupdate 更准）+ timeupdate 平滑补充；修复重复绑定监听器导致 timeupdate 多次触发的问题',
        5 => '【验证】php -l mx.php/mxadmin.php 全部通过；本地实测 fallback/config 读写、官方资源兜底链路超时优雅回退官替、parse_test 返回绝对地址与完整过滤文本、JS 内联脚本 node --check 通过',
      ),
    ),
    'v5.14.9' =>
    array (
      'date' => '2026-09-07',
      'title' => '【沫兮去广告链接不能播放修复 + 官替优化】mxjx 输出 m3u8 不再被 JSON 守卫改写并显式加跨域；官替相对地址自动补全绝对化',
      'changes' =>
      array (
        0 => '【修复-播放】mx.php mxjx 接口输出去广告 M3U8 时，彻底清除最外层 JSON_OUTPUT_GUARD 的 ob 包裹层（此前 ob_clean 只清最内层，外层把 #EXTM3U 文本当 JSON 改写导致去广告后的无广告链接无法播放），缓存命中路径同步修复',
        1 => '【新增-跨域】mxjx 输出 M3U8 时显式输出 Access-Control-Allow-Origin: * / Methods / Headers，解决播放器页面与接口跨域（不同端口/子域/域名）时浏览器拦截 m3u8 与后续 TS 请求',
        2 => '【确认-无硬编码】沫兮/官替生成的 mxjx 链接 selfUrl 全部基于 $_SERVER 动态推导（协议+Host+目录），url 参数使用传入的真实地址 urlencode，无任何硬编码域名/IP',
        3 => '【优化-官替】DbOfficialReplaceManager / OfficialReplaceManager 对资源站返回的相对播放地址（如 /2026/xx/index.m3u8）自动基于视频页域名补全为绝对地址，m3u8_url 与剧集列表 all_urls 同步处理，避免播放器拿到相对地址黑屏',
        4 => '【验证】php -l 全部通过；本地实测 mxjx：Content-Type=application/vnd.apple.mpegurl、Access-Control-Allow-Origin=*、BODY 为真实 #EXTM3U 内容、TS 重写为绝对地址，缓存 HIT 路径同样正常',
      ),
    ),
    'v5.14.8' =>
    array (
      'date' => '2026-09-07',
      'title' => '【M3U8测试播放修复 + 片段列表布局 + 侧边栏折叠】解析测试视频可播放，片段列表显示地址列，侧边栏支持折叠隐藏',
      'changes' =>
      array (
        0 => '【修复-播放】M3U8解析测试页播放器改用 hls.js 播放 m3u8（原生 video 不支持 HLS，Chrome/Firefox 此前无法播放）；hls.js 改为 bootcdn/jsdelivr/unpkg 多 CDN 兜底加载，网络不可达时自动切换源',
        1 => '【修复-播放】停止播放时同步销毁 hls.js 实例，切换播放模式/重新解析时先销毁旧实例再重建，避免内存泄漏与串流',
        2 => '【优化-片段列表】片段列表新增「地址」列（绝对地址，超出省略+悬浮显示完整），固定各列宽、表格横向滚动不挤压，容器加边框圆角',
        3 => '【新增-侧边栏折叠】顶部栏新增 ◧ 折叠按钮：折叠后侧边栏收缩为 64px 窄条（仅显示图标），内容区/播放器变宽显示完整；状态 localStorage 持久化，刷新保持；移动端自动隐藏折叠按钮',
        4 => '【验证】php -l 通过；浏览器实测：折叠/展开/刷新持久化正常，M3U8 解析测试页加载无 JS 报错',
      ),
    ),
    'v5.14.7' =>
    array (
      'date' => '2026-09-07',
      'title' => '【资源站多地址+自动换源】新增资源站支持填写多个采集地址，测速排序选最优，失败自动切换备用源',
      'changes' =>
      array (
        0 => '【新增-多地址】资源站支持 api_urls 多地址数组（存储于 config JSON），数据库版 DbResourceSiteManager 与文件版 ResourceSiteManager 同步支持；只填 api_url 的旧数据自动兼容回退为单地址',
        1 => '【新增-测速排序】mx.php 新增 sites/test_urls 测速接口：逐个检测地址可用性与响应时间，按「健康优先+速度升序」排序返回；前端一键把最快可用源排到最前',
        2 => '【新增-自动切换】fetchVideos/searchVideos 改为多地址自动切换：依次尝试 api_urls，首个成功即返回（带 switched_source 标记），全部失败返回各地址失败明细；健康检测 checkSiteHealth 对每个地址分别测速并返回最佳地址 active_url',
        3 => '【新增-前端】后台「资源站管理」与「域名发现」两个页面的采集接口表单均改为多地址列表（➕添加地址/✕删除），支持「⚡测速排序」一键排序；列表展示「N个源」徽标与主地址',
        4 => '【兼容-调用链】AiAutoLearner/OfficialReplaceManager/DbOfficialReplaceManager/gx.php 等全部改为传站点对象搜索/抓取，享受多地址自动切换；旧传字符串调用保持单地址兼容',
        5 => '【验证】php -l 全部通过；本地实测坏地址+备用源：自动切换成功（switched=true，取回20条视频）；testApiUrls 测速排序正常（OK源排前）',
      ),
    ),
    'v5.14.6' =>
    array (
      'date' => '2026-09-07',
      'title' => '【自动学习失败修复】M3U8Parser 自动跟随 Master playlist variant，解决多线程/自动学习全部学习失败',
      'changes' =>
      array (
        0 => '【修复-自动学习】src/M3U8Parser.php parse() 检测到 Master playlist（#EXT-X-STREAM-INF）且无片段时，自动跟随最高带宽 variant 重新解析媒体流，拿到真实片段列表；此前只解析出 0 片段导致学习链路报「Unsupported operand types: array * int」、自动学习/多线程学习全部失败',
        1 => '【兼容】保留 isMaster/variants 元信息，新增 selectedVariant/selectedVariantUri 字段；已有直接解析媒体流的场景不受影响',
        2 => '【验证】php -l 通过；本地实测 master 链接解析 0→645 片段，adPercentage 71.16% 正常可学习',
      ),
    ),
    'v5.14.5' =>
    array (
      'date' => '2026-09-07',
      'title' => '【公告实时化】announcement/list 实时生成"最新版本公告"，不再依赖旧 gg.txt',
      'changes' =>
      array (
        0 => '【公告-实时】announcement/list 基于 version.php 实时生成首条「最新版本 vX.Y.Z 发布：<变更标题>」公告（is_latest_version 标记），始终显示当前最新版本，不再受手工维护的 gg.txt 过期影响',
        1 => '【公告-保留历史】本地 gg.txt 的历史公告仍正常叠加返回，仅新增实时最新版本条目，不改变回复结构',
        2 => '【验证】php -l mx.php 通过',
      ),
    ),
    'v5.14.4' =>
    array (
      'date' => '2026-09-07',
      'title' => '【在线更新源修复 + 健康检测卡死修复】UpdateManager 更新源由 qcb 更正为 MXGT；修复 sites/health_check 全量检测长时间挂起',
      'changes' =>
      array (
        0 => '【修复-版本更新】src/UpdateManager.php 更新源由 ssmhdssmhd/qcb 更正为 ssmhdssmhd/MXGT，与 update.php 统一；修正在线更新检查/下载一直指向旧仓库而判定无更新的问题',
        1 => '【修复-健康检测】gz/ResourceSiteManager.php 将 checkSiteHealth 的 $timeout 真正透传给 fetchVideos/httpGet（此前 8s 超时参数从未生效，默认 30s 兜底导致大量不可达采集源串行把请求拖到分钟级）；fetchVideos 新增 $timeout 参数',
        2 => '【修复-健康检测】batchCheckHealth 新增总时间预算（默认 20s），整体超时即中断返回并带 skipped 统计，后台一键健康检测不再挂起',
        3 => '【验证】php -l ResourceSiteManager.php / UpdateManager.php 全部通过',
      ),
    ),
    'v5.14.3' =>
    array (
      'date' => '2026-09-07',
      'title' => '【接口去重 + API文档补全】删除 notice/* 与 ad_signatures/* 重复别名，补全 API 文档接口索引',
      'changes' =>
      array (
        0 => '【接口去重】mx.php 移除重复别名公告接口 notice/list、notice/save、notice/add、notice/refresh（保留规范名 announcement/*，前端/文档均使用该写法）',
        1 => '【接口去重】mx.php 移除重复别名特征码接口 ad_signatures/list、add、delete、stats、clean（保留规范名 signatures/*）；全局确认无残留引用',
        2 => '【API文档-索引】api_doc.php「完整接口索引」新增「资源站规则 / AI自动学习 / 公告管理 / 嗅探设置」四分类，并在「其他接口」补充 info/version、official/list、official/platforms、parse_test、placeholder_ts，列全所有规范接口',
        3 => '【保留别名】按约定保留公有解析别名 jx、parse/parse、moxi/api 以避免破坏外部已引用链接，仅清理后台私有别名',
        4 => '【验证】php -l mx.php / api_doc.php 全部 No syntax errors detected',
      ),
    ),
    'v5.14.2' =>
    array (
      'date' => '2026-09-07',
      'title' => '【版本标识】发布版本号提升至 v5.14.2',
      'changes' =>
      array (
        0 => '【版本】version_code 由 51401 提升至 51402，build/commit 标记为 v5.14.2-release，无业务逻辑改动',
      ),
    ),
    'v5.14.1' =>
    array (
      'date' => '2026-09-07',
      'title' => '【新增后台两个独立页面】侧边栏新增「域名发现」与「资源站规则」，并新增独立的资源站规则库体系',
      'changes' =>
      array (
        0 => '【页面-域名发现】侧边栏「资源管理」新增「🕵️ 域名发现」页：专注添加/管理资源站域名与采集接口，支持编辑、启停、删除、健康检测、搜索、统计速览',
        1 => '【页面-资源站规则】新增「🗂️ 资源站规则」页：独立规则库，按资源站分组配置时长/不连续/序列/文件名/关键词 5 类广告特征规则，支持增删改、启停、按站筛选、搜索、清空',
        2 => '【后端-独立规则库】新增 resource_site_rules 数据库表（schema_sqlite.sql / schema_mysql.sql），与域名规则(rules_*.php 文件式)完全独立互不干扰',
        3 => '【后端-新增接口】mx.php 新增 resource_rules/* 接口组：list / get / add / update / delete / toggle / clear，全部走数据库、占位符绑定、带 $useDb 降级兜底',
        4 => '【迁移适配】Database.php migrateTables 表清单加入 resource_site_rules；mx.php 初始化时对老库缺失该表自动补建(CREATE TABLE IF NOT EXISTS 幂等)，线上升级无需手动建表',
        5 => '【前端-触发加载】handleNavClick 增加 domain_discovery / site_rules 两页的数据加载，进入页面即自动刷新，资源站规则表单下拉自动填充已有资源站',
        6 => '【验证】php -l mxadmin.php / mx.php / db/Database.php 全部通过；本地 PHP 内置服务器实测 resource_rules add/get/update/toggle/delete/clear/list 全链路 CRUD 通过，mxadmin 新页面与菜单渲染正常',
      ),
    ),
    'v5.14.0' =>
    array (
      'date' => '2026-09-07',
      'title' => '【后台整体推倒重写视觉层】深紫→洋红渐变背景 + 玻璃拟态 (Glassmorphism) 现代后台外观',
      'changes' =>
      array (
        0 => '【视觉重写】mxadmin.php 新增 GlassSkin v6 覆盖皮肤层：body 由浅色改为 深紫(#581c87)→紫(#7e22ce)→紫红(#a21caf)→洋红(#c026d3) 135° 渐变背景并固定滚动',
        1 => '【侧边栏】半透明白色毛玻璃 (rgba(255,255,255,0.10) + backdrop-filter blur 20px)，Logo 渐变文字，菜单分组玻璃化，激活项改为 紫→粉 渐变胶囊 + 发光阴影',
        2 => '【顶部栏】毛玻璃 + 柔和径向高光，底部渐变光带分隔，视觉与内容区一体化',
        3 => '【卡片/数值卡】全属性半透明白玻璃 (blur 16px)，圆角 18px，紫色柔和投影；数值卡左侧色条改为六色渐变，悬停抬升',
        4 => '【按钮/表格/输入/Toast】主按钮改紫粉渐变 + 紫色光晕；表格表头紫→粉渐变白字；输入框、Toast 玻璃化圆角',
        5 => '【兼容】不改动任何页面结构与业务 JS，21 个页面全部保留原功能；背景图模式自动叠加紫色半透明蒙层保持统一观感',
        6 => '【仓库清理】.gitignore 新增忽略：.trae-html-share-packages/ 打包缓存、*.bak/旧备份、test_*/_diag*/_probe*/_e2e*/_build* 等测试/诊断/临时文件，避免污染仓库',
        7 => '【lint 通过】php -l mxadmin.php → No syntax errors detected；本地 php -S 返回 HTTP 200，GlassSkin 皮肤层与 22 个页面结构加载正常',
      ),
    ),
    'v5.13.11' =>
    array (
      'date' => '2026-09-07',
      'title' => '【真链接广告拦截验证 + 无广告链接修复】使用内置广告真实链接验证前置/中插/后置广告、CUE/DATERANGE/AD标签、内置广告字幕均被正确标记并完美拦截；修复过滤后无广告链接无法直播的绝对地址缺陷',
      'changes' =>
      array (
        0 => '【src/M3U8Parser】parse() 解析后为每个片段填充 absoluteUri（经 resolveUri 按媒体URL基地址解析），配合 OutputGenerator useAbsoluteUrls 产出「绝对地址」的过滤后 M3U8——返回的无广告链接可直接交给播放器播放，不再因相对路径解析错源而黑屏',
        1 => '【src/M3U8Parser】getBaseUrl/resolveUri 补全端口号，避免自定义端口（如 :8091）在绝对地址中被丢弃导致片段加载失败',
        2 => '【src/M3U8Parser】修复 #EXT-X-MEDIA 解析偏移错误（15→13）：原偏移导致 TYPE 读空，媒体轨（含广告字幕 SUBTITLES / CLOSED-CAPTIONS）过滤失效；修复后 filterSubtitles 能正确剥离内置广告字幕轨、隐藏式CC轨及变体上的 SUBTITLES/CLOSED-CAPTIONS 属性，仅保留音频与视频',
        3 => '【拦截验证】真实测试链接（cache/m3u8/test）：前置/中插/后置广告按关键词+文件名+片段模式全部命中移除；EXT-X-AD-START/END、EXT-X-DATERANGE、EXT-X-CUE-OUT/IN 广告标签段被识别且输出中标签全部剔除；混合流 mixed.m3u8 89 段中 14 段广告全部拦截，过滤后 M3U8 纯净（#EXT-X-ENDLIST 保留、广告标签清零、显示绝对地址）',
        4 => '【lint 通过】php -l src/M3U8Parser.php → No syntax errors detected',
      ),
    ),
    'v5.13.10' =>
    array (
      'date' => '2026-09-07',
      'title' => '【新增 M3U8 解析测试工具页】侧边栏新增解析测试，支持实时播放定位、片段列表、原始/过滤后 M3U8 对比、批量标记',
      'changes' =>
      array (
        0 => '【页面】后台侧边栏「接口工具」新增「M3U8解析测试」，暗色主题，含 URL/站点/代理输入栏、开始解析',
        1 => '【解析】新增 mx.php?action=parse_test 接口，返回片段明细（广告/正片标记、时长、起始时间）、原始/过滤后 M3U8 文本、统计（总段/广告/保留/耗时）',
        2 => '【播放】支持 4 种模式切换（播放过滤后/播放原始/整片/过滤后），用 Blob 直接播放，规避 mxjx 通道 JSON 守卫改写问题',
        3 => '【实时定位】跟随播放定位片段：播放时自动高亮对应片段行并滚动到可见，更新命中广告区域',
        4 => '【片段列表】全部/广告/正片/已标记 过滤、按段号/地址搜索、加载更多、区间批量标记、疑/止/广告类型下拉标记、取消标记',
        5 => '【对比】原始 M3U8 与过滤后 M3U8 左右分栏展示，一键复制',
        6 => '【lint 通过】php -l mx.php + mxadmin.php + version.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.9' =>
    array (
      'date' => '2026-09-07',
      'title' => '【在线更新源切换：qcb → MXGT】update.php 更新源改为当前 MXGT 仓库 main 分支 + 修复 PHP 8.5 curl_close 弃用警告',
      'changes' =>
      array (
        0 => '【update源切换】update.php 更新源由 ssmhdssmhd/qcb 改为 ssmhdssmhd/MXGT（main 分支），后续在线更新从当前仓库拉取',
        1 => '【兼容性修复】移除外层 curl_close()（PHP 8.0+ 句柄自动释放，8.5 已废弃），消除 Deprecated 警告，保证更新检查/下载逻辑无报错输出',
        2 => '【lint 通过】php -l update.php + version.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.8' =>
    array (
      'date' => '2026-08-14',
      'title' => '【APK 播放优化：官替 m3u8 直链优先 + xmflv.cc 真实源逆向分析失败文档化】',
      'changes' =>
      array (
        0 => '【J1 根因确认】此前 jiexi.php 返回的 play_url=https://jx.xmflv.cc/?url=... 是 HTML 播放器页面（<title>虾米播放器 <div id="Xmflv"> + 4 个混淆JS bundle + inline eval/Base64/RC4/window/DOM 动态解密），TVBox/影视仓 APK 内置播放器（ExoPlayer/IJKPlayer/MediaPlayer）只认 .m3u8/.mp4 直链，HTML 页面 URL 直接拿去播 = 播放失败黑屏',
        1 => '【J2 xmflv.cc 逆向】抓 jx.xmflv.cc HTML 10.6KB：5 个 <script src>（Cloudflare beacon + 4 个网易云 CDN JS 混淆包）+ 2 段 inline script：第一段 OBF/RC4+atob+urlDecode 动态解密，第二段 pushCode(@@@转义) + eval。URL/m3u8/parse/api/playerConfig 在 HTML 页面里 0 个明文字面量，全部在浏览器端 window+DOM 后才出来。结论：纯 PHP 服务器端无法逆向真实视频源。',
        2 => '【J3 concurrent 模式重写为「先官解 1-2s 快速备用 + 后官替 12s 直调 APK 可播优先」】执行顺序：①官解 curl_multi 请求 jx.xmflv.cc（1-2s 返回 HTML 播放器 URL 做兜底）；②官替 callOfficialReplaceDirectV2（OfficialReplaceManager 资源站匹配，带独立 replace_direct_timeout=12s 预算，超过就放弃不再 66s 阻塞）。主 play_url = replace_url ?? official_url：官替成功→返回资源站 m3u8 直链 APK 直接播；官替失败/超时→返回 jx.xmflv.cc HTML 播放器 URL 做 WebView/浏览器兜底',
        3 => '【J3 xt/config.php 新增 performance.replace_direct_timeout = 12.0】默认 12 秒（1-60 可配置），安全余量远小于 nginx fastcgi_read_timeout 60s / PHP max_execution_time 30s 避免 502',
        4 => '【J3 XT_CONCURRENT_RESULTS 双通道字段仍输出】JSON 同时含 official_url（jx.xmflv.cc HTML 播放器）+ replace_url（资源站 m3u8 直链），APK 侧可自行选择：替换源 m3u8（APK 直接播）/ 官方源 WebView（内嵌虾米播放器容器）',
        5 => '【APK 用法】部署后：①若官替资源站匹配成功（有片源），jiexi.php/xt.php 返回 .m3u8 直链→APK 秒播；②官替无匹配或超时，返回 jx.xmflv.cc HTML 播放器→APK 侧切换到 WebView 模式（内嵌 iframe）播放。想 100% 出 APK 可播直链→把嗅探模式从「同时调用」切到「官替(replace)」即可（只跑官替直调，匹配失败直接返回解析失败）。',
        6 => '【lint 0 错误通过】php -l xt/server.php + xt/config.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.7' =>
    array (
      'date' => '2026-08-14',
      'title' => '【修复：同时调用模式真正并行 + 127.0.0.1 回环 URL 彻底过滤】',
      'changes' =>
      array (
        0 => '【I1 根因确认】服务器上 xt.php 已部署且无参/错误URL秒回400，但真实URL超时因为 concurrent 模式串行执行（先官解1s+官替66s=67s超nginx 60s→502 Bad Gateway）。本地测试确认：callApiSingle 官解1.6s返回正确jx.xmflv.cc URL，官替20s返回NULL（资源站匹配不到）',
        1 => '【I2 真正并行：官解+官替同时跑 curl_multi】重写 getVideoLinkByConcurrentRace：①官解接口+官替HTTP请求合并到 allApis 数组→同一个 curl_multi 并发池→谁先成功用谁；②官替包装成 HTTP 请求 mx.php?action=official_replace/info（type=json url_field=ad_skip_url _channel=replace），不再用 callOfficialReplaceDirectV2 PHP 直调（直调无法放进 curl_multi）；③总超时 max(perfCfg.timeout, 90s) 给官替资源站匹配足够时间',
        2 => '【I2 buildApiUrl 修复】新增 url= 检测：模板已包含 url=（如官替完整HTTP URL）时不再追加 urlencode(videoUrl)，避免 URL 重复拼接',
        3 => '【I2 extractVideoUrl 并发 context 修复】concurrentRaceRequest 在调用 extractVideoUrl 之前，先把 requestContextByHandle[\'last\'] 更新为当前完成 handle 的 context（原来指向最后一个创建的 handle，并发场景下可能是错误的）',
        4 => '【I2 127.0.0.1/localhost 回环 URL 彻底过滤】concurrentRaceRequest + callApiSingle 两处：extractVideoUrl 返回的 URL 如果包含 127.0.0.1 或 ://localhost，直接记录失败并返回 null，不再把回环地址当 play_url 返回',
        5 => '【I2 本地测试通过】elapsed: 2.622s, code: 200, url: https://jx.xmflv.cc/?url=...&ref=https%3A%2F%2Fv.youku.com%2F, official_url: 同上, replace_url: NULL（官替服务器旧代码超时但主URL正确）',
        6 => '【lint 0 错误通过】php -l xt/server.php + xt/PerformanceOptimizer.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.6' =>
    array (
      'date' => '2026-08-14',
      'title' => '【新增：xt.php?url= 嗅探统一远程调用入口 + 解析列表接口升级】',
      'changes' =>
      array (
        0 => '【H1 新建 xt.php 入口：完全遵循嗅探设置】文件路径 xt.php，调用格式 xt.php?url=视频链接，内部 require xt/server.php 后直接调用 parseVideo()：100% 走 mxadmin 嗅探设置的当前通道（concurrent / official / replace），无需改代码即可切换行为。同 jiexi.php 支持 5 种视频参数（url/wd/v/video/t）+ 4 种输出格式（json/302/api/xml）+ JSONP callback',
        1 => '【H1 xt.php 同时调用模式输出双通道结果】读取 $GLOBALS[XT_CONCURRENT_RESULTS]，JSON/api/xml 三种格式下附加 official_url(官解通道独立URL) + replace_url(官替通道独立URL) 两个字段，和 jiexi.php G4 行为一致',
        2 => '【H2 parse/list 升级：所有远程 API 格式清晰枚举】mx.php action=parse/list（即 mx.php?action=parse/list）的 usage 表从 4 条扩展到 9 条，首位新增【推荐】xt.php?url=，新增 jiexi.php / api/v2 / official_replace info 与 resolve / parse/list 自指；supported_types 保持不变',
        3 => '【lint 0 错误通过】php -l xt.php + mx.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.5' =>
    array (
      'date' => '2026-08-14',
      'title' => '【修复：同时调用模式不再返回 127.0.0.1 + 双通道独立结果输出 JSON】',
      'changes' =>
      array (
        0 => '【G1 根因定位】getVideoLinkByConcurrentRace L885-905 原逻辑：replace_api.url 为空时自动生成 http://127.0.0.1/mx.php?action=official_replace/info&url= HTTP 回环地址→该 URL 被当成 play_url 返回给 jiexi.php。且 time=0s 说明是旧缓存命中（cacheKey=md5(videoUrl) 不含 mode，切换通道后旧缓存仍有效）',
        1 => '【G2 重写 getVideoLinkByConcurrentRace：官解走 curl_multi，官替走 PHP 直调】彻底删除 L885-905 的 HTTP 回环 URL 生成逻辑。新流程：①官解接口→concurrentRaceRequest(curl_multi 并发)→official_url；②官替接口→callOfficialReplaceDirectV2(PHP 内部直调 OfficialReplaceManager::resolve)→replace_url。不再生成任何 127.0.0.1/localhost URL，消除 HTTP 回环问题',
        2 => '【G3 双通道独立结果返回】getVideoLinkByConcurrentRace 返回值新增 official_url + replace_url + replace_orm 三个字段；同时写入 $GLOBALS[XT_CONCURRENT_RESULTS] 全局变量供 jiexi.php 读取。主 play_url 优先取官解（jx.xmflv.cc ~1s 响应快），官解失败 fallback 官替',
        3 => '【G4 jiexi.php 输出双通道独立结果】outputSuccess 函数新增 $extraUrls 参数，JSON 输出新增 official_url 和 replace_url 两个字段。同时调用模式下返回示例：{"code":200,"url":"https://jx.xmflv.cc/?url=...","official_url":"https://jx.xmflv.cc/?url=...","replace_url":"http://resource-site.com/.../video.m3u8"}。api/xml 格式也支持附加字段',
        4 => '【G5 缓存失效修复】cacheKey 从 md5($videoUrl) 改为 md5($videoUrl . | . sniffer.mode)，切换通道（official↔replace↔concurrent）时自动失效旧缓存，不再返回旧模式的缓存 URL',
        5 => '【PHP lint 0 错误通过】php -l xt/server.php + jiexi.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.4' =>
    array (
      'date' => '2026-08-14',
      'title' => '【新增：嗅探设置「同时调用」模式 + html_player 接口类型 + jiexi.php 多通道输出】',
      'changes' =>
      array (
        0 => '【F1 后台 UI 新增第三个通道选项「同时调用(concurrent)」】mxadmin.php 嗅探设置页主路由选择区，在原有「官解解析(official)」「官替接口(replace)」两个 radio 卡片之后新增第三个 radio「同时调用(concurrent)⚡v5.13.4」：选中后官解+官替同时发起 curl_multi 并发请求，最快成功的立即返回 play_url 给 jiexi.php；卡片描述明确标注"同时并发请求，最快成功的立即返回结果给 jiexi.php"',
        1 => '【F1 官解/官替接口类型下拉新增 html_player 选项】mxadmin.php 两处 <select>（snifferOfficialType / snifferReplaceType）新增 <option value="html_player">html_player（HTML播放器页面，直接返回URL给iframe/302）</option>，jx.xmflv.cc 等HTML播放器接口选 html_player；同时 html_player 类型时 url_field 非必填（saveSnifferConfig 校验逻辑跳过），解决虾米新接口不需要 JSON 字段名的问题',
        2 => '【F2 loadSnifferConfig/saveSnifferConfig/updateSnifferBadges 全面支持 concurrent】①loadSnifferConfig：默认 mode 从 official 改为 concurrent，新增 snifferModeConcurrent.checked 赋值；②saveSnifferConfig：mode 默认值改 concurrent，html_player 类型跳过 url_field 必填校验，concurrent 模式软校验提示"需要官解和官替都启用"；③updateSnifferBadges：concurrent 模式下两个通道卡片都标"并发中"且高亮 is-current，红色警告检测条件改为"concurrent 需两通道都启用"',
        3 => '【F3 mx.php 后端 save handler 接受 concurrent + html_player】①mode 白名单从 [official,replace] 扩展为 [official,replace,concurrent]，默认值改 concurrent；②type 白名单从 [redirect,json,text] 扩展为 [redirect,json,text,html_player]；③sniffer/config GET handler 默认 mode 从 official 改为 concurrent；④保存生成的 sniffer_config.php 注释更新为"concurrent=同时调用 / official=官解 / replace=官替"',
        4 => '【F4 server.php parseVideo 支持 mode=concurrent】原逻辑 $concurrentRace = !empty(concurrent_race_enabled) 改为：$snifferMode = mode ?? concurrent；若 mode=replace 且旧 concurrent_race_enabled=true 则自动升级为 concurrent（向后兼容）；$concurrentRace = (snifferMode === concurrent)；mode=concurrent → getVideoLinkByConcurrentRace 同时并发，mode=official/replace → getVideoLinkBySnifferMode 单通道+fallback',
        5 => '【F5 默认配置升级】①config.php sniffer.mode 默认从 replace 改为 concurrent；②config.php performance.concurrent_race_enabled 注释更新为"v5.13.4 已被 mode=concurrent 取代，仅作旧配置兼容"（值保持 true 确保向后兼容）；③sniffer_config.php mode 从 replace 改为 concurrent；官解接口 type=html_player 已在 v5.13.3 设好，本次确认无需改',
        6 => '【PHP lint 0 错误通过】php -l 5 文件：mxadmin.php / mx.php / xt/server.php / xt/config.php / xt/sniffer_config.php → 全部 No syntax errors detected',
      ),
    ),
    'v5.13.3' =>
    array (
      'date' => '2026-08-14',
      'title' => '【Hotfix：虾米官解新地址 https://jx.xmflv.cc/?url=&ref= 接入 + HTML播放器类型支持 + {url}/{ref}占位符 + Cloudflare WAF 兼容】',
      'changes' =>
      array (
        0 => '【D1 新接口探测】curl 3 场景（裸UA/Chrome126+ref=https://v.youku.com/ / 禁止跳转）确认 jx.xmflv.cc/?url=&ref= 返回的是完整 HTML 播放器页面(<title>虾米播放器…</title><div id=Xmflv></div> + 混淆 Xmflv JS runtime 拉流)；传统 api/v2 / mx.php / api.php / jx.php 等所有 JSON/Redirect 子路径全部 404；结论：新接口是「HTML播放器页」类型，play_url 就返回整段播放器 URL（302/iframe直接打开即可），无需后端抽 play_url JSON',
        1 => '【D3 buildApiUrl 占位符升级】xt/PerformanceOptimizer::buildApiUrl 原「纯前缀 + 后缀拼 urlencode」扩展为 6 个占位符替换：{url}=urlencode(原始视频页URL)、{ref}/{referer}=guessPlatformReferer 推断平台Referer、{origin}=scheme://host[:port]/、{ts}=time()秒、{t}=毫秒时间戳；模板中无占位符时保留旧行为，100% 向后兼容老前缀式配置',
        2 => '【D3 guessPlatformReferer 精准来源】新增私有方法 guessPlatformReferer($videoUrl) 针对优酷/爱奇艺/腾讯视频/芒果/乐视/B站/搜狐/PPTV 返回官方 Referer（例如 youku → https://v.youku.com/），其它站点按原 URL 返回 scheme://host/；同时被 buildApiUrl({ref}) 和 createCurlHandle(HTTP Referer 头) 两处复用',
        3 => '【D3 extractVideoUrl wrapper：识别HTML播放器页直接返回整URL】原 extractVideoUrl 全部走 jiami 闭包抽 JSON，对新 jx.xmflv.cc（HTML 不含裸m3u8）永远失败；v5.13.3 在调用 jiami 前前置4类命中判断：① api.type=html_player/page/iframe 且响应含 HTML DOCTYPE；② <title>虾米播放器 命中；③ 含 id="Xmflv"/class=Xmflv；④ host=xmflv.cc/jmflv/jx.xm* + 页面含 Xmflv 特征；命中且请求URL合法时，直接把 buildApiUrl 拼好的 xmflv.cc URL 作为 play_url 返回；并发 curl_multi 与串行 callApiSingle 统一走同一入口',
        4 => '【D4 5处默认配置替换为 jx.xmflv.cc enabled=true】xt/config.php：①sniffer.official_apis[] ②sniffer.official_api ③顶层 official_apis fallback 数组；xt/sniffer_config.php ④official_apis[] ⑤official_api：共5处，统一 url=https://jx.xmflv.cc/?url={url}&ref={ref} type=html_player headers含Accept/Accept-Language/sec-ch-ua/Upgrade-Insecure-Requests；name 字段明确标注 v5.13.3+html_player/302/iframe 直接播放',
        5 => '【D4 Cloudflare 403 兼容 + UA 升级】createCurlHandle 对 host 含 xmflv.cc / jmflv / jx.* 自动补 Chrome126 完整请求头：Accept(HTML优先) / Origin / Referer(按平台) / sec-ch-ua三件套 / Upgrade-Insecure-Requests；配置中的 http.user_agent 默认 Chrome120 升级 Chrome126（Mozilla/5.0 或空会自动替换）；大幅降低 jx.xmflv.cc 受 Cloudflare WAF 返回 403 的概率',
        6 => '【D4 requestContextByHandle 上下文】PerformanceOptimizer 新增成员变量 requestContextByHandle：每次 createCurlHandle 创建句柄时按 (int)$ch 存一份 {url,api,video_url}，并引用备份到 [last]；extractVideoUrl wrapper 直接取 requestContextByHandle[last][url] 作为HTML播放器接口最终返回，不修改 jiami 核心闭包（jiami v5.10.9 完全兼容，无需重新加密）',
        7 => '【D4 parseVideoByOfficialChannel 完美承接】官解最终返回的整段 jx.xmflv.cc 链接没有 .m3u8 后缀，parseVideoByOfficialChannel 会走 else 分支：setCache + buildResult(200,解析成功,$playUrl,$videoLink,$startTime)，直接把链接作为 play_url 返回给客户端，不做任何广告处理（由虾米官方浏览器端播放器自行处理CDN/广告逻辑，与旧 iframe 方式一致）',
        8 => '【PHP lint 0 错误通过 / 向后兼容】php -l 6 文件：xt/PerformanceOptimizer.php / xt/config.php / xt/sniffer_config.php / xt/server.php / mxadmin.php / version.php → 全部 No syntax errors detected；对外 JSON 字段完全不变，对旧配置纯前缀式 URL（无{占位符}）100% 与 v5.13.2 行为一致',
        9 => '【E4 验证加固：callApiSingle requestContext debug 落盘】callApiSingle curl_exec 后立即把 curl_getinfo + 响应体前 8KB 样本写入 requestContextByHandle[(int)$ch]，包含 http_code / content_type / size_download / request_size / total_time_ms / curl_error / 伪响应头（含状态行+主要header摘要）/ response_body_sample；前端时间线/调试脚本可直接读出 HTTP200 + <title>虾米播放器 + id=\"Xmflv\" 命中，证明接口真正被正确调用（绕过 jiami 黑盒 parseVideo 的 mode/replace 强制直调干扰）',
        10 => '【E3 端到端调用验证通过】E2 buildApiUrl 占位符 PASS(youku→https://jx.xmflv.cc/?url=URLENC&ref=https%3A%2F%2Fv.youku.com%2F) + E3 callApiSingle HTTP 200 text/html 响应4.8KB(虾米播放器)返回 play_url=jx.xmflv.cc 完整链接 PASS，最终汇总断言 EXIT=0',
      ),
    ),
    'v5.13.2' =>
    array (
      'date' => '2026-08-14',
      'title' => '【Hotfix：虾米官解 api/v2 业务级「验证失败!」根因修复 + 静默失败可追溯 + 后台告警横幅】',
      'changes' =>
      array (
        0 => '【C1 根因复现定案】直接 curl 请求第三方 114.134.184.91:9002/mx.php?action=api/v2&type=parse&url=...（5 种组合：裸UA/浏览器Chrome126UA+Accept/Referer:v.youku.com/加ts时间戳/加X-Forwarded-For）全部返回 {"success":false,"code":500,"message":"❌<br>验证失败!"}，排除简单 header/时间戳缺失，结论：第三方服务器已于 2026-08-14 改为签名+白名单校验，非授权 IP 100% 失败（非本项目代码 Bug）',
        1 => '【C2 默认配置下线已失效虾米官解】xt/config.php 3 处：sniffer.official_apis 数组 / sniffer.official_api 单接口兼容项 / 顶层 official_apis 兜底数组 + xt/sniffer_config.php 2 处 official_apis + official_api 共 5 处全部 enabled=false，并在 name 和注释中明确标注「第三方 2026-08-14 起已加签名验证，请改用官替本地直调或替换为可用的官解接口」，sniffer_config.update_date 升级到 2026-08-14',
        2 => '【C3 结束官解静默失败黑暗期】xt/PerformanceOptimizer 新增 recordFailedApi($api,$httpCode,$response,$extraReason) 辅助方法：自动识别 HTTP 非200 / 空响应 / HTTP200 但 JSON {success:false,code!=200,status!=1} 业务级错误(提取 message/msg/ZT)/成功但 url_field 为空 四类错误，写入 $GLOBALS[\'XT_FAILED_API_REQUESTS\'][]={name,url_prefix,http_code,response_len,reason,biz_message,ts_ms}',
        3 => '【C3 两处请求链路统一接 recordFailedApi】①callApiSingle 单接口串行请求：curl句柄创建失败/HTTP非200/空响应/业务级错误/提取空5类分别写明细；②concurrentRaceRequest 并发竞速路径：curl_multi_info_read 每完成一条即检测 HTTP 状态码 + JSON success 判断，同样写 recordFailedApi 再 recordApiResult(false) 扣分，并发路径不再是黑盒失败',
        4 => '【C4 后端诊断读出失败明细】xt/server.php B4 嗅探诊断时间线读取 $GLOBALS[\'XT_FAILED_API_REQUESTS\']，在 summary 中追加「官解接口失败明细(N 条)：1. 虾米官解 → 业务级错误：验证失败!；HTTP=200；resp_len=209；上游原消息=❌<br>验证失败!」并把 failed_api_requests 数组挂到 diagnosticStep.detail + debug_info.sniffer_diagnostic.failed_api_requests，前端时间线默认展开即看到具体错误',
        5 => '【C4 业务级错误自动出修复建议】B4 fix_tips 生成逻辑新增 2 条命中：①若 failed_api_requests 中任意条目 biz_message 包含「验证失败」且 fix_tips 空 → 自动输出「官解上游返回验证失败!，此服务器需要签名/白名单，未授权IP无法使用 → 切到 replace 模式 + 官替URL留空走本地直调」；②若 http_code=0 连接失败 → 自动输出「官解 curl 连接失败（超时/拒绝/不通）→ 取消该外部官解启用，改官替本地直调」',
        6 => '【C4 114.134.184.91:9002 文案升级】原 B4 的「已宕机 502」修正为更准确的「2026-08-14 起已加签名验证，任意请求返回验证失败!」并附带修复动作；失败返回的 message 直接替换为对该用户最具针对性的第 1 条 fix_tip，不再是泛泛的「当前通道未能解析」',
        7 => '【C4 嗅探设置红色告警 Banner + 一键修复】mxadmin.php 嗅探设置概览卡底部新增 xiamiDeprecatedBanner（默认 display:none），loadSnifferConfig() 完成后立即检测 official_api / official_apis 数组中是否存在 enabled=true 且 URL 包含 114.134.184.91 或 :9002，是则弹出并包含 4 步操作清单 + 「✅ 一键修复：取消该官解启用 + 官替URL置空 + 切到 replace 主路由」按钮（xiamiBannerOneClickFix）',
        8 => '【C4 一键修复 UX 收尾】点击后：标记 sniffer dirty（●有修改未保存 badge）、绿色滚动高亮「💾 保存嗅探设置」、5 秒成功 Toast「已完成一键修复预设：请点击保存再去首页刷新播放页即可」，保存后首页嗅探报错立即消失；Banner 同时保留「稍后自己改」收起按钮',
        9 => '【C5 语法 / 回归检查】php -l 5 文件：xt/config.php / xt/sniffer_config.php / xt/PerformanceOptimizer.php / xt/server.php / mxadmin.php → 全部 No syntax errors detected；C1/C3 端到端手动验证：即便用户手动重新启用 114.134.184.91，嗅探失败时返回 JSON 的 debug_info.sniffer_diagnostic.failed_api_requests[0].biz_message 也一定包含「验证失败!」，Banner 一定弹出',
      ),
    ),
    'v5.13.1' =>
    array (
      'date' => '2026-08-17',
      'title' => '【Hotfix：嗅探测试 502 Bad Gateway 根因修复 + 报错 UI 美化 + 通道诊断时间线】',
      'changes' =>
      array (
        0 => '【根因链路还原 B1】用户截图「502 Bad Gateway nginx」真实触发点：嗅探测试请求 xt/api.php → 走官替直调 callOfficialReplaceDirectV2 CPU 过载超过 Nginx FPM 超时(默认30s) → nginx 直接吐 502 HTML → 前端 JSON.parse 失败后整段裸贴 HTML（虾米官解 114.134.184.91:9002 本身也宕机 502，双通道双 502 叠加）',
        1 => '【B2 官替直调预算时间保护】xt/server.php 官替直调外层新增 $directBudget = min(performance.timeout, 25 秒)，直调前用 ini_set 收紧 max_execution_time，设置 $GLOBALS[\'XT_REPLACE_DIRECT_DEADLINE\'] 供 OfficialResolveManager 长循环感知；达到预算 99% 仍未成功立即软中断降级走 HTTP 官替，避免再触发 nginx 502',
        2 => '【B3 非 JSON/502 报错 UI 全面美化】mxadmin.php testSniffer() 原「返回非 JSON：<pre>整段502 HTML」拆为 5 档分级诊断卡(502/504/500/403/空响应/默认异常)：彩色卡片顶部(大标题+status-pill+HTTP状态+响应长度) + 双栅格(左：按概率排序的可能原因 / 右：修复操作建议) + 可折叠「查看原始响应」面板，502 场景自动匹配「官替直调 CPU 过载 / 虾米官解宕机 / FPM 满」3 条原因和 3 条一键修复建议',
        3 => '【B4 嗅探失败自动诊断时间线】xt/server.php 所有通道(直调→HTTP官替→官解→fallback数组)全部失败后，自动追加一条「🕵 嗅探通道诊断」时间线条目：展示当前模式 / 并发竞速开关 / 启用了哪些官解接口 / 官替是否启用 / 官替是否走本地直调 / 直调失败原因 / 直调耗时预算 vs 实耗 / 自动识别是否配置了宕机虾米 114.134.184.91:9002，附 fix_tips 数组，前端时间线默认展开失败条目可直接看到',
        4 => '【B4 debug_info 补充字段】失败返回 JSON 的 debug_info 新增 sniffer_diagnostic 子结构：mode / official_apis_enabled / replace_enabled / replace_direct_fail_reason / fix_tips，旧版前端也能在失败摘要上读到',
        5 => '【宕机服务器自动识别】后端 B4 诊断逻辑里检测到官解接口 URL 包含「114.134.184.91」或「:9002」时，fix_tips 首条明确提示「该服务器当前已宕机 502，请取消勾选官解接口的启用改走官替本地直调」，失败消息不再是泛泛的「当前通道未能解析」，直接替换为对该用户最具针对性的第一条 tip',
        6 => '【PHP lint 0 错误通过】php -l xt/server.php / mxadmin.php / version.php → 全部 No syntax errors detected；B2/B4 只在 parseVideo 函数内部加变量与分支，不改任何对外 API 签名与字段结构，前后向兼容',
        7 => '【版本元信息升级为 v5.13.1】version_code=51301 build=20260817-hotfix-sniffer-502-ui-diagnostic updated_at=2026-08-17；CHANGELOG 顶部追加 Hotfix 章节；README 附修复前后对比与用户可直接套用的 1 分钟修复操作清单',
      ),
    ),
    'v5.13.0' =>
    array (
      'date' => '2026-08-17',
      'title' => '【后台全面美化升级：全模块与嗅探设置风格统一，干净美观】',
      'changes' =>
      array (
        0 => '【设计规范统一】以用户好评的「嗅探设置」页面为基准，抽取 7 类通用组件类：① step-badge 编号徽章 ② overview-grid/overview-item 概览双栅格 ③ form-grid/inline-form-grid 表单双栅格 ④ status-pill 状态徽章 ⑤ sub-card+sub-card-header 子卡片 ⑥ action-bar 操作按钮栏 ⑦ section-caption / form-tip 说明文案，全部写进 mxadmin.php 内联 <style>，全站共享',
        1 => '【设计令牌 CSS 变量】定义 --primary / --success / --warning / --danger / --info / --purple 六色主色，圆角 --radius-sm/base/lg，间距 spacing-2/3/4/5，阴影 shadow-sm/base/lg，边框 border-base，文本 text-primary/regular/secondary，所有页面同一配色不漂移',
        2 => '【全局步骤编号系统】step-title + step-badge(info/success/warning/danger/primary/purple) 明确每个模块的执行顺序和层级关系；sub-card-header 内置字母徽章(A/B/C)做子模块编号，信息架构一目了然',
        3 => '【A4-1 3页升级】page-history 播放记录（概览卡+表格+快速操作）、page-batch 批量解析（概览+输入+选项sub-card+进度）、page-analyze 视频广告分析（概览卡+6项输入参数栅格+结果6色指标卡）',
        4 => '【A4-2 4页升级】page-rules 规则管理（概览+筛选子卡+操作栏+表格）、page-sites 资源站管理（概览+批量巡检子卡+新增表单+资源站表格）、page-ai_autolearn AI自动学习（双栅格概览+基础开关/样本过滤/资源站/附加选项4个分组sub-card）、page-official_sites 官方资源站（概览+推荐站表格+参数配置）',
        5 => '【A4-3 6页升级】page-official_replace 官替解析（概览+状态卡+核心参数/支持平台/API测试/在线播放/接口文档7个子模块）、page-moxi_api（概览+字段说明sub-card+多模式测试sub-card）、page-play 播放器（概览+内核参数sub-card+播放测试）、page-database 数据库（状态+表结构检查+配置+迁移4个大模块）、page-update 系统更新（8个运维卡片结构化）、page-autoupdate 自动维护（概览卡+8步详情sub-card+彩色日志面板）',
        6 => '【A4-4 6页升级】page-announcement 公告（概览双栅+操作面板+公告源优先级列表）、page-auth 授权（概览卡+4指标状态网格+本地/远程详情sub-card+授权配置inline-form-grid+授权码录入）、page-ai_skip AI去广告（保留紫渐变横幅+7编号模块+开关栅格化+结果对比sub-card）、page-ai_insert 插播识别（保留粉紫渐变横幅+4编号模块）、page-ai_subtitle 字幕分析（保留青绿渐变+6色指标卡+pill样式示例链接）、page-ai_watermark 水印处理（保留蓝青渐变+净化前后两张带编号徽章的sub-card对比）',
        7 => '【响应式布局】表单栅格统一使用 auto-fit + minmax 响应式写法，小屏自动变单列；overview-grid 默认两列，窄屏自动折叠为单列；按钮用 action-bar tight/with-top 自动换行',
        8 => '【AI 四大页面保留主题色】ai_skip(紫) / ai_insert(粉紫) / ai_subtitle(青绿) / ai_watermark(蓝青) 四张原渐变横幅完整保留，在外层统一包装 ①概览卡 ②编号步骤标题 ③sub-card 选项面板，既保留品牌色又统一整体风格',
        9 => '【表格/统计/按钮全面去内联样式】原先大量 style="display:flex;gap:12px;margin:16px 0" 等散乱内联样式，统一用 action-bar / sub-card / inline-form-grid / section-caption / form-tip 类替换，样式不再散落在 HTML 上，后续好改一处全站生效',
        10 => '【说明文案人性化】每个页面的概览卡 overview-item 都写了「这个模块用来干嘛 / 推荐怎么用 / 最佳实践」，并在表单下用 form-tip 给出易错点提示，减少用户看文档需求',
        11 => '【PHP lint 0 错误通过】php -l mxadmin.php：No syntax errors detected；核心修改为纯 HTML/CSS 类名替换，不改任何 PHP 逻辑变量名，不影响原有功能 API 行为',
        12 => '【版本元信息同步升级】version.php version=v5.13.0 build=20260817-ui-beautify-unified-backend updated_at=2026-08-17；README / CHANGELOG.md 同步补充 UI 美化专项说明与组件规范对照表',
      ),
    ),
    'v5.12.0' =>
    array (
      'date' => '2026-08-16',
      'title' => '【6平台独立元数据解析器(策略模式) + 极简提取减轻服务器负担】',
      'changes' =>
      array (
        0 => '【策略模式重构】OfficialReplaceManager 平台独立解析器彻底分开：fetchMeta_Youku / fetchMeta_Tencent / fetchMeta_Iqiyi / fetchMeta_Mgtv / fetchMeta_Bilibili / fetchMeta_Generic 六个独立方法，各自维护，互不干扰，好维护',
        1 => '【极简提取 = 减轻服务器负担】只保留 base_title(剧名) + episode_num(集数) 两个字段，description/cover/subtitle_guess/total_episodes/raw_title/hits 全部固定为空/空数组，减少内存占用和字符串处理开销',
        2 => '【通用提取引擎 _extractQuickBaseAndEpisode 全新上线】三层提取优先级：①内联JS(usercfg/__NEXT_DATA__/__INIT__/__INITIAL_STATE__ 等对象字面量/JSON)扫前260KB即break ②og:video:series_name / tv:series_name 等 meta 标签兜底 ③og:title/<title> 截取"第X集"前文本做最终兜底',
        3 => '【剧名清洗】自动去除《》<>""\'\'引号、去在线观看/高清/电视剧/电影/综艺/动漫/纪录片 等平台分类后缀、去优酷/爱奇艺/腾讯视频/芒果tv/哔哩哔哩 等站名后缀、banWords 黑名单过滤，长度严格 2~30 字符(电影40)',
        4 => '【集数提取多格式支持】第X集/话/期/部/季 中文写法、EPXX 英文写法、X/总集数(如 2/24全) 斜杠写法，统一从 og:title + <title> 拼接短文本(限320字)一次扫描，避免全HTML回溯',
        5 => '【优酷 fetchMeta_Youku】优先 usercfg.showName / videoShowName（避免 og:title 带副标题"张启山和吴老狗达成合作"污染 base_title），正确样例：九门 第2集 → base=九门 ep=2',
        6 => '【腾讯 fetchMeta_Tencent】优先 ld+json partOfSeries.name + episodeNumber（schema.org TVEpisode/TVSeries 标准字段，最稳定），兜底 __NEXT_DATA__ props.pageProps.seriesInfo.partOfSeries.name/seriesName',
        7 => '【爱奇艺 fetchMeta_Iqiyi】优先 og:video:series_name meta 标签（iqiyi 官方页面标准元数据），兜底 window.Q.playerInfo.albumName/seriesName/tvName',
        8 => '【芒果TV fetchMeta_Mgtv】优先 __INIT__.showInfo.showName/seriesName/partOfSeries.name（芒果内嵌初始化对象），meta 兜底',
        9 => '【B站番剧 fetchMeta_Bilibili Bangumi】优先 __INITIAL_STATE__.mediaInfo.season.title/seasonName/partOfSeries.name（使用完整系列名如"咒术回战 第二季"，利于资源站搜索匹配）',
        10 => '【B站UGC fetchMeta_Bilibili UGC】从 __INITIAL_STATE__.videoData.title 直接取完整标题(含4K修复等括号信息)作为剧名，无集数(ep=null)，处理全角括号和UTF-8编码异常',
        11 => '【通用 Generic】未知站点统一扫 showName/seriesName/albumName/partOfSeries.name 等常见字段，兼容任意影视站，og:title/<title> 兜底',
        12 => '【Step Trace 调试机制】resolve/fetchVideoInfo 每一步记录 name/title/status/summary/detail/elapsed_ms/ts，前端 mxadmin.php 嗅探测试区可视化时间线展示 ✓成功 △警告 ✕失败 ℹ信息，失败时一眼定位哪环节出问题',
        13 => '【服务器负担验证】7平台 mock 单元测试全部断言"其余字段为空"，description/cover/subtitle_guess/hits 等字段内存占用归零，单请求处理更快',
        14 => '【非正片占位流程保持不变】MD5 AdPlaceholderEngine + PlaceholderTsGenerator (黑屏静音TS)，广告段等时长占位不删除，保证进度条/解码器不中断，确保无广告无插播无不雅内容输出',
        15 => '【回归测试通过】7/7 平台 mock 断言 100%：Youku/Tencent/Iqiyi/Mgtv/Bilibili-Bangumi/Bilibili-UGC/Generic 的 base_title + episode_num 双正确 + 轻负担字段全空；PHP lint 核心 3 文件 0 语法错误',
      ),
    ),
    'v5.11.0' =>
    array (
      'date' => '2026-08-15',
      'title' => '【核心逻辑彻底重构：链接→官方页面识别→资源站精准搜索→AI+MD5非正片占位(不中断)】',
      'changes' =>
      array (
        0 => '【P0 新主链路】解析流程改为四步串联：1) 官方视频页面爬取 2) 元数据识别 3) 资源站搜索+集数/分剧名定位 4) AI+MD5 非正片占位替换（不再剔除广告段，保证进度条/播放器不中断）',
        1 => '【官方页面深度识别】OfficialReplaceManager::extractRichMetaFromHtml：从 youku/iqiyi/tencent 等 v_show / v_play 页面抽取 description、og:type、video:series、meta keywords 等字段，生成 base_title_guess(剧名) / episode_num(集次) / episode_subtitle(分剧名) 三维搜索词',
        2 => '修复 PCRE2 正则编译错误：\\u{3000} → \\x{3000}（PCRE2 不支持 \\u 单码点写法，统一用 \\x{xxxx}）',
        3 => '【资源站搜索匹配 + 集数定位升级 v2】findEpisodeUrl 改为双重匹配：分剧名(字幕)相似度匹配优先 + 集次匹配兜底；similar_text 做中文归一化比较（去空格/标点），subtitle_score ≥65 即锁定，解决资源站名 "九门 第2集 张启山和吴老狗达成合作" 的场景（老流程纯集次可能被集序错位干扰）',
        4 => '【AI + MD5 非正片占位引擎 全新上线】新建 gz/Md5AdPlaceholderEngine.php：Phase1=URL 关键词/路径/时长异常/连续等时长/前后 10% 段聚类 4 条规则；Phase2=并发 curl_range 0-256KB 下载每段算 MD5 + 同 host 重复率≥70% 判为非正片 + 本地 json DB 广告指纹黑/白名单；最终输出 playlist 不剔除段而是替换 URI',
        5 => '【Placeholder TS 零依赖生成器】新建 gz/PlaceholderTsGenerator.php：纯 PHP 手写 H264 Baseline QCIF(176x144) 全黑 SPS+PPS+IDR NALU + ADTS 头 + AAC-LC 单声道 44.1kHz 静音帧；封装 MPEG-TS PAT/PMT/PES/PTS/DTS/PCR 全部字段，每个 TS 包严格 188 字节对齐，mod188=0 通过',
        6 => '【本地代理占位 action】mx.php 新增 action=placeholder_ts&d=2.3：按 d 参数秒数输出对应长度的黑屏静音 TS；Cache-Control: max-age=604800 强缓存；CORS 全开；HTTP_STATUS 204 OPTIONS 预检',
        7 => '【核心 glue 层】xt/server.php 新增 runMd5PlaceholderPass()：读取 AdFilter::getSnapshot()（新增 public 方法，暴露 segments/global_tags/ext_key） → 构造 playlist 跑 Md5AdPlaceholderEngine → 再把 AdFilter 已判定 is_ad=true 的段强制转 placeholder（防止漏网）→ 生成保留所有段数、EXTINF 时长完全不变、广告段 URI 指向 mx.php?action=placeholder_ts 的最终 m3u8',
        8 => '【官解/官替双通道接入】parseVideoByOfficialChannel / parseVideoByReplaceChannel 都已接入 runMd5PlaceholderPass；AdFilter 输出的 clean_content 作为 fallback（占位引擎异常时回退到旧版去广告行为，绝不中断返回）',
        9 => '【XT config 新增 md5_placeholder 配置组】mode=auto/download_md5/rules_only；placeholder_mode=local_proxy/data_uri；download_max_bytes=256KB；download_timeout=4s；cluster_host_threshold=0.70；支持后台嗅探设置覆盖',
        10 => '【播放器不中断保证】广告段占位的 EXTINF 时长使用原广告时长，HLS 播放器按原时长推进进度条，解决"剔除段 → TARGETDURATION 失配 → 缓冲不足/进度回跳/下一段加载失败"的老问题；连续 N 段插播广告也是等时长黑屏静音通过，解码器不中断',
        11 => '【回归测试通过】6 个核心文件 PHP lint 全过；PlaceholderTsGenerator 单 PAT 包 size%188=0；2.3s 流 31960 bytes%188=0；0.3s 流 4700 bytes%188=0；合成 m3u8 测试 9/19 广告段 100% 命中 url_kw 规则并占位；title+description 样例抽出 base_title=老九门 subtitle=张启山和吴老狗达成合作 episode=2',
        12 => '【版本升级】xt/config.php version=5.11.0 build=20260815-md5-placeholder；version.php version=v5.11.0 branch=main；',
      ),
    ),
    'v5.10.9' =>
    array (
      'date' => '2026-08-13',
      'title' => '【解析失败根因修复 + 官替优先智能识别新架构】',
      'changes' => 
      array (
        0 => '【P0 根因修复】修复 findUrlInArray 误将 original_url 字段当作视频地址的严重 Bug',
        1 => 'Bug 成因：虾米官解返回 验证失败 error JSON 中含 original_url=原始页面URL，递归搜索函数误提取为视频流地址，导致 code=200 返回假成功，实际输出优酷/腾讯原始HTML页面 URL',
        2 => '修复范围：server.php::findUrlInArray() + PerformanceOptimizer::findUrlInArray() + extractVideoUrl() + getVideoLinkFromApiEntry() 共 6 处同步加固',
        3 => '新增 isSafeVideoUrl 三层守卫：①严格不等于原始URL ②同域名必须含 .m3u8/.mp4 等视频扩展名 ③兜底递归必须排除 original_url 等 20+ 非视频字段+排除原域名匹配',
        4 => '通用兜底字段(url/play_url/data.url)从 allowProxy=true 收紧为 allowProxy=false：必须含视频扩展名，避免伪视频地址蒙混过关',
        5 => '【架构升级】默认解析通道改为官替优先(mode=replace)：资源站搜索匹配 > AI智能识别 > 去广告输出，不再依赖虾米官解加密签名',
        6 => 'sniffer.replace_api.enabled 默认从 false 改为 true，url_field 从 m3u8_url 改为 ad_skip_url（优先取已去广告代理地址）',
        7 => '新增 callOfficialReplaceDirect() 本地官替直调通道：检测到 replace_api 指向本地 mx.php official_replace 时，直接 PHP 内调 OfficialReplaceManager，比 HTTP 回环快 30-70%',
        8 => '直调流程：识别平台/提取视频ID → 爬取官方页面获取标题 → 多个资源站并发搜索 → AI+PtManager 混合智能匹配最佳集数 → mxjx/deep AI 去广告+去插播+去水印 → 输出无广告播放地址',
        9 => '调用链安全：callSingleApi 新增官替直调入口，直调失败再 fallback HTTP，两种方式返回的视频地址均已通过 isSafeVideoUrl 校验',
        10 => '官替默认配置升级：default_site 从 量子 改为 抖剧TV，match_threshold 从 75 降至 65 提高命中率，新增 360kan.com 抖剧TV官替平台(priority=1)',
        11 => 'search_sites 首位插入抖剧TV，确保最高优先级命中；平台列表从7个扩容到8个（含360kan.com）',
        12 => 'jiami 分支：核心解析逻辑（callOfficialReplaceDirect / findUrlInArray / extractVideoUrl / isSafeVideoUrl 等）采用 Base64+乱码+自解码加密，防止被恶意扫描特征',
        13 => '【部署修复】sniffer_config.php 默认通道从 official 改为 replace，官替 enabled=false→true，url_field 从 m3u8_url→ad_skip_url，update_date=2026-08-13',
        14 => '【部署修复】config.php（兜底）version 升级到 5.10.9 / build=20260813，与 sniffer_config 保持一致',
        15 => '【解析链路修复】getVideoLinkByConcurrentRace 自动计算 baseUrl 修复：CLI 或 $_SERVER[HTTP_HOST] 非法(空/末尾带点/非Host格式)时，退化到 127.0.0.1，不再生成 http://localhost./ 这种 parse_url 直接失败的域名',
        16 => 'SCRIPT_NAME 目录处理：空/根/当前目录时统一生成不带多余斜杠的 $scriptDir，保证 /mx.php?action=official_replace/info&url= 可直接 curl 请求',
        17 => '官替通道薄包装验证：concurrentRaceRequest apiList 同时带 official + replace 两条，enabled=true，url_field=ad_skip_url，_channel=replace 打标正确',
      ),
    ),
    'v5.10.8' => 
    array (
      'date' => '2026-08-10',
      'title' => '【进度条卡住问题彻底修复 v2】site_check 轻量探测 + 每站独立推进 + 前端卡住自愈】',
      'changes' => 
      array (
        0 => '【重点修复】资源站巡检 task_site_check 彻底重构：从 searchVideos(30s×3重试×5策略=最坏几十分钟/站) 改为轻量两阶段探测',
        1 => '两阶段探测：阶段1=根接口 HEAD(3s超时快速排除明显失效)，阶段2=最小搜索 ac=list&limit=1(5s超时单次无重试)，单站最坏 ≤8s',
        2 => '【重点修复】all 动作进度步长拆分：site_check 不再是单步 25% 大权重，拆成 site_check_prepare + N×site_i(每站独立) + site_check_summary，每完成 1 个站立即写盘推进总进度，彻底解决"卡在 88% 不动"',
        3 => '【重点修复】GxProgressTracker 成员变量 public：task_site_check 内可直接判断 steps[$stepKey] 是否存在，精准调用 startStep/finishStep 写盘',
        4 => '前端进度卡住自愈（v5.10.8）：GX_STATE 增加 stuck_* 检测器，percent ≥25s 不变打 warn 日志(最多3条，避免刷屏)，≥180s 不变停轮询 + 手动刷新链接 + toast 提示',
        5 => '卡住自愈分级提示：按当前步骤（site_/ai_learn/official_refresh）给出人性化说明，消除用户"卡住了"的焦虑',
        6 => 'task_id 切换时自动重置日志 & 卡住检测器：避免历史任务遗留数据干扰',
        7 => 'site_check 默认巡检数量从 max=10 收紧为 max=8（all 动作），单步超时 8s，总耗时上限 ~64s（用户仍可在后台 max 输入框调大调小）',
        8 => 'gx_probe_api_root / gx_probe_site_search 新增 CURL_IPRESOLVE_V4：避免 IPv6 DNS 解析超时拉长探测时间',
      ),
    ),
    'v5.10.7' => 
    array (
      'date' => '2026-08-10',
      'title' => '【后台自动更新进度条可视化】一键操作+进度跟踪+彩色日志】',
      'changes' => 
      array (
        0 => '后台「系统管理」分组新增「🔄 自动更新/维护」菜单项（page-autoupdate），进度条徽章标识',
        1 => '进度条 UI：渐变紫色进度条 + 条纹动画，支持 0-100% 平滑过渡，显示百分比和当前步骤',
        2 => '步骤详情卡片：按任务权重渲染每个子步骤，待执行(灰)/执行中(蓝动)/成功(绿✅)/失败(红❌) 四态彩色显示',
        3 => '彩色日志区域：info/warn/error/ok(success) 四色分级显示，支持自动滚动开关 + 手动滚动',
        4 => '操作面板：action 下拉(all/check/migrate/official_refresh/ai_learn/site_check/status/reset_key)、max 数量、force 强制',
        5 => '按钮：开始执行 / 刷新进度 / 停止轮询 / 重置密钥 / 手动清理旧进度 五合一操作面板',
        6 => 'gx.php 进度跟踪升级：新增 GxProgressTracker 类，通过 gx/.gx_progress.json 记录步骤权重/百分比/日志/耗时',
        7 => 'gx.php 异步执行：gx_launch_async() 支持 exec() CLI 后台启动，禁用时回退到同步执行不阻塞 HTTP',
        8 => 'gx_execute.php 安全启动接口：HMAC-SHA256 签名 + 时间戳 ±24h，action 白名单校验，支持 exec()/fsockopen 双通道启动',
        9 => 'gx_progress.php 进度查询接口：返回当前任务进度 + 历史执行记录，支持后台轮询（1.2s/次，自动停止）',
        10 => '【重点修复】签名生成改为服务端 gx_token.php：彻底解决 Web Crypto API 需 HTTPS 安全上下文（HTTP/IP 访问不可用）、以及手写 JS HMAC 与 PHP 不一致的问题',
        11 => '安全增强：mxadmin.php 不再将 gx_secret 明文暴露到前端 JS，仅输出 __GX_SECRET_READY__ 标志，签名统一由 gx_token.php 服务端生成',
        12 => 'gx_execute.php 修复：HTTP_HOST 含端口时不再重复拼接 SERVER_PORT，max=null 字符串/JSON null/空/PHP null 四态全部统一识别为 null',
        13 => '浏览器端测试验证：action=status 任务 0s 完成，进度条→100%，步骤详情✅，彩色日志3条正确追加，轮询自动停止',
        14 => '后端集成测试通过：gx_token.php 生成签名 → gx_execute.php 校验通过 → 任务启动success:true → gx_progress.php 读取 percent=100%',
      ),
    ),
    'v5.10.6' => 
    array (
      'date' => '2026-08-10',
      'title' => '【gx.php 线上错误修复 v2】check/migrate/official_refresh 三大任务修复】',
      'changes' => 
      array (
        0 => '修复 check 任务 exec() 被禁用报错：禁用时改为 tokenizer 软校验（token_get_all），避免 Call to undefined function exec()',
        1 => '语法校验兼容：function_exists(exec) → php -l / token_get_all 双通道，线上 disable_functions 环境零报错',
        2 => '修复 migrate 任务：DataMigration 实际方法名是 migrateAll，新增按优先级尝试 migrateAll → runAll → migrate → run；无方法时走手动 pipeline(migrateDomainRules+migrateResourceSites+migrateOfficialPlatforms 等)',
        3 => '修复 official_refresh default_site=量子 纠正为抖剧TV：新增 ensureDoujuDefault() 自动写入/更新抖剧资源站/官替平台，default_site/search_sites 首位强制抖剧TV',
        4 => '官替阈值纠正：若 match_threshold 过高(>75) 自动降到推荐 65，enabled=true 默认开启',
        5 => '官替抽检 searchAllSites 不存在时 3 级回退：DbResourceSiteManager::searchAllSites → OfficialReplaceManager::searchInSites → 直接遍历前3个资源站searchVideos 逐个试',
        6 => '官替抽检新增 spot_sample：返回前3个命中视频样例(name/site/remarks)，便于排查空结果',
        7 => '官替平台 count 8个：含抖剧TV平台（360kan.com/抖剧TV 根源来源 priority=1）',
      ),
    ),
    'v5.10.5' => 
    array (
      'date' => '2026-08-09',
      'title' => '【新增 gx.php 全局定时/自动更新调度中心】',
      'changes' => 
      array (
        0 => '新增 gx.php v1.0：全局调度中心（CLI+Web 双模式，一键全部 or 单项）',
        1 => 'CLI 子命令：all / status / check / migrate / ai_learn / ai_cleanup / official_refresh / site_check / rule_check / reset_key',
        2 => 'Web 模式：自动生成随机32位密钥 gx/.gx_secret.php，访问 ?key=密钥&action=动作',
        3 => '互斥锁机制 (flock+pid)：防止 crontab 重叠运行 DB 死锁',
        4 => '分模块异常隔离：一个模块挂不影响其它模块',
        5 => '结构化日志 gx/gx_run.log + 最后一次执行摘要 gx/.gx_last_run.php',
        6 => 'task_check 健康检查：本地版本、核心文件存在性、语法 spot-check、目录可写、PHP版本',
        7 => 'task_migrate：DataMigration 自动升级 DB schema',
        8 => 'task_ai_learn：调用 AiAutoLearner::run()（force 忽略间隔，立即学）',
        9 => 'task_ai_cleanup：调用 AiAutoLearner::cleanupStaleRules() 清理失效规则域名',
        10 => 'task_official_refresh：官替配置重载 + 缓存清理 + 搜索抽检（随机热门词）',
        11 => 'task_site_check：资源站随机API健康巡检（返回OK/FAIL/cost_ms/videos数）',
        12 => 'task_rule_check：随机抽样 gz/rules_*.php 域名做 HTTPS HEAD 健康检查',
        13 => '兼容修复：DbResourceSiteManager curl_close 在 PHP 8.0+ 已废弃改为 null',
      ),
    ),
    'v5.10.4' => 
    array (
      'date' => '2026-08-09',
      'title' => '【官替识别大优化 v2】全链路修复识别不准、漏配、搜索无结果问题】',
      'changes' => 
      array (
        0 => 'resolve() 短链重定向解析：b23.tv/微信跳转/快手短链自动走 resolveRedirectChain 最多5跳重定向',
        1 => 'resolve() URL 规范化：去锚点、无前缀补 https、//scheme 自动补全',
        2 => '新增 detectPlatformFuzzy() 模糊平台识别：移动站子域名(m.v.qq/m.iqiyi/m.mgtv/m.bilibili/360kan/film.qq) 全部命中',
        3 => '360kan.com 域名自动识别为抖剧TV 官替平台（根源来源）',
        4 => 'extractVideoId() 大扩展：芒果 /b/xxxx、爱奇艺 aid/albumId、youku show_id、搜狐 vod/shtml id、PPTV programId、抖剧TV vod/detail/id',
        5 => '移动站URL 2次尝试 extract：extract 失败时自动 guessMobileUrl 换成 m.*.com URL 重试',
        6 => 'httpGet() 升级：自动伪造 Referer/Origin（按域名推导）、br brotli 解压、sec-ch-ua 伪造、403/500/429 重试、3xx带body放行',
        7 => 'httpGetMobile() 2次重试 + 最终回退增强版httpGet',
        8 => 'cleanTitle() 强解码：&#中文实体/html_entity_decode/\\uXXXX(\\u5e86这类) Unicode 转义 精准解码',
        9 => 'cleanTitle() 强清洗：平台名后缀25+种/画质/分辨率/预告/花絮/MV/纪录片/直播/演唱会/广告 等过滤',
        10 => 'DbResourceSiteManager searchVideos() 5策略搜索：ac=list→videolist→detail→list+t=1,2,3,4→list+h=24（苹果CMS10最通用 ac=list 排第一）',
        11 => '关键词变体搜索（含空格/下划线/去空格/去集季）+ JSONP callback 包装自动去除',
        12 => 'findBestMatch() 阈值动态三级：主阈值60 / fallbackThreshold(60-18=42) / hardFloorThreshold(35) 避免漏配',
        13 => 'findBestMatch() 新增年份+演员交叉验证（命中演员+6/人，最多18分；年份相同+12，±1年+4，差太远-8）',
        14 => 'findBestMatch() 非正片排除正则 50+种：从"解说/预告/片花/速看/混剪/Reaction/MV/花絮/直播回放/饭制/二创/广告/纪录片"到具体"定档预告/超长花絮/生日直播/红毯盛典/发布会/番外/彩蛋"',
        15 => '基础匹配多关键词变体取最高分（base_title/title/parsed.base_title/pure_title 逐个试）',
        16 => '验证脚本 22/22 100% 通过：13平台识别 + 4抖剧TV搜索 + 3官替配置 + 4cleanTitle',
      ),
    ),
    'v5.10.3' => 
    array (
      'date' => '2026-08-10',
      'title' => '【官替升级】新增抖剧TV 默认官替资源站 + 官替优先度 1-2000+】',
      'changes' => 
      array (
        0 => '新增官替资源站：抖剧TV（采集接口 https://www.douju.tv/api.php/provide/vod/，根源来源 www.360kan.com）',
        1 => '设置抖剧TV 为默认官替资源站（default_site=抖剧TV，官替 search_sites 数组第1位=抖剧TV）',
        2 => '抖剧TV 资源站 priority=1（数字越小越优先，1-2000+ 支持任意数字）',
        3 => '抖剧TV 官替平台 priority=1，与腾讯/爱奇艺/优酷/芒果/B站 同为最优先梯队，但在 search_sites 固定第一',
        4 => '后台官替管理页面：新增默认官替站显示、priority 升序表格 + 彩色徽章（⭐默认官替 / 高优先 / 普通 / 低优先）',
        5 => '官替优先度编辑器：min=1 max=2000 step=1，附"数字越小越优先，1=最高"说明',
        6 => '后台官替 API 配置：新增 default_site 输入框 + priority 规则说明渐变色提示条',
        7 => 'DbOfficialReplaceManager::searchInSites() 改为优先使用 DbResourceSiteManager（含 priority ASC 排序 + 最新资源站）',
        8 => 'DbOfficialReplaceManager::getDefaultConfig() 默认 default_site=抖剧TV，search_sites 头位抖剧TV，match_threshold=70',
        9 => '官替匹配平台查询：getAllPlatforms / getPlatformByDomain 全部按 priority ASC 排序（1=最先匹配）',
        10 => '资源站列表：抖剧TV 归类 group_name=官替资源站，root_source=www.360kan.com，official_replace=1',
      ),
    ),
    'v5.10.2' => 
    array (
      'date' => '2026-08-09',
      'title' => '后台菜单完善 + 官替平台 editor 修复',
      'changes' => 
      array (
        0 => '后台 mxadmin.php 新增 AI自动学习 / CCTV直播源 菜单分组和页面',
        1 => '官替平台 editor 支持 priority 输入',
        2 => '修复 header Warning、反射私有属性访问、DataMigration rules 列白名单等问题',
      ),
    ),
    'v5.10.1' => 
    array (
      'date' => '2026-08-08',
      'title' => '新增 CCTV 直播源扩展模块',
      'changes' => 
      array (
        0 => '新增 cctv/CctvSourceManager.php：GitHub 官方源抓取解析 + 定时自动更新',
        1 => '新增 cctv/CctvSourceVerifier.php：源可用性验证',
        2 => '新增 cctv/CctvPlaylistGenerator.php：M3U8 生成 + 播放页面集成',
      ),
    ),
    'v5.10.0' => 
    array (
      'date' => '2026-08-05',
      'title' => '新增 AI 自动学习模块（苹果CMS10接口分析）',
      'changes' => 
      array (
        0 => '新增 ai_learn/MacCMS10Analyzer.php：苹果CMS10 采集接口分析 + 去除非正片内容',
        1 => '新增 ai_learn/AutoLearnEngine.php：从资源站随机抓取视频 → 广告分析 → 学习规则',
        2 => '新增 ai_learn/TaskScheduler.php：定时任务调度，确保无广告且正片',
        3 => '新增 ai_learn/AiEndpointRouter.php：AI 接口地址配置化自动切换',
      ),
    ),
    'v5.9.7' => 
    array (
      'date' => '2026-08-03',
      'title' => 'M3U8 广告过滤系统基础版本',
      'changes' => 
      array (
        0 => '核心 M3U8AdSkipper + AdFilter：M3U8 广告分析与过滤',
        1 => 'DataMigration 数据库迁移 + 官替/资源站/规则 SQLite 表',
        2 => '后台资源站管理 + 规则管理 + 数据库管理',
      ),
    ),
  ),
);
