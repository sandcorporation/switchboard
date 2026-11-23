// ---- 설정 ----
// Four views under the 설정 tab: 질문 설정 (#settings), 역할
// (#settings-roles), 노드 (#settings-nodes, hosting only) and 다시 알림
// (#settings-reminders, GET /api/admin/settings, read-only). A row opens
// #settings/<kind>:<id>/<sub> with kind qs (question setting target), role
// or node; closing goes back to that kind's view.
const setViewHash={qsettings:'settings',roles:'settings-roles',nodes:'settings-nodes',groups:'settings-groups',reminders:'settings-reminders'};
const setKindView={qs:'qsettings',role:'roles',node:'nodes'};
let setView='qsettings';
const setNotes=new Map(),setForms=new Map();
function setViewButton(v){return $('setViews').querySelector('[data-view="'+v+'"]')}
function showSettingsView(v){
  if(!setViewHash[v]||((v==='nodes'||v==='groups')&&!hostingOn))v='qsettings';
  const changed=v!==setView;setView=v;
  for(const k of Object.keys(setViewHash)){$('setView-'+k).hidden=k!==v;setViewButton(k).setAttribute('aria-pressed',k===v?'true':'false')}
  if(changed){$('settingsNote').textContent='';setMsg($('settingsMsg'),'');if(tabLoaded.has('settings'))loadSettingsView()}
}
function settingsViewOfHash(h){for(const [v,x] of Object.entries(setViewHash))if(x===h)return v;return ''}
$('setViews').addEventListener('click',e=>{const b=e.target.closest('button[data-view]');if(b)selectTab(setViewHash[b.dataset.view])});
function loadSettingsView(){return ({qsettings:loadQSettings,roles:loadRoles,nodes:loadNodes,groups:loadGroups,reminders:loadReminders})[setView]()}
tabLoaders.settings=loadSettingsView;
// setNote keeps a result line for an item's detail across the redraws of
// the next few seconds (a save also triggers a live redraw).
function setNote(id,text,kind){setNotes.set(id,{text:text,kind:kind,at:Date.now()})}
function drawSetNote(box,id){
  const n=setNotes.get(id),m=node('div','','msg');m.setAttribute('role','status');
  if(n&&Date.now()-n.at<10000)setMsg(m,n.text,n.kind);
  box.append(m);
}
function setButton(text,cls,run){const b=node('button',text,cls);b.type='button';b.onclick=run;return b}
// cardText sets the words a two-line card shows before a value (the table
// header says it on a wide screen).
function cardText(td,pre){td.dataset.pre=pre;return td}
// 질문 설정
const qsCols=['대상','질문 가능','지켜보는 역할','디스코드','바뀐 때'];
async function loadQSettings(){
  $('settingsNote').textContent='불러오는 중…';
  try{
    const [rows,refused]=await Promise.all([api('question-settings'),api('question-refusals')]);
    if(setView!=='qsettings')return;
    drawRefusals(refused);
    const body=$('settingsTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const v of rows){
        const tr=node('tr','');
        const ca=cell('');ca.append(badge(v.can_ask?'질문 가능':'질문 불가',v.can_ask?'success':'neutral'));
        const at=cell(shortWhen(v.updated_at),qsCols[4],'nowrap');at.title=when(v.updated_at);
        tr.append(cell(v.target,'','strong mono'),ca,cardBreak(),cardText(cell(v.watchers.length?v.watchers.join(', '):'-',qsCols[2]),'지켜봄 '),cardText(cell(v.discord?'켬':'끔',qsCols[3],'nowrap'),'디스코드 '),at);
        detailRow(tr,'settings','qs:'+v.target);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,qsCols.length,'설정 없음');
    });
    $('settingsNote').textContent=rows.length+'개';
  }catch(e){if(setView==='qsettings')$('settingsNote').textContent=e.message}
}
// drawRefusals lists the roles refused for can_ask in the last 24 h (#286).
const refCols=['역할','맞은 대상','대신 물을 역할','횟수','마지막 거절'];
function drawRefusals(rows){
  const body=$('refusalsTable');body.replaceChildren();
  for(const v of rows){
    const tr=node('tr','');
    const at=cell(shortWhen(v.at),refCols[4],'nowrap');at.title=when(v.at);
    tr.append(cell(v.role,'','strong mono'),cardText(cell(v.target==='none'?'없음':v.target,refCols[1],'mono'),'대상 '),cardText(cell(v.ask_via||'-',refCols[2],'mono'),'물을 곳 '),cardText(cell(String(v.count),refCols[3],'num'),'횟수 '),at);
    body.append(tr);
  }
  if(!rows.length)emptyRow(body,refCols.length,'거절 없음');
}
// qsForm is the edit form of one target. It is kept across live redraws
// while the stored setting is unchanged, so a half-made edit survives them.
function qsForm(v){
  const kept=setForms.get(v.target);
  if(kept&&kept.stamp===v.updated_at)return kept.form;
  const f=node('form','','settings-form termform');
  const check=(text,on)=>{const l=node('label','','check'),i=document.createElement('input');i.type='checkbox';i.checked=on;l.append(i,' '+text);return [l,i]};
  const [l1,canAsk]=check('질문할 수 있음 (can_ask)',v.can_ask),[l2,discord]=check('디스코드 알림 (discord)',v.discord);
  const l3=node('label','지켜보는 역할 (쉼표로 구분, 최대 20) '),w=document.createElement('input');
  w.value=v.watchers.join(', ');w.autocomplete='off';w.spellcheck=false;w.setAttribute('autocapitalize','off');w.placeholder='team/boss, ops/lead';l3.append(w);
  const save=node('button','저장');save.type='submit';
  const row=node('div','','row');row.append(save);
  f.append(l1,l2,l3,row);
  const id='qs:'+v.target;
  f.onsubmit=async e=>{
    e.preventDefault();
    const watchers=w.value.split(',').map(x=>x.trim()).filter(Boolean);
    try{
      await apiSend('PUT','question-settings/'+targetPath(v.target),{can_ask:canAsk.checked,watchers:watchers,discord:discord.checked});
      setForms.delete(v.target);setNote(id,v.target+' 설정을 저장했습니다.','ok');
    }catch(err){setNote(id,err.message,'err')}
    loadQSettings();drawDetail();
  };
  setForms.set(v.target,{stamp:v.updated_at,form:f});
  return f;
}
function drawQS(v,box){
  if(v.target!=='default'){
    const acts=node('div','','detail-acts');
    acts.append(setButton('삭제','',async()=>{
      if(!confirm(v.target+' 설정을 지울까요?'))return;
      try{await apiSend('DELETE','question-settings/'+targetPath(v.target));setForms.delete(v.target);setMsg($('settingsMsg'),v.target+' 설정을 지웠습니다.','ok');closeDetail();loadQSettings()}
      catch(e){setNote('qs:'+v.target,e.message,'err');drawDetail()}
    }));
    box.append(acts);
  }
  drawSetNote(box,'qs:'+v.target);
  box.append(dl([['대상',v.target],['바뀐 때',when(v.updated_at)]]),qsForm(v));
}
$('setAddOpen').onclick=()=>{setMsg($('setAddMsg'),'');$('setAdd').showModal();$('setTarget').focus()};
$('setAddClose').onclick=()=>$('setAdd').close();
$('setAdd').addEventListener('keydown',e=>{if(e.key==='Escape'){e.preventDefault();$('setAdd').close()}});
$('settingsForm').onsubmit=async e=>{
  e.preventDefault();
  const target=$('setTarget').value.trim();
  if(!target){setMsg($('setAddMsg'),'대상을 쓰세요.','err');return}
  const watchers=$('setWatchers').value.split(',').map(x=>x.trim()).filter(Boolean);
  try{
    await apiSend('PUT','question-settings/'+targetPath(target),{can_ask:$('setCanAsk').checked,watchers:watchers,discord:$('setDiscord').checked});
    $('settingsForm').reset();$('setAdd').close();setForms.delete(target);
    setMsg($('settingsMsg'),target+' 설정을 저장했습니다.','ok');loadQSettings();
    openDetail('settings','qs:'+target,'edit');
  }catch(err){setMsg($('setAddMsg'),err.message,'err')}
};
$('setClear').onclick=()=>{$('settingsForm').reset();setMsg($('setAddMsg'),'')};
// 역할
const roleCols=['역할','상태','등록','끊은 때'];
async function loadRoles(){
  $('settingsNote').textContent='불러오는 중…';
  try{
    const rows=await api('roles');
    if(setView!=='roles')return;
    const body=$('rolesTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const v of rows){
        const tr=node('tr','',v.revoked_at?'done':'');
        const st=cell('');st.append(badge(v.revoked_at?'끊김':'사용 중',v.revoked_at?'danger':'success'));
        const at=cardText(cell(shortWhen(v.created_at),roleCols[2],'nowrap'),'등록 ');at.title=when(v.created_at);
        const off=cardText(cell(v.revoked_at?shortWhen(v.revoked_at):'-',roleCols[3],v.revoked_at?'nowrap revoked':'nowrap cardhide'),'끊음 ');if(v.revoked_at)off.title=when(v.revoked_at);
        tr.append(cell(v.role,'','strong mono'),st,cardBreak(),at,off);
        detailRow(tr,'settings','role:'+v.role);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,roleCols.length,'역할 없음');
    });
    const off=rows.filter(v=>v.revoked_at).length;
    $('settingsNote').textContent=rows.length+'개'+(off?' · 끊김 '+off:'');
  }catch(e){if(setView==='roles')$('settingsNote').textContent=e.message}
}
function drawRole(v,box){
  const id='role:'+v.role,revoke=!v.revoked_at,acts=node('div','','detail-acts');
  acts.append(setButton(revoke?'끊기':'되살리기','',async()=>{
    const used=revoke&&v.last_used_at&&Date.now()-new Date(v.last_used_at).getTime()<864e5;
    if(!confirm(revoke?v.role+' 역할을 끊을까요? 이 토큰으로 접속한 세션이 모두 끊깁니다.'+(used?'\n최근 사용: '+when(v.last_used_at)+(v.last_machine?' · '+v.last_machine:''):''):v.role+' 역할을 되살릴까요?'))return;
    try{await apiSend('POST','roles/'+targetPath(v.role)+(revoke?'/revoke?force=1':'/restore'));setNote(id,v.role+(revoke?' 역할을 끊었습니다.':' 역할을 되살렸습니다.'),'ok')}
    catch(e){setNote(id,e.message,'err')}
    loadRoles();drawDetail();
  }));
  box.append(acts);
  drawSetNote(box,id);
  box.append(dl([['역할',v.role],['등록',when(v.created_at)],['끊은 때',v.revoked_at?when(v.revoked_at):'-']]));
}
// 노드 (hosting)
const nodeCols=['노드','기계','OS','버전','마지막 접속','끊은 때'];
async function loadNodes(){
  if(!hostingOn)return;
  $('settingsNote').textContent='불러오는 중…';
  try{
    const rows=await api('nodes');
    if(setView!=='nodes')return;
    const body=$('nodesTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const v of rows){
        const tr=node('tr','',v.revoked_at?'offline':'');
        const name=cell(v.name,'','strong mono');
        if(v.revoked_at)name.append(' ',badge('끊김','danger'));
        const seen=cardText(cell(when(v.last_seen_at),nodeCols[4],'nowrap'),'접속 ');
        const off=cell(v.revoked_at?when(v.revoked_at):'-',nodeCols[5],v.revoked_at?'revoked cardhide':'cardhide');
        tr.append(name,cardBreak(),cell(v.machine||'-',nodeCols[1]),cell(v.os||'-',nodeCols[2]),cell(v.version||'-',nodeCols[3]),seen,off);
        detailRow(tr,'settings','node:'+v.name);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,nodeCols.length,'노드 없음');
    });
    const off=rows.filter(v=>v.revoked_at).length;
    $('settingsNote').textContent=rows.length+'개'+(off?' · 끊김 '+off:'');
  }catch(e){if(setView==='nodes')$('settingsNote').textContent=e.message}
}
// 묶음 (#435): work groups and their thresholds
const groupCols=['묶음','문턱 %','세션','바뀐 때',''];
function thresholdOf(input){const t=input.value.trim();return t===''?null:Number(t)}
async function loadGroups(){
  if(!hostingOn)return;
  $('settingsNote').textContent='불러오는 중…';
  try{
    const rows=await api('terminals/work-groups');
    if(setView!=='groups')return;
    const body=$('groupsTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const g of rows){
        const tr=node('tr');
        const th=document.createElement('input');th.type='number';th.min='0';th.max='100';th.step='1';th.inputMode='numeric';
        th.value=g.threshold===null||g.threshold===undefined?'':String(g.threshold);th.setAttribute('aria-label',g.name+' 문턱 %');
        const save=setButton('저장','',async()=>{
          try{await apiSend('PUT','terminals/work-groups/'+encodeURIComponent(g.name),{threshold:thresholdOf(th)});setMsg($('settingsMsg'),g.name+' 문턱 저장','ok')}
          catch(e){setMsg($('settingsMsg'),e.message,'err')}
          loadGroups();
        });
        const rm=setButton('지우기','',async()=>{
          if(!confirm(g.name+' 묶음을 지울까요?'))return;
          try{await apiSend('DELETE','terminals/work-groups/'+encodeURIComponent(g.name));setMsg($('settingsMsg'),g.name+' 묶음을 지웠습니다.','ok')}
          catch(e){setMsg($('settingsMsg'),e.message,'err')}
          loadGroups();
        });
        rm.disabled=g.sessions>0;if(g.sessions>0)rm.title='세션이 있는 묶음은 지울 수 없음';
        const tc=cell('',groupCols[1]);tc.append(th,' ',save);
        const acts=cell('',groupCols[4]);acts.append(rm);
        tr.append(cell(g.name,'','strong mono'),cardBreak(),tc,cell(String(g.sessions),groupCols[2],'num'),cell(when(g.updated_at),groupCols[3],'nowrap'),acts);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,groupCols.length,'묶음 없음');
    });
    $('settingsNote').textContent=rows.length+'개';
  }catch(e){if(setView==='groups')$('settingsNote').textContent=e.message}
}
$('groupForm').onsubmit=async e=>{
  e.preventDefault();
  const name=$('groupName').value.trim();
  try{await apiSend('POST','terminals/work-groups',{name:name,threshold:thresholdOf($('groupThreshold'))});$('groupName').value='';$('groupThreshold').value='';setMsg($('settingsMsg'),name+' 묶음 추가','ok')}
  catch(err){setMsg($('settingsMsg'),err.message,'err')}
  loadGroups();
};
function drawNode(v,box){
  const id='node:'+v.name;
  if(!v.revoked_at){
    const acts=node('div','','detail-acts');
    acts.append(setButton('끊기','',async()=>{
      if(!confirm(v.name+' 노드를 끊을까요? 연결이 바로 끊기고 이 노드의 토큰은 다시 쓸 수 없습니다.'))return;
      try{await apiSend('DELETE','nodes/'+encodeURIComponent(v.name));setNote(id,v.name+' 노드를 끊었습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadNodes();drawDetail();
    }));
    box.append(acts);
  }
  drawSetNote(box,id);
  box.append(dl([['노드',v.name],['기계',v.machine||'-'],['OS',v.os||'-'],['버전',v.version||'-'],['등록',when(v.created_at)],['마지막 접속',when(v.last_seen_at)],['끊은 때',v.revoked_at?when(v.revoked_at):'-']]));
  if(v.accounts&&v.accounts.length)box.append(accountTable(v,id));
}
// 계정 (#435): each agent account's remaining limit and the operator's value
const accountCols=['계정','남은 %','출처','세션','수동 %',''];
const accountSource={operator:'한도 켬',manual:'수동',adapter:'에이전트',screen:'화면'};
function accountTable(v,id){
  const t=node('table','','list'),head=node('tr'),body=document.createElement('tbody');
  for(const c of accountCols)head.append(node('th',c));
  const th=document.createElement('thead');th.append(head);t.append(th,body);
  for(const a of v.accounts){
    const tr=node('tr'),name=a.adapter+' / '+(a.account||'기본');
    const inp=document.createElement('input');inp.type='number';inp.min='0';inp.max='100';inp.step='1';inp.inputMode='numeric';
    inp.value=a.manual===null||a.manual===undefined?'':String(a.manual);inp.setAttribute('aria-label',name+' 수동 %');
    const send=async remaining=>{
      try{await apiSend('PUT','terminals/limit/accounts',{node:v.name,adapter:a.adapter,account:a.account||'',remaining:remaining});setNote(id,name+(remaining===null?' 수동 % 지움':' 수동 % 저장'),'ok')}
      catch(e){setNote(id,e.message,'err')}
      loadNodes();drawDetail();
    };
    const save=setButton('저장','',()=>send(thresholdOf(inp)));
    const clear=setButton('지우기','',()=>send(null));clear.disabled=a.manual===null||a.manual===undefined;
    const manual=document.createElement('td');manual.dataset.label=accountCols[4];manual.append(inp);
    const acts=document.createElement('td');acts.append(save,' ',clear);
    tr.append(cell(name,accountCols[0],'mono'),cell(a.remaining===null||a.remaining===undefined?'모름':a.remaining+'%',accountCols[1]),cell(accountSource[a.source]||'-',accountCols[2]),cell(String(a.sessions||0),accountCols[3]),manual,acts);
    body.append(tr);
  }
  return t;
}
// ruleLast: the newest answered-rule step (#274), with the closed IDs.
function ruleLast(l){
  if(!l)return '-';
  const ids=(l.ids||[]).map(i=>'#'+i).join(' ');
  return (l.from?l.from+' → '+l.to:l.to+' 저장')+' · '+l.marked+'건 닫음 · '+when(l.at)+(ids?' · '+ids:'');
}
// 다시 알림 (#223): the reminder settings and the server version, as the
// start-up line reports them.
async function loadReminders(){
  $('settingsNote').textContent='불러오는 중…';
  try{
    const s=await api('settings');
    if(setView!=='reminders')return;
    const r=s.reminders,st=document.createElement('span');
    st.append(badge(r.on?'켬':'끔',r.on?'success':'neutral'));
    $('remindBox').replaceChildren(dl([['다시 알림',st],['첫 알림',r.after],['최대 횟수',String(r.max)],['멈춤 문턱',r.idle==='off'?'끔':r.idle],['멈춤 알림',r.idle==='off'?'-':String(r.idle_max||1)+'번'],['켠 때',r.since?when(r.since):r.on?'첫 점검 전':'-'],['서버 판',s.version],['터미널 호스팅',s.hosting?'켬':'끔'],['답 규칙 판',String(s.answered_rule.version)],['마지막 이주',ruleLast(s.answered_rule.last)]]));
    $('settingsNote').textContent='';
  }catch(e){if(setView==='reminders')$('settingsNote').textContent=e.message}
}
async function findSetting(kind,key){
  if(kind==='qs')return (await api('question-settings')).find(x=>x.target===key);
  if(kind==='role')return (await api('roles')).find(x=>x.role===key);
  if(kind==='node'&&hostingOn)return (await api('nodes')).find(x=>x.name===key);
  return undefined;
}
registerDetail('settings',{
  list:'설정',kinds:['settings','terminals'],
  back:id=>setViewHash[setKindView[id.slice(0,id.indexOf(':'))]]||'settings',
  load:async id=>{
    const i=id.indexOf(':'),k=id.slice(0,i),v=i>0?await findSetting(k,id.slice(i+1)):undefined;
    return v?{k:k,v:v}:null;
  },
  head:x=>x.k==='qs'?{title:x.v.target,badge:x.v.can_ask?['질문 가능','success']:['질문 불가','neutral']}
    :x.k==='role'?{title:x.v.role,badge:x.v.revoked_at?['끊김','danger']:['사용 중','success']}
    :{title:x.v.name,badge:x.v.revoked_at?['끊김','danger']:null},
  subs:x=>x.k==='qs'?[['edit','설정']]:[['info','정보']],
  draw:(x,sub,box)=>x.k==='qs'?drawQS(x.v,box):x.k==='role'?drawRole(x.v,box):drawNode(x.v,box)
});
