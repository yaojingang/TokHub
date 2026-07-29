# TokHub 部署说明

## 单容器 Compose

本地或小团队自托管默认使用单入口模式：

```bash
cp -n .env.example .env || true
docker compose up -d --build
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/openapi.yaml
TOKHUB_BASE_URL=http://localhost:8080 npm run test:smoke
```

默认入口：

- Web、Admin、Console、Public API：`http://localhost:8080`
- Gateway：`http://localhost:8080/gateway/v1/*`
- Metrics：`http://localhost:8080/metrics`
- OpenAPI：`http://localhost:8080/openapi.yaml`

`TOKHUB_DOCS_DIR` 默认在源码环境指向 `docs`，Docker 镜像内指向 `/app/docs`。

本地 `.env.example` 默认使用 `TOKHUB_SEED_MODE=prod`，只创建管理员、默认组织、站点配置和模型目录，不创建示例通道或推荐。只有在明确设置 `TOKHUB_SEED_MODE=demo` 或 `TOKHUB_SEED_MODE=test` 时，才会创建演示/测试数据；生产环境必须继续使用 `TOKHUB_SEED_MODE=prod` 和 `TOKHUB_UPSTREAM_MODE=real`。

## TokHub Skill Login And Admin-Agent Bootstrap

管理员 agent 通道由 `TOKHUB_ADMIN_AGENT_ENABLED` 控制：开发默认启用，生产示例默认关闭。创建一次性 token 需要 owner 浏览器会话等价的账号、密码、CSRF 和 Cookie 流程，可用脚本完成：

```bash
TOKHUB_BASE_URL=http://localhost:8080 \
TOKHUB_ADMIN_EMAIL=admin@tokhub.local \
TOKHUB_ADMIN_PASSWORD='admin@tokhub.local' \
TOKHUB_ADMIN_AGENT_TOKEN_NAME=codex-admin \
TOKHUB_ADMIN_AGENT_TOKEN_SCOPES='admin:*' \
TOKHUB_ADMIN_AGENT_TOKEN_TTL_HOURS=24 \
deploy/scripts/create-admin-agent-token.sh
```

脚本默认只输出一次性明文 token。生产使用后应写入部署密钥系统或本机临时环境，不要提交到仓库。内部机器合同见 `docs/admin-agent-api.md`。

如果使用仓库内配套的 Codex/AI skill 连接本地或远程 TokHub 实例，默认使用通用 `tokhub` skill。操作者只在本机终端输入站点地址、用户名和密码，skill 保存本机 session profile，并按网站里同一个账号的 public、console、admin 权限执行：

```bash
node agent-skills/tokhub/scripts/tokhub.mjs login \
  --url https://www.tokhub.me \
  --profile default \
  --identifier <username-or-email>

node agent-skills/tokhub/scripts/tokhub.mjs preflight --profile default
node agent-skills/tokhub/scripts/tokhub.mjs workspaces list --profile default
node agent-skills/tokhub/scripts/tokhub.mjs request GET /api/console/settings --profile default
```

本机 profile 默认保存在 `~/.tokhub/sessions/<profile>.json`，脚本会按 `0600` 权限写入，并默认拒绝把 profile、admin-agent env、导出文件或 channel-site 包写进当前 git worktree；只有一次性测试数据才使用 `--allow-repo-output` 或 `TOKHUB_ALLOW_REPO_OUTPUT=1`。不要把 profile、token、导出的 CSV 或包提交到仓库。

owner/admin 需要长期或无浏览器的后台自动化时，可继续使用 `tokhub` skill 的 admin-agent 分支换取短期 bearer token：

```bash
node agent-skills/tokhub/scripts/tokhub.mjs admin-agent bootstrap \
  --url https://www.tokhub.me \
  --identifier <owner-username-or-email> \
  --save-env ~/.tokhub-admin.env

source ~/.tokhub-admin.env
node agent-skills/tokhub/scripts/tokhub.mjs admin-agent preflight
node agent-skills/tokhub/scripts/tokhub.mjs admin-agent request GET /api/admin/channels
```

`~/.tokhub-admin.env` 只保存在本机并应保持 `0600` 权限。仓库不再发布单独的 `tokhub-admin` skill；旧名称由 `tokhub` skill 的 admin-agent 分支兼容承接。

## 生产自托管注意事项

生产环境不要使用 `.env.example` 中的开发默认值。至少需要替换：

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

上线前必须运行：

