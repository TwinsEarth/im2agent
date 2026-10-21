#!/usr/bin/env bash
# IM2Agent Linux 一键安装脚本（Debian/Ubuntu/CentOS/Rocky 通用）
# 用法：
#   curl -fsSL https://example.com/install.sh | sudo bash
# 或本地：
#   sudo bash deploy/linux/install.sh [BINARY_PATH]
set -euo pipefail

BINARY="${1:-./im2agent-linux-amd64}"
INSTALL_DIR=/opt/im2agent
SERVICE_NAME=im2agent
PORT="${PORT:-8080}"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "请用 sudo 运行：sudo bash $0" >&2
  exit 1
fi

echo "==> 1/5 创建系统用户"
if ! id -u im2agent >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin im2agent
fi

echo "==> 2/5 安装二进制到 $INSTALL_DIR"
mkdir -p "$INSTALL_DIR" /var/log/im2agent
if [[ ! -f "$BINARY" ]]; then
  echo "找不到二进制：$BINARY" >&2
  echo "请先在开发机执行 make build-linux，或从 release 下载 im2agent-linux-amd64" >&2
  exit 1
fi
install -m 0755 "$BINARY" "$INSTALL_DIR/im2agent"
chown -R im2agent:im2agent "$INSTALL_DIR" /var/log/im2agent

echo "==> 3/5 安装 systemd unit"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install -m 0644 "$SCRIPT_DIR/im2agent.service" /etc/systemd/system/im2agent.service
systemctl daemon-reload

echo "==> 4/5 启动并设置开机自启"
systemctl enable --now im2agent
sleep 1
systemctl --no-pager status im2agent || true

echo "==> 5/5 健康检查"
if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null; then
  echo "✅ IM2Agent 已启动：http://<server-ip>:$PORT/  (用户面)  http://<server-ip>:$PORT/admin/  (运维面)"
else
  echo "⚠️  健康检查未通过，查看日志：journalctl -u im2agent -f" >&2
  exit 1
fi
