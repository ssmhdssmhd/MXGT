<?php
/**
 * 数据库版资源站管理器
 * 负责资源站列表管理、采集接口调用、自动学习调度
 * 使用数据库存储配置，完全兼容原 ResourceSiteManager 接口
 */

require_once __DIR__ . '/Database.php';

class DbResourceSiteManager {
    private $db;
    private $lastHttpError = '';
    private $proxyManager = null;
    private $useProxyOnFirstTry = true;

    public function __construct() {
        $this->db = Database::getInstance();
        $this->ensureTables();
    }

    public function setProxyManager($proxyManager) {
        $this->proxyManager = $proxyManager;
    }

    public function setUseProxyOnFirstTry($use) {
        $this->useProxyOnFirstTry = (bool)$use;
    }

    private function ensureTables() {
        if (!$this->db->tableExists('resource_sites')) {
            $this->db->initTables();
        }
        if (!$this->db->tableExists('sys_config')) {
            $this->db->initTables();
        }
    }

    private function parseSiteRow($row) {
        if (!$row) return null;
        $site = $row;
        if (!empty($site['config'])) {
            $config = json_decode($site['config'], true);
            if (is_array($config)) {
                $site = array_merge($config, $site);
            }
        }
        unset($site['config']);
        // 保证返回的站点始终带多地址字段 api_urls（兼容旧数据：仅 api_url 时回退为单地址）
        $site['api_urls'] = $this->normalizeApiUrls($site);
        if (empty($site['api_url']) && !empty($site['api_urls'])) {
            $site['api_url'] = $site['api_urls'][0];
        }
        return $site;
    }

    /**
     * 规范化多地址：接受 api_urls 数组或 api_url 字符串，去重、过滤空值，确保 https?:// 开头
     */
    private function normalizeApiUrls($siteData) {
        $urls = [];
        if (!empty($siteData['api_urls']) && is_array($siteData['api_urls'])) {
            $urls = $siteData['api_urls'];
        } elseif (!empty($siteData['api_url'])) {
            $urls = [$siteData['api_url']];
        }
        $result = [];
        foreach ($urls as $u) {
            $u = trim((string)$u);
            if ($u === '' || !preg_match('#^https?://#i', $u)) continue;
            if (in_array($u, $result)) continue;
            $result[] = $u;
        }
        return $result;
    }

    /**
     * 解析出待尝试的地址列表：传入 site 数组取 api_urls，传入字符串视为单地址
     */
    private function resolveApiUrls($apiUrlOrSite) {
        if (is_array($apiUrlOrSite)) {
            return $this->normalizeApiUrls($apiUrlOrSite);
        }
        return $this->normalizeApiUrls(['api_url' => $apiUrlOrSite]);
    }

    private function prepareSiteData($siteData) {
        $coreFields = ['name', 'site_url', 'api_url', 'type', 'status', 'priority', 'note', 'last_check_time', 'last_check_status', 'response_time'];
        $coreData = [];
        $extraConfig = [];

        // 多地址 api_urls 规范化后存入 config；第一个地址同步为主地址 api_url
        if (isset($siteData['api_urls']) || isset($siteData['api_url'])) {
            $urls = $this->normalizeApiUrls($siteData);
            $extraConfig['api_urls'] = $urls;
            if (!empty($urls)) {
                $siteData['api_url'] = $urls[0];
            } elseif (!isset($siteData['api_url'])) {
                $siteData['api_url'] = '';
            }
        }

        foreach ($siteData as $key => $value) {
            if (in_array($key, $coreFields)) {
                $coreData[$key] = $value;
            } else {
                $extraConfig[$key] = $value;
            }
        }

        if (!empty($extraConfig)) {
            $coreData['config'] = json_encode($extraConfig, JSON_UNESCAPED_UNICODE);
        }

        return $coreData;
    }

    public function getAllSites($includePaused = false) {
        $sql = 'SELECT * FROM resource_sites';
        $params = [];
        if (!$includePaused) {
            $sql .= ' WHERE status = :status';
            $params[':status'] = 'active';
        }
        $sql .= ' ORDER BY priority ASC';

        $rows = $this->db->query($sql, $params);
        $sites = [];
        foreach ($rows as $row) {
            $sites[] = $this->parseSiteRow($row);
        }
        return $sites;
    }

    public function getSiteByName($name) {
        $nameTrim = trim((string)$name);
        // 1. 精确匹配
        $row = $this->db->queryOne('SELECT * FROM resource_sites WHERE name = ?', [$nameTrim]);
        if ($row) return $this->parseSiteRow($row);
        // 2. 忽略大小写匹配（兜底：名字前后空格/大小写不一致）
        $row = $this->db->queryOne('SELECT * FROM resource_sites WHERE LOWER(TRIM(name)) = LOWER(?) LIMIT 1', [$nameTrim]);
        if ($row) return $this->parseSiteRow($row);
        return null;
    }

    public function getSiteById($id) {
        $row = $this->db->queryOne('SELECT * FROM resource_sites WHERE id = ?', [$id]);
        return $this->parseSiteRow($row);
    }

    public function getSitesByDomain($domain) {
        $allSites = $this->getAllSites(true);
        $result = [];
        foreach ($allSites as $site) {
            $siteDomain = parse_url($site['site_url'] ?? '', PHP_URL_HOST);
            $domains = [];
            foreach ($site['api_urls'] ?? [] as $u) {
                $h = parse_url($u, PHP_URL_HOST);
                if ($h) $domains[] = $h;
            }
            if ($siteDomain) $domains[] = $siteDomain;
            foreach ($domains as $d) {
                if ($d && stripos($domain, $d) !== false) {
                    $result[] = $site;
                    break;
                }
            }
        }
        return $result;
    }

    public function addSite($siteData) {
        $urls = $this->normalizeApiUrls($siteData);
        $site = array_merge([
            'name' => '',
            'site_url' => '',
            'api_url' => '',
            'type' => 'maccms',
            'status' => 'active',
            'note' => '',
            'priority' => 100
        ], $siteData);
        if (!empty($urls)) {
            $site['api_url'] = $urls[0];
            $site['api_urls'] = $urls;
        }

        if (empty($site['name']) || empty($urls)) {
            return ['success' => false, 'message' => '名称和采集接口不能为空'];
        }

        $exists = $this->getSiteByName($site['name']);
        if ($exists) {
            return ['success' => false, 'message' => '资源站名称已存在'];
        }

        $data = $this->prepareSiteData($site);
        $this->db->insert('resource_sites', $data);

        return ['success' => true, 'message' => '添加成功'];
    }

