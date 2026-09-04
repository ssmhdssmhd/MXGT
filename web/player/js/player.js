/**
 * player.js —— 播放器初始化、API 调用、跨域处理
 * 兼容 DPlayer（hls.js 原生支持 CORS 跨域播放）
 */
(function (window) {
  'use strict';

  var titleEl = document.getElementById('title');
  var metaEl = document.getElementById('meta');
  var statusEl = document.getElementById('status');
  var errorEl = document.getElementById('error');
  var player = null;

  function showError(msg) {
    errorEl.textContent = '❌ ' + msg;
    errorEl.classList.remove('hidden');
  }

  function setStatus(msg) {
    statusEl.textContent = msg;
  }

  /** 初始化播放器 */
  function play(url, type) {
    type = type || 'hls';
    setStatus('正在加载播放器...');

    player = new DPlayer({
      container: document.getElementById('player'),
      autoplay: true,
      video: { url: url, type: type }
    });
  }

  /** 主流程：解析 query → 调 resolve → 播放 */
  function main() {
    var target = window.getTargetUrl();
    if (!target) {
      showError('缺少 url 参数。用法: /?url=https://...&title=剧名&ep=5');
      return;
    }

    var title = urlParams.get('title');
    var ep = urlParams.get('ep');
    if (title) titleEl.textContent = title;
    metaEl.textContent = (title || '未知剧名') + (ep ? ' · 第' + ep + '集' : '') + '\n' + target;

    setStatus('正在解析: ' + target);
    window.resolveUrl(target)
      .then(function (res) {
        if (!res || res.code !== 1 || !res.data || !res.data.url) {
          throw new Error((res && res.msg) || '解析失败，未返回可播放地址');
        }
        var data = res.data;
        var playURL = data.proxy
          ? window.MXGT_API_BASE + data.url        // 走后端代理（跨域/防盗链）
          : data.url;                              // 直链（hls.js 跨域播放）
        setStatus('解析成功（rule#' + data.rule_id + (data.cache_hit ? ' 缓存命中' : '') + '）');
        play(playURL, data.type || 'hls');
      })
      .catch(function (err) {
        showError(err.message || String(err));
      });
  }

  document.addEventListener('DOMContentLoaded', main);
})(window);
