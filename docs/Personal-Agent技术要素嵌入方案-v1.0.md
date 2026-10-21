# IM2A · Personal Agent 技术要素嵌入方案（v1.0 增量）

> 本文是《技术设计基线 v1.0》的**增量决策文档**，不重写基线。
> 输入：2026 年 Personal Agent 技术栈调研（Meta Muse / OpenAI dots / PAP / PACT / HMO / MyContext / StepX Neo / Liquid Context / personal-agent 双语言运行时）。
> 目的：把外部技术收敛，逐项映射为 IM2A 现有代码的**新增包、表、接口、版本落点**，并给出与云厂商路径的差异化决策。
>
> 一句话收束：**2026 年 Personal Agent 的技术收敛方向不是"更强的模型"，而是隔离的执行环境 + 分层的记忆系统 + 不可绕过的授权门控 + 端侧持续感知。** IM2A 的三级 Agent 体系、五档信任矩阵、"付费在预审之后"原则恰好对应这四件事；第一优先级是 Context 飞轮的工程化，而不是追逐单一模型能力。

---

## 0. 核心判断：模型可以租，Context 租不到

这个世界上从来没有什么 Personal Agent，有的只有 **Context + Agent**。同一个 Agent，接上什么 Context，就是什么 Agent。

由此推出 IM2A 的技术优先级排序：

1. **Context 飞轮**（带得走的状态：锁定结论、授权边界、纠错记录、办到一半的任务）——长期壁垒，优先做；
2. **执行环境隔离 + 凭证不可见**——信任底座；
3. **不可绕过的授权门控**（Go 侧强制，非提示词）——已有 `internal/trustzone`，需强化；
4. **端侧持续感知**——24h 在线的工程定义；
5. **模型能力**——通过 `internal/llm` 租，不绑定单一厂商。

---

## 1. 四层技术栈 × IM2A 现状对照

| 层 | 2026 收敛形态（头部产品） | IM2A 现状（v0.6.1） | 差距 | 本方案落点 |
|---|---|---|---|---|
| **① 云端运行时** | 每用户独立 Secure VM / 云电脑；Runtime 显式管理 Goal / Task State / Schedule，执行与等待交替 | 有审批状态机（`internal/approval` 8 态）、协商 8 态（`internal/negotiation`），但都是**内存态、单任务**，无长期 Runtime | 缺 Person-scoped Runtime、缺 Task State 持久化与跨周期续跑 | **M1**：新增 `internal/runtime` |
| **② 端侧上下文层** | 手机 NPU 本地 Context 引擎（Liquid Context）、KVCache 前缀复用（Tandem）；端侧做采集与预过滤 | 基线 §2.5 有"端侧只做唤醒/敏感过滤"的方向性描述；demo 无任何端侧代码 | 缺端侧 MCP 原子能力层、缺 Context 采集/压缩/归并 | **M4**：`internal/edge`（端侧能力契约）+ 硬件 BLE |
| **③ 协议与授权** | PAP（身份/授权分离 + DPoP）、PACT（A2A Card → OAuth 设备授权 → 短期委派 token → scope 内执行） | 有 `internal/trustzone`（五档 Capability 门控）、`internal/ledger`（付费记账）；但付费语义是"记账"，不是"委派授权"；无跨域身份 | 缺跨信任域身份、缺 scope 委派 token、缺签名回执 | **M5**：新增 `internal/consent` |
| **④ Context 飞轮** | Active Memory / Active Focus 拆分；MyContext 确认锁定；HMO 三层记忆；Personal State / Task State / Working Context 分层 | 基线 §3.3 有"记忆分层（工作/情景/语义）"和"蒸馏闭环"，但无锁定区、无记忆表、无飞轮度量 | 缺记忆双域隔离、缺用户确认锁定硬约束、缺飞轮指标 | **M3 + M7**：新增 `internal/memory` + `internal/contextmetrics` |

横切两个底座：

| 底座 | 2026 收敛 | IM2A 现状 | 落点 |
|---|---|---|---|
| **凭证隔离 + 独立看守** | Muse：hatch-authd 保管 OAuth，Sentinel 独立判允许/拒绝/确认，主 Agent 看不到原始凭证 | 无 Secret Vault；LLM API Key 直接走环境变量；门控在编排器内（不是独立看守） | **M2**：新增 `internal/vault` + `internal/sentinel` |
| **唯一可信运行时** | personal-agent(Azusa010)：TS 主进程做所有路径校验/权限判定/副作用，Python 只跑 Agent Loop；LLM 恶意工具调用无法突破 | Go 侧已有 `trustzone` PreToolUse 门控 + `compliance-gate` 出口护栏，方向正确但覆盖面不全 | **M6**：强化 `internal/trustzone`，所有副作用收口到 Go Policy 层 |