    public function updateSite($name, $siteData) {
        $exists = $this->getSiteByName($name);
        if (!$exists) {
            return ['success' => false, 'message' => '资源站不存在'];
        }

        if (isset($siteData['name'])) {
            unset($siteData['name']);
        }

        // 只传 api_url 时保留原有 api_urls，把新主地址排在最前
        if (isset($siteData['api_url']) && !isset($siteData['api_urls'])) {
            $existing = $this->normalizeApiUrls($exists);
            $newMain = trim((string)$siteData['api_url']);
            $merged = [];
            if ($newMain !== '') $merged[] = $newMain;
            foreach ($existing as $u) {
                if ($u !== $newMain) $merged[] = $u;
            }
            $siteData['api_urls'] = $merged;
        }

        $data = $this->prepareSiteData($siteData);
        if (empty($data)) {
            return ['success' => true, 'message' => '更新成功'];
        }

        $this->db->update('resource_sites', $data, 'name = ?', [$name]);
        return ['success' => true, 'message' => '更新成功'];
    }

    public function updateSiteById($id, $siteData) {
        $exists = $this->getSiteById($id);
        if (!$exists) {
            return ['success' => false, 'message' => '资源站不存在'];
        }

        if (isset($siteData['id'])) {
            unset($siteData['id']);
        }

        // 只传 api_url 时保留原有 api_urls，把新主地址排在最前
        if (isset($siteData['api_url']) && !isset($siteData['api_urls'])) {
            $existing = $this->normalizeApiUrls($exists);
            $newMain = trim((string)$siteData['api_url']);
            $merged = [];
            if ($newMain !== '') $merged[] = $newMain;
            foreach ($existing as $u) {
                if ($u !== $newMain) $merged[] = $u;
            }
            $siteData['api_urls'] = $merged;
        }

        $data = $this->prepareSiteData($siteData);
        if (empty($data)) {
            return ['success' => true, 'message' => '更新成功'];
        }

        $this->db->update('resource_sites', $data, 'id = ?', [$id]);
        return ['success' => true, 'message' => '更新成功'];
    }

    public function deleteSite($name) {
        $nameTrim = trim((string)$name);
        $exists = $this->getSiteByName($nameTrim);
        if (!$exists) {
            return ['success' => false, 'message' => '资源站不存在: ' . $nameTrim];
        }
        // 按主键 id 删除最可靠（不会因为 name 的大小写/空格差异删错或删不到）
        $id = intval($exists['id'] ?? 0);
        if ($id <= 0) {
            // 回退：按名字删（TRIM+LOWER 条件兜底）
            $sql = sprintf('DELETE FROM %s WHERE LOWER(TRIM(name)) = LOWER(?)', 'resource_sites');
            $ok = $this->db->execute($sql, [$nameTrim]);
            // 再次查询确认是否已消失
            $still = $this->getSiteByName($nameTrim);
            if ($still) return ['success' => false, 'message' => '删除失败（回退按名删除未生效）'];
            return ['success' => true, 'message' => '删除成功'];
        }
        $this->db->delete('resource_sites', 'id = ?', [$id]);
        $still = $this->getSiteById($id);
        if ($still) return ['success' => false, 'message' => '删除失败（数据库删除未生效）'];
        return ['success' => true, 'message' => '删除成功（ID=' . $id . '）'];
    }

    public function deleteSiteById($id) {
        $exists = $this->getSiteById($id);
        if (!$exists) {
            return ['success' => false, 'message' => '资源站不存在'];
        }
        $idInt = intval($id);
        $this->db->delete('resource_sites', 'id = ?', [$idInt]);
        $still = $this->getSiteById($idInt);
        if ($still) return ['success' => false, 'message' => '删除失败（数据库删除未生效）'];
        return ['success' => true, 'message' => '删除成功'];
    }

    public function updateSiteStatus($siteName, $status, $note = '') {
        $exists = $this->getSiteByName($siteName);
        if (!$exists) {
            return false;
        }

        $data = ['status' => $status];
        if ($note) {
            $data['note'] = $note;
        }

        $this->db->update('resource_sites', $data, 'name = ?', [$siteName]);
        return true;
    }

    public function updateSiteStatusById($id, $status, $note = '') {
        $exists = $this->getSiteById($id);
        if (!$exists) {
            return false;
        }

        $data = ['status' => $status];
        if ($note) {
            $data['note'] = $note;
        }

        $this->db->update('resource_sites', $data, 'id = ?', [$id]);
        return true;
    }

    public function checkSiteHealth($site, $timeout = 8) {
        $urls = $this->resolveApiUrls($site);
        if (empty($urls)) {
            return ['healthy' => false, 'message' => '无API地址', 'response_time' => 0];
        }

        $allResults = [];
        $best = null;
        foreach ($urls as $url) {
            $startTime = microtime(true);
            $result = $this->fetchVideos($url, 1, 1, $timeout);
            $responseTime = round((microtime(true) - $startTime) * 1000, 0);

            $health = [
                'url' => $url,
                'healthy' => $result['success'] && !empty($result['videos']),
                'message' => $result['success'] ? '正常' : ($result['message'] ?? '未知错误'),
                'video_count' => $result['success'] ? count($result['videos']) : 0,
                'response_time' => $responseTime
            ];
            $allResults[] = $health;
            if ($health['healthy'] && ($best === null || $responseTime < $best['response_time'])) {
                $best = $health;
            }
        }

        if ($best !== null) {
            $best['urls_checked'] = $allResults;
            $best['active_url'] = $best['url'];
            return $best;
        }

        $first = $allResults[0] ?? [];
        return [
            'healthy' => false,
            'message' => $first['message'] ?? '未知错误',
            'urls_checked' => $allResults,
            'active_url' => $urls[0] ?? '',
            'response_time' => $first['response_time'] ?? 0
        ];
    }

