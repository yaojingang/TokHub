# TokHub Admin-Agent API

本文件是 TokHub 管理员 skill 和内部自动化使用的机器执行合同。它不属于第三方公开稳定 API；公开状态和网关合同仍以 `docs/openapi.yaml` 为准。

## 开关和鉴权

`TOKHUB_ADMIN_AGENT_ENABLED` 控制整个 admin-agent 通道：

- development 默认启用。
- production 默认关闭。
- 生产回滚时关闭该变量并吊销相关 token。

管理员 agent 使用已有 `/api/admin/*` 路由：

```bash
curl "$TOKHUB_BASE_URL/api/admin/production-health" \
  -H "Authorization: Bearer $TOKHUB_ADMIN_AGENT_TOKEN"
```

浏览器管理员仍使用 Cookie + CSRF。Bearer 请求不需要 CSRF，但写操作必须带：

- `X-TokHub-Agent-Reason`：3-240 字符，说明本次执行原因。
- `X-Idempotency-Key`：8-120 个非空白字符，同一个 token 下重复 key 会被拒绝。

## Token 生命周期

只有 owner 角色可以通过浏览器 Cookie + CSRF 管理 admin-agent token：

- `GET /api/admin/agent-tokens`
- `POST /api/admin/agent-tokens`
- `POST /api/admin/agent-tokens/{tokenID}/revoke`

创建 token 时只返回一次 `plainToken`。列表和 revoke 响应只返回 `tokenPrefix`、`tokenMask`、scope、创建人和时间字段，不返回明文 token。

Bearer token 不能管理 `/api/admin/agent-tokens`，避免 token 链式扩散。

创建示例：

```bash
curl -b cookies.txt -c cookies.txt -X POST "$TOKHUB_BASE_URL/api/admin/agent-tokens" \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -d '{"name":"codex-admin","scopes":["admin:*"],"ttlHours":24}'
```

## Scopes

支持 scope：

- `admin:read`：普通管理读取。
- `admin:write`：普通新增、编辑、探测、验证、评估、重算。
- `admin:dangerous`：删除、吊销、禁用、reset、bulk、构建/下载包含密钥的包、其他不可逆或半不可逆操作。
- `admin:secrets`：创建或轮换会返回/影响密钥的操作。
- `admin:export`：CSV、包下载等导出。

创建时允许传入 `admin:*`，后端会展开为以上全部运行时 scope。

## 审计

agent 写操作进入现有 `audit_events`：

- `actor_type=agent`
- `actor_id=<admin_agent_token_id>`
- metadata 包含 `delegated_user_id`、`delegated_user_email`、`agent_token_id`、`agent_token_name`、`agent_reason`、`idempotency_key`
- 如果业务代码原本写入 `actor_type=user` / `actor_id=<user_id>`，会保存在 `delegated_actor_type` / `delegated_actor_id`

审计 metadata 不应写入任何 token、gateway key、site key 或 provider key 明文。

## 常用执行

只读健康检查：

```bash
curl "$TOKHUB_BASE_URL/api/admin/production-health" \
  -H "Authorization: Bearer $TOKHUB_ADMIN_AGENT_TOKEN"
```

触发通道验证：

```bash
curl -X POST "$TOKHUB_BASE_URL/api/admin/channels/ch_123/validate" \
  -H "Authorization: Bearer $TOKHUB_ADMIN_AGENT_TOKEN" \
  -H "X-TokHub-Agent-Reason: validate provider after key rotation" \
  -H "X-Idempotency-Key: validate-ch_123-20260702-001"
```

下载 channel site 包需要 `admin:read`、`admin:dangerous`、`admin:secrets`、`admin:export`，并需要 reason/idempotency key。

平台通道 CSV 导入/导出和通道 API 同步都可能处理 provider key 明文。浏览器管理员仍必须输入当前登录密码完成二次验证；admin-agent Bearer token 可调用 `/api/admin/channels/export`、`/api/admin/channels/import` 和 `/api/admin/channels/sync`，但必须具备对应 `admin:secrets`、`admin:dangerous`、`admin:export` 等 scope，并提供 reason 与 idempotency key。导出文件和同步源 Site Key 必须按密钥材料保管。

## 回滚

1. 设置 `TOKHUB_ADMIN_AGENT_ENABLED=false` 并重启服务。
2. 用 owner 浏览器会话调用 revoke 接口吊销 token。
3. 保留数据库表即可；它们不会影响浏览器管理员后台。
