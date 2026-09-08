<?php
/**
 * 去广告实时监控（防止误删正片）
 *
 * 功能：
 *  1. 记录每次去广告处理的关键指标（总片段/删除数/占比/守护是否触发/命中规则等），识别可疑删除；
 *  2. 支持人工反馈「误删」：被误删片段进入保护名单（后续解析自动还原），命中规则记入误报计数，
 *     实现「正确率持续自动优化」——反馈越多，重复误删越少；
 *  3. 监控版本号 = 当前应用版本 + '.' + 三位数字计数（起始 0001），重置监控数据时自动递增；
 *  4. 数据落盘 gz/monitor_data.php（与 gz/*_config.php 同风格的 PHP 数组文件）。
 */

class AdMonitor
{
    /** @var string|null 数据文件路径 */
    private static $dataFile = null;

    /** @var array|null 内存缓存 */
    private static $cache = null;

    /** 最多保留的监控记录条数 */
    const MAX_RECORDS = 200;

    /**
     * 初始化：确定数据文件并加载
     * @param string|null $rootDir 项目根目录（默认当前文件所在目录的上一级）
     * @return array
     */
    public static function init($rootDir = null)
    {
        if (self::$dataFile === null) {
            self::$dataFile = ($rootDir ?: dirname(__DIR__)) . '/gz/monitor_data.php';
        }
        if (self::$cache === null) {
            self::$cache = self::load();
        }
        return self::$cache;
    }

    public static function setDataFile($file)
    {
        self::$dataFile = $file;
        self::$cache = null;
    }

    private static function load()
    {
        $default = self::defaultData();
        if (file_exists(self::$dataFile)) {
            $data = null;
            try {
                $data = @include self::$dataFile;
            } catch (Throwable $e) {
                // 数据文件损坏（截断/并发写坏导致语法错误）：@include 会抛 ParseError，@ 无法抑制
                $data = null;
            }
            if (is_array($data)) {
                return array_merge($default, $data);
            }
            // 文件损坏/内容非数组：备份后重建默认数据，避免接口持续报「监控数据异常」
            self::repairCorruptedFile();
        }
        return $default;
    }

    /**
     * 数据文件损坏自愈：将损坏文件改名备份，重建默认监控数据并写回
     */
    private static function repairCorruptedFile()
    {
        if (!file_exists(self::$dataFile)) {
            return;
        }
        $bak = self::$dataFile . '.bak-' . date('YmdHis');
        @rename(self::$dataFile, $bak);
        self::$cache = self::defaultData();
        self::save();
    }

    private static function defaultData()
    {
        $appVersion = self::detectAppVersion();
        return [
            'monitor_version' => $appVersion . '.0001',
            'base_version' => $appVersion,
            'counter' => 1,
            'created_at' => date('Y-m-d H:i:s'),
            'updated_at' => date('Y-m-d H:i:s'),
            'stats' => [
                'total' => 0,
                'suspicious' => 0,
                'correct' => 0,
                'false_delete' => 0,
                'protected' => 0,
            ],
            'records' => [],
            'protected_uris' => [],   // md5 => ['uri'=>..., 'abs_uri'=>..., 'ts'=>...]
            'rule_fp' => [],          // 规则名 => 误报次数（用于持续优化提示）
        ];
    }

    /** 读取当前应用版本号（去掉 v 前缀） */
    private static function detectAppVersion()
    {
        $versionFile = dirname(self::$dataFile) . '/../version.php';
        if (file_exists($versionFile)) {
            $vd = @include $versionFile;
            if (is_array($vd) && !empty($vd['version'])) {
                return preg_replace('/[^0-9.]/', '', (string)$vd['version']);
            }
        }
        return '5.15.2';
    }