---

## 2. 七个可嵌入模块（M1–M7）

每个模块给出：**现状 → 差距 → 工程落点（包/服务/接口/表）→ 优先级**。

### M1 · Person-scoped Runtime（个人运行时）

**本质**：总 Agent 不是路由器，而是一个跨天/跨周持续存在的个人运行时。子 Agent 的每次激活、等待、审批挂起都对应 Runtime 中持久化的 Task State，而不是散落在对话记录或内存里。

- **现状**：`internal/approval` 与 `internal/negotiation` 各有状态机，demo 用内存 store，重启即丢。
- **差距**：无"执行与等待交替"的统一调度器；无 Task State 检查点；无 Schedule（到期唤醒）。
- **工程落点**：
  - 新增 `internal/runtime`：
    - `Task`：`{TaskID, OwnerAgentID, Goal, State, WaitCondition, Checkpoint, ScheduleAt, ResumeToken}`；
    - `Scheduler`：扫描 `wait_until` 到期、外部事件满足 `WaitCondition` 时 Resume；
    - 复用 `approval` / `negotiation` 为**可被 Task 引用的子状态机**，不重写。
  - 新增 `cmd/runtime`（或在 prod 入口启用）：后台调度循环。
  - 新增表 `runtime_tasks`（见 §5）。
- **优先级**：P0（Context 飞轮的载体）。

### M2 · Secret Vault + Sentinel（凭证保管与独立看守）

**本质**：三件套之一——隔离执行 + 凭证不可见 + 独立看守。子 Agent 永远看不到原始 OAuth Token / 支付凭证；敏感操作由独立于主 Agent 的 Sentinel 判定。

- **现状**：LLM Key 走环境变量；门控逻辑在编排器内（自己判自己，不是独立看守）。
- **差距**：无 Vault、无代理凭证、无独立 Sentinel。
- **工程落点**：
  - 新增 `internal/vault`：
    - 凭证加密存储（KMS / 本地 age 加密），Agent 只拿到**一次性代理凭证**（proxy credential）；
    - 对外调用经 Vault 代理注入真实 Token，Token 永不进入 LLM prompt 与审计正文。
  - 新增 `internal/sentinel`：
    - 独立进程/独立模块，输入"动作 + 主体 + scope + 上下文摘要"，输出 `ALLOW / DENY / CONFIRM`；
    - 不与主 Agent 共享 prompt，避免被同一上下文污染；
    - 对支付、外发、删除、授权变更四类动作强制 `CONFIRM`。
  - 与现有 `trustzone` 关系：`trustzone` 判"能力是否在档位内"（静态），Sentinel 判"这一次具体动作是否允许"（动态、带上下文）。
- **优先级**：P0（信任底座）。

### M3 · 记忆双域 + 确认锁定（Context 飞轮核心）

**本质**：三层状态严格分离 + 用户锁定硬约束。

| 状态 | 生命周期 | 内容 | 存储 | 谁可写 |
|---|---|---|---|---|
| **Personal State** | 以年计 | 画像、偏好、关系、授权边界 | PG 关系表 + 锁定区 | 子 Agent 提议，**关键结论用户锁定** |
| **Task State** | 天/小时 | 检查点、等待条件、在途任务 | `runtime_tasks` | Runtime |
| **Working Context** | 单次执行 | 本次推理工作集 | 内存/向量临时切片 | Agent Loop，执行完即失效 |

**记忆框架分工**（不混在一个存储）：
- 子 Agent 短期工作记忆 → Letta 式运行时自管理（内存/向量）；
- 跨 Agent 实体关系与时间线 → Zep/Graphiti 式时序图（规模化后）；
- **用户锁定的关键结论 → 独立关系表 `locked_context`，只能读，任何子 Agent 不得覆写**。

**确认锁定硬约束**（对齐 MyContext）：
- 子 Agent 可自动提取/整理 Context，但关键结论写入权在用户；
- 冲突数据做语义级比对：能合并自动合并，拿不准交用户确认；
- 用户确认后进入锁定区，后续任务只读；
- 锁定/修改动作写入不可变审计，与审批流共用审计基础设施。

