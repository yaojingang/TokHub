# TokHub API 接入说明

本文件面向接入方和后续开发者，说明 TokHub 当前稳定 API 边界。机器可读合同见 `docs/openapi.yaml`，运行中的服务也会暴露：

```bash
curl http://localhost:8080/openapi.yaml
```

## API 分层

| Namespace | 使用方 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| `/api/public/*` | 前台页面、匿名访客 | 无 | 首页、详情页、推荐页公开数据 |
| `/api/auth/*` | 浏览器用户 | Cookie + CSRF | 登录、注册、会话、邮箱验证、找回密码 |
| `/api/me/*` | 登录用户 | Cookie + CSRF | 收藏、个人私有通道和 AI 开发者服务连接 |
| `/api/console/*` | 用户/企业工作区 | Cookie + CSRF | 专属网关、成员、用量、告警、审计 |
| `/api/admin/*` | 平台管理员 | Cookie + CSRF + admin role | 平台通道、推荐、站点配置、全局治理 |
| `/v1/status/*` | 第三方只读接入 | `X-Site-Key` 或 Bearer | 公开状态数据 Open API |
| `/gateway/v1/*` | 企业/个人专属网关调用方 | Bearer Gateway Key | OpenAI 兼容网关 |

管理员 agent 可通过 scoped Bearer token 复用 `/api/admin/*`，但它是内部机器执行合同，不属于第三方公开稳定 API。细节见 `docs/admin-agent-api.md` 和 `docs/admin-agent.openapi.yaml`；`docs/openapi.yaml` 继续只描述公开接入合同。

## 通用响应

JSON 成功响应直接返回业务对象。错误响应统一为：

```json
{
  "error": {
    "code": "rate_limited",
    "message": "Too many public API requests",
    "requestId": "req-..."
  }
}
```

前端和第三方接入方应以 `error.code` 做程序判断，不要解析英文 `message`。

## 公开监控 API

这些接口无鉴权，有基础缓存和限流，字段与前台页面保持一致。

```bash
curl http://localhost:8080/api/public/overview
curl "http://localhost:8080/api/public/channels?dimension=brand&status=healthy&range=24&page=1&pageSize=50"
curl http://localhost:8080/api/public/channels/ch_openai_gpt4o
curl "http://localhost:8080/api/public/channels/ch_openai_gpt4o/series?days=7"
curl "http://localhost:8080/api/public/providers/rank?range=24"
curl "http://localhost:8080/api/public/errors/summary?range=24"
curl http://localhost:8080/api/public/site-config
curl http://localhost:8080/api/public/recommend
```

`/api/public/channels`、`/api/public/providers/rank`、`/api/public/errors/summary` 支持 `range=24|7|30|all`。传入 range 时，列表的评分、可用率、成功率、延迟和 `trendBuckets` 会按该时间范围聚合；`status`、L1/L2/L3 仍代表最新探测状态。

推荐点击埋点：

```bash
curl -X POST http://localhost:8080/api/public/recommend/click \
  -H "Content-Type: application/json" \
  -d '{"itemType":"cta","itemId":"hero-button"}'
```

## 浏览器会话 API

所有写操作需要先获取 CSRF Token，浏览器端由 `web/src/lib/api.ts` 自动完成。

```bash
curl -c cookies.txt http://localhost:8080/api/auth/csrf
curl -b cookies.txt -c cookies.txt -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: <csrfToken>" \
  -d '{"email":"admin@tokhub.local","password":"admin@tokhub.local"}'
```

核心接口：

- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/me`
- `POST /api/auth/verify-email`
- `POST /api/auth/forgot-password`
- `POST /api/auth/reset-password`
- `POST /api/auth/revoke-sessions`

## 用户和工作区 API

登录用户的个人能力：

- `GET /api/me/favorites`
- `PUT /api/me/favorites/{channelID}`
- `DELETE /api/me/favorites/{channelID}`
- `GET /api/me/private-channels`
- `POST /api/me/private-channels`
- `PATCH /api/me/private-channels/{channelID}`
- `DELETE /api/me/private-channels/{channelID}`
- `POST /api/me/private-channels/{channelID}/probe-now`
- `GET /api/me/ai-connection-providers`
- `GET/POST /api/me/ai-connections`
- `GET/DELETE /api/me/ai-connections/{connectionID}`
- `POST /api/me/ai-connections/{connectionID}/validate`
- `POST /api/me/ai-connections/{connectionID}/rotate`
- `POST /api/me/ai-connections/{connectionID}/quick-relay`
- `GET/POST /api/me/ai-browser-connectors`
- `DELETE /api/me/ai-browser-connectors/{connectorID}`
- `POST /api/me/ai-browser-connections`
- `GET /api/me/ai-connections/{connectionID}/browser-risk`
- `POST /api/me/ai-connections/{connectionID}/browser-risk/pause`
- `POST /api/me/ai-connections/{connectionID}/browser-risk/resume`
- `POST /api/me/ai-auth/step-up`
- `POST /api/me/ai-authorizations`
- `GET /api/me/ai-authorizations/google/callback`
- `GET /api/me/ai-authorizations/{authorizationID}`
- `POST /api/me/ai-authorizations/{authorizationID}/complete`
- `DELETE /api/me/ai-authorizations/{authorizationID}`
- `POST /api/me/ai-connections/{connectionID}/reauthorize`
- `POST /api/me/ai-connections/{connectionID}/disconnect`

### AI 开发者服务连接

AI 连接中心支持 OpenAI、Gemini、Kimi、DeepSeek、豆包、Claude 和千问的官方开发者 API 产品线。服务端 Provider Registry 固定地域和官方 API Endpoint，客户端不能提交任意上游地址。

认证方式按平台发布：

| 平台 | 认证方式 | 发布级别 | 说明 |
|---|---|---|---|
| Gemini | Google 官方 OAuth | stable，默认关闭 | 需要 Google OAuth 客户端与 Cloud Project ID |
| DeepSeek | 官方开放平台引导 + API Key | stable，随全局开关生效 | TokHub 打开官方密钥页面，用户返回后粘贴开发者 API Key |
| DeepSeek | 网页账号 userToken | experimental，默认关闭 | 用户登录 DeepSeek 网页版后导入 `userToken.value`，通过独立 DS2API 桥接为个人中转提供 OpenAI 兼容接口 |
| ChatGPT | Codex OAuth | experimental，默认关闭 | 自托管实验功能，固定消费者接口、个人范围、每秒 1 次、最多 2 并发、单连接 1 个中转 |
| ChatGPT、Gemini、DeepSeek | OpenCLI 本机浏览器 | experimental，默认关闭 | 网页登录态留在用户电脑，通过受限任务队列提供纯文本非流式个人中转 |
| 其余平台 | 官方开发者 API Key | stable | 沿用已有连接和安全轮换流程 |

系统不会采集服务商密码、短信验证码、完整浏览器 Cookie、`cf_clearance` 或其他 Local Storage 数据。DeepSeek 实验适配器只接受 `userToken` 中的 `value`，二次验证字段只校验当前 TokHub 登录密码。

OpenCLI 本机模式额外提供设备 API：

- `POST /api/ai-browser-connectors/pair`
- `POST /api/ai-browser-connectors/heartbeat`
- `POST /api/ai-browser-connectors/tasks/claim`
- `POST /api/ai-browser-connectors/tasks/{taskID}/complete`

配对接口使用 10 分钟、单次有效的随机码；其余设备接口使用配对后签发的 Bearer Device Token。Device Token 只返回一次，服务端保存 SHA-256 哈希。服务端任务白名单固定为 `status`、`ask`，服务商白名单固定为 `openai`、`gemini`、`deepseek`。

创建浏览器连接：

```json
POST /api/me/ai-browser-connections
{
  "connectorId": "aibc_...",
  "provider": "deepseek",
  "displayName": "我的 DeepSeek 网页",
  "models": ["deepseek-web"],
  "termsAckVersion": "opencli-personal-browser-experimental-v1"
}
```

创建请求会排入一次 `status` 任务，在线连接器通过 OpenCLI 识别已连接 Chrome Profile 中的当前账号。成功后，TokHub 保存加密的连接器引用、脱敏账号标识和设备令牌派生的账号指纹。每次 `ask` 前，本机连接器都会重新执行 `whoami` 并比对账号指纹；账号已切换时请求会以 `identity_mismatch` 终止，不会发送给模型。

AI 服务连接固定归属当前用户的个人工作区。`X-TokHub-Workspace` 和工作区查询参数不会改变连接归属。团队共享需要独立的授权、接受和撤销流程，当前版本没有开放。

创建连接：

```bash
curl -b cookies.txt -X POST http://localhost:8080/api/me/ai-connections \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: <csrfToken>" \
  -d '{
    "provider":"openai",
    "region":"global",
    "displayName":"我的 OpenAI",
    "apiKey":"<official_api_key>",
    "models":["gpt-5.5"],
    "confirmBillable":true
  }'
