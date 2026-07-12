# TokHub

TokHub 是面向 AI API 中转站的开源监控、推荐运营与 OpenAI 兼容专属网关系统。它把公开状态页、供应商排行、用户工作区、平台管理后台、分层探测、用量计量、告警审计、密钥加密和 Docker 自托管部署放在同一个系统里，适合用来搭建 AI API 服务导航、可用性监控平台、企业内部专属网关或多上游容灾入口。

English: [README.en.md](docs/README.en.md)

## 它解决什么问题

AI API 中转站、模型服务商和企业自建上游通常会遇到几类问题：

- 公开页面只能展示“可用”或“不可用”，但不知道是 DNS、TLS、鉴权、模型列表还是生成链路出了问题。
- 用户有自己的私有 Key 和上游地址，却缺少统一的健康监控、配额、网关和审计。
- 平台推荐页依赖人工整理，缺少可复用的榜单规则、推荐位、点击统计和公开 Open API。
- 企业想用一个 OpenAI 兼容入口接入多个上游，但需要按延迟、成功率、成本做路由，并记录每次请求的用量和费用。
- 自托管部署不应只给源码，还需要生产预检、备份恢复、无演示数据检查、安全扫描和发布门禁。

TokHub 的目标是把这些能力做成一个可运行、可部署、可二次开发的开源基础系统。

## 核心能力

### 公开监控和推荐前台

- 公开首页、通道列表、通道详情、供应商排行和精选推荐。
- 支持按品牌、模型、状态、价格、延迟、成功率等维度组织展示。
- 前台推荐页由后台配置驱动，支持精选位、新人福利、场景推荐和多套榜单规则。
- 提供 `/api/public/*` 公开数据接口和 `/v1/status/*` 第三方只读 Open API。
- 支持生成独立通道站点资产，便于把公开监控和推荐能力拆给不同站点使用。

### 用户工作区

- 用户可以收藏公开通道，也可以创建自己的私有通道。
- 私有通道支持 Endpoint、模型、额度、状态、立即探测和连接测试。
- 用户工作区包含专属网关、Gateway Key、成员、用量、告警、事件和审计。
- 工作区数据按组织隔离，普通用户不能访问平台后台和其它工作区资源。

### 平台管理后台

- 管理平台通道、私有通道、用户、组织、成员、Gateway Key、Open API 站点和推荐运营配置。
- 支持通道 CSV 导入导出、通道同步、批量启停、批量删除和二次密码验证。
- 支持全局用量报表、请求事件、成本估算、审计导出和治理概览。
- 支持站点配置、前台文案、模型目录和价格配置的后台维护。

### OpenAI 兼容专属网关

- 对外暴露 `/gateway/v1/*`，兼容 OpenAI 风格的 Models 和 Chat Completions 调用。
- 每个网关可以绑定多个平台上游或用户私有上游。
- Gateway Key 支持 QPS、月配额、状态管理、撤销、删除和一次性明文展示。
- 兼容非流式和流式响应，记录请求模型、上游通道、状态码、Token、延迟、成本和错误类型。

## 探测和健康算法

TokHub 把通道健康拆成三层，不把“接口能连上”和“模型真的能生成”混为一谈。

### L1 连通性探测

L1 负责基础网络链路：

- 解析 Endpoint URL。
- DNS 解析目标主机。
- 建立 TCP 连接。
- 对 HTTPS 目标执行 TLS 握手，并记录证书过期时间。
- 发起 HTTP HEAD 请求，判断入口是否可达。

L1 能定位 `dns_failed`、`tcp_failed`、`tls_failed`、`http` 层错误和坏 Endpoint。

### L2 模型可用性探测

L2 调用上游 `/models`，验证：

- API Key 是否有效。
- 上游是否返回可解析的模型列表。
- 当前配置的模型是否存在或可用。
- 部分供应商可按 provider profile 跳过模型列表探测。

L2 会把 401、403 识别为 `auth_error`，把模型缺失识别为 `model_not_found`。

### L3 真实生成探测

L3 发起最小 Chat Completions 请求，提示词要求模型只返回固定内容，用来验证真实推理链路：

- 记录总延迟、首 Token 估算、HTTP 状态、Token 用量和成本。
- 校验生成内容是否符合预期，避免“HTTP 成功但模型没有正常生成”的假阳性。
- 对慢响应、限流、空内容、鉴权失败和模型不可用分别归类。

### 状态合成

系统会把 L1、L2、L3 的结果合成通道状态：

