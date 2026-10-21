# IM2Agent 局域网版 Dockerfile（多阶段构建）
# 用法：
#   docker build -f deploy/lan/Dockerfile -t im2agent:lan .
#   docker run -d -p 8080:8080 im2agent:lan

# ---- 构建阶段 ----
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
# 无 go.sum（纯 stdlib）
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/im2agent ./cmd/demo

# ---- 运行阶段：scratch 极简镜像 ----
FROM scratch
COPY --from=builder /out/im2agent /im2agent
EXPOSE 8080
# 健康检查（scratch 无 shell，直接调二进制自带 /healthz）
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD ["/im2agent", "-healthcheck"]
ENTRYPOINT ["/im2agent", "-addr", ":8080"]
