<?php
/**
 * 资源站规则自动获取（输入资源站链接 → 自动抓取 → 自动分析 → 自动配置规则）
 *
 * 功能：
 *  1. 输入资源站采集接口 URL（如 https://www.example.com/api.php/provide/vod/），自动拉取
 *     视频列表（AppleCMS10 ac=detail/videolist 策略），逐个解析视频 M3U8 深度分析广告特征；
 *  2. 自动汇总 5 类规则建议（时长 / 不连续 / 序列 / 文件名 / 关键词），写入
 *     resource_site_rules 表（site_name=资源站名，domain=视频所属域名）；
 *  3. 「一键同步全部资源站」与「每 2 小时自动执行」：遍历启用资源站的 api_urls 自动抓取
 *     分析更新规则，保证去广告正确率随资源站变化持续自动优化；
 *  4. 自动触发：后台任意请求时检测距上次同步是否 ≥2 小时，是则后台异步触发同步
 *     （mx.php?action=resource_rules/sync），不影响正常响应。
 */

class ResourceRuleAutoFetcher
{
    /** @var PDO|null 数据库连接（写入 resource_site_rules） */
    private $db = null;

    /** @var object|null 资源站管理器（DbResourceSiteManager / ResourceSiteManager） */
    private $siteManager = null;

    /** @var string|null 状态文件路径 */
    private $stateFile = null;

    /** @var array|null 内存状态 */
    private $state = null;

    /** 自动同步间隔（小时） */
    const INTERVAL_HOURS = 2;

    /** 每次最多深度分析的视频数 */
    const MAX_ANALYZE_VIDEOS = 5;

    /** 规则至少命中条数才写入（避免单次偶然误报） */
    const MIN_RULE_HITS = 2;

    /** 广告关键词词典（用于从广告片段 URI 提炼关键词规则） */
    const AD_KEYWORDS = [
        'ad', 'ads', 'advert', 'advertisement', 'commercial', 'promo', 'promotion',
        'sponsor', 'pre-roll', 'mid-roll', 'post-roll', 'preroll', 'midroll', 'postroll',
        'adinsert', 'ad_', '_ad', 'adv', 'adp', 'banner', 'trailer', 'teaser',
        '广告', '插播', '贴片', '片头', '片尾', '推广', '宣传', '预告',
    ];

    public function __construct($db = null, $siteManager = null)
    {
        $this->db = $db;
        $this->siteManager = $siteManager;
        $this->stateFile = dirname(__DIR__) . '/gz/.resource_rules_state.php';
    }

    // ============ 状态管理 ============

    public function setStateFile($file)
    {
        $this->stateFile = $file;
        $this->state = null;
    }

    private function loadState()
    {
        if ($this->state !== null) {
            return $this->state;
        }
        $default = [
            'enabled' => true,
            'interval_hours' => self::INTERVAL_HOURS,
            'last_run_time' => null,
            'last_run_summary' => null,
        ];
        if ($this->stateFile && file_exists($this->stateFile)) {
            $data = @include $this->stateFile;
            if (is_array($data)) {
                $this->state = array_merge($default, $data);
                return $this->state;
            }
        }
        $this->state = $default;
        return $this->state;
    }

    private function saveState()
    {
        if (!$this->stateFile) {
            return;
        }
        $content = "<?php\n/** 资源站规则自动同步状态（由 gz/ResourceRuleAutoFetcher.php 维护，勿手动编辑） */\n"
            . 'return ' . var_export($this->state, true) . ";\n";
        @file_put_contents($this->stateFile, $content);
    }

    public function getConfig()
    {
        $s = $this->loadState();
        return [
            'enabled' => !empty($s['enabled']),
            'interval_hours' => (int)($s['interval_hours'] ?? self::INTERVAL_HOURS),
            'last_run_time' => $s['last_run_time'] ?? null,
            'last_run_summary' => $s['last_run_summary'] ?? null,
        ];
    }

