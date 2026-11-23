// ---- 틀 (#510) ----
// The app frame: the title row, the left pane (search, the menu rows, the
// sessions grouped by service, 설정 and the other tables at the foot), the
// pane borders that drag to resize (kept per browser), and the status row
// (connection, session and machine counts, the agents' usage). Every
// button, field and select without a hover text gets one (fillTitles).
// Runs on the page's globals (node, api, kTokens, sessionStatus, openDetail,
// termRole, termSt, hostingOn, ws, can).
const PANE_KEY='switchboard.panes';
// paneBounds are the widths a pane may take (px): 왼쪽 180–480, 오른쪽 200–520.
const paneBounds={l:[180,480,248],r:[200,520,292]};
let paneW={l:paneBounds.l[2],r:paneBounds.r[2]};
function paneClamp(k,w){const [lo,hi,d]=paneBounds[k];w=Math.round(Number(w));return isFinite(w)?Math.min(hi,Math.max(lo,w)):d}
function paneApply(){
  const s=document.documentElement.style;
  s.setProperty('--lp-w',paneW.l+'px');s.setProperty('--rp-w',paneW.r+'px');
  for(const g of document.querySelectorAll('.fr-grip')){const k=g.dataset.pane||'l';g.setAttribute('aria-valuenow',String(paneW[k]));g.setAttribute('aria-valuemin',String(paneBounds[k][0]));g.setAttribute('aria-valuemax',String(paneBounds[k][1]))}
}
try{const o=JSON.parse(localStorage.getItem(PANE_KEY)||'{}');paneW={l:paneClamp('l',o.l===undefined?paneW.l:o.l),r:paneClamp('r',o.r===undefined?paneW.r:o.r)}}catch(e){}
function paneSave(){try{localStorage.setItem(PANE_KEY,JSON.stringify(paneW))}catch(e){}}
// frameGrip makes el the border of pane k ('l' left, 'r' right): dragging
// it, or ←/→ while it has focus, changes the width; the width is kept.
function frameGrip(el,k){
  el.dataset.pane=k;
  const sign=k==='l'?1:-1;
  el.addEventListener('pointerdown',e=>{
    e.preventDefault();el.setPointerCapture(e.pointerId);
    const x0=e.clientX,w0=paneW[k],html=document.documentElement;
    html.classList.add('fr-resizing');
    const mv=ev=>{paneW[k]=paneClamp(k,w0+sign*(ev.clientX-x0));paneApply()};
    const up=()=>{el.removeEventListener('pointermove',mv);el.removeEventListener('pointerup',up);el.removeEventListener('pointercancel',up);html.classList.remove('fr-resizing');paneSave()};
    el.addEventListener('pointermove',mv);el.addEventListener('pointerup',up);el.addEventListener('pointercancel',up);
  });
  el.addEventListener('keydown',e=>{
    const d={ArrowLeft:-16,ArrowRight:16}[e.key];if(!d)return;
    e.preventDefault();paneW[k]=paneClamp(k,paneW[k]+sign*d);paneApply();paneSave();
  });
  paneApply();
}
frameGrip($('gripL'),'l');

// ---- drawers (600px and below) ----
const frScrim=node('div','','fr-scrim');frScrim.id='frScrim';document.body.append(frScrim);
const frameScrimClose=[];
frScrim.onclick=()=>{frameDrawer(false);for(const f of frameScrimClose)f()};
// frameDrawer opens or closes the left pane as a drawer.
function frameDrawer(on){
  document.documentElement.classList.toggle('lp-open',on);
  $('lpOpen').setAttribute('aria-expanded',String(on));
}
$('lpOpen').onclick=()=>frameDrawer(!document.documentElement.classList.contains('lp-open'));
$('lp').addEventListener('click',e=>{if(e.target.closest('[role=tab],.sess'))frameDrawer(false)});
document.addEventListener('keydown',e=>{
  if(e.key!=='Escape'||e.defaultPrevented||!document.documentElement.classList.contains('lp-open'))return;
  if(e.target.closest&&e.target.closest('dialog'))return;
  e.preventDefault();frameDrawer(false);$('lpOpen').focus();
},true);

// ---- 새 세션 ----
// The add form lives in the 터미널 section; it moves to the body so the
// dialog opens whatever section is on screen.
document.body.append($('termAdd'));
$('newSession').onclick=$('sessNew').onclick=()=>$('termAddOpen').click();

