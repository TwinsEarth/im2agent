package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"im2agent/internal/approval"
	"im2agent/internal/audit"
	"im2agent/internal/ledger"
	"im2agent/internal/negotiation"
	"im2agent/internal/trustzone"
)

func TestRelationContract(t *testing.T) {
	s := NewMemoryStore(10)
	ctx := context.Background()
	if _, err := s.Relation.GetTrust(ctx, 1, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing trust err=%v want ErrNotFound", err)
	}
	if err := s.Relation.GrantTrust(ctx, 1, 2, trustzone.LevelFamily); err != nil {
		t.Fatalf("grant: %v", err)
	}
	lvl, err := s.Relation.GetTrust(ctx, 1, 2)
	if err != nil || lvl != trustzone.LevelFamily {
		t.Fatalf("get level=%v err=%v", lvl, err)
	}
	// upsert 改档位
	if err := s.Relation.GrantTrust(ctx, 1, 2, trustzone.LevelTrusted); err != nil {
		t.Fatalf("regrant: %v", err)
	}
	lvl, _ = s.Relation.GetTrust(ctx, 1, 2)
	if lvl != trustzone.LevelTrusted {
		t.Fatalf("updated level=%v want Trusted", lvl)
	}
	list, err := s.Relation.ListTrust(ctx, 1)
	if err != nil || len(list) != 1 || list[2] != trustzone.LevelTrusted {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := s.Relation.RevokeTrust(ctx, 1, 2); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.Relation.GetTrust(ctx, 1, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after revoke err=%v want ErrNotFound", err)
	}
}

func TestCapabilityContract(t *testing.T) {
	s := NewMemoryStore(10)
	ctx := context.Background()
	cap := trustzone.Capability{
		AgentID: "family-agent", TrustLevelMin: trustzone.LevelFamily,
		AllowedSkills: []string{"home-maintenance"}, RequiresApprovalAboveCents: 200000,
	}
	if err := s.Capability.UpsertCapability(ctx, cap); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := s.Capability.GetCapability(ctx, "family-agent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RequiresApprovalAboveCents != 200000 || len(got.AllowedSkills) != 1 {
		t.Fatalf("got cap=%+v", got)
	}
	all, err := s.Capability.ListCapabilities(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list=%d err=%v", len(all), err)
	}
	if _, err := s.Capability.GetCapability(ctx, "missing"); err == nil {
		t.Fatal("missing capability should error")
	}
}

func TestLedgerContract(t *testing.T) {
	s := NewMemoryStore(2)
	ctx := context.Background()
	_ = ctx
	if _, err := s.Ledger.Reserve("m1", 10, 20, ledger.ModeSendCharge, 100); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	e, err := s.Ledger.Charge("m1", "")
	if err != nil || e.Status != ledger.StatusCharged {
		t.Fatalf("charge e=%v err=%v", e, err)
	}
	if _, err := s.Ledger.Charge("m1", ""); !errors.Is(err, ledger.ErrAlreadyProcessed) {
		t.Fatalf("dup charge err=%v", err)
	}
}

func TestApprovalContract(t *testing.T) {
	s := NewMemoryStore(10)
	eng := approval.New(s.Approval)
	tk, err := eng.CreateTicket(1, "agent", approval.TicketPayment, map[string]any{"k": "v"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	loaded, err := s.Approval.Load(tk.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ID != tk.ID || loaded.State != approval.StatePending {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestNegotiationContract(t *testing.T) {
	s := NewMemoryStore(10)
	ctx := context.Background()
	sess := negotiation.NewSession("sess-x", 5, 0)
	if _, err := sess.Apply(negotiation.Turn{
		SessionID: "sess-x", FromAgentID: "a", ToAgentID: "b",
		Intent: negotiation.IntentPropose, Payload: map[string]any{"why": "合作"},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := s.Negotiation.SaveSession(ctx, sess); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.Negotiation.LoadSession(ctx, "sess-x")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.State != negotiation.StateWorking {
		t.Fatalf("state=%s want WORKING", got.State)
	}
}

func TestAuditSink(t *testing.T) {
	s := NewMemoryStore(10)
	err := s.Audit.Write(audit.Entry{
		At: time.Now(), MessageID: "m", AgentID: "a",
		Action: audit.ActionContentAllow, Decision: "ok",
	})
	if err != nil {
		t.Fatalf("audit write: %v", err)
	}
	mem, ok := s.Audit.(*audit.MemorySink)
	if !ok {
		t.Fatal("audit sink not memory")
	}
	if mem.Count()[audit.ActionContentAllow] != 1 {
		t.Fatalf("count=%v", mem.Count())
	}
}

// TestPostgresSkipped 真实 PostgreSQL 用例：设置 PG_DSN 时才运行（CI/资源闸门）。
// 驱动由服务主程序 blank import 注册；当前离线环境无驱动时 Open 会明确报错。
func TestPostgresSkipped(t *testing.T) {
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set; skipping PostgreSQL-backed store test")
	}
	db, err := Open(os.Getenv("PG_DRIVER"), dsn)
	if err != nil {
		t.Skipf("postgres driver/db unavailable: %v", err)
	}
	defer db.Close()
	s := NewPostgresStore(db, 10)
	ctx := context.Background()
	if err := s.Relation.GrantTrust(ctx, 9001, 9002, trustzone.LevelFamily); err != nil {
		t.Fatalf("pg grant: %v", err)
	}
	lvl, err := s.Relation.GetTrust(ctx, 9001, 9002)
	if err != nil || lvl != trustzone.LevelFamily {
		t.Fatalf("pg get level=%v err=%v", lvl, err)
	}
}