    public function saveConfig($data)
    {
        $s = $this->loadState();
        if (isset($data['enabled'])) {
            $s['enabled'] = (int)$data['enabled'] ? true : false;
        }
        if (isset($data['interval_hours'])) {
            $s['interval_hours'] = max(1, min(24, intval($data['interval_hours'])));
        }
        $this->state = $s;
        $this->saveState();
        return $this->getConfig();
    }

    // ============ 抓取视频列表 ============

    /**
     * 从资源站 API 拉取视频列表
     * @return array ['success'=>bool, 'list'=>[['name'=>..., 'play_url'=>...]], 'message'=>...]
     */
    private function fetchVideoList($apiUrl, $limit = 30, $timeout = 15)
    {
        if ($this->siteManager && method_exists($this->siteManager, 'fetchVideos')) {
            $result = $this->siteManager->fetchVideos($apiUrl, 1, $limit, $timeout);
            if (empty($result['success']) || empty($result['list'])) {
                return ['success' => false, 'message' => $result['message'] ?? '无视频数据'];
            }
            $list = [];
            foreach ($result['list'] as $v) {
                $name = $v['vod_name'] ?? ($v['name'] ?? '');
                $playUrl = $v['vod_play_url'] ?? ($v['play_url'] ?? '');
                $m3u8 = self::extractM3u8FromPlayUrl($playUrl);
                if ($m3u8 !== '') {
                    $list[] = ['name' => $name, 'play_url' => $m3u8];
                }
            }
            if (empty($list)) {
                return ['success' => false, 'message' => '视频列表无可用 m3u8 播放地址'];
            }
            return ['success' => true, 'list' => $list, 'total' => count($list)];
        }

        // 无管理器：直接 HTTP 抓取（ac=detail 策略）
        $data = $this->httpGetJson($apiUrl, ['ac' => 'detail', 'pg' => 1, 'limit' => $limit], $timeout);
        if (!$data || empty($data['list'])) {
            return ['success' => false, 'message' => '接口无返回或非视频JSON'];
        }
        $list = [];
        foreach ($data['list'] as $v) {
            $m3u8 = self::extractM3u8FromPlayUrl($v['vod_play_url'] ?? '');
            if ($m3u8 !== '') {
                $list[] = ['name' => $v['vod_name'] ?? '', 'play_url' => $m3u8];
            }
        }
        if (empty($list)) {
            return ['success' => false, 'message' => '视频列表无可用 m3u8 播放地址'];
        }
        return ['success' => true, 'list' => $list, 'total' => count($list)];
    }

    /** 从 AppleCMS vod_play_url 提取首个 m3u8 地址（格式：播放器$url#播放器$url） */
    public static function extractM3u8FromPlayUrl($playUrl)
    {
        $playUrl = trim((string)$playUrl);
        if ($playUrl === '') {
            return '';
        }
        // 先按 # 切分多个线路
        foreach (explode('#', $playUrl) as $line) {
            // 按 $ 切分 播放器名|地址
            $parts = explode('$', $line);
            $url = end($parts);
            $url = trim($url);
            if ($url !== '' && (stripos($url, '.m3u8') !== false || stripos($url, 'index.m3u8') !== false)) {
                if (preg_match('/https?:\/\/[^\s]+/i', $url, $m)) {
                    return $m[0];
                }
            }
        }
        return '';
    }

    private function httpGetJson($apiUrl, $params, $timeout)
    {
        $sep = strpos($apiUrl, '?') === false ? '?' : '&';
        $url = $apiUrl . $sep . http_build_query($params);
        $ch = @curl_init();
        if ($ch === false) {
            return null;
        }
        @curl_setopt_array($ch, [
            CURLOPT_URL => $url,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_CONNECTTIMEOUT => (int)$timeout,
            CURLOPT_TIMEOUT => (int)$timeout,
            CURLOPT_FOLLOWLOCATION => true,
            CURLOPT_MAXREDIRS => 3,
            CURLOPT_SSL_VERIFYPEER => false,
            CURLOPT_SSL_VERIFYHOST => false,
            CURLOPT_USERAGENT => 'Mozilla/5.0 (MXGT ResourceRuleAutoFetcher)',
        ]);
        $body = @curl_exec($ch);
        @curl_close($ch);
        if ($body === false || trim($body) === '') {
            return null;
        }
        $data = json_decode($body, true);
        if (!is_array($data)) {
            // 兼容 JSONP 包装
            if (preg_match('/^\s*[\w.]+\((.*)\)\s*;?\s*$/s', trim($body), $m)) {
                $data = json_decode($m[1], true);
            }
        }
        return is_array($data) ? $data : null;
    }

