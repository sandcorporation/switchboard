// ---- 보고 ----
// GET /api/admin/reports lists the boss roles' reports, newest first. The
// status is the server's: 진행 중 while a linked directive is open under the
// answered rule, else 완료. 진행 중 carries the count behind it, 열린 지시
// open/linked (#275). The count on the menu is the 진행 중 reports.
// The list column groups the reports by the owner's request they belong to
// (request_id, #501), the latest group first; reports without one sit in
// 「지시 없음」 at the end (#514). A row shows the report in the body.
const reportStatus={open:['진행 중','info'],done:['완료','success']};
function reportBadge(r){
  const [label,tone]=reportStatus[r.status]||[r.status,'neutral'];
  return [r.status==='open'?label+' · 열린 지시 '+(r.open_directives||0)+'/'+(r.directives||0):label,tone];
}
const reportChips=[['all','전체',()=>true],['open','진행 중',r=>r.status==='open'],['done','완료',r=>r.status==='done']];
let reportsData=null,reportsFilter='all',reportsSeq=0,reportRequests=new Map();
async function refreshReportCount(){
  try{setTabCount('reports',(await api('reports')).reports.filter(r=>r.status==='open').length||null)}catch(e){}
}
async function loadReports(){
  const seq=++reportsSeq;
  $('reportsNote').textContent='불러오는 중…';
  try{
    // The request titles name the groups; a failure leaves the numbers.
    const [list,reqs]=await Promise.all([api('reports').then(x=>x.reports),api('requests').then(x=>x.requests).catch(()=>[])]);
    if(seq!==reportsSeq)return;
    reportsData=list;reportRequests=new Map(reqs.map(q=>[q.id,q.title]));
    setTabCount('reports',list.filter(r=>r.status==='open').length||null);
    drawReports();
  }catch(e){if(seq===reportsSeq)$('reportsNote').textContent=e.message}
}
function reportRequestName(id){const t=reportRequests.get(id);return '#'+id+(t?' '+t:'')}
function drawReports(){
  const list=reportsData;if(!list)return;
  const chips=$('reportChips');chips.replaceChildren();
  for(const [k,label,f] of reportChips){
    const b=node('button',label+' ','ls-chip');b.type='button';b.setAttribute('aria-pressed',k===reportsFilter?'true':'false');
    b.title=label==='전체'?'모든 보고를 본다':'「'+label+'」 보고만 본다';
    b.append(node('span',String(list.filter(f).length),'ls-chip-n'));
    b.onclick=()=>{reportsFilter=k;drawReports()};
    chips.append(b);
  }
  const keep=reportChips.find(c=>c[0]===reportsFilter)[2],q=$('reportsFilter').value;
  const shown=list.filter(r=>keep(r)&&listMatch(q,'#'+r.id,'R'+r.id,r.title,r.author,r.request_id?reportRequestName(r.request_id):'지시 없음'));
  // Groups keep the list's order (newest first): a group sits where its
  // newest report does.
  const groups=new Map();
  for(const r of shown){const k=r.request_id||0;if(!groups.has(k))groups.set(k,[]);groups.get(k).push(r)}
  const keys=[...groups.keys()].filter(k=>k!==0);if(groups.has(0))keys.push(0);
  const out=[];
  for(const k of keys){
    const label=k?reportRequestName(k):'지시 없음';
    out.push(...listGroup('rep:'+k,label,groups.get(k).map(reportRow),{redraw:drawReports,hover:k?'지시 '+label:'지시에 딸리지 않은 보고'}));
  }
  listFill($('reports'),out,q||reportsFilter!=='all'?'맞는 보고 없음':'보고 없음');
  $('reportsNote').textContent=(shown.length===list.length?'':shown.length+' / ')+list.length;
  $('reportsNote').title='보고 '+list.length+'개'+(shown.length===list.length?'':' 중 '+shown.length+'개 보임');
}
function reportRow(r){
  const [label]=reportBadge(r);
  return listRow('reports',String(r.id),{top:'#'+r.id+' · '+r.author,right:shortWhen(r.created_at),rightHover:when(r.created_at),
    title:r.title,hover:'보고 #'+r.id+' '+r.title+' — '+label,cls:r.status==='open'?'':'done'});
}
$('reportsFilter').addEventListener('input',drawReports);
// reportBar is the report body's foot (#514): the request it belongs to and
// a copy of its text.
function reportBar(r){
  const bar=docBar(),msg=node('span','','msg');msg.setAttribute('role','status');
  bar.append(msg,node('span','','dc-sp'));
  if(r.request_id){
    const a=node('a','지시 열기','button');a.href=detailHash('requests',String(r.request_id));a.title='이 보고가 딸린 지시 #'+r.request_id+' 를 연다';
    bar.append(a);
  }
  const cp=node('button','복사');cp.type='button';cp.title='보고 제목과 글을 복사한다';
  cp.onclick=async()=>{
    try{await navigator.clipboard.writeText(r.title+'\n\n'+(r.body||''));setMsg(msg,'복사함','ok')}
    catch(e){setMsg(msg,'복사 안 됨 · 브라우저가 막음','err')}
  };
  bar.append(cp);
  return bar;
}
