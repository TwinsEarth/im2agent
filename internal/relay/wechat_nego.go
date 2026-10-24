package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"human-agent-im/internal/negotiation"
)

// NegoStore 协商持久化接口（store.NegotiationStore 天然满足）。
type NegoStore interface {
	SaveSession(ctx context.Context, s *negotiation.Session) error
	LoadSession(ctx context.Context, id string) (*negotiation.Session, error)
}

// WechatBridge 把微信 peer 消息桥接为 A2A 协商会话（8 态状态机），
// 让"用户↔用户"的 Agent 之间不是裸文本转发，而是可审计、可熔断、可上抛审批的协商。
type WechatBridge struct {
	store NegoStore
}

// NewWechatBridge 构造微信协商桥接；store 为 nil 时协商能力关闭（退化为纯转发）。
func NewWechatBridge(store NegoStore) *WechatBridge {
	if store == nil {
		return nil
	}
	return &WechatBridge{store: store}
}

// wechatPayload 是挂在协商 Turn.Payload 上的微信上下文。
func wechatPayload(accountID, fromWxid, text string) map[string]any {
	return map[string]any{
		"channel":  "wechat",
		"account":  accountID,
		"fromWxid": fromWxid,
		"text":     text,
		"tsUnix":   time.Now().Unix(),
	}
}

func newSessionID(fromWxid, peerWxid string) string {
	h := sha256.Sum256([]byte(fromWxid + "|" + peerWxid + "|" + strconv.FormatInt(time.Now().UnixNano(), 10)))
	return "wnego_" + hex.EncodeToString(h[:])[:24]
}

// OpenProposal 由 fromBotWxid 向 peerWxid 发起一个微信协商（首轮 PROPOSE），
// 状态机推进到 WORKING 并持久化 session + turn。
func (b *WechatBridge) OpenProposal(ctx context.Context, accountID, fromBotWxid, peerWxid, body string) (*negotiation.Session, error) {
	if b == nil || b.store == nil {
		return nil, errors.New("relay: negotiation bridge not configured")
	}
	id := newSessionID(fromBotWxid, peerWxid)
	sess := negotiation.NewSession(id, negotiation.DefaultMaxRounds, 0)
	t := negotiation.Turn{
		SessionID:   id,
		FromAgentID: fromBotWxid,
		ToAgentID:   peerWxid,
		Intent:      negotiation.IntentPropose,
		Payload:     wechatPayload(accountID, fromBotWxid, body),
	}
	if _, err := sess.Apply(t); err != nil {
		return nil, err
	}
	if err := b.store.SaveSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// Reply 推进一个已存在的微信协商会话。
// responderWxid 为当前回复方（from 或 to），intent 为 ACCEPT/COUNTER/REJECT/REQUEST_INFO/REQUEST_AUTH。
func (b *WechatBridge) Reply(ctx context.Context, sessionID, responderWxid, intentStr, body string) (*negotiation.Session, error) {
	if b == nil || b.store == nil {
		return nil, errors.New("relay: negotiation bridge not configured")
	}
	sess, err := b.store.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	intent := negotiation.Intent(strings.ToUpper(strings.TrimSpace(intentStr)))
	switch intent {
	case negotiation.IntentAccept, negotiation.IntentCounter, negotiation.IntentReject,
		negotiation.IntentRequestInfo, negotiation.IntentRequestAuth:
	default:
		return nil, fmt.Errorf("relay: invalid nego intent %q", intentStr)
	}
	// 回复方是另一方（对最新一轮的发起者）。
	other := sess.Turns[len(sess.Turns)-1].FromAgentID
	if other == responderWxid {
		if len(sess.Turns) >= 2 {
			other = sess.Turns[len(sess.Turns)-2].FromAgentID
		}
	}
	t := negotiation.Turn{
		SessionID:   sessionID,
		FromAgentID: responderWxid,
		ToAgentID:   other,
		Intent:      intent,
		Payload:     wechatPayload("", responderWxid, body),
	}
	if _, err := sess.Apply(t); err != nil && !errors.Is(err, negotiation.ErrRoundLimit) {
		return nil, err
	}
	if err := b.store.SaveSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}
