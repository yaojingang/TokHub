# AI 账号授权与个人中转运行手册

本文适用于 TokHub `2.0.0-rc.2`。生产个人网关支持官方 API、Gemini Google Cloud OAuth、ChatGPT Codex 官方客户端和 Grok Build 官方客户端。本机实验室包含 OpenCLI、DeepSeek 网页 Session 与旧 Codex OAuth，实验连接无法生成生产 Gateway Key。

## 支持边界

| 平台 | 生产允许 | 实验室允许 | 生产模型入口 |
| --- | --- | --- | --- |
| ChatGPT | API Key、Codex 官方客户端 | OpenCLI、旧 Codex OAuth | API 模型或 `chatgpt-personal` |
| Gemini | API Key、Google Cloud OAuth | OpenCLI | Gemini API 模型 |
| DeepSeek | API Key | OpenCLI、网页 Session | DeepSeek API 模型 |
| Grok | API Key、Grok Build 官方客户端 | 无网页 Cookie 模式 | API 模型或 `grok-personal` |

Kimi、豆包、Claude 与千问继续使用官方 API Key。

## RC2 安全迁移

迁移 `0051_provider_policy_safety.sql` 执行以下一次性处理：

1. 将现有 `codex_oauth` 与 `deepseek_web_token` 连接改为 `disabled`。
2. 清除 Access Token、Refresh Token、ID Token、`userToken` 和相关密文载荷。
3. 保留连接名称、账号掩码、模型、审计和用量记录。
4. 禁用这些连接生成的托管 Channel，保留 Gateway Key。
5. 保留 `opencli_browser` 设备引用，并在生产 `/gateway/v1/*` 拒绝其任务。

凭据清理无法通过代码回滚恢复。执行迁移前需要完成变更审批和数据库备份。备份仅用于灾难恢复，不用于把已清理的消费者 Token 重新投入生产。

DS2API 已从默认 Compose 与生产 Helm 路径移除。只有显式 `lab` profile 或 `aiAuthorization.labMode=true` 且实验开关与风险确认同时满足时，部署系统才会创建桥服务。

## Provider Policy

代码内置策略包含允许的生产模式、实验模式、条款 URL、条款摘要、复核时间、身份要求和 Kill Switch。连接目录、新建连接、快速中转和网关执行都会检查同一策略。

官方客户端策略每 90 天复核一次。RC2 的复核到期日为 `2026-11-17`。到期后官方客户端与消费者实验任务停止，官方 API Key 路径继续运行。

运维系统可把最新条款摘要写入以下变量。值与代码内置摘要不一致时，委托账号模式立即停止：

```env
TOKHUB_AI_OPENAI_TERMS_DIGEST=
TOKHUB_AI_GEMINI_TERMS_DIGEST=
TOKHUB_AI_DEEPSEEK_TERMS_DIGEST=
TOKHUB_AI_GROK_TERMS_DIGEST=
```

紧急停止变量：

```env
TOKHUB_AI_OPENAI_KILL_SWITCH=false
TOKHUB_AI_GEMINI_KILL_SWITCH=false
TOKHUB_AI_DEEPSEEK_KILL_SWITCH=false
TOKHUB_AI_GROK_KILL_SWITCH=false
```

Kill Switch 关闭对应平台的委托账号、OAuth 或实验连接。该平台的官方 API Key 路径保持可用。

## 服务端配置

生产启用官方客户端：

```env
TOKHUB_AI_WEB_AUTH_ENABLED=true
TOKHUB_AI_OFFICIAL_CLIENT_ENABLED=true
TOKHUB_AI_OFFICIAL_CLIENT_TASK_TIMEOUT=3m
TOKHUB_AI_LAB_MODE=false
```

依赖项：

- PostgreSQL 已执行 `0051` 与 `0052` 迁移。
- Redis 具备持久连接与受控内存，官方客户端载荷存储失败时任务安全关闭。
- 凭据加密密钥环与指纹密钥环使用独立材料。
- 生产 `TOKHUB_PUBLIC_URL` 使用 HTTPS。
- 反向代理保留请求上下文，客户端断开时 Go 请求 Context 可以及时取消。

Gemini OAuth 继续使用：

```env
TOKHUB_AI_GEMINI_OAUTH_ENABLED=true
TOKHUB_GOOGLE_OAUTH_CLIENT_ID=
TOKHUB_GOOGLE_OAUTH_CLIENT_SECRET=
TOKHUB_GOOGLE_OAUTH_PROJECT_ID=
```

Google Cloud OAuth 回调地址固定为：

`https://<TOKHUB_PUBLIC_URL>/api/me/ai-authorizations/google/callback`

## 官方客户端镜像

构建镜像：

```bash
docker build -t tokhub-client-connector:2.0.0-rc.2 \
  -f deploy/official-client-connector/Dockerfile .
```

镜像依赖：

- `@openai/codex@0.148.0`，由 npm 锁文件固定。
- Grok Build `1.0.5`，由 xAI 官方下载地址与架构 SHA256 固定。