```bash
deploy/scripts/preflight.sh --env-file .env.production
deploy/scripts/release-check.sh
```

## 线上服务器从 GitHub 拉取更新

已有服务器更新到 GitHub 最新代码时，先拉取仓库，再单独执行迁移，最后重建应用容器：

```bash
git pull origin main
docker compose run --rm --build migrate
docker compose up -d --build --no-deps app
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

首次部署仍可使用 `docker compose up -d --build`。后续更新推荐显式先跑 `migrate`，避免应用进程先于数据库结构或内置运营数据启动。

精选推荐的 AIGoCode、Pipellm、PackyCode 三条信息作为应用内推荐兜底内容保留在首页和精选推荐页；fresh 生产部署不会把它们自动插入「平台通道」，因此公开通道列表默认仍为空。部署方后续可以在后台手动添加自己的平台通道和推荐配置。

## 分 role Compose

当需要把流量入口、企业网关和探测任务拆开扩容时：

```bash
docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml up -d --build
curl http://localhost:8081/readyz
curl http://localhost:8082/readyz
```

默认端口：

- `api`：`8081`
- `gateway`：`8082`
- `prober`、`worker`：只在 Compose 网络内暴露健康检查端口

## 备份

```bash
deploy/scripts/backup.sh
```

脚本会生成 `backups/tokhub-*.dump` 和对应 sha256 文件。也可以显式传入输出路径。

## 恢复

恢复会清理并重建目标库对象，必须显式确认：

```bash
TOKHUB_RESTORE_CONFIRM=restore deploy/scripts/restore.sh backups/tokhub-20260101T000000Z.dump
```

非破坏性恢复演练会把备份恢复到临时数据库，校验核心表计数后自动删除临时库：

```bash
deploy/scripts/restore-drill.sh backups/tokhub-20260101T000000Z.dump
```

## 安全与 DB 检查

```bash
npm run test:security
npm run test:ops
npm run test:restore
```

安全扫描覆盖常见 Key、私钥、硬编码密码和敏感字段回显风险；DB 检查覆盖 Phase 8 迁移、关键索引和慢查询扩展状态。

## 示例数据清理

先做 dry-run，确认报告后再决定是否清理：

```bash
deploy/scripts/purge-demo-data.sh --dry-run
```

真正删除需要先备份数据库，并显式传入备份文件：

```bash
deploy/scripts/backup.sh backups/pre-demo-purge.dump
TOKHUB_DEMO_PURGE_BACKUP=backups/pre-demo-purge.dump deploy/scripts/purge-demo-data.sh --confirm purge-demo
deploy/scripts/no-demo-data-check.sh
```

生产环境还需要额外设置 `TOKHUB_ALLOW_DEMO_PURGE=true`，防止误删真实业务数据。

## 压测

```bash
TOKHUB_PUBLIC_URL=http://localhost:8080 node deploy/load/prepare-auth.js > tmp/load-auth.json
LOAD_DURATION_SECONDS=10 LOAD_QPS=4 npm run load:public
GATEWAY_KEY="$(node -e "console.log(JSON.parse(require('fs').readFileSync('tmp/load-auth.json')).gatewayKey)")" LOAD_DURATION_SECONDS=10 LOAD_QPS=100 npm run load:gateway
COOKIE="$(node -e "console.log(JSON.parse(require('fs').readFileSync('tmp/load-auth.json')).cookie)")" LOAD_DURATION_SECONDS=10 LOAD_QPS=50 npm run load:admin
COOKIE="$(node -e "console.log(JSON.parse(require('fs').readFileSync('tmp/load-auth.json')).cookie)")" CSRF_TOKEN="$(node -e "console.log(JSON.parse(require('fs').readFileSync('tmp/load-auth.json')).csrfToken)")" CHANNEL_ID="$(node -e "console.log(JSON.parse(require('fs').readFileSync('tmp/load-auth.json')).channelId)")" LOAD_DURATION_SECONDS=10 LOAD_QPS=20 npm run load:prober
```

## Helm 初版

Helm 模板位于 `deploy/helm/tokhub`，默认假设 PostgreSQL/TimescaleDB、Redis、NATS 由外部托管或独立 Chart 提供。

```bash
helm template tokhub deploy/helm/tokhub \
  --set image.repository=your-registry/tokhub \
  --set image.tag=2.0.0-rc.1 \
  --set publicUrl=https://tokhub.example.com \
  --set secretKey='replace-with-32-byte-min-secret'
```
