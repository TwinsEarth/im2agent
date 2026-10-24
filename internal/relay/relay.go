// Package relay 实现 v2.2.1 微信消息中枢（微信 → openclaw → IM2A → openclaw → 微信）。
//
// openclaw 收到微信消息后，通过 MCP 工具 im2a_route（或 REST webhook）把消息交给本服务：
//   - 发给自己的 Bot（私聊助手）→ 调主/备双通道大模型（DeepSeek 主，Kimi 备）智能回复；
//   - 显式发给另一个用户（@对方wxid 或 "发给<wxid>：…"）→ botroute 落库，并经 openclaw
//     出站 API 主动推送到对方微信（用户↔用户），后续由对方 Agent 做 A2A 协商/审批。
//
// 所有 inbound 消息与回复都落 wechat_relay_messages，支持幂等重放与审计。
package relay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"human-agent-im/internal/botroute"
	"human-agent-im/internal/model"
	"human-agent-im/internal/negotiation"
)

// Route 常量。
const (
	RouteSelfChat  = "self_chat"  // 跟自己的 Bot 对话
	RoutePeerRoute = "peer_route" // 转发给另一个用户
)

var (
	// ErrUnboundAccount 账号无法解析到 bot wxid。
	ErrUnboundAccount = errors.New("relay: account not bound to a wxid")
)

// PeerSender 是出站投递能力：由指定 openclaw bot 账号给 targetWxid 发微信，
// 返回 openclaw messageId。由 internal/openclaw.Sender 实现（relay 不直接依赖它）。
type PeerSender interface {
	Send(ctx context.Context, accountID, targetWxid, text string) (string, error)
}

// Service 是 relay 中枢。
type Service struct {
	db       *sql.DB
	resolver *AccountResolver
	mdl      model.Provider
	router   *botroute.Service
	sender   PeerSender
	nego     *WechatBridge
}

// New 构造 relay 服务。router 可为 nil（peer 路由不可用时仅私聊）。
func New(db *sql.DB, resolver *AccountResolver, mdl model.Provider, router *botroute.Service) *Service {
	return &Service{db: db, resolver: resolver, mdl: mdl, router: router}
}

// WithPeerSender 注入 openclaw 出站投递器（peer 消息真实推送到对方微信）。
func (s *Service) WithPeerSender(p PeerSender) *Service {
	s.sender = p
	return s
}

// WithNegotiation 注入微信↔A2A 协商桥接（nil 则 peer 退化为纯转发）。
func (s *Service) WithNegotiation(b *WechatBridge) *Service {
	s.nego = b
	return s
}

// Result 是一次 relay 的结果。
type Result struct {
	Reply         string `json:"reply"`
	Route         string `json:"route"`
	RoutedTo      string `json:"routedTo"` // primary / fallback
	BotWxid       string `json:"botWxid"`
	PeerWxid      string `json:"peerWxid,omitempty"`
	PeerDelivered bool   `json:"peerDelivered,omitempty"`
	PeerMessageID string `json:"peerMessageId,omitempty"`
	NegoSessionID string `json:"negoSessionId,omitempty"`
	NegoState     string `json:"negoState,omitempty"`
}

// peer 模式：@wxid 或 "发给 wxid ：..." / "告诉 wxid ..."
var peerAtRe = regexp.MustCompile(`@([A-Za-z0-9_\-]+@im\.wechat)\s*([\s\S]*)`)
var peerSendRe = regexp.MustCompile(`^(?:发给|转达|告诉|通知)\s*([A-Za-z0-9_\-]+@im\.wechat)[：:,\s]+([\s\S]+)`)

// systemPrompt 是个人助理的人设。
const systemPrompt = `你是 IM2A（im2agent）用户的专属 AI 助理，运行在用户的微信 Bot 内。
你的职责：作为用户的秘书/助理/管家，处理日程、提醒、待办、信息查询、写作、翻译、分析与日常问答。
要求：
- 用中文、简洁、有条理地回复，像一个靠谱的私人秘书；
- 涉及对外承诺、金钱、发消息给第三方、日程确认等动作时，先说明你将怎么做，等用户确认；
- 不知道的不要编造；可以主动追问关键信息。`

