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
function frameCard(r){
  const b=node('button','','sess');b.type='button';b.dataset.role=r.role;if(r.term)b.dataset.term=r.term.id;
  const [stText,stCls]=r.st==='offline'?['꺼짐','st-offline']:r.st==='unknown'?['모름','st-unknown']:sessionStatus(r.st);
  const name=frameService(r.role)===r.role?r.role:r.role.slice(frameService(r.role).length+1);
  const l1=node('span','','sess-l1'),l2=node('span','','sess-l2');
  l1.append(node('span','','dot '+stCls),node('b',name+(r.label?' · '+r.label:''),'sess-nm'));
  if(r.cli&&r.cli!=='unknown')l1.append(node('span',r.cli.split(/\s/)[0],'tag'));
  if(r.wait)l1.append(node('span',String(r.wait),'sess-w'));
  if(r.machine)l2.append(node('span',r.machine,'tag'));
  l2.append(node('span',r.ctx===null||r.ctx===undefined?'-':kTokens(r.ctx),'sess-ctx'));
  if(r.dir)l2.append(node('span','\u200e'+r.dir+'\u200e','sess-dir'));
  l1.firstChild.title=frameStates[r.st]||stText;
  b.append(l1,l2);
  b.title=r.role+' — '+(frameStates[r.st]||stText)+', 컨텍스트 '+(r.ctx===null||r.ctx===undefined?'-':kTokens(r.ctx))+(r.machine?', '+r.machine:'')+(r.wait?', 답 기다림 '+r.wait:'');
  b.onclick=()=>frameOpen(r);
  return b;
}
// frameOpen shows a session: a hosted one as its terminal panel (the
// workspace) or the terminal detail, any other one as the session detail.
function frameOpen(r){
  frameSel=r.term?r.term.id:r.role;frameMark();
  if(r.term&&ws&&ws.openTerm){frameLeaveList();ws.openTerm(r.term)}
  else if(r.term)openDetail('terminals',r.term.id,'screen');
  else openDetail('sessions',r.role);
}
// frameLeaveList steps out of a list screen (#514) to the workspace: the
// hash drops the screen, so its menu row opens it again.
function frameLeaveList(){
  if(!listTab(curTab))return;
  history.replaceState(null,'',location.pathname+location.search);
  hideDetail();$('panel-'+curTab).hidden=true;frameSplit(false);
  for(const n of tabNames){const t=$('tab-'+n);t.setAttribute('aria-selected','false');t.tabIndex=n==='requests'?0:-1}
  curTab='';
}
// frameMark marks the card of the session on screen.
function frameMark(){
  const cur=ws&&ws.session?ws.session()||frameSel:frameSel;
  for(const b of $('sessList').querySelectorAll('.sess')){const on=!!cur&&(b.dataset.term===cur||b.dataset.role===cur);b.classList.toggle('on',on);if(on)b.setAttribute('aria-current','true');else b.removeAttribute('aria-current')}
}
document.addEventListener('ws-follow',frameMark);
$('gsQ').addEventListener('input',frameDraw);
$('sessWait').onclick=()=>{frameWait=!frameWait;$('sessWait').setAttribute('aria-pressed',String(frameWait));frameDraw()};

// ---- 상태 줄 ----
function frameStatus(){
  const d=frameData;if(!d)return;
  const on=d.sessions.filter(s=>s.online).length;
  const machines=d.nodes?d.nodes.filter(n=>!n.revoked_at&&n.connected).length:null;
  $('stCount').textContent='세션 '+on+(machines===null?'':' · 기계 '+machines);
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