- **工程落点**：新增 `internal/memory`（`Store` 接口 + 三层实现）；新增表 `personal_state` / `locked_context` / `agent_work_memory`；扩展现有 `internal/audit` 记录锁定事件。
- **优先级**：P0（最高，飞轮本体）。

### M4 · 端侧 Context 层（24h 在线的工程定义）

**24 小时在线的准确定义**：**感知层永远在线（NPU 级功耗），编排层按需唤醒**。端侧不做完整 Agent，只做"守门人"和"感知器"。

| 层 | 职责 | 选型 |
|---|---|---|
| 感知层（端侧） | 唤醒词、VAD、敏感内容前置过滤、意图粗分类 | Syntiant NDP120 级低功耗 NPU；TFLite Micro |
| 设备能力层（端侧） | 通过 MCP 暴露原子能力（日程、通知、本地文件、通信） | 参考 Step Amoo atomic capability engine |
| 编排层（云/边缘） | 总 Agent 路由、子 Agent 调度、协商 | 云端大模型 + 共享工作区 |
| 记忆层（双域） | 个人 Context 锁定存储 + Agent 工作记忆 | 端侧加密存储 + 云端分层 |

- **工程落点**：
  - 新增 `internal/edge`：定义端侧 MCP 能力契约（`device.capabilities` manifest），总 Agent 用统一 MCP Client 调用，不直接依赖硬件 SDK；
  - 端侧采集后**本地压缩 + 粗结构化**，只上传摘要，不上传原始音频（呼应架构图"原始音频不离开设备"）；
  - 跨设备 Context 同步/归并协议**不绑定任何单一芯片厂商 SDK**（风险对策见 §6）。
- **优先级**：P1（硬件首发节奏相关，Phase 1）。

### M5 · PACT/PAP 兼容授权（陌生人付费的授权语义）

**关键认知升级**：付费解锁的不是"通路"，而是一次 **带 scope 的委派**。陌生人付费后获得的不是"发消息的权限"，而是"被允许查看本条消息原始内容"这一**特定 scope**；目标用户的 Agent 仍按信任矩阵和来意质量决定是否呈报本人。

**PACT 三步模型映射到 IM2A**：

```
对方 Agent 提交来意（A2A Card 发现，Agent 身份验证）
        │  ← 身份与授权分离：先验 Agent 身份，不涉及用户账户
        ▼
触发付费 → 获得短期 delegation token（绑定 消息ID / Agent平台 / scope）
        │  ← scope 例：intent.summary / message.read / message.reply
        ▼
在 scope 内执行 → 每次操作签发绑定回执（messageId + scope + 操作）
        │  ← 超出 scope → auth-required 中断，回到 Agent 预审
        ▼
回执写入不可变审计（复用 ledger + audit）
```

**PACT 原则映射**：

| PACT 原则 | IM2A 落地 |
|---|---|
| 身份与授权分离 | Agent 身份（哪家的 Agent）与被允许代表用户做什么（消息读取/回复 scope）分开验证 |
| 企业/用户定义权限范围 | 每个用户为陌生人消息设置可授权范围（仅来意摘要 / 可查看原始消息 / 可回复） |
| 登录留在授权方 | 支付凭证与登录态永远不经过对方 Agent（由 Vault 保证，M2） |
| 签名回执 | 每次扣费/审批生成绑定 `message_id + scope + 操作类型` 的签名回执，写不可变审计 |

- **工程落点**：新增 `internal/consent`（Agent Card 发现、OAuth 设备授权流、短期 JWT 委派签发与校验、scope 检查）；新增表 `delegated_tokens` / `signed_receipts`；改造 `internal/ledger` 使其在扣费成功时触发 delegation 而非直接放行。
- **优先级**：P1（Phase 1 付费消息上线时必须）。

### M6 · 唯一可信运行时（Go 侧 Policy 收口）

**本质**（借鉴 Azusa010 双语言架构）：Go 代码层承担**所有权限判定和副作用执行**，LLM 仅负责规划与生成建议。任何 Agent 输出在执行前必须经过 Go 侧 Policy 检查，检查逻辑不依赖模型自觉。