xAI 当前公共 npm Registry 没有可验证的 `@xai-official/grok@0.1.4`。RC2 采用 xAI 官方二进制分发，AMD64 与 ARM64 摘要记录在 `deploy/official-client-connector/checksums.txt`。

Compose 安全合同：

- `read_only: true`
- `user: 10001:10001`
- `cap_drop: [ALL]`
- `no-new-privileges:true`
- `/tmp` 与 `/workspace` 使用 `noexec,nosuid,nodev` tmpfs
- 只挂载 Codex、Grok、Connector 三个命名卷
- 禁止 `$HOME`、Chrome Profile、项目目录和 Docker Socket 挂载

运行 `docker inspect` 或 `podman inspect` 时，Mounts 列表应只包含这三个命名卷。

## 用户配对与登录

1. 用户进入“个人空间 > AI 服务连接 > 官方账号本地连接”。
2. 点击“添加官方客户端”，获得 10 分钟一次性配对码。
3. 在专用容器中执行配对：

```bash
cd deploy/official-client-connector
docker compose run --rm connector pair \
  --server https://tokhub.example.com \
  --code <pairing-code>
```

4. 登录 ChatGPT：

```bash
docker compose run --rm connector login chatgpt
```

该命令执行 Codex Device Auth。用户在官方页面完成登录。

5. 登录 Grok：

```bash
docker compose run --rm connector login grok
```

Grok 官方 CLI 负责登录流程。当前官方命令为 `grok login`。

6. 诊断与持续运行：

```bash
docker compose run --rm connector doctor
docker compose up -d connector
docker compose logs -f connector
```

7. 返回 TokHub，选择在线连接器并创建 ChatGPT 或 Grok 官方客户端连接。

连接器支持：

```text
doctor
pair
login chatgpt|grok
run
logout chatgpt|grok|all
sessions clean
```

`sessions clean` 只删除超过 24 小时的 Codex 与 Grok 会话文件，不删除认证文件。`run` 在启动时和每 24 小时自动执行同一清理。

## 设备签名

首次配对在容器的 Connector 命名卷生成 Ed25519 密钥。服务端保存设备公钥和 Token 摘要，私钥与设备 Token 保持在权限 `0600` 的容器卷文件中。

签名正文包含：

```text
HTTP method
request path
SHA256(body)
unix timestamp
random nonce
```

服务端拒绝以下请求：

- 时间偏差超过 60 秒。
- Nonce 已在 Redis 中出现。
- Body、方法或路径与签名不一致。
- 设备已撤销或工作区成员关系失效。
- 生产环境使用 HTTP。

开发环境只允许 loopback HTTP。

## Codex 驱动

连接器通过 Codex JSON-RPC 调用：

- `initialize`
- `account/read`
- `model/list`
- `thread/start`
- `thread/resume`
- `turn/start`

`clientInfo.name` 固定为 `tokhub_official_connector`。工作目录为空目录，sandbox 固定为只读，Approval Policy 固定为 `never`。连接器关闭 Web Search、Apps、Browser、Computer、Shell 和统一执行能力。

`account/read` 当前协议提供 ChatGPT 邮箱与套餐信息。TokHub 将邮箱转换为加密指纹并只展示脱敏值。协议缺少稳定账号 ID 时，账号保证级别仍标记为账户级，后续登录需要匹配同一指纹。

## Grok 驱动

连接器通过 Grok Build ACP 调用：

- `initialize`
- `authenticate`，方法固定为 `cached_token`
- `session/new`
- `session/load`
- `session/prompt`
- `session/cancel`

MCP Server 列表为空。`/etc/grok/requirements.toml` 由 root 拥有，配置 `deny = ["*"]`，同时关闭 Subagent、Memory、Web Search、自动更新、遥测、反馈、LSP 和代码索引。

ACP 返回稳定账号主体时，TokHub 使用账户级身份。协议缺少稳定主体时，连接采用设备级身份；账号切换后需要重新配对。

## 任务与会话

数据库表 `ai_client_tasks` 只保存设备、连接、平台、动作、状态、租约摘要、错误分类和有效期。Prompt 与结果使用 AES-256-GCM 加密后写入 Redis：

1. TokHub 创建任务元数据与 Redis 加密载荷。
2. 连接器领取任务后，Prompt 从 Redis 删除。
3. 连接器执行官方客户端调用。
4. 成功结果加密写入 Redis。
5. 调用方读取结果后立即删除。
6. 超时载荷依靠短 TTL 清理。

Redis 不可用时任务创建、领取或完成安全关闭。监控中的 `tokhub_ai_client_payloads_residual` 正常值为 0。

`/v1/responses` 多轮规则：

- 首次响应返回 TokHub `resp_*` ID。
- 后续请求用 `previous_response_id` 恢复同一官方客户端会话。
- Response ID 绑定相同用户、Gateway Key、连接和模型。
- 跨用户、跨 Key、跨连接或跨模型引用返回 404。
- 过期会话返回 `409 session_expired`。
- 空闲 24 小时清理，创建 7 天后强制清理。
- 服务端只保存设备密封后的会话引用、所有权和有效期。
- Chat Completions 继续要求调用方提交完整 `messages`。
- 客户端断开时，服务端取消任务，连接器终止官方客户端进程；未完成结果无法成为后续会话节点。

