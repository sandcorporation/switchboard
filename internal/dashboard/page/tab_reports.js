// ---- 보고 ----
// GET /api/admin/reports lists the boss roles' reports, newest first. The
// status is the server's: 진행 중 while a linked directive is open under the
// answered rule, else 완료. 진행 중 carries the count behind it, 열린 지시
// open/linked (#275). The count on the menu is the 진행 중 reports.
const reportCols=['상태','제목','보낸 쪽','시각'];
const reportStatus={open:['진행 중','info'],done:['완료','success']};
function reportBadge(r){
  const [label,tone]=reportStatus[r.status]||[r.status,'neutral'];
  return [r.status==='open'?label+' · 열린 지시 '+(r.open_directives||0)+'/'+(r.directives||0):label,tone];
}
const reportChips=[['all','전체',()=>true],['open','진행 중',r=>r.status==='open'],['done','완료',r=>r.status==='done']];
let reportsData=null,reportsFilter='all',reportsSeq=0;
async function refreshReportCount(){
  try{setTabCount('reports',(await api('reports')).reports.filter(r=>r.status==='open').length||null)}catch(e){}
}
async function loadReports(){
  const seq=++reportsSeq;
  $('reportsNote').textContent='불러오는 중…';
  try{
    const list=(await api('reports')).reports;
    if(seq!==reportsSeq)return;
    reportsData=list;
    setTabCount('reports',list.filter(r=>r.status==='open').length||null);
    drawReports();
  }catch(e){if(seq===reportsSeq)$('reportsNote').textContent=e.message}
}
function drawReports(){
  const list=reportsData;if(!list)return;
  const chips=$('reportChips');chips.replaceChildren();
  for(const [k,label,f] of reportChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===reportsFilter?'true':'false');
    b.append(node('span',String(list.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{reportsFilter=k;drawReports()};
    chips.append(b);
  }
  const keep=reportChips.find(c=>c[0]===reportsFilter)[2],shown=list.filter(keep),body=$('reports');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const r of shown)body.append(reportRow(r));
    if(!shown.length)emptyRow(body,reportCols.length,'보고 없음');
  });
  $('reportsNote').textContent=(shown.length===list.length?'':shown.length+' / ')+list.length+'개';
}
function reportRow(r){
  const [label,tone]=reportBadge(r);
  const tr=node('tr','',r.status==='open'?'':'done');
  const st=cell('');st.append(badge(label,tone));
  const t=cell('',reportCols[1],'wrap');t.append(node('strong',r.title));
  if(r.mail&&r.mail.length)t.append(' ',node('span','지시 '+r.mail.length,'muted'));
  const at=cell(shortWhen(r.created_at),reportCols[3],'nowrap');at.title=when(r.created_at);
  tr.append(st,t,cell(r.author,reportCols[2]),at);
  detailRow(tr,'reports',r.id);
  return tr;
}
