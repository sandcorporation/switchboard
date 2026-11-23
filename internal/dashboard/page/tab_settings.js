// ---- 설정 (#515) ----
// One list column of groups — 역할, 기계 and 묶음 (hosting only), 로그인,
// 질문 설정 (with 최근 거절) and 다시 알림 — beside the open item's detail,
// #settings/<kind>:<id>/<sub>. The old view links (#settings-roles …) still
// work: they unfold that group and bring it into view.
const setViewHash={qsettings:'settings',roles:'settings-roles',nodes:'settings-nodes',groups:'settings-groups',logins:'settings-logins',reminders:'settings-reminders'};
const setKindView={qs:'qsettings',refusals:'qsettings',role:'roles',node:'nodes',group:'groups',login:'logins',server:'reminders'};
let setView='qsettings',setJump=false,setData=null;
const setNotes=new Map(),setForms=new Map();
function showSettingsView(v){
  if(!setViewHash[v]||((v==='nodes'||v==='groups')&&!hostingOn))v='qsettings';
  setView=v;setJump=true;lvFold.set('set-'+v,false);
  if(setData)drawSettings();
}
function settingsViewOfHash(h){for(const [v,x] of Object.entries(setViewHash))if(x===h)return v;return ''}
// loadSettingsView reads every group at once; a group whose read fails
// shows its error, the others still draw.
async function loadSettingsView(){
  if(!setData)$('settingsNote').textContent='불러오는 중…';
  const host=hostingOn,get=p=>p.catch(e=>({err:e.message}));
  const [qs,ref,roles,logins,nodes,groups,srv]=await Promise.all([get(api('question-settings')),get(api('question-refusals')),get(api('roles')),get(api('logins')),host?get(api('nodes')):null,host?get(api('terminals/work-groups')):null,get(api('settings'))]);
  if(host!==hostingOn)return loadSettingsView();
  setData={qs:qs,ref:ref,roles:roles,logins:logins,nodes:nodes,groups:groups,srv:srv};
  $('settingsNote').textContent='';
  drawSettings();
}
tabLoaders.settings=loadSettingsView;
$('setFind').oninput=()=>lvFind($('panel-settings'),$('setFind').value);
// setGroup draws one group: rows is a list, {err} or null (not shown).
function setGroup(box,v,name,rows,each,empty,add){
  if(rows===null)return null;
  const ok=Array.isArray(rows),g=lvGroup(box,'set-'+v,name,ok?rows.length:'',{add:add});
  if(!ok){lvEmpty(g,rows.err);return g}
  for(const x of rows)g.append(each(x));
  if(!rows.length&&empty)lvEmpty(g,empty);
  return g;
}
function setAddButton(title,run){
  const b=node('button','+','fr-ib');b.type='button';b.title=title;b.setAttribute('aria-label',title);b.onclick=run;return b;
}
function openSetAdd(){setMsg($('setAddMsg'),'');$('setAdd').showModal();$('setTarget').focus()}
const permName={view:'보기',type:'입력',files:'파일',sessions:'세션',discard:'버리기'};
function drawSettings(){
  const d=setData,box=$('settingsList');
  keepRowFocus(box,()=>{
    box.replaceChildren();
    setGroup(box,'roles','역할',d.roles,v=>lvRow('settings','role:'+v.role,[v.last_machine||'',v.last_used_at?'최근 '+shortWhen(v.last_used_at):''],v.role,v.revoked_at||v.retired_at?roleBadge(v):null),'역할 없음');
    setGroup(box,'nodes','기계',d.nodes,v=>lvRow('settings','node:'+v.name,[v.machine||'',v.os||'',v.version||''],v.name,v.revoked_at?['끊김','danger']:null),'기계 없음');
    setGroup(box,'groups','묶음',d.groups,g=>lvRow('settings','group:'+g.name,[g.threshold===null||g.threshold===undefined?'문턱 없음':'문턱 '+g.threshold+'%','세션 '+g.sessions],g.name),'묶음 없음',setAddButton('묶음 추가',()=>openDetail('settings','group:+','edit')));
    setGroup(box,'logins','로그인',d.logins,v=>lvRow('settings','login:'+v.name,[v.kind==='builtin'?'내장':'외부',v.perms.length?v.perms.map(p=>permName[p]||p).join(' · '):'권한 없음'],v.name,v.disabled?['꺼짐','danger']:null),'로그인 없음');
    const q=setGroup(box,'qsettings','질문 설정',d.qs,v=>lvRow('settings','qs:'+v.target,[v.can_ask?'질문 가능':'질문 불가',v.discord?'디스코드':'',v.watchers.length?'지켜봄 '+v.watchers.length:''],v.target),'설정 없음',setAddButton('질문 설정 추가',openSetAdd));
    if(q&&Array.isArray(d.ref))q.append(lvRow('settings','refusals:24h',['24시간'],'최근 거절',d.ref.length?[d.ref.length+'건','warning']:null));
    setGroup(box,'reminders','다시 알림',d.srv&&!d.srv.err?[d.srv]:d.srv,s=>lvRow('settings','server:now',[s.reminders.on?'켬':'끔','판 '+s.version],'다시 알림 · 서버'),'');
  });
  lvFind($('panel-settings'),$('setFind').value);
  if(setJump){
    setJump=false;
    const g=box.querySelector('[data-grp="set-'+setView+'"]');
    if(g&&g.previousElementSibling&&g.previousElementSibling.scrollIntoView)g.previousElementSibling.scrollIntoView({block:'nearest'});
  }
}
// setNote keeps a result line for an item's detail across the redraws of
// the next few seconds (a save also triggers a live redraw).
function setNote(id,text,kind){setNotes.set(id,{text:text,kind:kind,at:Date.now()})}
function drawSetNote(box,id){
  const n=setNotes.get(id),m=node('div','','msg');m.setAttribute('role','status');
  if(n&&Date.now()-n.at<10000)setMsg(m,n.text,n.kind);
  box.append(m);
}
function setButton(text,cls,run,title){const b=node('button',text,cls);b.type='button';b.title=title||text;b.onclick=run;return b}
// 최근 거절 (#286): the roles refused for can_ask in the last 24 h.
const refCols=['역할','맞은 대상','대신 물을 역할','횟수','마지막 거절'];
function drawRefusals(x,box){
  const t=node('table','','stack list cards'),head=node('tr'),th=document.createElement('thead'),body=document.createElement('tbody');
  for(const c of refCols)head.append(node('th',c,c==='횟수'?'num':''));
  th.append(head);t.append(th,body);
  for(const v of x.rows){
    const tr=node('tr','');
    const at=cell(shortWhen(v.at),refCols[4],'nowrap');at.title=when(v.at);
    tr.append(cell(v.role,'','strong mono'),cell(v.target==='none'?'없음':v.target,refCols[1],'mono'),cell(v.ask_via||'-',refCols[2],'mono'),cell(String(v.count),refCols[3],'num'),at);
    body.append(tr);
  }
  if(!x.rows.length)emptyRow(body,refCols.length,'거절 없음');
  box.append(t);
}
// qsForm is the edit form of one target. It is kept across live redraws
// while the stored setting is unchanged, so a half-made edit survives them.
function qsForm(v){
  const kept=setForms.get(v.target);
  if(kept&&kept.stamp===v.updated_at)return kept.form;
  const f=node('form','','settings-form termform');
  const check=(text,on)=>{const l=node('label','','check'),i=document.createElement('input');i.type='checkbox';i.checked=on;i.title=text;l.append(i,' '+text);return [l,i]};
  const [l1,canAsk]=check('질문할 수 있음 (can_ask)',v.can_ask),[l2,discord]=check('디스코드 알림 (discord)',v.discord);
  const l3=node('label','지켜보는 역할 (쉼표로 구분, 최대 20) '),w=document.createElement('input');
  w.value=v.watchers.join(', ');w.autocomplete='off';w.spellcheck=false;w.setAttribute('autocapitalize','off');w.placeholder='team/boss, ops/lead';w.title='지켜보는 역할 (쉼표로 구분, 최대 20)';l3.append(w);
  const save=node('button','저장');save.type='submit';save.title='저장';
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
    loadSettingsView();drawDetail();
  };
  setForms.set(v.target,{stamp:v.updated_at,form:f});
  return f;
}
function drawQS(v,box){
  if(v.target!=='default'){
    const acts=node('div','','detail-acts');
    acts.append(setButton('삭제','',async()=>{
      if(!confirm(v.target+' 설정을 지울까요?'))return;
      try{await apiSend('DELETE','question-settings/'+targetPath(v.target));setForms.delete(v.target);setMsg($('settingsMsg'),v.target+' 설정을 지웠습니다.','ok');closeDetail();loadSettingsView()}
      catch(e){setNote('qs:'+v.target,e.message,'err');drawDetail()}
    }));
    box.append(acts);
  }
  drawSetNote(box,'qs:'+v.target);
  box.append(dl([['대상',v.target],['바뀐 때',when(v.updated_at)]]),qsForm(v));
}
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
    setMsg($('settingsMsg'),target+' 설정을 저장했습니다.','ok');loadSettingsView();
    openDetail('settings','qs:'+target,'edit');
  }catch(err){setMsg($('setAddMsg'),err.message,'err')}
};
$('setClear').onclick=()=>{$('settingsForm').reset();setMsg($('setAddMsg'),'')};
function roleBadge(v){return v.retired_at?['은퇴','neutral']:v.revoked_at?['끊김','danger']:['사용 중','success']}
// roleToken is the token a rotate just made (#499): shown in that role's
// detail until the detail is left, then gone for good.
let roleToken=null;
addEventListener('hashchange',()=>{if(roleToken&&!decodeURIComponent(location.hash).includes(roleToken.id))roleToken=null});
// roleSessions counts the sessions on role now: the role and its numbered
// slots (<role>-N), online.
async function roleSessions(role){
  const p=await api('presence');
  return p.sessions.filter(s=>s.online&&(s.role===role||(s.role.startsWith(role+'-')&&/^[1-9][0-9]{0,2}$/.test(s.role.slice(role.length+1))))).length;
}
function drawRoleToken(box,t){
  const row=node('div','','detail-acts'),field=document.createElement('input');
  field.type='text';field.readOnly=true;field.value=t.token;field.className='mono';field.setAttribute('aria-label','새 토큰');field.title='새 토큰 — 이 화면을 떠나면 다시 볼 수 없음';
  field.style.cssText='flex:1 1 16em;min-width:0';
  const copy=setButton('복사','',async()=>{
    try{await navigator.clipboard.writeText(t.token)}
    catch(e){field.select();document.execCommand('copy')}
    copy.textContent='복사됨';
  });
  row.append(field,copy);box.append(row);
}
function drawRole(v,box){
  const id='role:'+v.role,revoke=!v.revoked_at,acts=node('div','','detail-acts');
  if(!v.retired_at){
    acts.append(setButton(revoke?'끊기':'되살리기','',async()=>{
      const used=revoke&&v.last_used_at&&Date.now()-new Date(v.last_used_at).getTime()<864e5;
      if(!confirm(revoke?v.role+' 역할을 끊을까요? 이 토큰으로 접속한 세션이 모두 끊깁니다.'+(used?'\n최근 사용: '+when(v.last_used_at)+(v.last_machine?' · '+v.last_machine:''):''):v.role+' 역할을 되살릴까요?'))return;
      try{await apiSend('POST','roles/'+targetPath(v.role)+(revoke?'/revoke?force=1':'/restore'));setNote(id,v.role+(revoke?' 역할을 끊었습니다.':' 역할을 되살렸습니다.'),'ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    }));
    acts.append(setButton('토큰 바꾸기','',async()=>{
      if(!confirm(v.role+' 토큰을 바꿀까요? 지금 토큰은 바로 못 쓰게 됩니다.'))return;
      try{const r=await apiSend('POST','roles/'+targetPath(v.role)+'/rotate',{generate:true});roleToken={id:id,token:r.token};setNote(id,v.role+' 토큰을 바꿨습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    }));
    const retire=setButton('은퇴','danger',async()=>{
      let n='?';try{n=String(await roleSessions(v.role))}catch(e){}
      if(!confirm(v.role+' 역할을 은퇴할까요? 되돌릴 수 없습니다. 같은 이름으로 다시 등록할 수 없습니다.\n지금 붙어 있는 세션: '+n))return;
      try{await apiSend('POST','roles/'+targetPath(v.role)+'/retire?force=1');roleToken=null;setNote(id,v.role+' 역할을 은퇴했습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    });
    retire.style.cssText='background:var(--st-error);border-color:var(--st-error);color:var(--color-on-accent)';
    acts.append(retire);
    box.append(acts);
  }
  drawSetNote(box,id);
  if(roleToken&&roleToken.id===id&&!v.retired_at)drawRoleToken(box,roleToken);
  const rows=[['역할',v.role],['등록',when(v.created_at)],['바꾼 때',v.rotated_at?when(v.rotated_at):'-'],['끊은 때',v.revoked_at?when(v.revoked_at):'-']];
  if(v.retired_at)rows.push(['은퇴한 때',when(v.retired_at)]);
  box.append(dl(rows));
}
function drawNode(v,box){
  const id='node:'+v.name;
  if(!v.revoked_at){
    const acts=node('div','','detail-acts');
    acts.append(setButton('끊기','',async()=>{
      if(!confirm(v.name+' 노드를 끊을까요? 연결이 바로 끊기고 이 노드의 토큰은 다시 쓸 수 없습니다.'))return;
      try{await apiSend('DELETE','nodes/'+encodeURIComponent(v.name));setNote(id,v.name+' 노드를 끊었습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
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
    inp.value=a.manual===null||a.manual===undefined?'':String(a.manual);inp.setAttribute('aria-label',name+' 수동 %');inp.title=name+' 수동 %';
    const send=async remaining=>{
      try{await apiSend('PUT','terminals/limit/accounts',{node:v.name,adapter:a.adapter,account:a.account||'',remaining:remaining});setNote(id,name+(remaining===null?' 수동 % 지움':' 수동 % 저장'),'ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
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
// 로그인 (#476): read only; the permissions change with `admin allow` only.
function drawLogin(v,box){
  const perms=node('span',v.perms.length?v.perms.map(p=>permName[p]?permName[p]+' ('+p+')':p).join(', '):'없음');
  perms.title='바꾸기: switchboard admin allow';
  box.append(dl([['이름',v.name],['종류',v.kind==='builtin'?'내장 로그인':'외부 로그인'],['권한',perms],[v.kind==='builtin'?'등록':'권한 바뀐 때',v.created_at?when(v.created_at):'-'],['마지막 로그인',v.last_login?when(v.last_login):'-'],['상태',v.disabled?'꺼짐':'켬']]));
}
// 묶음 (#435): a work group's threshold, or a new group (id group:+). The
// form is kept across live redraws while the stored group is unchanged.
function thresholdOf(input){const t=input.value.trim();return t===''?null:Number(t)}
function groupForm(g){
  const key='group:'+(g.fresh?'+':g.name),kept=setForms.get(key);
  if(kept&&kept.stamp===(g.updated_at||''))return kept.form;
  const f=node('form','','fld');
  const name=document.createElement('input');name.required=true;name.maxLength=64;name.autocomplete='off';name.spellcheck=false;name.setAttribute('autocapitalize','off');
  name.value=g.name;name.disabled=!g.fresh;name.title='묶음 이름';name.setAttribute('aria-label','이름');
  const th=document.createElement('input');th.type='number';th.min='0';th.max='100';th.step='1';th.inputMode='numeric';
  th.value=g.threshold===null||g.threshold===undefined?'':String(g.threshold);
  th.title='남은 한도가 이 값(%)보다 작으면 새 일을 받지 않음. 비우면 문턱 없음';th.setAttribute('aria-label','문턱 %');
  const save=node('button',g.fresh?'추가':'저장');save.type='submit';save.title=g.fresh?'묶음 추가':'문턱 저장';
  f.append(node('label','이름'),name,node('label','문턱 %'),th,node('span'),save);
  f.onsubmit=async e=>{
    e.preventDefault();
    try{
      if(g.fresh){
        const n=name.value.trim();
        await apiSend('POST','terminals/work-groups',{name:n,threshold:thresholdOf(th)});
        setForms.delete(key);setNote('group:'+n,n+' 묶음 추가','ok');loadSettingsView();openDetail('settings','group:'+n,'edit');return;
      }
      await apiSend('PUT','terminals/work-groups/'+encodeURIComponent(g.name),{threshold:thresholdOf(th)});
      setForms.delete(key);setNote(key,g.name+' 문턱 저장','ok');
    }catch(err){setNote(key,err.message,'err')}
    loadSettingsView();drawDetail();
  };
  setForms.set(key,{stamp:g.updated_at||'',form:f});
  return f;
}
function drawWorkGroup(g,box){
  const id='group:'+(g.fresh?'+':g.name);
  if(!g.fresh){
    const acts=node('div','','detail-acts');
    const rm=setButton('지우기','',async()=>{
      if(!confirm(g.name+' 묶음을 지울까요?'))return;
      try{await apiSend('DELETE','terminals/work-groups/'+encodeURIComponent(g.name));setForms.delete(id);setMsg($('settingsMsg'),g.name+' 묶음을 지웠습니다.','ok');closeDetail()}
      catch(e){setNote(id,e.message,'err');drawDetail()}
      loadSettingsView();
    },'이 묶음을 지운다');
    rm.disabled=g.sessions>0;if(g.sessions>0)rm.title='세션이 있는 묶음은 지울 수 없음';
    acts.append(rm);box.append(acts);
  }
  drawSetNote(box,id);
  box.append(groupForm(g));
  if(!g.fresh)box.append(dl([['세션',String(g.sessions)],['바뀐 때',when(g.updated_at)]]));
}
// 다시 알림 (#223): the reminder settings and the server version, as the
// start-up line reports them.
function drawServer(s,box){
  const r=s.reminders,st=document.createElement('span');
  st.append(badge(r.on?'켬':'끔',r.on?'success':'neutral'));
  box.append(dl([['다시 알림',st],['첫 알림',r.after],['최대 횟수',String(r.max)],['멈춤 문턱',r.idle==='off'?'끔':r.idle],['멈춤 알림',r.idle==='off'?'-':String(r.idle_max||1)+'번'],['켠 때',r.since?when(r.since):r.on?'첫 점검 전':'-'],['서버 판',s.version],['터미널 호스팅',s.hosting?'켬':'끔'],['답 규칙 판',String(s.answered_rule.version)],['마지막 이주',ruleLast(s.answered_rule.last)]]));
}
async function findSetting(kind,key){
  if(kind==='qs')return (await api('question-settings')).find(x=>x.target===key);
  if(kind==='refusals')return {rows:await api('question-refusals')};
  if(kind==='role')return (await api('roles')).find(x=>x.role===key);
  if(kind==='login')return (await api('logins')).find(x=>x.name===key);
  if(kind==='node'&&hostingOn)return (await api('nodes')).find(x=>x.name===key);
  if(kind==='group'&&hostingOn)return key==='+'?{name:'',fresh:true}:(await api('terminals/work-groups')).find(x=>x.name===key);
  if(kind==='server')return api('settings');
  return undefined;
}
const setHeads={
  qs:v=>({title:v.target,badge:v.can_ask?['질문 가능','success']:['질문 불가','neutral']}),
  refusals:v=>({title:'최근 거절',badge:[v.rows.length+'건',v.rows.length?'warning':'neutral']}),
  role:v=>({title:v.role,badge:roleBadge(v)}),
  login:v=>({title:v.name,badge:v.disabled?['꺼짐','danger']:[v.kind==='builtin'?'내장':'외부','neutral']}),
  node:v=>({title:v.name,badge:v.revoked_at?['끊김','danger']:null}),
  group:v=>({title:v.fresh?'새 묶음':v.name,badge:null}),
  server:v=>({title:'다시 알림 · 서버',badge:v.reminders.on?['켬','success']:['끔','neutral']}),
};
const setDraws={qs:drawQS,refusals:drawRefusals,role:drawRole,login:drawLogin,node:drawNode,group:drawWorkGroup,server:drawServer};
registerDetail('settings',{
  list:'설정',kinds:['settings','terminals'],
  back:id=>setViewHash[setKindView[id.slice(0,id.indexOf(':'))]]||'settings',
  load:async id=>{
    const i=id.indexOf(':'),k=id.slice(0,i),v=i>0&&setHeads[k]?await findSetting(k,id.slice(i+1)):undefined;
    return v?{k:k,v:v}:null;
  },
  head:x=>setHeads[x.k](x.v),
  subs:x=>x.k==='qs'||x.k==='group'?[['edit','설정']]:[['info','정보']],
  draw:(x,sub,box)=>setDraws[x.k](x.v,box)
});
