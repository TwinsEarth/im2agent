package a2a

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func testDB(t *testing.T) *sql.DB {
	dsn:=os.Getenv("PG_DSN")
	if dsn=="" { t.Skip("PG_DSN not set") }
	db,err:=sql.Open("pgx",dsn)
	if err!=nil { t.Skipf("open: %v",err) }
	_,err=db.Exec(`CREATE TABLE IF NOT EXISTS a2a_envelopes (
		msg_id TEXT PRIMARY KEY, conv_id TEXT NOT NULL, from_agent TEXT NOT NULL,
		to_agent TEXT NOT NULL, msg_type TEXT NOT NULL DEFAULT 'proposal',
		payload JSONB NOT NULL DEFAULT '{}'::jsonb, delivered BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(), delivered_at TIMESTAMPTZ)`)
	if err!=nil { t.Skipf("migrate: %v",err) }
	suf:=fmt.Sprintf("v180_%d",time.Now().UnixNano())
	t.Cleanup(func(){
		db.Exec(`DELETE FROM a2a_envelopes WHERE msg_id LIKE '`+suf+`_%'`)
		db.Close()
	})
	return db
}

func TestV180SendIdempotentInboxDeliver(t *testing.T) {
	db:=testDB(t); s:=New(db); ctx:=context.Background()
	suf:=fmt.Sprintf("v180_%d",time.Now().UnixNano())
	fromA,toB:=suf+"_A",suf+"_B"
	env:=&Envelope{MsgID:suf+"_m1",ConvID:suf+"_conv",FromAgent:fromA,ToAgent:toB,
		Type:"proposal",Payload:map[string]any{"when":"Fri 15:00"}}
	if _,err:=s.Send(ctx,env); err!=nil { t.Fatalf("send: %v",err) }
	if _,err:=s.Send(ctx,env); err!=nil { t.Fatalf("send2: %v",err) }
	var n int
	db.QueryRow(`SELECT count(*) FROM a2a_envelopes WHERE msg_id=$1`,env.MsgID).Scan(&n)
	if n!=1 { t.Fatalf("幂等失败：%d 行（应1）",n) }
	inbox,err:=s.Inbox(ctx,toB,50)
	if err!=nil { t.Fatalf("inbox: %v",err) }
	found:=false
	for _,e:=range inbox {
		if e.MsgID==env.MsgID {
			found=true
			if e.Type!="proposal"||e.FromAgent!=fromA { t.Fatalf("字段错: %+v",e) }
		}
	}
	if !found { t.Fatalf("B 收件箱未含 %s",env.MsgID) }
	if other,_:=s.Inbox(ctx,suf+"_nobody",50); len(other)!=0 {
		t.Fatalf("越权：nobody %d 条（应0）",len(other))
	}
	if _,err:=s.Deliver(ctx,env.MsgID); err!=nil { t.Fatalf("deliver: %v",err) }
	for _,e:=range mustInbox(t,s,toB,ctx) {
		if e.MsgID==env.MsgID { t.Fatalf("deliver 后仍含 %s",env.MsgID) }
	}
	if noRows,_:=s.Deliver(ctx,env.MsgID); !noRows {
		t.Fatalf("重复 deliver 应 alreadyDelivered")
	}
}

func mustInbox(t *testing.T,s *Service,agent string,ctx context.Context) []*Envelope {
	t.Helper()
	l,err:=s.Inbox(ctx,agent,50)
	if err!=nil { t.Fatalf("inbox: %v",err) }
	return l
}