- `healthy`：网络、模型和生成链路正常。
- `degraded`：仍可用，但存在慢响应、限流、模型探测异常或局部网络问题。
- `connectivity_down`：基础连接或模型列表链路不可达。
- `functional_down`：网络可能可达，但真实生成链路失败。
- `auth_error`：上游凭据失效或权限不足。
- `unknown`：探测数据不足。

健康评分会结合当前状态和成功率生成，快照会记录 24 小时可用率、成功率、P95 延迟、L1/L2/L3 延迟、Token 和成本。

## 网关路由算法

专属网关会先读取网关绑定的上游，再生成候选路由：

1. 跳过未启用上游。
2. 优先过滤 `connectivity_down`、`auth_error`、`functional_down` 等故障上游。
3. 如果全部候选都故障，则退回到所有启用上游，避免空路由。
4. 按网关策略排序。
5. 跳过短期熔断中的通道。
6. 把本次路由计划写入 Redis，便于观测和后续扩展。

支持三种策略：

- `latency`：按 P95 延迟从低到高排序，同分时健康评分高的优先。
- `success`：按成功率从高到低排序，同分时健康评分高的优先。
- `cost`：按成本从低到高排序，同分时健康评分高的优先。

Redis 还承担 Gateway QPS 秒级桶、通道短期熔断标记和路由计划缓存。即使 Redis 不可用，服务也会降级到内存熔断和数据库路由，不直接中断核心网关能力。

## 安全和加密

TokHub 默认把密钥材料当作生产数据处理。

- 上游 API Key、私有通道 Key 和通知目标使用 AES-GCM 加密保存。
- 主密钥由 `TOKHUB_SECRET_KEY` 派生，生产环境要求至少 32 字符。
- 每次加密使用随机 nonce，数据库保存 ciphertext、nonce、mask 和 fingerprint。
- Gateway Key 使用 `sk-th-` 前缀随机生成，服务端保存 SHA-256 哈希、短前缀和 mask。
- 完整 Gateway Key 只在创建响应中展示一次，后续只能轮换或重新签发。
- 登录密码使用 bcrypt 保存，Session Token 只保存哈希。
- 浏览器写操作使用 Cookie + CSRF Token 双重校验。
- 生产环境要求 `TOKHUB_SESSION_SECURE=true`，避免明文 Cookie。
- 官网抓取和通道介绍解析会阻断 localhost、内网、链路本地、组播、保留地址和文档网段，降低 SSRF 风险。
- 删除通道、删除用户和治理动作会清理或擦除相关密钥材料，并写入审计事件。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go、go-chi、pgx、sqlc、bcrypt |
| 前端 | React、Vite、TypeScript、React Router、Radix UI |
| 数据库 | PostgreSQL、TimescaleDB、迁移 SQL、sqlc 生成查询 |
| 缓存和限流 | Redis |
| 事件和任务扩展 | NATS |
| 探测和网关 | L1/L2/L3 Probe、OpenAI 兼容网关、Anthropic/Gemini/OpenAI 适配 |
| 部署 | Dockerfile、Docker Compose、分 role Compose、Helm 模板 |
| 验证 | Go test、go vet、TypeScript、Vite build、Playwright、发布脚本和安全扫描 |

## 架构特点

### 单入口，多角色

后端只有一个 Go 入口 `cmd/tokhub`，通过 `TOKHUB_ROLE` 切换运行角色：

- `all`：单进程运行 Web、API、Gateway、探测和任务能力，适合本地和小团队自托管。
- `api`：只运行公开前台、用户控制台、平台后台和 Open API。
- `gateway`：只运行 OpenAI 兼容专属网关。
- `prober`：运行探测任务。
- `worker`：运行异步任务扩展。
- `migrate`：执行数据库迁移。
- `seed`：初始化管理员、默认组织、站点配置和模型目录。

### 从单容器到分角色

默认部署用单容器 Compose，适合最小化运维成本。需要扩展时，可以叠加 `deploy/compose/docker-compose.roles.yml`，把 API、Gateway、Prober 和 Worker 拆开部署。

### 数据模型围绕真实运营

核心表包括用户、组织、通道、通道凭据、模型目录、模型价格、探测运行、探测快照、Incident、Gateway、Gateway Key、请求事件、用量 Rollup、告警、通知通道、审计和 Open API 站点。它不是只给演示用的状态页模型，而是面向运营、监控和网关调用的完整数据边界。

### 发布硬化