- **现状**：`internal/trustzone` 已在 PreToolUse 做代码层门控；`compliance-gate` 已做出口护栏。
- **差距**：副作用（发消息、支付、删除、外部 HTTP）尚未**全部**收口到统一 Policy 层；部分路径仍可绕过。
- **工程落点**：
  - 建立 `Sidecar / SideEffect` 唯一出口：所有外部副作用只允许经一个 Go 接口 `ExecuteSideEffect(action) error`，内部串联 `trustzone → sentinel → vault → idempotency`；
  - 八层纵深（参考）：Scope → Retriever → Context → Alignment → Binder/PathGuard → Sidecar → Permission → Idempotency；
  - 幂等键（对齐现有 `ledger` message_id 幂等）。
- **优先级**：P0（信任边界不可绕过，尽早收口）。

### M7 · Context 飞轮度量（新指标体系）

不只监控审批通过率，更监控 **Context 增长曲线**——飞轮转得越快，Agent 越"懂你"。

| 指标 | 含义 | 来源 |
|---|---|---|
| `locked_context_count` | 子 Agent 累计被用户锁定的结论数 | locked_context |
| `context_reuse_rate` | 锁定结论跨任务复用次数 / 任务数 | runtime + memory |
| `user_correction_rate` | 用户驳回/修改 Agent 建议的频率 | approval events |
| `personal_prior_hit` | 总 Agent 个人化权限先验命中（某档位下自动批准正确）率 | trustzone + approval |
| `negotiation_quality_score` | 对方 Agent 历史来意质量（无中心评分，凭协商记录） | negotiation + ledger |

- **工程落点**：新增 `internal/contextmetrics`；在 `/admin/` 运维台新增"Context 飞轮"面板（复用 v0.2.1-admin 的黑底白字控制台）。
- **优先级**：P1（度量飞轮，验证产品复利）。

---

## 3. 与云厂商路径的三个结构性差异

云厂商（Meta / OpenAI / 阿里）的 Personal Agent 有共同宿命：Context 来自平台生态、Agent 运行在平台 VM、授权协议由平台主导——它们是"平台的延伸"，不是"用户的代理"。IM2A 守三个交集：

### 决策一：Runtime 可迁移，Context 主权归用户

- 云厂商：VM 与存储绑定在厂商云，用户带不走。
- IM2A：Runtime 设计为**可迁移的轻量状态序列化包**（Personal State + Task State 导出/导入）。用户从 demo（内存）→ 局域网 → 公网生产 → 自建，Agent 的记忆与长期任务可随之迁移。这是开源基线相对闭源云产品的结构性优势。
- **落点**：M1/M3 提供 `runtime export/import` 接口（单文件包，含签名与版本）。

### 决策二：Context 飞轮的输入源是 Agent 协商记录

- 云厂商：Context 主要来自自家生态或 API 接入。
- IM2A：以**协商过程产生的结构化决策记录**为核心燃料——每次陌生人来意、每次熟人协调、每次用户驳回，都是其他平台不会产生的独特 Context。这些记录既驱动蒸馏，也构成陌生人付费的信誉基础（`negotiation_quality_score` 由历史协商记录评估，不依赖中心化评分）。

### 决策三：付费是可验证的意图信号，不是绕过门控的通道

- 云厂商：跨域依赖 OAuth 委托。
- IM2A：付费 + PACT scope 叠加——支付触发的是 A2A 式 `authorization-required` 状态迁移（触发一次带 scope 的协商会话），**不是绕过门控直接投递**。"付费在 Agent 预审之后"由此获得协议级表达。

---

## 4. 与现有代码的落点对照

| 现有包/服务 | 职责 | 本方案动作 |
|---|---|---|
| `internal/trustzone` | 五档 Capability 静态门控 | 保留（M6 收口到统一 SideEffect）；新增 Sentinel 动态判定（M2） |
| `internal/approval` | 审批 8 态 + 事件溯源 | 被 `runtime.Task` 引用为子状态机（M1）；提供锁定事件（M3） |
| `internal/negotiation` | 协商 8 态 + 熔断 | **新增轨迹审查器**（风险二，§6）：每轮检查提案是否越界，越界即 `input-required` 上抛 |
| `internal/ledger` | 幂等付费账本 | 扣费成功触发 delegation（M5）；签发签名回执 |
| `internal/llm` | OpenAI 兼容 client | 不变（模型可租）；Key 改由 Vault 注入（M2） |
| `internal/audit` | 不可变审计 | 复用：锁定、授权、回执、Sentinel 判定全部写审计 |
| `internal/compliance` | 出口护栏 | 保留为 M6 纵深一环（Alignment/出口） |
| — | — | **新增** `internal/runtime`（M1） |
| — | — | **新增** `internal/memory`（M3） |
| — | — | **新增** `internal/vault`（M2） |
| — | — | **新增** `internal/sentinel`（M2） |
| — | — | **新增** `internal/consent`（M5） |
| — | — | **新增** `internal/edge`（M4 契约） |
| — | — | **新增** `internal/contextmetrics`（M7） |

