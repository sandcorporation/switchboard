// ---- 설정 (#515, #538) ----
// 설정 takes the whole screen (html.fr-set): the left pane becomes the
// settings menu (#setNav: 돌아가기, 설정 찾기, the groups 일반 · 연결 · 화면
// and the roles one per line under 역할), the centre the picked thing under
// the path row (#setHead: 설정 › 분류 › 항목, ✕). A category with several
// items (기계 …) lists them in the body (#panel-settings); a row opens its
// item there, #settings/<kind>:<id>/<sub>. The old view links
// (#settings-roles …) open that category. ← 돌아가기, ✕ and Esc go back to
// the screen before 설정; below 720px the menu and the body take turns
// (‹ and Esc go back to the menu).
const setViewHash={qsettings:'settings',roles:'settings-roles',nodes:'settings-nodes',groups:'settings-groups',logins:'settings-logins',watches:'settings-watches',hooks:'settings-hooks',reminders:'settings-reminders'};
const setKindView={qs:'qsettings',refusals:'qsettings',role:'roles',node:'nodes',group:'groups',login:'logins',watch:'watches',hook:'hooks',server:'reminders'};
let setView='qsettings',setData=null;
const setNotes=new Map(),setForms=new Map();
// setCats are the menu's categories, in its order: grp its group, add the
// new item's id (+ opens its blank form), host only with hosting on, one
// the single item a menu click opens at once.
const setCats=[
  {v:'logins',name:'로그인',grp:'일반'},
  {v:'qsettings',name:'질문 설정',grp:'일반',add:'qs:+'},
  {v:'reminders',name:'다시 알림',grp:'일반',one:'server:now'},
  {v:'nodes',name:'기계',grp:'연결',host:true},
  {v:'groups',name:'묶음',grp:'연결',host:true,add:'group:+'},
  {v:'hooks',name:'웹훅',grp:'연결',add:'hook:+'},
  {v:'watches',name:'지켜보기',grp:'연결',add:'watch:+',addPerm:'sessions'},
  {v:'roles',name:'역할',grp:'역할'},
];
const setIcons={
  logins:'M8 8a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM3 14c.5-2.8 2.4-4 5-4s4.5 1.2 5 4',
  qsettings:'M8 14.5a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13zM6.3 6a1.8 1.8 0 1 1 2.4 1.7c-.5.2-.7.6-.7 1.1v.5M8 11.2v.1',
  reminders:'M4 11V7a4 4 0 0 1 8 0v4l1.5 1.5h-11zM6.5 14h3',
  nodes:'M2.5 3h11v6.5h-11zM6 13h4M8 9.5V13',
  groups:'M2.5 3.5h4.5v4.5h-4.5zM9 3.5h4.5v4.5H9zM2.5 10h4.5v3.5h-4.5zM9 10h4.5v3.5H9z',
  hooks:'M6 3.5a2.5 2.5 0 1 1 3.5 2.3L8 9M4.5 9.5a2.5 2.5 0 1 0 2.4 3.2H11M11.5 9.5a2.5 2.5 0 1 1-1.6 4.4',
  watches:'M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8zM8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
  progress:'M2 8h3l2-5 2 10 2-5h3',
  sessions:'M2.5 3.5h11v7h-11zM6 13.5h4',
  terminals:'M2 3h12v10H2zM4.5 6l2 2-2 2M8 10h3.5',
  'view:nodes':'M8 2v4M8 6l-4.5 4M8 6l4.5 4M2 10.5h3v3H2zM6.5 10.5h3v3h-3zM11 10.5h3v3h-3z',
};
function setIcon(k){
  const s=document.createElementNS('http://www.w3.org/2000/svg','svg');s.setAttribute('width','14');s.setAttribute('height','14');s.setAttribute('viewBox','0 0 16 16');s.setAttribute('aria-hidden','true');
  const p=document.createElementNS('http://www.w3.org/2000/svg','path');p.setAttribute('d',setIcons[k]||'');p.setAttribute('fill','none');p.setAttribute('stroke','currentColor');p.setAttribute('stroke-width','1.3');p.setAttribute('stroke-linecap','round');p.setAttribute('stroke-linejoin','round');
  s.append(p);return s;
}
function setCatOn(c){return !c.host||hostingOn}
function setAddOk(c){return !!c.add&&setCatOn(c)&&(!c.addPerm||can(c.addPerm))}
function setCatOf(v){return setCats.find(c=>c.v===v)}
// showSettingsView(v) opens category v in the body.
function showSettingsView(v){
  if(!setViewHash[v]||((v==='nodes'||v==='groups')&&!hostingOn))v='qsettings';
  setView=v;
}
function settingsViewOfHash(h){for(const [v,x] of Object.entries(setViewHash))if(x===h)return v;return ''}
// loadSettingsView reads every category at once; a category whose read
// fails shows its error, the others still draw.
async function loadSettingsView(){
  if(!setData)$('settingsNote').textContent='불러오는 중…';
  const host=hostingOn,get=p=>p.catch(e=>({err:e.message}));
  const [qs,ref,roles,logins,nodes,groups,srv,watches,hooks]=await Promise.all([get(api('question-settings')),get(api('question-refusals')),get(api('roles')),get(api('logins')),host?get(api('nodes')):null,host?get(api('terminals/work-groups')):null,get(api('settings')),can('view')?get(api('watches')):[],get(api('hooks'))]);
  if(host!==hostingOn)return loadSettingsView();
  setData={qs:qs,ref:ref,roles:roles,logins:logins,nodes:nodes,groups:groups,srv:srv,watches:watches,hooks:hooks};
  drawSettings();
}
tabLoaders.settings=loadSettingsView;
$('setFind').oninput=()=>{if(setData)drawSettings()};
// setRow is one body row on one line: the name, its facts (top) and an
// optional [text,tone] at the right.
function setRow(id,top,title,st){
  const r=node('div','','sr');r.setAttribute('role','button');
  const t=node('span',st?st[0]:'','sr-t');if(st&&st[1])t.classList.add('tone-'+st[1]);
  const m=node('span',top.filter(Boolean).join(' · '),'sr-m');
  r.append(node('b',title||'(이름 없음)','sr-n'),m,t);r.title=[title].concat(top.filter(Boolean)).join(' · ');
  detailRow(r,'settings',id);
  return r;
}
const permName={view:'보기',type:'입력',files:'파일',sessions:'세션',discard:'버리기'};
// setCatRows is category v's body rows: a list, {err} or null (not shown).
function setCatRows(v){
  const d=setData;if(!d)return [];
  const each={
    roles:[d.roles,v=>setRow('role:'+v.role,[v.last_machine,v.last_used_at?'최근 '+shortWhen(v.last_used_at):''],v.role,v.revoked_at||v.retired_at?roleBadge(v):null)],
    nodes:[d.nodes,v=>setRow('node:'+v.name,[v.machine,v.os,v.version],v.name,v.revoked_at?['끊김','danger']:null)],
    groups:[d.groups,g=>setRow('group:'+g.name,[g.threshold===null||g.threshold===undefined?'문턱 없음':'문턱 '+g.threshold+'%','세션 '+g.sessions],g.name)],
    logins:[d.logins,v=>setRow('login:'+v.name,[v.kind==='builtin'?'내장':'외부',v.perms.length?v.perms.map(p=>permName[p]||p).join(' · '):'권한 없음'],v.name,v.disabled?['꺼짐','danger']:null)],
    qsettings:[d.qs,v=>setRow('qs:'+v.target,[v.can_ask?'질문 가능':'질문 불가',v.discord?'디스코드':'',v.watchers.length?'지켜봄 '+v.watchers.length:''],v.target)],
    watches:[d.watches,watchRow],
    hooks:[d.hooks,hookRow],
    reminders:[d.srv&&!d.srv.err?[d.srv]:d.srv,s=>setRow('server:now',[s.reminders.on?'켬':'끔','판 '+s.version],'다시 알림 · 서버')],
  }[v];
  if(!each||each[0]===null)return null;
  if(!Array.isArray(each[0]))return each[0];
  const rows=each[0].map(each[1]);
  if(v==='qsettings'&&Array.isArray(d.ref))rows.push(setRow('refusals:24h',['24시간'],'최근 거절',d.ref.length?[d.ref.length+'건','warning']:null));
  return rows;
}
// setScreens are the menu's 화면 rows: 진행·세션 표·터미널 open their own
// page (the address stays #progress …), 노드 the workspace's node index.
function setScreens(){
  const s=[['progress','진행','#progress'],['sessions','세션 표','#sessions']];
  if(hostingOn)s.push(['terminals','터미널','#terminals']);
  if(ws&&ws.nodes)s.push(['view:nodes','노드',detailHash('settings','view:nodes','')]);
  return s;
}
// setSel is the menu key of what the body shows: cat:<v>, scr:<tab>,
// role:<name>.
function setSel(){
  const c=detailCur;
  if(c&&c.tab==='settings'){
    const k=c.id.slice(0,c.id.indexOf(':'));
    if(k==='role')return c.id;
    if(k==='view')return 'scr:'+c.id;
    return 'cat:'+(setKindView[k]||'');
  }
  if(c&&setTabs.includes(c.tab))return 'scr:'+c.tab;
  if(curTab==='settings')return 'cat:'+setView;
  return setTabs.includes(curTab)?'scr:'+curTab:'';
}
// setPick: a menu click shows the body (below 720px the body takes the
// screen in the menu's place).
function setPick(){setPane='body';setChrome()}
function setMenuRow(key,name,href,icon,right,tone,hover){
  const a=node('a','','sn-i');a.href=href;a.dataset.setKey=key;a.title=hover||name;
  if(icon)a.append(setIcon(icon));
  a.append(node('span',name,'sn-nm'));
  if(right!==undefined&&right!==''){const t=node('span',String(right),'sn-rt');if(tone)t.classList.add('tone-'+tone);a.append(t)}
  a.addEventListener('click',()=>{setPick();if(location.hash===a.getAttribute('href'))route()});
  return a;
}
function setAddButton(title,id){
  const b=node('button','+','fr-ib sn-add');b.type='button';b.title=title;b.setAttribute('aria-label',title);
  b.onclick=()=>{setPick();openDetail('settings',id,'edit')};
  return b;
}
function setMenuHead(name,add){
  const h=node('div','','sn-h');h.append(node('span',name,'sn-hn'));if(add)h.append(add);return h;
}
// drawSetMenu draws the menu, 설정 찾기 keeping the categories whose name or
// an item's text holds it and the matching roles. A redraw keeps the
// menu's scroll and its marked row (#536).
function drawSetMenu(){
  const box=$('setMenu'),top=box.scrollTop,q=$('setFind').value,out=[];
  let grp='',head=null,n=0;
  const put=(g,row)=>{if(g!==grp){grp=g;head=setMenuHead(g);out.push(head)}out.push(row);n++};
  for(const c of setCats){
    if(c.v==='roles'||!setCatOn(c))continue;
    const rows=setCatRows(c.v);
    if(rows===null)continue;
    const items=Array.isArray(rows)?rows:[];
    if(!listMatch(q,c.name,c.grp)&&!items.some(r=>listMatch(q,r.textContent)))continue;
    const bad=Array.isArray(rows)?'':'!';
    const href=c.one?detailHash('settings',c.one,''):'#'+setViewHash[c.v];
    const right=bad||(c.one?(setData&&setData.srv&&!setData.srv.err?(setData.srv.reminders.on?'켬':'끔'):''):items.filter(r=>!r.dataset.detailId.startsWith('refusals:')).length);
    const row=setMenuRow('cat:'+c.v,c.name,href,c.v,right,bad?'danger':'',c.name+(bad?' — '+rows.err:''));
    if(setAddOk(c)){const w=node('div','','sn-row');w.append(row,setAddButton(c.name+' 추가',c.add));put(c.grp,w)}
    else put(c.grp,row);
  }
  for(const [k,name,href] of setScreens()){
    if(!listMatch(q,name,'화면'))continue;
    put('화면',setMenuRow('scr:'+k,name,href,k,''));
  }
  const rc=setCatOf('roles'),roles=setData?setData.roles:null;
  const rh=setMenuHead('역할',setAddOk(rc)?setAddButton('역할 초대',rc.add):null);
  const rs=[];
  if(roles&&!Array.isArray(roles))rs.push(node('p',roles.err,'msg err sn-empty'));
  else for(const v of roles||[]){
    if(!listMatch(q,v.role,v.last_machine))continue;
    const st=v.revoked_at||v.retired_at?roleBadge(v):null;
    rs.push(setMenuRow('role:'+v.role,v.role,detailHash('settings','role:'+v.role,''),'',st?st[0]:v.last_machine||'',st?st[1]:'',v.role+(v.last_machine?' · '+v.last_machine:'')+(st?' · '+st[0]:'')));
  }
  if(rs.length||!q){out.push(rh,...rs);n+=rs.length}
  if(!n&&q)out.push(node('p','찾은 것 없음','empty sn-empty'));
  const a=document.activeElement,key=a&&box.contains(a)?a.dataset.setKey:undefined;
  box.replaceChildren(...out);
  if(key)for(const r of box.querySelectorAll('[data-set-key]'))if(r.dataset.setKey===key){r.focus({preventScroll:true});break}
  box.scrollTop=top;
  setMark();
}
// setMark marks the menu row of what the body shows; a row out of sight
// (an address opened anew) comes into view, nothing else moves (#536).
function setMark(){
  const sel=setSel();
  for(const r of $('setMenu').querySelectorAll('[data-set-key]')){
    const on=r.dataset.setKey===sel;
    if(on)r.setAttribute('aria-current','page');else r.removeAttribute('aria-current');
    r.classList.toggle('on',on);
    if(on&&setJump&&r.scrollIntoView){setJump=false;r.scrollIntoView({block:'nearest'})}
  }
}
let setJump=true;
// drawSettings redraws the menu and the open category's body.
function drawSettings(){
  if(!setData)return;
  drawSetMenu();
  const c=setCatOf(setView)||setCatOf('qsettings'),rows=setCatRows(setView),q=$('setFind').value,box=$('setItems');
  $('setTitle').textContent=c.name;
  const add=$('setAddBtn');add.hidden=!setAddOk(c);
  if(c.add){add.title=c.v==='roles'?'역할 초대':c.name+' 추가';add.setAttribute('aria-label',add.title);add.onclick=()=>openDetail('settings',c.add,'edit')}
  if(rows===null){$('settingsNote').textContent='';box.replaceChildren(node('p','없음','empty'));return}
  if(!Array.isArray(rows)){$('settingsNote').textContent='';box.replaceChildren(node('p',rows.err,'msg err'));return}
  // 설정 찾기 keeps every row of a category it named, else the matching ones.
  const shown=listMatch(q,c.name,c.grp)?rows:rows.filter(r=>listMatch(q,r.textContent));
  const n=rows.filter(r=>!r.dataset.detailId.startsWith('refusals:')).length;
  $('settingsNote').textContent=n+'개'+(shown.length<rows.length?' · 찾은 것 '+shown.length:'');
  listFill(box,shown,q?'찾은 것 없음':c.name+' 없음');
}
// setPane is which of the menu and the body is on screen below 720px.
let setPane='body',setReturn='';
function setModeHash(h){const p=h.replace(/^#/,'').split('/')[0];return p==='settings'||p.startsWith('settings-')||setTabs.includes(p)}
if(setModeHash(location.hash))setPane=location.hash==='#settings'?'menu':'body';
// Entering 설정 keeps the address it came from for 돌아가기.
addEventListener('hashchange',e=>{
  const old=new URL(e.oldURL).hash;
  if(setModeHash(location.hash)&&!setModeHash(old)){setReturn=old;setJump=true;setPane=location.hash==='#settings'?'menu':'body'}
  else if(setModeHash(location.hash)&&location.hash!=='#settings')setPane='body';
});
// setExit leaves 설정 for the screen before it (지시 when there was none).
function setExit(){
  const r=setReturn;setReturn='';
  $('setFind').value='';
  location.hash=r&&r!=='#'&&!setModeHash(r)?r.slice(1):'requests';
}
// setPathOf is the path row's parts, [text, href or ''], for the open item
// c (detailCur), the tab on screen and the open category: 설정 › 분류 ›
// 항목, the category a link back to its list.
function setPathOf(c,tab,view,title){
  const segs=[['설정','']];
  if(c&&c.tab==='settings'){
    const k=c.id.slice(0,c.id.indexOf(':')),v=setKindView[k],cat=setCatOf(v);
    if(k==='view')segs.push([title,'']);
    else if(cat&&cat.one)segs.push([cat.name,'']);
    else{if(cat)segs.push([cat.name,'#'+setViewHash[v]]);segs.push([title,''])}
  }else if(c&&setTabs.includes(c.tab)){
    const s=setScreens().find(x=>x[0]===c.tab);
    segs.push([s?s[1]:c.tab,'#'+c.tab],[title,'']);
  }else if(tab==='settings'){const cat=setCatOf(view);segs.push([cat?cat.name:'',''])}
  else{const s=setScreens().find(x=>x[0]===tab);segs.push([s?s[1]:tab,''])}
  return segs;
}
// setChrome fits the menu and the path row to what is on screen.
function setChrome(){
  const html=document.documentElement,on=html.classList.contains('fr-set');
  html.classList.toggle('fr-setm',on&&setPane==='menu');
  if(!on)return;
  const c=detailCur,path=$('setPath');
  const segs=setPathOf(c,curTab,setView,$('detailTitle').textContent).map(([t,h])=>{if(!h)return node('span',t);const a=node('a',t);a.href=h;a.title=t;return a});
  const out=[];
  segs.forEach((s,i)=>{if(i)out.push(node('span','›','sh-sep'));s.classList.add('sh-seg');out.push(s)});
  segs[segs.length-1].setAttribute('aria-current','page');
  path.replaceChildren(...out);path.title=segs.map(s=>s.textContent).join(' › ');
  $('detail').classList.toggle('sd-form',!!c&&c.tab==='settings'&&!c.id.startsWith('view:'));
  if(setData)drawSettings();
}
$('setBack').onclick=setExit;
$('setClose').onclick=setExit;
$('setMenuBack').onclick=()=>{setPane='menu';setChrome();$('setFind').focus({preventScroll:true})};
// Esc (before the detail's own Esc): a field's own Esc first (a filled
// 찾기, an open dialog, a terminal); below 720px the body goes back to the
// menu; else 설정 closes.
document.addEventListener('keydown',e=>{
  if(e.key!=='Escape'||e.defaultPrevented||!document.documentElement.classList.contains('fr-set'))return;
  const t=e.target;
  if(document.querySelector('dialog[open]')||(t&&t.closest&&t.closest('.terminal,#termPane'))||(t&&t.type==='search'&&t.value))return;
  e.preventDefault();
  if(matchMedia('(max-width:720px)').matches&&setPane==='body'){setPane='menu';setChrome();$('setFind').focus({preventScroll:true})}
  else setExit();
},true);
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
  return setRow('hook:'+v.id,[hookModeName[v.mode]||v.mode,v.role,v.last_at?'최근 '+shortWhen(v.last_at):''],v.name,st);
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
    acts.append(setButton('주소 다시 만들기','',async()=>{
      if(!confirm(v.name+' 주소를 다시 만들까요? 지금 주소는 바로 못 쓰게 됩니다.'))return;
      try{const r=await apiSend('POST','hooks/'+v.id+'/address');hookAddr={id:v.id,token:r.token};setNote(id,'새 주소를 만들었습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadSettingsView();drawDetail();
    },'새 받는 주소를 만들고 지금 주소를 끊음'));
    acts.append(setButton('지우기','',async()=>{
      if(!confirm(v.name+' 웹훅을 지울까요? 이 주소로 오는 알림은 더 받지 않습니다.'))return;
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
  if(kind==='role')return (await api('roles')).find(x=>x.role===key);
  if(kind==='login')return (await api('logins')).find(x=>x.name===key);
  if(kind==='node'&&hostingOn)return (await api('nodes')).find(x=>x.name===key);
  if(kind==='group'&&hostingOn)return key==='+'?{name:'',fresh:true}:(await api('terminals/work-groups')).find(x=>x.name===key);
  if(kind==='watch')return findWatch(key);
  if(kind==='hook')return key==='+'?{fresh:true,name:'',role:'',mode:'mail',urgent:false,window_min:10,dest:'none'}:api('hooks/'+encodeURIComponent(key)).catch(()=>undefined);
  if(kind==='server')return api('settings');
  if(kind==='view'&&key==='nodes'&&ws&&ws.nodes)return {};
  return undefined;
}
const setHeads={
  qs:v=>v.fresh?{title:'새 질문 설정',badge:null}:({title:v.target,badge:v.can_ask?['질문 가능','success']:['질문 불가','neutral']}),
  refusals:v=>({title:'최근 거절',badge:[v.rows.length+'건',v.rows.length?'warning':'neutral']}),
  role:v=>({title:v.role,badge:roleBadge(v)}),
  login:v=>({title:v.name,badge:v.disabled?['꺼짐','danger']:[v.kind==='builtin'?'내장':'외부','neutral']}),
  node:v=>({title:v.name,badge:v.revoked_at?['끊김','danger']:null}),
  group:v=>({title:v.fresh?'새 묶음':v.name,badge:null}),
  hook:v=>({title:v.fresh?'새 웹훅':v.name,badge:v.fresh?null:v.dest==='unreadable'?['다시 등록 필요','danger']:[hookModeName[v.mode]||v.mode,'neutral']}),
  server:v=>({title:'다시 알림 · 서버',badge:v.reminders.on?['켬','success']:['끔','neutral']}),
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
  head:x=>setHeads[x.k](x.v),
  subs:x=>x.k==='qs'||x.k==='group'||x.k==='watch'||x.k==='hook'?[['edit','설정']]:[['info','정보']],
  draw:(x,sub,box)=>setDraws[x.k](x.v,box)
});
