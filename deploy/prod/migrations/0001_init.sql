-- IM2Agent PostgreSQL schema migration 0001
-- 用法（在已建好的 im2agent 库里执行一次）：
--   psql "$DATABASE_URL" -f deploy/prod/migrations/0001_init.sql
-- 幂等：所有 CREATE 都带 IF NOT EXISTS，可重复执行。

BEGIN;

-- 五档信任矩阵（§1.4）：user_id 对 target_id 的档位
CREATE TABLE IF NOT EXISTS trust_levels (
    user_id    BIGINT      NOT NULL,
    target_id  BIGINT      NOT NULL,
    level      TEXT        NOT NULL,           -- family / trusted / known / stranger / self
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_id)
);

-- Agent 能力声明（代码层门控的数据源）
CREATE TABLE IF NOT EXISTS agent_capabilities (
    agent_id                    TEXT PRIMARY KEY,
    trust_level_min             TEXT NOT NULL,
    allowed_skills              JSONB NOT NULL DEFAULT '[]'::jsonb,
    budget_cents_per_day        BIGINT NOT NULL DEFAULT 0,
    requires_approval_above_cents BIGINT NOT NULL DEFAULT 0,
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 审批单（总 Agent 路由 / 子 Agent 状态机）
CREATE TABLE IF NOT EXISTS approval_tickets (
    id           TEXT PRIMARY KEY,
    user_id      BIGINT      NOT NULL,
    source_agent TEXT        NOT NULL,
    type         TEXT        NOT NULL,
    state        TEXT        NOT NULL,           -- pending / completed / rejected / failed
    payload      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_approval_tickets_user ON approval_tickets(user_id);
CREATE INDEX IF NOT EXISTS idx_approval_tickets_state ON approval_tickets(state);

-- 审批事件流（不可变审计）
CREATE TABLE IF NOT EXISTS approval_events (
    id          BIGSERIAL PRIMARY KEY,
    ticket_id   TEXT        NOT NULL REFERENCES approval_tickets(id),
    actor       TEXT        NOT NULL,
    action      TEXT        NOT NULL,
    detail      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_approval_events_ticket ON approval_events(ticket_id);

-- 陌生人付费消息账本（不可变）
CREATE TABLE IF NOT EXISTS paid_message_ledger (
    message_id        TEXT PRIMARY KEY,
    sender_id         BIGINT      NOT NULL,
    receiver_id       BIGINT      NOT NULL,
    mode              TEXT        NOT NULL,       -- send_then_charge / read_then_charge
    amount_cents      BIGINT      NOT NULL,
    status            TEXT        NOT NULL,       -- reserved / charged / refunded
    approval_card_id  TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    charged_at        TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ledger_sender ON paid_message_ledger(sender_id, created_at DESC);

CREATE TABLE IF NOT EXISTS paid_message_events (
    id          BIGSERIAL PRIMARY KEY,
    message_id  TEXT        NOT NULL REFERENCES paid_message_ledger(message_id),
    event       TEXT        NOT NULL,
    detail      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Agent 协商会话（A2A）
CREATE TABLE IF NOT EXISTS negotiation_sessions (
    id          TEXT PRIMARY KEY,
    initiator   TEXT        NOT NULL,
    responder   TEXT        NOT NULL,
    topic       TEXT        NOT NULL,
    state       TEXT        NOT NULL,
    payload     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS negotiation_turns (
    id          BIGSERIAL PRIMARY KEY,
    session_id  TEXT        NOT NULL REFERENCES negotiation_sessions(id),
    actor       TEXT        NOT NULL,
    content     TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_negotiation_turns_session ON negotiation_turns(session_id);

-- 通用审计事件流（门控 / 付费 / 审批全量回放，运维台用）
CREATE TABLE IF NOT EXISTS audit_events (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    TEXT,
    action      TEXT        NOT NULL,
    decision    TEXT,
    detail      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC);

COMMIT;
