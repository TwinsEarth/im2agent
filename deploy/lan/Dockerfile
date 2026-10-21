# IM2Agent 局域网版 Dockerfile（多阶段构建）
# v0.2.6：加非 root 用户 + tzdata + OCI label
# 用法：
#   docker build -f deploy/lan/Dockerfile -t im2agent:lan .
#   docker run -d -p 8080:8080 im2agent:lan

# ---- 构建阶段 ----
FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod ./
# 无 go.sum（纯 stdlib）
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/im2agent ./cmd/demo

# ---- 运行阶段：alpine（含 ca-certificates + tzdata）----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S im2agent && adduser -S im2agent -G im2agent
COPY --from=builder /out/im2agent /usr/local/bin/im2agent
USER im2agent
EXPOSE 8080
# 健康检查（用 wget，alpine 自带 busybox）
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["im2agent", "-addr", ":8080"]

# OCI 标签
LABEL org.opencontainers.image.source="https://github.com/TwinsEarth/im2agent" \
      org.opencontainers.image.title="IM2Agent" \
      org.opencontainers.image.description="AI Native Hybrid Human-Agent IM Platform"
