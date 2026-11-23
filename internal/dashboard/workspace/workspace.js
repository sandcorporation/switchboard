// ---- 작업 화면 (#444): the host-mode workspace ----
// Three columns: the menu on the left (the side menu), panels in the centre,
// the detail panel on the right. The centre is wsLayout's tree of splits and
// tab groups; a tab drags to another group or to a group's edge, a border
// drags to resize. A 메뉴 item opens its tab's section (#panel-<tab>, moved in
// from the old page, so its drawing code runs as before) as a panel; a
// terminal opens as its own panel. The layout is kept per browser
// (localStorage); a layout that does not read back gives the default. At
// 600px and below the centre shows one panel with every open panel as a tab,
// and the menu is a drawer. Runs on the page's globals (node, $, setMsg,
// curTab, visibleTabs, selectTab, loadTab, termLoad, termRole, ...).
// The right column (#445, wsSide) follows the focused panel's session: its
// 기록, 예약 and 설정 views, the detail's own drawing functions. Above it,
// 「답이 필요함」 holds every session's pending questions and permission
// requests; one opens as a centre panel (ask:<id>, perm:<id>) drawn by the
// detail spec of its tab. An open detail takes the column's place; at 600px
// and below the column is a drawer.
// Its 파일 view (#447, wsFiles) is the session folder's tree; a file opens as
// a centre panel (file:<id>:<path>): by extension an <img>, <audio> or
// <video> streaming /files/raw (Range seeks), else the editor (#448), saved
// with 저장 or Ctrl+S; a file changed elsewhere since it was read offers
// 다시 읽기 and 그래도 저장.
// wsIcons are the right column's icon paths (#510), drawn by wsIcon.
const wsIcons={
  files:'M5.5 2.5h6l3.5 3.5v11.5h-9.5zM11.5 2.5V6H15',
  log:'M6 5h10M6 10h10M6 15h10M3 5h.01M3 10h.01M3 15h.01',
  mail:'M4 4.5h12a1.5 1.5 0 011.5 1.5v8a1.5 1.5 0 01-1.5 1.5H4A1.5 1.5 0 012.5 14V6A1.5 1.5 0 014 4.5zM3 6l7 5 7-5',
  needs:'M10 3a5 5 0 00-5 5v3l-1.5 2.5h13L15 11V8a5 5 0 00-5-5zM8.5 16a1.5 1.5 0 003 0',
  autos:'M17 10a7 7 0 11-14 0 7 7 0 0114 0zM10 6v4l2.5 2',
  settings:'M12.5 10a2.5 2.5 0 11-5 0 2.5 2.5 0 015 0zM10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4',
};
// wsMail lists the latest mail to or from role (#510); a row opens the
// message. Every server string goes in as text.
async function wsMail(role,out){
  const d=await api('messages?'+new URLSearchParams({box:'all',role:role,limit:'30'}));
  const rows=Array.isArray(d)?d:[];
  const ul=node('ul','','ws-mail');
  for(const m of rows){
    const b=node('button','','ws-mailrow');b.type='button';b.title=m.subject+' — '+m.sender+' → '+m.recipient;
    const top=node('span','','ws-mail1');top.append(node('strong',m.subject),node('span',shortWhen(m.created_at),'muted'));
    b.append(top,node('span',m.sender+' → '+m.recipient,'ws-mail2 muted'));
    b.onclick=()=>openMessage(m.id);
    const li=node('li','');li.append(b);ul.append(li);
  }
  out.append(rows.length?ul:node('p','메일 없음','empty'));
}
function wsIcon(k){
  const NS='http://www.w3.org/2000/svg',svg=document.createElementNS(NS,'svg'),p=document.createElementNS(NS,'path');
  svg.setAttribute('viewBox','0 0 20 20');svg.setAttribute('aria-hidden','true');p.setAttribute('d',wsIcons[k]||'');svg.append(p);
  return svg;
}
function wsStart(){
  const L=wsLayout,S=wsSide,F=wsFiles,KEY='switchboard.workspace',SKEY=KEY+'.side',DT='application/x-switchboard-panel';
  const mq=window.matchMedia('(max-width:600px)');
  const main=document.querySelector('.sc-app-page');
  const box=node('div','','ws');box.id='ws';
  main.before(box);main.hidden=true;
  const html=document.documentElement;html.classList.add('ws-on');
  frameScrimClose.push(()=>sideDrawer(false));
  const terms=new Map(),wraps=new Map(),titles=new Map(),items=new Map(),files=new Map(),trees=new Map();
  let autoFor=null,dragging=false;
  function valid(p){
    if(p.startsWith('menu:'))return visibleTabs().includes(p.slice(5));
    return (p.startsWith('term:')&&p.length>5)||!!S.itemOf(p)||!!F.fileOf(p);
  }
  function load(){
    try{const s=L.parse(localStorage.getItem(KEY)||'',valid);if(s)return s}catch(e){}
    return L.initial('menu:'+(curTab||'requests'));
  }
  function save(){try{localStorage.setItem(KEY,L.serialize(st))}catch(e){}}
  let st=load();
  const menuOf=p=>p.startsWith('menu:')?p.slice(5):'';
  function title(p){
    const m=menuOf(p);
    if(m){const t=$('tab-'+m);return t?t.querySelector('.tab-name').textContent:m}
    const f=F.fileOf(p);if(f)return F.base(f.path)+(fileDirty(files.get(p))?' •':'');
    const it=S.itemOf(p);
    if(titles.has(p)||!it)return titles.get(p)||p.slice(5);
    return it.tab==='questions'?'질문':'권한 요청';
  }
  // term returns the open terminal of a panel, making it on first use.
  function term(id,v){
    let t=terms.get(id);
    if(t)return t;
    t=termMake(id);terms.set(id,t);
    t.onWide=()=>maximize('term:'+id);
    if(termRefused){t.el.replaceChildren(node('p',termRefusedText[termRefused]||'이 주소에서는 터미널이 꺼져 있음','msg err'));return t}
    const named=r=>{titles.set('term:'+id,termRole(r)+(r.label?' · '+r.label:''));retitle('term:'+id)};
    if(v){named(v);termOpen(t,v)}
    else termLoad(id).then(r=>{
      if(t.closed)return;
      if(!r){setMsg(t.msgEl,'없음 · 지워졌거나 바뀜','err');return}
      named(r);termOpen(t,r);
    }).catch(e=>{if(!t.closed)setMsg(t.msgEl,e.message,'err')});
    return t;
  }
  function content(p){
    const m=menuOf(p);
    if(m)return $('panel-'+m);
    if(F.fileOf(p))return filePane(p);
    return S.itemOf(p)?itemPane(p):term(p.slice(5)).el;
  }
  function wrap(p){
    let w=wraps.get(p);
    if(!w){w=node('div','','ws-panel');w.setAttribute('role','tabpanel');w.dataset.p=p;w.append(content(p));wraps.set(p,w)}
    return w;
  }
  function retitle(p){
    for(const b of box.querySelectorAll('.ws-tab'))if(b.dataset.p===p){b.textContent=title(p);b.title=title(p)}
  }
  function commit(){save();render()}
  function shownPanels(){
    if(mq.matches)return st.focus?[st.focus]:[];
    if(st.max)return [st.max];
    return L.groups(st).map(g=>g.active).filter(Boolean);
  }
  function render(){
    const narrow=mq.matches;
    box.classList.toggle('ws-narrow',narrow);
    box.classList.toggle('ws-maxed',!narrow&&!!st.max);
    const view=narrow?{g:'',tabs:L.panels(st),active:st.focus}:st.root;
    box.replaceChildren(draw(view,[],narrow));
    for(const [p,t] of terms){termWideShow(t,!narrow&&st.max===p);t.wideEl.hidden=narrow}
    for(const p of shownPanels()){const m=menuOf(p);if(m)loadTab(m)}
    follow();markNeeds();
  }
  function draw(n,path,narrow){
    if(typeof n.g==='string')return drawGroup(n,narrow);
    const el=node('div','','ws-split ws-'+n.dir);
    n.kids.forEach((k,i)=>{
      if(i)el.append(gutter(el,n,path,i-1));
      const c=node('div','','ws-cell');c.style.flex=n.sizes[i]+' 1 0';
      c.append(draw(k,path.concat(i),narrow));
      el.append(c);
    });
    return el;
  }
  function gutter(el,n,path,i){
    const row=n.dir==='row',g=node('div','','ws-gutter');
    g.setAttribute('role','separator');g.setAttribute('aria-orientation',row?'vertical':'horizontal');g.setAttribute('aria-label','크기');g.tabIndex=0;
    const sizes=()=>{const s=L.nodeAt(st,path).sizes;[...el.children].filter(c=>c.classList.contains('ws-cell')).forEach((c,j)=>{c.style.flex=s[j]+' 1 0'})};
    g.addEventListener('pointerdown',e=>{
      e.preventDefault();g.setPointerCapture(e.pointerId);
      const r=el.getBoundingClientRect(),from=st,start=row?e.clientX:e.clientY,len=row?r.width:r.height;
      box.classList.add('ws-resizing');
      const mv=ev=>{st=L.resize(from,path,i,((row?ev.clientX:ev.clientY)-start)/len);sizes()};
      const up=()=>{g.removeEventListener('pointermove',mv);g.removeEventListener('pointerup',up);g.removeEventListener('pointercancel',up);box.classList.remove('ws-resizing');save()};
      g.addEventListener('pointermove',mv);g.addEventListener('pointerup',up);g.addEventListener('pointercancel',up);
    });
    g.addEventListener('keydown',e=>{
      const d={ArrowLeft:-1,ArrowUp:-1,ArrowRight:1,ArrowDown:1}[e.key];
      if(!d)return;
      e.preventDefault();st=L.resize(st,path,i,d*0.05);sizes();save();
    });
    return g;
  }
  function drawGroup(g,narrow){
    const el=node('div','','ws-group');
    if(st.max&&g.tabs.includes(st.max))el.classList.add('ws-max');
    const bar=node('div','','ws-tabs');
    if(narrow){
      const m=node('button','☰','ws-menu');m.type='button';m.title='메뉴';m.setAttribute('aria-label','메뉴');m.setAttribute('aria-expanded',String(html.classList.contains('lp-open')));
      m.onclick=()=>drawer(true);bar.append(m);
    }
    const side=()=>{
      const b=node('button','','ws-sidebtn');b.type='button';b.title='답이 필요함 · 세션';b.setAttribute('aria-label','답이 필요함 · 세션');
      b.setAttribute('aria-expanded',String(html.classList.contains('ws-right-open')));
      b.append(node('span','▤'),node('span',needN?String(needN):'','ws-sidecount'));
      b.onclick=()=>sideDrawer(true);return b;
    };
    const list=node('div','','ws-tablist');list.setAttribute('role','tablist');
    for(const p of g.tabs){
      const on=p===g.active,cell=node('span','','ws-tabcell'+(on?' on':''));
      const tab=node('button',title(p),'ws-tab');tab.type='button';tab.title=title(p);tab.dataset.p=p;
      tab.setAttribute('role','tab');tab.setAttribute('aria-selected',String(on));tab.tabIndex=on?0:-1;
      tab.onclick=()=>pick(p);
      if(!narrow){
        tab.draggable=true;
        tab.addEventListener('dragstart',e=>{dragging=true;e.dataTransfer.setData(DT,p);e.dataTransfer.effectAllowed='move'});
        tab.addEventListener('dragend',()=>{dragging=false;for(const d of box.querySelectorAll('.ws-drop'))d.remove()});
      }
      const x=node('button','×','ws-x');x.type='button';x.title='닫기';x.setAttribute('aria-label',title(p)+' 닫기');x.onclick=()=>closePanel(p);
      cell.append(tab,x);list.append(cell);
    }
    list.addEventListener('keydown',e=>{
      const i=g.tabs.indexOf(g.active),j={ArrowLeft:i-1,ArrowRight:i+1,Home:0,End:g.tabs.length-1}[e.key];
      if(j===undefined||!g.tabs.length)return;
      e.preventDefault();const p=g.tabs[(j+g.tabs.length)%g.tabs.length];pick(p);
      for(const b of box.querySelectorAll('.ws-tab'))if(b.dataset.p===p)b.focus();
    });
    bar.append(list);
    if(narrow)bar.append(side());
    const body=node('div','','ws-body');
    for(const p of g.tabs){const w=wrap(p);w.hidden=p!==g.active;body.append(w)}
    if(!g.tabs.length)body.append(node('p','열린 패널 없음','empty ws-empty'));
    if(!narrow){
      dropTarget(bar,g,()=>'center');
      dropTarget(body,g,e=>{const r=body.getBoundingClientRect();return L.dropZone(r.width,r.height,e.clientX-r.left,e.clientY-r.top)});
    }
    el.addEventListener('pointerdown',()=>{if(g.active&&st.focus!==g.active){st.focus=g.active;save();markFocus()}},true);
    if(g.active&&g.active===st.focus)el.classList.add('ws-focus');
    el.dataset.g=g.g;
    el.append(bar,body);
    return el;
  }
  function markFocus(){
    const fg=L.groupOf(st,st.focus);
    for(const el of box.querySelectorAll('.ws-group'))el.classList.toggle('ws-focus',!!fg&&el.dataset.g===fg.g);
    follow();markNeeds();
  }
  function dropTarget(el,g,zoneOf){
    let mark=null;
    const clear=()=>{if(mark){mark.remove();mark=null}};
    el.addEventListener('dragover',e=>{
      if(!dragging||!e.dataTransfer.types.includes(DT))return;
      e.preventDefault();e.dataTransfer.dropEffect='move';
      const z=zoneOf(e),host=el.classList.contains('ws-body')?el:el.parentNode.querySelector('.ws-body');
      if(!mark){mark=node('div','','ws-drop');host.append(mark)}
      mark.dataset.zone=z;
    });
    el.addEventListener('dragleave',e=>{if(!el.contains(e.relatedTarget))clear()});
    el.addEventListener('drop',e=>{
      const p=e.dataTransfer.getData(DT);if(!p)return;
      e.preventDefault();const z=zoneOf(e);clear();dragging=false;
      const s=L.move(st,p,g.g,z);if(s!==st){st=s;commit()}
    });
  }
  // pick shows a panel; a 메뉴 panel also becomes the page's tab (hash).
  function pick(p){
    st=L.open(st,p);commit();
    const m=menuOf(p);if(m&&m!==curTab)selectTab(m);
  }
  function closePanel(p){
    if(F.fileOf(p)&&fileDirty(files.get(p))&&!confirm('저장하지 않은 고침을 버림'))return;
    const m=menuOf(p),w=wraps.get(p);
    if(m)main.append($('panel-'+m));
    else if(S.itemOf(p)){const it=items.get(p);if(it)it.seq++;items.delete(p)}
    else if(F.fileOf(p)){const f=files.get(p);if(f)f.seq++;files.delete(p)}
    else{const id=p.slice(5),t=terms.get(id);if(t){termStop(t);terms.delete(id)}if(autoFor===id)autoFor=null}
    if(w)w.remove();
    wraps.delete(p);titles.delete(p);
    st=L.close(st,p);commit();
  }
  function maximize(p){st=L.setMax(st,st.max===p?null:p);commit()}
  function drawer(on){frameDrawer(on)}
  function sideDrawer(on){
    html.classList.toggle('ws-right-open',on);
    for(const b of box.querySelectorAll('.ws-sidebtn'))b.setAttribute('aria-expanded',String(on));
    if(on)sideX.focus({preventScroll:true});
  }
  // Esc: a typing terminal keeps it; otherwise the drawer closes, then
  // 넓히기 returns to the normal size, and only then does the detail panel's
  // own Esc close the detail.
  document.addEventListener('keydown',e=>{
    if(e.key!=='Escape'||e.defaultPrevented)return;
    if(e.target.closest&&e.target.closest('dialog'))return;
    if(html.classList.contains('lp-open')){e.preventDefault();drawer(false);return}
    if(html.classList.contains('ws-right-open')){e.preventDefault();sideDrawer(false);return}
    if(!st.max)return;
    const t=terms.get(st.max.slice(5));
    if(t&&t.mode==='input'&&t.screen.contains(e.target))return;
    e.preventDefault();maximize(st.max);
  },true);
  mq.addEventListener('change',()=>{drawer(false);sideDrawer(false);render()});
  // openTerm shows a terminal's panel; a new one joins the group that holds
  // terminals, else opens to the right of the focused group.
  function openTerm(v){
    const p='term:'+v.id;
    term(v.id,v);
    if(L.groupOf(st,p)||mq.matches){st=L.open(st,p)}
    else{
      const tg=L.groups(st).find(g=>g.tabs.some(x=>x.startsWith('term:')));
      const fg=L.groupOf(st,st.focus)||L.groups(st)[0];
      st=tg?L.open(st,p,tg.g):L.openAt(st,p,fg.g,fg.tabs.length?'right':'center');
    }
    commit();
  }
  // ---- 오른쪽 칸 (#445) ----
  let side=S.initial();
  try{side=S.parse(localStorage.getItem(SKEY)||'')}catch(e){}
  function saveSide(){try{localStorage.setItem(SKEY,S.serialize(side))}catch(e){}}
  const sideEl=node('aside','','ws-side');sideEl.id='wsSide';sideEl.setAttribute('aria-label','이 세션의 곁 칸');
  const sideX=node('button','×','ws-side-x');sideX.type='button';sideX.title='닫기';sideX.setAttribute('aria-label','닫기');sideX.onclick=()=>sideDrawer(false);
  // The column's top row (#510) is one icon per view (S.SUBS); only the
  // chosen view shows. 질문 · 권한 요청 lists the followed session's waiting
  // questions and permission requests (every session's when none is followed).
  const ix=node('div','','ix');ix.setAttribute('role','tablist');ix.setAttribute('aria-label','곁 칸');
  const ixBtn={};
  for(const [k,label] of S.SUBS){
    const b=node('button','','ix-b');b.type='button';b.dataset.sub=k;b.title=S.TIPS[k];
    b.setAttribute('role','tab');b.setAttribute('aria-label',label);b.setAttribute('aria-controls','wsSessBody');
    b.append(wsIcon(k),node('span','','ix-n'));b.onclick=()=>pickSub(k);ixBtn[k]=b;ix.append(b);
  }
  ix.addEventListener('keydown',e=>{
    const ks=S.SUBS.map(s=>s[0]),i=ks.indexOf(side.sub),d={ArrowLeft:-1,ArrowRight:1}[e.key];
    if(!d)return;
    e.preventDefault();pickSub(ks[(i+d+ks.length)%ks.length]);
  });
  ix.append(sideX);
  const sessHead=node('div','','ixh'),sessTitle=node('h2',''),sessBadge=node('span','','sc-badge'),sessWhere=node('span','','ixc');
  sessHead.append(sessTitle,sessBadge,sessWhere);
  const needBox=node('section','','ws-needs'),needList=node('ul','','ws-needlist');needBox.append(needList);
  const sessBody=node('div','','ws-sess-body');
  sessBody.setAttribute('role','tabpanel');sessBody.id='wsSessBody';sessBadge.hidden=true;
  sideEl.append(ix,sessHead,needBox,sessBody);
  const gripR=node('div','','fr-grip fr-grip-r');gripR.tabIndex=0;gripR.title='끌어서 오른쪽 칸 너비를 바꾼다 (← → 키도 된다)';
  gripR.setAttribute('role','separator');gripR.setAttribute('aria-orientation','vertical');gripR.setAttribute('aria-label','오른쪽 칸 너비');gripR.setAttribute('aria-controls','wsSide');
  $('appMain').append(gripR);frameGrip(gripR,'r');
  $('appMain').append(sideEl);
  let needN=0,needSeq=0,sessSeq=0,sessShown='',needRows=[],sessRole='';
  function pickSub(k){side.sub=k;saveSide();drawSess().then(()=>ixBtn[k].focus())}
  // markNeeds marks the row whose panel has focus.
  function markNeeds(){for(const b of needList.querySelectorAll('.ws-need'))if(b.dataset.p===st.focus)b.setAttribute('aria-current','true');else b.removeAttribute('aria-current')}
  async function loadNeeds(){
    const seq=++needSeq;
    try{
      const [q,pm,tm]=await Promise.all([api('questions?status=pending&limit=500'),api('permissions?status=pending'),api('terminals').catch(()=>[])]);
      if(seq!==needSeq)return;
      const sess=Array.isArray(tm)?tm:[];
      needRows=S.needs(q.questions,pm.permissions,sess);needSess=sess;
      needN=needRows.length;
      for(const c of box.querySelectorAll('.ws-sidecount'))c.textContent=needN?String(needN):'';
      drawNeeds();
    }catch(e){if(seq===needSeq)needList.replaceChildren(node('li',e.message,'msg err'))}
  }
  let needSess=[];
  // drawNeeds lists the waiting rows of the followed session (all with none).
  function drawNeeds(){
    const sess=needSess,id=side.session,a=document.activeElement,was=needList.contains(a)?a.dataset.p:'';
    const rows=id?needRows.filter(r=>r.pid==='term:'+id||(!!sessRole&&r.who===sessRole)):needRows;
    ixBtn.needs.querySelector('.ix-n').textContent=rows.length?String(rows.length):'';
    {
      needList.replaceChildren(...rows.map(r=>{
        const li=node('li',''),b=node('button','','ws-need');b.type='button';b.dataset.p=r.pid;b.title=r.title;
        const ask=r.kind==='ask',det=r.kind==='detached';
        b.append(badge(det?'못 붙음':ask?'질문':'권한',det?'danger':ask?'warning':'info'),node('span',r.title,'ws-need-title'),node('span',r.who+(r.at?' · '+when(r.at):''),'ws-need-who'));
        b.onclick=det?()=>{const v=sess.find(x=>'term:'+x.id===r.pid);if(v){openTerm(v);sideDrawer(false)}}:()=>openItem(r.pid);li.append(b);return li;
      }));
      if(!rows.length)needList.append(node('li','대기 없음','empty'));
      markNeeds();
      if(was)for(const b of needList.querySelectorAll('.ws-need'))if(b.dataset.p===was)b.focus({preventScroll:true});
    }
  }
  // openItem shows a question or permission request as a centre panel.
  function openItem(p){
    st=L.open(st,p);if(st.max&&st.max!==p)st=L.setMax(st,null);
    const it=items.get(p);if(it)drawItem(p);
    sideDrawer(false);commit();
  }
  function follow(){
    const id=S.follow(side.session,st.focus);
    if(id!==side.session){side.session=id;saveSide();drawSess();document.dispatchEvent(new CustomEvent('ws-follow'))}
  }
  function subTabs(el,subs,cur,pick){
    el.replaceChildren();
    for(const [k,label] of subs){
      const b=node('button',label,'ws-sub');b.type='button';b.dataset.sub=k;
      b.setAttribute('role','tab');b.setAttribute('aria-selected',String(k===cur));b.tabIndex=k===cur?0:-1;
      b.onclick=()=>pick(k);el.append(b);
    }
  }
  // drawSess draws the followed session's view with the detail's functions;
  // an open detail holds the same views, so the column stays empty meanwhile.
  async function drawSess(){
    const id=side.session,seq=++sessSeq;
    for(const [k,b] of Object.entries(ixBtn)){const on=k===side.sub;b.setAttribute('aria-selected',String(on));b.tabIndex=on?0:-1}
    const needs=side.sub==='needs';needBox.hidden=!needs;
    if(detailCur){sessShown='';sessBody.replaceChildren();return}
    sessShown=id||'-';
    if(!id){sessRole='';sessTitle.textContent=needs?'답을 기다리는 것':'세션';sessWhere.textContent='';sessBadge.hidden=true;sessBody.replaceChildren(needs?'':node('p','세션 없음','empty'));drawNeeds();return}
    const out=document.createElement('div');
    let v=null;
    try{
      v=await termLoad(id);
      if(seq!==sessSeq)return;
      if(v){
        sessRole=termRole(v);
        if(!needs)termActs(v,out);
        if(needs){}
        else if(side.sub==='files')out.append(fileTree(v.id));
        else if(side.sub==='autos')await termAutos(v,out);
        else if(side.sub==='mail')await wsMail(termRole(v),out);
        else if(side.sub==='settings')termSettings(v,out);
        else await termHistory(v,out);
      }else out.append(node('p','없음 · 지워졌거나 바뀜','empty'));
    }catch(e){out.append(node('p',e.message,'msg err'))}
    if(seq!==sessSeq)return;
    const h=v?details.terminals.head(v):{title:titles.get('term:'+id)||id,badge:null};
    sessTitle.textContent=h.title;sessBadge.hidden=!h.badge;
    sessWhere.textContent=v&&v.directory||'';sessWhere.title=v?(v.node||'')+' › '+(v.directory||''):'';
    drawNeeds();
    if(h.badge){sessBadge.textContent=h.badge[0];sessBadge.className='sc-badge '+(toneClass[h.badge[1]]||toneClass.neutral)}
    const top=sideEl.scrollTop,a=document.activeElement,inBody=sessBody.contains(a);
    sessBody.replaceChildren(...out.childNodes);
    sideEl.scrollTop=top;
    if(inBody&&!sessBody.contains(document.activeElement))sessTitle.tabIndex=-1,sessTitle.focus({preventScroll:true});
  }
  // ---- 파일 (#447) ----
  // filesGet asks the files API: the listing (JSON) or, with raw, the bytes.
  // A refusal throws the text for its code.
  async function filesGet(id,path,raw){
    let r;
    try{r=await fetch('/api/admin/'+F.url(id,path,raw),{credentials:'same-origin',headers:{'Accept':raw?'*/*':'application/json'}})}
    catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
    if(r.status===401)signedOut();
    if(r.redirected)throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
    if(!r.ok){let b='';try{b=await r.text()}catch(e){}throw Error(F.problem(r.status,b))}
    return raw?new Uint8Array(await r.arrayBuffer()):r.json();
  }
  // fileTree is the 파일 view of session id, kept per session so a redraw
  // of the column keeps what is open; ↻ reads the folder again. ⬆ 올리기
  // puts files in the open folder (the last one opened, else the session
  // folder); each row has ✎ (rename in place) and 🗑 (delete after an
  // in-row confirm), shown on hover or focus, always where there is no
  // hover (#473–#475). Without the files permission (#476) none of them.
  function fileTree(id){
    let t=trees.get(id);
    if(t)return t.el;
    const el=node('div','','ws-files'),bar=node('div','','ws-files-bar'),re=node('button','↻','ws-files-re'),list=node('ul','','ws-tree'),
      up=node('button','⬆ 올리기','ws-files-up'),pick=document.createElement('input'),msg=node('div','','ws-files-msg');
    re.type='button';re.title='다시 읽기';re.setAttribute('aria-label','다시 읽기');
    up.type='button';up.hidden=!can('files');
    pick.type='file';pick.multiple=true;pick.hidden=true;pick.tabIndex=-1;
    msg.setAttribute('role','status');msg.hidden=true;
    list.setAttribute('role','tree');list.setAttribute('aria-label','세션 폴더');
    bar.append(up,re,pick);el.append(bar,msg,list);
    t={el:el,list:list,msg:msg,up:up,cur:'',uls:new Map([['',list]])};trees.set(id,t);
    treeCur(t,'');
    re.onclick=()=>{treeMsg(t,'');fillDir(id,'',list)};
    up.onclick=()=>pick.click();
    pick.onchange=async()=>{
      const fl=[...pick.files];pick.value='';
      const dir=t.cur;
      for(const f of fl)if(!await uploadOne(id,t,dir,f))break;
      const ul=t.uls.get(dir);if(ul&&ul.isConnected)fillDir(id,dir,ul);
    };
    if(termRefused)list.append(node('li','이 주소에서는 파일이 꺼져 있음','msg err'));
    else fillDir(id,'',list);
    return el;
  }
  // treeMsg shows a line under the bar ('' hides it).
  function treeMsg(t,text,cls){t.msg.hidden=!text;t.msg.textContent=text;t.msg.className='ws-files-msg msg '+(cls||'')}
  // treeCur makes dir the folder ⬆ 올리기 puts files in.
  function treeCur(t,dir){
    t.cur=dir;t.up.title='올리기: '+(dir?dir+'/':'세션 폴더');
    for(const b of t.list.querySelectorAll('.ws-tree-cur'))b.classList.remove('ws-tree-cur');
    if(dir)for(const b of t.list.querySelectorAll('.ws-tree-dir'))if(b.dataset.path===dir)b.classList.add('ws-tree-cur');
  }
  // fileSend sends a change of the files pane: DELETE, or POST with body.
  async function fileSend(id,method,path,body){
    let r;
    try{r=await fetch('/api/admin/'+F.url(id,path,false),{method:method,credentials:'same-origin',headers:{'Accept':'application/json','Content-Type':'application/json','X-Switchboard-Dashboard':'1'},body:body===undefined?undefined:JSON.stringify(body)})}
    catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
    if(r.status===401)signedOut();
    if(!r.ok){let b='';try{b=await r.text()}catch(e){}let code='';try{code=JSON.parse(b).code||''}catch(e){}const e=Error(F.problem(r.status,b));e.code=code;throw e}
    return r.json();
  }
  // b64 is a file's bytes as base64.
  function b64(file){
    return new Promise((ok,no)=>{const fr=new FileReader();fr.onload=()=>{const s=String(fr.result);ok(s.slice(s.indexOf(',')+1))};fr.onerror=()=>no(Error('읽기 실패'));fr.readAsDataURL(file)});
  }
  // uploadOne puts file in dir; a name already there asks first. false
  // stops the rest.
  async function uploadOne(id,t,dir,file){
    if(file.size>F.UPLOAD_MAX){treeMsg(t,file.name+' · 너무 큼 (4 MiB까지)','err');return true}
    treeMsg(t,file.name+' 올리는 중…');
    try{
      const data=await b64(file),send=replace=>fileSend(id,'POST',dir,{op:'upload',name:file.name,data:data,replace:replace});
      try{await send(false)}
      catch(e){
        if(e.code!=='exists')throw e;
        if(!confirm(F.join(dir,file.name)+' 덮어씀'))return(treeMsg(t,''),true);
        await send(true);
      }
      treeMsg(t,'');return true;
    }catch(e){treeMsg(t,file.name+' · '+e.message,'err');return false}
  }
  // rowActs puts ✎ and 🗑 on a row of path (in dir, listed into ul).
  function rowActs(id,t,line,b,e,dir,path,ul){
    const acts=node('span','','ws-tree-acts'),ren=node('button','✎','ws-tree-act'),del=node('button','🗑','ws-tree-act');
    ren.type=del.type='button';
    ren.title='이름 바꾸기';ren.setAttribute('aria-label',e.name+' 이름 바꾸기');
    del.title='지우기';del.setAttribute('aria-label',e.name+' 지우기');
    acts.append(ren,del);line.append(acts);
    const back=()=>{line.replaceChildren(b,acts);line.classList.remove('ws-tree-ask')};
    del.onclick=()=>{
      const yes=node('button','지움','sc-btn sc-btn--sm ws-tree-yes'),no=node('button','취소','sc-btn sc-btn--sm sc-btn--secondary');
      yes.type=no.type='button';line.classList.add('ws-tree-ask');
      line.replaceChildren(node('span',e.name+' 지움 · 되돌릴 수 없음','ws-tree-warn'),yes,no);
      no.onclick=()=>{back();del.focus()};
      line.onkeydown=k=>{if(k.key==='Escape'&&line.classList.contains('ws-tree-ask')){k.stopPropagation();back();del.focus()}};
      yes.onclick=async()=>{
        yes.disabled=no.disabled=true;
        try{await fileSend(id,'DELETE',path);treeMsg(t,'');gone(id,path,null);if(t.cur===path||t.cur.startsWith(path+'/'))treeCur(t,dir);fillDir(id,dir,ul)}
        catch(err){back();treeMsg(t,e.name+' · '+(err.code==='not-empty'?'비어 있지 않은 폴더':err.message),'err')}
      };
      no.focus();
    };
    ren.onclick=()=>{
      const inp=document.createElement('input');inp.className='ws-tree-input';inp.value=e.name;inp.spellcheck=false;inp.autocomplete='off';
      inp.setAttribute('aria-label',e.name+' 새 이름');
      line.replaceChildren(inp);inp.focus();
      const dot=e.kind==='dir'?-1:e.name.lastIndexOf('.');inp.setSelectionRange(0,dot>0?dot:e.name.length);
      let busy=false;
      const cancel=()=>{if(!busy){back();ren.focus()}};
      inp.onkeydown=async k=>{
        if(k.key==='Escape'){k.preventDefault();k.stopPropagation();cancel();return}
        if(k.key!=='Enter')return;
        k.preventDefault();
        const to=inp.value.trim();
        if(!to||to===e.name){cancel();return}
        busy=true;inp.disabled=true;
        try{const r=await fileSend(id,'POST',path,{op:'rename',to:to});treeMsg(t,'');gone(id,path,r.path);if(t.cur===path)treeCur(t,r.path);fillDir(id,dir,ul)}
        catch(err){busy=false;inp.disabled=false;inp.focus();treeMsg(t,to+' · '+(err.code==='exists'?'같은 이름이 있음':err.message),'err')}
      };
      inp.onblur=()=>setTimeout(()=>{if(line.contains(inp)&&document.activeElement!==inp)cancel()},0);
    };
  }
  // gone closes the panel of a file deleted (to null) or renamed, and opens
  // the renamed one in its place.
  function gone(id,path,to){
    const p=F.pid(id,path);
    if(!files.has(p))return;
    closePanel(p);
    if(to&&!files.has(p))openFile(id,to);
  }
  // fillDir lists dir of session id into ul: folders first, each opening
  // in place; a file opens as a centre panel.
  async function fillDir(id,dir,ul){
    const t=trees.get(id);
    if(t)t.uls.set(dir,ul);
    ul.replaceChildren(node('li','…','empty'));
    let ls;
    try{ls=await filesGet(id,dir,false)}
    catch(e){ul.replaceChildren(node('li',e.message,'msg err'));return}
    const rows=(ls.entries||[]).map(e=>{
      const li=node('li','','ws-tree-row'),line=node('div','','ws-tree-line'),path=F.join(dir,e.name),dirRow=e.kind==='dir';
      li.setAttribute('role','treeitem');
      const b=node('button','','ws-tree-item ws-tree-'+e.kind);b.type='button';b.title=path;b.dataset.path=path;
      if(t&&dirRow&&t.cur===path)b.classList.add('ws-tree-cur');
      b.append(node('span',dirRow?'▸':'','ws-tree-mark'),node('span',e.name,'ws-tree-name'));
      if(e.kind==='file')b.append(node('span',F.size(e.size),'ws-tree-size'));
      line.append(b);li.append(line);
      if(t&&can('files'))rowActs(id,t,line,b,e,dir,path,ul);
      if(dirRow){
        li.setAttribute('aria-expanded','false');
        const sub=node('ul','','ws-tree');sub.setAttribute('role','group');sub.hidden=true;li.append(sub);
        b.onclick=()=>{
          const open=sub.hidden;sub.hidden=!open;li.setAttribute('aria-expanded',String(open));b.firstChild.textContent=open?'▾':'▸';
          if(open)fillDir(id,path,sub);
          if(t)treeCur(t,open?path:t.cur===path||t.cur.startsWith(path+'/')?dir:t.cur);
        };
      }else if(e.kind==='file')b.onclick=()=>openFile(id,path);
      else b.disabled=true;
      return li;
    });
    if(!rows.length)rows.push(node('li','비어 있음','empty'));
    if(ls.truncated)rows.push(node('li','처음 '+rows.length+'개','empty'));
    ul.replaceChildren(...rows);
  }
  // openFile shows a file of session id's folder as a centre panel.
  function openFile(id,path){
    const p=F.pid(id,path);
    st=L.open(st,p);if(st.max&&st.max!==p)st=L.setMax(st,null);
    if(files.has(p))drawFile(p);
    sideDrawer(false);commit();
  }
  // filePane is a file panel's element: the path, its size, 저장 and the
  // editor (#448), or why the file does not open as text.
  function filePane(p){
    let f=files.get(p);if(f)return f.el;
    const x=F.fileOf(p),el=node('div','','ws-file'),head=node('div','','ws-item-head'),h=node('h2',x.path),meta=node('span','','ws-file-meta'),
      save=node('button','저장','sc-btn sc-btn--sm ws-file-save'),re=node('button','↻','ws-files-re'),bar=node('div','','ws-file-bar'),body=node('div','','ws-file-body');
    save.type='button';save.hidden=true;save.disabled=true;save.title='저장 (Ctrl+S)';
    re.type='button';re.title='다시 읽기';re.setAttribute('aria-label','다시 읽기');h.title=x.path;
    bar.hidden=true;bar.setAttribute('role','status');
    head.append(h,meta,save,re);el.append(head,bar,body);
    f={el:el,meta:meta,save:save,bar:bar,body:body,seq:0,ed:null,fp:'',busy:false};files.set(p,f);
    re.onclick=()=>{if(fileDirty(f)&&!confirm('저장하지 않은 고침을 버림'))return;drawFile(p)};
    save.onclick=()=>saveFile(p,false);
    drawFile(p);
    return el;
  }
  function fileDirty(f){return !!(f&&f.ed&&!f.ed.clean())}
  // fileBar shows a line above the editor: a message, and buttons.
  function fileBar(f,text,cls,buttons){
    if(!text){f.bar.hidden=true;f.bar.replaceChildren();return}
    f.bar.className='ws-file-bar '+(cls||'');
    f.bar.replaceChildren(node('span',text),...(buttons||[]));f.bar.hidden=false;
  }
  function fileMarks(p){
    const f=files.get(p);if(!f||!f.ed)return;
    f.save.disabled=!fileDirty(f)||f.busy;
    retitle(p);
  }
  // editorLib loads the editor's files once; null after a failure, so the
  // next file tries again (and opens in a plain text box meanwhile).
  let editorLib=null;
  function loadEditor(){
    if(!editorLib)editorLib=loadAssets(editorAssets).then(()=>{if(typeof CodeMirror!=='function')throw Error('불러오기 실패')}).catch(e=>{editorLib=null;throw e});
    return editorLib;
  }
  // makeEditor puts an editor of text into parent: CodeMirror with the
  // path's colouring, or a plain text box without it. clean() is whether
  // it has the text last marked (mark()).
  function makeEditor(parent,text,path,cm,changed,save){
    if(cm){
      const ed=CodeMirror(parent,{value:text,mode:F.modeOf(path),lineNumbers:true,readOnly:!can('files'),indentUnit:F.modeOf(path)==='go'?8:2,indentWithTabs:F.modeOf(path)==='go',
        tabSize:F.modeOf(path)==='go'?8:4,lineWrapping:false,extraKeys:{'Ctrl-S':save,'Cmd-S':save}});
      let gen=ed.changeGeneration();
      ed.on('changes',changed);
      // A panel moved, shown or resized draws again.
      if(typeof ResizeObserver==='function')new ResizeObserver(()=>ed.refresh()).observe(parent);
      return {clean:()=>ed.isClean(gen),mark:()=>{gen=ed.changeGeneration()},get:()=>ed.getValue(),focus:()=>ed.focus()};
    }
    const ta=node('textarea','','ws-text ws-edit');ta.value=text;ta.spellcheck=false;ta.readOnly=!can('files');ta.setAttribute('aria-label',path);
    let saved=text;
    ta.oninput=changed;
    ta.onkeydown=e=>{if((e.ctrlKey||e.metaKey)&&e.key==='s'){e.preventDefault();save()}};
    parent.append(ta);
    return {clean:()=>ta.value===saved,mark:()=>{saved=ta.value},get:()=>ta.value,focus:()=>ta.focus()};
  }
  // readFile is the text of session id's path and its fingerprint.
  async function readFile(id,path){
    let r;
    try{r=await fetch('/api/admin/'+F.url(id,path,true),{credentials:'same-origin',headers:{'Accept':'*/*'}})}
    catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
    if(r.status===401)signedOut();
    if(r.redirected)throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
    if(!r.ok){let b='';try{b=await r.text()}catch(e){}throw Error(F.problem(r.status,b))}
    return {bytes:new Uint8Array(await r.arrayBuffer()),fp:r.headers.get(F.FP_HEADER)||''};
  }
  async function drawFile(p){
    const f=files.get(p),x=F.fileOf(p);if(!f||!x)return;
    const seq=++f.seq;
    f.ed=null;f.fp='';f.save.hidden=true;fileBar(f,'');retitle(p);
    try{
      const ls=await filesGet(x.session,x.path,false);
      if(seq!==f.seq)return;
      f.meta.textContent=F.size(ls.stat.size);
      if(ls.stat.kind!=='file'){f.body.replaceChildren(node('p','글 파일 아님','empty'));return}
      const kind=F.kindOf(x.path);
      if(kind!=='text'){
        // The browser streams the bytes itself from /files/raw: Range (206)
        // seeks audio and video; an svg is an <img>, never run.
        const m=document.createElement(kind==='image'?'img':kind);
        m.className='ws-media ws-'+kind;m.src='/api/admin/'+F.url(x.session,x.path,true,seq);
        if(kind==='image')m.alt=F.base(x.path);else{m.controls=true;m.preload='metadata'}
        m.onerror=()=>{if(seq===f.seq)f.body.replaceChildren(node('p','열 수 없음','msg err'))};
        f.body.replaceChildren(m);return;
      }
      if(ls.stat.size>F.TEXT_MAX){f.body.replaceChildren(node('p','너무 큼','empty'));return}
      const [rd,cm]=await Promise.all([readFile(x.session,x.path),loadEditor().then(()=>true,()=>false)]);
      if(seq!==f.seq)return;
      const t=F.textOf(rd.bytes.length,rd.bytes);
      if(t.why){f.body.replaceChildren(node('p',t.why,'empty'));return}
      f.meta.textContent=F.size(rd.bytes.length);
      f.body.replaceChildren();
      f.fp=rd.fp;
      f.ed=makeEditor(f.body,t.text,x.path,cm,()=>fileMarks(p),()=>saveFile(p,false));
      f.save.hidden=!f.fp||!can('files');
      fileMarks(p);
    }catch(e){if(seq===f.seq)f.body.replaceChildren(node('p',e.message,'msg err'))}
  }
  // saveFile writes the editor's text back; anyway saves over a change
  // made elsewhere since the read (그래도 저장).
  async function saveFile(p,anyway){
    if(!can('files'))return;
    const f=files.get(p),x=F.fileOf(p);if(!f||!x||!f.ed||f.busy||!f.fp)return;
    if(!anyway&&f.ed.clean())return;
    const text=f.ed.get(),seq=f.seq;
    if(new TextEncoder().encode(text).length>F.TEXT_MAX){fileBar(f,'너무 큼','err');return}
    f.busy=true;fileMarks(p);fileBar(f,'저장 중…','muted');
    let r;
    try{r=await fetch('/api/admin/'+F.url(x.session,x.path,true),{method:'PUT',credentials:'same-origin',
      headers:{'Accept':'application/json','Content-Type':'application/json','X-Switchboard-Dashboard':'1',[F.FP_HEADER]:anyway?'*':f.fp},
      body:JSON.stringify({text:text})})}
    catch(e){r=null}
    f.busy=false;
    if(seq!==f.seq||files.get(p)!==f)return;
    if(!r){fileBar(f,'네트워크 오류 · 연결과 로그인 확인','err');fileMarks(p);return}
    if(r.status===401)signedOut();
    if(r.ok){
      let out=null;try{out=await r.json()}catch(e){}
      if(!out||!out.fingerprint){fileBar(f,'로그인 필요 · 다시 로그인한 뒤 새로 고침','err');fileMarks(p);return}
      f.fp=out.fingerprint;f.meta.textContent=F.size(out.stat.size);
      // Typing during the save stays unsaved.
      if(f.ed.get()===text)f.ed.mark();
      fileBar(f,'');fileMarks(p);
      return;
    }
    let b='';try{b=await r.text()}catch(e){}
    if(r.status===409){
      const again=node('button','다시 읽기','sc-btn sc-btn--sm sc-btn--secondary'),over=node('button','그래도 저장','sc-btn sc-btn--sm');
      again.type=over.type='button';
      again.onclick=()=>drawFile(p);over.onclick=()=>saveFile(p,true);
      fileBar(f,F.problem(409,b),'warn',[again,over]);
    }else fileBar(f,r.status===403&&!b.includes('"code"')?'쓰기 거부(403) · 같은 주소에서 열었는지 확인':F.problem(r.status,b),'err');
    fileMarks(p);
  }
  // itemPane is an item panel's element, drawn by its tab's detail spec.
  function itemPane(p){
    let it=items.get(p);if(it)return it.el;
    const el=node('div','','ws-item'),head=node('div','','ws-item-head'),h=node('h2',''),b=node('span','','sc-badge'),tabs=node('div','','ws-subs'),body=node('div','','ws-item-body');
    tabs.setAttribute('role','tablist');b.hidden=true;h.textContent=title(p);
    head.append(h,b);el.append(head,tabs,body);
    it={el:el,h:h,b:b,tabs:tabs,body:body,sub:'',seq:0};items.set(p,it);
    drawItem(p);
    return el;
  }
  async function drawItem(p){
    const it=items.get(p),x=S.itemOf(p);if(!it||!x)return;
    const spec=details[x.tab],seq=++it.seq,out=document.createElement('div');
    let subs=[];
    try{
      const v=await spec.load(x.id);
      if(seq!==it.seq)return;
      if(!v)out.append(node('p','없음 · 지워졌거나 바뀜','empty'));
      else{
        const h=spec.head(v);subs=spec.subs(v);
        if(!subs.some(s=>s[0]===it.sub))it.sub=subs[0][0];
        it.h.textContent=h.title;titles.set(p,h.title);retitle(p);
        it.b.hidden=!h.badge;
        if(h.badge){it.b.textContent=h.badge[0];it.b.className='sc-badge '+(toneClass[h.badge[1]]||toneClass.neutral)}
        await spec.draw(v,it.sub,out);
      }
    }catch(e){out.replaceChildren(node('p',e.message,'msg err'))}
    if(seq!==it.seq)return;
    subTabs(it.tabs,subs.length>1?subs:[],it.sub,k=>{it.sub=k;drawItem(p)});
    const a=document.activeElement,inBody=it.body.contains(a);
    it.body.replaceChildren(...out.childNodes);
    if(inBody&&a.isConnected&&it.body.contains(a))a.focus({preventScroll:true});
  }
  // Redraws gather for 300ms; a view with a field in use waits.
  let dirty=new Set(),dirtyT=0;
  function mark(...ks){for(const k of ks)dirty.add(k);if(!dirtyT)dirtyT=setTimeout(flush,300)}
  function busy(el){const a=document.activeElement;return !!a&&el.contains(a)&&a.matches('input,textarea,select')}
  function flush(){
    dirtyT=0;const later=new Set();
    for(const k of dirty){
      if(k==='needs')loadNeeds();
      else if(k==='sess'){if(busy(sessBody))later.add(k);else drawSess()}
      else if(k==='items'){if([...items.values()].some(it=>busy(it.body)))later.add(k);else for(const p of items.keys())drawItem(p)}
    }
    dirty=later;if(later.size)dirtyT=setTimeout(flush,2000);
  }
  // The 노드 index (#446): a session opens its terminal panel, which the
  // right column then follows.
  const nodeIdx=wsNodeIndex({openSession(v){
    openTerm(v);
    if(mq.matches)drawer(false);
  }});
  for(const p of L.panels(st))if(p.startsWith('term:'))term(p.slice(5));
  if(location.hash.length>1&&curTab)st=L.open(st,'menu:'+curTab);
  side.session=S.follow(side.session,st.focus);
  render();drawSess();loadNeeds();
  return {
    // showMenu opens (or goes to) the section of tab name.
    showMenu(name){
      const p='menu:'+name,g=L.groupOf(st,p);
      if(g&&g.active===p&&(st.focus===p||!mq.matches)&&(!st.max||st.max===p))return;
      st=L.open(st,p);if(st.max&&st.max!==p)st=L.setMax(st,null);commit();
    },
    // openTerm shows a hosted session's terminal panel (the left pane's
    // session cards, #510); session is the session the column follows.
    openTerm(v){openTerm(v);if(mq.matches)drawer(false)},
    session(){return side.session},
    // shown says whether tab name's section is on screen now.
    shown(name){return shownPanels().includes('menu:'+name)},
    // termPaneFor stands in a detail body for the terminal screen: it opens
    // the terminal's panel the first time the detail shows it.
    termPaneFor(v){
      if(autoFor!==v.id){autoFor=v.id;openTerm(v)}
      const b=node('button','화면 열기');b.type='button';b.onclick=()=>openTerm(v);
      return b;
    },
    // termGc: once the detail closes, showing it again opens the panel again.
    termGc(){if(!detailCur)autoFor=null;if(!!detailCur===!!sessShown)drawSess()},
    // live takes the kinds of a live change (#445): the column and the item
    // panels redraw with them.
    live(kinds){
      if(kinds.includes('questions')||kinds.includes('permissions'))mark('needs','items');
      if(kinds.includes('terminals'))mark('sess');
      nodeIdx.live(kinds);
    },
    // refresh redraws the column and the item panels after an action.
    refresh(){mark('needs','sess','items')},
  };
}
