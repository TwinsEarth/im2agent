GO ?= go
SERVICES := msg-svc relation-svc approval-engine negotiation-svc payment-svc compliance-gate agent-orchestrator gateway

.PHONY: build vet selftest-all gotest test gate proto up down clean help

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## 编译全部服务
	$(GO) build ./...

vet: ## 静态检查
	$(GO) vet ./...

gotest: ## 运行 Go 单元/端到端测试（含竞态检测）
	$(GO) test -race ./...

selftest-all: ## 运行全部服务自测（验证核心机制）
	@for s in $(SERVICES); do \
		echo "===== $$s ====="; \
		$(GO) run ./services/$$s -selftest || exit 1; \
	done

test: gate ## 全量关卡（build+vet+race 测试+服务自测）

gate: build vet gotest selftest-all ## 发布前全量关卡：编译 / vet / -race 测试 / 7 服务自测

run: ## 启动单个服务（示例：make run SVC=agent-orchestrator PORT=8080）
	$(GO) run ./services/$(SVC) -port $(PORT)

gateway: ## 启动统一网关（示例：make gateway PORT=8080）
	$(GO) run ./services/gateway -port $(PORT)

proto: ## 生成 Protobuf 代码（需 protoc + protoc-gen-go + protoc-gen-go-grpc）
	@command -v protoc >/dev/null 2>&1 || (echo "protoc 未安装：brew install protobuf"; exit 1)
	@command -v protoc-gen-go >/dev/null 2>&1 || (echo "安装：go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"; exit 1)
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || (echo "安装：go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"; exit 1)
	mkdir -p proto/gen
	protoc -I proto --go_out=paths=source_relative:proto/gen --go-grpc_out=paths=source_relative:proto/gen proto/*.proto

up: ## 启动基础依赖（postgres/redis/kafka/etcd/opensearch）
	docker compose -f deploy/docker-compose.yml up -d

down: ## 停止基础依赖
	docker compose -f deploy/docker-compose.yml down

clean: ## 清理生成物
	rm -rf proto/gen bin dist data logs