    private static function save()
    {
        $content = "<?php\n"
            . "/**\n"
            . " * 去广告实时监控数据（由 gz/AdMonitor.php 自动维护，勿手动编辑）\n"
            . " * 监控版本: " . (self::$cache['monitor_version'] ?? '') . "  更新时间: " . date('Y-m-d H:i:s') . "\n"
            . " */\n"
            . 'return ' . var_export(self::$cache, true) . ";\n";

        // 原子写：临时文件 + rename，避免多请求并发写坏数据文件（写一半/交错导致 ParseError）
        $dir = dirname(self::$dataFile);
        if (!is_dir($dir)) {
            @mkdir($dir, 0755, true);
        }
        $tmp = $dir . '/.' . basename(self::$dataFile) . '.tmp.' . getmypid();
        $fp = @fopen($tmp, 'wb');
        if (!$fp) {
            return false;
        }
        if (!flock($fp, LOCK_EX)) {
            @fclose($fp);
            @unlink($tmp);
            return false;
        }
        $ok = fwrite($fp, $content) !== false;
        if ($ok) {
            fflush($fp);
        }
        flock($fp, LOCK_UN);
        fclose($fp);
        if (!$ok) {
            @unlink($tmp);
            return false;
        }
        if (!@rename($tmp, self::$dataFile)) {
            // rename 失败（跨设备/权限受限）时退化为直接写入
            @unlink($tmp);
            return @file_put_contents(self::$dataFile, $content) !== false;
        }
        return true;
    }

    /**
     * 记录一次去广告处理
     * @param array $info domain/url/total/removed/ad_ratio/safeguard_triggered/safeguard_reason/removed_indexes/matched_rules
     * @return array ['id'=>..., 'risk'=>..., 'flags'=>..., 'version'=>..., 'protected_count'=>...]
     */
    public static function record($info)
    {
        self::init();
        $total = max(0, intval($info['total'] ?? 0));
        $removed = max(0, intval($info['removed'] ?? 0));
        $adRatio = round((float)($info['ad_ratio'] ?? 0), 2);
        if ($total > 0 && $removed > 0 && $adRatio <= 0) {
            $adRatio = round($removed / $total * 100, 2);
        }

        // 风险识别
        $flags = [];
        if (!empty($info['safeguard_triggered'])) {
            $flags[] = 'safeguard_triggered';
        }
        if ($total >= 10 && $adRatio >= 50) {
            $flags[] = 'high_ad_ratio';
        }
        $removedIndexes = array_values(array_map('intval', (array)($info['removed_indexes'] ?? [])));
        if (count($removedIndexes) >= 3 && self::isSparseRemovals($removedIndexes)) {
            $flags[] = 'sparse_removals';
        }
        if ($total >= 10 && $removed > 0 && $removed == $total) {
            $flags[] = 'all_removed';
        }

        $risk = 'normal';
        if (!empty($flags)) {
            $risk = ($adRatio >= 70 || in_array('safeguard_triggered', $flags, true) || in_array('all_removed', $flags, true)) ? 'danger' : 'warning';
        }

        $id = 'mon_' . date('ymd_His') . '_' . substr(uniqid('', true), -6);

        $record = [
            'id' => $id,
            'ts' => time(),
            'time' => date('Y-m-d H:i:s'),
            'domain' => (string)($info['domain'] ?? ''),
            'url' => mb_substr((string)($info['url'] ?? ''), 0, 200),
            'total' => $total,
            'removed' => $removed,
            'ad_ratio' => $adRatio,
            'safeguard_triggered' => !empty($info['safeguard_triggered']),
            'safeguard_reason' => (string)($info['safeguard_reason'] ?? ''),
            'removed_indexes' => array_slice($removedIndexes, 0, 40),
            'removed_uris' => array_slice((array)($info['removed_uris'] ?? []), 0, 40), // [['uri'=>..., 'abs_uri'=>...], ...] 用于误删反馈时保护
            'matched_rules' => (array)($info['matched_rules'] ?? []),
            'flags' => $flags,
            'risk' => $risk,
            'feedback' => 'pending', // pending | correct | false_delete
            'feedback_ts' => null,
        ];

        array_unshift(self::$cache['records'], $record);
        if (count(self::$cache['records']) > self::MAX_RECORDS) {
            self::$cache['records'] = array_slice(self::$cache['records'], 0, self::MAX_RECORDS);
        }
        self::$cache['stats']['total'] = (int)(self::$cache['stats']['total'] ?? 0) + 1;
        if ($risk !== 'normal') {
            self::$cache['stats']['suspicious'] = (int)(self::$cache['stats']['suspicious'] ?? 0) + 1;
        }
        self::$cache['updated_at'] = date('Y-m-d H:i:s');
        self::save();

        return [
            'id' => $id,
            'risk' => $risk,
            'flags' => $flags,
            'version' => self::$cache['monitor_version'] ?? '',
            'protected_count' => count(self::$cache['protected_uris'] ?? []),
        ];
    }