---

## 5. 新增数据模型（PostgreSQL，增量）

```sql
-- M1：Person-scoped Runtime 任务状态（执行与等待交替）
CREATE TABLE runtime_tasks (
  task_id        UUID PRIMARY KEY,
  owner_agent_id VARCHAR(64) NOT NULL,
  user_id        BIGINT NOT NULL,
  goal           TEXT NOT NULL,
  state          VARCHAR(32) NOT NULL,       -- RUNNING/WAITING/APPROVAL_PENDING/DONE/FAILED
  wait_condition JSONB,                      -- 等待条件（外部事件/审批/超时）
  checkpoint     JSONB,                      -- 可恢复检查点
  schedule_at    TIMESTAMPTZ,                -- 到期唤醒
  resume_token   TEXT,
  created_at     TIMESTAMPTZ DEFAULT now(),
  updated_at     TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_runtime_waiting ON runtime_tasks(state, schedule_at);

-- M3：个人长期画像（Personal State）
CREATE TABLE personal_state (
  user_id BIGINT NOT NULL,
  scope   VARCHAR(64) NOT NULL,             -- 偏好/关系/授权边界/...
  key     VARCHAR(128) NOT NULL,
  value   JSONB NOT NULL,
  source  VARCHAR(32) DEFAULT 'agent',      -- agent 提议 / user 锁定
  updated_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, scope, key)
);

-- M3：用户确认锁定的关键结论（只读，不可被子 Agent 覆写）
CREATE TABLE locked_context (
  lock_id    UUID PRIMARY KEY,
  user_id    BIGINT NOT NULL,
  scope      VARCHAR(64) NOT NULL,
  statement  TEXT NOT NULL,                -- 锁定的结论
  lock_hash  TEXT NOT NULL,                -- 内容哈希，防篡改
  locked_at  TIMESTAMPTZ DEFAULT now(),
  audit_ref  UUID                          -- 指向不可变审计事件
);
CREATE INDEX idx_locked_user ON locked_context(user_id, scope);

-- M3：Agent 工作记忆（向量；锁定结论不放这里）
-- agent_work_memory 使用 pgvector，带 agent_id 过滤列，沿用基线命名空间隔离

-- M5：PACT/PAP 短期委派令牌
CREATE TABLE delegated_tokens (
  token_id    UUID PRIMARY KEY,
  message_id  UUID,                        -- 绑定具体付费消息
  agent_platform VARCHAR(64),              -- 对方 Agent 平台身份
  subject     VARCHAR(64),                 -- 被授权主体
  scope       TEXT[] NOT NULL,             -- intent.summary/message.read/message.reply
  dpop_jti    TEXT,                        -- DPoP 绑定
  expires_at  TIMESTAMPTZ NOT NULL,
  revoked     BOOLEAN DEFAULT FALSE,
  created_at  TIMESTAMPTZ DEFAULT now()
);

-- M5：签名回执（每次扣费/审批/外发）
CREATE TABLE signed_receipts (
  receipt_id  UUID PRIMARY KEY,
  message_id  UUID,
  actor       VARCHAR(64) NOT NULL,
  action      VARCHAR(64) NOT NULL,
  scope       TEXT[],
  signature   TEXT NOT NULL,
  audit_ref   UUID,
  created_at  TIMESTAMPTZ DEFAULT now()
);
```

> M7 指标优先以 `runtime_tasks / locked_context / approval events` 现算 + 物化视图，不急于新建独立时序表。

---

## 6. 两个必须预判的技术风险

### 风险一：端侧 Context 层碎片化

高通 Liquid Context、各手机厂商自研记忆层、可穿戴本地感知正在形成孤岛。若 IM2A 重度依赖端侧硬件采集，手机/耳机/PC 上的 Agent 行为会不一致。

