# MXGT-Go AI 去广告模块（独立目录，可单独更新）

本目录 `ai/` 是 **AI 去广告** 的独立子模块，与主程序 `main.go` 分开存放、独立版本号，便于后续单独更新。

## 目录结构

| 文件 | 说明 |
|---|---|
| `config.json` | AI 去广告配置（启用开关、模式、AI 服务商/地址/key/模型/提示词） |
| `VERSION` | 本模块独立版本号（如 `v0.1.0`） |
| `README.md` | 本说明 |

## 配置文件说明（`ai/config.json`）

```json
{
  "enabled": false,          // 是否启用 AI（false=默认规则去广告）
  "mode": "basic",           // 去广告模式：basic=仅规则引擎；ai=规则+AI 审核；auto=有 AI 配置则用 AI 否则规则
  "provider": "openai",      // AI 服务商（见下方支持列表）
  "api_url": "https://api.deepseek.com/v1/chat/completions",
  "api_key": "sk-xxx",       // 请勿提交真实 key，运行时在服务器填写
  "model": "deepseek-chat",
  "prompt": "……",            // 去广告识别提示词模板
  "max_segments": 300,       // 超过该片段数不送审（控制 token）
  "timeout": 25,             // AI 请求超时(秒)
  "replace_enabled": false,  // 是否启用「AI 智能官替」（官方链接→AI识别剧名/集数→全资源站搜索→AI挑最优播放链接）
  "replace_prompt": "",      // 官替判定提示词（留空用内置默认）
  "format": "openai"         // 请求协议：openai（OpenAI 兼容，默认）| anthropic | gemini
}
```

## 支持的大模型提供商（后台「🧠 AI 大模型接入」下拉可选）

- **OpenAI 兼容协议**（一大类）：`zhipu` 智谱 GLM（免费）、`siliconflow` 硅基流动（免费）、`modelscope` 魔搭（免费）、`baidu` 百度千帆 ERNIE（免费）、`groq` Groq（免费，需科学上网）、`openai` OpenAI、`deepseek` DeepSeek、`moonshot` Kimi、`qwen` 通义千问、`hunyuan` 腾讯混元、`ollama` 本地 Ollama、`custom` 自定义中转/OneAPI；
- **Anthropic 协议**：`anthropic` Claude；
- **Google 协议**：`gemini` Gemini（免费额度，需科学上网）。
- 选择提供商会自动填入接口地址与默认模型；也可手动改 `api_url` / `model`，`format` 由提供商预设自动决定、可手动覆盖。

## 🆓 免费使用指引（0 成本先试效果）

功能要求不高时，以下任意一家注册拿 Key 即可，免费额度足够试用 AI 去广告 / AI 智能官替：

| 提供商 | 免费额度 | 申请入口 | 后台选择 | 备注 |
|---|---|---|---|---|
| 智谱 AI（推荐） | GLM-4-Flash 完全免费，新用户送 2000 万 Token | open.bigmodel.cn（手机号即开） | 智谱 GLM（免费） | 国内直连，中文好，模型 `glm-4-flash` |
| 硅基流动 | 送 2000 万 Token + 9B 以下模型永久免费 | cloud.siliconflow.cn | 硅基流动（免费） | 国内直连，模型 `Qwen/Qwen2.5-7B-Instruct` |
| 魔搭 ModelScope | 免费 2000 次/天 | modelscope.cn（需阿里云实名） | 魔搭 ModelScope（免费） | 国内直连 |
| 百度千帆 | ERNIE-Speed 永久免费 QPS50 | console.bce.baidu.com/qianfan | 百度千帆（免费） | 国内直连 |
| Groq | 免费额度（RPM30/天14.4k次） | console.groq.com | Groq（免费） | 超低延迟，需科学上网 |

**三步开始试用**：
1. 去上表任一平台注册账号 → 控制台创建 API Key（复制以 `sk-`/`xxx` 开头的 Key）；
2. 打开本服务后台 `/mxadmin` → 「🧠 AI 大模型接入」→ 下拉选对应提供商 → 粘贴 Key → 💾 保存；
3. 点 📡 测试连接看到「连通成功」后，勾选「启用 AI」（去广告）和/或「AI 智能官替」→ 💾 保存，即可去「解析测试 / 官替链路」面板试用。

> 默认配置已预填免费方案（智谱 GLM-4-Flash），只需填 Key 并启用。Key 仅保存在服务器本地 `ai/config.json`，后台不回传明文。

- 主程序启动时读取可执行文件同目录下的 `ai/config.json`；若不存在则用内置默认（`enabled=false, mode=basic`，即默认规则去广告，行为完全不变）。
- 部署时把本目录整个放到 `./ai/` 与二进制同级即可；改配置后**无需重编主程序**，独立更新本文件夹即完成 AI 去广告版本升级。
- **推荐直接在后台配置**（v0.6.26+）：`/mxadmin` → 「🧠 AI 大模型接入」面板下拉选择提供商、填 Key、💾保存、📡测试连接，无需手改文件。

## 使用方式

- 后台「解析测试」页可选择「去广告引擎」= 基础 / 自动 / AI；
- HTTP 接口可用 `engine=basic|auto|ai` 指定，如：
  - `GET /api/clean?url=<m3u8>&engine=ai`
  - `GET /api/jx?url=<m3u8>&engine=ai`（JSON 兼容接口）
- 查看/更新/测试：
  - `GET  /api/ai/config`（查看，key 打码）
  - `POST /api/ai/config`（需登录 admin，更新配置）
  - `POST /api/ai/test`（需登录 admin，连通测试）