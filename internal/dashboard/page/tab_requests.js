// ---- 지시 ----
// GET /api/admin/requests lists the owner's requests to the boss roles
// (#501), newest first, with the status the server derives: waiting (a linked
// panel question is unanswered), done (marked done, nothing after, not
// checked), progress, checked. The list column groups them in that order,
// 확인함 folded (#514). A row shows the request in the body: the owner's
// words, the notes, the result and the linked questions, reports and mail in
// one table (#509 계약 9). A signed-in login checks a done request (POST
// requests/<id>/check) and takes the check back (uncheck).
const requestGroups=[['waiting','대표 답 기다림','warning'],['done','끝 — 안 봄','success'],['progress','진행','info'],['checked','확인함','neutral']];
let requestsData=null,requestsSeq=0;
function requestCount(list){return list.filter(r=>r.status==='waiting'||r.status==='done').length||null}
async function refreshRequestCount(){
  try{setTabCount('requests',requestCount((await api('requests')).requests))}catch(e){}
}
async function loadRequests(){
  const seq=++requestsSeq;
  $('requestsNote').textContent='불러오는 중…';
  try{
    const list=(await api('requests')).requests;
    if(seq!==requestsSeq)return;
    requestsData=list;
    setTabCount('requests',requestCount(list));
    drawRequests();
  }catch(e){if(seq===requestsSeq)$('requestsNote').textContent=e.message}
}
function requestNews(r){
  if((r.status==='done'||r.status==='checked')&&r.result)return r.result;
  if(!r.last)return '';
  const l=r.last;
  if(l.kind==='question')return '질문 '+l.title;
  if(l.kind==='mail')return '메일 #'+l.id+' '+l.title;
  return '보고 '+l.id+' '+l.title;
}
function requestLabel(st){return requestGroups.find(g=>g[0]===st)||[st,st,'neutral']}
function drawRequests(){
  const list=requestsData;if(!list)return;
  const q=$('requestsFilter').value,out=[];
  const shown=list.filter(r=>listMatch(q,'#'+r.id,r.title,r.result,r.author));
  for(const [st,label] of requestGroups){
    const rows=shown.filter(r=>r.status===st);
    if(!rows.length)continue;
    out.push(...listGroup('req:'+st,label,rows.map(requestItem),{fold:st==='checked',redraw:drawRequests}));
  }
  listFill($('requests'),out,q?'맞는 지시 없음':'지시 없음');
  $('requestsNote').textContent=(shown.length===list.length?'':shown.length+' / ')+list.length;
  $('requestsNote').title='지시 '+list.length+'개'+(shown.length===list.length?'':' 중 '+shown.length+'개 보임');
}
function requestItem(r){
  const at=r.last&&r.status!=='done'&&r.status!=='checked'?r.last.at:(r.done_at||r.created_at);
  const news=requestNews(r);
  let top='#'+r.id;
  if(r.open_questions)top+=' · 답 '+r.open_questions;
  else if(r.reports||r.mail)top+=' · 보고 '+(r.reports||0)+' · 메일 '+(r.mail||0);
  return listRow('requests',String(r.id),{top:top,right:shortWhen(at),rightHover:when(at),title:r.title,
    hover:'#'+r.id+' '+r.title+(news?' — '+news:''),cls:r.status==='waiting'||r.status==='done'?'un':''});
}
// requestLinks merges the request's questions, reports and mail into the
// body's table, newest first.
function requestLinks(d){
  const rows=[];
  for(const q of d.question_list||[]){
    const ans=q.answers&&q.answers.length;
    rows.push({at:q.created_at,kind:'질문',who:q.asker,title:q.header+' | '+q.question,state:ans?'답함':q.status==='withdrawn'?'거둠':'답 기다림',stateCls:ans?'g':q.status==='pending'?'w':'',
      href:detailHash('questions',q.id,'answer'),hover:'질문 — '+(ans?'→ '+q.answers.join(' / '):'답하는 화면을 연다')});
  }
  for(const x of d.report_list||[])rows.push({at:x.created_at,kind:'보고 #'+x.id,who:x.author,title:x.title,href:detailHash('reports',String(x.id)),hover:'보고 #'+x.id+' '+x.title});
  for(const x of d.mail_list||[])rows.push({at:x.created_at,kind:'메일 #'+x.id,who:x.sender+' → '+x.recipient,title:x.subject||'(제목 없음)',href:detailHash('mail',String(x.id),'body'),hover:'메일 #'+x.id+' '+(x.subject||'')});
  return rows.sort((a,b)=>new Date(b.at)-new Date(a.at));
}
registerDetail('requests',{
  list:'지시',kinds:['requests'],
  load:async id=>{
    try{return await api('requests/'+encodeURIComponent(id))}
    catch(e){if(e.status===404||/\(404\)/.test(e.message))return null;throw e}
  },
  head:d=>{
    const [,label,tone]=requestLabel(d.status);
    const n=(d.question_list||[]).length,rep=(d.report_list||[]).length,mail=(d.mail_list||[]).length;
    return {title:'#'+d.id+' '+d.title,badge:[label,tone],crumb:{path:['지시 #'+d.id,d.author],
      meta:[[shortWhen(d.created_at)+' 등록','등록한 때 — '+when(d.created_at)],['질문 '+n+' · 보고 '+rep+' · 메일 '+mail,'이 지시에 딸린 것']]}};
  },
  subs:()=>[['body','지시']],
  draw:async(d,sub,box)=>{
    docHead(box,'대표 원문');
    const t=node('p',d.text,'dc-quote');t.title=d.author+' · '+when(d.created_at);box.append(t);
    const notes=d.note_list||[];
    if(notes.length){
      docHead(box,'덧붙인 말 '+notes.length);
      for(const n of notes){const p=node('p',n.text,'dc-quote');p.title=when(n.created_at);box.append(p)}
    }
    if(d.result){docHead(box,'결과');box.append(node('p',d.result,'dc-p'))}
    const links=requestLinks(d);
    docHead(box,'딸린 기록 '+links.length);
    box.append(links.length?linkTable(links):node('p','딸린 기록 없음','empty'));
    box.append(requestBar(d));
  }
});
// requestBar is the body's foot: 확인 for a done request, 되돌리기 for a
// checked one; before that 확인 shows, unpressable.
function requestBar(d){
  const bar=docBar(),msg=node('span','','msg');msg.setAttribute('role','status');
  const check=d.status!=='checked';
  const btn=node('button',check?'확인':'되돌리기',check?'btn-p':'');btn.type='button';
  if(d.status==='checked'){const c=node('span','확인 '+shortWhen(d.checked_at)+(d.checked_by?' · '+d.checked_by:''),'muted');c.title=when(d.checked_at);bar.append(c)}
  bar.append(msg,node('span','','dc-sp'));
  if(d.status==='done'||d.status==='checked'){
    btn.title=check?'끝난 지시를 「확인함」으로 옮긴다':'확인을 거두고 「끝 — 안 봄」으로 되돌린다';
    btn.onclick=async()=>{
      btn.disabled=true;setMsg(msg,'');
      try{
        await apiSend('POST','requests/'+d.id+(check?'/check':'/uncheck'));
        await loadRequests();
        if(!detailCur||detailCur.tab!=='requests')return;
        drawDetail();
      }catch(e){btn.disabled=false;setMsg(msg,e.message,'err')}
    };
  }else{btn.disabled=true;btn.title='아직 끝나지 않아 누를 수 없다'}
  bar.append(btn);
  return bar;
}
// openRequest shows the 지시 screen with one request in the body (a search hit).
function openRequest(id){openDetail('requests',String(id))}
$('requestsFilter').addEventListener('input',drawRequests);
