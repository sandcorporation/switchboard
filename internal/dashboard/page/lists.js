// ---- 목록 칸 (#514) ----
// The list screens (listTabs) draw their list column with these: listRow is
// one two-line row (a small top line, the title under it) that shows its item
// in the body beside the list (detailRow); listGroup is a group head that
// folds, followed by its rows (o.add: a button at its end, #515); listMatch is the 거르기 field's test.
function listRow(tab,id,o){
  const r=node('div','','ml'+(o.cls?' '+o.cls:''));r.setAttribute('role','button');
  const top=node('span','','ml-r'),a=node('span',o.top||'','ml-a'),t=node('span',o.right||'','ml-t');
  if(o.rightHover)t.title=o.rightHover;
  if(o.tone)t.classList.add('tone-'+o.tone);
  top.append(a,t);
  const b=node('b',o.title||'(제목 없음)','ml-b');
  r.append(top,b);r.title=o.hover||o.title||'';
  detailRow(r,tab,id);
  return r;
}
// listFolds keeps each group's fold across redraws (key → folded).
const listFolds=new Map();
function listGroup(key,label,rows,o){
  o=o||{};
  const folded=listFolds.has(key)?listFolds.get(key):!!o.fold;
  const h=node('button','','grp');h.type='button';h.setAttribute('aria-expanded',String(!folded));
  h.title=(o.hover||label)+' '+rows.length+' — 눌러 접거나 편다';
  h.append(node('span',folded?'▸':'▾','grp-mark'),node('span',label,'grp-nm'),node('span',String(o.count===undefined?rows.length:o.count),'grp-n'));
  h.onclick=()=>{listFolds.set(key,!folded);if(o.redraw)o.redraw()};
  // add (#515) is a button at the head's end (작업 추가 …).
  let head=h;if(o.add){head=node('div','','grp-row');head.append(h,o.add)}
  return folded?[head]:[head,...rows];
}
function listMatch(q,...texts){
  q=(q||'').trim().toLowerCase();
  return !q||texts.some(t=>t!==undefined&&t!==null&&String(t).toLowerCase().includes(q));
}
// listFill replaces box's rows, keeping focus on the row it was on.
function listFill(box,items,empty){
  const a=document.activeElement,id=a&&box.contains(a)&&a.dataset?a.dataset.detailId:undefined,grp=a&&box.contains(a)&&a.classList.contains('grp')?a.querySelector('.grp-nm').textContent:undefined;
  box.replaceChildren(...items);
  if(!items.length)box.append(node('p',empty,'empty ls-empty'));
  if(id!==undefined){for(const r of box.querySelectorAll('[data-detail-id]'))if(r.dataset.detailId===id){r.focus({preventScroll:true});break}}
  else if(grp!==undefined){for(const g of box.querySelectorAll('.grp'))if(g.querySelector('.grp-nm').textContent===grp){g.focus({preventScroll:true});break}}
}
// docHead appends a body section head (h2) to box.
function docHead(box,text){box.append(node('h2',text,'dc-h'))}
// docBar is the action row at the foot of a body.
function docBar(){return node('div','','dc-bar')}
// linkTable is the body's table of linked items: rows of
// {at, kind, who, title, state, stateCls, href, hover}.
function linkTable(rows){
  const t=node('table','','dc-t'),head=node('tr','');
  for(const h of ['때','갈래','누가','제목','상태'])head.append(node('th',h));
  const thead=document.createElement('thead');thead.append(head);
  const body=document.createElement('tbody');
  for(const x of rows){
    const tr=node('tr','');tr.title=x.hover||x.title;
    const at=node('td',shortWhen(x.at),'dc-dim');at.title=when(x.at);
    const ti=node('td','','dc-ti');const a=node('a',x.title||'(제목 없음)');a.href=x.href;a.title=x.hover||x.title;ti.append(a);
    tr.append(at,node('td',x.kind),node('td',x.who||''),ti,node('td',x.state||'—','dc-st'+(x.stateCls?' '+x.stateCls:'')));
    tr.onclick=e=>{if(!e.target.closest('a'))location.hash=x.href};
    body.append(tr);
  }
  t.append(thead,body);
  return t;
}
// docTable is a body table with its own heads (#581): rows of {cells, href,
// hover, cls}; a cell is text or a node, and a row with href opens it. An
// empty list shows one row of empty across the table.
function docTable(heads,rows,empty){
  const t=node('table','','dc-t dc-g'),head=node('tr','');
  for(const h of heads)head.append(node('th',h));
  const thead=document.createElement('thead');thead.append(head);
  const body=document.createElement('tbody');
  for(const x of rows){
    const tr=node('tr','',x.cls||'');if(x.hover)tr.title=x.hover;
    for(const c of x.cells){const td=node('td','');if(c instanceof Node)td.append(c);else td.textContent=c;tr.append(td)}
    if(x.href)tr.onclick=e=>{if(!e.target.closest('a,button'))location.hash=x.href};
    else tr.classList.add('dc-still');
    body.append(tr);
  }
  if(!rows.length){const tr=node('tr','','dc-still'),td=node('td',empty||'없음','dc-dim');td.colSpan=heads.length;tr.append(td);body.append(tr)}
  t.append(thead,body);
  return t;
}
// docLink is a table cell's link: text that opens href.
function docLink(text,href,hover){const a=node('a',text);a.href=href;a.title=hover||text;return a}