```

服务端会按产品能力验证模型列表，并为每个已配置模型发送一次最小生成请求，再使用版本化 AES-256-GCM 密钥环保存凭证。创建、复验和轮换请求都要显式提交 `confirmBillable:true`。响应只包含凭证 mask 和 HMAC 指纹关联信息。验证结果按模型保存；连接处于 `attention` 时，已经验证通过的模型仍可创建个人中转，失败模型继续隔离。同一用户、个人工作区、服务商 Endpoint 和凭证指纹不能重复创建连接。每位用户最多保存 32 个有效连接。

### OAuth 与开放平台引导

Gemini OAuth、ChatGPT Codex 和开放平台引导等敏感授权操作先调用 `POST /api/me/ai-auth/step-up`，提交当前 TokHub 密码。返回的 grant 与当前用户、登录 Session 绑定，10 分钟内只能使用一次。

随后调用 `POST /api/me/ai-authorizations`：

```json
{
  "provider": "gemini",
  "method": "oauth",
  "stepUpGrant": "<single-use-grant>",
  "displayName": "我的 Gemini",
  "projectId": "my-google-cloud-project",
  "models": ["gemini-2.5-pro"]
}
```

响应包含授权事务 ID、官方授权地址、完成模式、过期时间和建议轮询间隔。事务使用 PKCE S256、单次 state、Session 绑定和 Redis TTL。Google 回调会完成 Token Exchange、OIDC 签名与 claims 校验、模型验证和凭证加密。前端通过 `GET /api/me/ai-authorizations/{authorizationID}` 轮询结果。

ChatGPT Codex 的官方 CLI 回调固定为 `http://localhost:1455/auth/callback`。用户完成登录后，把浏览器最终地址提交到 `POST /api/me/ai-authorizations/{authorizationID}/complete`。服务端只接受固定 scheme、host、port 与 path，并核对单次 state。

DeepSeek 的 `api_key_guided` 流程先创建授权事务并打开 `https://platform.deepseek.com/api_keys`。用户创建 API Key 后，调用现有 `POST /api/me/ai-connections`，额外传入：

```json
{
  "authMethod": "api_key_guided",
  "authorizationId": "<authorization-id>"
}
```

DeepSeek 网页账号流程使用 `method: "deepseek_web_token"` 开始授权。请求必须确认实验条款：

```json
{
  "provider": "deepseek",
  "method": "deepseek_web_token",
  "displayName": "我的 DeepSeek 网页账号",
  "models": ["deepseek-v4-flash"],
  "termsAckVersion": "deepseek-web-session-experimental-v1"
}
```

该流程使用当前 TokHub 登录 Session、CSRF、个人工作区隔离、实验条款确认和授权频率限制，不要求在识别前重复输入 TokHub 密码。响应的 `completionMode` 为 `paste_token`，`authorizationUrl` 指向 `https://chat.deepseek.com`。