    /**
     * 多地址测速：逐个检测 url 列表，返回按健康+速度排序的结果，供前端选择最优源
     */
    public function testApiUrls($urls, $timeout = 6) {
        $results = [];
        $healthyCount = 0;
        foreach ($urls as $url) {
            $url = trim((string)$url);
            if ($url === '') continue;
            if (!preg_match('#^https?://#i', $url)) {
                $results[] = ['url' => $url, 'healthy' => false, 'message' => '地址格式无效', 'response_time' => 0, 'video_count' => 0];
                continue;
            }
            $startTime = microtime(true);
            $result = $this->fetchVideos($url, 1, 1, $timeout);
            $responseTime = round((microtime(true) - $startTime) * 1000, 0);
            $ok = $result['success'] && !empty($result['videos']);
            if ($ok) $healthyCount++;
            $results[] = [
                'url' => $url,
                'healthy' => $ok,
                'message' => $ok ? '正常' : ($result['message'] ?? '未知错误'),
                'video_count' => $ok ? count($result['videos']) : 0,
                'response_time' => $responseTime
            ];
        }

        usort($results, function ($a, $b) {
            if (($a['healthy'] ?? false) !== ($b['healthy'] ?? false)) {
                return ($a['healthy'] ?? false) ? -1 : 1;
            }
            return ($a['response_time'] ?? 0) - ($b['response_time'] ?? 0);
        });

        return [
            'success' => true,
            'healthy' => $healthyCount,
            'total' => count($results),
            'results' => $results
        ];
    }

    public function batchCheckHealth($maxSites = null, $timeoutPerSite = 8) {
        $sites = $this->getAllSites(true);
        $results = [];
        $activeCount = 0;
        $failedCount = 0;

        foreach ($sites as $idx => $site) {
            if ($maxSites !== null && $idx >= $maxSites) break;

            $health = $this->checkSiteHealth($site, $timeoutPerSite);
            $results[] = [
                'name' => $site['name'],
                'api_url' => $health['active_url'] ?? ($site['api_url'] ?? ''),
                'api_urls' => $site['api_urls'] ?? [],
                'urls_checked' => $health['urls_checked'] ?? [],
                'status' => $site['status'] ?? 'active',
                'priority' => $site['priority'] ?? 100,
                'healthy' => $health['healthy'],
                'message' => $health['message'],
                'response_time' => $health['response_time']
            ];

            if ($health['healthy']) {
                $activeCount++;
            } else {
                $failedCount++;
            }
        }

        return [
            'total' => count($results),
            'healthy' => $activeCount,
            'failed' => $failedCount,
            'results' => $results
        ];
    }

    private function isDomainFailureError($error) {
        if (!$error) return false;
        $error = strtolower($error);
        $patterns = [
            'could not resolve',
            'dns',
            'name lookup',
            'no such host',
            'host not found',
            'ssl_error_syscall',
            'connection refused',
            'connection timed out',
            'failed to connect',
            'operation timed out',
            'timed out after',
        ];
        foreach ($patterns as $pattern) {
            if (strpos($error, strtolower($pattern)) !== false) {
                return true;
            }
        }
        return false;
    }

    /**
     * 多地址自动切换：依次尝试 api_urls（或单地址），第一个成功即返回，失败自动换下一个源
     */
    public function fetchVideos($apiUrl, $page = 1, $limit = 20, $timeout = 30) {
        $urls = $this->resolveApiUrls($apiUrl);
        if (empty($urls)) {
            return ['success' => false, 'message' => '无API地址'];
        }

        $errors = [];
        $attempted = 0;
        foreach ($urls as $idx => $url) {
            $attempted++;
            $result = $this->fetchVideosSingle($url, $page, $limit, $timeout);
            if ($result['success']) {
                $result['page'] = $page;
                $result['api_url'] = $url;
                if ($attempted > 1) {
                    $result['switched_source'] = true;
                    $result['message'] = '已自动切换至备用源';
                }
                return $result;
            }
            $errors[] = $url . ' → ' . ($result['message'] ?? '失败');
        }

        return [
            'success' => false,
            'message' => '所有采集地址均失败（' . $attempted . '个）: ' . implode(' | ', array_slice($errors, 0, 6)),
            'errors' => array_slice($errors, 0, 6)
        ];
    }

    public function fetchVideosSingle($apiUrl, $page = 1, $limit = 20, $timeout = 30) {
        $urlsToTry = $this->generateApiUrlVariants($apiUrl);
        $fetchStrategies = [
            ['ac' => 'detail'],
            ['ac' => 'videolist'],
        ];

        $lastError = '';
        $isDomainFailure = false;
        $dnsHost = '';
        foreach ($urlsToTry as $tryUrl) {
            foreach ($fetchStrategies as $strategy) {
                $params = array_merge($strategy, [
                    'pg' => intval($page),
                    'limit' => intval($limit)
                ]);
                $url = $this->buildApiUrl($tryUrl, $params);

                $response = $this->httpGet($url, $timeout);
                if ($response === false) {
                    $lastError = $this->lastHttpError ?? '未知错误';
                    if ($this->isDomainFailureError($lastError)) {
                        $isDomainFailure = true;
                        $parsed = parse_url($tryUrl);
                        $dnsHost = $parsed['host'] ?? '';
                    }
                    continue;
                }

                $data = json_decode($response, true);
                if (!$data) {
                    $lastError = '解析JSON失败';
                    continue;
                }

                $result = $this->parseVideoList($data);
                if ($result['success']) {
                    $result['page'] = $page;
                    return $result;
                }
                $lastError = $result['message'] ?? '无视频数据';
            }
        }

        if ($isDomainFailure) {
            $hostDisplay = $dnsHost ? '（域名: ' . $dnsHost . '）' : '';
            return [
                'success' => false,
                'message' => '资源站API无法连接' . $hostDisplay . '，该资源站可能已失效，请更换其他资源站',
                'error_type' => 'dns_failure',
                'dns_host' => $dnsHost
            ];
        }

        return ['success' => false, 'message' => '获取失败: ' . $lastError];
    }

    /**
     * 多地址自动切换搜索：依次尝试 api_urls，失败自动换下一个源
     */
    public function searchVideos($apiUrl, $keyword, $page = 1, $limit = 20, $timeout = 30) {
        $urls = $this->resolveApiUrls($apiUrl);
        if (empty($urls)) {
            return ['success' => false, 'message' => '无API地址'];
        }

        $errors = [];
        $attempted = 0;
        foreach ($urls as $url) {
            $attempted++;
            $result = $this->searchVideosSingle($url, $keyword, $page, $limit, $timeout);
            if ($result['success']) {
                $result['page'] = $page;
                $result['api_url'] = $url;
                if ($attempted > 1) {
                    $result['switched_source'] = true;
                    $result['message'] = '已自动切换至备用源';
                }
                return $result;
            }
            $errors[] = $url . ' → ' . ($result['message'] ?? '失败');
        }

        return [
            'success' => false,
            'message' => '所有采集地址均失败（' . $attempted . '个）: ' . implode(' | ', array_slice($errors, 0, 6)),
            'errors' => array_slice($errors, 0, 6)
        ];
    }