仓库包含开源发布预检、生产变量预检、无演示数据检查、备份、恢复演练、安全扫描、Compose 配置校验、Docker 构建和 smoke 测试脚本。发布前可以用一个命令跑基础门禁。

## 快速启动

```bash
cp -n .env.example .env || true
docker compose up -d --build
```

默认入口：

- Web / API / Gateway：`http://localhost:8080`
- OpenAPI：`http://localhost:8080/openapi.yaml`
- Metrics：`http://localhost:8080/metrics`
- Gateway：`http://localhost:8080/gateway/v1/*`
- 本地开发管理员账号：`admin`
- 本地开发默认密码：`admin@tokhub.local`

上述账号和密码也是默认后台管理入口的登录账号和登录密码，只用于本地开发。生产环境必须在 `.env.production` 中替换 `TOKHUB_ADMIN_PASSWORD` 和 `TOKHUB_SECRET_KEY`。

服务启动后的轻量冒烟：

```bash
TOKHUB_BASE_URL=http://localhost:8080 npm run test:smoke
```

## 本地验收

基础检查：

```bash
go test ./...
go vet ./...
sqlc generate
npm run typecheck
npm run lint
npm run build
npm run test:security
docker compose config
```

应用启动后可以继续跑：

```bash
npm run test:ops
npm run test:restore
npm run test:e2e
npm run test:visual
```

发布前建议运行：

```bash
deploy/scripts/release-check.sh
```

如果本地 Docker 服务已启动，并且要做完整发布检查：

```bash
RUN_DB_CHECK=1 RUN_RESTORE=1 RUN_E2E=1 RUN_VISUAL=1 RUN_SMOKE=1 deploy/scripts/release-check.sh
```

## 生产部署

生产环境不要使用 `.env.example` 中的开发默认值。至少需要准备：

- `TOKHUB_PUBLIC_URL`
- `TOKHUB_ADMIN_EMAIL`
- `TOKHUB_ADMIN_PASSWORD`
- `TOKHUB_SECRET_KEY`
- `DATABASE_URL`
- `REDIS_URL`
- `NATS_URL`
- `SMTP_URL`，如果需要真实邮件通知

生产环境推荐保持：

- `TOKHUB_ENV=production`
- `TOKHUB_SEED_MODE=prod`
- `TOKHUB_UPSTREAM_MODE=real`
- `TOKHUB_SESSION_SECURE=true`
- `TOKHUB_EXPOSE_DEV_TOKENS=false`

单容器发布：

```bash
cp .env.production.example .env.production
# 填入真实密钥、域名和外部依赖地址
deploy/scripts/preflight.sh --env-file .env.production
docker compose --env-file .env.production up -d --build
curl -fsS "$TOKHUB_PUBLIC_URL/healthz"
curl -fsS "$TOKHUB_PUBLIC_URL/readyz"
```

分角色发布：

```bash
docker compose --env-file .env.production -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml up -d --build
```

更多细节见 [部署说明](docs/DEPLOYMENT.md)、[发布流程](docs/RELEASE.md) 和 [恢复演练](docs/RECOVERY-DRILL.md)。

## API

- 人可读 API 接入说明：[docs/API.md](docs/API.md)
- 机器可读 OpenAPI 合同：[docs/openapi.yaml](docs/openapi.yaml)
- 管理员 Agent API：[docs/admin-agent-api.md](docs/admin-agent-api.md)
- 管理员 Agent OpenAPI：[docs/admin-agent.openapi.yaml](docs/admin-agent.openapi.yaml)
- 运行中服务 OpenAPI：`http://localhost:8080/openapi.yaml`

主要 API 分层：

- `/api/public/*`：公开前台数据。
- `/api/auth/*`：注册、登录、会话、邮箱验证和密码重置。
- `/api/me/*`：个人收藏和私有通道。
- `/api/console/*`：用户或企业工作区。
- `/api/admin/*`：平台管理后台。
- `/v1/status/*`：第三方状态 Open API。
- `/gateway/v1/*`：OpenAI 兼容专属网关。

## 目录

- `cmd/tokhub/`：单入口进程，按 `TOKHUB_ROLE` 启动不同角色。
- `internal/`：后端模块，包括 API、认证、加密、探测、网关、事件和数据访问。
- `web/`：React / Vite 前端。
- `db/`：数据库迁移和 sqlc 查询。
- `deploy/`：Compose、Helm、备份恢复、压测和发布脚本。
- `docs/`：API、部署、发布、恢复、开源规则和机器合同文档。
- `tests/`：Playwright 端到端和视觉测试。

## License

TokHub is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
