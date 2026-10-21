# IM2Agent · AI 人机混合熟人社交 IM 平台

> 让每个人都像大老板一样，拥有自己的 Agent 秘书/助理/管家团队，管理日常与杂碎事务，
> 专注于自我提升、家庭生活与工作事业。人机混合 IM：Agent 承担协调/预约/确认的"行政层"，
> 人只处理情感层与最终决策层。

**IM2Agent** 是一个开源的人机混合熟人社交 IM 平台工程基线：总 Agent→子 Agent→Skill 三级个人
Agent 体系，人类 IM 与 Agent 间协商双通道分离，熟人五档信任矩阵由代码层 capability 门控强制，
陌生人付费消息永远发生在 Agent 预审之后。目标不是"做一个更聪明的 Agent"，而是把 Agent 的自主性、
IM 的实时性、熟人社交的信任约束与陌生人付费的经济机制收敛到同一个可运行系统中。

- **状态**：v0.2（Phase 0 工程基线；详见 [交付物清单-v1.1](docs/交付物清单-v1.1.md)）
- **许可证**：[MIT](LICENSE)
- **文档**：`docs/技术设计基线-v1.0.md`（架构/框架/SDK/协议/开发计划）
- **协议契约**：`proto/`（9 个 Protobuf，权威协议定义）
- **代码**：`services/*`（7 个 Go 服务，v0.2 核心机制可运行可自测）
- **客户端**：`client/app/`（Flutter 骨架：审批态 / 巡视态 / IM 会话）
- **贡献**：见 [CONTRIBUTING.md](CONTRIBUTING.md) 与 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- **安全**：漏洞报告见 [SECURITY.md](SECURITY.md)

## 一、核心机制（v0.2 已实现并可自测）

| # | 机制 | 实现 | 验证 |
|---|---|---|---|
| 1 | 消息序号 64 分片锁 + FNV 哈希（§2.3） | `internal/msgseq` | `go run ./services/msg-svc -selftest`（100 用户×100 消息并发无重复无空洞） |
| 2 | 熟人五档信任矩阵 + Capability 门控（§1.4/§3.3） | `internal/trustzone` | `go run ./services/relation-svc -selftest`（7 个场景：自主/转审/禁止） |
| 3 | 审批状态机 + 事件溯源 + 超时升级（§7.3） | `internal/approval` | `go run ./services/approval-engine -selftest`（通过/驳回/修改/30min手表/2h短信） |
| 4 | Agent 协商 8 态状态机 + 轮次熔断（§2.4/§10-R6） | `internal/negotiation` | `go run ./services/negotiation-svc -selftest`（达成/熔断/挂起审批/拒绝） |
| 5 | 付费消息账本：幂等扣费 + 单日上限（§1.3/§10-R2） | `internal/ledger` | `go run ./services/payment-svc -selftest`（重复回调不二次扣费/退款/日限） |
| 6 | 合规出口护栏（§8.2） | `services/compliance-gate` | `go run ./services/compliance-gate -selftest`（关键词/手机号/URL 拦截+审计） |
| 7 | Agent 注册/门控/审批/审计全链路（§3.3） | `services/agent-orchestrator` | `go run ./services/agent-orchestrator -selftest`（旗舰演示） |

## 二、快速开始

前置：macOS/Linux，Go ≥ 1.21（go.mod 声明 1.27）。本仓库 Go 代码 **stdlib-only**，离线可编译。

```bash
# 1. 全量关卡：build + vet + go test -race（单元/端到端）+ 7 服务自测
make test          # 或 make gate

# 仅跑 Go 测试（含竞态检测）
make gotest
# 仅跑服务机制自测
make selftest-all

# 2. 启动单个服务（默认内存存储，零外部依赖；HTTP JSON API）
go run ./services/agent-orchestrator -port 8080
curl -s localhost:8080/healthz

# 3.（可选）切换 PostgreSQL 存储：先起依赖，再注入 PG_DSN
make up                       # 首次自动执行 deploy/migrations/001_init.sql
PG_DRIVER=postgres PG_DSN='host=127.0.0.1 user=hai password=hai_dev dbname=hai_platform sslmode=disable' \
  go run ./services/agent-orchestrator -port 8080

# 4.（可选）生成 Protobuf 代码（需 protoc，见 Makefile proto 目标）
make proto

# 5. Flutter 客户端
cd client/app && flutter create . --platforms=android,ios,macos,windows,web && flutter run -d macos
```

