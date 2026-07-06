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
| `/api/me/*` | 登录用户 | Cookie + CSRF | 收藏和个人私有通道 |
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
curl "http://localhost:8080/api/public/channels?dimension=brand&status=healthy&page=1&pageSize=50"
curl http://localhost:8080/api/public/channels/ch_openai_gpt4o
curl "http://localhost:8080/api/public/channels/ch_openai_gpt4o/series?days=7"
curl http://localhost:8080/api/public/providers/rank
curl http://localhost:8080/api/public/errors/summary
curl http://localhost:8080/api/public/site-config
curl http://localhost:8080/api/public/recommend
```

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