    // ============ 单视频分析 ============

    /**
     * 深度分析一个视频 M3U8，提炼规则建议
     * @return array ['success', 'domain', 'segments_count', 'ad_count', 'ad_percentage',
     *                'suggestions' => ['keyword'=>[k=>hits], 'filename'=>[name=>hits],
     *                                  'duration'=>[dur=>hits], 'sequence'=>[type=>hits], 'discontinuity'=>bool]]
     */
    private function analyzePlaylist($playUrl, $videoName = '', $maxExec = 25)
    {
        $startTime = microtime(true);
        try {
            $parsedUrl = parse_url($playUrl);
            $videoDomain = $parsedUrl['host'] ?? '';
            if (empty($videoDomain)) {
                return ['success' => false, 'message' => '无法解析播放地址域名'];
            }

            if (!class_exists('M3U8Parser')) {
                require_once __DIR__ . '/../src/M3U8Parser.php';
            }
            $parser = new M3U8Parser();
            $parser->setMaxSegments(800);
            $parser->setConnectTimeout(6);
            $parser->setTimeout(15);

            // 跟随 Master playlist 变体
            $mediaUrl = $playUrl;
            try {
                $first = $parser->parse($playUrl);
                if (!empty($first['isMaster']) && !empty($first['variants'])) {
                    $best = $first['variants'][0]['uri'] ?? '';
                    foreach ($first['variants'] as $v) {
                        if (($v['bandwidth'] ?? 0) > ($first['variants'][0]['bandwidth'] ?? 0)) {
                            $best = $v['uri'];
                        }
                    }
                    if ($best !== '') {
                        $mediaUrl = preg_match('/^https?:\/\//i', $best)
                            ? $best
                            : rtrim(preg_replace('#/[^/]*$#', '', $playUrl), '/') . '/' . ltrim($best, '/');
                        $playlist = $parser->parse($mediaUrl);
                    } else {
                        $playlist = $first;
                    }
                } else {
                    $playlist = $first;
                }
            } catch (Throwable $e) {
                return ['success' => false, 'message' => '解析失败: ' . $e->getMessage()];
            }

            $segments = $playlist['segments'] ?? [];
            if (count($segments) < 10) {
                return ['success' => false, 'message' => '片段数不足(' . count($segments) . ')'];
            }

            if (!class_exists('EnhancedAdRuleEngine')) {
                require_once __DIR__ . '/EnhancedAdRuleEngine.php';
            }
            $engine = new EnhancedAdRuleEngine([
                'checkDiscontinuity' => true,
                'checkRepetitiveDuration' => true,
            ]);
            $engine->setDomain($videoDomain);
            $analysis = $engine->analyzeAllSegments($segments);

            $adCount = (int)($analysis['adCount'] ?? 0);
            if ($adCount <= 0) {
                return [
                    'success' => false, 'message' => '未检测到广告',
                    'domain' => $videoDomain, 'segments_count' => count($segments),
                ];
            }

            // 配对引擎逐段结果与原始片段
            $engineResults = $analysis['segments'] ?? [];
            $suggestions = ['keyword' => [], 'filename' => [], 'duration' => [], 'sequence' => [], 'discontinuity' => false];
            $adUris = [];

            for ($i = 0; $i < count($segments); $i++) {
                $seg = $segments[$i];
                $res = $engineResults[$i] ?? null;
                $isAd = $res ? !empty($res['isAd']) : false;
                if (!$isAd) {
                    continue;
                }
                $uri = strtolower((string)($seg['uri'] ?? ''));
                $adUris[] = $uri;
                $dur = round((float)($seg['duration'] ?? 0), 2);

                // 关键词
                foreach (self::AD_KEYWORDS as $kw) {
                    $kwLow = strtolower($kw);
                    if ($kwLow !== '' && strpos($uri, $kwLow) !== false) {
                        $suggestions['keyword'][$kw] = (int)($suggestions['keyword'][$kw] ?? 0) + 1;
                    }
                }

                // 文件名特征：提取命中规则的规则名
                foreach (($res['matchedRules'] ?? []) as $rule) {
                    $name = is_array($rule) ? ($rule['name'] ?? '') : $rule;
                    if ($name !== '' && strpos($name, 'pattern') !== false) {
                        $suggestions['filename'][$name] = (int)($suggestions['filename'][$name] ?? 0) + 1;
                    }
                }

                // 时长：广告片段重复时长
                if ($dur > 0) {
                    $bucket = (string)round($dur * 2) / 2; // 0.5s 粒度
                    $suggestions['duration'][$bucket] = (int)($suggestions['duration'][$bucket] ?? 0) + 1;
                }

                // 不连续标记
                if (!empty($seg['discontinuity']) || !empty($seg['adMarkers']) || !empty($seg['cueMarkers'])) {
                    $suggestions['discontinuity'] = true;
                }
            }

            // 序列跳变：广告簇位置
            foreach (($analysis['insertionPoints'] ?? []) as $pos => $info) {
                if (is_array($info) && !empty($info['found'])) {
                    $suggestions['sequence'][$pos] = (int)($suggestions['sequence'][$pos] ?? 0) + 1;
                }
            }

            return [
                'success' => true,
                'domain' => $videoDomain,
                'video_name' => $videoName,
                'segments_count' => count($segments),
                'ad_count' => $adCount,
                'ad_percentage' => round((float)($analysis['adPercentage'] ?? 0), 2),
                'suggestions' => $suggestions,
                'elapsed_ms' => round((microtime(true) - $startTime) * 1000),
            ];
        } catch (Throwable $e) {
            return ['success' => false, 'message' => $e->getMessage(), 'elapsed_ms' => round((microtime(true) - $startTime) * 1000)];
        }
    }

