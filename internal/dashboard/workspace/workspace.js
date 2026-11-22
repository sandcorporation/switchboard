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
function wsStart(){
  const L=wsLayout,KEY='switchboard.workspace',DT='application/x-switchboard-panel';
  const mq=window.matchMedia('(max-width:600px)');
  const main=document.querySelector('.sc-app-page');
  const box=node('div','','ws');box.id='ws';
  main.before(box);main.hidden=true;
  const html=document.documentElement;html.classList.add('ws-on');
  const scrim=node('div','','ws-scrim');scrim.onclick=()=>drawer(false);document.body.append(scrim);
  const terms=new Map(),wraps=new Map(),titles=new Map();
  let autoFor=null,dragging=false;
  function valid(p){
    if(p.startsWith('menu:'))return visibleTabs().includes(p.slice(5));
    return p.startsWith('term:')&&p.length>5;
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
    return titles.get(p)||p.slice(5);
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
    return m?$('panel-'+m):term(p.slice(5)).el;
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
  $('tabs').addEventListener('click',e=>{if(e.target.closest('[role=tab]'))drawer(false)});
  // Esc: a typing terminal keeps it; otherwise the drawer closes, then
  // 넓히기 returns to the normal size, and only then does the detail panel's
  // own Esc close the detail.
  document.addEventListener('keydown',e=>{
    if(e.key!=='Escape'||e.defaultPrevented)return;
    if(e.target.closest&&e.target.closest('dialog'))return;
    if(html.classList.contains('ws-left-open')){e.preventDefault();drawer(false);return}
    if(!st.max)return;
    const t=terms.get(st.max.slice(5));
    if(t&&t.mode==='input'&&t.screen.contains(e.target))return;
    e.preventDefault();maximize(st.max);
  },true);
  mq.addEventListener('change',()=>{drawer(false);render()});
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
  for(const p of L.panels(st))if(p.startsWith('term:'))term(p.slice(5));
  if(location.hash.length>1&&curTab)st=L.open(st,'menu:'+curTab);
  render();
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
    termGc(){if(!detailCur)autoFor=null},
  };
}
