// ---- 목록 칸 (#515) ----
// A panel marked data-lv is a list column of grouped two-line rows; a row
// opens its item in the detail beside it (detailRow). lvGroup adds a group
// head that folds its rows (kept for the page's life, per key) and returns
// the box its rows go in; lvRow makes one row; lvFind hides the rows whose
// words do not hold the filter text.
const lvFold=new Map();
function lvGroup(box,key,name,n,opts){
  opts=opts||{};
  const folded=lvFold.has(key)?lvFold.get(key):!!opts.folded;
  const head=node('button','','lv-grp');head.type='button';
  head.title=name+' 접기 · 펼치기';head.setAttribute('aria-expanded',folded?'false':'true');
  const mark=node('span',folded?'▸':'▾','lv-grp-mark');mark.setAttribute('aria-hidden','true');
  head.append(mark,node('span',name,'lv-grp-name'),node('span',n===null||n===undefined?'':String(n),'lv-grp-n'));
  const rows=node('div','','lv-rows');rows.dataset.grp=key;rows.hidden=folded;
  head.onclick=()=>{const f=!rows.hidden;rows.hidden=f;lvFold.set(key,f);mark.textContent=f?'▸':'▾';head.setAttribute('aria-expanded',f?'false':'true')};
  if(opts.add){const row=node('div','','lv-grp-row');row.append(head,opts.add);box.append(row)}else box.append(head);
  box.append(rows);
  return rows;
}
// lvRow: meta is the small first line's parts, name the bold second line,
// st an optional [text,tone] at its end, find extra words for lvFind.
function lvRow(tab,id,meta,name,st,find){
  const r=node('div','','ml');r.setAttribute('role','button');
  const m=node('div','','ml-meta');
  for(const p of meta.filter(x=>x!==''&&x!==null&&x!==undefined))m.append(node('span',p));
  const l2=node('div','','ml-l2');l2.append(node('span',name,'ml-nm'));
  if(st){const s=node('span',st[0],'ml-st '+(st[1]||''));l2.append(s)}
  r.append(m,l2);
  r.dataset.find=[name].concat(meta,find||[]).join(' ').toLowerCase();
  detailRow(r,tab,id);
  return r;
}
function lvEmpty(box,text){box.append(node('div',text,'lv-empty'))}
function lvFind(panel,q){
  q=q.trim().toLowerCase();
  for(const r of panel.querySelectorAll('.ml'))r.hidden=!!q&&!r.dataset.find.includes(q);
}
// lvAge: how long ago t was, as 「n분째」 for a thing still going.
function lvAge(t){
  if(!t)return '';
  const m=Math.max(0,Math.floor((Date.now()-new Date(t).getTime())/60000));
  return m<60?m+'분째':m<1440?Math.floor(m/60)+'시간째':Math.floor(m/1440)+'일째';
}
