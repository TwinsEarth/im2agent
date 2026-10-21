# IM2Agent 局域网部署版（v0.6-lan）

在家庭或办公室内网一台机器上一键起 IM2Agent，其他设备（手机/平板/同事电脑）通过浏览器访问。

## 前置条件

一台装了 Docker 的机器（Linux/macOS/Windows 均可，建议常开）。

## 一键启动

```bash
git clone https://github.com/TwinsEarth/im2agent.git
cd im2agent
docker compose -f deploy/lan/docker-compose.yml up -d
```

首次构建约 1-2 分钟（拉 Go 镜像编译），后续直接用本地缓存。

## 找到本机局域网 IP

| 系统 | 命令 |
| --- | --- |
| macOS | `ipconfig getifaddr en0` |
| Linux | `hostname -I \| awk '{print $1}'` |
| Windows | `ipconfig` 找以太网/WLAN 的 IPv4 |

假设 IP 是 `192.168.1.100`，则：

- 用户面：http://192.168.1.100:8080/
- 运维面：http://192.168.1.100:8080/admin/
- 健康检查：http://192.168.1.100:8080/healthz

**手机/平板连同一个 WiFi 即可打开**，无需任何客户端。

## 常用命令

```bash
docker compose -f deploy/lan/docker-compose.yml logs -f   # 看日志
docker compose -f deploy/lan/docker-compose.yml restart   # 重启
docker compose -f deploy/lan/docker-compose.yml down      # 停止并删除容器
```

## 与 v0.5 Linux 裸机版的区别

| 维度 | v0.5 Linux 裸机 | v0.6 LAN Docker |
| --- | --- | --- |
| 部署方式 | systemd + 二进制 | docker compose 一键起 |
| 依赖 | 无 | Docker |
| 隔离 | 系统用户 + ProtectSystem | 容器 cgroup |
| 适合 | 云服务器长期跑 | 家庭/办公室临时演示 |
| 镜像/二进制大小 | 7 MB | 镜像 < 10 MB |

## 安全提醒

- **不要把 8080 端口暴露到公网**（路由器/防火墙不要做端口映射）
- 当前版本无鉴权，任何能访问该端口的人都能审批
- 局域网内"熟人"假设：只有你信任的设备和人在这个网段里
- 重启容器后审批/账本数据重置（内存版），持久化请等 v0.9 生产版