    // ============ 规则写入 ============

    /**
     * 合并多个视频的建议并写入 resource_site_rules
     * @return array ['success', 'site_name', 'domain', 'analyzed', 'rules_added', 'rules_skipped', 'detail'=>[...]]
     */
    private function upsertSuggestions($siteName, $suggestions)
    {
        $added = 0;
        $skipped = 0;
        $detail = [];
        if (!$this->db) {
            return ['success' => false, 'message' => '数据库不可用'];
        }

        // 关键词规则
        foreach (($suggestions['keyword'] ?? []) as $kw => $hits) {
            if ($hits < self::MIN_RULE_HITS) continue;
            $useCn = preg_match('/[\x{4e00}-\x{9fa5}]/u', $kw) ? 1 : 0;
            if ($this->insertRule($siteName, $kw, 'keyword', $kw, $useCn, '【自动】关键词命中' . $hits . '次')) {
                $added++;
                $detail[] = '关键词规则: ' . $kw . '（命中' . $hits . '次）';
            } else {
                $skipped++;
            }
        }

        // 文件名特征规则
        foreach (($suggestions['filename'] ?? []) as $name => $hits) {
            if ($hits < self::MIN_RULE_HITS) continue;
            $pattern = $name;
            if ($this->insertRule($siteName, $name, 'filename', $pattern, 0, '【自动】文件名特征命中' . $hits . '次')) {
                $added++;
                $detail[] = '文件名规则: ' . $name . '（命中' . $hits . '次）';
            } else {
                $skipped++;
            }
        }

        // 时长规则
        foreach (($suggestions['duration'] ?? []) as $dur => $hits) {
            if ($hits < self::MIN_RULE_HITS) continue;
            if ($this->insertRule($siteName, (string)$dur, 'duration', (string)$dur, 0, '【自动】广告重复时长 ' . $dur . 's 命中' . $hits . '次')) {
                $added++;
                $detail[] = '时长规则: ' . $dur . 's（命中' . $hits . '次）';
            } else {
                $skipped++;
            }
        }

        // 序列规则
        foreach (($suggestions['sequence'] ?? []) as $pos => $hits) {
            if ($hits < self::MIN_RULE_HITS) continue;
            $posName = ['pre_roll' => '片头', 'mid_roll' => '中插', 'post_roll' => '片尾'][$pos] ?? $pos;
            if ($this->insertRule($siteName, $pos, 'sequence', $pos, 0, '【自动】' . $posName . '广告序列命中' . $hits . '次')) {
                $added++;
                $detail[] = '序列规则: ' . $posName . '（命中' . $hits . '次）';
            } else {
                $skipped++;
            }
        }

        // 不连续规则
        if (!empty($suggestions['discontinuity'])) {
            if ($this->insertRule($siteName, 'discontinuity', 'discontinuity', 'discontinuity', 0, '【自动】广告段伴随不连续标记')) {
                $added++;
                $detail[] = '不连续规则: 广告段含 EXT-X-DISCONTINUITY';
            } else {
                $skipped++;
            }
        }

        return ['success' => true, 'rules_added' => $added, 'rules_skipped' => $skipped, 'detail' => $detail];
    }

