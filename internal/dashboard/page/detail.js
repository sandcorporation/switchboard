// ---- 상세 패널 ----
// A list row opens its item in the right panel (#detail, 440px; the whole
// screen below 720px). The hash is #<tab>/<id>/<sub> (id URI-encoded), so
// reload and back/forward restore the item and its sub-tab. A tab plugs in
// with registerDetail(tab,spec):
//   list   the list's name on the mobile back button (← 세션)
//   kinds  live change kinds that redraw the open item (default [tab])
//   load   async id => item, or null when it is gone
//   head   item => {title, sub, badge:[text,tone], crumb} (sub, optional: the dim line beside the title; tone: success|info|warning|danger|neutral;
//          crumb, optional: {path:[text,...], meta:[[text,hover],...]}, the
//          line under the title on a list screen, #514)
//   subs   item => [[key,label],...]; the first is the default sub-tab
//   draw   async (item,sub,box) => fills box with text nodes only
//   back   id => the hash closing goes to (optional; default #<tab>)
// List renderers call detailRow(tr,tab,id) on each row (Tab-reachable, Enter
// or click opens, the open row is aria-current). openDetail(tab,id,sub) and
// closeDetail() move the hash; drawDetail() redraws the open item. Esc
// closes; focus moves to the title on open and back to the row on close.
const details={};
let detailCur=null,detailSeq=0;
function registerDetail(tab,spec){details[tab]=spec}
function detailHash(tab,id,sub){return '#'+tab+'/'+encodeURIComponent(id)+(sub?'/'+sub:'')}
function openDetail(tab,id,sub){
  if(sub===undefined)sub=detailCur&&detailCur.tab===tab?detailCur.sub:'';
  const h=detailHash(tab,id,sub);
  if(location.hash===h)route();else location.hash=h;
}
function closeDetail(){if(detailCur){const b=details[detailCur.tab].back;location.hash=b?b(detailCur.id):detailCur.tab}}
function detailRow(tr,tab,id){
  id=String(id);tr.dataset.detailId=id;tr.tabIndex=0;
  if(detailCur&&detailCur.tab===tab&&detailCur.id===id){tr.setAttribute('aria-current','true');tr.classList.add('on')}
  tr.addEventListener('click',e=>{if(!e.target.closest('a,button,input'))openDetail(tab,id)});
  tr.addEventListener('keydown',e=>{if(e.target===tr&&(e.key==='Enter'||e.key===' ')){e.preventDefault();openDetail(tab,id)}});
}
function detailRowFor(tab,id){
  for(const tr of $('panel-'+tab).querySelectorAll('[data-detail-id]'))if(tr.dataset.detailId===id)return tr;
  return null;
}
function markDetailRows(){
  for(const tr of document.querySelectorAll('[data-detail-id]')){
    const p=tr.closest('[role=tabpanel]');if(!p)continue;
    const tab=p.id.slice(6),on=!!detailCur&&detailCur.tab===tab&&detailCur.id===tr.dataset.detailId;
    if(on)tr.setAttribute('aria-current','true');else tr.removeAttribute('aria-current');
    tr.classList.toggle('on',on);
  }
}
// keepRowFocus(tbody,fill) rebuilds a list with fill() and puts focus back on
// the row it was on, so a live reload does not drop the keyboard user.
function keepRowFocus(body,fill){
  const a=document.activeElement,id=a&&a.dataset&&body.contains(a)?a.dataset.detailId:undefined;
  fill();
  if(id!==undefined)for(const tr of body.querySelectorAll('[data-detail-id]'))if(tr.dataset.detailId===id){tr.focus();break}
}
function showDetail(tab,id,sub){
  const fresh=!detailCur||detailCur.tab!==tab||detailCur.id!==id;
  detailCur={tab:tab,id:id,sub:sub||''};
  $('appMain').classList.add('has-detail');$('detail').hidden=false;
  $('detailBack').textContent='← '+details[tab].list;
  markDetailRows();splitEmptyShow();
  if(fresh){
    $('detailTitle').textContent=id;$('detailSub').textContent='';$('detailBadge').hidden=true;$('detailTabs').replaceChildren();$('detailCrumb').replaceChildren();
    $('detailBody').replaceChildren(node('p','불러오는 중…','muted'));$('detail').scrollTop=0;
    $('detailTitle').focus({preventScroll:true});
  }
  drawDetail();
}
function hideDetail(){
  const c=detailCur;if(!c)return;
  const a=document.activeElement,back=$('detail').contains(a)||a===document.body||!a;
  detailCur=null;detailSeq++;
  $('detail').hidden=true;$('appMain').classList.remove('has-detail');
  $('detailBody').replaceChildren();termGc();
  markDetailRows();splitEmptyShow();setChrome();
  const tr=detailRowFor(c.tab,c.id);
  if(back&&tr&&curTab===c.tab)tr.focus();
}
const toneClass={success:'sc-badge--success',info:'sc-badge--info',warning:'sc-badge--warning',danger:'sc-badge--danger',neutral:'sc-badge--neutral'};
async function drawDetail(){
  if(ws)ws.refresh();
  const c=detailCur;if(!c)return;
  const seq=++detailSeq,spec=details[c.tab],box=document.createElement('div');
  let head={title:c.id,badge:null},subs=[],sub=c.sub;
  try{
    const item=await spec.load(c.id);
    if(seq!==detailSeq)return;
    if(!item)box.append(node('p','없음 · 지워졌거나 바뀜','empty'));
    else{
      head=spec.head(item);subs=spec.subs(item);
      if(!subs.some(s=>s[0]===sub))sub=subs[0][0];
      await spec.draw(item,sub,box);
    }
  }catch(e){box.replaceChildren(node('p',e.message,'msg err'))}
  if(seq!==detailSeq)return;
  $('detailTitle').textContent=head.title;$('detailSub').textContent=head.sub||'';
  detailCrumbDraw(head.crumb);
  const b=$('detailBadge');b.hidden=!head.badge;
  if(head.badge){b.textContent=head.badge[0];b.className='sc-badge '+(toneClass[head.badge[1]]||toneClass.neutral)}
  // Redrawing replaces the sub-tab links and the body: keep focus where it was.
  const nav=$('detailTabs'),act=document.activeElement,onTab=nav.contains(act)?act.getAttribute('href'):null,inBody=$('detailBody').contains(act);
  nav.replaceChildren();nav.hidden=subs.length<2;
  for(const [k,label] of subs){
    const a=node('a',label);a.href=detailHash(c.tab,c.id,k);
    if(k===sub)a.setAttribute('aria-current','page');
    nav.append(a);
    if(onTab===a.getAttribute('href'))a.focus();
  }
  const top=$('detail').scrollTop;
  $('detailBody').replaceChildren(...box.childNodes);
  $('detail').scrollTop=top;
  setChrome();
  if(inBody&&$('detailBody').contains(act))act.focus({preventScroll:true});
  else if(inBody||(onTab&&!nav.contains(document.activeElement)))$('detailTitle').focus({preventScroll:true});
  termGc();
}
$('detailClose').onclick=closeDetail;
$('detailBack').onclick=closeDetail;
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&detailCur&&!e.defaultPrevented){e.preventDefault();closeDetail()}});
// detailCrumbDraw fills the line under the title: where the item sits
// (path) and, on the right, its facts, each with its hover text.
function detailCrumbDraw(c){
  const box=$('detailCrumb');box.replaceChildren();box.hidden=!c;
  if(!c)return;
  const path=node('span',(c.path||[]).filter(Boolean).join('  ›  '),'dc-path');path.title=path.textContent;
  const rt=node('span','','dc-rt');
  for(const [t,h] of c.meta||[]){if(!t)continue;const m=node('span',t);m.title=h;rt.append(m)}
  box.append(path,rt);
}
function dl(pairs){
  const d=node('dl','','pdl');
  for(const [k,v] of pairs){const dd=document.createElement('dd');if(v instanceof Node)dd.append(v);else dd.textContent=v;d.append(node('dt',k),dd)}
  return d;
}
