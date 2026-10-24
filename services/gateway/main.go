// gateway：统一 API 网关（v0.5.0）
//
// 把 7 个服务的 HTTP API 合并到一个端口，统一路由、认证、限流。
// 运行：go run ./services/gateway -port 8080
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"human-agent-im/internal/botroute"
	"human-agent-im/internal/broadcast"
	"human-agent-im/internal/gray"
	"human-agent-im/internal/metrics"
	"human-agent-im/internal/model"
	"human-agent-im/internal/openclaw"
	"human-agent-im/internal/relay"
	"human-agent-im/internal/tempgroup"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"human-agent-im/internal/api"
	"human-agent-im/internal/auth"
	"human-agent-im/internal/masking"
	"human-agent-im/internal/privacy"
	"human-agent-im/internal/push"
	"human-agent-im/internal/ratelimit"
	"human-agent-im/internal/security"
	"human-agent-im/internal/version"
)

const adminLoginHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Admin Login</title>
<style>body{background:#0b0f14;color:#e6edf3;font-family:sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh}
.box{background:#131a22;padding:32px;border-radius:12px;width:340px}
input{width:100%;padding:10px;margin:10px 0;background:#0b0f14;border:1px solid #222c38;color:#fff;border-radius:6px}
button{width:100%;padding:10px;background:#3b82f6;border:0;color:#fff;border-radius:6px;cursor:pointer}
.err{color:#ef4444;font-size:12px;min-height:16px}</style></head><body>
<div class="box"><h2>IM2A Admin</h2><form onsubmit="event.preventDefault();const t=document.getElementById('t').value;if(t){document.cookie='admin_token='+t+';path=/;max-age=86400';location.reload()}">
<input id="t" type="password" placeholder="Admin Token"><button>登录</button></form>
<div class="err" id="e"></div></div>
<script>if(location.search.includes('e=1')){document.getElementById('e').textContent='Token 错误'}</script>
</body></html>`
const wechatBindHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>微信绑定</title>
<style>
body{font-family:-apple-system,sans-serif;background:#1a1a2e;color:#fff;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0}
.card{background:#16213e;padding:40px;border-radius:16px;text-align:center;max-width:420px}
h1{font-size:20px;margin:0 0 8px}
p{color:#aaa;font-size:13px;margin:8px 0}
#qr{margin:20px auto;background:#fff;padding:12px;border-radius:8px;width:240px;height:240px;display:flex;align-items:center;justify-content:center}
#qr img{width:100%;height:100%}
#timer{font-size:28px;color:#6cf;font-weight:700;margin:10px 0}
button{background:#0f3460;color:#fff;border:none;padding:10px 24px;border-radius:6px;cursor:pointer;font-size:14px;margin-top:10px}
button:hover{background:#164880}
a{color:#6cf;font-size:12px;word-break:break-all}
#status{margin-top:14px;font-size:13px;color:#ffd166}
</style></head><body>
<div class="card">
<h1>绑定你的微信</h1>
<p>用微信扫一扫下方二维码，5 分钟内有效</p>
<div id="guide" style="display:flex;gap:8px;justify-content:center;margin:6px 0 2px;flex-wrap:wrap">
<span style="background:#0f3460;padding:5px 10px;border-radius:14px;font-size:12px">① 打开微信</span>
<span style="background:#0f3460;padding:5px 10px;border-radius:14px;font-size:12px">② 点右上角「+」</span>
<span style="background:#0f3460;padding:5px 10px;border-radius:14px;font-size:12px">③ 扫一扫</span>
<span style="background:#164880;padding:5px 10px;border-radius:14px;font-size:12px">④ 确认登录</span>
</div>
<div id="qr"><span style="color:#999">加载中...</span></div>
<div id="timer">05:00</div>
<button onclick="start()">重新生成二维码</button>
<p id="status"></p>
<a id="link" target="_blank"></a>
</div>
<script>
const TOK="im2a_demo_token";let sec=300,timer;
async function start(){
  document.getElementById('status').textContent='启动 OpenClaw...';
  const r=await fetch('/v1/openclaw/start',{method:'POST',headers:{Authorization:'Bearer '+TOK}});
  const j=await r.json();
  const st=document.getElementById('status');
  function show(msg){st.textContent=msg;}
  if(j.queued){
    show(j.message+'：前面排第 '+(j.position||'?')+' 位，预计约 '+((j.etaSeconds||0)/60).toFixed(1)+' 分钟。'+(j.nextStep||''));
    const owner=j.owner;
    clearInterval(timer);
    timer=setInterval(async()=>{
      try{
        const q=await (await fetch('/v1/openclaw/pool/queue?owner='+encodeURIComponent(owner),{headers:{Authorization:'Bearer '+TOK}})).json();
        if(q.ready&&q.qrUrl){clearInterval(timer);draw(q);}
        else if(q.queued){show(q.message+'：前面排第 '+(q.position||'?')+' 位，预计约 '+((q.etaSeconds||0)/60).toFixed(1)+' 分钟。');}
        else {show('状态刷新中...');}
      }catch(e){show('网络抖动，正在重试...');}
    },2000);
    return;
  }
  if(!j.qrUrl){show((j.message||'失败: '+(j.error||JSON.stringify(j)))+(j.nextStep?'（'+j.nextStep+'）':''));return}
  draw(j);
  function draw(j){
    document.getElementById('qr').innerHTML='<img src="https://api.qrserver.com/v1/create-qr-code/?size=220x220&margin=0&data='+encodeURIComponent(j.qrUrl)+'">';
    document.getElementById('link').href=j.qrUrl;
    document.getElementById('link').textContent='或点此链接打开';
    sec=300;clearInterval(timer);
    timer=setInterval(()=>{
      sec--;
      if(sec<0){clearInterval(timer);show('二维码已过期，'+(j.nextStep||'点「重新生成二维码」再扫'));return}
      const m=Math.floor(sec/60),s=sec%60;
      document.getElementById('timer').textContent=String(m).padStart(2,'0')+':'+String(s).padStart(2,'0');
    },1000);
    show('等待扫码...（扫码成功会直接发欢迎语到你微信）');
  }
}
start();
</script></body></html>
`
const playgroundHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>IM2A API Playground</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,Segoe UI,Roboto,sans-serif;background:#0f1419;color:#e6e6e6;height:100vh;display:flex;flex-direction:column}
header{padding:10px 16px;background:#1a2230;border-bottom:1px solid #2a3444;display:flex;gap:12px;align-items:center}
header b{color:#6cf;font-size:14px}
header input{background:#0f1419;border:1px solid #2a3444;color:#e6e6e6;padding:6px 10px;border-radius:4px;font-size:12px;font-family:monospace}
#tok{width:340px}
#base{width:240px}
main{flex:1;display:flex;overflow:hidden}
#left{width:280px;border-right:1px solid #2a3444;overflow-y:auto;padding:8px}
#left .g{color:#6cf;font-size:11px;margin:10px 0 4px;text-transform:uppercase}
#left button{display:block;width:100%;text-align:left;background:none;border:none;color:#ccc;padding:5px 8px;font-size:12px;cursor:pointer;border-radius:3px;font-family:monospace}
#left button:hover{background:#1a2230}
#left button.active{background:#23406b;color:#fff}
#left .m{display:inline-block;width:42px;font-weight:700}
.mPOST{color:#6f6}.mGET{color:#69f}.mDELETE{color:#f66}
#mid{flex:1;padding:16px;overflow-y:auto;border-right:1px solid #2a3444}
#mid label{display:block;font-size:12px;color:#999;margin:10px 0 4px}
#mid input,#mid select,#mid textarea{width:100%;background:#0a0e14;border:1px solid #2a3444;color:#e6e6e6;padding:8px;border-radius:4px;font-family:monospace;font-size:12px}
#mid textarea{min-height:140px;resize:vertical}
#mid button{margin-top:14px;background:#23406b;color:#fff;border:none;padding:10px 24px;border-radius:4px;cursor:pointer;font-size:13px}
#mid button:hover{background:#2d5188}
#right{flex:1;padding:16px;overflow-y:auto}
#right h3{font-size:12px;color:#999;margin-bottom:8px}
#out{background:#0a0e14;border:1px solid #2a3444;border-radius:4px;padding:12px;font-family:monospace;font-size:12px;white-space:pre-wrap;word-break:break-all;min-height:300px}
code{color:#6cf}
</style>
</head>
<body>
<header>
  <b>IM2A API</b>
  <input id="base" value="https://geefish.cn">
  <input id="tok" placeholder="Bearer token" value="im2a_demo_token">
</header>
<main>
<div id="left"></div>
<div id="mid">
  <label>方法</label><select id="method"></select>
  <label>路径（{id} 用下方替换）</label><input id="path">
  <label>路径参数</label><input id="pathParams" placeholder="例如：id=123 或 qrId=oc_xxx">
  <label>Body (JSON)</label><textarea id="body">{}</label>
  <button onclick="send()">发送 →</button>
</div>
<div id="right">
  <h3>响应</h3>
  <div id="out">点左侧端点开始</div>
</div>
</main>
<script>
const EP=[
 ['健康',['GET','/healthz',{}]],
 ['监控',['GET','/metrics',{}]],
 ['认证',['POST','/v1/auth/login',{user_id:"u_demo",device:"web"}],['GET','/v1/auth/me',{}]],
 ['微信身份',['POST','/v1/wechat/identify',{wxid:"o_demo"}]],
 ['Bot组',['POST','/v1/wechat/groups',{owner:"u_demo",name:"我的组"}],['POST','/v1/wechat/bindings',{groupId:"g_xxx",wxid:"o_demo",device:"ios"}]],
 ['OpenClaw 扫码',['POST','/v1/openclaw/qr',{owner:"u_demo",device:"ios"}],['POST','/v1/openclaw/callback',{qrId:"oc_xxx",wxid:"wx_demo"}],['GET','/v1/openclaw/qr/{qrId}',{qrId:"oc_xxx"}]],
 ['审批',['GET','/v1/approvals',{}]],
 ['协商',['GET','/v1/negotiations',{}]],
 ['付费',['GET','/v1/paid/inbox',{}]],
 ['Agent',['GET','/v1/agents/directory',{}]],
 ['临时群',['POST','/v1/tempgroup/create',{peer:"o_other"}]],
 ['Bot 路由',['POST','/v1/botroute/send',{to:"wx_target",text:"hello"}]],
 ['工作汇报',['POST','/v1/report/generate',{userId:"u_demo"}]],
];
const left=document.getElementById('left');
EP.forEach(([g,eps])=>{
  const h=document.createElement('div');h.className='g';h.textContent=g;left.appendChild(h);
  eps.forEach(ep=>{
    const b=document.createElement('button');
    b.innerHTML=` + "`" + `<span class="m m${ep[0]}">${ep[0]}</span> ${ep[1]}` + "`" + `;
    b.onclick=()=>select(b,ep);
    left.appendChild(b);
  });
});
let cur=null;
function select(btn,ep){
  document.querySelectorAll('#left button').forEach(x=>x.classList.remove('active'));
  btn.classList.add('active');cur=ep;
  document.getElementById('method').innerHTML=` + "`" + `<option>${ep[0]}</option>` + "`" + `;
  document.getElementById('path').value=ep[1];
  document.getElementById('body').value=JSON.stringify(ep[2],null,2);
  document.getElementById('out').textContent='';
}
async function send(){
  const base=document.getElementById('base').value.replace(/\/$/,'');
  let path=document.getElementById('path').value;
  const pp=document.getElementById('pathParams').value;
  if(pp) pp.split(';').forEach(kv=>{const[k,v]=kv.split('=');path=path.replace('{'+k.trim()+'}',v.trim())});
  const tok=document.getElementById('tok').value;
  const out=document.getElementById('out');
  out.textContent='请求中...';
  try{
    const opt={method:document.getElementById('method').value,headers:{Authorization:'Bearer '+tok,'Content-Type':'application/json'}};
    if(opt.method!=='GET'&&opt.method!=='DELETE') opt.body=document.getElementById('body').value;
    const t0=Date.now();
    const r=await fetch(base+path,opt);
    const txt=await r.text();
    let pretty=txt;try{pretty=JSON.stringify(JSON.parse(txt),null,2)}catch(e){}
    out.textContent=` + "`" + `HTTP ${r.status}  ${Date.now()-t0}ms\n\n${pretty}` + "`" + `;
  }catch(e){out.textContent='ERR: '+e.message}
}
</script>
</body></html>
`

func main() {
	port := flag.Int("port", 8080, "listen port")
	selftest := flag.Bool("selftest", false, "run self-test")
	flag.Parse()

	if *selftest {
		runSelfTest()
		return
	}

	deps, err := api.NewDepsFromEnv()
	if err != nil {
		log.Fatalf("deps: %v", err)
	}

	mux := http.NewServeMux()
	// v1.9.9：/metrics 与 /healthz 均要求 admin token 鉴权（IM2A_ADMIN_TOKEN）
	adminProbeToken := os.Getenv("IM2A_ADMIN_TOKEN")
	mux.Handle("/metrics", auth.RequireToken(metrics.Handler(), adminProbeToken)) // Prometheus 抓取端点
	mux.Handle("/healthz", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.HealthHandler("gateway")(w, r)
	}), adminProbeToken))
	// A7：出站排队/并发/熔断可观测（admin 鉴权）
	mux.Handle("/v1/ops/outbox", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(openclaw.SnapshotOutbox())
	}), adminProbeToken))

	// v2.3.0 L4 编排骨架状态（默认 L4_ENABLED=off，回退 legacy self_chat）
	mux.Handle("/v1/ops/orchestration", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"l4Enabled": os.Getenv("L4_ENABLED") == "on",
			"phase":     "A-skeleton",
			"package":   "internal/orchestration (Commander+Plan+Dispatcher, tests only)",
		})
	}), adminProbeToken))

	// 根路径：欢迎页（不经过认证/限流），方便用户直接打开 IP 验证
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"service": "IM2A Gateway",
			"status":  "ok",
			"version": version.Version,
			"commit":  version.GitCommit,
			"docs":    "/healthz, /v1/events, /v1/trust/, /v1/approvals/, /v1/negotiations/, /v1/paid/, /v1/compliance/, /v1/msg/, /v1/memory/, /v1/distill/, /v1/model/",
			"auth":    "Bearer token required",
		})
	})

	// 中间件链：认证 → 限流
	token := os.Getenv("GATEWAY_TOKEN")
	auditor := security.New(deps.DB)
	corsAllowed := []string{"https://geefish.cn", "https://www.geefish.cn"}
	if v := os.Getenv("CORS_ALLOWED"); v != "" {
		corsAllowed = strings.Split(v, ",")
	}
	// v1.9.0/v1.9.1：JWT 身份（wrap 统一认证需先取密钥）
	jwtSecret := os.Getenv("IM2A_JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "im2a-dev-jwt-secret-CHANGE-ME"
		log.Println("WARN: IM2A_JWT_SECRET not set; using insecure dev secret")
	}
	// v1.9.7：限流阈值可经环境变量配置（默认 100 rps / burst 200，GA 压测可调大）
	limitRPS, limitBurst := 100, 200
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limitRPS = n
		}
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limitBurst = n
		}
	}
	// 中间件链（外→内）：CORS 白名单 → 安全审计落库 → 限流 → 认证 → 安全响应头 → 业务
	wrap := func(h http.Handler) http.Handler {
		h = auth.Authenticate(h, token, jwtSecret)
		h = ratelimit.IPLimit(h, limitRPS, limitBurst)
		h = auditor.Audit(h)              // 捕获 401/403/429 与正常请求，落 security_events
		h = security.CORS(corsAllowed, h) // 预检在认证前；非白名单来源拒绝
		h = security.Headers(h)           // 所有响应（含 401/429）均带安全头
		h = security.MaxBody(2<<20, h)    // 统一 2MiB 请求体上限，缓解内存放大 DoS
		return h
	}

	// v1.1.3：通用 WS 端点 /v1/ws?topic=xxx[&access_token=...]（v1.1 审查：纳入认证+限流，修复匿名越权订阅）
	mux.Handle("/v1/ws", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topic := r.URL.Query().Get("topic")
		if topic == "" {
			writeJSON(w, 400, map[string]any{"error": "missing topic"})
			return
		}
		deps.Stream.HandleWS(topic, w, r)
	})))
	// v1.1.4：WS 连接统计（需认证）
	mux.Handle("/v1/ws/stats", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, deps.Stream.Stats())
	})))
	// 事件流查询（经过认证）— 用 deps.Bus（与 memory/distill/model 同源）
	mux.Handle("/v1/events", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events := deps.Bus.List()
		json.NewEncoder(w).Encode(map[string]any{"events": events, "count": len(events)})
	})))

	// v1.9.0：JWT 登录/me 路由（jwtSecret 已在 wrap 前定义）
	const accessTTL = 72 * time.Hour
	// login 公开（IP 限流防爆破），按 identifier 落库并签发 JWT
	mux.Handle("POST /v1/auth/login",
		security.MaxBody(2<<20,
			security.Headers(
				security.CORS(corsAllowed,
					ratelimit.IPLimit(http.HandlerFunc(deps.AuthLogin(jwtSecret, accessTTL)), 20, 100)))))
	// me 只认 JWT（RequireIdentity 把 sub 注入 user_id）
	mux.Handle("GET /v1/auth/me",
		security.Headers(
			security.CORS(corsAllowed,
				ratelimit.IPLimit(
					auth.RequireIdentity(http.HandlerFunc(deps.AuthMe), jwtSecret), 60, 200))))

	// 各服务路由（按前缀分发，经过中间件）
	mux.Handle("/v1/trust/", wrap(deps.RelationMux("relation-svc")))
	mux.Handle("/v1/agents/", wrap(deps.RelationMux("relation-svc")))
	mux.Handle("POST /v1/memory/encrypted", wrap(http.HandlerFunc(deps.PutEncryptedContext)))
	mux.Handle("GET /v1/memory/encrypted", wrap(http.HandlerFunc(deps.GetEncryptedContext)))
	mux.Handle("GET /v1/memory/encrypted/export", wrap(http.HandlerFunc(deps.ExportEncryptedContext)))
	mux.Handle("POST /v1/memory/encrypted/import", wrap(http.HandlerFunc(deps.ImportEncryptedContext)))

	mux.Handle("/v1/approvals", wrap(deps.ApprovalMux("approval-engine")))
	mux.Handle("/v1/approvals/", wrap(deps.ApprovalMux("approval-engine")))
	mux.Handle("/v1/negotiations", wrap(deps.NegotiationMux("negotiation-svc")))
	mux.Handle("/v1/negotiations/", wrap(deps.NegotiationMux("negotiation-svc")))
	mux.Handle("/v1/a2a/", wrap(deps.A2AMux()))
	mux.Handle("/v1/wechatbot/", wrap(deps.WechatV2Mux())) // v2.0.5 微信 Bot 协议入口
	mux.Handle("/v1/wechat/", wrap(deps.WechatV2Mux()))    // v2.0.0/v2.0.1 Bot组/识别
	mux.Handle("/v1/quota/", wrap(deps.WechatV2Mux()))     // v2.0.1 Agent 额度
	mux.Handle("/v1/tempgroup/", wrap(deps.WechatV2Mux())) // v2.0.8 临时协商群
	mux.Handle("/v1/botroute/", wrap(deps.WechatV2Mux()))  // v2.0.9 Bot 间路由
	mux.Handle("/v1/report/", wrap(deps.WechatV2Mux()))    // v2.1.0 工作汇报
	adminAuth := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, _ := r.Cookie("admin_token")
			tok := ""
			if c != nil {
				tok = c.Value
			}
			if tok == "" {
				if q := r.URL.Query().Get("token"); q != "" {
					tok = q
					http.SetCookie(w, &http.Cookie{Name: "admin_token", Value: tok, Path: "/", MaxAge: 86400})
				}
			}
			if tok == "" || tok != adminProbeToken {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(401)
				w.Write([]byte(adminLoginHTML))
				return
			}
			h.ServeHTTP(w, r)
		})
	}
	mux.Handle("/admin/api-playground", adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(playgroundHTML))
	})))
	mux.Handle("/admin/broadcast", adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(broadcastHTML))
	})))
	mux.HandleFunc("/admin/wechat-bind", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(wechatBindHTML))
	})

	// v2.1.2 OpenClaw 账号目录 watcher：新微信扫码后自动落库
	ocWatcher := openclaw.NewWatcher("/root/.openclaw/openclaw-weixin/accounts", "http://127.0.0.1:8080/v1/openclaw/auto-bind")
	ocWatcher.Start()

	// A9 掉线/失效自动探测：账号文件消失 = 被踢/登录态失效，通知管理员并发"需重扫"给用户。
	ocHealth := openclaw.NewHealthChecker("/root/.openclaw/openclaw-weixin/accounts", 30*time.Second, openclaw.AccountNotifier(healthNotifier{db: deps.DB}))
	ocHealth.Start()
	mux.Handle("GET /v1/openclaw/health", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, ocHealth.Status())
	})))

	// v2.2.0 微信 → openclaw → IM2A relay：MCP streamable-http 端点。
	// 仅本机 openclaw 调用（127.0.0.1），不经过公网/认证包装，也不暴露到 nginx。
	if deps.DB != nil {
		accountsDir := os.Getenv("OPENCLAW_ACCOUNTS_DIR")
		if accountsDir == "" {
			accountsDir = "/root/.openclaw/openclaw-weixin/accounts"
		}
		ocResolver := relay.NewAccountResolver(accountsDir)
		relaySvc := relay.New(deps.DB, ocResolver, deps.Model, botroute.New(deps.DB)).
			WithPeerSender(openclaw.NewSender()).
			WithNegotiation(relay.NewWechatBridge(deps.Store.Negotiation).
				WithTempGroup(tempgroup.New(deps.DB)))
		// v2.2.2 全量广播：复用同一 openclaw 账号解析器与出站发送器。
		deps.Broadcast = broadcast.New(deps.DB, openclaw.NewSender(), ocResolver)
		// B2：注入流式模型，微信自聊首块即时回发
		relaySvc.SetStreamer(model.NewOpenAICompatible(os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL")))
		mux.Handle("/v2/wechat/mcp", api.NewMCPHandler(relaySvc))
		// v2.2.1 标准 REST webhook（openclaw / 渠道确定性 POST），与 MCP 共用 relay 中枢。
		mux.Handle("/v1/wechat/webhook", api.NewWechatWebhookHandler(relaySvc, deps.DB))
		mux.HandleFunc("/v1/chat/stream", api.ChatStreamHandler)
		// v2.0.8 双 Agent 自动协商：双方 Agent 经 A2A 状态机多轮对谈，每轮真实推送给对方微信。
		mux.Handle("/v1/wechat/auto-nego", api.NewWechatAutoNegoHandler(relaySvc))
	}
	mux.Handle("/v1/openclaw/", wrap(deps.WechatV2Mux())) // v2.1.2 扫码绑定
	mux.Handle("/v1/directory/", wrap(deps.DirectoryMux()))
	mux.Handle("/v1/exp/", wrap(deps.ExperienceMux()))
	mux.Handle("/v1/paid/", wrap(deps.PaymentMux("payment-svc")))
	mux.Handle("/v1/paidflow/", wrap(deps.PaidFlowMux("paid-flow-svc")))
	mux.Handle("/v1/compliance/", wrap(deps.ComplianceMux("compliance-gate")))
	mux.Handle("/v1/msg/", wrap(api.NewMsgMuxWithStore("msg-svc", deps.Store.Message)))

	// Context 记忆（v0.6）：三层记忆 + 子 Agent 隔离
	mux.Handle("/v1/memory/", wrap(deps.MemoryMux("memory-svc")))

	// 蒸馏闭环（v0.7）：审批反馈→阈值自适应
	mux.Handle("/v1/distill/", wrap(deps.DistillMux("distill-svc")))

	// L5 模型层（v0.8）：端侧桩 + 云端兼容
	// v1.2.2：腾讯云服务器到服务器回调不携带用户 Bearer；webhook 走独立 HMAC 验签（?key 或签名头）。
	// 其余 /v1/im/ 接口仍强制用户 Token；整体保留限流。
	imH := deps.IMMux()
	mux.Handle("/v1/im/", ratelimit.IPLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/im/webhook" {
			imH.ServeHTTP(w, r)
			return
		}
		auth.RequireToken(imH, token).ServeHTTP(w, r)
	}), 100, 200))
	// v1.7.4：离线推送管理 API
	pushBase := os.Getenv("TIM_BASE_URL")
	if pushBase == "" {
		pushBase = "https://console.tim.qq.com" // 与 internal/im 默认端点一致，避免未配置时静默空 URL
	}
	deps.PushClient = &push.Client{BaseURL: pushBase, SDKAppID: "1600168829", HTTP: http.DefaultClient}
	mux.Handle("/v1/push/", wrap(deps.PushMux()))
	mux.Handle("/v1/support/", wrap(deps.SupportMux()))
	mux.Handle("/v1/moderation/", wrap(deps.ModerationMux()))
	mux.Handle("/v1/intent/", wrap(deps.LLMIntentMux()))
	mux.Handle("/v1/guard/", wrap(deps.LLMGuardMux()))
	mux.Handle("/v1/distill/feedback", wrap(deps.LLMDistillMux()))
	mux.Handle("/v1/route/", wrap(deps.LLMRouterMux()))
	mux.Handle("/v1/capability/", wrap(deps.CapabilityMux()))
	mux.Handle("/v1/ledger/", wrap(deps.PaidMux()))
	mux.Handle("GET /v1/sse/", wrap(http.HandlerFunc(deps.SSEStream)))
	mux.Handle("POST /v1/sse/publish", wrap(http.HandlerFunc(deps.SSEPublish)))
	mux.Handle("GET /v1/agent/logs", wrap(http.HandlerFunc(deps.AgentLog)))
	mux.Handle("POST /v1/watch/approve", wrap(http.HandlerFunc(deps.WatchApprove)))
	mux.Handle("POST /v1/watch/reject", wrap(http.HandlerFunc(deps.WatchReject)))
	mux.Handle("POST /v1/watch/escalate", wrap(http.HandlerFunc(deps.WatchEscalate)))
	mux.Handle("POST /v1/device/register", wrap(http.HandlerFunc(deps.RegisterDevice)))
	mux.Handle("POST /v1/sync/state", wrap(http.HandlerFunc(deps.SyncState)))
	mux.Handle("GET /v1/sync/state/", wrap(http.HandlerFunc(deps.GetState)))
	mux.Handle("POST /v1/offline/submit", wrap(http.HandlerFunc(deps.OfflineSubmit)))
	mux.Handle("GET /v1/offline/pending", wrap(http.HandlerFunc(deps.OfflineList)))
	mux.Handle("POST /v1/offline/sync", wrap(http.HandlerFunc(deps.OfflineSync)))
	// v1.7 运维
	mux.Handle("GET /v1/admin/metrics", wrap(http.HandlerFunc(deps.AdminMetrics)))
	mux.Handle("GET /v1/admin/audit", wrap(http.HandlerFunc(deps.AdminAudit)))
	mux.Handle("GET /v1/admin/health", wrap(http.HandlerFunc(deps.AdminHealth)))
	// v1.9.8 灰度门控评估：GET /v1/gray/eval?feature=f&percent=p&entity=e
	mux.Handle("GET /v1/gray/eval", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		feature := q.Get("feature")
		entity := q.Get("entity")
		pct, _ := strconv.Atoi(q.Get("percent"))
		if feature == "" || entity == "" {
			http.Error(w, "feature/entity required", http.StatusBadRequest)
			return
		}
		hit := gray.Gate{Feature: feature, Percent: pct}.Enabled(entity)
		writeJSON(w, http.StatusOK, map[string]any{"feature": feature, "entity": entity, "percent": pct, "enabled": hit})
	})))
	// v1.7.6：运营后台总入口——仪表盘真实聚合（独立管理员 Token，缺省回退网关 Token）+ 页面
	adminToken := os.Getenv("IM2A_ADMIN_TOKEN")
	if adminToken == "" {
		adminToken = token
	}
	adminH := auth.RequireToken(http.HandlerFunc(deps.AdminMux().ServeHTTP), adminToken)
	mux.Handle("/v1/admin/", adminH)
	mux.Handle("GET /v1/admin/security", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.DB == nil {
			http.Error(w, "db not configured", http.StatusServiceUnavailable)
			return
		}
		list, err := auditor.List(r.Context(), r.URL.Query().Get("type"), 200)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"events": list})
	}), adminToken))
	mux.Handle("GET /v1/egress/tickets", auth.RequireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.DB == nil {
			http.Error(w, "db not configured", http.StatusServiceUnavailable)
			return
		}
		role := r.URL.Query().Get("role")
		if role == "" {
			role = masking.RoleOther // 缺省最小暴露
		}
		mk := masking.New(role)
		out, n, err := mk.EgressTickets(r.Context(), deps.DB, "gateway:"+mk.Role, "egress_tickets")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"role": mk.Role, "fields_masked": n, "tickets": out})
	}), adminToken))
	// v1.8.6 数据主体权利（个保法 §44-47）：导出 / 删除 / 查询，管理员 token 保护。
	privacyH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.DB == nil {
			http.Error(w, "db not configured", http.StatusServiceUnavailable)
			return
		}
		uid, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
		if err != nil || uid <= 0 {
			http.Error(w, "user_id required", http.StatusBadRequest)
			return
		}
		pv := privacy.New(deps.DB)
		reason := r.URL.Query().Get("reason")
		switch {
		case r.URL.Path == "/v1/privacy/export" && r.Method == http.MethodPost:
			data, id, err := pv.Export(r.Context(), uid, reason)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"request_id": id, "data": data})
		case r.URL.Path == "/v1/privacy/delete" && r.Method == http.MethodPost:
			report, id, err := pv.Erase(r.Context(), uid, reason)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"request_id": id, "report": report})
		case r.URL.Path == "/v1/privacy/requests" && r.Method == http.MethodGet:
			reqs, err := pv.List(r.Context(), uid, 100)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"requests": reqs, "count": len(reqs)})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
	mux.Handle("POST /v1/privacy/export", auth.RequireToken(privacyH, adminToken))
	mux.Handle("POST /v1/privacy/delete", auth.RequireToken(privacyH, adminToken))
	mux.Handle("GET /v1/privacy/requests", auth.RequireToken(privacyH, adminToken))
	mux.Handle("/admin/", adminAuth(deps.AdminPage()))
	mux.Handle("POST /v1/edge/pipeline", wrap(http.HandlerFunc(deps.EdgePipeline)))
	mux.Handle("GET /v1/edge/egress/report", wrap(http.HandlerFunc(deps.EgressReport)))
	mux.Handle("/v1/model/", wrap(api.ModelMux("model-svc", *deps)))

	// 编排器（特殊路由）
	orch := api.NewOrchestrator(*deps)
	mux.Handle("/v1/agents/register", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/gate/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/stranger/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/orchestrator/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/v1/audit", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/mcp", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/mcp/", wrap(orch.Mux("agent-orchestrator")))
	mux.Handle("/mcp/skills", wrap(orch.Mux("agent-orchestrator")))

	// v1.2.3：启动审批超时升级
	deps.StartApprovalEscalator()

	addr := ":" + itoa(*port)
	log.Printf("gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, metrics.Middleware(mux))) // v1.9.9 全请求计数
}

// healthNotifier 实现 openclaw.AccountNotifier：掉线时落日志 + 给该账号绑定用户发"需重扫"。
type healthNotifier struct{ db interface{} }

func (n healthNotifier) OnOffline(accountID, wxid string) {
	log.Printf("[A9-health] ALERT account=%s 已掉线（疑似被踢/登录态失效）", accountID)
	// 读账号文件拿到 wxid（账号文件消失前已知；若已删则跳过发送）
	accFile := "/root/.openclaw/openclaw-weixin/accounts/" + accountID + ".json"
	if b, err := os.ReadFile(accFile); err == nil {
		var af struct {
			UserID string `json:"userId"`
		}
		if json.Unmarshal(b, &af) == nil && af.UserID != "" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if _, err := openclaw.NewSender().Send(ctx, accountID, af.UserID, "你的登录好像掉线了，麻烦重新扫一下二维码绑回来哈。"); err != nil {
					log.Printf("[A9-health] 重扫提示发送失败 account=%s err=%v", accountID, err)
				}
			}()
		}
	}
}
func (n healthNotifier) OnOnline(accountID string) {}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 6)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func runSelfTest() {
	log.Println("[gateway] SELF-TEST: 统一 API 网关路由分发（healthz + msg + relation）")
	deps, err := api.NewDepsFromEnv()
	if err != nil {
		log.Fatalf("deps: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", api.HealthHandler("gateway"))
	// v1.9.9 Prometheus 指标端点（供抓取，不鉴权）
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/v1/trust/", deps.RelationMux("relation-svc"))
	mux.Handle("/v1/msg/", api.NewMsgMux("msg-svc"))

	// 模拟请求
	ts := httptest.NewServer(mux)
	defer ts.Close()

	check := func(name, path, method string, body []byte) {
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
		resp, err := ts.Client().Do(req)
		if err != nil {
			log.Fatalf("[FAIL] %s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			log.Fatalf("[FAIL] %s: status=%d", name, resp.StatusCode)
		}
		log.Printf("  [PASS] %s → %d", name, resp.StatusCode)
	}

	check("healthz", "/healthz", "GET", nil)
	check("msg seq", "/v1/msg/seq", "POST", []byte(`{"userId":1}`))
	check("trust grant", "/v1/trust/grant", "POST", []byte(`{"userId":1,"targetId":2,"level":3}`))

	log.Println("[gateway] SELF-TEST PASS: 网关路由分发正常")
}
