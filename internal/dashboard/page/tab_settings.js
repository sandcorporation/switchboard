// ---- 설정 (#515, #538, #544) ----
// 설정 is a full screen (html.fr-set) beside the session list. The top
// (#setTop) holds 설정, 설정 전체에서 찾기 and the picked tab's add button
// (+ 역할 …), then the tab row, each tab with its count. Under it the picked
// category's table takes the width (#setItems); a row opens its item in the
// panel on the right (#detail, 380px; the whole screen below 720px) and the
// table stays beside it, its scroll and marked row kept. 화면 picks
// 세션 표 · 터미널 · 노드 with small buttons (#setScr), the picked one
// under the tab row (진행 is a menu row since #576). Addresses:
// #settings-<tab> picks a tab (#settings the last one),
// #settings/<kind>:<id>/<sub> an item, #sessions #terminals and
// #settings/view:nodes the 화면.
const setViewHash={roles:'settings-roles',nodes:'settings-nodes',groups:'settings-groups',logins:'settings-logins',qsettings:'settings-qsettings',watches:'settings-watches',hooks:'settings-hooks',reminders:'settings-reminders'};
const setKindView={qs:'qsettings',refusals:'qsettings',role:'roles',node:'nodes',group:'groups',login:'logins',watch:'watches',hook:'hooks',server:'reminders'};
let setView='roles',setData=null;
const setNotes=new Map(),setForms=new Map();
// setCats are the tabs, in order: add the new item's id (+ opens its blank
// form in the panel; 역할's makes an invite code), host only with hosting
// on. 화면 (screens) has buttons instead of a table.
const setCats=[
  {v:'roles',name:'역할',add:'role:+',addPerm:'sessions'},
  {v:'nodes',name:'기계',host:true},
  {v:'groups',name:'묶음',host:true,add:'group:+'},
  {v:'logins',name:'로그인'},
  {v:'qsettings',name:'질문 설정',add:'qs:+'},
  {v:'watches',name:'지켜보기',add:'watch:+',addPerm:'sessions'},
  {v:'hooks',name:'웹훅',add:'hook:+'},
  {v:'screens',name:'화면'},
  {v:'reminders',name:'다시 알림'},
];
// setCols are each table's columns, [head, narrow, class]: a narrow
// column hides below 720px.
const setCols={
  roles:[['이름'],['주소',0,'mono st-dim'],['기계',1],['상태'],['컨텍스트',1,'num st-dim']],
  nodes:[['이름'],['기계'],['OS',1],['판',1,'st-dim'],['상태']],
  groups:[['이름'],['문턱'],['세션',0,'num']],
  logins:[['이름'],['종류'],['권한',1],['상태']],
  qsettings:[['대상',0,'mono'],['질문'],['디스코드',1],['지켜봄',1,'num']],
  watches:[['이름'],['받는 역할',0,'mono st-dim'],['주기',1],['최근',1,'st-dim'],['상태']],
  hooks:[['이름'],['방식'],['받는 역할',1,'mono st-dim'],['최근',1,'st-dim'],['상태']],
  reminders:[['종류'],['받는 쪽',1,'st-dim'],['기한'],['넘긴 것']],
};
function setCatOn(c){return !c.host||hostingOn}
function setAddOk(c){return !!c.add&&setCatOn(c)&&(!c.addPerm||can(c.addPerm))}
function setCatOf(v){return setCats.find(c=>c.v===v)}
// showSettingsView(v) picks tab v.
function showSettingsView(v){
  if(!setViewHash[v]||((v==='nodes'||v==='groups')&&!hostingOn))v='roles';
  setView=v;
}
function settingsViewOfHash(h){if(h==='settings')return setView;for(const [v,x] of Object.entries(setViewHash))if(x===h)return v;return ''}
// setTabHref is the address tab v opens: 화면 opens its first screen.
function setTabHref(v){return v==='screens'?setScreens()[0][2]:'#'+setViewHash[v]}
// loadSettingsView reads every category at once; a category whose read
// fails shows its error, the others still draw.
async function loadSettingsView(){
  if(!setData)$('settingsNote').textContent='불러오는 중…';
  const host=hostingOn,get=p=>p.catch(e=>({err:e.message}));
  const [qs,ref,roles,logins,nodes,groups,srv,watches,hooks]=await Promise.all([get(api('question-settings')),get(api('question-refusals')),get(api('roles')),get(api('logins')),host?get(api('nodes')):null,host?get(api('terminals/work-groups')):null,get(api('settings')),can('view')?get(api('watches')):[],get(api('hooks'))]);
  if(host!==hostingOn)return loadSettingsView();
  setData={qs:qs,ref:ref,roles:roles,logins:logins,nodes:nodes,groups:groups,srv:srv,watches:watches,hooks:hooks};
  $('settingsNote').textContent='';
  drawSettings();
}
tabLoaders.settings=loadSettingsView;
// 설정 전체에서 찾기 counts the matches of every tab and opens the first tab
// that has one.
$('setFind').oninput=()=>{
  if(!setData)return;
  const q=$('setFind').value;
  if(q.trim()){
    const c=setCats.find(c=>{const n=setCount(c,q);return typeof n==='number'&&n>0});
    if(c&&c.v!==setTabNow()){location.hash=setTabHref(c.v);return}
  }
  drawSettings();
};
// setRow is one table row: cells are text or [text,tone], the first the
// item's name. A click opens the item in the panel (setOpen).
function setRow(id,cells){
  const tr=document.createElement('tr');
  cells.forEach((c,i)=>{
    const [t,tone]=Array.isArray(c)?c:[c,''];
    const td=node('td',t===undefined||t===null?'':String(t),i?'':'st-nm');
    if(tone)td.classList.add('tone-'+tone);
    tr.append(td);
  });
  tr.title=cells.map(c=>Array.isArray(c)?c[0]:c).filter(Boolean).join(' · ');
  tr.dataset.detailId=id;tr.tabIndex=0;
  tr.addEventListener('click',e=>{if(!e.target.closest('a,button,input'))setOpen(id)});
  tr.addEventListener('keydown',e=>{if(e.target===tr&&(e.key==='Enter'||e.key===' ')){e.preventDefault();setOpen(id)}});
  return tr;
}
// setDirty is the id of the open item whose form has values not saved;
// opening another item asks first whether to drop them.
let setDirty='';
// Leaving a form with unsaved values asks at the top of its body (#581).
async function setOpen(id){
  const c=detailCur;
  if(c&&c.tab==='settings'&&setDirty===c.id&&c.id!==id){
    if($('detailBody').querySelector(':scope>.ask'))return;
    if(!await askOn($('detailBody'),{top:true,text:'저장하지 않은 값이 있습니다. 버리고 옮길까요?',ok:'버리고 옮김'}))return;
    setForms.clear();
  }
  setDirty='';openDetail('settings',id);
}
for(const ev of ['input','change'])$('detailBody').addEventListener(ev,e=>{if(detailCur&&detailCur.tab==='settings'&&e.target.closest('form'))setDirty=detailCur.id});
$('detailBody').addEventListener('submit',()=>{setDirty=''},true);
const permName={view:'보기',type:'입력',files:'파일',sessions:'세션',discard:'버리기'};
// setLive is the session list's rows (frameData) found by role: get(role)
// is the session of that role and of its slots (team/coordinator-1,
// team/worker-1-2 under team/coordinator, team/worker), the busiest one
// when several run (setBusy order) — the same row the session list shows.
const setBusy=['busy','compacting','blocked','error','idle','unknown','offline'];
function setBusyRank(st){const k=/^(error|blocked):/.exec(st||'');const i=setBusy.indexOf(k?k[1]:st);return i<0?setBusy.length:i}
function setLive(){
  const rows=typeof frameData!=='undefined'&&frameData?frameRows(frameData):[];
  const names=new Set(typeof setData!=='undefined'&&setData&&Array.isArray(setData.roles)?setData.roles.map(v=>v.role):[]);
  // setOwner is the one registered role a session address belongs to: the
  // address itself, else its longest registered base with a -N tail.
  const owner=addr=>{
    if(names.has(addr))return addr;
    let best='';
    for(const n of names)if(n.length>best.length&&addr.startsWith(n+'-')&&/^(-\d+)+$/.test(addr.slice(n.length)))best=n;
    return best;
  };
  const by=new Map();
  for(const r of rows){
    const o=owner(r.role)||r.role,cur=by.get(o);
    if(!cur||setBusyRank(r.st)<setBusyRank(cur.st))by.set(o,r);
  }
  return {get:role=>by.get(role)||null};
}
const setStTone={'st-busy':'success','st-compacting':'success','st-blocked':'warning','st-error':'danger'};
function setRoleName(v,s){return s&&s.label?s.label:v.role.slice(v.role.indexOf('/')+1)}
// setRoleState is a role's state in the table and the detail badge: its
// registration when ended (끊김·은퇴), else its session's state, else 꺼짐.
function setRoleState(v,s){
  if(v.revoked_at||v.retired_at)return roleBadge(v);
  if(s&&s.st!=='offline'){const [t,c]=s.st==='unknown'?['모름','']:sessionStatus(s.st);return [t,setStTone[c]||'']}
  return ['꺼짐',''];
}
function setRoleRow(v,live){
  const s=live.get(v.role);
  return setRow('role:'+v.role,[setRoleName(v,s),v.role,(s&&s.machine)||v.last_machine||'',setRoleState(v,s),s&&s.ctx!==null&&s.ctx!==undefined?kTokens(s.ctx):'-']);
}
// setLiveBadge re-badges an open role's detail from the session list
// without redrawing its form (a live update must not drop typed values).
function setLiveBadge(){
  const c=detailCur;if(!c||c.tab!=='settings'||!c.id.startsWith('role:')||!setData||!Array.isArray(setData.roles))return;
  const v=setData.roles.find(r=>'role:'+r.role===c.id);if(!v)return;
  const t=setRoleState(v,setLive().get(v.role)),b=$('detailBadge');
  b.textContent=t[0];b.className='sc-badge '+(toneClass[t[1]]||toneClass.neutral);
}
// setCatRows is category v's table rows: a list, {err} or null (not shown).
function setCatRows(v){
  const d=setData;if(!d)return [];
  const live=v==='roles'?setLive():null;
  const each={
    roles:[d.roles,v=>setRoleRow(v,live)],
    nodes:[d.nodes,v=>setRow('node:'+v.name,[v.name,v.machine||'',v.os||'',v.version||'',v.revoked_at?['끊김','danger']:['사용 중','success']])],
    groups:[d.groups,g=>setRow('group:'+g.name,[g.name,g.threshold===null||g.threshold===undefined?'없음':g.threshold+'%',String(g.sessions)])],
    logins:[d.logins,v=>setRow('login:'+v.name,[v.name,v.kind==='builtin'?'내장':'외부',v.perms.length?v.perms.map(p=>permName[p]||p).join(' · '):'권한 없음',v.disabled?['꺼짐','danger']:['켬','success']])],
    qsettings:[d.qs,v=>setRow('qs:'+v.target,[v.target,v.can_ask?['가능','success']:'불가',v.discord?'켬':'끔',String(v.watchers.length)])],
    watches:[d.watches,watchRow],
    hooks:[d.hooks,hookRow],
    reminders:[d.srv&&!d.srv.err?[...(d.srv.remind||[]).map(k=>({k:k})),{srv:d.srv}]:d.srv,s=>s.k?remindRow(s.k):setRow('server:now',['서버','','판 '+s.srv.version,''])],
  }[v];
  if(!each||each[0]===null)return null;
  if(!Array.isArray(each[0]))return each[0];
  const rows=each[0].map(each[1]);
  if(v==='qsettings'&&Array.isArray(d.ref))rows.push(setRow('refusals:24h',['최근 거절',d.ref.length?[d.ref.length+'건','warning']:'0건','24시간','']));
  return rows;
}
// setCount is tab c's count under 찾기 q: a number, '!' (its read
// failed) or null (the tab is not shown).
function setCount(c,q){
  if(!setCatOn(c))return null;
  if(c.v==='screens')return setScreens().filter(s=>listMatch(q,s[1])).length;
  const rows=setCatRows(c.v);
  if(rows===null)return null;
  if(!Array.isArray(rows))return '!';
  return rows.filter(r=>!r.dataset.detailId.startsWith('refusals:')&&listMatch(q,r.textContent)).length;
}
// setScreens are 화면's buttons: 세션 표·터미널 open their own page (the
// address stays #sessions …), 노드 the workspace's node index.
function setScreens(){
  const s=[['sessions','세션 표','#sessions']];
  if(hostingOn)s.push(['terminals','터미널','#terminals']);
  if(ws&&ws.nodes)s.push(['view:nodes','노드',detailHash('settings','view:nodes','')]);
  return s;
}
// setTabNow is the tab on screen: the open item's category, 화면 for
// 세션 표·터미널 and 노드, else the picked tab.
function setTabNow(){
  const c=detailCur;
  if(c&&c.tab==='settings'){const k=c.id.slice(0,c.id.indexOf(':'));return k==='view'?'screens':setKindView[k]||setView}
  if(setTabs.includes(curTab)||(c&&setTabs.includes(c.tab)))return 'screens';
  return setView;
}
// setScrNow is the 화면 button on: sessions, terminals or view:nodes.
function setScrNow(){const c=detailCur;if(c&&c.tab==='settings'&&c.id.startsWith('view:'))return c.id;return setTabs.includes(curTab)?curTab:''}
// drawSettings draws the top (tabs with their counts, + 추가, 화면's
// buttons) and the picked tab's table. A redraw keeps the table's scroll,
// its marked row and the focused row; another tab starts at the top.
let setDrawn='';
function drawSettings(){
  if(!setData)return;
  const q=$('setFind').value,now=setTabNow(),tabs=[],bar=$('setTabsRow');
  const focus=document.activeElement&&bar.contains(document.activeElement)?document.activeElement.dataset.setTab:'';
  for(const c of setCats){
    const n=setCount(c,q);
    if(n===null)continue;
    const a=node('a','','st-tab');a.href=setTabHref(c.v);a.dataset.setTab=c.v;
    a.title=c.name+(n==='!'?' — 불러오지 못함':'');
    a.append(node('span',c.name),node('span',String(n),'st-c'+(n==='!'?' tone-danger':'')));
    if(c.v===now){a.classList.add('on');a.setAttribute('aria-current','page')}
    a.addEventListener('click',()=>{if(location.hash===a.getAttribute('href'))route()});
    tabs.push(a);
  }
  bar.replaceChildren(...tabs);
  for(const a of tabs){
    if(a.dataset.setTab===focus)a.focus({preventScroll:true});
    if(a.dataset.setTab===now&&setDrawn!==now&&a.scrollIntoView)a.scrollIntoView({block:'nearest',inline:'nearest'});
  }
  const cat=setCatOf(now),add=$('setAddBtn');
  add.hidden=!cat||!setAddOk(cat);
  if(!add.hidden){add.textContent='+ '+cat.name;add.title=cat.v==='roles'?'초대 코드를 만들어 역할을 더한다':cat.name+'을(를) 더한다';add.onclick=()=>setOpen(cat.add)}
  const scr=$('setScr');scr.hidden=now!=='screens';
  if(now==='screens'){
    const on=setScrNow();
    scr.replaceChildren(...setScreens().filter(s=>listMatch(q,s[1])).map(([k,name,href])=>{
      const a=node('a',name,'st-sb');a.href=href;a.title=name+' 화면을 연다';
      if(k===on){a.classList.add('on');a.setAttribute('aria-current','page')}
      return a;
    }));
  }
  const same=setDrawn===now;setDrawn=now;
  if(now==='screens')return;
  const rows=setCatRows(now),box=$('setItems'),page=document.querySelector('.sc-app-page'),top=page?page.scrollTop:0;
  if(rows===null){box.replaceChildren(node('p','없음','empty'));return}
  if(!Array.isArray(rows)){box.replaceChildren(node('p',rows.err,'msg err'));return}
  const cols=setCols[now]||[],shown=rows.filter(r=>listMatch(q,r.textContent));
  const t=node('table','','st-tbl'),th=document.createElement('thead'),hr=document.createElement('tr'),body=document.createElement('tbody');
  const cls=i=>[cols[i]&&cols[i][1]?'st-n':'',cols[i]&&cols[i][2]||''].filter(Boolean).join(' ');
  cols.forEach(([h],i)=>hr.append(node('th',h,cls(i))));
  for(const r of shown)[...r.children].forEach((td,i)=>{const k=cls(i);if(k)td.className=(td.className+' '+k).trim()});
  th.append(hr);t.append(th,body);
  keepRowFocus(box,()=>{body.append(...shown);box.replaceChildren(t);if(!shown.length)box.append(node('p',q.trim()?'찾은 것 없음':cat.name+' 없음','empty'))});
  if(page)page.scrollTop=same?top:0;
  markDetailRows();
}
// setChrome fits the screen to what is open: an item of 설정 in the panel
// beside the table, 노드 and the items of 세션 표·터미널 over the
// whole width.
function setChrome(){
  const on=document.documentElement.classList.contains('fr-set'),c=detailCur;
  $('appMain').classList.toggle('st-wide',on&&!!c&&(c.tab!=='settings'||c.id.startsWith('view:')));
  $('detail').classList.toggle('sd-form',on&&!!c&&c.tab==='settings'&&!c.id.startsWith('view:'));
  if(on&&setData)drawSettings();
}
// setFoot gathers the item's actions into the panel's foot: the risky ones (끊기,
// 은퇴, 삭제 …) left, the form's 저장 right. Section heads go over the
// facts (정보) and the form (설정).
let setFormSeq=0;
function setFoot(box){
  const l=node('div','','sd-fl'),r=node('div','','sd-fr');
  for(const a of box.querySelectorAll(':scope>.detail-acts'))if([...a.children].every(b=>b.tagName==='BUTTON')){l.append(...a.childNodes);a.remove()}
  const f=box.querySelector(':scope>form');
  if(f){
    if(!f.sdFoot)f.sdFoot=[...f.querySelectorAll('button[type=submit]')];
    if(!f.id)f.id='sdForm'+(++setFormSeq);
    for(const b of f.sdFoot){b.setAttribute('form',f.id);r.append(b)}
    for(const row of f.querySelectorAll(':scope>.row'))if(!row.children.length)row.remove();
    f.before(node('h3','설정','dsec'));
  }
  const d=box.querySelector(':scope>dl');if(d)d.before(node('h3','정보','dsec'));
  if(l.children.length||r.children.length){const foot=node('div','','sd-foot');foot.append(l,r);box.append(foot)}
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
// qsForm is the edit form of one target (v.fresh: a new one, with its
// 대상 field; #538 opens it in the body from 질문 설정's +). It is kept
// across live redraws while the stored setting is unchanged, so a
// half-made edit survives them.
function qsForm(v){
  const key=v.fresh?'qs:+':v.target,kept=setForms.get(key);
  if(kept&&kept.stamp===v.updated_at)return kept.form;
  const f=node('form','','settings-form termform');
  const check=(text,on,tip)=>{const l=node('label','','check'),i=document.createElement('input');i.type='checkbox';i.checked=on;i.title=tip;l.append(i,' '+text);return [l,i]};
  const [l1,canAsk]=check('질문할 수 있음 (can_ask)',v.can_ask,'이 역할이 대표에게 질문할 수 있다'),[l2,discord]=check('디스코드 알림 (discord)',v.discord,'이 역할의 질문을 디스코드로도 알린다');
  const l3=node('label','지켜보는 역할 (쉼표로 구분, 최대 20) '),w=document.createElement('input');
  w.value=v.watchers.join(', ');w.autocomplete='off';w.spellcheck=false;w.setAttribute('autocapitalize','off');w.placeholder='team/boss, ops/lead';w.title='지켜보는 역할 (쉼표로 구분, 최대 20)';l3.append(w);
  let tg=null;
  if(v.fresh){
    const l0=node('label','대상 ');tg=document.createElement('input');tg.required=true;tg.autocomplete='off';tg.spellcheck=false;tg.setAttribute('autocapitalize','off');tg.placeholder='svc/coordinator 또는 svc/*';
    tg.title='default, 역할(svc/coordinator — svc/coordinator-N 포함), 접두사(svc/*). 정확한 대상 → 가장 긴 접두사 → default 순으로 하나만 적용';
    l0.append(tg);f.append(l0);
  }
  const save=node('button','저장');save.type='submit';save.title='저장';
  const row=node('div','','row');row.append(save);
  f.append(l1,l2,l3,row);
  f.onsubmit=async e=>{
    e.preventDefault();
    const target=v.fresh?tg.value.trim():v.target,id='qs:'+(v.fresh?'+':target);
    if(!target){setNote(id,'대상을 쓰세요.','err');drawDetail();return}
    const watchers=w.value.split(',').map(x=>x.trim()).filter(Boolean);
    try{
      await apiSend('PUT','question-settings/'+targetPath(target),{can_ask:canAsk.checked,watchers:watchers,discord:discord.checked});
      setForms.delete(key);setForms.delete(target);setNote('qs:'+target,target+' 설정을 저장했습니다.','ok');
      loadSettingsView();
      if(v.fresh){openDetail('settings','qs:'+target,'edit');return}
    }catch(err){setNote(id,err.message,'err')}
    loadSettingsView();drawDetail();
  };
  setForms.set(key,{stamp:v.updated_at,form:f});
  return f;
}
function drawQS(v,box){
  if(v.fresh){drawSetNote(box,'qs:+');box.append(qsForm(v));return}
  if(v.target!=='default'){
    const acts=node('div','','detail-acts');
    acts.append(setButton('삭제','',async e=>{
      if(!await askOn(e.currentTarget,{text:v.target+' 설정을 지울까요?'}))return;
      try{await apiSend('DELETE','question-settings/'+targetPath(v.target));setForms.delete(v.target);setMsg($('settingsMsg'),v.target+' 설정을 지웠습니다.','ok');closeDetail();loadSettingsView()}
      catch(e){setNote('qs:'+v.target,e.message,'err');drawDetail()}
    }));
    box.append(acts);
  }
  drawSetNote(box,'qs:'+v.target);
  box.append(dl([['대상',v.target],['바뀐 때',when(v.updated_at)]]),qsForm(v));
}
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
// drawRoleInvite is 역할's +: a role comes in through a one-time invite
// (POST invites {prefix, role?, ttl}); its code is shown here once, until
// the screen is left.
let roleInvite=null;
addEventListener('hashchange',()=>{if(roleInvite&&!location.hash.includes('role%3A%2B')&&!location.hash.includes('role:+'))roleInvite=null});
function drawRoleInvite(box){
  const id='role:+';
  drawSetNote(box,id);
  if(roleInvite){
    box.append(dl([['접두사',roleInvite.prefix],['역할',roleInvite.role||'접두사 아래 아무 역할'],['기한',when(roleInvite.expires_at)]]));
    drawRoleToken(box,{token:roleInvite.code});
    box.lastChild.querySelector('input').title='초대 코드 — 이 화면을 떠나면 다시 볼 수 없음';
    box.append(setButton('하나 더','',()=>{roleInvite=null;drawDetail()},'다른 초대 코드를 만든다'));
    return;
  }
  const f=node('form','','settings-form termform');
  const field=(text,ph,tip)=>{const l=node('label',text+' '),i=document.createElement('input');i.autocomplete='off';i.spellcheck=false;i.setAttribute('autocapitalize','off');i.placeholder=ph;i.title=tip;l.append(i);f.append(l);return i};
  const prefix=field('접두사','team','역할 묶음의 접두사 — 초대받은 쪽이 <접두사>/… 역할을 만든다');prefix.required=true;
  const role=field('역할','team/worker','비우면 접두사 아래 아무 역할, 쓰면 그 역할 하나');
  const tl=node('label','기한 '),ttl=document.createElement('select');ttl.title='코드를 쓸 수 있는 기간';
  for(const [k,t] of [['24h','1일'],['72h','3일'],['7d','7일'],['30d','30일']])ttl.append(new Option(t,k));
  ttl.value='72h';tl.append(ttl);f.append(tl);
  const save=node('button','초대 코드 만들기');save.type='submit';save.title='한 번 쓰는 초대 코드를 만든다';
  const row=node('div','','row');row.append(save);f.append(row);
  f.onsubmit=async e=>{
    e.preventDefault();
    const body={prefix:prefix.value.trim(),ttl:ttl.value};if(role.value.trim())body.role=role.value.trim();
    try{roleInvite=await apiSend('POST','invites',body);setNote(id,(roleInvite.role||roleInvite.prefix+'/…')+' 초대 코드를 만들었습니다.','ok')}
    catch(err){setNote(id,err.message,'err')}
    drawDetail();
  };
  box.append(f);
}
function drawRole(v,box){
  if(v.fresh){drawRoleInvite(box);return}
  const id='role:'+v.role,revoke=!v.revoked_at,acts=node('div','','detail-acts');
  if(!v.retired_at){
    acts.append(setButton(revoke?'끊기':'되살리기','',async e=>{
      const used=revoke&&v.last_used_at&&Date.now()-new Date(v.last_used_at).getTime()<864e5;
      if(!await askOn(e.currentTarget,{text:revoke?v.role+' 역할을 끊을까요? 이 토큰으로 접속한 세션이 모두 끊깁니다.'+(used?' 최근 사용: '+when(v.last_used_at)+(v.last_machine?' · '+v.last_machine:''):''):v.role+' 역할을 되살릴까요?'}))return;
      try{await apiSend('POST','roles/'+targetPath(v.role)+(revoke?'/revoke?force=1':'/restore'));setNote(id,v.role+(revoke?' 역할을 끊었습니다.':' 역할을 되살렸습니다.'),'ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    }));
    acts.append(setButton('토큰 바꾸기','',async e=>{
      if(!await askOn(e.currentTarget,{text:v.role+' 토큰을 바꿀까요? 지금 토큰은 바로 못 쓰게 됩니다.'}))return;
      try{const r=await apiSend('POST','roles/'+targetPath(v.role)+'/rotate',{generate:true});roleToken={id:id,token:r.token};setNote(id,v.role+' 토큰을 바꿨습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    }));
    const retire=setButton('은퇴','danger',async e=>{
      const at=e.currentTarget;
      let n='?';try{n=String(await roleSessions(v.role))}catch(e){}
      if(!await askOn(at,{danger:true,text:v.role+' 역할을 은퇴할까요? 되돌릴 수 없습니다. 같은 이름으로 다시 등록할 수 없습니다. 지금 붙어 있는 세션: '+n}))return;
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
    acts.append(setButton('끊기','',async e=>{
      if(!await askOn(e.currentTarget,{text:v.name+' 노드를 끊을까요? 연결이 바로 끊기고 이 노드의 토큰은 다시 쓸 수 없습니다.'}))return;
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
    const rm=setButton('지우기','',async e=>{
      if(!await askOn(e.currentTarget,{text:g.name+' 묶음을 지울까요?'}))return;
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
// 다시 알림 (#223, #586): one row per reminder kind (mail, left items,
// requests) with who gets it, its time and how many are past it; the 서버
// row holds the version and the idle check.
const remindName={mail:'메일',left:'남긴 것',request:'지시'};
const remindTip={mail:'읽고 끝나지 않은 지시 메일이 이 시간 동안 답이 없으면 받은 역할에게',left:'남긴 것이 이 시간 넘게 처리되지 않으면 지시한 역할에게 묶어서',request:'진행 중 지시에 이 시간 동안 소식이 없으면 맡은 대장에게 — 대표 답 대기면 보내지 않음'};
function remindTime(k){
  if(!k.on)return '끔';
  const m=Math.round(k.after_seconds/60),h=Math.floor(m/60),r=m%60;
  return (h?h+'시간':'')+(r?(h?' ':'')+r+'분':'');
}
function remindOver(k){
  const t=k.overdue+'건'+(k.kind==='request'?' · 대표 답 대기 '+k.waiting:'');
  return [t,k.overdue||k.waiting?'warning':''];
}
function remindRow(k){
  const tr=setRow('server:'+k.kind,[remindName[k.kind]||k.kind,k.to,k.on?remindTime(k):['끔','neutral'],remindOver(k)]);
  tr.title=remindTip[k.kind]||'';
  return tr;
}
function drawServer(s,box){
  const k=(s.remind||[]).find(x=>x.kind===s.key);
  if(k){drawRemindKind(k,box);return}
  const r=s.reminders;
  box.append(dl([['멈춤 문턱',r.idle==='off'?'끔':r.idle],['멈춤 알림',r.idle==='off'||!r.on?'-':String(r.idle_max||1)+'번'],['서버 판',s.version],['터미널 호스팅',s.hosting?'켬':'끔'],['답 규칙 판',String(s.answered_rule.version)],['마지막 이주',ruleLast(s.answered_rule.last)]]));
}
function remindHours(k){return k.on?String(Math.round(k.after_seconds/36)/100):''}
function drawRemindKind(k,box){
  const key='server:'+k.kind;
  drawSetNote(box,key);
  const over=remindOver(k),overEl=document.createElement('span');
  overEl.append(over[1]?badge(over[0],over[1]):over[0]);
  box.append(dl([['받는 쪽',k.to],['기한',remindTime(k)],['기준 시각',k.since?when(k.since):'-'],['넘긴 것',overEl]]));
  const stamp=String(k.after_seconds)+(k.since||''),kept=setForms.get(key);
  if(kept&&kept.stamp===stamp){box.append(kept.form);return}
  const f=node('form','','settings-form termform');
  const l=node('label','기한 (시간) '),h=document.createElement('input');
  h.type='number';h.min='0';h.max='8760';h.step='any';h.inputMode='decimal';h.placeholder='끔';h.value=remindHours(k);
  h.title=(remindTip[k.kind]||'')+' — 비우거나 0이면 끔, 0.5는 30분';h.setAttribute('aria-label','기한 (시간)');
  l.append(h);
  const save=node('button','저장');save.type='submit';save.title='저장 — 바꾼 때부터 다시 셈';
  const row=node('div','','row');row.append(save);
  f.append(l,row);
  f.onsubmit=async e=>{
    e.preventDefault();
    const v=parseFloat(h.value),mins=isFinite(v)&&v>0?Math.max(1,Math.round(v*60)):0;
    try{
      const got=await apiSend('PUT','settings/remind/'+k.kind,{after:mins?mins+'m':''});
      setForms.delete(key);setNote(key,mins?'기한을 '+remindTime(got)+'으로 저장했습니다.':'껐습니다.','ok');
    }catch(err){setNote(key,err.message,'err')}
    loadSettingsView();drawDetail();
  };
  setForms.set(key,{stamp:stamp,form:f});
  box.append(f);
}
// 웹훅 (#527): incoming webhooks, as hook:<id> (hook:+ a new one). The
// address shows once, after adding or making it again, until the detail is
// left; the original destination shows only as 있음/없음 with 바꾸기.
const hookModeName={mail:'메일만',forward:'다시 보내기'};
const hookModeTip={mail:'바깥 도구가 원래 목적지에 직접 보내고 switchboard에도 보냄',forward:'switchboard가 받아 원래 목적지로 다시 보냄'};
let hookAddr=null;
addEventListener('hashchange',()=>{if(hookAddr&&!decodeURIComponent(location.hash).includes('hook:'+hookAddr.id))hookAddr=null});
function hookDestText(v){return v.dest==='set'?'있음':v.dest==='unreadable'?'다시 등록 필요':'없음'}
function hookRow(v){
  const st=v.dest==='unreadable'?['다시 등록 필요','danger']:v.urgent?['긴급','warning']:null;
  return setRow('hook:'+v.id,[v.name,hookModeName[v.mode]||v.mode,v.role,v.last_at?shortWhen(v.last_at):'-',st||'']);
}
function hookForm(v){
  const key='hook:'+(v.fresh?'+':v.id),kept=setForms.get(key);
  if(kept&&kept.stamp===(v.updated_at||''))return kept.form;
  const f=node('form','','fld');
  const inp=(val,label,title)=>{const i=document.createElement('input');i.value=val;i.autocomplete='off';i.spellcheck=false;i.setAttribute('autocapitalize','off');i.setAttribute('aria-label',label);i.title=title;return i};
  const name=inp(v.name,'이름','웹훅 이름 (64자까지)');name.required=true;name.maxLength=64;
  const role=inp(v.role,'받는 역할','알림 메일을 받을 역할');role.required=true;role.placeholder='team/boss';role.className='mono';
  const mode=document.createElement('select');mode.setAttribute('aria-label','꼴');
  for(const m of ['mail','forward']){const o=node('option',hookModeName[m]);o.value=m;o.title=hookModeTip[m];mode.append(o)}
  mode.value=v.mode||'mail';mode.title=hookModeTip[mode.value];
  const urgent=document.createElement('input');urgent.type='checkbox';urgent.checked=!!v.urgent;urgent.title='긴급 메일로 보냄';urgent.setAttribute('aria-label','긴급');
  const win=document.createElement('input');win.type='number';win.min='1';win.max='1440';win.step='1';win.inputMode='numeric';win.value=String(v.window_min||10);
  win.title='같은 제목의 알림을 이 시간(분) 안에서 메일 하나로 묶음';win.setAttribute('aria-label','묶는 시간(분)');
  const dest=inp('','원래 목적지','원래 목적지 주소 — 저장 뒤 다시 볼 수 없음');dest.type='password';dest.className='mono';dest.placeholder='https://';
  let clear=false;
  const destCell=node('div','','row');
  const showDest=()=>{destCell.replaceChildren(dest);dest.focus()};
  if(v.fresh)destCell.append(dest);
  else{
    destCell.append(node('span',hookDestText(v)),setButton('바꾸기','',showDest,'원래 목적지를 새로 넣음'));
    if(v.dest!=='none'){
      const rm=setButton('없애기','',()=>{clear=true;destCell.replaceChildren(node('span','저장하면 없앰'))},'원래 목적지를 지움');
      destCell.append(rm);
    }
  }
  mode.onchange=()=>{mode.title=hookModeTip[mode.value]};
  const save=node('button',v.fresh?'추가':'저장');save.type='submit';save.title=v.fresh?'웹훅 추가':'저장';
  f.append(node('label','이름'),name,node('label','받는 역할'),role,node('label','꼴'),mode,node('label','긴급'),urgent,node('label','묶는 시간(분)'),win,node('label','원래 목적지'),destCell,node('span'),save);
  f.onsubmit=async e=>{
    e.preventDefault();
    const body={name:name.value.trim(),role:role.value.trim(),mode:mode.value,urgent:urgent.checked,window_min:Number(win.value)||10};
    if(dest.value.trim())body.dest=dest.value.trim();else if(clear)body.clear_dest=true;
    try{
      if(v.fresh){
        const r=await apiSend('POST','hooks',body);
        setForms.delete(key);hookAddr={id:r.id,token:r.token};setNote('hook:'+r.id,r.name+' 웹훅 추가','ok');
        loadSettingsView();openDetail('settings','hook:'+r.id,'edit');return;
      }
      await apiSend('PUT','hooks/'+v.id,body);
      setForms.delete(key);setNote(key,body.name+' 저장','ok');
    }catch(err){setNote(key,err.message,'err')}
    loadSettingsView();drawDetail();
  };
  setForms.set(key,{stamp:v.updated_at||'',form:f});
  return f;
}
function drawHookAddr(box,t){
  const url=location.origin+'/hook/'+t.token;
  const row=node('div','','detail-acts'),field=document.createElement('input');
  field.type='text';field.readOnly=true;field.value=url;field.className='mono';field.setAttribute('aria-label','받는 주소');field.title='받는 주소 — 이 화면을 떠나면 다시 볼 수 없음';
  field.style.cssText='flex:1 1 16em;min-width:0';
  const copy=setButton('복사','',async()=>{
    try{await navigator.clipboard.writeText(url)}
    catch(e){field.select();document.execCommand('copy')}
    copy.textContent='복사됨';
  },'받는 주소 복사');
  row.append(field,copy);box.append(row);
}
const hookCols=['시각','제목','전송'];
function hookForward(x){
  if(x.forward==='sent')return ['보냄'+(x.status?' '+x.status:''),'success'];
  if(x.forward==='failed')return ['실패'+(x.status?' '+x.status:'')+(x.attempts?' · '+x.attempts+'번':''),'danger'];
  if(x.forward==='pending')return [x.attempts?'다시 시도 중 · '+x.attempts+'번':'보내는 중','warning'];
  return null;
}
function drawHook(v,box){
  const id='hook:'+(v.fresh?'+':v.id);
  if(!v.fresh){
    const acts=node('div','','detail-acts');
    acts.append(setButton('주소 다시 만들기','',async e=>{
      if(!await askOn(e.currentTarget,{text:v.name+' 주소를 다시 만들까요? 지금 주소는 바로 못 쓰게 됩니다.'}))return;
      try{const r=await apiSend('POST','hooks/'+v.id+'/address');hookAddr={id:v.id,token:r.token};setNote(id,'새 주소를 만들었습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    },'새 받는 주소를 만들고 지금 주소를 끊음'));
    acts.append(setButton('지우기','',async e=>{
      if(!await askOn(e.currentTarget,{text:v.name+' 웹훅을 지울까요? 이 주소로 오는 알림은 더 받지 않습니다.'}))return;
      try{await apiSend('DELETE','hooks/'+v.id);setForms.delete(id);setMsg($('settingsMsg'),v.name+' 웹훅을 지웠습니다.','ok');closeDetail()}
      catch(e){setNote(id,e.message,'err');drawDetail()}
      loadSettingsView();
    },'이 웹훅을 지움'));
    box.append(acts);
    if(hookAddr&&hookAddr.id===v.id)drawHookAddr(box,hookAddr);
  }
  drawSetNote(box,id);
  box.append(hookForm(v));
  if(v.fresh)return;
  const t=node('table','','stack list cards'),head=node('tr'),th=document.createElement('thead'),body=document.createElement('tbody');
  for(const c of hookCols)head.append(node('th',c));
  th.append(head);t.append(th,body);
  for(const x of v.receipts||[]){
    const tr=node('tr',''),at=cell(shortWhen(x.at),hookCols[0],'nowrap');at.title=when(x.at);
    const fw=hookForward(x),fc=cell('',hookCols[2],'nowrap');
    if(fw)fc.append(badge(fw[0],fw[1]));else fc.textContent='-';
    tr.append(at,cell(x.subject,hookCols[1]),fc);body.append(tr);
  }
  if(!(v.receipts||[]).length)emptyRow(body,hookCols.length,'받은 알림 없음');
  box.append(node('h3','최근 받은 알림','dsec'),t);
}
async function findSetting(kind,key){
  if(kind==='qs')return key==='+'?{fresh:true,target:'',can_ask:false,discord:false,watchers:[],updated_at:''}:(await api('question-settings')).find(x=>x.target===key);
  if(kind==='refusals')return {rows:await api('question-refusals')};
  if(kind==='role')return key==='+'?{fresh:true,role:''}:(await api('roles')).find(x=>x.role===key);
  if(kind==='login')return (await api('logins')).find(x=>x.name===key);
  if(kind==='node'&&hostingOn)return (await api('nodes')).find(x=>x.name===key);
  if(kind==='group'&&hostingOn)return key==='+'?{name:'',fresh:true}:(await api('terminals/work-groups')).find(x=>x.name===key);
  if(kind==='watch')return findWatch(key);
  if(kind==='hook')return key==='+'?{fresh:true,name:'',role:'',mode:'mail',urgent:false,window_min:10,dest:'none'}:api('hooks/'+encodeURIComponent(key)).catch(()=>undefined);
  if(kind==='server')return api('settings').then(v=>Object.assign(v,{key:key}));
  if(kind==='view'&&key==='nodes'&&ws&&ws.nodes)return {};
  return undefined;
}
const setHeads={
  qs:v=>v.fresh?{title:'새 질문 설정',badge:null}:({title:v.target,badge:v.can_ask?['질문 가능','success']:['질문 불가','neutral']}),
  refusals:v=>({title:'최근 거절',badge:[v.rows.length+'건',v.rows.length?'warning':'neutral']}),
  role:v=>v.fresh?{title:'역할 초대',badge:null}:({title:setRoleName(v,setLive().get(v.role)),sub:v.role,badge:setRoleState(v,setLive().get(v.role))}),
  login:v=>({title:v.name,badge:v.disabled?['꺼짐','danger']:[v.kind==='builtin'?'내장':'외부','neutral']}),
  node:v=>({title:v.name,badge:v.revoked_at?['끊김','danger']:null}),
  group:v=>({title:v.fresh?'새 묶음':v.name,badge:null}),
  hook:v=>({title:v.fresh?'새 웹훅':v.name,badge:v.fresh?null:v.dest==='unreadable'?['다시 등록 필요','danger']:[hookModeName[v.mode]||v.mode,'neutral']}),
  server:v=>{const k=(v.remind||[]).find(x=>x.kind===v.key);return k?{title:'다시 알림 · '+(remindName[k.kind]||k.kind),badge:k.on?['켬','success']:['끔','neutral']}:{title:'다시 알림 · 서버',badge:null}},
  view:()=>({title:'노드',badge:null}),
};
const setDraws={qs:drawQS,refusals:drawRefusals,role:drawRole,login:drawLogin,node:drawNode,group:drawWorkGroup,hook:drawHook,server:drawServer,view:(v,box)=>{const n=ws.nodes();box.append(n.el);n.show()}};
registerDetail('settings',{
  list:'설정',kinds:['settings','terminals'],
  back:id=>setViewHash[setKindView[id.slice(0,id.indexOf(':'))]]||'settings',
  load:async id=>{
    const i=id.indexOf(':'),k=id.slice(0,i),v=i>0&&setHeads[k]?await findSetting(k,id.slice(i+1)):undefined;
    return v?{k:k,v:v}:null;
  },
  // The line beside the name: a role's address, else the item's tab.
  head:x=>{const h=setHeads[x.k](x.v);if(h.sub===undefined){const c=setCatOf(setKindView[x.k]);h.sub=c?c.name:''}return h},
  subs:x=>x.k==='qs'||(x.k==='role'&&x.v.fresh)||x.k==='group'||x.k==='watch'||x.k==='hook'||(x.k==='server'&&x.v.key in remindName)?[['edit','설정']]:[['info','정보']],
  draw:async(x,sub,box)=>{await setDraws[x.k](x.v,box);if(x.k!=='view')setFoot(box)}
});