    private function insertRule($siteName, $ruleKey, $ruleType, $keyword, $useCn, $note)
    {
        try {
            // 去重：同站 + 同类型 + 同特征已存在则跳过
            $exists = $this->db->queryOne(
                'SELECT id FROM resource_site_rules WHERE site_name = ? AND rule_type = ? AND keyword = ?',
                [$siteName, $ruleType, $keyword]
            );
            if ($exists !== null) {
                return false;
            }
            $this->db->insert('resource_site_rules', [
                'site_name'      => $siteName,
                'domain'         => '',
                'rule_name'      => '【自动】' . $ruleKey,
                'rule_type'      => $ruleType,
                'keyword'        => $keyword,
                'use_cn_pattern' => $useCn,
                'ad_threshold'   => 80,
                'note'           => $note,
                'enabled'        => 1,
            ]);
            return true;
        } catch (Throwable $e) {
            return false;
        }
    }

    // ============ 对外入口 ============

    /**
     * 输入资源站链接 → 自动抓取 → 自动分析 → 自动配置规则
     * @param string $apiUrl 资源站采集接口 URL
     * @param array $options max_videos / videos_limit / site_name
     * @return array 汇总结果
     */
    public function autoFetchRules($apiUrl, $options = [])
    {
        $apiUrl = trim((string)$apiUrl);
        if ($apiUrl === '' || !preg_match('/^https?:\/\//i', $apiUrl)) {
            return ['success' => false, 'message' => '请输入合法的资源站链接（http/https）'];
        }
        $maxVideos = max(1, min(10, (int)($options['max_videos'] ?? self::MAX_ANALYZE_VIDEOS)));
        $listLimit = max(10, min(80, (int)($options['videos_limit'] ?? 30)));

        $parsed = parse_url($apiUrl);
        $host = $parsed['host'] ?? '';
        $siteName = trim((string)($options['site_name'] ?? ''));
        if ($siteName === '') {
            $siteName = preg_replace('/^www\./', '', $host);
        }

        $fetched = $this->fetchVideoList($apiUrl, $listLimit, 15);
        if (empty($fetched['success'])) {
            return ['success' => false, 'message' => '拉取视频列表失败: ' . ($fetched['message'] ?? '未知错误'), 'api_url' => $apiUrl];
        }

        $merged = ['keyword' => [], 'filename' => [], 'duration' => [], 'sequence' => [], 'discontinuity' => false];
        $analyzed = 0;
        $adVideos = 0;
        $domains = [];
        $videos = array_slice($fetched['list'], 0, $maxVideos);
        foreach ($videos as $v) {
            $res = $this->analyzePlaylist($v['play_url'], $v['name'], 25);
            if (empty($res['success'])) {
                continue;
            }
            $analyzed++;
            if (!empty($res['suggestions'])) {
                $adVideos++;
                foreach (['keyword', 'filename', 'duration', 'sequence'] as $cat) {
                    foreach (($res['suggestions'][$cat] ?? []) as $k => $hits) {
                        $merged[$cat][$k] = (int)($merged[$cat][$k] ?? 0) + (int)$hits;
                    }
                }
                if (!empty($res['suggestions']['discontinuity'])) {
                    $merged['discontinuity'] = true;
                }
            }
            if (!empty($res['domain'])) {
                $domains[$res['domain']] = true;
            }
        }

        if ($analyzed === 0) {
            return [
                'success' => false, 'message' => '拉取到视频但全部解析失败（可能需代理或非标准源）',
                'api_url' => $apiUrl, 'site_name' => $siteName, 'total' => $fetched['total'],
            ];
        }

        // 合并建议需累计命中（跨视频），写入
        $writeResult = $this->upsertSuggestions($siteName, $merged);
        $rulesAdded = (int)($writeResult['rules_added'] ?? 0);
        $detail = $writeResult['detail'] ?? [];

        return [
            'success' => true,
            'api_url' => $apiUrl,
            'host' => $host,
            'site_name' => $siteName,
            'total_videos' => $fetched['total'],
            'analyzed_videos' => $analyzed,
            'ad_videos' => $adVideos,
            'domains' => array_keys($domains),
            'rules_added' => $rulesAdded,
            'rules_skipped' => (int)($writeResult['rules_skipped'] ?? 0),
            'suggestions' => $merged,
            'detail' => array_slice($detail, 0, 30),
        ];
    }

