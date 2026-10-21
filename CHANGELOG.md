# Changelog

本文件记录 Go 单二进制路线（cmd/demo + Flutter 客户端 + deploy/）的版本演进。
Node.js 主线（services/ + apps/im-live）见 README 与 docs/。

## v0.4.0 (2026-10-21)

### Release
- README 顶部加 CI / npm / platforms / version / license 徽章
- 版本号统一升级到 v0.4.0（healthz + 客户端版本号）
- 五方发布&核验入口：npm 0.4.0 / GitHub Release + SHA256SUMS / Packages 0.4.0 / CI / Compat

## [Unreleased]

## v0.3.7 (2026-10-21)

### Docs
- README 快速开始加 `make demo` / `make build-prod` 说明

## v0.3.6 (2026-10-21)

### Build
- Makefile 加 `build-prod` 目标（一键调 deploy/prod/build.sh）

## v0.3.5 (2026-10-21)

### Deploy
- LAN compose 内存上限 256M → 512M（与生产 compose/systemd 对齐）

## v0.3.4 (2026-10-21)

### Deploy
- 生产 compose 加 `mem_limit: 512m` + `cpus: 1.0`

## v0.3.3 (2026-10-21)

### Security
- Caddy 限制请求体 1MB（防大文件上传攻击）

## v0.3.2 (2026-10-21)

### Deploy
- systemd unit 加 `MemoryLimit=512M` + `CPUQuota=1.0` + `OOMScoreAdjust=-100`

## v0.3.1 (2026-10-21)

### Docs
- 新建 `docs/GO-ROADMAP.md`，记录 v0.0.1 → v0.3.0 全景表

## v0.3.0 (2026-10-21)

### Client
- Flutter 底部导航栏显示版本号 v0.3.0

## v0.2.9 (2026-10-21)

### API
- `/healthz` 返回加 `version` 字段

## v0.2.8 (2026-10-21)

### Docs
- `.env.example` 补 `DATABASE_URL` / `PG_DRIVER` / `LOG_LEVEL`

## v0.2.7 (2026-10-21)

### Deploy
- LAN compose healthcheck 切 wget + start_period

## v0.2.6 (2026-10-21)

### Deploy
- Dockerfile 从 scratch 升级到 alpine + 非 root 用户 + tzdata + OCI label

## v0.2.5 (2026-10-21)

### Client
- 审批卡信任档中文标签（家庭绿 / 陌生人橙）

## v0.2.4 (2026-10-21)

### Deploy
- 生产 compose 传 `DATABASE_URL` + `depends_on postgres` + `start_period`

## v0.2.3 (2026-10-21)

### Security
- Caddy 加 CSP + admin noindex + 日志 JSON / 30 天

## v0.2.2 (2026-10-21)

### Deploy
- journald 日志轮转（7 天 / 500M），install.sh 自动装

## v0.2.1 (2026-10-21)

### Client
- Flutter Context 飞轮屏（八态导航）

## v0.2.0 (2026-10-21)

### Build
- 新建 `deploy/prod/build.sh` 生产构建脚本
