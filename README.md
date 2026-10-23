# human-agent-im · AI 人机混合熟人社交 IM 平台

![CI](https://github.com/TwinsEarth/im2agent/actions/workflows/ci.yml/badge.svg)
![Compat](https://github.com/TwinsEarth/im2agent/actions/workflows/compat.yml/badge.svg)
![Release](https://github.com/TwinsEarth/im2agent/actions/workflows/release.yml/badge.svg)
![npm](https://img.shields.io/npm/v/@twinsearth%2Fim2agent?logo=npm&label=npm)
![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![Flutter](https://img.shields.io/badge/Flutter-SDK-02569B?logo=flutter&logoColor=white)
![Version](https://img.shields.io/badge/version-v1.8.8-blue)
![License](https://img.shields.io/badge/license-MIT-green)

> **Slogan**：让每个人都拥有自己专属的 AI 秘书、助理与管家团队。
>
> 传统 IM 连接人，人类被海量信息奴役。IM2A 连接 Agent 网络，人类退居幕后，掌握最终裁决权。
> 这不仅是 IM，更是 AGI 时代个人生活方式的"数字平权"基础设施。

---

## 一、产品定位

| 维度 | 说明 |
|---|---|
| **个人目标** | 把日常杂碎、行政事务、繁琐沟通外包给 Agent 团队，人类专注自我提升、家庭情感、事业创造 |
| **社会愿景** | 机器承担基础运转与协调劳动，推动人类从"劳动者"向"消费者/指挥者"转换 |
| **终极使命** | 打破资源壁垒，让普通人以极低成本配备顶级私人法务、家庭医生、资深采购、行政助理 |

### 三条不可妥协原则

1. **付费永远发生在 Agent 预审之后**——付费不是买通路，是买已被预审过的有效触点
2. **信任边界是代码层 capability 门控**——不是提示词约束，模型无法绕过（`trustzone.Check`）
3. **"不上传原始音频"必须可验证**——端侧只做唤醒/意图粗分类/敏感过滤，原始音频不离开设备

---

## 二、总体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│ L1 接入层      耳夹耳机/眼镜(BLE) · 手机 · PC · 手表(审批/摘要) · Web │
├─────────────────────────────────────────────────────────────────────┤
│ L2 边缘/端侧   端侧唤醒/意图粗分类 · 敏感前置过滤 · 离线审批缓存       │
│               原始音频不离开设备（可验证承诺）                        │
├─────────────────────────────────────────────────────────────────────┤
│ L3 IM 通信层   腾讯云 IM PaaS · WebSocket 长连接 · 多端同步 · 离线推送 │
├─────────────────────────────────────────────────────────────────────┤
│ L4 Agent 编排层（自研核心资产）                                       │
│   总Agent(管家) → 子Agent调度(日程/采购/法律/亲子/物业)               │
│   → 权限门控 TrustZone PreToolUse 代码层强制                         │
│   → 审批工作流引擎 PENDING→…→COMPLETED/FAILED                       │
│   → Skill Runtime(MCP) 沙箱+权限清单                                │
│   → 记忆分层(工作/情景/语义) · 子Agent隔离                           │
│   → 出口护栏 外发内容必过审                                          │
│   蒸馏闭环：审批/驳回/修改反馈 → Improvement Memory → 自动收紧阈值   │
├─────────────────────────────────────────────────────────────────────┤
│ L5 模型层      端侧小模型(唤醒/意图/过滤) ←→ 云端大模型(规划/协商)    │
│               智能路由：置信度≥0.6 端侧直接用，<0.6 转云端             │
├─────────────────────────────────────────────────────────────────────┤
│ L6 数据层      PostgreSQL(pgvector) · OpenSearch · Kafka · 时序库    │
│               对象存储 WORM 审计                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 三条关键流量分离（核心决策）

| 通道 | 机制 | 用途 |
|---|---|---|
| ① 人类 IM 通道 | 腾讯云 IM PaaS · WebSocket | 人与人情感交互、多端同步、离线推送 |
| ② Agent 协商通道 | 自研 gRPC 双向流 · 8 态 Task 状态机 | Agent 间高频协商，终止时仅注入一张审批卡片 |
| ③ 审批与审计事件流 | Kafka | 每次工具调用/门控/扣费写事件，可解释性 & 合规追溯 |

---

## 三、熟人五档信任矩阵

| 档位 | 自主执行 | 需审批 |
|---|---|---|
| L0 本人 | 全部 | — |
| L1 家庭 | 日程/采购/物业 | 大额/对外承诺 |
| L2 信任 | 预约/留言 | 第三方信息 |
| L3 已知 | 接收留言摘要 | 所有对外回复 |
| L4 陌生 | 仅结构化来意 | 付费后查看 |

---

## 四、陌生人付费注意力经济

```
① 发送即扣费 → 过滤批量垃圾
② 回复才扣费 → 高价值触点
账本不可变 · message_id 幂等 · 单日上限
```

### 付费消息状态机

```
pending（reserve 占位）
  ↓
charged（billingMode=1 发送即扣）
  或 delivered（billingMode=2 等待打开）
  ↓
opened（接收方打开）
  或 rejected（拒收 → 自动退款）
  或 expired（超时未打开 → 自动关闭/退款）
  或 auto_rejected（接收方偏好自动拒绝）
```

### 拦截顺序

```
send 请求
  ↓
1. compliance 预审（关键词/手机号/URL）
2. 接收方定价检查（minCents）
3. 黑名单检查
4. 接收方偏好（autoReject / autoAccept）
5. ledger reserve → charge
```

---

## 五、核心模块与版本演进

### v0.1-v0.5 基座（已完成）

| 版本 | 模块 | 说明 |
|---|---|---|
| v0.1 | 7 核心机制 | 消息分片锁/信任门控/审批状态机/协商熔断/幂等账本/出口护栏/编排闭环 |
| v0.2 | 协议契约 | 9 个 Protobuf + PG schema + MCP Skill Runtime + HTTP API |
| v0.5 | 统一网关 | 认证中间件 + 真实消息存储 + 限流 + 事件总线 |

### v0.6 记忆基座（Context 飞轮）

| 版本 | 功能 |
|---|---|
| v0.6 | 三层记忆（Working KV / Episodic 时间线 / Semantic 带置信度）+ 子 Agent 隔离 |
| v0.6.1 | TTL 惰性过期 + 决策闭环集成 |
| v0.6.2 | 记忆导出/导入 + 统计 |
| v0.6.3 | 置信度衰减 + 全局搜索 + 幂等 |
| v0.6.4 | Fact 强化 + 标签过滤 + GC |
| v0.6.5 | 事件流 + 快照回滚 |

### v0.7 蒸馏闭环（自进化）

| 版本 | 功能 |
|---|---|
| v0.7.0 | 连续 N 次负反馈或负反馈占比≥30% 自动收紧（只收紧不放宽） |
| v0.7.1 | 接入门控闭环 |
| v0.7.2 | 事件溯源与可解释性 |
| v0.7.3 | 规则参数 Config 化 |
| v0.7.4 | 用户级导出/导入 |
| v0.7.5 | 报表 + 记忆联动 |
| v0.7.6 | 审批卡蒸馏标注 + 批量重置 |

### v0.8 L5 模型层

| 版本 | 功能 |
|---|---|
| v0.8.0 | Provider 接口（端侧桩 + 云端兼容 + 混合路由） |
| v0.8.1 | 云端 OpenAI 兼容真实调用 |
| v0.8.2 | 智能路由（置信度≥0.6 端侧直接用） |
| v0.8.3 | 模型调用审计事件 |
| v0.8.4 | 多模型 fallback 链 |
| v0.8.5 | Per-agent 云端配额（防烧钱） |
| v0.8.6 | 9 个意图域分类扩展 |
| v0.8.7 | 热切换云端配置 |

### v0.9 陌生人付费经济（当前）

| 版本 | 功能 |
|---|---|
| v0.9.0 | 付费消息端到端闭环 |
| v0.9.1 | 用户自主定价 + 拒收退款 |
| v0.9.2 | 收件箱/已发列表/内容查看 |
| v0.9.3 | 付费统计 dashboard |
| v0.9.4 | 付费消息黑名单 |
| v0.9.5 | 消息过期（超时自动关闭） |
| v0.9.6 | 按状态过滤查询 |
| v0.9.7 | 批量导出（对账用） |
| v0.9.8 | 接收方偏好（自动接受/自动拒绝） |

### v1.0 生产化层（已完成）

| 版本 | 功能 |
|---|---|
| v1.0.x | 认证中间件 + 真实 PG 消息存储 + 全局限流 + 优雅退出 + 配置热加载 |

### v1.1 实时通道（已完成）

| 版本 | 功能 |
|---|---|
| v1.1.x | WebSocket 长连接通道：在线状态、心跳、多端连接管理、消息实时下发 |

### v1.2 腾讯云 IM 双通道物理隔离（已完成）

| 版本 | 功能 |
|---|---|
| v1.2.0 | 人类消息走腾讯云 IM；Agent 消息走自研协商流，物理隔离 |
| v1.2.1 | UserSig 签发服务（签名 + 过期刷新） |
| v1.2.2 | Webhook 回调接收（消息/已读/群事件） |
| v1.2.3 | 消息路由分流：人类→IM、Agent→协商流、审批→审计事件流 |
| v1.2.4 | 群聊支持：关系链/群成员/消息广播 |

### v1.3 Flutter 完整客户端（已完成）

| 版本 | 功能 |
|---|---|
| v1.3.0 | 审批态卡片流 + 巡视态日志 + 会话态 IM 三态全实现 |
| v1.3.1 | 审批卡片交互：通过/修改/驳回/超时升级 |
| v1.3.2 | 巡视态：Agent 工作轨迹、协商时间线、审计事件流 |
| v1.3.3 | 会话态：Agent 摘要层 IM + 人类消息分流 |
| v1.3.4 | api_client 真实对接网关全部路由 |
| v1.3.5 | Flutter Desktop（PC 窗口 + 多端同步） |

### v1.4 端侧智能 + BLE（已完成）

| 版本 | 功能 |
|---|---|
| v1.4.0 | 端侧唤醒词、意图粗分类、敏感前置过滤 |
| v1.4.1 | 端侧意图分类服务（小模型/规则可插拔） |
| v1.4.2 | 敏感词端侧拦截，不出端 |
| v1.4.3 | BLE 网关协议（耳机/眼镜/指环接入） |
| v1.4.4 | "音频不出端"出端流量白名单审计报告 |

### v1.5 手表端 + 多端同步（已完成）

| 版本 | 功能 |
|---|---|
| v1.5.0 | 手表一键审批、离线缓存、冲突合并 |
| v1.5.1 | 手表 App：通知/一键通过驳回/紧急升级 |
| v1.5.2 | 离线审批缓存（断网可审批，联网同步） |
| v1.5.3 | 多端状态同步（审批/消息/关系一致） |

### v1.6 工程化与五方发布（已完成）

| 版本 | 功能 |
|---|---|
| v1.6.x | 全量集成门禁 `make gate`、CI、npm 包、Compat 兼容性门禁、Release workflow 五方发布 |

### v1.7 运营后台（已完成）

| 版本 | 功能 |
|---|---|
| v1.7.0 | Webhook 公网 HTTPS 投递（Let's Encrypt 443） |
| v1.7.1 | 管理仪表盘（用户/Agent/付费/调用量实时面板） |
| v1.7.2 | 管理仪表盘细化（趋势/付费流水/Agent 明细） |
| v1.7.3 | 客服工单：用户反馈→工单→处理闭环 |
| v1.7.4 | 审计可视化：事件流时间线 + 门控决策回放 |
| v1.7.5 | 内容审核：自动规则 + 人工审核队列 |

### v1.8 Agent 网络（当前）

| 版本 | 功能 |
|---|---|
| v1.8.0 | A2A 统一信封：Agent 间协商通道（msg_id 幂等 + 轮询收件箱 + deliver） |
| v1.8.1 | Agent 通讯录一等公民：对外别名/AttentionLevel/tags/月预算钱包，信任档位继承自 Agent 背后的人 |
| v1.8.2 | 双层经验库：案例层写入仅限人类审批决策 + 模式层须人类批准才生效；Propose-Commit-Audit 三分离（最终审计裁决归人类） |
| — | 真实 APNS 推送通道（.p8 JWT，官方 apns2 库，待 Apple 凭据真机投递） |
| v1.8.3 | 安全合规基线：安全中间件 + 审计落库（security_events）+ 威胁模型框架 |
| v1.8.4 | STRIDE 威胁建模与修复：强认证、IP 限流、安全响应头、注入防护、越权拦截 |
| v1.8.5 | 数据脱敏：手机号/金额/地址出端脱敏，审计保留原值（可证明） |
| v1.8.6 | 等保/个保对齐：合规文档 + 数据主体权利（访问/更正/删除/导出/拒绝自动处理） |
| v1.8.7 | 渗透测试套件：8 攻击向量真实打公网网关 + 修复闭环 + 渗透报告 |
---

## 六、HTTP API 端点清单

### 统一网关（`/v1/` 前缀）

| 模块 | 方法 | 路径 | 说明 |
|---|---|---|---|
| **信任** | POST | `/v1/trust/grant` | 授权 `{userId,targetId,level 0..4}` |
| | GET | `/v1/trust/get?user=&target=` | 查询信任档位 |
| | POST | `/v1/trust/check` | 门控检查（ALLOW/REQUIRE_APPROVAL/DENY） |
| **Agent** | POST | `/v1/agents/register` | 注册 Agent |
| | POST | `/v1/agents/capabilities` | 注册 Capability |
| **审批** | POST | `/v1/approvals` | 创建审批单 |
| | POST | `/v1/approvals/{id}/approve` `/reject` `/modify` | 状态迁移 |
| | GET | `/v1/approvals/pending?user=` | 待办列表 |
| **协商** | POST | `/v1/negotiations` | 建立协商会话 |
| | POST | `/v1/negotiations/{id}/turns` | 提交一轮 |
| **消息** | POST | `/v1/msg/seq` | `{userId}` → `{seq}` |
| | POST | `/v1/msg/seq/batch` | `{userId,n}` → `{seqs}` |
| **合规** | POST | `/v1/compliance/check` | `{messageId,content}` |
| **编排** | POST | `/v1/gate/check` | 编排器门控 |
| | POST | `/v1/stranger/intent` | 陌生人来意预审 |
| | POST | `/v1/orchestrator/tickets/{id}/decide` | 审批决策 |
| **审计** | GET | `/v1/audit` | 审计轨迹回放 |
| **MCP** | POST | `/mcp` | MCP JSON-RPC |
| | GET | `/mcp/skills` | Skill 清单 |
| **记忆** | POST | `/v1/memory/master` | 注册总 Agent |
| | POST | `/v1/memory/fact` | 写语义记忆 |
| | GET | `/v1/memory/cross/facts` | 跨子 Agent 读取 |
| **蒸馏** | GET | `/v1/distill/stats` | 蒸馏统计 |
| | POST | `/v1/distill/feedback` | 手动反馈 |
| **模型** | POST | `/v1/model/chat` | 智能路由对话 |
| | GET | `/v1/model/info` | 模型信息 |
| | GET | `/v1/model/stats` | 调用统计 |
| | GET | `/v1/model/quota` | 云端配额 |
| | POST | `/v1/model/cloud` | 热切换云端 |
| **付费消息** | POST | `/v1/paidflow/send` | 发送付费消息 |
| | POST | `/v1/paidflow/open` | 打开消息 |
| | POST | `/v1/paidflow/price` | 设置最低定价 |
| | POST | `/v1/paidflow/reject` | 拒收并退款 |
| | GET | `/v1/paidflow/inbox?receiverId=&status=` | 收件箱 |
| | GET | `/v1/paidflow/outbox?senderId=&status=` | 已发列表 |
| | GET | `/v1/paidflow/content?messageId=` | 查看内容 |
| | GET | `/v1/paidflow/stats?userId=` | 统计 dashboard |
| | POST | `/v1/paidflow/block` | 拉黑发送方 |
| | POST | `/v1/paidflow/unblock` | 解除拉黑 |
| | POST | `/v1/paidflow/expire` | 过期清理 |
| | GET | `/v1/paidflow/export?userId=&role=` | 批量导出 |
| | POST | `/v1/paidflow/preferences` | 接收方偏好 |

---

## 七、快速开始

前置：macOS/Linux，Go ≥ 1.21。本仓库 Go 代码 **stdlib-only**，离线可编译。

```bash
# 全量关卡：build + vet + go test -race + 7 服务自测
make gate

# 启动统一网关
make gateway PORT=8080
curl -s localhost:8080/healthz

# 付费消息示例
curl -s -X POST localhost:8080/v1/paidflow/send \
  -d '{"senderId":100,"receiverId":200,"content":"hello","billingMode":1,"amountCents":100}'

# 模型对话（端侧桩，无 key 自动回退）
curl -s -X POST localhost:8080/v1/model/chat \
  -d '{"messages":[{"role":"user","content":"今天天气怎么样"}]}'

# Flutter 客户端
cd client/app && flutter create . && flutter run
```

环境变量：
- `GATEWAY_TOKEN`：Bearer 认证（空=跳过）
- `PG_DSN`/`PG_DRIVER`：PostgreSQL 连接（空=内存存储）
- `LLM_BASE_URL`/`LLM_API_KEY`/`LLM_MODEL`：云端模型（空=回退端侧桩）

---

## 八、仓库结构

```
human-agent-im/
├── docs/                        # 技术文档
│   ├── 技术设计基线-v1.0.md     # 架构/协议/开发计划
│   ├── 技术设计基线-v2.0-后续版本规划.md
│   └── 版本路线图-v0.6-v1.0.html
├── proto/                       # 9 个 Protobuf 协议契约
├── internal/                    # 核心逻辑库
│   ├── msgseq/  trustzone/  approval/  negotiation/  ledger/
│   ├── audit/   config/     id/       store/       skillruntime/
│   ├── compliance/           # 出口护栏
│   ├── memory/                # 三层记忆（v0.6）
│   ├── distill/               # 蒸馏闭环（v0.7）
│   ├── model/                 # 模型抽象层（v0.8）
│   └── api/                   # HTTP handler + bootstrap
├── services/                   # 8 个服务入口
│   ├── gateway/               # 统一网关（v0.5）
│   ├── agent-orchestrator/    # 编排器（核心）
│   ├── approval-engine/
│   ├── negotiation-svc/
│   ├── relation-svc/
│   ├── payment-svc/
│   ├── msg-svc/
│   └── compliance-gate/
├── deploy/
│   ├── docker-compose.yml      # postgres/redis/kafka/etcd/opensearch
│   └── migrations/001_init.sql
├── client/app/                  # Flutter 客户端
├── .github/workflows/ci.yml    # GitHub Actions
├── Makefile
└── go.mod
```

---

## 九、开发路线（v0.6 → v1.0）

| 里程碑 | 版本 | 核心交付 |
|---|---|---|
| **M1 活大脑** | v0.6-v0.8 | 记忆基座 + 蒸馏闭环 + 模型抽象层 |
| **M2 闭环经济** | v0.9-v0.10 | 陌生人付费（当前）+ 协商双向流实跑 |
| **M3 生产化** | v0.11-v0.12 | PG+向量+Kafka 生产化 + 腾讯云 IM 接入 |
| **M4 全端** | v0.13-v0.15 | Flutter 全端（手机/PC/手表/Web） |
| **M5 GA** | v1.0 | 本地加密 + Context 可导出 + 正式发布 |

---

## 十、护城河

| 壁垒 | 说明 |
|---|---|
| **Context 飞轮** | 世界上没有 Personal Agent，只有 Context + Agent。积累的人类纠错记录、授权边界、办事习惯是核心资产，迁移成本极高 |
| **技术领先** | 大模型泛化能力 + 代码级零信任网络，攻克 Agent 越权、API 安全调用、云端/本地记忆分层 |
| **关系网网络效应** | 一旦熟人、同事、家人都在使用，他们的 Agent 已建立高频协作默契（如管家知道对方助理周四下午只接受商务会谈） |

---

**v1.8.7 已交付**：三条流量分离全链路打通（人类→腾讯云 IM / Agent→A2A 协商 / 审批→审计事件流），PG 真实落库测试全 PASS，运营后台 + 双层经验库 + 真实 APNS 通道就位；v1.8.3–v1.8.7 补齐安全合规（STRIDE 威胁建模、数据脱敏、个保数据主体权利、8 向量渗透测试）。

**工程质量（本轮全版本审查收口）**：全量 `go test ./...`（注入真实 PG_DSN）27 包全 ok、0 FAIL、0 panic；`make gate`（build/vet/`-race`/7 服务自测）全 PASS；`-race` 无 DATA RACE；所有 real_test 注册 `t.Cleanup`，业务表跑测前后行数完全一致（零残留），`audit_events` 按 WORM 合规设计 append-only（触发器禁删/改，靠唯一标识隔离）。
