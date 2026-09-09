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
  "enabled": false,          // 是否启用 AI 去广告（false=默认规则去广告）
  "mode": "basic",           // 去广告模式：basic=仅规则引擎；ai=规则+AI 审核；auto=有 AI 配置则用 AI 否则规则
  "provider": "openai",      // AI 服务商：openai（OpenAI/通义/DeepSeek 等 Chat Completions 兼容）
  "api_url": "https://api.deepseek.com/v1/chat/completions",
  "api_key": "sk-xxx",       // 请勿提交真实 key，运行时在服务器填写
  "model": "deepseek-chat",
  "prompt": "……",            // 识别提示词模板
  "max_segments": 300,       // 超过该片段数不送审（控制 token）
  "timeout": 25              // AI 请求超时(秒)
}
```

- 主程序启动时读取可执行文件同目录下的 `ai/config.json`；若不存在则用内置默认（`enabled=false, mode=basic`，即默认规则去广告，行为完全不变）。
- 部署时把本目录整个放到 `./ai/` 与二进制同级即可；改配置后**无需重编主程序**，独立更新本文件夹即完成 AI 去广告版本升级。

## 使用方式

- 后台「解析测试」页可选择「去广告引擎」= 基础 / 自动 / AI；
- HTTP 接口可用 `engine=basic|auto|ai` 指定，如：
  - `GET /api/clean?url=<m3u8>&engine=ai`
  - `GET /api/jx?url=<m3u8>&engine=ai`（JSON 兼容接口）
- 查看/更新配置：
  - `GET  /api/ai/config`（查看，key 打码）
  - `POST /api/ai/config`（需登录 admin，更新配置）