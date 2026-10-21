# IM2Agent 公网生产部署版（v0.9-prod）

把 demo 单二进制从"本地跑"升级到"公网可用、HTTPS、有鉴权"。

## 前置清单

| 项 | 说明 |
| --- | --- |
| 一台 Linux VPS | Ubuntu 22.04+，1C2G 起步，公网 IP |
| 一个域名 | A 记录已解析到 VPS IP，80/443 端口放行 |
| Docker | `curl -fsSL https://get.docker.com \| sh` |
| 强随机 token | `openssl rand -hex 32` 生成两个：ADMIN_TOKEN / POSTGRES_PASSWORD |

## 一键启动

```bash
cat > .env <<'EOF'
DOMAIN=im2agent.yourdomain.com
ADMIN_TOKEN=<openssl rand -hex 32 的输出>
POSTGRES_PASSWORD=<openssl rand -hex 32 的输出>
OPENAI_API_KEY=sk-xxx
OPENAI_BASE_URL=https://api.deepseek.com/v1
LLM_MODEL=deepseek-chat
EOF

cd deploy/prod
docker compose up -d
docker compose logs -f im2agent
```

打开 `https://im2agent.yourdomain.com` 即可看到前端。运维界面 `/admin/` 需要：
```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" https://im2agent.yourdomain.com/api/admin/state
```

## 安全 Checklist

- [x] Caddy 自动申请 Let's Encrypt 证书，90 天自动续
- [x] HSTS 头开启
- [x] `/api/admin/*` 与 `/admin/` 走 Bearer Token
- [x] 业务端口 8080 不绑公网，只对 Caddy 开放
- [x] Caddy 限制 `X-Frame-Options DENY` 防点击劫持
- [ ] 待办：PostgreSQL 切真实存储（demo 仍是内存版，重启丢数据）
- [ ] 待办：腾讯云 IM WebSocket 长连（替换轮询）
- [ ] 待办：APNs / FCM 推送
- [ ] 待办：每日数据库备份（pg_dump cron）

## 故障排查

```bash
docker compose logs caddy | grep -i certificate
docker compose ps im2agent
curl http://127.0.0.1:8080/healthz
```

## 升级流程

```bash
git pull
cd deploy/prod
docker compose build im2agent
docker compose up -d
```