// Relay 处理一条微信消息，返回要回给发送者的文本。
func (s *Service) Relay(ctx context.Context, accountID, fromWxid, text, idemKey string) (*Result, error) {
	text = strings.TrimSpace(text)
	if accountID == "" || fromWxid == "" || text == "" {
		return nil, errors.New("relay: accountId/fromWxid/text required")
	}

	if idemKey != "" {
		if r, ok := s.byIdem(ctx, idemKey); ok {
			return r, nil
		}
	}

	botWxid, err := s.resolver.Resolve(accountID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnboundAccount, accountID, err)
	}

	if peerWxid, body, ok := parsePeer(text); ok {
		return s.handlePeer(ctx, accountID, botWxid, fromWxid, peerWxid, body, idemKey)
	}
	return s.handleSelfChat(ctx, accountID, botWxid, fromWxid, text, idemKey)
}

// parsePeer 解析转发意图，返回目标 wxid 与正文。
func parsePeer(text string) (peer, body string, ok bool) {
	if m := peerAtRe.FindStringSubmatch(text); m != nil {
		return m[1], strings.TrimSpace(m[2]), m[2] != ""
	}
	if m := peerSendRe.FindStringSubmatch(text); m != nil {
		return m[1], strings.TrimSpace(m[2]), m[2] != ""
	}
	return "", "", false
}

// handleSelfChat 私聊助手：取最近上下文 → 双通道模型 → 落库。
func (s *Service) handleSelfChat(ctx context.Context, accountID, botWxid, fromWxid, text, idemKey string) (*Result, error) {
	history, err := s.recentHistory(ctx, botWxid, fromWxid, 10)
	if err != nil {
		return nil, err
	}
	msgs := append([]model.Message{{Role: "system", Content: systemPrompt}}, history...)
	msgs = append(msgs, model.Message{Role: "user", Content: text})

	resp, err := s.mdl.Chat(ctx, &model.ChatRequest{
		Messages:    msgs,
		Temperature: 0.6,
		MaxTokens:   800,
	})
	if err != nil {
		return nil, fmt.Errorf("relay: model chat: %w", err)
	}
	reply := strings.TrimSpace(resp.Content)
	if reply == "" {
		reply = "（暂时没有组织好语言，稍后再试）"
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO wechat_relay_messages(account_id,bot_wxid,from_wxid,text,route,reply,routed_to,idem_key)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		accountID, botWxid, fromWxid, text, RouteSelfChat, reply, resp.RoutedTo, idemKey)
	if err != nil {
		return nil, err
	}

	return &Result{Reply: reply, Route: RouteSelfChat, RoutedTo: resp.RoutedTo, BotWxid: botWxid}, nil
}

