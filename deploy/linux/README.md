# IM2Agent Linux 部署指南

单二进制、零外部依赖（内存存储版），适合 1 台 1C1G 云服务器跑通完整闭环。

## 1. 编译

在开发机（macOS/Linux）执行：

```bash
make build-linux
# 产出 dist/im2agent-linux-amd64（~7 MB，静态链接，无 libc 依赖）
```

或直接从 GitHub Releases 下载预编译二进制。

## 2. 上传到服务器

```bash
scp dist/im2agent-linux-amd64 deploy/linux/install.sh deploy/linux/im2agent.service \
    root@your-server:/root/im2agent/
```

## 3. 一键安装

```bash
ssh root@your-server
cd /root/im2agent
sudo bash install.sh ./im2agent-linux-amd64
```

脚本会：
- 创建 `im2agent` 系统用户（无登录 shell）
- 二进制装到 `/opt/im2agent/im2agent`
- 安装 systemd unit，`enable --now` 启动
- 健康检查 `/healthz`

## 4. 访问

| 路径 | 用途 |
| --- | --- |
| `http://<server>:8080/` | 用户演示面（审批卡片 + 会话列表） |
| `http://<server>:8080/admin/` | 运维控制台（Agent 注册表 / 账本 / 审计） |
| `http://<server>:8080/healthz` | 健康检查 |

## 5. 常用运维命令

```bash
systemctl status im2agent        # 状态
systemctl restart im2agent       # 重启
journalctl -u im2agent -f       # 实时日志
journalctl -u im2agent --since "10 min ago"
```

## 6. 改端口

```bash
# 编辑 /etc/systemd/system/im2agent.service 把 ExecStart 的 -addr 改掉
sudo systemctl daemon-reload && sudo systemctl restart im2agent
```

## 7. 生产化提醒（内存版的边界）

当前 v0.5 是**内存存储**版本：
- 重启后所有审批单、账本、Agent 注册数据丢失
- 单进程，无水平扩展
- 无鉴权，任何能访问 8080 的人都能审批

**直接暴露公网前必须**：
1. 套一层反向代理（Caddy/Nginx）+ HTTPS + Basic Auth 或登录
2. 切到 PostgreSQL 持久化（见 `deploy/docker-compose.yml`，v0.6 局域网版 / v0.9 生产版）
3. 加鉴权中间件（v0.9 计划内）