    /** 判定删除片段是否「零散」（互不相邻且散布全片 → 疑似误删） */
    private static function isSparseRemovals($indexes)
    {
        sort($indexes);
        $adjacent = 0;
        $prev = null;
        foreach ($indexes as $idx) {
            if ($prev !== null && ($idx - $prev) <= 2) {
                $adjacent++;
            }
            $prev = $idx;
        }
        // 零散判定：相邻(≤2)的对数少于总数的 1/3
        return $adjacent < count($indexes) / 3;
    }

    /**
     * 人工反馈
     * @param string $id 记录ID
     * @param string $result correct | false_delete
     * @return array ['success'=>bool,'message'=>...,'protected_added'=>int]
     */
    public static function feedback($id, $result)
    {
        self::init();
        $result = in_array($result, ['correct', 'false_delete'], true) ? $result : 'correct';
        $protectedAdded = 0;
        foreach (self::$cache['records'] as &$rec) {
            if ($rec['id'] === $id) {
                if ($rec['feedback'] !== 'pending') {
                    // 已是终态，先回退旧计数
                    self::rollbackFeedbackCount($rec['feedback']);
                }
                $rec['feedback'] = $result;
                $rec['feedback_ts'] = date('Y-m-d H:i:s');

                if ($result === 'false_delete') {
                    self::$cache['stats']['false_delete'] = (int)(self::$cache['stats']['false_delete'] ?? 0) + 1;
                    // 命中规则记入误报计数
                    foreach (($rec['matched_rules'] ?? []) as $ruleName => $cnt) {
                        self::$cache['rule_fp'][$ruleName] = (int)(self::$cache['rule_fp'][$ruleName] ?? 0) + intval($cnt);
                    }
                    // 被删片段快照进入保护名单（后续解析自动还原，防止重复误删）
                    foreach (($rec['removed_uris'] ?? []) as $u) {
                        if (self::addProtectedUri($u['uri'] ?? '', $u['abs_uri'] ?? '')) {
                            $protectedAdded++;
                        }
                    }
                } else {
                    self::$cache['stats']['correct'] = (int)(self::$cache['stats']['correct'] ?? 0) + 1;
                }
                self::$cache['updated_at'] = date('Y-m-d H:i:s');
                self::save();
                return ['success' => true, 'message' => $result === 'false_delete' ? '已标记为误删，相关片段已进入保护名单' : '已标记为正常删除', 'protected_added' => $protectedAdded];
            }
        }
        return ['success' => false, 'message' => '未找到对应监控记录'];
    }

    private static function rollbackFeedbackCount($oldFeedback)
    {
        if ($oldFeedback === 'false_delete') {
            self::$cache['stats']['false_delete'] = max(0, (int)(self::$cache['stats']['false_delete'] ?? 0) - 1);
        } elseif ($oldFeedback === 'correct') {
            self::$cache['stats']['correct'] = max(0, (int)(self::$cache['stats']['correct'] ?? 0) - 1);
        }
    }

    /** 将误删片段加入保护名单（自动还原） */
    public static function protectUrisFromRecord($id)
    {
        return self::protectRemovedOfRecord($id);
    }

    /**
     * 添加受保护片段
     */
    public static function addProtectedUri($uri, $absUri = '')
    {
        self::init();
        $uri = trim((string)$uri);
        $absUri = trim((string)$absUri);
        if ($uri === '' && $absUri === '') {
            return false;
        }
        $key = md5($uri !== '' ? $uri : $absUri);
        if (isset(self::$cache['protected_uris'][$key])) {
            return false;
        }
        self::$cache['protected_uris'][$key] = [
            'uri' => mb_substr($uri, 0, 300),
            'abs_uri' => mb_substr($absUri, 0, 300),
            'ts' => date('Y-m-d H:i:s'),
        ];
        self::$cache['stats']['protected'] = count(self::$cache['protected_uris']);
        self::$cache['updated_at'] = date('Y-m-d H:i:s');
        self::save();
        return true;
    }