    /**
     * 一键同步全部启用资源站：遍历 api_urls 自动抓取分析更新规则
     * @param array $options max_sites / max_videos
     * @return array 汇总
     */
    public function syncFromAllSites($options = [])
    {
        $s = $this->loadState();
        if (!$this->siteManager || !method_exists($this->siteManager, 'getAllSites')) {
            return ['success' => false, 'message' => '资源站管理器不可用'];
        }
        $maxSites = max(1, min(20, (int)($options['max_sites'] ?? 8)));
        $sites = $this->siteManager->getAllSites(false);
        if (empty($sites)) {
            return ['success' => false, 'message' => '暂无启用的资源站'];
        }

        $summary = [
            'sites_total' => count($sites),
            'sites_done' => 0,
            'sites_failed' => 0,
            'videos_analyzed' => 0,
            'rules_added' => 0,
            'details' => [],
        ];

        $count = 0;
        foreach ($sites as $site) {
            if ($count >= $maxSites) break;
            $count++;
            $name = $site['name'] ?? ($site['site_name'] ?? '');
            $apiUrls = $this->resolveSiteApiUrls($site);
            if (empty($apiUrls)) {
                $summary['sites_failed']++;
                $summary['details'][] = $name . '：无采集地址';
                continue;
            }
            $ok = false;
            foreach ($apiUrls as $apiUrl) {
                $res = $this->autoFetchRules($apiUrl, ['site_name' => $name, 'max_videos' => (int)($options['max_videos'] ?? 3)]);
                if (!empty($res['success'])) {
                    $summary['sites_done']++;
                    $summary['videos_analyzed'] += (int)$res['analyzed_videos'];
                    $summary['rules_added'] += (int)$res['rules_added'];
                    $summary['details'][] = $name . '：' . $res['analyzed_videos'] . '片/' . $res['ad_videos'] . '含广告，新增规则 ' . $res['rules_added'] . ' 条';
                    $ok = true;
                    break;
                }
            }
            if (!$ok) {
                $summary['sites_failed']++;
                $summary['details'][] = $name . '：全部采集地址失败';
            }
        }

        $s['last_run_time'] = date('Y-m-d H:i:s');
        $s['last_run_summary'] = $summary;
        $this->state = $s;
        $this->saveState();

        $summary['success'] = true;
        $summary['message'] = '同步完成：' . $summary['sites_done'] . '/' . $summary['sites_total'] . ' 站成功，新增规则 ' . $summary['rules_added'] . ' 条';
        return $summary;
    }

