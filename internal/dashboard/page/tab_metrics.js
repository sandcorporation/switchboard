// ---- 지표 ----
// One GET /api/admin/metrics?days=N fills the four cards. metricsSeq drops a
// slow answer for a period the user has already switched away from.
let metricsDays=7,metricsSeq=0;
function dur(s){
  if(s===null||s===undefined)return '-';
  if(s<60)return s+'초';
  if(s<3600)return Math.round(s/60)+'분';
  if(s<86400){const h=s/3600;return (h<10?h.toFixed(1).replace(/\.0$/,''):Math.round(h))+'시간'}
  const d=s/86400;return (d<10?d.toFixed(1).replace(/\.0$/,''):Math.round(d))+'일';
}
const bucketLabel={'<10m':'<10분','<1h':'<1시간','<4h':'<4시간','<1d':'<1일','>=1d':'≥1일'};
function stateText(st){return sessionStatus(st)[0]}
// metricsBreaches counts what is over an alert threshold now: pending mail the
// server flagged, plus current error/blocked sessions past their limit (0 = off).
function metricsBreaches(m){
  const t=m.thresholds||{};
  let n=(m.pending||[]).filter(p=>p.over_threshold).length;
  for(const c of (m.sessions&&m.sessions.current)||[])if(sessionOver(c,t))n++;
  return n;
}
function sessionOver(c,t){
  const lim=/^error:/.test(c.state)?t.error_s:/^blocked:/.test(c.state)?t.blocked_s:0;
  return lim>0&&c.seconds>=lim;
}
function cells(tr,labels,values,classes){
  values.forEach((v,i)=>{const td=node('td',v,(classes&&classes[i])||'');if(i>0)td.dataset.label=labels[i];tr.append(td)});
  return tr;
}
function emptyRow(body,cols,text){const tr=node('tr','');const td=node('td',text,'muted');td.colSpan=cols;tr.append(td);body.append(tr)}
const SVG='http://www.w3.org/2000/svg';
function svgEl(tag,attrs,text){const e=document.createElementNS(SVG,tag);for(const k in attrs)e.setAttribute(k,attrs[k]);if(text!==undefined)e.textContent=text;return e}
// bucketChart draws buckets as bars, each with its count above and label below.
function bucketChart(buckets){
  const W=320,H=140,top=18,bottom=24,gap=10,n=Math.max(buckets.length,1);
  const bw=(W-gap*(n+1))/n,max=Math.max(1,...buckets.map(b=>b.count)),plot=H-top-bottom;
  const svg=svgEl('svg',{viewBox:'0 0 '+W+' '+H,role:'img','aria-label':'첫 답장 분포: '+buckets.map(b=>(bucketLabel[b.label]||b.label)+' '+b.count+'통').join(', ')});
  svg.append(svgEl('title',{},'첫 답장까지 걸린 시간 분포'));
  buckets.forEach((b,i)=>{
    const x=gap+i*(bw+gap),h=Math.round(plot*b.count/max),y=top+plot-h;
    const g=svgEl('g',{});
    g.append(svgEl('rect',{class:'bar',x:x,y:y,width:bw,height:Math.max(h,b.count?1:0)}));
    g.append(svgEl('text',{x:x+bw/2,y:y-4,'text-anchor':'middle'},String(b.count)));
    g.append(svgEl('text',{x:x+bw/2,y:H-8,'text-anchor':'middle'},bucketLabel[b.label]||b.label));
    svg.append(g);
  });
  svg.append(svgEl('line',{class:'axis',x1:0,x2:W,y1:top+plot+.5,y2:top+plot+.5}));
  return svg;
}
function statTile(label,value){const d=node('div','','stat');d.append(node('b',value),node('span',label));return d}
function renderFirstReply(f){
  $('mFirstStats').replaceChildren(statTile('스레드 시작',String(f.count)),statTile('답 받음',String(f.answered)),statTile('답 없음',String(f.unanswered)),statTile('p50',dur(f.p50_s)),statTile('p90',dur(f.p90_s)),statTile('최장',dur(f.max_s)));
  $('mFirstChart').replaceChildren(f.count?bucketChart(f.buckets||[]):node('p','이 기간 스레드 없음','empty'));
  const body=$('mFirstByRecipient');body.replaceChildren();
  const labels=['받는 쪽','메일','답 받음','답 없음','p50','p90','최장'],num=['','num','num','num','num','num','num'];
  for(const r of f.by_recipient||[])body.append(cells(node('tr',''),labels,[r.base,String(r.count),String(r.answered),String(r.unanswered),dur(r.p50_s),dur(r.p90_s),dur(r.max_s)],num));
  if(!body.children.length)emptyRow(body,7,'받는 쪽 없음');
}
function renderPending(list,t){
  $('mPendingNote').textContent=t.pending_s>0?'문턱 '+dur(t.pending_s):'알림 꺼짐';
  const body=$('mPending');body.replaceChildren();
  for(const p of list){
    const tr=cells(node('tr','',p.over_threshold?'over':''),['역할','안 읽음','가장 오래된 것'],[p.role,p.pending+'통',dur(p.oldest_s)+(p.over_threshold?' · 문턱 넘음':'')],['','num','num']);
    body.append(tr);
  }
  if(!list.length)emptyRow(body,3,'안 읽힌 메일 없음');
}
function renderThreads(th){
  $('mThreadsNote').textContent=th.multi_round?'스레드 '+th.multi_round+'개':'';
  const ol=$('mThreads');ol.replaceChildren();
  for(const x of th.top||[]){
    const li=node('li',''),b=node('button','');
    b.type='button';
    b.append(document.createTextNode('#'+x.thread_id+' '+(x.subject||'(제목 없음)')),node('small',(x.parties||[]).join(' ↔ ')+' · 왕복 '+x.round_trips+'번'));
    b.onclick=()=>openThread(x.thread_id);
    li.append(b);ol.append(li);
  }
  if(!ol.children.length)ol.append(node('li','되물은 스레드 없음','empty'));
}
function renderSessionStates(ss,t){
  const lim=[t.error_s>0?'오류 '+dur(t.error_s):'',t.blocked_s>0?'막힘 '+dur(t.blocked_s):''].filter(Boolean);
  $('mSessionsNote').textContent=lim.length?'문턱 '+lim.join(' · '):'알림 꺼짐';
  const body=$('mStates');body.replaceChildren();
  for(const r of ss.by_role||[]){
    const kinds=(r.top_kinds||[]).map(k=>stateText(k.state)+' '+dur(k.seconds)).join(', ')||'-';
    body.append(cells(node('tr',''),['역할','오류','막힘','많았던 상태'],[r.role,dur(r.error_s),dur(r.blocked_s),kinds],['','num','num','wrap']));
  }
  if(!body.children.length)emptyRow(body,4,'오류·막힘 없음');
  const cur=$('mCurrent');cur.replaceChildren();
  for(const c of ss.current||[]){
    const over=sessionOver(c,t),[text,cls]=sessionStatus(c.state);
    cur.append(cells(node('tr','',over?'over':''),['역할','상태','시작','지난 시간'],[c.role,text,when(c.since),dur(c.seconds)+(over?' · 문턱 넘음':'')],['',cls,'','num']));
  }
  if(!cur.children.length)emptyRow(cur,4,'지금 오류·막힘 없음');
}
// renderAliases shows old-name use since the server started (#72).
function renderAliases(a){
  a=a||{};const e=a.old_env||[];
  $('mAliases').textContent='옛 이름 환경 변수: '+(e.join(', ')||'없음');
}
async function loadMetrics(){
  const seq=++metricsSeq;
  $('metricsNote').textContent='불러오는 중…';
  try{
    const m=await api('metrics?days='+metricsDays);
    if(seq!==metricsSeq)return;
    const t=m.thresholds||{};
    renderFirstReply(m.first_reply);renderPending(m.pending||[],t);renderThreads(m.threads||{multi_round:0,top:[]});renderSessionStates(m.sessions||{},t);renderAliases(m.aliases);
    const n=metricsBreaches(m);setTabCount('metrics',n||null);
    $('metricsNote').textContent='최근 '+m.window.days+'일 · '+when(m.window.from)+' ~ '+when(m.window.to)+(n?' · 문턱 넘은 것 '+n+'개':'');
  }catch(e){if(seq===metricsSeq)$('metricsNote').textContent=e.message}
}
// refreshMetricsCount fills the tab number before the tab is first opened.
async function refreshMetricsCount(){
  try{const m=await api('metrics?days=1');if(!tabLoaded.has('metrics')){const n=metricsBreaches(m);setTabCount('metrics',n||null)}}catch(e){}
}
function setMetricsDays(d){
  metricsDays=d;
  for(const b of [$('metDays7'),$('metDays30')])b.setAttribute('aria-pressed',Number(b.dataset.days)===d?'true':'false');
  loadMetrics();
}
// openThread shows the 메일 tab filtered to one thread (the 「스레드 보기」 filter),
// clearing the other filters so the whole thread shows.
function openThread(id){openMail({thread:id})}
// openMail shows the 메일 tab with only the given filter (thread, task or id)
// set and every other filter cleared.
function openMail(f){
  $('thread').value=f.thread||'';$('task').value=f.task||'';mailID=f.id?String(f.id):'';
  $('search').value='';$('status').value='';$('role').value='';$('box').value='all';
  const fresh=!tabLoaded.has('mail');
  if(f.id)openDetail('mail',String(f.id),'body');else selectTab('mail');
  if(!fresh)load();
  window.scrollTo({top:0});
}
function openMessage(id){openMail({id:id})}
