#!/usr/bin/env node
/**
 * render-title/fetch-title.js
 * 可选增强抓取：用无头 Chromium(真浏览器)渲染官方剧集页，取 JS 渲染后的真实剧名。
 *
 * 背景：腾讯/爱奇艺/芒果/搜狐/PP 等平台播页对"无浏览器环境的 HTTP 客户端"统一返回 JS 渲染空壳
 * （<title> 只有平台名），纯静态 http.Get 拿不到真实剧名。而真浏览器渲染后能拿到。
 *
 * 用法：
 *   1) 首次使用先安装依赖与 Chromium（见本目录 README）：
 *        cd render-title && npm install && npx playwright install chromium --with-deps
 *   2) 调用：
 *        node fetch-title.js "https://v.qq.com/x/page/xxx.html"
 *   输出：stdout 单行 JSON  { "title": "...", "og_title": "...", "err": "" }
 *   非零退出码 + err 字段表示失败（Go 端应回退到静态抓取）。
 *
 * 这是"可选"增强：主程序(GO 单文件)检测到 `node` + 本脚本存在才调用，
 * 缺 Node / 未装依赖 / 脚本异常 都会自动回退静态抓取，不破坏主程序形态。
 *
 * 标题提取优先级：
 *   1) 页面 JSON-LD（application/ld+json 的 VideoObject/Episode.name）
 *   2) og:title / twitter:title
 *   3) document.title（JS 渲染后）
 *   4) 特定平台选择器（哔哩哔哩 412 反爬时走公开 API 兜底）
 */
const { chromium } = require('playwright');

const url = process.argv[2];
if (!url) {
  console.log(JSON.stringify({ title: '', og_title: '', err: 'no url argument' }));
  process.exit(1);
}

// 哔哩哔哩：视频页对数据中心 IP 反爬（412/「出错啦!」），真浏览器也拿不到 → 直接走公开 API（无需登录）
async function tryBilibiliAPI(u) {
  try {
    const m = u.match(/\/video\/(BV[0-9A-Za-z]{10,12})/);
    if (!m) return '';
    const r = await fetch('https://api.bilibili.com/x/web-interface/view?bvid=' + encodeURIComponent(m[1]), {
      headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' },
    });
    if (!r.ok) return '';
    const j = await r.json();
    if (j && j.code === 0 && j.data && j.data.title) return String(j.data.title).trim();
  } catch (e) { /* ignore */ }
  return '';
}

(async () => {
  if (/bilibili\.com/i.test(url)) {
    const apiTitle = await tryBilibiliAPI(url);
    if (apiTitle) {
      console.log(JSON.stringify({ title: apiTitle, og_title: '', err: '' }));
      process.exit(0);
    }
  }
  let browser = null;
  try {
    browser = await chromium.launch({
      headless: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage', '--disable-gpu'],
    });
    const ctx = await browser.newContext({
      userAgent:
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36',
      locale: 'zh-CN',
      viewport: { width: 1280, height: 900 },
    });
    const page = await ctx.newPage();
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 25000 });
    // 多数平台的 `document.title` 由 JS 写标，等一会儿再读
    await page.waitForTimeout(4000);
    let title = (await page.title() || '').trim();
    let og = '';
    // JSON-LD：VideoObject/Episode 的 name 最可靠（爱奇艺/腾讯/优酷等都有）
    const jsonLd = await page
      .evaluate(() => {
        const out = [];
        document.querySelectorAll('script[type="application/ld+json"]').forEach((s) => {
          try {
            const d = JSON.parse(s.textContent || '');
            const list = Array.isArray(d) ? d : [d];
            list.forEach((it) => {
              if (it && it.name && (it['@type'] || '').toLowerCase().indexOf('video') >= 0) out.push(it.name);
            });
          } catch (e) {}
        });
        return out;
      })
      .catch(() => []);
    if (jsonLd.length && jsonLd[0]) og = String(jsonLd[0]).trim();
    // og:title / twitter:title
    if (!og) {
      og = await page
        .evaluate(() => {
          const m = document.querySelector('meta[property="og:title"], meta[name="og:title"], meta[property="twitter:title"], meta[name="twitter:title"]');
          return m ? (m.getAttribute('content') || '') : '';
        })
        .catch(() => '');
      og = String(og || '').trim();
    }
    // h1 兜底（部分平台正文大标题即剧名）
    if (!og && !title) {
      title = await page.evaluate(() => {
        const h = document.querySelector('h1');
        return h ? (h.textContent || '').trim() : '';
      }).catch(() => '');
    }
    console.log(JSON.stringify({ title: title || '', og_title: og || '', err: '' }));
  } catch (e) {
    console.log(JSON.stringify({ title: '', og_title: '', err: (e && e.message ? e.message : String(e)).split('\n')[0] }));
  } finally {
    if (browser) await browser.close().catch(() => {});
  }
})();