    /**
     * 由反馈记录批量保护：把记录中被删片段对应的 uri 快照加入保护名单。
     * 记录在 record() 时保存 removed_uris 快照（uri + absUri）。
     */
    public static function protectRemovedOfRecord($id)
    {
        self::init();
        $added = 0;
        foreach (self::$cache['records'] as &$rec) {
            if ($rec['id'] !== $id) continue;
            $uris = $rec['removed_uris'] ?? [];
            foreach ($uris as $u) {
                if (self::addProtectedUri($u['uri'] ?? '', $u['abs_uri'] ?? '')) {
                    $added++;
                }
            }
            break;
        }
        return $added;
    }

    /**
     * 移除受保护片段
     * @param string $md5 保护名单的 key
     */
    public static function removeProtectedUri($md5)
    {
        self::init();
        if (isset(self::$cache['protected_uris'][$md5])) {
            unset(self::$cache['protected_uris'][$md5]);
            self::$cache['stats']['protected'] = count(self::$cache['protected_uris']);
            self::$cache['updated_at'] = date('Y-m-d H:i:s');
            self::save();
            return true;
        }
        return false;
    }

    /**
     * 获取保护名单（md5 => ['uri'=>..., 'abs_uri'=>..., 'ts'=>...]）
     */
    public static function getProtectedUris()
    {
        self::init();
        return self::$cache['protected_uris'] ?? [];
    }

    /**
     * 判断片段是否受保护（uri 或绝对地址命中保护名单）
     */
    public static function isProtectedSeg($uri, $absUri = '')
    {
        self::init();
        if (empty($uri) && empty($absUri)) {
            return false;
        }
        if (!empty($uri) && isset(self::$cache['protected_uris'][md5($uri)])) {
            return true;
        }
        if (!empty($absUri) && isset(self::$cache['protected_uris'][md5($absUri)])) {
            return true;
        }
        return false;
    }

    public static function getStats()
    {
        self::init();
        $stats = self::$cache['stats'] ?? [];
        $stats['records'] = count(self::$cache['records'] ?? []);
        $stats['protected'] = count(self::$cache['protected_uris'] ?? []);
        $totalFeedback = (int)($stats['correct'] ?? 0) + (int)($stats['false_delete'] ?? 0);
        $stats['accuracy'] = $totalFeedback > 0
            ? round((int)($stats['correct'] ?? 0) / $totalFeedback * 100, 1)
            : null;
        return $stats;
    }

    public static function getRecords($limit = 50)
    {
        self::init();
        return array_slice(self::$cache['records'] ?? [], 0, max(1, intval($limit)));
    }

    public static function getRuleFp()
    {
        self::init();
        return self::$cache['rule_fp'] ?? [];
    }

    public static function getVersion()
    {
        self::init();
        // 应用版本升级后，监控版本自动跟随（保留计数编号，前缀切到新版本）
        $app = self::detectAppVersion();
        $base = self::$cache['base_version'] ?? '';
        if ($base !== $app) {
            self::$cache['base_version'] = $app;
            $counter = max(1, (int)(self::$cache['counter'] ?? 1));
            self::$cache['monitor_version'] = $app . '.' . str_pad((string)$counter, 4, '0', STR_PAD_LEFT);
            self::save();
        }
        return self::$cache['monitor_version'] ?? '';
    }

    /**
     * 重置监控数据并递增监控版本（当前版本 + 三位数字计数）
     */
    public static function reset()
    {
        self::init();
        $base = self::$cache['base_version'] ?? self::detectAppVersion();
        self::$cache['counter'] = (int)(self::$cache['counter'] ?? 0) + 1;
        self::$cache['monitor_version'] = $base . '.' . str_pad((string)self::$cache['counter'], 4, '0', STR_PAD_LEFT);
        self::$cache['records'] = [];
        self::$cache['protected_uris'] = [];
        self::$cache['rule_fp'] = [];
        self::$cache['stats'] = [
            'total' => 0,
            'suspicious' => 0,
            'correct' => 0,
            'false_delete' => 0,
            'protected' => 0,
        ];
        self::$cache['created_at'] = date('Y-m-d H:i:s');
        self::$cache['updated_at'] = date('Y-m-d H:i:s');
        self::save();
        return self::$cache['monitor_version'];
    }
}
