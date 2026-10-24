package main

// broadcastHTML 运营后台 · 微信全量广播页（v2.2.2）。页面由 admin token cookie 门保护，
// 接口调用再带 Authorization: Bearer。
const broadcastHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>IM2A · 全量广播</title>
<style>
:root{--bg:#0d1117;--card:#161b22;--bd:#30363d;--tx:#e6edf3;--mut:#8b949e;--ok:#3fb950;--warn:#d29922;--err:#f85149;--pri:#2f81f7;}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--tx);font:14px/1.6 -apple-system,"PingFang SC","Microsoft YaHei",sans-serif}
.wrap{max-width:860px;margin:0 auto;padding:24px 18px 60px}
h1{font-size:20px;margin:0 0 4px}
.sub{color:var(--mut);margin:0 0 20px}
.card{background:var(--card);border:1px solid var(--bd);border-radius:10px;padding:16px 18px;margin-bottom:16px}
.card h2{font-size:14px;margin:0 0 12px;color:var(--mut);font-weight:600;letter-spacing:.5px}
.row{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
.metric{background:#0d1117;border:1px solid var(--bd);border-radius:8px;padding:8px 12px;min-width:96px}
.metric b{display:block;font-size:20px}
.metric span{color:var(--mut);font-size:12px}
textarea{width:100%;min-height:120px;background:#0d1117;border:1px solid var(--bd);border-radius:8px;color:var(--tx);padding:12px;font:inherit;resize:vertical}
button{border:0;border-radius:8px;padding:10px 18px;font-size:14px;font-weight:600;cursor:pointer}
.btn-pri{background:var(--pri);color:#fff}
.btn-pri:disabled{opacity:.5;cursor:not-allowed}
.btn-ghost{background:transparent;color:var(--mut);border:1px solid var(--bd)}
.pill{display:inline-block;padding:2px 10px;border-radius:999px;font-size:12px;font-weight:600}
.pill.running{background:#1f2d4a;color:#79b8ff}
.pill.completed{background:#11331c;color:var(--ok)}
.pill.partial{background:#3a2f08;color:var(--warn)}
.pill.failed{background:#3d1418;color:var(--err)}
.pill.pending{background:#22262d;color:var(--mut)}
.bar{height:8px;background:#0d1117;border-radius:99px;overflow:hidden;margin:10px 0}
.bar i{display:block;height:100%;background:var(--ok);width:0;transition:width .4s}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{text-align:left;padding:7px 8px;border-bottom:1px solid var(--bd)}
th{color:var(--mut);font-weight:600}
.mono{font-family:ui-monospace,Menlo,monospace;font-size:12px;color:var(--mut)}
.st-ok{color:var(--ok)} .st-fail{color:var(--err)} .st-pend{color:var(--mut)}
.muted{color:var(--mut)}
.tag{font-size:12px;color:var(--mut)}
.hist{cursor:pointer}
.hist:hover{background:#0d1117}
</style>
</head>
<body>
<div class="wrap">
  <h1>IM2A · 微信全量广播</h1>
  <p class="sub">运营后台一键群发：经官方 Bot 账号向所有绑定用户的微信逐条投递（自动限流，排除官方号自身）。</p>

  <div class="card">
    <h2>① 投放目标（实时）</h2>
    <div id="targets" class="muted">加载中…</div>
  </div>

  <div class="card">
    <h2>② 广播文案</h2>
    <textarea id="msg">【通知】2026年10月23号，IM2A 智能服务已上线。</textarea>
    <div class="row" style="margin-top:12px;justify-content:flex-end">
      <button class="btn-ghost" onclick="loadPreview()">刷新目标</button>
      <button id="sendBtn" class="btn-pri" onclick="send()">一键群发</button>
    </div>
  </div>

  <div class="card" id="progCard" style="display:none">
    <h2>③ 投递进度 <span id="progPill" class="pill running">running</span></h2>
    <div class="row" style="margin-bottom:8px">
      <div class="metric"><b id="mTotal">-</b><span>目标</span></div>
      <div class="metric"><b id="mSent" style="color:var(--ok)">-</b><span>已送达</span></div>
      <div class="metric"><b id="mFail" style="color:var(--err)">-</b><span>失败</span></div>
    </div>
    <div class="bar"><i id="bar"></i></div>
    <table>
      <thead><tr><th>目标 wxid</th><th>状态</th><th>messageId / 错误</th></tr></thead>
      <tbody id="dels"></tbody>
    </table>
  </div>

  <div class="card">
    <h2>④ 历史广播</h2>
    <table>
      <thead><tr><th>任务 ID</th><th>状态</th><th>送达/总数</th><th>时间</th></tr></thead>
      <tbody id="hist"><tr><td colspan="4" class="muted">加载中…</td></tr></tbody>
    </table>
  </div>
</div>
<script>
function tok(){var m=document.cookie.match(/admin_token=([^;]+)/);return m?decodeURIComponent(m[1]):"";}
function api(path,opt){opt=opt||{};opt.headers=Object.assign({"Content-Type":"application/json","Authorization":"Bearer "+tok()},opt.headers||{});return fetch(path,opt).then(function(r){return r.json().then(function(j){if(!r.ok)throw new Error(j.error||r.status);return j;});});}
function esc(s){return String(s==null?"":s).replace(/[&<>]/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;"}[c];});}
function shortWxid(w){if(!w)return "";return w.length>28?w.slice(0,14)+"…"+w.slice(-10):w;}
function pillClass(st){return ["running","completed","partial","failed","pending"].indexOf(st)>=0?st:"pending";}

function loadPreview(){
  api("/v1/admin/broadcast?preview=1").then(function(j){
    var html='<div class="row" style="margin-bottom:10px">'+
      '<div class="metric"><b>'+j.count+'</b><span>接收用户</span></div>'+
      '<div class="metric"><b style="font-size:13px">'+esc(shortWxid(j.sender_wxid))+'</b><span>官方发送号</span></div>'+
      '</div>';
    if(j.targets&&j.targets.length){
      html+='<div class="tag">将投递到：</div><div class="mono" style="margin-top:6px">'+j.targets.map(esc).join("<br>")+'</div>';
    }else{
      html+='<div class="st-fail">当前没有可投递的其他用户（仅有官方发送号在线）。</div>';
    }
    document.getElementById("targets").innerHTML=html;
  }).catch(function(e){document.getElementById("targets").innerHTML='<span class="st-fail">加载失败：'+esc(e.message)+'</span>';});
}
function send(){
  var text=document.getElementById("msg").value.trim();
  if(!text){alert("请输入广播文案");return;}
  if(!confirm("确认向全部在线用户群发该通知？"))return;
  var btn=document.getElementById("sendBtn");btn.disabled=true;btn.textContent="群发中…";
  api("/v1/admin/broadcast",{method:"POST",body:JSON.stringify({text:text})}).then(function(j){
    document.getElementById("progCard").style.display="block";
    poll(j.id);
  }).catch(function(e){alert("创建失败："+e.message);btn.disabled=false;btn.textContent="一键群发";});
}
function poll(id){
  api("/v1/admin/broadcast?id="+encodeURIComponent(id)).then(function(j){
    var b=j.broadcast;
    document.getElementById("mTotal").textContent=b.total;
    document.getElementById("mSent").textContent=b.sent;
    document.getElementById("mFail").textContent=b.failed;
    var p=document.getElementById("progPill");p.textContent=b.status;p.className="pill "+pillClass(b.status);
    var pct=b.total?Math.round((b.sent+b.failed)/b.total*100):0;
    document.getElementById("bar").style.width=pct+"%";
    document.getElementById("bar").style.background=b.failed>0?"var(--warn)":"var(--ok)";
    var rows=(b.deliveries||[]).map(function(d){
      var cls=d.status==="sent"?"st-ok":(d.status==="failed"?"st-fail":"st-pend");
      var info=d.status==="sent"?d.message_id:(d.error||"");
      return '<tr><td class="mono">'+esc(shortWxid(d.to_wxid))+'</td><td class="'+cls+'">'+esc(d.status)+'</td><td class="mono">'+esc(info)+'</td></tr>';
    }).join("");
    document.getElementById("dels").innerHTML=rows;
    if(b.status==="running"||b.status==="pending"){setTimeout(function(){poll(id);},1500);}
    else{var btn=document.getElementById("sendBtn");btn.disabled=false;btn.textContent="一键群发";loadHistory();}
  }).catch(function(e){setTimeout(function(){poll(id);},2000);});
}
function loadHistory(){
  api("/v1/admin/broadcast?limit=20").then(function(j){
    var rows=(j.broadcasts||[]).map(function(b){
      return '<tr class="hist" onclick="openHist(\''+b.id+'\')"><td class="mono">'+esc(b.id)+'</td>'+
        '<td><span class="pill '+pillClass(b.status)+'">'+esc(b.status)+'</span></td>'+
        '<td>'+b.sent+"/"+b.total+(b.failed?" ("+b.failed+" 失败)":"")+'</td>'+
        '<td class="tag">'+esc(new Date(b.created_at).toLocaleString())+'</td></tr>';
    }).join("");
    document.getElementById("hist").innerHTML=rows||'<tr><td colspan="4" class="muted">暂无</td></tr>';
  }).catch(function(){document.getElementById("hist").innerHTML='<tr><td colspan="4" class="st-fail">加载失败</td></tr>';});
}
function openHist(id){
  document.getElementById("progCard").style.display="block";poll(id);
}
loadPreview();loadHistory();
</script>
</body>
</html>`
