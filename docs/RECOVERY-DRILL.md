# TokHub 恢复演练

## 目标

验证备份文件可以恢复核心数据：用户、组织、通道、网关、审计、用量、告警和站点配置。

## 演练步骤

1. 启动本地环境：

```bash
docker compose up -d --build
```

2. 创建备份：

```bash
deploy/scripts/backup.sh backups/drill.dump
```

3. 记录恢复前计数：

```bash
docker compose exec db psql -U tokhub -d tokhub -c "select 'users' t,count(*) from users union all select 'channels',count(*) from channels union all select 'audit_events',count(*) from audit_events;"
```

4. 非破坏性恢复演练：

```bash
deploy/scripts/restore-drill.sh backups/drill.dump
```

5. 如需恢复到目标环境：

```bash
TOKHUB_RESTORE_CONFIRM=restore deploy/scripts/restore.sh backups/drill.dump
```

6. 校验：

```bash
deploy/scripts/db-check.sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## 通过标准

- `restore.sh` 无错误退出。
- `/healthz`、`/readyz` 返回 2xx。
- 管理员可以登录，首页、用户控制台、平台后台可访问。
- 审计导出不泄露 Gateway Key、私有通道 Key 或 Open API Site Key。