// handlePeer 转发给另一个用户：botroute 落库 → openclaw 主动推送到对方微信 → 标记投递。
func (s *Service) handlePeer(ctx context.Context, accountID, botWxid, fromWxid, peerWxid, body, idemKey string) (*Result, error) {
	payload := map[string]any{
		"text":      body,
		"fromName":  fromWxid,
		"viaBot":    botWxid,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	var reply string
	var routeID int64
	routed := false
	var negoSID, negoState string

	if s.router != nil {
		id, err := s.router.Route(ctx, botWxid, peerWxid, "text", payload)
		if err != nil {
			if errors.Is(err, botroute.ErrUnbound) {
				reply = fmt.Sprintf("目标 %s 尚未绑定 IM2A Bot，暂无法转达。", peerWxid)
			} else if errors.Is(err, botroute.ErrSelf) {
				reply = "不能给自己转达。"
			} else {
				return nil, err
			}
		} else {
			routeID = id
			routed = true
			reply = fmt.Sprintf("已让你的 Agent 把这条消息转达给 %s，对方 Agent 会协商/审批后处理。", peerWxid)
		}
	} else {
		reply = "用户间路由未启用。"
	}

	// 建立 A2A 协商会话（8 态状态机），微信上下文落 negotiation_sessions/turns。
	if routed && s.nego != nil {
		if sess, nerr := s.nego.OpenProposal(ctx, accountID, botWxid, peerWxid, body); nerr == nil {
			negoSID, negoState = sess.ID, sess.State.String()
		}
	}

	// 经 openclaw 出站 API 主动推送到对方微信（由发送方 bot 账号 → 对方 wxid）。
	var (
		delivered bool
		peerMsgID string
	)
	if routed && s.sender != nil {
		if fromAccount := s.accountForWxid(ctx, botWxid); fromAccount != "" {
			outText := composePeerText(fromWxid, body)
			if negoSID != "" {
				outText = outText + fmt.Sprintf("\n— 协商单 %s（状态 %s），回复可推进协商", negoSID, negoState)
			}
			mid, serr := s.sender.Send(ctx, fromAccount, peerWxid, outText)
			if serr != nil {
				reply = fmt.Sprintf("消息已进入对方 Agent 收件箱，但实时推送失败（对方上线后会收到）：%v", serr)
			} else {
				delivered, peerMsgID = true, mid
				if routeID > 0 {
					_ = s.router.Deliver(ctx, routeID)
				}
				reply = fmt.Sprintf("已发送到 %s 的微信（对方已收到，messageId=%s）。", peerWxid, mid)
			}
		} else {
			reply = "已记录转达，但找不到发送方 bot 的 openclaw 账号，无法实时推送。"
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO wechat_relay_messages(account_id,bot_wxid,from_wxid,text,route,reply,routed_to,peer_wxid,peer_delivered,peer_message_id,idem_key)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		accountID, botWxid, fromWxid, body, RoutePeerRoute, reply, "", peerWxid, delivered, peerMsgID, idemKey)
	if err != nil {
		return nil, err
	}
	return &Result{
		Reply: reply, Route: RoutePeerRoute, BotWxid: botWxid, PeerWxid: peerWxid,
		PeerDelivered: delivered, PeerMessageID: peerMsgID,
		NegoSessionID: negoSID, NegoState: negoState,
	}, nil
}

// NegoReply 推进一个微信协商会话（供 webhook 显式回复：ACCEPT/COUNTER/REJECT/REQUEST_INFO/REQUEST_AUTH）。
func (s *Service) NegoReply(ctx context.Context, sessionID, responderWxid, intent, body string) (*negotiation.Session, error) {
	if s.nego == nil {
		return nil, errors.New("relay: negotiation disabled")
	}
	return s.nego.Reply(ctx, sessionID, responderWxid, intent, body)
}

// accountForWxid 反查 bot wxid 对应的 openclaw accountId（用于选定出站发送账号）。
func (s *Service) accountForWxid(ctx context.Context, botWxid string) string {
	all, err := s.resolver.AllAccounts()
	if err != nil {
		return ""
	}
	for acc, wx := range all {
		if wx == botWxid {
			return acc
		}
	}
	return ""
}

// composePeerText 构造发给对方微信的转达文本。
func composePeerText(fromWxid, body string) string {
	short := fromWxid
	if i := strings.Index(short, "@"); i > 0 {
		short = short[:i]
	}
	if len(short) > 10 {
		short = short[:10]
	}
	return fmt.Sprintf("【%s 的 AI 助手转达】\n%s", short, body)
}

// recentHistory 取同一会话最近 n 条往返（user/assistant 交替）。
func (s *Service) recentHistory(ctx context.Context, botWxid, fromWxid string, n int) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT text, reply FROM wechat_relay_messages
		 WHERE bot_wxid=$1 AND from_wxid=$2 AND route=$3 AND length(reply)>0
		 ORDER BY id DESC LIMIT $4`,
		botWxid, fromWxid, RouteSelfChat, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pair struct{ u, a string }
	var rev []pair
	for rows.Next() {
		var u, a string
		if err := rows.Scan(&u, &a); err != nil {
			return nil, err
		}
		rev = append(rev, pair{u, a})
	}
	var out []model.Message
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out,
			model.Message{Role: "user", Content: rev[i].u},
			model.Message{Role: "assistant", Content: rev[i].a},
		)
	}
	return out, nil
}

func (s *Service) byIdem(ctx context.Context, key string) (*Result, bool) {
	var (
		accountID, botWxid, from, text, route, reply, routedTo, peer, peerMsg string
		peerDelivered                                                        bool
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT account_id,bot_wxid,from_wxid,text,route,reply,routed_to,
		        peer_wxid,peer_delivered,peer_message_id
		 FROM wechat_relay_messages WHERE idem_key=$1`, key).
		Scan(&accountID, &botWxid, &from, &text, &route, &reply, &routedTo, &peer, &peerDelivered, &peerMsg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	return &Result{
		Reply: reply, Route: route, RoutedTo: routedTo, BotWxid: botWxid,
		PeerWxid: peer, PeerDelivered: peerDelivered, PeerMessageID: peerMsg,
	}, true
}
