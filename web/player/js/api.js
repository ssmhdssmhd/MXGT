/**
 * api.js —— 封装 resolve / proxy 调用
 * 关键点：API 地址不硬编码，按 query → localStorage → 同域 三层兜底
 */
(function (window) {
  'use strict';

  // 1. 从 query 参数 api_base 读取（部署时灵活切换）
  var urlParams = new URLSearchParams(window.location.search);
  var apiBase = urlParams.get('api_base')
    // 2. 从 localStorage 读取（开发调试用）
    || window.localStorage.getItem('MXGT_API_BASE')
    // 3. 默认同域（生产环境，前端页面和后端同部署）
    || window.location.protocol + '//' + window.location.host;

  window.MXGT_API_BASE = apiBase.replace(/\/+$/, '');

  /** 解析源站 URL，返回真实视频链接 */
  window.resolveUrl = function (sourceUrl) {
    var url = window.MXGT_API_BASE + '/api/resolve?url=' + encodeURIComponent(sourceUrl);
    return fetch(url).then(function (resp) {
      return resp.json();
    });
  };

  /** 读取 query 目标 URL（支持多别名参数，方便伪装） */
  window.getTargetUrl = function () {
    var aliases = ['url', 'video', 'src', 'link', 'v', 'u'];
    for (var i = 0; i < aliases.length; i++) {
      var v = urlParams.get(aliases[i]);
      if (v && (v.indexOf('http://') === 0 || v.indexOf('https://') === 0)) {
        return decodeURIComponent(v);
      }
    }
    return null;
  };
})(window);
