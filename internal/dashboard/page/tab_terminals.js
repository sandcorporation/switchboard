// ---- 터미널 (hosting) ----
// The list opens a record in the detail panel (#terminals/<id>/<sub>): 화면
// (the live terminal), 기록 (runtime and the access log) and 설정. The add
// form sits in a drawer.
let termLimited=false;
async function loadTermLimit(){
  try{
    const l=await api('terminals/limit');
    termLimited=!!l.limited;
    let t=termLimited?'걸림 · '+(l.source==='operator'?'수동':'화면')+' · '+when(l.since):'없음';
    if(termLimited&&l.reset_at)t+=' · 초기화 '+when(l.reset_at);
    $('termLimitText').textContent=t;
    $('termLimitBtn').textContent=termLimited?'한도 풀기':'한도 걸기';
    $('termLimitBtn').setAttribute('aria-pressed',String(termLimited));
  }catch(e){$('termLimitText').textContent=e.message}
}
$('termLimitBtn').onclick=async()=>{
  try{await apiSend('PUT','terminals/limit',{limited:!termLimited});setMsg($('terminalsMsg'),termLimited?'구독 한도 풀림':'구독 한도 걸림','ok');loadTerminals()}
  catch(e){setMsg($('terminalsMsg'),e.message,'err')}
};
const termPresence={online:'온라인',offline:'오프라인','offline (process alive)':'오프라인 (프로세스 살아 있음)'};
// hostedBySlot maps a slot role (team/worker-1) to the hosted session holding it.
async function hostedBySlot(){
  const m=new Map();
  try{for(const v of await api('terminals'))if(v.slot&&v.slot.role)m.set(v.slot.role,v)}catch(e){}
  return m;
}
const termState={running:'실행 중',starting:'시작 중',stopped:'멈춤',waiting:'대기',crashloop:'반복 실패','backing off':'재시도 대기','resume failed':'재개 실패',relaunching:'다시 띄우는 중','backend down':'터미널 끊김',unknown:'모름','node offline':'노드 오프라인',gone:'삭제됨'};
function termStateText(st,rt){
  let t=termState[st]||st;
  if(st==='backing off'&&rt.next_attempt_at){
    const s=Math.max(0,Math.ceil((rt.next_attempt_at-Date.now())/1000));
    t+=' · '+(s<60?s+'초':Math.floor(s/60)+'분 '+(s%60)+'초')+' 뒤';
  }
  if(rt.resume_failed)t+=' · 새 대화(재개 실패)';
  return t;
}
function termSt(v){return (v.runtime||{}).state||'unknown'}
function termStClass(st){return st==='running'?'st-idle':st==='node offline'||st==='unknown'?'st-offline':st==='crashloop'||st==='resume failed'||st==='backend down'?'st-error':''}
const termTone={'st-idle':'success','st-error':'danger'};
function termRole(v){return v.slot&&v.slot.role?v.slot.role:v.base}
function termDesire(v){const st=termSt(v);return v.desired==='stopped'&&st!=='stopped'&&st!=='node offline'?' → 멈춤':''}
const termCols=['이름','프로필','노드','상태','권한','재시작','마지막 시작'];
const termChips=[['all','전체',()=>true],['running','실행 중',v=>termSt(v)==='running'],['stopped','멈춤',v=>v.desired==='stopped'||termSt(v)==='stopped'],['problem','문제',v=>/^(crashloop|resume failed|backing off|backend down)$/.test(termSt(v))],['offline','오프라인',v=>/^(node offline|unknown)$/.test(termSt(v))]];
// termSeq drops an answer a later load has overtaken.
let termData=null,termFilter='all',termSeq=0;
async function loadTerminals(){
  const seq=++termSeq;
  $('terminalsNote').textContent='불러오는 중…';
  loadTermLimit();
  try{
    const rows=await api('terminals');
    if(seq!==termSeq)return;
    termData=rows;drawTerminals();
  }catch(e){if(seq===termSeq)$('terminalsNote').textContent=e.message}
}
function drawTerminals(){
  if(!termData)return;
  const rows=termData,chips=$('termChips');chips.replaceChildren();
  for(const [k,label,f] of termChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===termFilter?'true':'false');
    b.append(node('span',String(rows.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{termFilter=k;drawTerminals()};
    chips.append(b);
  }
  const shown=rows.filter(termChips.find(c=>c[0]===termFilter)[2]),body=$('terminals');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const v of shown){
      const rt=v.runtime||{},st=termSt(v),stc=termStClass(st);
      const tr=node('tr','',st==='node offline'?'offline':stc==='st-error'?'rowerror':'');
      const name=node('td',termRole(v));if(v.slot&&v.slot.role)name.title=v.base;
      if(v.label)name.append(node('small',v.label));
      const state=node('td',termStateText(st,rt)+termDesire(v),stc);
      const pr=v.presence||'',sub=[termPresence[pr]||pr,v.accepting_work===false?'일감 받지 않음':''].filter(Boolean).join(' · ');
      if(sub)state.append(node('small',sub));
      const last=node('td',rt.started_at?ago(Date.now(),rt.started_at):'-','nowrap');last.title=when(rt.started_at);
      const cells=[name,node('td',v.profile||'-'),node('td',v.node),state,node('td',v.skip_permissions?'건너뜀':'물음','nowrap'),node('td',String(rt.restarts||0),'num'),last];
      cells.forEach((td,i)=>{if(i>0)td.dataset.label=termCols[i];tr.append(td)});
      detailRow(tr,'terminals',v.id);
      body.append(tr);
    }
    if(!shown.length)emptyRow(body,termCols.length,'터미널 없음');
  });
  $('terminalsNote').textContent=(shown.length===rows.length?'':shown.length+' / ')+rows.length+'개';
}
tabLoaders.terminals=loadTerminals;
// termNote is the last action's result, shown above the open record's sub-tabs.
let termNote=null;
async function termAct(v,method,path,body,done){
  try{
    await apiSend(method,'terminals/'+v.id+path,body);
    termNote={id:v.id,text:done,cls:'ok'};setMsg($('terminalsMsg'),done,'ok');loadTerminals();
    if(method==='DELETE'&&detailCur&&detailCur.id===v.id){closeDetail();return}
  }catch(e){termNote={id:v.id,text:e.message,cls:'err'};setMsg($('terminalsMsg'),e.message,'err')}
  drawDetail();
}
// termActs puts the record's actions (멈춤 or 시작 · 재시작 · 새 대화 · 지우기) in box.
function termActs(v,box){
  if(!can('sessions')){if(termNote&&termNote.id===v.id)box.append(node('p',termNote.text,'msg '+termNote.cls));return}
  const acts=node('div','','detail-acts');
  const add=(label,fn)=>{const b=node('button',label);b.type='button';b.onclick=fn;acts.append(b)};
  if(v.desired==='stopped')add('시작',()=>termAct(v,'PUT','',{desired:'running'},v.base+' 시작'));
  else{
    add('멈춤',()=>termAct(v,'PUT','',{desired:'stopped'},v.base+' 멈춤'));
    add('재시작',()=>termAct(v,'POST','/restart',undefined,v.base+' 재시작'));
  }
  add('새 대화',()=>{if(confirm(v.node+' '+v.base+' '+v.directory+' 세션을 새 대화로 시작할까요? 지금 대화로는 돌아가지 않습니다.'))termAct(v,'POST','/new-conversation',undefined,v.base+' 새 대화')});
  add('지우기',()=>{if(confirm(v.node+' '+v.base+' '+v.directory+' 세션을 지울까요?'))termAct(v,'DELETE','',undefined,v.base+' 삭제')});
  box.append(acts);
  if(termNote&&termNote.id===v.id)box.append(node('p',termNote.text,'msg '+termNote.cls));
}
function termDir(d){const s=node('span','‎'+d+'‎','dir');s.title=d;return s}
const termLogKind={attach:'연결',detach:'끊음',input:'입력'};
const termLogMode={view:'보기',input:'입력'};
const termEventKind={start:'시작',restart:'재시작',stop:'정지',crash:'비정상 종료',exit:'종료',adopt:'이어받음','plugin-failed':'플러그인 설치 실패',add:'추가',remove:'제거'};
const termEventActor={admin:'관리자',policy:'재시작 규칙',node:'노드'};
// termTable is a stack table with head labels and one row per item (rowOf: values and classes).
function termTable(id,labels,items,rowOf){
  const tbl=node('table','','stack list'),head=node('tr','');
  for(const h of labels)head.append(node('th',h));
  const thead=document.createElement('thead');thead.append(head);
  const body=document.createElement('tbody');body.id=id;
  for(const it of items){const tr=node('tr','');const [vals,cls]=rowOf(it);cells(tr,labels,vals,cls);body.append(tr)}
  if(!items.length)emptyRow(body,labels.length,'기록 없음');
  tbl.append(thead,body);return tbl;
}
// termHistory draws 기록: the runtime now, the session's starts, restarts, stops and exits (#252), then who attached and typed.
async function termHistory(v,box){
  const rt=v.runtime||{},st=termSt(v);
  const pairs=[['상태',termStateText(st,rt)+termDesire(v)],['이후',when(rt.since)],['마지막 시작',when(rt.started_at)],['재시작',String(rt.restarts||0)+'회'],['마지막 종료',rt.last_exit===null||rt.last_exit===undefined?'-':String(rt.last_exit)]];
  if(rt.next_attempt_at)pairs.push(['다음 시도',when(rt.next_attempt_at)]);
  if(rt.resume_failed)pairs.push(['재개','실패 · 새 대화']);
  const id=encodeURIComponent(v.id);
  // acts: the admin actions on this session (#446); a view that cannot read
  // them (a boss role's) leaves the section out.
  const [events,rows,acts]=await Promise.all([api('terminals/'+id+'/events'),api('terminals/'+id+'/log'),api('admin-actions?target='+id+'&limit=20').catch(()=>null)]);
  box.append(dl(pairs),node('h3','실행'));
  box.append(termTable('termEvents',['시각','종류','종료 코드','재시작','요청'],events,e=>[[when(e.at),(termEventKind[e.kind]||e.kind)+(e.reason?' · '+e.reason:''),e.exit_code===null||e.exit_code===undefined?'-':String(e.exit_code),String(e.restarts||0),termEventActor[e.actor]||e.actor||'-'],['','','num','num','']]));
  box.append(node('h3','접속'));
  box.append(termTable('termLog',['시각','관리자','종류','방식','주소','바이트'],rows,e=>[[when(e.at),e.admin_user,termLogKind[e.kind]||e.kind,termLogMode[e.mode]||e.mode||'-',e.ip||'-',e.kind==='input'?String(e.bytes):'-'],['','','','','mono','num']]));
  if(Array.isArray(acts)){
    box.append(node('h3','관리'));
    box.append(termTable('termActs',['시각','관리자','동작','방식','주소'],acts,e=>[[when(e.at),e.admin,(window.wsNodes?wsNodes.actionName(e.action):e.action)+(e.detail?' · '+e.detail:''),e.method||'-',e.addr||'-'],['','','','','mono']]));
  }
}
// termSettings draws 설정: what the record runs and the fields a PUT changes.
function termSettings(v,box){
  const pr=v.presence||'';
  box.append(dl([['노드',v.node],['역할',v.base],['슬롯',v.slot&&v.slot.role?v.slot.role:'-'],['접속',termPresence[pr]||pr||'-'],['프로필',v.profile||'-'],['인자',(v.args||[]).join(' ')||'-'],['폴더',termDir(v.directory)],['묶음',v.group||'없음'],['일감',v.accepting_work===false?'받지 않음':'받음']]));
  const perm=node('button',v.skip_permissions?'권한 건너뜀':'권한 물음');perm.type='button';perm.id='termPermToggle';
  perm.setAttribute('aria-pressed',String(!!v.skip_permissions));
  perm.onclick=()=>termAct(v,'PUT','',{skip_permissions:!v.skip_permissions},v.base+(v.skip_permissions?' 권한 물음':' 권한 건너뜀'));
  const f=node('form','','settings-form termform');f.id='termEdit';
  const field=(label,el)=>{const l=node('label',label+' ');l.append(el);f.append(l);return el};
  const input=(val)=>{const i=document.createElement('input');i.value=val||'';i.autocomplete='off';i.spellcheck=false;return i};
  const select=(val,opts)=>{const s=document.createElement('select');for(const [k,t] of opts){const o=node('option',t);o.value=k;s.append(o)}s.value=val;return s};
  const label=field('라벨',input(v.label)),parent=field('부모',input(v.parent));
  const grp=field('묶음',select(v.group||'',[['','없음'],...(v.group?[[v.group,v.group]]:[])]));groupOptions(grp,v.group||'');
  const adp=field('어댑터',select(v.adapter||'',[['','기본'],...(v.adapter?[[v.adapter,v.adapter]]:[])]));adapterOptions(adp,v.node,v.adapter||'');
  const restart=field('재시작',select(v.restart||'always',[['always','always'],['on-failure','on-failure'],['never','never']]));
  const args=field('인자',input((v.args||[]).join(' ')));
  const row=node('div','','row'),save=node('button','저장');save.type='submit';row.append(save,perm);f.append(row);
  f.onsubmit=e=>{
    e.preventDefault();
    const c={},a=args.value.split(/\s+/).filter(Boolean);
    if(label.value.trim()!==(v.label||''))c.label=label.value.trim();
    if(parent.value.trim()!==(v.parent||''))c.parent=parent.value.trim();
    if(grp.value!==(v.group||''))c.group=grp.value;
    if(adp.value!==(v.adapter||''))c.adapter=adp.value;
    if(restart.value!==(v.restart||'always'))c.restart=restart.value;
    if(a.join(' ')!==(v.args||[]).join(' '))c.args=a;
    if(!Object.keys(c).length)return;
    termAct(v,'PUT','',c,v.base+' 설정 저장');
  };
  if(!can('sessions')){perm.hidden=true;save.hidden=true;for(const el of f.elements)el.disabled=true}
  box.append(f,node('p','바꾼 설정은 다음 시작부터 적용','muted'));
}
// ---- 예약 (#336): the record's scheduled runs and their recent runs ----
const autoResult={fired:'실행',skipped:'건너뜀',error:'오류',timeout:'시간 초과',overrun:'시간 넘김',busy:'겹침',waiting:'대기','node-offline':'노드 꺼짐'};
const autoTone={fired:'success',skipped:'neutral',error:'danger',timeout:'danger',overrun:'danger',busy:'warning',waiting:'warning','node-offline':'warning'};
function autoBadge(r){return badge(autoResult[r]||r,autoTone[r]||'neutral')}
// autoOpen holds the first run id of each skipped stretch the viewer opened.
const autoOpen=new Set();
// autoGroups folds runs of two or more skipped in a row into one group.
function autoGroups(runs){
  const out=[];
  for(const r of runs){
    const g=out[out.length-1];
    if(r.result==='skipped'&&g&&g.skipped)g.runs.push(r);
    else out.push({skipped:r.result==='skipped',runs:[r]});
  }
  return out;
}
function autoRunRow(body,labels,r){
  const tr=node('tr','');
  const res=node('td','','nowrap');res.append(autoBadge(r.result));
  if(r.exit_code!==null&&r.exit_code!==undefined)res.append(' '+r.exit_code);
  if(r.manual)res.append(' 수동');
  const det=node('td',r.detail||(r.output?'':'-'),'wrap');
  if(r.output){const d=document.createElement('details');d.append(node('summary','출력'),node('pre',r.output));det.append(d)}
  [node('td',when(r.at),'nowrap'),res,det].forEach((td,i)=>{if(i>0)td.dataset.label=labels[i];tr.append(td)});
  body.append(tr);
}
function autoRuns(runs){
  const labels=['시각','결과','상세'];
  const tbl=node('table','','stack list autoruns'),head=node('tr','');
  for(const h of labels)head.append(node('th',h));
  const thead=document.createElement('thead');thead.append(head);
  const body=document.createElement('tbody');
  for(const g of autoGroups(runs)){
    const first=g.runs[0];
    if(!g.skipped||g.runs.length<2||autoOpen.has(first.id)){for(const r of g.runs)autoRunRow(body,labels,r);continue}
    const tr=node('tr',''),t=node('td',when(g.runs[g.runs.length-1].at)+' ~ '+when(first.at),'wrap');
    const res=node('td','');res.dataset.label=labels[1];res.colSpan=2;
    const more=node('button','건너뜀 '+g.runs.length+'회');more.type='button';
    more.onclick=()=>{autoOpen.add(first.id);drawDetail()};
    res.append(more);
    tr.append(t,res);body.append(tr);
  }
  if(!runs.length)emptyRow(body,labels.length,'기록 없음');
  tbl.append(thead,body);return tbl;
}
async function autoAct(v,a,method,path,done){
  try{
    await apiSend(method,'terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+path);
    termNote={id:v.id,text:done,cls:'ok'};
  }catch(e){termNote={id:v.id,text:e.message,cls:'err'}}
  drawDetail();
}
function autoName(a){return a.label||'예약 실행 '+a.id}
async function termAutos(v,box){
  const id=encodeURIComponent(v.id);
  const list=await api('terminals/'+id+'/autos');
  if(!list.length){box.append(node('p','예약 실행 없음','muted'));return}
  const views=await Promise.all(list.map(a=>api('terminals/'+id+'/autos/'+a.id+'?runs=20')));
  for(const a of views){
    const sec=node('section','','auto');sec.id='auto-'+a.id;
    const h=node('h3',autoName(a)+' ');
    h.append(badge(a.state==='paused'?'멈춤':'활성',a.state==='paused'?'warning':'success'));
    if(a.alert){const b=autoBadge(a.alert);b.title='알림 중';h.append(' ',b)}
    sec.append(h);
    const pairs=[['cron',a.cron+' ('+a.tz+')'],['다음',a.state==='paused'?'-':when(a.next_fire_at)],['사전 확인',a.check?node('code',a.check):'-'],['대화',a.resume?'이어서':'새 대화'],['최대 실행',Math.round(a.max_run_ms/60000)+'분'],['알림 받는 역할',(a.notify||[]).join(', ')||'-'],['프롬프트',node('pre',a.prompt)]];
    sec.append(dl(pairs));
    const acts=node('div','','detail-acts');
    const add=(label,fn)=>{const b=node('button',label);b.type='button';b.onclick=fn;acts.append(b)};
    add('지금 실행',()=>autoAct(v,a,'POST','/run',autoName(a)+' 실행'));
    if(a.state==='paused')add('다시',()=>autoAct(v,a,'POST','/resume',autoName(a)+' 다시'));
    else add('멈춤',()=>autoAct(v,a,'POST','/pause',autoName(a)+' 멈춤'));
    add('지우기',()=>{if(confirm(autoName(a)+'을(를) 지울까요? 실행 기록도 함께 지워지고 되돌릴 수 없습니다.'))autoAct(v,a,'DELETE','',autoName(a)+' 삭제')});
    sec.append(acts,autoRuns(a.runs||[]));
    box.append(sec);
  }
}
const termLoad=async id=>(await api('terminals')).find(v=>v.id===id)||null;
registerDetail('terminals',{
  list:'터미널',
  load:termLoad,
  head:v=>{const st=termSt(v);return {title:termRole(v)+(v.label?' · '+v.label:''),badge:[termState[st]||st,termTone[termStClass(st)]||'neutral']}},
  subs:()=>[['screen','화면'],['log','기록'],['autos','예약'],['settings','설정']],
  draw:async(v,sub,box)=>{
    termActs(v,box);
    if(sub==='log'){await termHistory(v,box);return}
    if(sub==='settings'){termSettings(v,box);return}
    if(sub==='autos'){await termAutos(v,box);return}
    box.append(termPaneFor(v));
  }
});
$('termAddOpen').onclick=()=>$('termAdd').showModal();
$('termAddOpen').addEventListener('click',()=>{adapterOptions($('termAdapter'),$('termNode').value.trim(),$('termAdapter').value);groupOptions($('termGroup'),$('termGroup').value)});
$('termNode').onchange=()=>adapterOptions($('termAdapter'),$('termNode').value.trim(),'');
// adapterOptions fills sel with the agent adapters the node reported (#434);
// the first option, the empty value, is the node's default.
// groupOptions fills sel with the work groups (#435); the empty value is none.
async function groupOptions(sel,cur){
  let list=[];
  try{list=(await api('terminals/work-groups')).map(g=>g.name)}catch(e){}
  if(cur&&!list.includes(cur))list=list.concat(cur);
  const first=node('option','없음');first.value='';
  sel.replaceChildren(first,...list.map(g=>{const o=node('option',g);o.value=g;return o}));
  sel.value=cur||'';
}
async function adapterOptions(sel,nodeName,cur){
  let list=[],def='';
  try{const n=(await api('nodes')).find(x=>x.name===nodeName);if(n){list=n.adapters||[];def=n.default_adapter||''}}catch(e){}
  if(cur&&!list.includes(cur))list=list.concat(cur);
  const first=node('option',def?'기본 · '+def:'기본');first.value='';
  sel.replaceChildren(first,...list.map(a=>{const o=node('option',a);o.value=a;return o}));
  sel.value=cur||'';
}
$('termAddClose').onclick=()=>$('termAdd').close();
// The drawer takes its own Esc, so an open detail panel stays open.
$('termAdd').addEventListener('keydown',e=>{if(e.key==='Escape'){e.preventDefault();$('termAdd').close()}});
// ---- 터미널 화면 ----
// With hosting on the page loads the workspace (internal/dashboard/workspace,
// files listed by /api/admin/me as workspace_assets): the three-column frame
// and the terminal panels, one socket and renderer per panel. ws is that
// workspace; null until it has loaded, and always with hosting off.
// #termHold keeps the old single screen's markup, unused.
let ws=null,wsFailed='';
// termRefusedText is why this address cannot open a terminal
// (terminal_refused from /api/admin/me, SWITCHBOARD_HOSTING_TERMINAL).
const termRefusedText={off:'터미널이 꺼져 있음',loopback:'이 주소에서는 터미널이 꺼져 있음 (이 기계에서만 열림)',allow:'이 주소는 터미널 허용 대역 밖'};
// termPaneFor(v) is what a detail body shows for the terminal: the workspace
// opens it as a centre panel.
function termPaneFor(v){
  if(termRefused)return node('p',termRefusedText[termRefused]||'이 주소에서는 터미널이 꺼져 있음','msg err');
  if(ws)return ws.termPaneFor(v);
  return wsFailed?node('p',wsFailed,'msg err'):node('p','불러오는 중…','muted');
}
function termGc(){if(ws)ws.termGc()}
// loadAssets adds stylesheets and scripts (same origin) in order.
async function loadAssets(list){
  for(const u of list){
    await new Promise((ok,fail)=>{
      let el;
      if(u.endsWith('.css')){el=document.createElement('link');el.rel='stylesheet';el.href=u}
      else{el=document.createElement('script');el.src=u}
      el.onload=ok;el.onerror=()=>fail(Error('불러오기 실패: '+u));document.head.append(el);
    });
  }
}
let wsLoading=null;
function loadWorkspace(list){
  if(ws||wsLoading||!list.length)return;
  wsLoading=loadAssets(list).then(()=>{ws=wsStart();if(listTab(curTab))frameSplit(true);if(curTab)frameScreen('menu');frameSessionTry()}).catch(e=>{wsFailed=e.message}).then(()=>{if(detailCur)drawDetail()});
}
$('termForm').onsubmit=async e=>{
  e.preventDefault();
  const args=$('termArgs').value.split(/\s+/).filter(Boolean);
  const rec={node:$('termNode').value.trim(),base:$('termBase').value.trim(),profile:$('termProfile').value.trim(),directory:$('termDir').value.trim(),label:$('termLabel').value.trim(),parent:$('termParent').value.trim(),restart:$('termRestart').value,group:$('termGroup').value,adapter:$('termAdapter').value,skip_permissions:$('termPerms').value==='skip',args:args};
  try{await apiSend('POST','terminals',rec);setMsg($('terminalsMsg'),rec.base+' 추가','ok');$('termForm').reset();$('termAdd').close();loadTerminals()}
  catch(err){setMsg($('termAddMsg'),err.message,'err')}
};
$('termClear').onclick=()=>{$('termForm').reset();setMsg($('termAddMsg'),'')};
$('reloadTerminals').onclick=loadTerminals;
// targetPath keeps the slashes of a target (svc/coordinator, svc/*) as path separators.
function targetPath(t){return t.split('/').map(encodeURIComponent).join('/')}
