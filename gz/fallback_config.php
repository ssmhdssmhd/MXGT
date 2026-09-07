<?php
/**
 * 去插播兜底线路配置（由后台「沫兮API」页面自动维护）
 * 更新时间: 2026-09-07 20:25:29
 *
 * 配置项说明：
 *   - enabled: 全局开关（关闭后走原有官替/官解链路）
 *   - lines:   兜底线路列表，取第一条启用的线路生效
 *     - name:  线路名称
 *     - url:   接口地址模板，占位参数 url= 会自动拼接要清洗的资源地址
 */
return array (
  'enabled' => true,
  'lines' => 
  array (
    0 => 
    array (
      'id' => 'fb_moxi_1',
      'name' => '沫兮兜底 1',
      'url' => 'https://mxqcb.ssmhd.com/api/clean/?url=',
      'enabled' => true,
      'sort' => 1,
    ),
  ),
  'update_date' => '2026-09-07 20:25:29',
);
