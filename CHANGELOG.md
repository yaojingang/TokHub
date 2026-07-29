# Changelog

TokHub 从 `v2.0.0-rc.1` 开始使用语义化版本。2.0 代表第二代产品架构，覆盖公开监控、工作区、专属网关和个人 AI 账号授权。

## [2.0.0-rc.1] - 2026-07-29

### Breaking Changes

1. **运行环境基线**：源码构建使用 Go 1.26.5、Node.js 25、React 19.2.7 和 React Router 8.3.0。
2. **发布版本统一**：Docker、Helm、OpenAPI、Agent Skill 和运行时健康信息统一使用 `2.0.0-rc.1`。

### New Features

1. **个人 AI 服务连接**：普通用户可以在个人空间连接 OpenAI、Gemini、Kimi、DeepSeek、豆包、Claude 和千问。
2. **消费者账号授权**：支持 ChatGPT Codex OAuth、Gemini Google OAuth 和 DeepSeek 网页登录态实验连接。
3. **TokHub AI 登录助手**：Chrome 助手可以识别 ChatGPT localhost 回调，并在用户点击后读取 DeepSeek `userToken.value`。
4. **个人专属中转**：连接验证通过后可以创建 OpenAI 兼容的个人中转站，并使用独立 Gateway Key。
5. **授权生命周期管理**：支持加密保存、到期刷新、失效检测、重新授权、账号一致性检查、删除和审计。

### Fixes & Improvements

1. **刷新并发保护**：旧刷新任务无法覆盖用户刚完成的新授权。
2. **授权入口保护**：账号授权连接无法通过 API Key 轮换接口替换登录态。
3. **安全回调**：OAuth 公网回调要求 HTTPS，回调结果页禁止携带一次性信息作为 Referrer。
4. **依赖安全**：升级 Go 运行时、Chi、pgx、React Router 和相关依赖，当前可达漏洞扫描结果为 0。
5. **DeepSeek 稳定性**：网页登录态通过固定桥服务完成验证和转发，失效后连接进入重新授权状态。

### Deprecations

当前 RC 没有新增弃用项。官方 API Key 连接、公开状态 API 和 `/gateway/v1/*` 保持兼容。

[2.0.0-rc.1]: https://github.com/yaojingang/TokHub/releases/tag/v2.0.0-rc.1