// ---- 세션 목록 ----
let frameData=null,frameSeq=0,frameTimer=0,frameWait=false,frameSel='';
const frameStates={idle:'유휴',busy:'일함',compacting:'compact 중',unknown:'모름',offline:'꺼짐'};
// frameRows merges presence sessions with hosted sessions (by role) and
// counts each one's pending questions and permission requests.
function frameRows(d){
  const hosted=new Map();
  for(const v of d.terms)hosted.set(termRole(v),v);
  const wait=new Map(),add=r=>{if(r)wait.set(r,(wait.get(r)||0)+1)};
  for(const q of d.questions)add(q.asker);
  for(const p of d.perms)add(p.requester);
  const rows=[],seen=new Set();
  for(const s of d.sessions){
    seen.add(s.role);
    rows.push({role:s.role,st:s.online?(s.status||'unknown'):'offline',ctx:s.context_tokens,machine:s.machine||'',cli:s.cli||'',dir:s.directory||'',label:s.label||'',term:hosted.get(s.role)||null,wait:wait.get(s.role)||0});
  }
  for(const [role,v] of hosted)if(!seen.has(role))
    rows.push({role:role,st:termSt(v)==='running'?'unknown':'offline',ctx:null,machine:v.node||'',cli:v.adapter||'',dir:v.directory||'',label:v.label||'',term:v,wait:wait.get(role)||0});
  return rows;
}
function frameService(role){const i=role.indexOf('/');return i>0?role.slice(0,i):role}
async function frameLoad(){
  const seq=++frameSeq;
  try{
    const [p,terms,q,pm,nodes,hr]=await Promise.all([
      api('presence'),hostingOn?api('terminals').catch(()=>[]):[],
      api('questions?status=pending&limit=500').catch(()=>({questions:[]})),api('permissions?status=pending').catch(()=>({permissions:[]})),
      hostingOn?api('nodes').catch(()=>null):null,
      api('metrics/hourly').catch(()=>null)]);
    if(seq!==frameSeq)return;
    frameData={sessions:p.sessions||[],terms:Array.isArray(terms)?terms:[],questions:q.questions||[],perms:pm.permissions||[],nodes:Array.isArray(nodes)?nodes:null,hour:hr&&hr.last_hour?hr.last_hour:null};
    frameDraw();frameStatus();
    if(frameWant)frameSessionTry();else if(frameSessRole)frameSessDraw();
  }catch(e){if(seq===frameSeq){$('sessList').replaceChildren(node('p',e.message,'msg err lp-msg'))}}
}
function frameDraw(){
  if(!frameData)return;
  const list=$('sessList'),q=$('gsQ').value.trim().toLowerCase();
  const rows=frameRows(frameData).filter(r=>(!frameWait||r.wait>0)&&(!q||(r.role+' '+r.label+' '+r.machine).toLowerCase().includes(q)));
  const groups=new Map();
  for(const r of rows.sort((a,b)=>a.role.localeCompare(b.role))){const k=frameService(r.role);if(!groups.has(k))groups.set(k,[]);groups.get(k).push(r)}
  const a=document.activeElement,was=list.contains(a)&&a.dataset?a.dataset.role:'';
  const out=[];
  for(const [svc,rs] of [...groups].sort((x,y)=>x[0].localeCompare(y[0]))){
    const h=node('div','','lp-grp');h.title=svc+' — 세션 '+rs.length;
    h.append(node('span','▾','lp-grp-mark'),node('span',svc));out.push(h);
    for(const r of rs)out.push(frameCard(r));
  }
  if(!out.length)out.push(node('p',frameWait?'기다리는 세션 없음':q?'맞는 세션 없음':'세션 없음','empty lp-msg'));
  list.replaceChildren(...out);
  if(was)for(const b of list.querySelectorAll('.sess'))if(b.dataset.role===was)b.focus({preventScroll:true});
  frameMark();
}
// frameKind is the session's kind on its card (#517): the name part of the
// role address without its number tail (team/worker-1-2 → worker).
function frameKind(role){return role.slice(role.lastIndexOf('/')+1).replace(/(-\d+)+$/,'')}
function frameCard(r){
  const b=node('button','','sess');b.type='button';b.dataset.role=r.role;if(r.term)b.dataset.term=r.term.id;
  const [stText,stCls]=r.st==='offline'?['꺼짐','st-offline']:r.st==='unknown'?['모름','st-unknown']:sessionStatus(r.st);
  const name=frameService(r.role)===r.role?r.role:r.role.slice(frameService(r.role).length+1);
  const l1=node('span','','sess-l1'),l2=node('span','','sess-l2');
  l1.append(node('span','','dot '+stCls),node('b',name+(r.label?' · '+r.label:''),'sess-nm'));
  const kind=node('span',frameKind(r.role),'tag');kind.title=r.role;l1.append(kind);
  if(r.wait)l1.append(node('span',String(r.wait),'sess-w'));
  if(r.machine)l2.append(node('span',r.machine,'tag'));
  if(r.cli&&r.cli!=='unknown'){const c=node('span',r.cli.split(/\s/)[0],'tag');c.title=r.cli;l2.append(c)}
  l2.append(node('span',r.ctx===null||r.ctx===undefined?'-':kTokens(r.ctx),'sess-ctx'));
  if(r.dir)l2.append(node('span','\u200e'+r.dir+'\u200e','sess-dir'));
  l1.firstChild.title=frameStates[r.st]||stText;
  b.append(l1,l2);
  b.title=r.role+' — '+(frameStates[r.st]||stText)+', 컨텍스트 '+(r.ctx===null||r.ctx===undefined?'-':kTokens(r.ctx))+(r.machine?', '+r.machine:'')+(r.wait?', 답 기다림 '+r.wait:'');
  b.onclick=()=>frameOpen(r);
  return b;
}
// frameOpen shows a session's screen (#519): the address points at it.
function frameOpen(r){
  const h='#session='+encodeURIComponent(r.role);
  if(location.hash===h)frameSession(r.role);else location.hash=h.slice(1);
}
// ---- 세션 화면 (#519) ----
// A card opens its session's screen in the centre: a hosted one's terminal
// panel (the workspace), any other one's page (#sessScreen: the head, then
// 상태 · 답을 기다림 · 메일 · 작업 one under another). The right column
// shows on the session screen only, for the session on it; a menu screen
// hides it (html.fr-menu).
let frameWant='',frameSessRole='',frameSessSeq=0;
// frameScreen sets which screen the centre holds: 'menu', 'sess' (a page
// session) or '' (the workspace's session panels).
function frameScreen(kind){
  const html=document.documentElement;
  html.classList.toggle('fr-menu',kind==='menu');
  html.classList.toggle('fr-sess',kind==='sess');
  if(kind!=='sess'){frameSessRole='';$('sessScreen').hidden=true}
  if(ws)document.querySelector('.sc-app-page').hidden=!(html.classList.contains('fr-split')||kind==='sess');
}
// frameSession opens role's screen once the list (and, for a hosted one,
// the workspace) is there; frameLoad and the workspace start call it again.
function frameSession(role){frameWant=role;frameSessionTry()}
function frameSessionTry(){
  const role=frameWant;
  if(!role||!frameData)return;
  const r=frameRows(frameData).find(x=>x.role===role)||null;
  // With hosting on, the workspace (its terminal panels, the right column)
  // comes first.
  if(hostingOn&&!ws&&!wsFailed)return;
  frameWant='';
  frameLeaveMenu();
  frameSel=r&&r.term?r.term.id:role;
  if(r&&r.term&&ws){frameScreen('');ws.followRole('');ws.openTerm(r.term)}
  else{frameScreen('sess');frameSessRole=role;frameSessDraw();if(ws)ws.followRole(role)}
  frameCrumb();frameMark();
}
// frameLeaveMenu steps out of the menu screen on the centre: no menu row
// is marked and the detail closes.
function frameLeaveMenu(){
  hideDetail();
  if(curTab)$('panel-'+curTab).hidden=!(ws&&!listTab(curTab));
  frameSplit(false);
  for(const n of tabNames){const t=$('tab-'+n);t.setAttribute('aria-selected','false');t.tabIndex=n==='requests'?0:-1}
  curTab='';
}
// frameToSession: a session panel of the workspace got the focus (a click
// on it), so the centre is the session screen again.
function frameToSession(){
  const html=document.documentElement;
  if(!curTab&&!html.classList.contains('fr-sess')&&!html.classList.contains('fr-menu'))return;
  frameLeaveMenu();frameScreen('');if(ws)ws.followRole('');frameCrumb();
}
// frameCrumb names the session on screen in the title row and the address.
function frameCrumb(){
  if(curTab)return;
  let role=frameSessRole;
  if(!role&&ws&&ws.session()&&frameData){const r=frameRows(frameData).find(x=>x.term&&x.term.id===ws.session());role=r?r.role:''}
  $('crumbTab').textContent=role||'세션';
  const h=role?'#session='+encodeURIComponent(role):'';
  if(location.hash!==h)history.replaceState(null,'',h||location.pathname+location.search);
}
// frameSessDraw draws the page of a session that is not hosted, with the
// session detail's own drawing (registerDetail('sessions')).
async function frameSessDraw(){
  const role=frameSessRole,seq=++frameSessSeq,box=$('sessScreen');
  if(!role)return;
  box.hidden=false;
  const head=node('div','','ss-head'),out=[head];
  const sec=(title,el)=>{const s=node('section','','ss-sec');s.append(node('h2',title,'ss-h'),el);out.push(s)};
  let it=null;
  try{it=await details.sessions.load(role)}catch(e){if(seq===frameSessSeq)box.replaceChildren(node('p',e.message,'msg err'));return}
  if(seq!==frameSessSeq)return;
  const r=frameData?frameRows(frameData).find(x=>x.role===role):null;
  const [stText,stCls]=!r||r.st==='offline'?['꺼짐','st-offline']:r.st==='unknown'?['모름','st-unknown']:sessionStatus(r.st);
  const nm=node('h1',role+(r&&r.label?' · '+r.label:''),'ss-name');nm.title=role;
  head.append(node('span','','dot '+stCls),nm,node('span',stText,'tag'));
  if(r&&r.machine)head.append(node('span',r.machine,'tag'));
  if(r&&r.cli&&r.cli!=='unknown'){const c=node('span',r.cli.split(/\s/)[0],'tag');c.title=r.cli;head.append(c)}
  if(!it){out.push(node('p','세션 없음','empty'));box.replaceChildren(...out);return}
  const status=document.createElement('div'),mail=document.createElement('div'),jobs=document.createElement('div');
  await Promise.all([details.sessions.draw(it,'status',status),details.sessions.draw(it,'mail',mail).catch(e=>mail.append(node('p',e.message,'msg err'))),frameSessJobs(role,jobs)]);
  if(seq!==frameSessSeq)return;
  sec('상태',status);
  sec('답을 기다림',frameSessNeeds(role));
  sec('메일',mail);
  sec('작업',jobs);
  const top=box.scrollTop;box.replaceChildren(...out);box.scrollTop=top;
}
// frameSessNeeds lists role's waiting questions and permission requests.
function frameSessNeeds(role){
  const ul=node('ul','','dlist'),d=frameData||{questions:[],perms:[]};
  for(const q of d.questions)if(q.asker===role){
    const li=node('li',''),a=node('a',(q.questions||[]).map(x=>x.header).join(' · ')||q.id);a.href=detailHash('questions',q.id,'answer');
    li.append(a,node('small','질문 · '+when(q.created_at)));ul.append(li);
  }
  for(const p of d.perms)if(p.requester===role){
    const li=node('li',''),a=node('a',(p.tool||'')+(p.summary?' · '+p.summary:''));a.href=detailHash('permissions',p.id,'request');
    li.append(a,node('small','권한 요청 · '+when(p.created_at)));ul.append(li);
  }
  return ul.childNodes.length?ul:node('p','대기 없음','empty');
}
// frameSessJobs lists role's jobs and watches.
async function frameSessJobs(role,box){
  try{
    const jobs=await api('jobs?'+new URLSearchParams({role:role}));
    const ul=node('ul','','dlist');
    for(const j of jobs||[]){
      const li=node('li',''),a=node('a',(jobKinds[j.kind]||j.kind)+' · '+(j.name||j.command||j.id));a.href=detailHash('jobs','job:'+j.id,'');
      li.append(a,node('small',(j.state||'')+(j.machine?' · '+j.machine:'')+' · '+when(j.started_at||j.created_at)));ul.append(li);
    }
    box.append(ul.childNodes.length?ul:node('p','작업 없음','empty'));
  }catch(e){box.append(node('p',e.message,'msg err'))}
}
// frameMark marks the card of the session on screen.
function frameMark(){
  const cur=frameSessRole||(ws&&ws.session?ws.session()||frameSel:frameSel);
  for(const b of $('sessList').querySelectorAll('.sess')){const on=!!cur&&(b.dataset.term===cur||b.dataset.role===cur);b.classList.toggle('on',on);if(on)b.setAttribute('aria-current','true');else b.removeAttribute('aria-current')}
}
document.addEventListener('ws-follow',()=>{frameMark();frameCrumb()});
$('gsQ').addEventListener('input',frameDraw);
$('sessWait').onclick=()=>{frameWait=!frameWait;$('sessWait').setAttribute('aria-pressed',String(frameWait));frameDraw()};

