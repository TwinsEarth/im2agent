# IM2Agent Go 路线版本记录（cmd/demo · Flutter 客户端 · deploy/）

> 本文件记录 Go 单二进制路线的逐版本演进。Node.js 主线（services/ + apps/im-live）见 README 与 docs/00-技术文档-总览.md。

## 版本总览

| 版本 | 形态 | 关键改动 |
|---|---|---|
| v0.0.1 | Simple&Demo | 修 MemoryStore.Save byUser 倒排索引 bug；统一决策入口 POST /api/approval/decide |
| v0.0.2 | Linux | 7.6M 静态 ELF；.env.example；systemd EnvironmentFile |
| v0.0.3 | Admin | 黑底白字 #0b0e14；审批流监控卡片 |
| v0.0.4 | Client | 修前后端契约（snake_case）；账本态 screen |
| v0.0.5 | LAN | Dockerfile builder golang:1.27-alpine |
| v0.0.6 | LLM | 三档降级（template/llm/fallback）；7 测试 PASS |
| v0.0.7 | Prod | withAdminAuth 支持 Bearer + ?token=；.env.production.example |
| v0.0.8 | SSE | /api/stream 实时推送；decide 后 broadcast |
| v0.0.9 | PG schema | 9 张表幂等 migration |
| v0.1.0 | 付费闭环 | 陌生人 approve→Charge，reject→Refund |
| v0.1.1 | Context 飞轮 | /api/admin/state 加 metrics 段 |
| v0.1.2 | A2A 可视化 | MemoryNegotiation.List()；seed demo 会话 |
| v0.1.3 | 信任矩阵 | POST /v1/trust/grant；admin 信任卡片 |
| v0.1.4 | 客户端 SSE | ApiClient.streamEvents()；审批屏驱动刷新 |
| v0.1.5 | 信任 UI | Flutter 信任授权屏；五态导航 |
| v0.1.6 | 审计屏 | Flutter 审计流屏；六态导航 |
| v0.1.7 | 账本 SSE | 账本屏接扣费事件实时刷 |
| v0.1.8 | 协商屏 | Flutter A2A 协商屏；七态导航 |
| v0.1.9 | PG 优雅降级 | DATABASE_URL 非空失败时回退内存 |
| v0.2.0 | 生产构建 | deploy/prod/build.sh 一键编译 |
| v0.2.1 | 飞轮屏 | Flutter Context 飞轮屏；八态导航 |
| v0.2.2 | 日志轮转 | journald-im2agent.conf（7 天 / 500M） |
| v0.2.3 | Caddy 安全 | CSP + admin noindex + 日志 JSON |
| v0.2.4 | compose PG | 传 DATABASE_URL + depends_on postgres |
| v0.2.5 | 信任标签 | 审批卡中文标签（家庭绿/陌生人橙） |
| v0.2.6 | Docker 硬化 | scratch → alpine + 非 root + tzdata |
| v0.2.7 | LAN 对齐 | healthcheck 切 wget |
| v0.2.8 | .env 补全 | DATABASE_URL/PG_DRIVER/LOG_LEVEL |
| v0.2.9 | healthz 版本 | /healthz 返回 version 字段 |
| v0.3.0 | 客户端版本 | 底部导航栏显示版本号 |

## 技术栈

- 后端：Go 1.27 纯 stdlib（零外部依赖）
- 客户端：Flutter（八态导航：审批/团队/账本/信任/审计/协商/飞轮/会话）
- 部署：systemd + Docker（alpine）+ Caddy 自动 HTTPS
- 存储：内存（demo）/ PostgreSQL（生产，9 张表）