    public function searchVideosSingle($apiUrl, $keyword, $page = 1, $limit = 20, $timeout = 30) {
        $urlsToTry = $this->generateApiUrlVariants($apiUrl);
        // 策略顺序（苹果CMS10 官方兼容顺序：ac=list 最通用，其次 detail/videolist；t=类型过滤也常出结果）
        $searchStrategies = [
            ['ac' => 'list', 'wd' => $keyword],                          // 最通用，苹果CMS10 100% 支持
            ['ac' => 'videolist', 'wd' => $keyword],                     // 旧版本 / 兼容
            ['ac' => 'detail', 'wd' => $keyword],                        // 部分小站
            ['ac' => 'list', 'wd' => $keyword, 't' => '1,2,3,4'],       // t=类型过滤（部分站对 wd 单独返回空需 t）
            ['ac' => 'list', 'wd' => $keyword, 'h' => '24'],            // h=24h 热片
        ];

        // 如果 keyword 是纯数字ID（苹果CMS详情搜索），多试一个 ac=detail&ids=
        if (preg_match('/^\d{1,8}$/', $keyword)) {
            array_unshift($searchStrategies, ['ac' => 'detail', 'ids' => $keyword]);
        }
        // 如果 keyword 含 空格：也尝试把空格换成 下划线 / 或 去掉空格再查
        $keywordVariants = [$keyword];
        if (strpos($keyword, ' ') !== false) {
            $keywordVariants[] = str_replace(' ', '', $keyword);
            $keywordVariants[] = str_replace(' ', '_', $keyword);
            $keywordVariants[] = preg_replace('/\s*第\s*\d+\s*[集季期部章回]\s*/u', '', $keyword);
        }
        $keywordVariants = array_values(array_unique(array_filter($keywordVariants)));

        $lastError = '';
        $isDomainFailure = false;
        $dnsHost = '';
        foreach ($keywordVariants as $variantKeyword) {
            foreach ($urlsToTry as $tryUrl) {
                foreach ($searchStrategies as $strategy) {
                    // 替换策略里的 wd 为当前 variant
                    $curStrategy = $strategy;
                    if (isset($curStrategy['wd'])) {
                        $curStrategy['wd'] = $variantKeyword;
                    }
                    $params = array_merge($curStrategy, [
                        'pg' => intval($page),
                        'limit' => intval($limit)
                    ]);
                    $url = $this->buildApiUrl($tryUrl, $params);

                    $response = $this->httpGet($url, $timeout);
                    if ($response === false) {
                        $lastError = $this->lastHttpError ?? '未知错误';
                        if ($this->isDomainFailureError($lastError)) {
                            $isDomainFailure = true;
                            $parsed = parse_url($tryUrl);
                            $dnsHost = $parsed['host'] ?? '';
                        }
                        continue;
                    }

                    $data = json_decode($response, true);
                    if (!$data) {
                        // 尝试去 JSONP 包装再解码
                        $cleaned = preg_replace('/^\w+\s*\(/', '', $response);
                        $cleaned = preg_replace('/\)\s*;?\s*$/', '', $cleaned);
                        $data = json_decode($cleaned, true);
                        if (!$data) {
                            $lastError = '解析JSON失败';
                            continue;
                        }
                    }

                    $code = $data['code'] ?? $data['status'] ?? 0;
                    $hasList = !empty($data['list']) || !empty($data['data']);
                    if ($code != 200 && $code != 1 && !empty($data['msg']) && !$hasList) {
                        $lastError = $data['msg'] ?? '接口返回错误';
                        continue;
                    }

                    $result = $this->parseVideoList($data);
                    if ($result['success']) {
                        $result['page'] = $page;
                        $result['search_variant'] = $variantKeyword;
                        $result['strategy'] = json_encode($curStrategy, JSON_UNESCAPED_UNICODE);
                        return $result;
                    }
                    $lastError = $result['message'] ?? '搜索无结果';
                }
            }
        }

        if ($isDomainFailure) {
            $hostDisplay = $dnsHost ? '（域名: ' . $dnsHost . '）' : '';
            return [
                'success' => false,
                'message' => '资源站API无法连接' . $hostDisplay . '，该资源站可能已失效，请更换其他资源站',
                'error_type' => 'dns_failure',
                'dns_host' => $dnsHost
            ];
        }

        return ['success' => false, 'message' => '搜索失败: ' . $lastError];
    }

    public function generateApiUrlVariants($apiUrl) {
        $urls = [$apiUrl];
        $parsed = parse_url($apiUrl);
        if (!$parsed) return $urls;

        $path = $parsed['path'] ?? '';

        if (preg_match('#^(.*)/from/[^/]+/?$#', $path, $m)) {
            $newPath = $m[1];
            $newUrl = $this->buildUrl($parsed, $newPath);
            if ($newUrl !== $apiUrl) {
                $urls[] = $newUrl;
            }
        }

        $existingParams = [];
        if (!empty($parsed['query'])) {
            parse_str($parsed['query'], $existingParams);
            if (isset($existingParams['ac'])) {
                unset($existingParams['ac']);
                $newUrl = $this->buildUrl($parsed, $path, $existingParams);
                if ($newUrl !== $apiUrl && !in_array($newUrl, $urls)) {
                    $urls[] = $newUrl;
                }
            }
        }

        $host = $parsed['host'] ?? '';
        if (strpos($host, 'www.') === 0) {
            $newHost = substr($host, 4);
            $newParsed = $parsed;
            $newParsed['host'] = $newHost;
            $newUrl = $this->buildUrl($newParsed, $path, $existingParams);
            if (!in_array($newUrl, $urls)) {
                $urls[] = $newUrl;
            }
        }

        return array_unique($urls);
    }

    private function buildUrl($parsed, $path = null, $params = null) {
        $scheme = $parsed['scheme'] ?? 'https';
        $host = $parsed['host'] ?? '';
        $port = isset($parsed['port']) ? ':' . $parsed['port'] : '';
        $path = $path ?? ($parsed['path'] ?? '/');

        $url = $scheme . '://' . $host . $port . $path;
        if (!empty($params)) {
            $url .= '?' . http_build_query($params);
        } elseif (!empty($parsed['query']) && $params === null) {
            $url .= '?' . $parsed['query'];
        }

        return $url;
    }