### 极轻量验证原型（v0.3.7 起）

```bash
# 单二进制 demo（内存存储 + 内嵌网页 + SSE 实时推送）
make demo
# 访问 http://localhost:8080/ 用户面，http://localhost:8080/admin/ 运维面

# 生产构建（含 pgx 驱动，需联网）
make build-prod
# 产出 dist/im2agent-prod-linux-amd64
```

> 存储说明：服务默认使用内存存储（`internal/store`，每进程独立，仅用于离线演示），
> 设 `PG_DSN`（并在运行时注入 `PG_DRIVER` 驱动）后切换 PostgreSQL；仓储代码不 import 具体驱动，
> 以保持 stdlib-only 离线可编译。

## 三、总体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│ L1 接入层      可穿戴(耳机/眼镜 BLE) · 手机 · PC · 手表(Flutter)      │
├─────────────────────────────────────────────────────────────────────┤
│ L2 边缘/端侧   端侧ASR/意图粗分类 · 敏感过滤 · 离线审批缓存 · BLE网关 │
├─────────────────────────────────────────────────────────────────────┤
│ L3 IM通信层    腾讯云IM PaaS（长连接/多端同步/离线推送）              │
├─────────────────────────────────────────────────────────────────────┤
│ L4 Agent编排层 Agent Registry · Skill Runtime · 审批引擎 · 权限门控   │
│               记忆分层 · 出口护栏 · 协商通道(gRPC, 独立于 IM)         │
├─────────────────────────────────────────────────────────────────────┤
│ L5 模型层      端侧小模型(唤醒/意图/过滤) ←→ 云端大模型(规划/协商)    │
├─────────────────────────────────────────────────────────────────────┤
│ L6 数据层      PostgreSQL · pgvector · OpenSearch · Kafka · 时序库    │
└─────────────────────────────────────────────────────────────────────┘
```

**三条关键流量分离**：人类 IM 消息走 PaaS 长连接；Agent 间协商走自研 gRPC 通道；
审批与审计事件走内部消息队列。三条通道不混用（详见技术文档 §2.2）。

## 四、HTTP JSON API 端点清单（v0.2）

服务以 `-port` 启动后暴露以下端点；所有服务均有 `GET /healthz`。
状态码约定：200 放行/成功，201 已创建，202 需审批（门控），403 门控拒绝，
409 幂等冲突/非法状态，422 合规/协商熔断，429 超日限，404 未找到。

### relation-svc（信任/门控）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/trust/grant` | 授权 `{userId,targetId,level 0..4}` |
| GET | `/v1/trust/get?user=&target=` | 查询信任档位 |
| POST | `/v1/trust/check` | 门控检查（ALLOW 200 / REQUIRE_APPROVAL 202 / DENY 403） |
| POST | `/v1/agents/capabilities` | 注册/更新 Agent Capability（camelCase：agentId/trustLevelMin/allowedSkills/requiresApprovalAboveCents） |

### payment-svc（付费账本）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/paid/reserve` | 占位（messageId 可空，服务端生成 UUID） |
| POST | `/v1/paid/charge` | 扣款（重复回调 409，幂等） |
| POST | `/v1/paid/refund` | 退款 |
| GET | `/v1/paid/usage?sender=` | 当日用量/上限 |
| GET | `/v1/paid/get?messageId=` | 账本条目（status 为 int：0 PENDING / 1 CHARGED / 2 REFUNDED） |

### approval-engine（审批）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/approvals` | 创建审批单（**201**） |
| POST | `/v1/approvals/{id}/approve` `/reject` `/modify` `/complete` | 状态迁移（200） |
| GET | `/v1/approvals/pending?user=` | 待办列表 `{tickets:[]}` |
| GET | `/v1/approvals/{id}` | 审批单详情 |
| POST | `/v1/approvals/timeouts` | 超时扫描（30min→watch_push / 2h→sms） |

### negotiation-svc（Agent 协商）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/negotiations` | 建立协商会话 |
| POST | `/v1/negotiations/{id}/turns` | 提交一轮（PROPOSE/COUNTER/ACCEPT/REJECT/REQUEST_INFO/REQUEST_AUTH） |
| POST | `/v1/negotiations/{id}/bind-approval` | 绑定审批单 |
| GET | `/v1/negotiations/{id}` | 会话状态（轮次/token 熔断 422） |