安装 TokHub DeepSeek Chrome 扩展后，用户点击“一键读取当前登录态”，扩展会在已打开的 DeepSeek 标签页中读取 `localStorage.userToken.value`，通过带随机请求 ID 的同源页面消息直接传给 TokHub。扩展只接受 `tokhub.me`、`www.tokhub.me` 及固定本地开发端口的请求，不读取 Cookie、密码或完整 Local Storage，也不持久化 Token。

未安装扩展时，用户可以从 DeepSeek Local Storage 的 `userToken` 对象复制 `value`，再提交：

```json
{
  "deepSeekToken": "<userToken.value>",
  "termsAckVersion": "deepseek-web-session-experimental-v1"
}
```

服务端拒绝 Cookie 形态、空白字符和超长输入，通过固定桥地址发送最小生成请求。只有真实验证成功的 Token 才会使用 AES-256-GCM 密钥环保存。桥接服务不会暴露宿主机端口，TokHub 也不会把 Token 写入日志、审计或响应。

受管授权凭证以 `oauth_bundle_v1` 保存，包含 Token、到期时间和最小账号标识。可续期的 OAuth 凭证由后台任务在到期前刷新，并按服务商执行独立的并发、QPS 和单次超时保护。DeepSeek `userToken` 没有公开续期协议，网关收到 401 后会把连接标记为 `reauth_required`，用户重新登录并导入新 Token 即可恢复。

受管授权连接通过 `POST /api/me/ai-connections/{connectionID}/disconnect` 断开，请求体提交当前 TokHub 登录密码。服务端先停用受管路由、吊销 Gateway Key 并擦除本地密文，再尝试调用服务商撤销接口；不支持远程撤销的 DeepSeek 网页登录态会返回 `unsupported` 并完成本地擦除。

安全轮换遵循“验证新凭证、事务替换旧凭证”的顺序。新凭证验证失败时，当前可用凭证保持不变，并写入拒绝轮换审计事件。

一键创建个人中转：

```bash
curl -b cookies.txt -X POST \
  http://localhost:8080/api/me/ai-connections/<connection_id>/quick-relay \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: <csrfToken>" \
  -H "Idempotency-Key: ai-relay-<uuid>" \
  -d '{
    "modelIds":["<connection_model_id>"],
    "name":"个人 OpenAI 中转",
    "policy":"latency",
    "qpsLimit":20,
    "quotaMonth":100000
  }'
```

同一用户、个人工作区和 `Idempotency-Key` 在 10 分钟内返回同一个网关和同一个一次性 Gateway Key。请求内容变化会返回 `idempotency_conflict`。Gateway Key 完整值只在创建结果或有效期内的幂等重放中展示。

`quotaMonth` 沿用现有网关字段名，当前语义是 Gateway Key 的累计请求次数上限。

删除连接会立即擦除凭证密文和未过期的一次性密钥密文，停用受管通道。失去全部可用路由的关联中转站会被暂停，其 Gateway Key 会被吊销。

连接密钥环配置：

- `TOKHUB_CREDENTIAL_ACTIVE_KEY_ID`
- `TOKHUB_CREDENTIAL_ENCRYPTION_KEYS`，格式为 `enc-v1:<secret>,enc-v2:<secret>`
- `TOKHUB_CREDENTIAL_ACTIVE_FINGERPRINT_KEY_ID`
- `TOKHUB_CREDENTIAL_FINGERPRINT_KEYS`，格式为 `fp-v1:<secret>,fp-v2:<secret>`

生产预检要求同时配置独立的加密密钥环和指纹密钥环。生产运行时不会回落到 `TOKHUB_SECRET_KEY`，两个密钥环也不能使用相同的密钥材料。轮换时先加入新 Key ID 并切换 active ID，历史 Key 保留到全部旧凭证完成重加密。

生产环境应使用独立的加密密钥和指纹密钥，并保留旧 Key ID 直到关联凭证完成轮换。`/metrics` 额外暴露授权事务状态、OAuth 连接状态和当前连续刷新失败数，标签只包含 provider、method 和状态。

