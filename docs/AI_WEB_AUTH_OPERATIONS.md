# AI 账号授权与个人中转运行手册

## 发布边界

功能总开关 `TOKHUB_AI_WEB_AUTH_ENABLED` 默认关闭。关闭时已有官方 API Key 连接保持原有行为，授权 API 返回 404，用户界面只展示 API Key 方式。

当前适配器：

| 适配器 | 开关 | 依赖 | 建议发布 |
|---|---|---|---|
| Gemini Google OAuth | `TOKHUB_AI_GEMINI_OAUTH_ENABLED` | Google OAuth Client、Cloud Project、HTTPS Public URL、Redis | 完成 Google 配置与回调验证后灰度 |
| DeepSeek 开放平台引导 | `TOKHUB_AI_DEEPSEEK_GUIDED_ENABLED` | Redis、凭证密钥环 | 可先发布 |
| ChatGPT Codex OAuth | `TOKHUB_AI_CHATGPT_CODEX_EXPERIMENTAL` | Redis、凭证密钥环、固定风险确认值 | 仅自托管实验 |

ChatGPT 实验开关还要求：

```env
TOKHUB_AI_EXPERIMENTAL_BRIDGE_ACK=I_ACCEPT_CHATGPT_CODEX_EXPERIMENTAL_RISK
```

## Gemini 配置

1. 在 Google Cloud 创建 Web application OAuth Client。
2. 配置回调地址：

   `https://<TOKHUB_PUBLIC_URL>/api/me/ai-authorizations/google/callback`

3. 配置以下密钥：

```env
TOKHUB_GOOGLE_OAUTH_CLIENT_ID=
TOKHUB_GOOGLE_OAUTH_CLIENT_SECRET=
TOKHUB_GOOGLE_OAUTH_PROJECT_ID=
```

用户可以在授权时填写自己的 Project ID。空值会使用部署级默认 Project ID。TokHub 请求 Gemini 时固定使用 `https://generativelanguage.googleapis.com/v1beta`，认证头由适配器生成。

## 数据与安全

- 授权事务和二次验证 grant 保存在 Redis，默认 10 分钟过期并单次消费。
- state、PKCE verifier、nonce 与当前用户和登录 Session 绑定。
- OIDC ID Token 使用服务商 discovery 和 JWKS 完成 RS256 签名校验，再校验 issuer、audience、authorized party、subject、expiry 和 Google nonce。Google 文档列出的 `https://accounts.google.com` 与 `accounts.google.com` 均可识别。
- OAuth bundle 使用版本化 AES-256-GCM 密钥环加密。
- 服务端日志、审计和指标不记录 Token、Cookie、授权 code、密码或完整账号。
- 出站请求只能使用适配器固定 endpoint 与允许的认证头。
- 重新授权会锁定原 `provider subject`；ChatGPT 同时锁定原 `account id`。账号不一致时记录 `identity_mismatch` 并保留原连接。Gemini 未明确提交新 Project ID 时沿用原项目。
- 删除 OAuth 连接需要再次验证当前 TokHub 登录密码。本地路由与凭证会先停用和擦除，再尝试调用服务商撤销接口，避免撤销接口故障延长本地暴露窗口。

## 刷新与故障状态

后台运行时每分钟扫描即将到期的 OAuth 凭证。刷新使用 Redis 分布式锁和数据库版本条件更新，避免多实例重复覆盖。全局 worker、单服务商并发、单服务商 QPS 和单次刷新超时分别由以下变量控制：

```env
TOKHUB_AI_OAUTH_REFRESH_WORKERS=8
TOKHUB_AI_OAUTH_PROVIDER_CONCURRENCY=4
TOKHUB_AI_OAUTH_PROVIDER_QPS=2
TOKHUB_AI_OAUTH_REFRESH_ATTEMPT_TIMEOUT=20s
```

服务商级门控可以降低一批凭证同时到期时触发 429 或放大上游故障的概率。单次刷新超时结束后会按临时错误进入退避。

退避时间依次为 1、5、15、60 分钟。`invalid_grant`、`invalid_refresh_token`、`token_expired`、`app_session_terminated`、`refresh_token_reused`、`refresh_token_invalidated`、缺少 Refresh Token 或明确撤销会进入 `reauth_required`。错误码可以来自 OAuth 标准字符串或服务商嵌套错误对象，错误详情不会写入用户响应和日志。临时网络错误保留当前凭证并进入下一次退避。

网关在发送响应前收到 401 时会执行一次受锁保护的刷新和一次重试。流式响应写出首字节后不会重放。

## ChatGPT 实验保护

- 个人范围，无法切换到共享工作区。
- 每个 Codex OAuth 连接最多创建一个 active 或 paused 中转。
- 网关 QPS 在服务端强制设为 1。
- Redis 并发槽上限为 2，槽保护不可用时返回 503。
- 私有接口固定为 `https://chatgpt.com/backend-api/codex/responses`。
- 出站请求固定发送配套的 `User-Agent`、`Originator`、`Version` 与 `OpenAI-Beta`，当前桥接版本为 `0.144.1`。
- Chat Completions 文本与 function tool 子集会转换为 Responses 请求。
- tool result、`tool_choice`、并行工具开关、函数调用结果和函数参数增量均进入协议转换。
- Responses SSE 可直通；Chat Completions SSE 会转换为标准 chunk，并保持函数 item 与 call 使用同一工具索引。
- 服务商接口或条款变化时，先关闭实验开关。已有连接会因为适配器不可用而停止刷新和转发。

## 个人中转协议与超时

- Gemini 流式请求固定调用 `streamGenerateContent?alt=sse`，服务端将 Gemini chunk、结束原因和 `usageMetadata` 转换为 OpenAI Chat Completions 或 Responses SSE。
- Responses 流包含 created、output item、content part、text delta、done 与 completed 生命周期事件。
- 上游响应头采用允许列表，只转发内容类型、请求 ID、限流和重试信息。`Set-Cookie`、`WWW-Authenticate` 与服务端指纹头会被丢弃。
- OAuth 个人中转单次请求默认上限为 300 秒，服务端写响应上限为 310 秒。客户端断开会立即取消上游请求。

## 监控

Prometheus 指标：

- `tokhub_ai_authorization_attempts{provider,method,status}`
- `tokhub_ai_oauth_connections{provider,method,auth_status}`
- `tokhub_ai_oauth_refresh_failures_current{provider}`
- `tokhub_ai_connections_active`
- `tokhub_ai_connections_attention`
- `tokhub_ai_quick_relays_total`

建议告警：

- `reauth_required` 连接数量持续增长 15 分钟。
- `identity_mismatch` 授权失败出现时通知连接所有者检查供应商登录账号。
- 任一 provider 的连续刷新失败数大于 3。
- 授权 `failed / (completed + failed)` 在 15 分钟窗口超过 30%。
- ChatGPT 实验网关出现持续 401、403、404 或协议解析错误。

## 发布检查

1. 数据库迁移 `0047_ai_web_authorization.sql` 已完成。
2. Redis 可用，`/readyz` 返回 ready。
3. 加密密钥环和指纹密钥环使用独立材料。
4. `TOKHUB_PUBLIC_URL` 使用 HTTPS，Google 回调精确匹配。
5. 先开启全局开关和 DeepSeek 引导，观察授权状态指标。
6. Gemini 在测试用户验证授权、刷新、重新授权和删除后再扩大范围。
7. ChatGPT 只在完成风险确认、自托管环境和紧急关闭演练后启用。