    public function parseVideoList($data) {
        $videos = [];
        $list = $data['list'] ?? $data['data'] ?? [];

        if (empty($list)) {
            return ['success' => false, 'message' => '无视频数据'];
        }

        foreach ($list as $item) {
            $vodPlayUrl = $item['vod_play_url'] ?? $item['play_url'] ?? '';
            $vodPlayFrom = $item['vod_play_from'] ?? $item['play_from'] ?? '';
            $vodName = $item['vod_name'] ?? $item['name'] ?? '';
            $vodId = $item['vod_id'] ?? $item['id'] ?? 0;
            $vodPic = $item['vod_pic'] ?? $item['pic'] ?? '';
            $vodRemarks = $item['vod_remarks'] ?? $item['remarks'] ?? '';

            if (empty($vodPlayUrl)) continue;

            $allUrls = $this->extractAllPlayUrls($vodPlayUrl, $vodPlayFrom);
            $m3u8Urls = array_filter($allUrls, function($u) {
                return stripos($u['url'] ?? '', '.m3u8') !== false;
            });
            $m3u8Urls = array_values($m3u8Urls);

            if (!empty($m3u8Urls)) {
                $videos[] = [
                    'id' => $vodId,
                    'name' => $vodName,
                    'pic' => $vodPic,
                    'remarks' => $vodRemarks,
                    'urls' => $m3u8Urls,
                    'first_url' => $m3u8Urls[0]['url'] ?? '',
                    'raw_play_url' => $vodPlayUrl,
                    'play_from' => $vodPlayFrom,
                    'has_non_m3u8' => count($allUrls) > count($m3u8Urls),
                    'all_urls_count' => count($allUrls)
                ];
            } elseif (!empty($allUrls)) {
                $videos[] = [
                    'id' => $vodId,
                    'name' => $vodName,
                    'pic' => $vodPic,
                    'remarks' => $vodRemarks,
                    'urls' => $allUrls,
                    'first_url' => $allUrls[0]['url'] ?? '',
                    'raw_play_url' => $vodPlayUrl,
                    'play_from' => $vodPlayFrom,
                    'is_non_m3u8' => true,
                    'all_urls_count' => count($allUrls)
                ];
            }
        }

        if (empty($videos)) {
            return ['success' => false, 'message' => '无有效视频'];
        }

        return [
            'success' => true,
            'total' => $data['total'] ?? $data['page']['pagecount'] ?? count($videos),
            'pagecount' => $data['pagecount'] ?? $data['page']['pagecount'] ?? 1,
            'videos' => $videos
        ];
    }

    private function extractAllPlayUrls($playUrl, $playFrom = '') {
        if (empty($playUrl)) return [];

        $fromGroups = [];
        if (!empty($playFrom) && strpos($playFrom, '$$$') !== false && strpos($playUrl, '$$$') !== false) {
            $fromParts = explode('$$$', $playFrom);
            $urlParts = explode('$$$', $playUrl);
            $count = min(count($fromParts), count($urlParts));
            for ($i = 0; $i < $count; $i++) {
                $fromGroups[] = [
                    'from' => trim($fromParts[$i]),
                    'url' => trim($urlParts[$i])
                ];
            }
        } else {
            $fromGroups[] = [
                'from' => trim($playFrom) ?: 'default',
                'url' => $playUrl
            ];
        }

        $allUrls = [];
        $seen = [];

        foreach ($fromGroups as $group) {
            $fromName = $group['from'];
            $groupUrlStr = $group['url'];
            $groupUrls = $this->parsePlayUrlGroup($groupUrlStr, $fromName);

            foreach ($groupUrls as $u) {
                $cleanUrl = preg_replace('/#.*$/', '', $u['url']);
                $key = $fromName . '|' . $cleanUrl;
                if (!isset($seen[$key])) {
                    $seen[$key] = true;
                    $u['play_from'] = $fromName;
                    $allUrls[] = $u;
                }
            }
        }

        return $allUrls;
    }

    private function parsePlayUrlGroup($playUrl, $fromName = '') {
        $urls = [];
        if (empty($playUrl)) return $urls;

        $lines = [];
        if (strpos($playUrl, "\r\n") !== false || strpos($playUrl, "\n") !== false) {
            $lines = preg_split('/\r\n|\n/', $playUrl);
        } else {
            $lines = [$playUrl];
        }

        $lineNum = 0;
        foreach ($lines as $line) {
            $line = trim($line);
            if (empty($line)) continue;
            $lineNum++;

            if (strpos($line, '$') !== false) {
                $parts = explode('$', $line);
                $urlIndices = [];
                foreach ($parts as $i => $part) {
                    $part = trim($part);
                    if (preg_match('/^https?:\/\//i', $part)) {
                        $urlIndices[] = $i;
                    }
                }
                foreach ($urlIndices as $idx) {
                    $url = $parts[$idx];
                    $name = '';

                    if ($idx > 0) {
                        $prev = trim($parts[$idx - 1] ?? '');
                        if (!preg_match('/^https?:\/\//i', $prev) && !empty($prev)) {
                            if (preg_match('/^\d+$/', $prev)) {
                                $name = '第' . $prev . '集';
                            } else {
                                $name = $prev;
                            }
                        }
                    }

                    if (empty($name)) {
                        $epFromUrl = $this->extractEpisodeFromUrl($url);
                        if ($epFromUrl) {
                            if (preg_match('/^\d+$/', $epFromUrl)) {
                                $name = '第' . $epFromUrl . '集';
                            } else {
                                $name = $epFromUrl;
                            }
                        }
                    }

                    if (empty($name)) {
                        $frag = parse_url($url, PHP_URL_FRAGMENT);
                        if ($frag && preg_match('/^\d+$/', $frag)) {
                            $name = '第' . $frag . '集';
                        }
                    }

                    if (empty($name)) {
                        $num = count($urls) + 1;
                        $name = '第' . $num . '集';
                    }

                    $urls[] = ['name' => $name, 'url' => $url];
                }
            } else {
                if (preg_match('/^https?:\/\//i', $line)) {
                    $name = $fromName ? ($fromName . ' 第' . $lineNum . '集') : ('第' . $lineNum . '集');
                    $urls[] = ['name' => $name, 'url' => $line];
                }
            }
        }

        return $urls;
    }

