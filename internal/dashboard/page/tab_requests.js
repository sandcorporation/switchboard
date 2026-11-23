// ---- 지시 ----
// GET /api/admin/requests lists the owner's requests to the boss roles
// (#501), newest first, with the status the server derives: waiting (a linked
// panel question is unanswered), done (marked done, nothing after, not
// checked), progress, checked. The tab groups them in that order, 확인함
// folded. A row opens in place: the owner's words and notes, the panel
// answers and the linked reports and mail. A signed-in login checks a done
// request (POST requests/<id>/check) and takes the check back (uncheck).
const requestGroups=[['waiting','대표 답 기다림','warning'],['done','끝 — 안 봄','success'],['progress','진행','info'],['checked','확인함','neutral']];
let requestsData=null,requestsSeq=0,requestOpen=new Set(),requestFold=true;
const requestDetail=new Map();
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
    // Open rows read their detail again: a live change may have added to it.
    await Promise.all([...requestOpen].map(id=>loadRequestDetail(id)));
    if(seq!==requestsSeq)return;
    drawRequests();
  }catch(e){if(seq===requestsSeq)$('requestsNote').textContent=e.message}
}
async function loadRequestDetail(id){
  try{requestDetail.set(id,await api('requests/'+id))}catch(e){requestDetail.set(id,{error:e.message})}
}
function drawRequests(){
  const list=requestsData;if(!list)return;
  const box=$('requests'),a=document.activeElement;
  // Put focus back on the row it was on, so a live reload does not drop the
  // keyboard user.
  const focusID=a&&box.contains(a)&&a.classList.contains('reqrow')?a.closest('li').dataset.id:undefined;
  {
    box.replaceChildren();
    if(!list.length){box.append(node('p','지시 없음','muted'))}
    for(const [st,label] of requestGroups){
      const rows=list.filter(r=>r.status===st);
      if(!rows.length)continue;
      const ul=node('ul','','reqlist');
      for(const r of rows)ul.append(requestItem(r));
      const h=node('h3',label+' ','sechead');h.append(node('span',String(rows.length),'muted'));
      if(st==='checked'){
        const d=node('details','','reqfold');d.open=!requestFold;
        const s=node('summary','');s.append(h);d.append(s,ul);
        d.ontoggle=()=>{requestFold=!d.open};
        box.append(d);
      }else box.append(h,ul);
    }
  }
  if(focusID!==undefined){const li=box.querySelector('li.req[data-id="'+focusID+'"]');if(li)li.querySelector('.reqrow').focus()}
  $('requestsNote').textContent=list.length+'개';
}
function requestNews(r){
  if((r.status==='done'||r.status==='checked')&&r.result)return r.result;
  if(!r.last)return '';
  const l=r.last;
  if(l.kind==='question')return '질문 '+l.title;
  if(l.kind==='mail')return '메일 #'+l.id+' '+l.title;
  return '보고 '+l.id+' '+l.title;
}
function requestItem(r){
  const id=String(r.id),open=requestOpen.has(id);
  const li=node('li','',open?'req open':'req');li.dataset.id=id;
  const [,label,tone]=requestGroups.find(g=>g[0]===r.status)||[r.status,r.status,'neutral'];
  const b=node('button','','reqrow');b.type='button';b.setAttribute('aria-expanded',open?'true':'false');
  const head=node('span','','reqhead');
  const tt=node('strong',r.title,'reqtitle');tt.title=r.title;
  head.append(badge(label,tone),tt);
  const meta=node('span','','reqmeta muted');
  const at=r.last&&r.status!=='done'&&r.status!=='checked'?r.last.at:(r.done_at||r.created_at);
  const t=node('span',shortWhen(at));t.title=when(at);
  meta.append(t);
  const news=requestNews(r);if(news)meta.append(' · ',node('span',news,'reqnews'));
  b.append(head,meta);
  b.onclick=async()=>{
    if(requestOpen.has(id)){requestOpen.delete(id);drawRequests();return}
    requestOpen.add(id);
    if(!requestDetail.has(id))await loadRequestDetail(id);
    drawRequests();
  };
  li.append(b);
  if(open)li.append(requestBody(r,requestDetail.get(id)));
  return li;
}
function requestBody(r,d){
  const box=node('div','','reqbody');
  if(!d){box.append(node('p','불러오는 중…','muted'));return box}
  if(d.error){box.append(node('p',d.error,'msg err'));return box}
  box.append(node('h4',d.author+' · '+when(d.created_at),'reqsub'),node('p',d.text,'reqtext'));
  for(const n of d.note_list||[])box.append(node('h4','덧붙임 · '+when(n.created_at),'reqsub'),node('p',n.text,'reqtext'));
  if(d.result)box.append(node('h4','결과','reqsub'),node('p',d.result,'reqtext'));
  if((d.question_list||[]).length){
    box.append(node('h4','패널 답','reqsub'));
    const ul=node('ul','','reqitems');
    for(const q of d.question_list){
      const li=node('li','');
      const a=node('a',q.header+' | '+q.question);a.href=detailHash('questions',q.id,'answer');
      li.append(a,' ',node('span',q.answers&&q.answers.length?'→ '+q.answers.join(' / '):'답 기다림',q.answers&&q.answers.length?'':'muted'));
      ul.append(li);
    }
    box.append(ul);
  }
  const reps=d.report_list||[],mails=d.mail_list||[];
  box.append(node('h4','보고 '+reps.length+' · 넘긴 메일 '+mails.length,'reqsub'));
  if(reps.length||mails.length){
    const ul=node('ul','','reqitems');
    for(const x of reps){const li=node('li','');const a=node('a','R'+x.id+' '+x.title);a.href=detailHash('reports',String(x.id));li.append(a,' ',node('span',x.author+' · '+shortWhen(x.created_at),'muted'));ul.append(li)}
    for(const x of mails){const li=node('li','');const a=node('a','#'+x.id+' '+(x.subject||'(제목 없음)'));a.href=detailHash('mail',String(x.id),'body');li.append(a,' ',node('span',x.sender+' → '+x.recipient+' · '+shortWhen(x.created_at),'muted'));ul.append(li)}
    box.append(ul);
  }
  const acts=node('div','','reqacts'),msg=node('p','','msg');
  if(r.status==='done'||r.status==='checked'){
    const check=r.status==='done';
    const btn=node('button',check?'확인':'되돌리기',check?'reqcheck':'');btn.type='button';
    if(!check)acts.append(node('span','확인 '+when(d.checked_at)+(d.checked_by?' · '+d.checked_by:''),'muted'),' ');
    btn.onclick=async()=>{
      btn.disabled=true;setMsg(msg,'');
      try{
        requestDetail.set(String(r.id),await apiSend('POST','requests/'+r.id+(check?'/check':'/uncheck')));
        await loadRequests();
      }catch(e){btn.disabled=false;setMsg(msg,e.message,'err')}
    };
    acts.append(btn);
    box.append(acts,msg);
  }
  return box;
}
// openRequest shows the 지시 tab with one request opened (a search hit).
async function openRequest(id){
  id=String(id);requestOpen.add(id);
  selectTab('requests');
  await loadRequests();
  const li=$('requests').querySelector('li.req[data-id="'+id+'"]');
  if(li){const d=li.closest('details');if(d)d.open=true;li.scrollIntoView({block:'nearest'});li.querySelector('.reqrow').focus()}
}