    /** 解析资源站的 api_urls（兼容 api_url 单地址与 api_urls 多地址） */
    private function resolveSiteApiUrls($site)
    {
        $urls = [];
        if (!empty($site['api_urls']) && is_array($site['api_urls'])) {
            foreach ($site['api_urls'] as $u) {
                $u = trim((string)$u);
                if ($u !== '') $urls[] = $u;
            }
        }
        if (empty($urls) && !empty($site['api_url'])) {
            $urls[] = trim((string)$site['api_url']);
        }
        return $urls;
    }

    // ============ 自动触发（每 2 小时） ============

    /**
     * 是否达到自动同步间隔（默认 2 小时）
     */
    public function shouldSync()
    {
        $s = $this->loadState();
        if (empty($s['enabled'])) {
            return false;
        }
        $interval = max(1, (int)($s['interval_hours'] ?? self::INTERVAL_HOURS));
        if (empty($s['last_run_time'])) {
            return true;
        }
        $lastTs = strtotime((string)$s['last_run_time']);
        return $lastTs === false || (time() - $lastTs) >= ($interval * 3600);
    }

    /**
     * 请求时自动触发（后台任意请求调用一次即可）：
     * 距上次同步 ≥2 小时 → 后台异步触发 mx.php?action=resource_rules/sync
     * @return array
     */
    public function autoTriggerIfNeeded()
    {
        if (!$this->shouldSync()) {
            return ['triggered' => false, 'reason' => 'not_due'];
        }
        $ok = $this->triggerAsyncSync();
        return ['triggered' => $ok, 'reason' => $ok ? 'async_started' : 'async_failed'];
    }

    /** 后台异步触发同步（exec / fsockopen / 短超时 curl 三策略回退） */
    private function triggerAsyncSync()
    {
        $host = $_SERVER['HTTP_HOST'] ?? '';
        if (!empty($host)) {
            $scheme = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') ? 'https' : 'http';
            $base = $scheme . '://' . $host . '/mx.php?action=resource_rules/sync';
            return $this->asyncHttpGet($base);
        }
        return false;
    }

    private function asyncHttpGet($url)
    {
        // 策略1：exec
        if (function_exists('exec') && !in_array('exec', array_map('trim', explode(',', ini_get('disable_functions') ?: '')))) {
            $phpBin = PHP_BINARY ?: 'php';
            $cmd = escapeshellarg($phpBin) . ' -r ' . escapeshellarg('$ch=curl_init(' . var_export($url, true) . ');curl_setopt_array($ch,[CURLOPT_RETURNTRANSFER=>1,CURLOPT_TIMEOUT=>1]);curl_exec($ch);');
            if (PHP_OS_FAMILY === 'Windows') {
                @pclose(@popen('start /B ' . $cmd . ' > NUL 2>&1', 'r'));
            } else {
                @exec($cmd . ' > /dev/null 2>&1 &');
            }
            return true;
        }
        // 策略2：fsockopen 非阻塞
        if (function_exists('fsockopen')) {
            $parts = parse_url($url);
            $port = isset($parts['port']) ? $parts['port'] : (($parts['scheme'] ?? '') === 'https' ? 443 : 80);
            $path = ($parts['path'] ?? '/') . (isset($parts['query']) ? '?' . $parts['query'] : '');
            $fp = @fsockopen(($parts['scheme'] ?? '') === 'https' ? 'ssl://' . $parts['host'] : $parts['host'], $port, $errno, $errstr, 1);
            if ($fp) {
                $req = "GET " . $path . " HTTP/1.1\r\nHost: " . $parts['host'] . "\r\nConnection: Close\r\n\r\n";
                fwrite($fp, $req);
                fclose($fp);
                return true;
            }
        }
        // 策略3：短超时 curl
        if (function_exists('curl_init')) {
            $ch = @curl_init();
            if ($ch !== false) {
                @curl_setopt_array($ch, [CURLOPT_URL => $url, CURLOPT_RETURNTRANSFER => true, CURLOPT_TIMEOUT => 1, CURLOPT_CONNECTTIMEOUT => 1]);
                @curl_exec($ch);
                @curl_close($ch);
                return true;
            }
        }
        return false;
    }
}
