package relay

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"human-agent-im/internal/botroute"
	"human-agent-im/internal/store"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// 微信 peer 消息 → A2A 协商会话真实落库；对方 ACCEPT 推进到 COMPLETED。
func TestRelayWechatNegoReal(t *testing.T) {
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set")
	}
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	dir := t.TempDir()
	suf := fmt.Sprintf("vnego_%d", time.Now().UnixNano())
	accountID := suf + "-im-bot"
	botWxid := suf + "_bot@im.wechat"
	fromWxid := suf + "_owner@im.wechat"
	peerWxid := suf + "_peer@im.wechat"
	if err := os.WriteFile(filepath.Join(dir, accountID+".json"),
		[]byte(`{"userId":"`+botWxid+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM negotiation_sessions WHERE from_agent_id LIKE $1 OR to_agent_id LIKE $1`, suf+"%")
		_, _ = d.Exec(`DELETE FROM wechat_relay_messages WHERE account_id=$1 OR from_wxid LIKE $2 OR bot_wxid LIKE $2`, accountID, suf+"%")
		_, _ = d.Exec(`DELETE FROM bot_route_messages WHERE from_wxid LIKE $1 OR to_wxid LIKE $1`, suf+"%")
		_, _ = d.Exec(`DELETE FROM wechat_identity WHERE wxid LIKE $1`, suf+"%")
	})

	if _, err := d.Exec(`INSERT INTO wechat_identity(wxid,user_id) VALUES($1,930001),($2,930002) ON CONFLICT DO NOTHING`, botWxid, peerWxid); err != nil {
		t.Fatal(err)
	}

	negoStore := store.NewPostgresStore(d, 10).Negotiation
	svc := New(d, NewAccountResolver(dir), &fakeModel{reply: "x"}, botroute.New(d)).
		WithPeerSender(&fakeSender{}).
		WithNegotiation(NewWechatBridge(negoStore))
	ctx := context.Background()

	// 1) peer 发起协商（PROPOSE）→ WORKING
	res, err := svc.Relay(ctx, accountID, fromWxid,
		"@"+peerWxid+" 我想约你明天下午三点开会，方便吗？", "nego_"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if res.NegoSessionID == "" || res.NegoState != "WORKING" {
		t.Fatalf("want nego WORKING, got sid=%q state=%q", res.NegoSessionID, res.NegoState)
	}

	var state, fromA, toA string
	var round, turnCount int
	err = d.QueryRow(`SELECT state, round, from_agent_id, to_agent_id FROM negotiation_sessions WHERE id=$1`,
		res.NegoSessionID).Scan(&state, &round, &fromA, &toA)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if state != "WORKING" || round != 1 || fromA != botWxid || toA != peerWxid {
		t.Fatalf("session state=%s round=%d from=%s to=%s", state, round, fromA, toA)
	}
	if err := d.QueryRow(`SELECT count(*) FROM negotiation_turns WHERE session_id=$1`, res.NegoSessionID).Scan(&turnCount); err != nil {
		t.Fatal(err)
	}
	if turnCount != 1 {
		t.Fatalf("want 1 turn after propose, got %d", turnCount)
	}

	// 2) 对方 Agent 回复 ACCEPT → COMPLETED，turns=2
	sess2, err := svc.NegoReply(ctx, res.NegoSessionID, peerWxid, "ACCEPT", "同意，明天三点见")
	if err != nil {
		t.Fatalf("nego reply: %v", err)
	}
	if sess2.State.String() != "COMPLETED" {
		t.Fatalf("want COMPLETED, got %s", sess2.State)
	}
	if err := d.QueryRow(`SELECT count(*) FROM negotiation_turns WHERE session_id=$1`, res.NegoSessionID).Scan(&turnCount); err != nil {
		t.Fatal(err)
	}
	if turnCount != 2 {
		t.Fatalf("want 2 turns after accept, got %d", turnCount)
	}
}