### compliance-gate（出口护栏）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/compliance/check` | `{messageId,content}`；违规返回 422 |

### msg-svc（消息序号）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/msg/seq` | `{userId}` → `{seq}`（每用户连续无空洞） |
| POST | `/v1/msg/seq/batch` | `{userId,n}` → `{seqs,count}` |
| GET | `/v1/msg/stats` | 分片热度/汇总 |

### agent-orchestrator（编排器 + MCP + 端到端来意）
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/agents/register` | 注册 Agent（meta+cap） |
| POST | `/v1/gate/check` | 编排器门控（注意与 relation 的 `/v1/trust/check` 不同） |
| POST | `/v1/stranger/intent` | 陌生人来意：合规→来意规则→协商→占位→审批卡。合规 422；不接受类别 200+REJECTED；接受 **201** PENDING_APPROVAL |
| POST | `/v1/orchestrator/tickets/{id}/decide` | `{decision: approve/reply/reject}`；发送即扣费 approve 即扣，回复才扣费 reply 才扣 |
| GET | `/v1/audit` | 审计轨迹回放 |
| POST | `/mcp` | MCP JSON-RPC（initialize 返回 `protocolVersion=2026-07-28`） |
| GET | `/mcp/skills` | 已注册 Skill 清单 |

## 五、仓库结构

```
im2agent/
├── docs/技术设计基线-v1.0.md     # 技术文档（架构/框架/SDK/协议/开发计划）
├── docs/交付物清单-v1.0.md / v1.1.md
├── proto/                        # Protobuf 协议契约（9 个）
│   ├── common/relation/agent/approval/negotiation/payment/msg/auth/user
├── internal/                     # 核心逻辑库（服务共享）
│   ├── msgseq/   trustzone/   approval/   negotiation/   ledger/   audit/   config/
│   ├── id/            # UUIDv4（crypto/rand）
│   ├── store/         # 仓储接口 + 内存/PostgreSQL 双实现（driver 运行时注入）
│   ├── skillruntime/  # MCP 无状态 JSON-RPC Skill Runtime + 权限门控
│   ├── compliance/    # 出口护栏（关键词/手机号/URL）
│   └── api/           # 七个服务的 HTTP JSON handler + bootstrap
├── services/                     # 7 个服务入口（-selftest 自测 / -port HTTP）
│   ├── agent-orchestrator/       # 总Agent路由 + 权限门控 + 审批闭环（核心）
│   ├── approval-engine/ negotiation-svc/ relation-svc/
│   ├── payment-svc/  msg-svc/  compliance-gate/
├── deploy/
│   ├── docker-compose.yml        # postgres/redis/kafka/etcd/opensearch
│   └── migrations/001_init.sql   # 七组核心表+事件表（compose 首次初始化自动执行）
├── client/app/                   # Flutter 客户端（审批态/巡视态/IM 会话）
├── Makefile                      # build/vet/gotest/selftest-all/test(gate)/proto/up/down
└── go.mod                        # Go 模块（stdlib-only）
```

## 六、开发路线（详见文档 §9）

| 阶段 | 周期 | 核心交付 |
|---|---|---|
| Phase 0（当前 v0.1→v0.2） | 0—4 月 | 合规申报 + 最小 Agent 闭环（审批态/巡视态）；Go-Zero 骨架；腾讯云 IM 接入；存储双实现 + HTTP API + MCP Runtime |
| Phase 1 | 4—9 月 | 付费消息 + 熟人五档权限 + 耳夹式硬件首发 + 手表审批 |
| Phase 2 | 9—18 月 | 子 Agent 扩展 + 眼镜形态 + 蒸馏体系 + Skill 生态 |
| Phase 3 | 18 月+ | A2A 边界网关 + 个人 Agent 协作网络 |

## 七、三条不可妥协的产品原则

1. **付费永远发生在 Agent 预审之后**——付费不是买通路，是买已被预审过的有效触点。
2. **信任边界是代码层 capability 门控**——不是提示词约束，模型无法绕过（`trustzone.Check`）。
3. **"不上传原始音频"必须可验证**——端侧只做唤醒/意图粗分类/敏感过滤，原始音频不离开设备。