    public function searchAllSites($keyword, $maxSites = 5, $limitPerSite = 10) {
        $sites = $this->getAllSites(false);
        $sites = array_slice($sites, 0, $maxSites);

        $results = [];
        $totalVideos = 0;
        $blockedSites = [];

        foreach ($sites as $site) {
            $searchResult = $this->searchVideos($site, $keyword, 1, $limitPerSite);
            if ($searchResult['success']) {
                foreach ($searchResult['videos'] as &$video) {
                    $video['site_name'] = $site['name'];
                    $video['site_url'] = $site['site_url'] ?? '';
                }
                unset($video);
                $results[] = [
                    'site' => $site['name'],
                    'site_url' => $site['site_url'] ?? '',
                    'count' => count($searchResult['videos']),
                    'videos' => $searchResult['videos'],
                    'auto_blocked' => false
                ];
                $totalVideos += count($searchResult['videos']);
            } else {
                // 自动屏蔽不能搜索的资源站：搜索失败的站点自动置为暂停（屏蔽），退出活跃列表
                $blockReason = trim((string)($searchResult['message'] ?? '搜索失败'));
                $blockReason = mb_substr($blockReason, 0, 80);
                $this->updateSiteStatus($site['name'], 'paused', '自动屏蔽·不可搜索: ' . $blockReason);
                $blockedSites[] = $site['name'];
                $results[] = [
                    'site' => $site['name'],
                    'site_url' => $site['site_url'] ?? '',
                    'count' => 0,
                    'videos' => [],
                    'error' => $searchResult['message'],
                    'auto_blocked' => true
                ];
            }
        }

        return [
            'success' => true,
            'keyword' => $keyword,
            'sites_searched' => count($sites),
            'total_videos' => $totalVideos,
            'auto_blocked' => count($blockedSites),
            'blocked_sites' => $blockedSites,
            'results' => $results
        ];
    }

    private function extractEpisodeFromUrl($url) {
        if (preg_match('/#(.+)$/i', $url, $m)) {
            $frag = trim($m[1]);
            if (!empty($frag) && preg_match('/第[^\s]+/u', $frag)) {
                return $frag;
            }
            if (!empty($frag) && mb_strlen($frag) <= 20) {
                return $frag;
            }
        }
        return null;
    }

    public function getAutoLearnConfig() {
        $row = $this->db->queryOne('SELECT config_value FROM sys_config WHERE config_key = ?', ['auto_learn']);
        if ($row && !empty($row['config_value'])) {
            $config = json_decode($row['config_value'], true);
            if (is_array($config)) {
                return $config;
            }
        }
        return [
            'enabled' => true,
            'interval_days' => 3,
            'videos_per_site' => 5,
            'max_sites_per_run' => 5,
            'min_segments' => 50,
            'max_ad_percentage' => 90
        ];
    }

    public function setAutoLearnConfig($config) {
        $default = $this->getAutoLearnConfig();
        $mergedConfig = array_merge($default, $config);
        $configJson = json_encode($mergedConfig, JSON_UNESCAPED_UNICODE);

        $exists = $this->db->queryOne('SELECT id FROM sys_config WHERE config_key = ?', ['auto_learn']);
        if ($exists) {
            $this->db->update('sys_config', ['config_value' => $configJson], 'config_key = ?', ['auto_learn']);
        } else {
            $this->db->insert('sys_config', [
                'config_key' => 'auto_learn',
                'config_value' => $configJson,
                'description' => '自动学习配置'
            ]);
        }

        return ['success' => true, 'message' => '配置已更新'];
    }

    public function saveAutoLearnConfig($config) {
        return $this->setAutoLearnConfig($config);
    }

    public function runAutoLearn($domainRuleManager, $options = []) {
        try {
            $config = $this->getAutoLearnConfig();
            if (empty($config['enabled'])) {
                return ['success' => false, 'message' => '自动学习未启用'];
            }

            $maxSites = $options['max_sites'] ?? $config['max_sites_per_run'] ?? 5;
            $maxSites = min($maxSites, 10);
            $videosPerSite = $options['videos_per_site'] ?? $config['videos_per_site'] ?? 5;
            $videosPerSite = min($videosPerSite, 10);
            $minSegments = $config['min_segments'] ?? 50;
            $maxAdPercentage = $config['max_ad_percentage'] ?? 90;
            $keyword = $options['keyword'] ?? '';

            $sites = $this->getAllSites(false);
            $sites = array_slice($sites, 0, $maxSites);

            $results = [];
            $totalLearned = 0;
            $totalFailed = 0;

            foreach ($sites as $site) {
                $siteResult = [
                    'site' => $site['name'],
                    'videos_checked' => 0,
                    'videos_learned' => 0,
                    'videos_failed' => 0,
                    'domains' => []
                ];

                try {
                    if (!empty($keyword)) {
                        $fetchResult = $this->searchVideos($site, $keyword, 1, $videosPerSite * 3);
                    } else {
                        $fetchResult = $this->fetchVideos($site, 1, $videosPerSite * 3);
                    }

                    if (!$fetchResult['success']) {
                        $siteResult['error'] = $fetchResult['message'];
                        $results[] = $siteResult;
                        continue;
                    }

                    $videos = $fetchResult['videos'] ?? [];
                    $learnedCount = 0;

                    foreach ($videos as $video) {
                        if ($learnedCount >= $videosPerSite) break;

                        $siteResult['videos_checked']++;

                        $videoUrl = $video['url'] ?? $video['first_url'] ?? '';
                        if (empty($videoUrl)) continue;

                        $learnResult = $this->learnFromVideoUrl($videoUrl, $domainRuleManager, [
                            'min_segments' => $minSegments,
                            'max_ad_percentage' => $maxAdPercentage
                        ]);

                        if ($learnResult['success']) {
                            $videoDomain = $learnResult['domain'] ?? '';
                            if ($videoDomain) {
                                if (!isset($siteResult['domains'][$videoDomain])) {
                                    $siteResult['domains'][$videoDomain] = 0;
                                }
                                $siteResult['domains'][$videoDomain]++;
                            }
                            $siteResult['videos_learned']++;
                            $totalLearned++;
                            $learnedCount++;
                        } else {
                            $siteResult['videos_failed']++;
                            $totalFailed++;
                        }

                        unset($learnResult);
                        if (function_exists('gc_collect_cycles')) {
                            gc_collect_cycles();
                        }
                    }

                    unset($videos);
                    unset($fetchResult);
                    if (function_exists('gc_collect_cycles')) {
                        gc_collect_cycles();
                    }
                } catch (Throwable $e) {
                    $siteResult['error'] = $e->getMessage();
                    $siteResult['videos_failed']++;
                    $totalFailed++;
                }

                $results[] = $siteResult;
            }

            $this->setLastLearnTime();

            return [
                'success' => true,
                'message' => '自动学习完成',
                'keyword' => $keyword,
                'sites_processed' => count($sites),
                'total_learned' => $totalLearned,
                'total_failed' => $totalFailed,
                'details' => $results
            ];
        } catch (Throwable $e) {
            return [
                'success' => false,
                'message' => '自动学习异常: ' . $e->getMessage(),
                'error_file' => basename($e->getFile()),
                'error_line' => $e->getLine()
            ];
        }
    }

