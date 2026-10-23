package api

import (
	"context"
	"net/http"
)

// AdminMux v1.7.6：运营后台总入口——管理仪表盘真实数据聚合（只读）。
// 路由在网关侧用独立管理员 Token 保护（IM2A_ADMIN_TOKEN，缺省回退 GATEWAY_TOKEN）。
func (d *Deps) AdminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/admin/stats", d.adminStats)
	return mux
}

func (d *Deps) adminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := map[string]any{}

	// 用户 / IM 账号 / Agent 注册
	stats["users_total"] = d.countQ(ctx, `SELECT count(*) FROM users`)
	stats["im_accounts"] = d.countQ(ctx, `SELECT count(*) FROM account_identities`)
	stats["agent_capabilities"] = d.countQ(ctx, `SELECT count(*) FROM agent_capabilities`)

	// 付费（陌生人付费注意力）
	stats["paid_total_cents"] = d.sumQ(ctx, `SELECT COALESCE(sum(amount_cents),0) FROM paid_message_ledger WHERE status=1`)
	stats["paid_charged"] = d.countQ(ctx, `SELECT count(*) FROM paid_message_ledger WHERE status=1`)
	stats["paid_refunded"] = d.countQ(ctx, `SELECT count(*) FROM paid_message_ledger WHERE status=2`)

	// 审批 / 协商
	stats["approval_total"] = d.countQ(ctx, `SELECT count(*) FROM approval_tickets`)
	stats["approval_by_state"] = d.groupCount(ctx, `SELECT state, count(*) FROM approval_tickets GROUP BY state`)
	stats["negotiation_total"] = d.countQ(ctx, `SELECT count(*) FROM negotiation_sessions`)
	stats["negotiation_by_state"] = d.groupCount(ctx, `SELECT state, count(*) FROM negotiation_sessions GROUP BY state`)

	// 模型调用 / 出端 / 内容审核（来自审计事件流）
	stats["model_calls"] = d.countQ(ctx, `SELECT count(*) FROM audit_events WHERE action='model_call'`)
	stats["edge_egress"] = d.countQ(ctx, `SELECT count(*) FROM audit_events WHERE action='edge_egress'`)
	stats["content_audited"] = d.countQ(ctx, `SELECT count(*) FROM audit_events WHERE action='CONTENT_ALLOWED' OR action='CONTENT_BLOCKED'`)
	stats["audit_total"] = d.countQ(ctx, `SELECT count(*) FROM audit_events`)

	// IM 人类消息通道
	stats["im_messages"] = d.countQ(ctx, `SELECT count(*) FROM im_messages`)
	stats["im_webhook_events"] = d.countQ(ctx, `SELECT count(*) FROM im_webhook_events`)
	stats["im_relations"] = d.countQ(ctx, `SELECT count(*) FROM im_relations`)
	stats["push_pending"] = d.countQ(ctx, `SELECT count(*) FROM im_push_tasks WHERE status='pending'`)

	// 客服工单
	stats["tickets"] = d.countQ(ctx, `SELECT count(*) FROM ticket_state`)

	writeJSON(w, http.StatusOK, map[string]any{
		"service": "IM2A Admin", "status": "ok", "stats": stats,
	})
}

func (d *Deps) countQ(r context.Context, q string) int {
	var n int
	_ = d.DB.QueryRowContext(r, q).Scan(&n)
	return n
}
func (d *Deps) sumQ(r context.Context, q string) int64 {
	var n int64
	_ = d.DB.QueryRowContext(r, q).Scan(&n)
	return n
}
func (d *Deps) groupCount(r context.Context, q string) map[string]int {
	out := map[string]int{}
	rows, err := d.DB.QueryContext(r, q)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err == nil {
			out[k] = n
		}
	}
	return out
}

// AdminPage 运营后台单页仪表盘（深色，自包含；数据由 /v1/admin/stats 带管理员 Token 拉取）。
func (d *Deps) AdminPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(adminHTML))
	})
}

const adminHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>IM2A 运营后台</title>
<style>
:root{--bg:#0b0f14;--card:#131a22;--line:#222c38;--fg:#e6edf3;--mut:#8b98a5;--acc:#3b82f6;--ok:#22c55e;--warn:#f59e0b;--bad:#ef4444}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--fg);font:14px/1.5 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;padding:24px}
h1{font-size:20px;margin-bottom:4px}.sub{color:var(--mut);margin-bottom:18px}
.bar{display:flex;gap:10px;align-items:center;margin-bottom:22px}
input{background:var(--card);border:1px solid var(--line);color:var(--fg);border-radius:8px;padding:9px 12px;width:320px}
button{background:var(--acc);border:0;color:#fff;border-radius:8px;padding:9px 16px;cursor:pointer;font-weight:600}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:14px}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:16px}
.card .k{color:var(--mut);font-size:12px;margin-bottom:8px}.card .v{font-size:26px;font-weight:700}
.card .v small{font-size:13px;color:var(--mut);font-weight:400}
.sec{margin-top:26px}.sec h2{font-size:15px;color:var(--mut);margin-bottom:10px;font-weight:600}
.chips{display:flex;flex-wrap:wrap;gap:8px}
.chip{background:var(--card);border:1px solid var(--line);border-radius:999px;padding:6px 12px;font-size:13px}
.chip b{color:var(--acc)}
.err{color:var(--bad)}.ok{color:var(--ok)}
</style></head><body>
<h1>IM2A 运营后台</h1>
<div class="sub">人机混合 IM · 实时运营仪表盘 · 数据来自生产 PG 实时聚合</div>
<div class="bar">
  <input id="tok" type="password" placeholder="管理员 Token（IM2A_ADMIN_TOKEN）">
  <button onclick="load()">刷新</button>
  <span id="status" class="sub"></span>
</div>
<div class="grid" id="grid"></div>
<div class="sec"><h2>审批状态分布</h2><div class="chips" id="appr"></div></div>
<div class="sec"><h2>协商状态分布</h2><div class="chips" id="nego"></div></div>
<script>
const MONEY=["users_total","im_accounts","agent_capabilities"];
const CARDS=[
 ["users_total","用户总数"],["im_accounts","IM 账号"],["agent_capabilities","Agent 能力"],
 ["paid_total_cents","付费收入(分)"],["paid_charged","已计费笔数"],["paid_refunded","已退款"],
 ["approval_total","审批单总数"],["negotiation_total","协商会话"],
 ["model_calls","模型调用"],["edge_egress","出端事件"],["content_audited","内容审核"],
 ["im_messages","IM 消息"],["im_webhook_events","Webhook 事件"],["im_relations","关系链"],
 ["push_pending","待推送"],["tickets","客服工单"],["audit_total","审计事件总数"]
];
async function load(){
 const tok=document.getElementById("tok").value.trim();
 localStorage.setItem("im2a_admin_tok",tok);
 const st=document.getElementById("status");
 try{
  const r=await fetch("/v1/admin/stats",{headers:{Authorization:"Bearer "+tok}});
  if(!r.ok){st.className="err";st.textContent="HTTP "+r.status;return;}
  const j=await r.json();const s=j.stats||{};
  st.className="ok";st.textContent="更新于 "+new Date().toLocaleTimeString();
  document.getElementById("grid").innerHTML=CARDS.map(([k,label])=>{
   let v=s[k]??0;
   if(k==="paid_total_cents") v=(v/100).toFixed(2)+" 元";
   return '<div class="card"><div class="k">'+label+'</div><div class="v">'+v+'</div></div>';
  }).join("");
  chips("appr",s.approval_by_state);chips("nego",s.negotiation_by_state);
 }catch(e){st.className="err";st.textContent=String(e);}
}
function chips(id,obj){const o=obj||{};document.getElementById(id).innerHTML=
 Object.entries(o).map(([k,v])=>'<span class="chip">'+k+' <b>'+v+'</b></span>').join("")||'<span class="chip">无</span>';}
window.addEventListener("load",()=>{const t=localStorage.getItem("im2a_admin_tok");if(t){document.getElementById("tok").value=t;load();}});
</script></body></html>`
