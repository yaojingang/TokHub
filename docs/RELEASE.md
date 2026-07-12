# TokHub 发布流程

## 发布前准备

1. 确认开源发布检查通过：

```bash
bash deploy/scripts/open-source-preflight.sh
```

2. 准备生产环境变量：

```bash
cp .env.production.example .env.production
deploy/scripts/preflight.sh --env-file .env.production
```

生产环境变量必须保持：

- `TOKHUB_ENV=production`
- `TOKHUB_SEED_MODE=prod`
- `TOKHUB_UPSTREAM_MODE=real`

## 本地发布检查

基础检查：

```bash
deploy/scripts/release-check.sh
```

该脚本会执行 Go/TypeScript/前端构建、安全扫描、Docker Compose 配置校验、生产示例预检；仓库存在提交后，还会校验 `sqlc generate` 是否造成生成文件漂移。

如果本地 Docker 环境已启动，并且要做完整发布检查：

```bash
RUN_DB_CHECK=1 RUN_RESTORE=1 RUN_E2E=1 RUN_VISUAL=1 RUN_SMOKE=1 deploy/scripts/release-check.sh
```

若要确认当前运行库已经不包含 demo/test 数据：

```bash
TOKHUB_ENV=production RUN_NO_DEMO=1 deploy/scripts/release-check.sh
```

若要从空数据库验证生产 seed、一键启动、无示例数据检查和前台/用户后台/管理后台 smoke：

```bash
RUN_FRESH_PROD=1 deploy/scripts/release-check.sh
```

若已有生产模式服务在运行，可单独执行无示例数据页面/API smoke：

```bash
TOKHUB_BASE_URL="$TOKHUB_PUBLIC_URL" npm run test:no-demo-smoke
```

## 镜像构建

```bash
docker build -t tokhub:0.1.0 .
```

## 单容器首次发布

```bash
docker compose --env-file .env.production up -d --build
curl -fsS "$TOKHUB_PUBLIC_URL/healthz"
curl -fsS "$TOKHUB_PUBLIC_URL/readyz"
TOKHUB_BASE_URL="$TOKHUB_PUBLIC_URL" npm run test:smoke
```

## 已有线上服务更新

已有数据库的线上服务更新时，只跑迁移，再重建应用容器；不要用全量 `up` 当作日常更新命令，避免重复执行 seed job。

```bash
git pull origin main
docker compose --env-file .env.production run --rm --build migrate
docker compose --env-file .env.production up -d --build --no-deps app
curl -fsS "$TOKHUB_PUBLIC_URL/healthz"
curl -fsS "$TOKHUB_PUBLIC_URL/readyz"
```

## 分 role 首次发布

```bash
docker compose --env-file .env.production -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml up -d --build
```

分 role 已有线上服务更新同样先跑 `migrate`，再分别 `up -d --build --no-deps api gateway prober worker`。

检查：

```bash
docker compose -f docker-compose.yml -f deploy/compose/docker-compose.roles.yml ps
```

## 回滚

1. 保留上一版镜像 tag。
2. 发布前运行 `deploy/scripts/backup.sh`。
3. 如需恢复数据库，先在临时库运行 `deploy/scripts/restore-drill.sh <dump>`。
4. 确认后才运行：

```bash
TOKHUB_RESTORE_CONFIRM=restore deploy/scripts/restore.sh <dump>
```

## 发布后冒烟

至少检查：

- `/healthz`
- `/readyz`
- `/openapi.yaml`
- `/`
- `/login`
- `/console`
- `/admin`
- `/gateway/v1/models`，使用真实 Gateway Key
- `/metrics`