    public function learnFromVideoUrl($videoUrl, $domainRuleManager, $options = []) {
        $minSegments = $options['min_segments'] ?? 50;
        $maxAdPercentage = $options['max_ad_percentage'] ?? 90;

        try {
            $parsedUrl = parse_url($videoUrl);
            $videoDomain = $parsedUrl['host'] ?? '';
            if (empty($videoDomain)) {
                return ['success' => false, 'message' => '无法解析域名'];
            }

            if (function_exists('memory_get_usage')) {
                $currentLimit = @ini_get('memory_limit');
                $currentLimitBytes = $this->return_bytes($currentLimit);
                if ($currentLimitBytes < 256 * 1024 * 1024) {
                    @ini_set('memory_limit', '256M');
                }
            }

            $mediaUrl = $this->resolveMasterPlaylist($videoUrl);

            if (!class_exists('M3U8Parser')) {
                require_once __DIR__ . '/../src/M3U8Parser.php';
            }
            $parser = new M3U8Parser();
            $parser->setMaxSegments(3000);
            $playlist = $parser->parse($mediaUrl);
            unset($parser);

            if (empty($playlist['segments']) || count($playlist['segments']) < $minSegments) {
                unset($playlist);
                return ['success' => false, 'message' => '片段数不足', 'domain' => $videoDomain];
            }

            if (!class_exists('EnhancedAdRuleEngine')) {
                require_once __DIR__ . '/../gz/EnhancedAdRuleEngine.php';
            }
            $engine = new EnhancedAdRuleEngine([
                'checkDiscontinuity' => true,
                'checkRepetitiveDuration' => true
            ]);
            $engine->setDomain($videoDomain);
            $analysis = $engine->analyzeAllSegments($playlist['segments']);
            unset($engine);

            $segmentsCount = count($playlist['segments']);
            unset($playlist);

            $adPercentage = $analysis['totalCount'] > 0
                ? ($analysis['adCount'] / $analysis['totalCount'] * 100)
                : 0;

            if ($adPercentage >= $maxAdPercentage) {
                unset($analysis);
                return ['success' => false, 'message' => '广告占比过高', 'domain' => $videoDomain, 'ad_percentage' => $adPercentage];
            }

            $domainResult = $domainRuleManager->learnFromAnalysis($videoDomain, $analysis);

            // ===== 保存广告特征码到数据库 =====
            $adSignature = new DbAdSignature($this->db);
            $signatures = [];
            if (!empty($analysis['durationDistribution'])) {
                foreach ($analysis['durationDistribution'] as $dur => $count) {
                    if ((float)$dur < 3.0 && $count > 1) {
                        $signatures[] = ['type' => 'duration', 'value' => (string)$dur, 'weight' => min(50, $count * 5), 'confidence' => min(80, $count * 10)];
                    }
                }
            }
            if (!empty($analysis['adClusters'])) {
                foreach ($analysis['adClusters'] as $cluster) {
                    if (!empty($cluster['avgDuration']) && $cluster['avgDuration'] < 3.0) {
                        $signatures[] = ['type' => 'duration', 'value' => (string)round($cluster['avgDuration'], 2), 'weight' => 40, 'confidence' => 60];
                    }
                }
            }
            if (!empty($analysis['sequenceJumps'])) {
                foreach ($analysis['sequenceJumps'] as $jump) {
                    if (!empty($jump['jump']) && $jump['jump'] > 1) {
                        $signatures[] = ['type' => 'sequence', 'value' => (string)$jump['jump'], 'weight' => 35, 'confidence' => 50];
                    }
                }
            }
            if ($analysis['discontinuityCount'] > 0) {
                $signatures[] = ['type' => 'discontinuity', 'value' => 'true', 'weight' => 30, 'confidence' => 50];
            }
            $adSignature->addSignatures($videoDomain, $signatures);

            // ===== 记录域名分析统计 =====
            $domainStats = new DbDomainAnalysisStats($this->db);
            $domainStats->recordLearn($videoDomain);
            $domainStats->recordAnalyze($videoDomain, $analysis['totalCount'] ?? 0, $analysis['adCount'] ?? 0, $adPercentage);

            unset($analysis);

            if ($domainResult) {
                return [
                    'success' => true,
                    'domain' => $videoDomain,
                    'segments_count' => $segmentsCount,
                    'ad_count' => 0,
                    'ad_percentage' => $adPercentage,
                    'rule_updated' => $domainResult
                ];
            } else {
                return ['success' => false, 'message' => '规则学习失败', 'domain' => $videoDomain];
            }
        } catch (Throwable $e) {
            if (isset($playlist)) unset($playlist);
            if (isset($engine)) unset($engine);
            if (isset($analysis)) unset($analysis);
            $msg = $e->getMessage();
            if (strpos($msg, 'memory') !== false || strpos($msg, 'Allowed memory') !== false) {
                return ['success' => false, 'message' => '内存不足，视频过大', 'domain' => $videoDomain ?? ''];
            }
            return ['success' => false, 'message' => $msg];
        }
    }

    private function return_bytes($val) {
        $val = trim($val);
        $last = strtolower($val[strlen($val)-1]);
        $val = (int)$val;
        switch($last) {
            case 'g': $val *= 1024;
            case 'm': $val *= 1024;
            case 'k': $val *= 1024;
        }
        return $val;
    }

    private function resolveMasterPlaylist($url) {
        if (!class_exists('M3U8Parser')) {
            require_once __DIR__ . '/../src/M3U8Parser.php';
        }
        $parser = new M3U8Parser();
        try {
            $playlist = $parser->parse($url);
            if (!empty($playlist['isMaster']) && !empty($playlist['variants'])) {
                $firstVariant = $playlist['variants'][0]['uri'] ?? '';
                if ($firstVariant) {
                    $parsedUrl = parse_url($url);
                    $baseUrl = $parsedUrl['scheme'] . '://' . $parsedUrl['host'];
                    if (isset($parsedUrl['port'])) {
                        $baseUrl .= ':' . $parsedUrl['port'];
                    }
                    $pathDir = dirname($parsedUrl['path'] ?? '');
                    $pathDir = $pathDir === '.' ? '' : $pathDir;
                    if (strpos($firstVariant, '/') === 0) {
                        return $baseUrl . $firstVariant;
                    } else {
                        return $baseUrl . $pathDir . '/' . $firstVariant;
                    }
                }
            }
        } catch (Throwable $e) {
        }
        return $url;
    }