用户/企业工作区能力：

- `GET/POST /api/console/gateways`
- `GET/POST /api/console/gateway-keys`
- `POST /api/console/gateway-keys/{keyID}/revoke`
- `GET /api/console/members`
- `GET /api/console/usage`
- `POST /api/console/usage/rollup/recompute`
- `GET/POST /api/console/alerts`
- `GET /api/console/audit`
- `GET /api/console/audit/export`
- `GET /api/console/incidents`
- `POST /api/console/incidents`
- `PATCH /api/console/incidents/{incidentID}`
- `POST /api/console/incidents/{incidentID}/resolve`
- `POST /api/console/incidents/{incidentID}/reopen`
- `DELETE /api/console/incidents/{incidentID}`
- `POST /api/console/incidents/bulk`
- `GET /api/console/governance/summary`

安全规则：

- 私有通道 Key 永不通过 API 明文返回。
- Gateway Key 列表只展示 mask；完整 Key 只在创建响应展示一次，后续只能轮换或重新签发。
- AI 连接接受官方开发者 API Key、已启用的官方 OAuth、显式开启的 ChatGPT Codex OAuth，以及显式开启的 DeepSeek `userToken.value`。密码、验证码、浏览器 Cookie、完整 Local Storage、`cf_clearance` 和 PoW 数据会被产品策略拒绝。
- 受管通道只引用 `ai_connection_id`，凭证密文在连接密钥表集中保存，不复制到通道凭证表。
- `/api/console/*` 必须按当前用户工作区过滤。
- 普通用户不应依赖 `/api/admin/*`。

## 平台管理 API

平台管理员能力：

- `GET /api/admin/channels`
- `POST /api/admin/channels/export`
- `POST /api/admin/channels/import`
- `POST /api/admin/channels/sync`
- `POST /api/admin/channels/{channelID}/probe-now`
- `GET/POST /api/admin/gateways`
- `PATCH /api/admin/gateways/{gatewayID}`
- `DELETE /api/admin/gateways/{gatewayID}`
- `POST /api/admin/gateways/bulk`
- `GET/POST /api/admin/members`
- `PATCH /api/admin/members/{userID}`
- `DELETE /api/admin/members/{userID}`
- `POST /api/admin/members/bulk`
- `GET/POST /api/admin/gateway-keys`
- `PATCH /api/admin/gateway-keys/{keyID}`
- `POST /api/admin/gateway-keys/{keyID}/revoke`
- `DELETE /api/admin/gateway-keys/{keyID}`
- `POST /api/admin/gateway-keys/bulk`
- `GET/PATCH /api/admin/settings`
- `GET /api/admin/recommend`
- `PUT /api/admin/recommend`
- `GET /api/admin/open-api`
- `POST /api/admin/open-api/sites`
- `GET/PATCH /api/admin/web`
- `GET /api/admin/usage`
- `POST /api/admin/usage/rollup/recompute`
- `GET/POST /api/admin/alerts`
- `GET /api/admin/audit`
- `GET /api/admin/audit/export`
- `GET /api/admin/incidents`
- `POST /api/admin/incidents`
- `PATCH /api/admin/incidents/{incidentID}`
- `POST /api/admin/incidents/{incidentID}/resolve`
- `POST /api/admin/incidents/{incidentID}/reopen`
- `DELETE /api/admin/incidents/{incidentID}`
- `POST /api/admin/incidents/bulk`
- `GET /api/admin/governance/summary`

`PUT /api/admin/recommend` 保存推荐运营配置，当前 payload 包含：

- `picks`：TOP3/精选推荐位，支持新增、编辑、排序、启停、复制和删除。
- `rewards`：新人福利模板，支持新增、编辑、启停、复制和删除。
- `scenarios`：场景推荐，支持新增、编辑、排序、启停、复制和删除。
- `rankRules`：多维度榜单规则，支持新增、编辑标题/指标/说明、排序、启停、复制和删除。前台 `/recommend` 只展示启用的榜单规则。

