#!/bin/sh
# 生产构建脚本：在有 Go 工具链 + 网络的环境执行。
#
# 产出：dist/im2agent-prod-linux-amd64（静态 ELF，内嵌 pgx）。
# 用法：
#   ./deploy/prod/build.sh
set -e
cd "$(dirname "$0")/../.."

# 1. 拉取 pgx（首次需要）
go get github.com/jackc/pgx/v5@latest
go get github.com/jackc/pgx/v5/stdlib@latest

# 2. 静态编译（CGO 关闭）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" \
  -o dist/im2agent-prod-linux-amd64 \
  ./cmd/demo

# 3. 体积
ls -lh dist/im2agent-prod-linux-amd64
