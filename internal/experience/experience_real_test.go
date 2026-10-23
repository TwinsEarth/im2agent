package experience

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func db(t *testing.T) *sql.DB {
	dsn:=os.Getenv("PG_DSN")
	if dsn=="" { t.Skip("PG_DSN not set") }
	d,err:=sql.Open("pgx",dsn)
	if err!=nil { t.Skipf("open: %v",err) }
	for _,q:=range []string{
		`CREATE TABLE IF NOT EXISTS case_experiences(id BIGSERIAL PRIMARY KEY, user_id TEXT NOT NULL, scenario TEXT NOT NULL, decision TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', payload JSONB NOT NULL DEFAULT '{}'::jsonb, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS distilled_patterns(id BIGSERIAL PRIMARY KEY, pattern_text TEXT NOT NULL, source_case_ids JSONB NOT NULL DEFAULT '[]'::jsonb, hit_count BIGINT NOT NULL DEFAULT 0, approved_by_human BOOLEAN NOT NULL DEFAULT false, derived_at TIMESTAMPTZ NOT NULL DEFAULT now(), approved_at TIMESTAMPTZ)`,
		`CREATE TABLE IF NOT EXISTS propose_audit(id BIGSERIAL PRIMARY KEY, user_id TEXT NOT NULL, phase TEXT NOT NULL, proposal TEXT NOT NULL DEFAULT '', commit_verdict TEXT NOT NULL DEFAULT '', audit_verdict TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	}{
		if _,err:=d.Exec(q); err!=nil { t.Skipf("migrate: %v",err) }
	}
	suf:=fmt.Sprintf("v182_%d",time.Now().UnixNano())
	t.Cleanup(func(){
		d.Exec("DELETE FROM case_experiences WHERE user_id LIKE '"+suf+"_%'")
		d.Exec("DELETE FROM propose_audit WHERE user_id LIKE '"+suf+"_%'")
		d.Exec("DELETE FROM distilled_patterns WHERE pattern_text LIKE '"+suf+"_%'")
		d.Close()
	})
	return d
}

func TestV182TwoLayerAndAudit(t *testing.T) {
	d:=db(t); s:=New(d); ctx:=context.Background()
	suf:=fmt.Sprintf("v182_%d",time.Now().UnixNano()); user:=suf+"_u"

	// 1) 案例层：人类审批决策落库
	cid,err:=s.RecordCase(ctx,&Case{UserID:user,Scenario:"陌生人提议饭局",Decision:"reject",Reason:"不熟悉"})
	if err!=nil { t.Fatalf("case: %v",err) }
	if cid<=0 { t.Fatal("case id 应>0") }
	if _,err:=s.RecordCase(ctx,&Case{UserID:user,Scenario:"x",Decision:"model-self"}); err==nil {
		t.Fatal("非人类决策(model-self)应被拒绝写入")
	}
	cases,_:=s.ListCases(ctx,user)
	if len(cases)!=1 { t.Fatalf("案例应1条，实际 %d",len(cases)) }

	// 2) 模式层：提案默认不生效
	pid,err:=s.ProposePattern(ctx,suf+"_rule:不熟悉的饭局一律先问我",[]int64{cid})
	if err!=nil { t.Fatalf("propose pattern: %v",err) }
	eff,_:=s.EffectivePatterns(ctx)
	for _,p:=range eff { if p.ID==pid { t.Fatal("未批准的模式不应生效") } }
	// 人类批准后生效
	if err:=s.ApprovePattern(ctx,pid); err!=nil { t.Fatalf("approve: %v",err) }
	eff2,_:=s.EffectivePatterns(ctx)
	found:=false
	for _,p:=range eff2 { if p.ID==pid { found=true } }
	if !found { t.Fatalf("人类批准后模式应生效") }

	// 3) Propose-Commit-Audit：审计最终裁决归人类
	aid,err:=s.Propose(ctx,user,"自动接受邻居的代收包裹请求")
	if err!=nil { t.Fatalf("propose: %v",err) }
	if err:=s.FinalAudit(ctx,aid,"approve","reject"); err!=nil { t.Fatalf("final audit: %v",err) }
	rec,err:=s.GetAudit(ctx,aid)
	if err!=nil { t.Fatalf("get: %v",err) }
	if rec.AuditVerdict!="reject" || rec.Phase!="audit" { t.Fatalf("审计裁决应为人类 reject: %+v",rec) }
	// 模型 verdict 非法
	if err:=s.FinalAudit(ctx,aid,"approve","model-auto"); err==nil {
		t.Fatal("非人类 approve|reject 的审计裁决应被拒绝")
	}
}