支持文本流式与非流式响应。图片、音频、工具、函数调用和 Anthropic Messages 返回 `422 unsupported_capability`。

## 账号保护状态机

固定额度：

- 每个用户工作区一个活动连接器。
- 每个平台一个官方客户端连接。
- 每个连接器一个活动任务。
- 最小请求间隔 15 秒。
- 每小时最多 20 次。
- 近 24 小时最多 80 次。
- TokHub 零重试。

系统不执行多账号池、账号轮换、代理切换和备用账号接管。

| 事件 | 状态 | 恢复方式 |
| --- | --- | --- |
| 401、登录失效 | `reauth_required` | 官方客户端重新登录，验证原账号，再由用户手动恢复 |
| 403、安全挑战 | `security_locked` | 无限期锁定，管理员审查 |
| 身份变化 | `security_locked` | 无限期锁定，禁止自动迁移到新账号 |
| 首次 429 | `cooldown` | 等待 `max(Retry-After, 1 小时)` |
| 24 小时内第二次 429 | `manual_recovery` | 人工确认后恢复 |
| 超时或 5xx | `cooldown` | 5 分钟后自动恢复 |
| 工具、文件或命令事件 | `policy_locked` | 管理员审查客户端与策略 |

暂停只允许从正常状态进入 `paused`。用户恢复可以解除 `paused` 与人工确认后的 `manual_recovery`，无法解除 `security_locked` 和 `policy_locked`。

这些控制降低高频自动化、异常切换与重试放大的暴露面。服务商可以依据条款、套餐、异常行为与安全策略限制账号，系统不提供零封号风险承诺。

## 监控与告警

新增 Prometheus 指标：

- `tokhub_ai_client_connectors_online{connector_version,codex_version,grok_version}`
- `tokhub_ai_client_tasks_total{provider,status,outcome}`
- `tokhub_ai_client_sessions_total{provider,status,resumed}`
- `tokhub_ai_client_risk_connections{provider,state,identity_assurance}`
- `tokhub_ai_client_identity_changes_total{provider,state,identity_assurance}`
- `tokhub_ai_client_payload_store_available`
- `tokhub_ai_client_payloads_residual`
- `tokhub_ai_provider_policy_info{provider,policy_version,review_state,review_expires_at,terms_digest_state}`
- `tokhub_ai_provider_kill_switch{provider}`

指标与日志不包含 Prompt、回答、Token、Cookie、账号主体和完整 IP。

建议告警：

- 加密临时载荷存储不可用超过 1 分钟。
- `tokhub_ai_client_payloads_residual > 0` 持续 5 分钟。
- 策略 `review_state="expired"` 或 `terms_digest_state="changed"`。
- 任一平台出现 403、安全挑战、身份变化或工具拦截。
- 同一平台 429 在 24 小时内重复出现。
- 连接器离线超过 5 分钟或版本偏离固定镜像。
- `security_locked`、`policy_locked`、`reauth_required` 持续增长。

## 实验室

实验室需要同时设置：

```env
TOKHUB_AI_LAB_MODE=true
```

OpenCLI 和 DeepSeek 网页 Session 还需要各自的实验开关与精确风险确认。实验连接在 Provider Policy 中拥有独立白名单，生产 Gateway 执行层始终拒绝 `opencli_browser`、`deepseek_web_token` 和旧 `codex_oauth`。

DS2API Compose 使用：

```bash
docker compose --profile lab up deepseek-web-bridge
```

Helm 需要 `aiAuthorization.labMode=true`、`deepseekWeb.enabled=true` 与 `deployBridge=true`。生产 values 保持三者关闭。

## 发布验收

自动质量门：

```bash
go test -count=1 ./...
npm run typecheck
npm run build
./deploy/scripts/security-scan.sh
./deploy/scripts/preflight.sh
./deploy/scripts/version-check.sh
docker compose config
docker compose -f deploy/official-client-connector/compose.yaml config
helm template tokhub deploy/helm/tokhub
```

真实账号验收需要一个可用 ChatGPT Codex 套餐账号、一个可用 Grok Build 账号以及 Docker Desktop 或 Podman。依次验证：

1. 配对与 Device Auth。
2. 账号识别与模型检查。
3. 非流式文本。
4. 流式文本。
5. 连续三轮 Responses。
6. 连接器重启后的会话恢复。
7. 登出后的 401 与重新登录流程。
8. 会话删除、24 小时空闲过期与 7 天强制过期。
9. 客户端断开后的 Turn 中断。
10. 安全挑战后零重试、零账号切换。
11. 容器 Mounts 中不存在宿主机目录。

## 回滚

三个交付阶段应保持独立提交：安全收敛、ChatGPT 官方客户端、Grok 与产品化。代码和部署可以按阶段回滚。`0051` 已擦除的消费者 Token 不会恢复。回滚后，用户需要重新建立符合当前生产策略的连接。