// ---- 상태 줄 ----
function frameStatus(){
  const d=frameData;if(!d)return;
  const on=d.sessions.filter(s=>s.online).length;
  // 기계 (#519): the machines the online sessions run on, not the nodes.
  const machines=[...new Set(d.sessions.filter(s=>s.online&&s.machine).map(s=>s.machine))].sort();
  $('stCount').textContent='세션 '+on+' · 기계 '+machines.length;
  $('stCount').title=machines.length?'기계 — '+machines.join(', '):'붙은 기계 없음';
  // 지난 한 시간 (#511 T3): mail and reports from GET metrics/hourly; the
  // cell stays empty (and hidden) when the server gave none.
  const h=d.hour,hb=$('stHour');hb.hidden=!h;
  hb.textContent=h?'지난 한 시간 메일 '+h.mail+' · 보고 '+h.reports:'';
  const box=$('stUsage'),out=[];
  if(d.nodes){
    const worst=new Map();
    for(const n of d.nodes){
      if(n.revoked_at)continue;
      for(const u of n.usage||[]){
        if(u.remaining===null||u.remaining===undefined)continue;
        const k=u.adapter+'\n'+(u.account||''),w=worst.get(k);
        if(!w||u.remaining<w.remaining)worst.set(k,u);
      }
    }
    for(const u of [...worst.values()].sort((a,b)=>(a.adapter+a.account).localeCompare(b.adapter+b.account))){
      const used=Math.max(0,Math.min(100,100-u.remaining)),name=u.adapter.charAt(0).toUpperCase()+u.adapter.slice(1)+(u.account?' · '+u.account:'');
      const m=node('span','','meter'+(used>=80?' hot':'')),bar=node('i',''),fill=node('b','');
      fill.style.width=used+'%';bar.append(fill);
      m.append(node('strong',name),bar,node('span',used+'%'));
      m.title=name+' 한도 — '+used+'% 씀'+(u.resets_at?', '+new Date(u.resets_at).toLocaleString('ko-KR',{weekday:'short',hour:'2-digit',minute:'2-digit'})+'에 다시 찬다':'');
      out.push(m);
    }
  }
  box.replaceChildren(...out);
}
// frameLive takes the kinds of a live change: the list follows sessions,
// hosted sessions, questions and permission requests.
function frameLive(kinds){
  if(!kinds.some(k=>k==='sessions'||k==='terminals'||k==='questions'||k==='permissions'||k==='progress'||k==='settings'||k==='metrics'))return;
  if(!frameTimer)frameTimer=setTimeout(()=>{frameTimer=0;frameLoad()},400);
}
// frameStart runs once /api/admin/me answered (hostingOn known).
function frameStart(){
  $('newSession').hidden=$('sessNew').hidden=!hostingOn||!can('sessions');
  frameLoad();
  setInterval(frameLoad,30000);
}

// ---- 호버 설명 ----
// fillTitles gives every button, field and select under root without a
// title one: its aria-label, its placeholder, its label's text or its own.
function fillTitles(root){
  if(!root||!root.querySelectorAll)return;
  const els=root.matches&&root.matches('button,input,select,textarea')?[root]:[];
  els.push(...root.querySelectorAll('button:not([title]),input:not([title]),select:not([title]),textarea:not([title])'));
  for(const el of els){
    if(el.title||el.type==='hidden')continue;
    let t=el.getAttribute('aria-label')||el.getAttribute('placeholder')||'';
    if(!t){const l=el.closest('label')||(el.id&&document.querySelector('label[for="'+CSS.escape(el.id)+'"]'));if(l)t=l.textContent.trim()}
    if(!t&&el.tagName==='BUTTON')t=el.textContent.trim();
    if(!t&&el.tagName==='SELECT'&&el.selectedOptions[0])t=el.selectedOptions[0].textContent.trim();
    if(t)el.title=t.replace(/\s+/g,' ').slice(0,120);
  }
}
fillTitles(document.body);
new MutationObserver(ms=>{for(const m of ms)for(const n of m.addedNodes)if(n.nodeType===1)fillTitles(n)}).observe(document.body,{childList:true,subtree:true});