兼容规则：如果请求省略 `rankRules` 字段，后端保留现有榜单规则；如果传入 `"rankRules":[]`，后端会清空榜单规则。

`POST /api/admin/channels/export`、`POST /api/admin/channels/import` 和 `POST /api/admin/channels/sync` 只用于平台通道迁移或同步。浏览器管理员调用时要求当前管理员登录密码二次验证；admin-agent 调用时要求对应高权限 scope、`X-TokHub-Agent-Reason` 和 `X-Idempotency-Key`。导出 CSV 和通道 API 同步都可能处理平台通道明文 `api_key`，响应禁止缓存，下载文件或 Site Key 必须按密钥材料保管。导入 CSV 中的 `id` 已存在则更新平台通道，`id` 为空或不存在则创建通道；任意一行校验失败时整批不落库。`/api/admin/channels/sync` 会调用源站 `/v1/status/channel-sync?includeCredentials=1`，复用同一批量导入逻辑，并尽量保留源站最新监控快照。

平台后台用于站点级运营和治理。`/admin/gateways`、`/admin/members` 和 `/admin/gateway-keys` 管理平台默认组织的网关、成员和 Key；普通用户/企业自己的工作区网关、成员和 Key 使用 `/api/console/*`。

`PATCH /api/admin/settings` 是局部更新接口。省略字段表示保持原值；显式传入空字符串表示写入空值并走统一校验，因此可选字段如 `subtitle`、`footerText` 可以被清空，必填字段如 `brandName`、`logoMark` 仍会被拒绝为空。

## 第三方状态 Open API

正式对外路径只使用 `/v1/status/*`。鉴权方式任选其一：

```bash
curl http://localhost:8080/v1/status/overview -H "X-Site-Key: <site_key>"
curl http://localhost:8080/v1/status/channels -H "Authorization: Bearer <site_key>"
```

端点：

- `GET /v1/status/overview`
- `GET /v1/status/channels`
- `GET /v1/status/channels/{channelID}`
- `GET /v1/status/channel-sync`
- `GET /v1/status/uptime`
- `GET /v1/status/incidents`

Site Key 在 `/admin/open-api` 创建。每个 Site Key 可配置 scope 和 QPS，调用日志写入后台 Open API 页面。

`GET /v1/status/channel-sync` 需要单独勾选 `channel_sync` scope。默认只返回配置和 key mask；目标站同步会显式请求 `?includeCredentials=1`，此时响应包含平台通道明文 `apiKey`、官网 URL、价格配置、providerConfig、当前监控快照和最近探测日志。该 Site Key 不应和普通公开状态页共用。

## 专属网关 API

Gateway Key 在 `/console/keys` 创建。完整 Key 只在创建时展示一次；列表默认只显示 mask，忘记后需要轮换或重新签发。调用方式：

```bash
curl http://localhost:8080/gateway/v1/models \
  -H "Authorization: Bearer <gateway_key>"
```

Chat Completions:

```bash
curl -X POST http://localhost:8080/gateway/v1/chat/completions \
  -H "Authorization: Bearer <gateway_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}],"stream":false}'
```

Responses:

```bash
curl -X POST http://localhost:8080/gateway/v1/responses \
  -H "Authorization: Bearer <gateway_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","input":"ping","stream":false}'
```

网关支持 QPS 限流、配额、熔断、首字节前 failover 和 SSE 流式透传。流式响应开始后不会切换上游。

## 冒烟检查

本地或生产服务启动后可运行：

```bash
TOKHUB_BASE_URL=http://localhost:8080 npm run test:smoke
```

带第三方 Key 的完整冒烟：

```bash
TOKHUB_BASE_URL=https://tokhub.example.com \
TOKHUB_SITE_KEY=<site_key> \
TOKHUB_GATEWAY_KEY=<gateway_key> \
npm run test:smoke
```
