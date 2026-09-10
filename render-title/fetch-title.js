#!/usr/bin/env node
/**
 * render-title/fetch-title.js
 * 可选增强抓取：用无头 Chromium(真浏览器)渲染官方剧集页，取 JS 渲染后的真实剧名。
 *
 * 背景：腾讯/爱奇艺/芒果等平台播页对"无浏览器环境的 HTTP 客户端"统一返回 JS 渲染空壳
 * （<title> 只有平台名），纯静态 http.Get 拿不到真实剧名。而真浏览器渲染后能拿到。
 *
 * 用法：
 *   1) 首次使用先安装依赖与 Chromium（见本目录 README）：
 *        cd render-title && npm install && npx playwright install chromium
 *   2) 调用：
 *        node fetch-title.js "https://v.qq.com/x/page/xxx.html"
 *   输出：stdout 单行 JSON  { "title": "...", "og_title": "...", "err": "" }
 *   非零退出码 + err 字段表示失败（Go 端应回退到静态抓取）。
 *
 * 这是"可选"增强：主程序(GO 单文件)检测到 `node` + 本脚本存在才调用，
 * 缺 Node / 未装依赖 / 脚本异常 都会自动回退静态抓取，不破坏主程序形态。
 */
const { chromium } = require('playwright');

const url = process.argv[2];
if (!url) {
  console.log(JSON.stringify({ title: '', og_title: '', err: 'no url argument' }));
  process.exit(1);
}

(async () => {
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
    let title = await page.title();
    const og = await page
      .evaluate(() => {
        const m = document.querySelector('meta[property="og:title"], meta[name="og:title"]');
        return m ? (m.getAttribute('content') || '') : '';
      })
      .catch(() => '');
    title = (title || '').trim();
    // hint：这里可扩展特定平台选择器，先用 title 兜底
    console.log(JSON.stringify({ title, og_title: og.trim(), err: '' }));
  } catch (e) {
    console.log(JSON.stringify({ title: '', og_title: '', err: (e && e.message ? e.message : String(e)).split('\n')[0] }));
  } finally {
    if (browser) await browser.close().catch(() => {});
  }
})();