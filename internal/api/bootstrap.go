// 服务引导：构建存储 / 审批引擎 / Skill 运行时。
//
// 默认内存实现（离线自测/演示，零外部依赖）；
// 设置环境变量 PG_DSN（以及可选 PG_DRIVER，默认 pgx）时切换 PostgreSQL。
// 示例：
//	PG_DRIVER=pgx PG_DSN="postgres://user:pass@host:5432/haim?sslmode=disable" \
//	  go run ./services/agent-orchestrator
package api

import (
	"os"

	"im2agent/internal/approval"
	"im2agent/internal/skillruntime"
	"im2agent/internal/store"
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
	return &Deps{Store: s, Skills: rt, Engine: eng}, nil
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