- **对策**：M4 的 `internal/edge` 定义**厂商中立的跨设备 Context 同步/归并协议**（同步层不依赖任何单一芯片 SDK）；冲突归并复用 M3 的"语义比对 + 确认锁定"；端侧只上传摘要，原始数据留在本地。

### 风险二：Agent 协商的 Context 污染（传话游戏）

双方 Agent 多轮协商时，每轮输出成为下轮输入，协商历史可能逐渐偏离原始意图。

- **对策**：`internal/negotiation` 内置**轻量轨迹审查器**（借鉴 Muse Sentinel 隔离思路）：
  - 每轮协商后，由独立于协商双方的审查器检查提案是否仍在用户预设边界（预算、时间、授权范围、原始意图）；
  - 越界立即触发 `input-required`（或现有熔断态）上抛，**不让两个 Agent 在无人监督下继续讨论**；
  - 审查结论写审计。

---

## 7. 版本路线（在 v0.6.1 之后）

| 版本 | 主题 | 交付模块 | 验收 |
|---|---|---|---|
| **v0.7.0** | 唯一可信运行时收口 | M6：副作用统一 `ExecuteSideEffect`；现有路径全量过 Policy | 无任何外部副作用绕过 Sidecar；旧自测全绿 |
| **v0.8.0** | Context 飞轮底座 | M3：`internal/memory` 三层 + locked_context 表 + 确认锁定 UI；M7 最小指标 | 子 Agent 无法覆写锁定结论；锁定写审计；/admin 显示飞轮指标 |
| **v0.9.0** | 个人运行时 | M1：`internal/runtime` + runtime_tasks + Scheduler；approval/negotiation 挂为子状态机 | 重启后等待中任务可续跑；到期唤醒正确 |
| **v0.10.0** | 凭证与看守 | M2：Vault（代理凭证）+ Sentinel（独立判定） | 原始 Token 不出现在 prompt/审计；四类敏感动作强制 CONFIRM |
| **v0.11.0** | 授权协议 | M5：`internal/consent`（Agent Card/委派 token/回执）；ledger 改触发 delegation | 陌生人付费获得 scope 而非通路；越界 auth-required；回执可验 |
| **v1.0.0** | 端侧 Context + 可迁移 | M4：edge 能力契约 + 跨设备归并；Runtime export/import | 换部署实例记忆/任务可迁移；端侧只上传摘要 |

> M6 / M3 / M1 为 P0（信任与飞轮），M2 / M5 / M4 / M7 为 P1。版本顺序按"先收口信任边界、再建 Context 飞轮、最后接端侧与协议"推进。

---

## 8. 验收清单（宣布完成前逐项核对）

- [ ] **M6**：所有外部副作用（发消息/支付/删除/外部 HTTP/授权变更）只经 `ExecuteSideEffect`，无旁路；
- [ ] **M3**：`locked_context` 中的结论任何子 Agent 无法 UPDATE/DELETE（应用层只允许 INSERT 新版本 + 审计）；
- [ ] **M3**：冲突 Context 能自动合并或交用户确认，不静默覆盖；
- [ ] **M1**：进程重启后，WAITING / APPROVAL_PENDING 任务能按 check­point 续跑；
- [ ] **M2**：审计正文与 LLM prompt 中检索不到任何原始 OAuth Token / 支付凭证；
- [ ] **M2**：支付/外发/删除/授权变更四类动作在无用户确认时一律不执行；
- [ ] **M5**：付费只获得绑定 message_id 的 scope token，超 scope 操作被拒绝并 auth-required；
- [ ] **M5**：每次扣费/审批有可验签的 receipt；
- [ ] **M4**：端侧上传内容不含原始音频；换设备 Context 可归并且不依赖单一芯片 SDK；
- [ ] **M7**：/admin 可看到 locked_context_count / reuse_rate / correction_rate 三项飞轮指标；
- [ ] **风险二**：协商越界在同一轮被审查器拦截并上抛，不允许无限漂移。

---

## 附：与基线的关系

- 本文不改变基线《技术设计基线 v1.0》§2.1 的六层分层与 §2.2 的三条流量分离；
- M1–M7 落在 **L4 Agent 编排层** 与 **L6 数据层** 内部细化，M4 落在 **L2 边缘/端侧**，M5 落在 **L1 接入层与 L4 之间的协议面**；
- 基线 §3.3"记忆分层 / 蒸馏闭环"由本文 M3/M7 具体化；基线 §4.2 A2A 由本文 M5 扩展为 PACT/PAP 兼容授权。
