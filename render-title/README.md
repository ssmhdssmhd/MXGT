# render-title（可选增强抓取）

用无头 Chromium（真浏览器）渲染官方剧集页，取 **JS 渲染后**的真实剧名，解决「一键映射抓取不到」的主因——腾讯/爱奇艺/芒果/搜狐等平台播页对**无浏览器环境的 HTTP 客户端**统一返回 JS 渲染空壳（`<title>` 只有平台名）。

## 这是"可选"增强

主程序（[main.go](../main.go) 单文件 Go、标准库零依赖）**检测到 `node` + 本脚本存在**才会调用它；缺 Node / 未装依赖 / 脚本异常 都会**自动回退**到原有静态抓取。因此：

- **不破坏**主程序"单文件 / 零依赖 / 静态编译"形态；
- 未启用时系统行为与 v0.5.8 完全一致（明确失败原因提示）。

## 启用（在服务器上，一次即可）

```bash
cd render-title
npm install --no-audit --no-fund        # 装 playwright
npx playwright install chromium --with-deps   # 下载 Chromium（约 400MB）+ 系统依赖
```

验证：

```bash
node fetch-title.js "https://www.iqiyi.com/v_2bkbhy2gi2w.html"
# 输出单行 JSON：
# {"title":"生逢其时-电视剧全集-完整版视频在线观看-爱奇艺","og_title":"","err":""}
```

## 调用方式

Go 主程序在**一键映射**时，如果检测到可用，会执行：

```
node <本脚本绝对路径> "<官方剧集 URL>"
```

取 stdout 解析出的 `title`（优先，含真实剧名）作为抓取标题，成功后走原来的剧名解析 + 自动映射；`err` 非空或未启用则回退静态抓取。

## 注意

- 部署机需要 **Node.js ≥ 18** 与可写缓存目录（`~/.cache/ms-playwright`）；
- 部分平台（如哔哩哔哩）对数据中心 IP 反爬更强，真浏览器也可能拿不到；
- 首次启动 Chromium 较慢（约数秒），一键映射是低频操作，可接受。