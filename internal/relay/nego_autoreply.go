package relay

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RouteNegoReply 自由微信消息自动挂接到未完成的 A2A 协商。
const RouteNegoReply = "nego_reply"

// autoReplyOpenNego 把"自由文本"自动当作 REQUEST_INFO 推进到该 wxid 最近一个未完成协商，
// 并把协商回执主动 push 给对方手机。命中返回 (result,true,nil)；无未完成协商返回 (nil,false,nil)。
//
// 设计要点（v2.2.5 真机打通缺口）：
//   - 手机自由打字无法携带 negoSessionId，因此按"该 bot id 是任一方、24h 内非终态"定位最近协商；
//   - 推进后由【对方自己的 bot 账号】给对方自己的 ilink id 发回执（与 peer 投递同一套正确姿势）；
//   - 同时落 wechat_relay_messages 审计行。
func (s *Service) autoReplyOpenNego(ctx context.Context, accountID, botWxid, fromWxid, text, idemKey string) (*Result, bool, error) {
	if s == nil || s.nego == nil || s.db == nil {
		return nil, false, nil
	}
	var sessID, other string
	// 关键：只挂到“最后一轮是对方发来、正等我回复”的协商；
	// 若我发起的协商还在等对方回复（最后一轮 from=我自己），自由文本不硬挂，避免鸡同鸭讲。
	err := s.db.QueryRowContext(ctx, `
SELECT ns.id, CASE WHEN ns.from_agent_id=$1 THEN ns.to_agent_id ELSE ns.from_agent_id END
FROM negotiation_sessions ns
WHERE (ns.from_agent_id=$1 OR ns.to_agent_id=$1)
  AND ns.state IN ('WORKING','INPUT_REQUIRED','AUTH_REQUIRED')
  AND ns.updated_at > now() - interval '24 hours'
  AND (SELECT nt.from_agent_id FROM negotiation_turns nt WHERE nt.session_id=ns.id ORDER BY nt.created_at DESC LIMIT 1) <> $1
ORDER BY ns.updated_at DESC LIMIT 1`, fromWxid).Scan(&sessID, &other)
	if err == sql.ErrNoRows || err != nil {
		return nil, false, nil
	}

	sess, err := s.nego.Reply(ctx, sessID, fromWxid, "REQUEST_INFO", text)
	if err != nil {
		return nil, true, err
	}

	delivered, pmid := false, ""
	if s.sender != nil && other != "" {
		if acc := s.accountForWxid(ctx, other); acc != "" {
			short := fromWxid
			if i := strings.Index(short, "@"); i > 0 {
				short = short[:i]
			}
			summary := fmt.Sprintf("%s：%s", s.userName(ctx, fromWxid), text)
			if mid, serr := s.sender.Send(ctx, acc, other, summary); serr == nil {
				delivered, pmid = true, mid
			}
		}
	}

	reply := fmt.Sprintf("好，跟%s说了。", s.userName(ctx, other))
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO wechat_relay_messages(account_id,bot_wxid,from_wxid,text,route,reply,routed_to,idem_key)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		accountID, botWxid, fromWxid, text, RouteNegoReply, reply, string(sess.State), idemKey); err != nil {
		// 审计落库失败不影响主链路
		_ = err
	}
	return &Result{
		Reply: reply, Route: RouteNegoReply, BotWxid: botWxid,
		PeerWxid: other, PeerDelivered: delivered, PeerMessageID: pmid,
		NegoSessionID: sessID, NegoState: string(sess.State),
	}, true, nil
}

var _ = time.Now // keep time import if future throttling needed
