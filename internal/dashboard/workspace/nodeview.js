// ---- 노드 색인 (#446): the workspace's second left index ----
// The side menu gains two index tabs: 메뉴 (the tab list as it was) and 노드
// (a node → session tree). A node row shows its state, version (marked when
// below the server's), session count and last contact; opened, it shows the
// agents' remaining usage, the roles granted on it with their directory
// roots, its sessions and its admin actions. 노드 붙이기 makes a one-time
// invite code (shown once, never kept here), 끊기 and 거두기 ask once.
// Runs on the page's globals (node, api, apiSend, setMsg, badge, when,
// shortWhen, termRole, termSt, termStateText, detailHash) and wsNodes.
function wsNodeIndex(opts){
  const N=wsNodes,KEY='switchboard.workspace.index';
  const html=document.documentElement,tabs=$('lpMain');
  const idx=node('div','','ws-idx');idx.setAttribute('role','tablist');idx.setAttribute('aria-label','색인');
  const box=node('section','','ws-nodes');box.id='wsNodes';box.setAttribute('role','tabpanel');box.setAttribute('aria-label','노드');box.hidden=true;
  const btns={};
  for(const [k,t] of [['menu','메뉴'],['nodes','노드']]){
    const b=node('button',t,'ws-idx-tab');b.type='button';b.id='wsIdx-'+k;b.setAttribute('role','tab');
    b.onclick=()=>show(k,true);btns[k]=b;idx.append(b);
  }
  idx.addEventListener('keydown',e=>{
    if(e.key!=='ArrowLeft'&&e.key!=='ArrowRight'&&e.key!=='ArrowUp'&&e.key!=='ArrowDown')return;
    e.preventDefault();show(cur==='menu'?'nodes':'menu',true);btns[cur].focus();
  });
  $('lpFoot').prepend(idx);tabs.after(box);
  const bar=node('div','','ws-nodes-bar');
  const addBtn=node('button','노드 붙이기');addBtn.type='button';
  const note=node('span','','muted');note.setAttribute('role','status');note.setAttribute('aria-live','polite');
  bar.append(addBtn,note);
  const inv=node('form','','ws-invite');inv.hidden=true;
  const invName=node('input');invName.name='name';invName.required=true;invName.autocomplete='off';invName.spellcheck=false;invName.placeholder='pc-2';invName.setAttribute('aria-label','새 노드 이름');invName.maxLength=64;
  const invGo=node('button','초대 코드 만들기');invGo.type='submit';
  const invMsg=node('p','','msg');
  inv.append(invName,invGo,invMsg);
  const got=node('div','','ws-invited');got.hidden=true;
  const tree=node('ul','','ws-tree');
  box.append(bar,inv,got,tree);
  addBtn.onclick=()=>{inv.hidden=!inv.hidden;setMsg(invMsg,'');if(!inv.hidden)invName.focus()};
  inv.onsubmit=async e=>{
    e.preventDefault();invGo.disabled=true;setMsg(invMsg,'');
    try{
      const r=await apiSend('POST','node-invites',{name:invName.value.trim()});
      inv.reset();inv.hidden=true;showCode(r);
    }catch(err){setMsg(invMsg,err.message,'err')}
    invGo.disabled=false;
  };
  // showCode shows an invite code once; closing it drops it from the page.
  function showCode(r){
    const code=node('code',r.code,'ws-code');
    const copy=node('button','복사');copy.type='button';
    copy.onclick=async()=>{try{await navigator.clipboard.writeText(r.code);copy.textContent='복사됨'}catch(e){copy.textContent='복사 실패'}};
    const x=node('button','×','ws-x');x.type='button';x.title='닫기';x.setAttribute('aria-label','초대 코드 닫기');
    x.onclick=()=>{got.replaceChildren();got.hidden=true};
    const head=node('div','','ws-invited-head');head.append(node('strong',r.name),node('span',new Date(r.expires_at).toLocaleTimeString('ko-KR',{hour:'2-digit',minute:'2-digit'})+'까지','muted'),x);
    const cmd=node('code','switchboard node --host '+location.origin+' --join '+r.code,'ws-cmd');
    got.replaceChildren(head,code,copy,cmd);got.hidden=false;
  }
  let cur='menu',stale=true,timer=0,serverVersion=null,last=null;
  const open=new Set(),msgs=new Map();
  try{if(localStorage.getItem(KEY)==='nodes')cur='nodes'}catch(e){}
  function show(k,store){
    cur=k;
    for(const [n,b] of Object.entries(btns)){const on=n===k;b.setAttribute('aria-selected',String(on));b.tabIndex=on?0:-1}
    tabs.hidden=k!=='menu';box.hidden=k!=='nodes';
    html.classList.toggle('ws-idx-nodes',k==='nodes');
    if(store)try{localStorage.setItem(KEY,k)}catch(e){}
    if(k==='nodes'&&stale)load();
  }
  async function load(){
    stale=false;
    if(!tree.childElementCount)note.textContent='불러오는 중…';
    try{
      if(serverVersion===null)serverVersion=(await api('settings')).version||'';
      const [nodes,recs,grants]=await Promise.all([api('nodes'),api('terminals'),api('host-grants')]);
      last=[nodes,recs,grants];draw(nodes,recs,grants);
      const on=nodes.filter(n=>N.state(n)==='on').length;
      note.textContent=nodes.length?on+'/'+nodes.length+' 붙음':'';
    }catch(e){note.textContent=e.message}
  }
  function draw(nodes,recs,grants){
    const focus=document.activeElement&&box.contains(document.activeElement)?document.activeElement.dataset.key:'';
    tree.replaceChildren();
    for(const n of nodes)tree.append(nodeItem(n,N.sessionsOn(recs,n.name),grants));
    if(!nodes.length)tree.append(node('li','노드 없음','empty'));
    if(focus){const el=tree.querySelector('[data-key="'+CSS.escape(focus)+'"]');if(el)el.focus()}
  }
  function nodeItem(n,sess,grants){
    const st=N.state(n),li=node('li','','ws-node ws-st-'+st),on=open.has(n.name);
    const row=node('button','','ws-node-row');row.type='button';row.dataset.key='n:'+n.name;row.setAttribute('aria-expanded',String(on));
    const dot=node('span','','ws-dot');dot.title=N.stateText[st];
    const name=node('span',n.name,'ws-node-name mono');
    const meta=node('span','','ws-node-meta');
    meta.append(node('span',N.stateText[st]));
    if(n.version){
      const v=node('span',n.version,'mono');
      if(N.older(n.version,serverVersion)){v.className='mono ws-old';v.title='서버 '+serverVersion}
      meta.append(v);
    }
    meta.append(node('span','세션 '+sess.length));
    const seen=node('span',n.last_seen_at?shortWhen(n.last_seen_at):'-');seen.title='마지막 접속 '+when(n.last_seen_at);
    meta.append(seen);
    row.append(dot,name,meta);
    row.onclick=()=>{if(open.has(n.name))open.delete(n.name);else open.add(n.name);if(last)draw(...last)};
    li.append(row);
    if(on)li.append(nodeBody(n,sess,grants));
    return li;
  }
  function section(title){const s=node('div','','ws-sec');s.append(node('h4',title));return s}
  function nodeBody(n,sess,grants){
    const b=node('div','','ws-node-body'),msg=node('p','','msg');
    const m=msgs.get(n.name);if(m){setMsg(msg,m[0],m[1]);msgs.delete(n.name)}
    // Sessions first: they are what a click opens.
    const ss=section('세션'),sl=node('ul','','ws-sess');
    for(const v of sess){
      const li=node('li'),a=node('button','','ws-sess-row');a.type='button';a.dataset.key='s:'+v.id;
      const t=termSt(v);
      a.append(node('span',termRole(v)+(v.label?' · '+v.label:''),'ws-sess-name'),node('span',termStateText(t,v.runtime||{}),'muted'));
      if(v.detached){const d=node('span','못 붙음','sc-badge sc-badge--danger');d.title='실행 중이지만 메일함 슬롯이 붙어 있지 않음';a.append(d)}
      a.title=v.directory||'';
      a.onclick=()=>opts.openSession(v);
      li.append(a);sl.append(li);
    }
    if(!sess.length)sl.append(node('li','-','muted'));
    ss.append(sl);
    const us=section('남은 한도'),ul=node('ul','','ws-plain');
    for(const u of n.usage||[]){
      const li=node('li',N.usageText(u));
      if(u.resets_at)li.title='다시 참 '+when(u.resets_at);
      ul.append(li);
    }
    if(!(n.usage||[]).length)ul.append(node('li','-','muted'));
    us.append(ul);
    const gs=section('허가'),gl=node('ul','','ws-plain');
    const mine=N.grantsOn(grants,n.name);
    for(const g of mine){
      const li=node('li','','ws-grant');
      const t=node('span','','ws-grant-text');t.append(node('span',g.role,'mono'),node('span',(g.dir_roots||[]).join(', '),'mono muted'));
      const x=node('button','거두기');x.type='button';x.dataset.key='g:'+n.name+':'+g.role;
      x.onclick=async()=>{
        if(!confirm(g.role+' 역할의 '+n.name+' 허가를 거둘까요?'))return;
        const w=N.grantWithout(g,n.name);
        try{
          if(w.method==='DELETE')await apiSend('DELETE','host-grants?role='+encodeURIComponent(g.role));
          else await apiSend('POST','host-grants',w.body);
msgs.set(n.name,[g.role+' 허가를 거뒀습니다.','ok'])}
        catch(e){msgs.set(n.name,[e.message,'err'])}
        load();
      };
      li.append(t,x);gl.append(li);
    }
    if(!mine.length)gl.append(node('li','-','muted'));
    gs.append(gl,grantForm(n,grants));
    const hs=section('기록'),hl=node('ul','','ws-plain ws-log');hl.append(node('li','…','muted'));
    hs.append(hl);
    api('admin-actions?target='+encodeURIComponent(n.name)+'&limit=5').then(rows=>{
      hl.replaceChildren();
      for(const a of rows){const li=node('li',shortWhen(a.at)+' '+N.actionName(a.action)+' · '+a.admin);li.title=when(a.at)+(a.method?' · '+a.method:'')+(a.addr?' · '+a.addr:'');hl.append(li)}
      if(!rows.length)hl.append(node('li','-','muted'));
    }).catch(e=>{hl.replaceChildren(node('li',e.message,'msg err'))});
    b.append(msg,ss,us,gs,hs);
    if(N.state(n)!=='revoked'){
      const acts=node('div','','ws-node-acts');
      const cut=node('button','끊기');cut.type='button';cut.dataset.key='x:'+n.name;
      cut.onclick=async()=>{
        if(!confirm(n.name+' 노드를 끊을까요? 연결이 바로 끊기고 이 노드의 토큰은 다시 쓸 수 없습니다.'))return;
        try{await apiSend('DELETE','nodes/'+encodeURIComponent(n.name));msgs.set(n.name,[n.name+' 노드를 끊었습니다.','ok'])}
        catch(e){msgs.set(n.name,[e.message,'err'])}
        load();
      };
      acts.append(cut);b.append(acts);
    }
    return b;
  }
  function grantForm(n,grants){
    const f=node('form','','ws-grant-form');
    const field=(name,label)=>{const i=node('input');i.name=name;i.placeholder=label;i.autocomplete='off';i.spellcheck=false;i.setAttribute('aria-label',label);i.dataset.key='f:'+n.name+':'+name;return i};
    const role=field('role','역할'),bases=field('bases','기반 역할'),roots=field('roots','폴더 뿌리');
    role.required=true;
    const go=node('button','허가 주기');go.type='submit';
    f.append(role,bases,roots,go);
    f.onsubmit=async e=>{
      e.preventDefault();
      const r=role.value.trim(),had=(grants||[]).find(g=>g.role===r)||null;
      go.disabled=true;
      try{await apiSend('POST','host-grants',N.grantWith(had,n.name,r,bases.value,roots.value));msgs.set(n.name,[r+' 역할에 허가를 줬습니다.','ok'])}
      catch(err){msgs.set(n.name,[err.message,'err'])}
      go.disabled=false;load();
    };
    return f;
  }
  // live redraws the tree on a hosting or settings change; hidden, it waits
  // until the tab shows. A focused field keeps its text: the redraw waits.
  function live(kinds){
    if(!kinds.includes('terminals')&&!kinds.includes('settings'))return;
    if(box.hidden){stale=true;return}
    if(!timer)timer=setTimeout(flush,300);
  }
  function flush(){
    timer=0;
    const a=document.activeElement;
    if(a&&box.contains(a)&&a.matches('input')){timer=setTimeout(flush,2000);return}
    load();
  }
  show(cur,false);
  return {live:live,show:show};
}
