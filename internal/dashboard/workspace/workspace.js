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
function wsStart(){
  const L=wsLayout,S=wsSide,KEY='switchboard.workspace',SKEY=KEY+'.side',DT='application/x-switchboard-panel';
  const mq=window.matchMedia('(max-width:600px)');
  const main=document.querySelector('.sc-app-page');
  const box=node('div','','ws');box.id='ws';
  main.before(box);main.hidden=true;
  const html=document.documentElement;html.classList.add('ws-on');
  const scrim=node('div','','ws-scrim');scrim.onclick=()=>{drawer(false);sideDrawer(false)};document.body.append(scrim);
  const terms=new Map(),wraps=new Map(),titles=new Map(),items=new Map();
  let autoFor=null,dragging=false;
  function valid(p){
    if(p.startsWith('menu:'))return visibleTabs().includes(p.slice(5));
    return (p.startsWith('term:')&&p.length>5)||!!S.itemOf(p);
  }
  function load(){
    try{const s=L.parse(localStorage.getItem(KEY)||'',valid);if(s)return s}catch(e){}
    return L.initial('menu:'+(curTab||'progress'));
  }
  function save(){try{localStorage.setItem(KEY,L.serialize(st))}catch(e){}}
  let st=load();
  const menuOf=p=>p.startsWith('menu:')?p.slice(5):'';
  function title(p){
    const m=menuOf(p);
    if(m){const t=$('tab-'+m);return t?t.querySelector('.tab-name').textContent:m}
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
      const m=node('button','☰','ws-menu');m.type='button';m.title='메뉴';m.setAttribute('aria-label','메뉴');m.setAttribute('aria-expanded',String(html.classList.contains('ws-left-open')));
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
    const m=menuOf(p),w=wraps.get(p);
    if(m)main.append($('panel-'+m));
    else if(S.itemOf(p)){const it=items.get(p);if(it)it.seq++;items.delete(p)}
    else{const id=p.slice(5),t=terms.get(id);if(t){termStop(t);terms.delete(id)}if(autoFor===id)autoFor=null}
    if(w)w.remove();
    wraps.delete(p);titles.delete(p);
    st=L.close(st,p);commit();
  }
  function maximize(p){st=L.setMax(st,st.max===p?null:p);commit()}
  function drawer(on){
    html.classList.toggle('ws-left-open',on);
    for(const b of box.querySelectorAll('.ws-menu'))b.setAttribute('aria-expanded',String(on));
  }
  function sideDrawer(on){
    html.classList.toggle('ws-right-open',on);
    for(const b of box.querySelectorAll('.ws-sidebtn'))b.setAttribute('aria-expanded',String(on));
    if(on)sideX.focus({preventScroll:true});
  }
  $('tabs').addEventListener('click',e=>{if(e.target.closest('[role=tab]'))drawer(false)});
  // Esc: a typing terminal keeps it; otherwise the drawer closes, then
  // 넓히기 returns to the normal size, and only then does the detail panel's
  // own Esc close the detail.
  document.addEventListener('keydown',e=>{
    if(e.key!=='Escape'||e.defaultPrevented)return;
    if(e.target.closest&&e.target.closest('dialog'))return;
    if(html.classList.contains('ws-left-open')){e.preventDefault();drawer(false);return}
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
  const sideEl=node('aside','','ws-side');sideEl.id='wsSide';sideEl.setAttribute('aria-label','답이 필요함 · 세션');
  const sideX=node('button','×','ws-side-x');sideX.type='button';sideX.title='닫기';sideX.setAttribute('aria-label','닫기');sideX.onclick=()=>sideDrawer(false);
  const needBox=node('section','','ws-needs'),needHead=node('h2','답이 필요함 '),needCount=node('span','','sc-chip-count');
  const needList=node('ul','','ws-needlist');
  needHead.append(needCount);needBox.append(needHead,needList);
  const sessBox=node('section','','ws-sess'),sessHead=node('div','','ws-sess-head'),sessTitle=node('h2',''),sessBadge=node('span','','sc-badge');
  const sessTabs=node('div','','ws-subs'),sessBody=node('div','','ws-sess-body');
  sessTabs.setAttribute('role','tablist');sessBody.setAttribute('role','tabpanel');sessBody.id='wsSessBody';sessBadge.hidden=true;
  sessHead.append(sessTitle,sessBadge);sessBox.append(sessHead,sessTabs,sessBody);
  sideEl.append(sideX,needBox,sessBox);
  $('appMain').append(sideEl);
  let needN=0,needSeq=0,sessSeq=0,sessShown='';
  // markNeeds marks the row whose panel has focus.
  function markNeeds(){for(const b of needList.querySelectorAll('.ws-need'))if(b.dataset.p===st.focus)b.setAttribute('aria-current','true');else b.removeAttribute('aria-current')}
  async function loadNeeds(){
    const seq=++needSeq;
    try{
      const [q,pm]=await Promise.all([api('questions?status=pending&limit=500'),api('permissions?status=pending')]);
      if(seq!==needSeq)return;
      const rows=S.needs(q.questions,pm.permissions),a=document.activeElement,was=needList.contains(a)?a.dataset.p:'';
      needN=rows.length;needCount.textContent=String(rows.length);
      for(const c of box.querySelectorAll('.ws-sidecount'))c.textContent=needN?String(needN):'';
      needList.replaceChildren(...rows.map(r=>{
        const li=node('li',''),b=node('button','','ws-need');b.type='button';b.dataset.p=r.pid;b.title=r.title;
        const ask=r.kind==='ask';
        b.append(badge(ask?'질문':'권한',ask?'warning':'info'),node('span',r.title,'ws-need-title'),node('span',r.who+' · '+when(r.at),'ws-need-who'));
        b.onclick=()=>openItem(r.pid);li.append(b);return li;
      }));
      if(!rows.length)needList.append(node('li','대기 없음','empty'));
      markNeeds();
      if(was)for(const b of needList.querySelectorAll('.ws-need'))if(b.dataset.p===was)b.focus({preventScroll:true});
    }catch(e){if(seq===needSeq)needList.replaceChildren(node('li',e.message,'msg err'))}
  }
  // openItem shows a question or permission request as a centre panel.
  function openItem(p){
    st=L.open(st,p);if(st.max&&st.max!==p)st=L.setMax(st,null);
    const it=items.get(p);if(it)drawItem(p);
    sideDrawer(false);commit();
  }
  function follow(){
    const id=S.follow(side.session,st.focus);
    if(id!==side.session){side.session=id;saveSide();drawSess()}
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
    subTabs(sessTabs,id?S.SUBS:[],side.sub,k=>{side.sub=k;saveSide();drawSess().then(()=>{const b=sessTabs.querySelector('[aria-selected=true]');if(b)b.focus()})});
    if(detailCur){sessShown='';sessBody.replaceChildren();return}
    sessShown=id||'-';
    if(!id){sessTitle.textContent='세션';sessBadge.hidden=true;sessBody.replaceChildren(node('p','세션 없음','empty'));return}
    const out=document.createElement('div');
    let v=null;
    try{
      v=await termLoad(id);
      if(seq!==sessSeq)return;
      if(v){
        termActs(v,out);
        if(side.sub==='autos')await termAutos(v,out);
        else if(side.sub==='settings')termSettings(v,out);
        else await termHistory(v,out);
      }else out.append(node('p','없음 · 지워졌거나 바뀜','empty'));
    }catch(e){out.append(node('p',e.message,'msg err'))}
    if(seq!==sessSeq)return;
    const h=v?details.terminals.head(v):{title:titles.get('term:'+id)||id,badge:null};
    sessTitle.textContent=h.title;sessBadge.hidden=!h.badge;
    if(h.badge){sessBadge.textContent=h.badge[0];sessBadge.className='sc-badge '+(toneClass[h.badge[1]]||toneClass.neutral)}
    const top=sideEl.scrollTop,a=document.activeElement,inBody=sessBody.contains(a);
    sessBody.replaceChildren(...out.childNodes);
    sideEl.scrollTop=top;
    if(inBody&&!sessBody.contains(document.activeElement))sessTitle.tabIndex=-1,sessTitle.focus({preventScroll:true});
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
    },
    // refresh redraws the column and the item panels after an action.
    refresh(){mark('needs','sess','items')},
  };
}
