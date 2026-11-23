// ---- 검색 (#481) ----
// The top bar's search box: Enter searches mail, reports, questions and left
// items (GET /api/admin/search), newest first; a row opens its record.
const gsKinds={mail:'메일',report:'보고',question:'질문',left:'남긴 것'};
let gsSeq=0,gsNext='';
async function gsFetch(p){
  let r;
  try{r=await fetch('/api/admin/search?'+p,{credentials:'same-origin',headers:{'Accept':'application/json'}})}
  catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
  if(r.status===401)signedOut();
  if(r.redirected)throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
  if(r.status===400){let t='';try{t=(await r.text()).trim().slice(0,300)}catch(e){}throw Error(t||'잘못된 검색어')}
  if(!r.ok||!(r.headers.get('content-type')||'').includes('application/json'))throw Error('요청 실패('+r.status+')');
  return r.json();
}
function gsOpen(){$('gsPanel').hidden=false;$('gsQ').setAttribute('aria-expanded','true')}
function gsClose(){gsSeq++;$('gsPanel').hidden=true;$('gsQ').setAttribute('aria-expanded','false')}
function gsRange(){return (gsKinds[$('gsKind').value]||'전체')+($('gsRe').checked?' · 정규식':'')}
async function gsRun(more){
  const q=$('gsQ').value.trim();
  if(!q){gsClose();return}
  const seq=++gsSeq;
  gsOpen();
  if(!more){$('gsList').replaceChildren();$('gsCount').textContent='찾는 중…'}
  $('gsMore').hidden=true;
  const p=new URLSearchParams({q:q});
  if($('gsRe').checked)p.set('regex','1');
  if($('gsKind').value)p.set('kind',$('gsKind').value);
  if(more&&gsNext)p.set('after',gsNext);
  let r;
  try{r=await gsFetch(p)}
  catch(e){if(seq===gsSeq)$('gsCount').textContent=e.message;return}
  if(seq!==gsSeq)return;
  for(const h of r.hits)$('gsList').append(gsRow(h));
  gsNext=r.next||'';
  const n=$('gsList').children.length;
  $('gsCount').textContent=(n?n+'건'+(gsNext?'+':''):'없음')+' · '+gsRange()+(r.cut?' · 끊김':'');
  $('gsMore').hidden=!gsNext;
}
function gsRow(h){
  const li=document.createElement('li'),b=document.createElement('button');
  b.type='button';b.className='gsrow';
  const meta=node('span','','gsmeta');
  meta.append(node('span',gsKinds[h.kind]||h.kind,'sc-badge'));
  if(h.kind!=='question')meta.append(node('span',(h.kind==='report'?'R':'#')+h.id));
  meta.append(node('span',when(h.at)),node('span',h.kind==='mail'&&h.to?h.from+' → '+h.to:h.from));
  b.append(meta);
  if(h.title&&h.title!==h.line&&(h.kind==='mail'||h.kind==='report'))b.append(node('span',h.title,'gstitle'));
  b.append(node('span',h.line,'gsline'));
  b.onclick=()=>{gsClose();gsOpenHit(h)};
  li.append(b);
  return li;
}
// gsOpenHit opens a hit in its detail view; a left item opens its directive.
function gsOpenHit(h){
  if(h.kind==='mail')openMessage(h.id);
  else if(h.kind==='left')openMessage(String(h.directive));
  else if(h.kind==='report')openDetail('reports',h.id);
  else if(h.kind==='question')openDetail('questions',h.id);
}
$('gsForm').addEventListener('submit',e=>{e.preventDefault();gsRun(false)});
$('gsKind').onchange=()=>gsRun(false);
$('gsRe').onchange=()=>gsRun(false);
$('gsMore').onclick=()=>gsRun(true);
$('gsQ').addEventListener('focus',()=>{if($('gsQ').value.trim()&&$('gsList').children.length)gsOpen()});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!$('gsPanel').hidden&&(e.target.closest&&e.target.closest('#gsPanel,#gsForm'))){gsClose();$('gsQ').focus()}});
document.addEventListener('click',e=>{if(!$('gsPanel').hidden&&!e.target.closest('#gsPanel,#gsForm'))gsClose()});