    public function getLastLearnTime() {
        $row = $this->db->queryOne('SELECT config_value FROM sys_config WHERE config_key = ?', ['auto_learn_state']);
        if ($row && !empty($row['config_value'])) {
            $state = json_decode($row['config_value'], true);
            return $state['last_learn_time'] ?? null;
        }
        return null;
    }

    public function setLastLearnTime() {
        $state = [
            'last_learn_time' => date('Y-m-d H:i:s'),
            'last_learn_timestamp' => time()
        ];
        $stateJson = json_encode($state, JSON_UNESCAPED_UNICODE);

        $exists = $this->db->queryOne('SELECT id FROM sys_config WHERE config_key = ?', ['auto_learn_state']);
        if ($exists) {
            $this->db->update('sys_config', ['config_value' => $stateJson], 'config_key = ?', ['auto_learn_state']);
        } else {
            $this->db->insert('sys_config', [
                'config_key' => 'auto_learn_state',
                'config_value' => $stateJson,
                'description' => '自动学习状态'
            ]);
        }
    }

    public function shouldAutoLearn() {
        $config = $this->getAutoLearnConfig();
        if (empty($config['enabled'])) return false;

        $lastTime = $this->getLastLearnTime();
        if (!$lastTime) return true;

        $intervalDays = $config['interval_days'] ?? 3;
        $lastTimestamp = strtotime($lastTime);
        return (time() - $lastTimestamp) >= ($intervalDays * 86400);
    }

    private function buildApiUrl($baseUrl, $params = []) {
        $parsed = parse_url($baseUrl);
        if ($parsed === false) {
            return $baseUrl;
        }

        $existingParams = [];
        if (!empty($parsed['query'])) {
            parse_str($parsed['query'], $existingParams);
        }

        $mergedParams = array_merge($existingParams, $params);

        $scheme = $parsed['scheme'] ?? 'https';
        $host = $parsed['host'] ?? '';
        $port = isset($parsed['port']) ? ':' . $parsed['port'] : '';
        $path = $parsed['path'] ?? '/';

        $url = $scheme . '://' . $host . $port . $path;
        if (!empty($mergedParams)) {
            $url .= '?' . http_build_query($mergedParams);
        }

        return $url;
    }

    private function httpGet($url, $timeout = 30, $retry = 3) {
        $lastError = '';
        $userAgents = [
            'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
            'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15',
            'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1'
        ];

        $proxyMgr = $this->proxyManager;
        if ($proxyMgr === null) {
            $proxyFile = __DIR__ . '/../proxy/ProxyManager.php';
            if (file_exists($proxyFile)) {
                require_once $proxyFile;
                $proxyMgr = new ProxyManager();
            }
        }

        $sslVersions = [null, CURL_SSLVERSION_TLSv1_2, CURL_SSLVERSION_TLSv1_1, CURL_SSLVERSION_SSLv2 | CURL_SSLVERSION_SSLv3];

        for ($attempt = 0; $attempt <= $retry; $attempt++) {
            foreach ($sslVersions as $sslIdx => $sslVersion) {
                $ch = curl_init();
                curl_setopt($ch, CURLOPT_URL, $url);
                curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
                curl_setopt($ch, CURLOPT_TIMEOUT, $timeout);
                curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 8);
                curl_setopt($ch, CURLOPT_FOLLOWLOCATION, true);
                curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
                curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, false);
                if ($sslVersion !== null) {
                    curl_setopt($ch, CURLOPT_SSLVERSION, $sslVersion);
                }
                curl_setopt($ch, CURLOPT_ENCODING, 'gzip,deflate');
                curl_setopt($ch, CURLOPT_USERAGENT, $userAgents[$attempt % count($userAgents)]);
                curl_setopt($ch, CURLOPT_HTTPHEADER, [
                    'Accept: application/json, text/plain, */*',
                    'Accept-Language: zh-CN,zh;q=0.9,en;q=0.8',
                    'Referer: ' . (parse_url($url, PHP_URL_SCHEME) . '://' . parse_url($url, PHP_URL_HOST) . '/')
                ]);
                curl_setopt($ch, CURLOPT_IPRESOLVE, CURL_IPRESOLVE_V4);

                $currentProxy = null;
                if ($proxyMgr && $proxyMgr->isEnabled() && ($this->useProxyOnFirstTry || $attempt > 0)) {
                    $currentProxy = $proxyMgr->applyProxyToCurl($ch);
                }

                $startTime = microtime(true);
                $response = curl_exec($ch);
                $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
                $error = curl_error($ch);
                $responseTime = round((microtime(true) - $startTime) * 1000, 2);
                if (PHP_VERSION_ID < 80000) { @curl_close($ch); } else { $ch = null; }

                if ($currentProxy) {
                    if ($httpCode >= 200 && $httpCode < 300 && $response !== false) {
                        $proxyMgr->markProxySuccess($currentProxy['id'], $responseTime);
                        return $response;
                    } else {
                        $proxyMgr->markProxyFailed($currentProxy['id']);
                    }
                }

                if ($httpCode >= 200 && $httpCode < 300 && $response !== false) {
                    return $response;
                }

                $lastError = $error ? $error : ('HTTP ' . $httpCode);

                $isSslError = $error && (
                    stripos($error, 'SSL') !== false ||
                    stripos($error, 'tls') !== false ||
                    stripos($error, 'certificate') !== false
                );

                if (!$isSslError) {
                    break;
                }
            }

            $isRetryable = $lastError && (
                strpos($lastError, 'Could not resolve') !== false ||
                strpos($lastError, 'Connection timed out') !== false ||
                strpos($lastError, 'Failed to connect') !== false ||
                strpos($lastError, 'Operation timed out') !== false ||
                stripos($lastError, 'SSL') !== false ||
                stripos($lastError, 'tls') !== false
            ) || ($httpCode >= 500 || $httpCode == 429);

            if ($attempt < $retry && $isRetryable) {
                usleep(300000 + $attempt * 200000);
            }
        }

        $this->lastHttpError = $lastError;
        return false;
    }
}
