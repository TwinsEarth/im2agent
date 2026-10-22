// 服务引导：构建存储 / 审批引擎 / Skill 运行时。
//
// 默认内存实现（离线自测/演示，零外部依赖）；
// 设置环境变量 PG_DSN（以及可选 PG_DRIVER，默认 pgx）时切换 PostgreSQL。
// 示例：
//
//	PG_DRIVER=pgx PG_DSN="postgres://user:pass@host:5432/haim?sslmode=disable" \
//	  go run ./services/agent-orchestrator
package api

import (
	"os"

	"human-agent-im/internal/approval"
	"human-agent-im/internal/distill"
	"human-agent-im/internal/eventbus"
	"human-agent-im/internal/memory"
	"human-agent-im/internal/model"
	"human-agent-im/internal/skillruntime"
	"human-agent-im/internal/store"
)

// 付费消息单日发送上限（Phase 1 默认值）。
const defaultDailyLedgerLimit = 10

// NewDeps 构建服务依赖。driver/dsn 为空时使用内存实现。
func NewDeps(driver, dsn string) (*Deps, error) {
	var s *store.Store
	if dsn != "" {
		db, err := store.Open(driver, dsn)
		if err != nil {
			return nil, err
		}
		s = store.NewPostgresStore(db, defaultDailyLedgerLimit)
	} else {
		s = store.NewMemoryStore(defaultDailyLedgerLimit)
	}
	eng := approval.New(s.Approval)
	rt := skillruntime.NewRuntime(skillruntime.DefaultRegistry(), s.Capability)
	// v0.5.2：事件总线（审批/门控/扣费/记忆变更统一事件流）。
	bus := eventbus.New()
	// v0.6：Context 三层记忆（内存实现，生产由 PG 替换）。
	mem := memory.NewGuarded(memory.NewMemoryStore())
	mem.SetBus(bus)
	// v0.7：蒸馏闭环（订阅记忆强化事件，自动收紧阈值）。
	distillEng := distill.New(bus)
	// v0.8：L5 模型层（端侧桩 + 云端兼容，离线可跑）。
	edge := model.NewStub()
	cloud := model.NewOpenAICompatible(
		os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL"),
	)
	hybrid := model.NewHybrid(edge, cloud)
	hybrid.SetBus(&modelBusAdapter{bus: bus})
	return &Deps{Store: s, Skills: rt, Engine: eng, Memory: mem, Bus: bus, Distill: distillEng, Model: hybrid}, nil
}

// NewDepsFromEnv 从环境变量读取 PG 配置（PG_DRIVER / PG_DSN）。
func NewDepsFromEnv() (*Deps, error) {
	dsn := os.Getenv("PG_DSN")
	driver := os.Getenv("PG_DRIVER")
	if dsn != "" && driver == "" {
		driver = "pgx"
	}
	return NewDeps(driver, dsn)
}
