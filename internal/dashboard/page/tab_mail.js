// ---- 메일 ----
// mailID is set by openMessage (진행 tab) to show one message; 「초기화」 or a
// new search clears it.
let mailID='';
function shortWhen(t){
  const d=new Date(t),n=new Date();
  return d.toDateString()===n.toDateString()?d.toLocaleTimeString('ko-KR',{hour:'2-digit',minute:'2-digit'}):d.toLocaleDateString('ko-KR',{month:'numeric',day:'numeric'});
}
const mailTone={pending:'warning',read:'neutral',acknowledged:'info',complete:'success',completed:'success',held:'neutral'};
// mailRows are the rows shown; a page is mailPage rows and 「더 오래된 메일」
// asks for the next one before the last row's ID (#8).
let mailRows=[];
const mailPage=100;
function mailQuery(extra){
  const thread=$('thread').value,task=$('task').value.trim();
  return new URLSearchParams(Object.assign({box:$('box').value,role:$('role').value,status:$('status').value,q:$('search').value,thread:thread,task:task,id:mailID},extra));
}
function drawMail(more){
  listFill($('list'),mailRows.map(mailRow),'메일 없음');
  const btn=$('olderMail');
  btn.hidden=!more;
  $('conn').textContent=mailRows.length+'통';
}
async function load(){
  $('conn').textContent='불러오는 중…';
  const thread=$('thread').value,task=$('task').value.trim(),id=mailID;
  $('notice').textContent=[id?'메일 #'+id:'',thread?'스레드 #'+thread:'',task?'task '+task:''].filter(Boolean).join(' · ')+(id||thread||task?'만 보는 중':'');
  try{
    // A refresh keeps the pages already shown (up to the server's 500).
    const n=Math.min(500,Math.max(mailPage,mailRows.length));
    const rows=await api('messages?'+mailQuery({limit:n}));
    mailRows=rows;
    drawMail(rows.length>=n);
  }catch(e){$('conn').textContent=e.message}
}
async function loadOlder(){
  const btn=$('olderMail');
  if(!mailRows.length)return;
  btn.disabled=true;
  try{
    const rows=await api('messages?'+mailQuery({limit:mailPage,before:mailRows[mailRows.length-1].id}));
    mailRows=mailRows.concat(rows);
    drawMail(rows.length>=mailPage);
  }catch(e){$('conn').textContent=e.message}
  btn.disabled=false;
}
$('olderMail').onclick=loadOlder;
// alertRepeat is how a collapsing alert mail that came again reads (#525):
// "×3 · 마지막 14:05"; '' for any other mail.
function alertRepeat(m){
  if(!(m.alert_count>1))return '';
  if(!m.alert_last_at)return '×'+m.alert_count;
  const d=new Date(m.alert_last_at);
  return '×'+m.alert_count+' · 마지막 '+String(d.getHours()).padStart(2,'0')+':'+String(d.getMinutes()).padStart(2,'0');
}
// mailRow is one row of the list column: who → whom, the number and the
// request it belongs to (#501) on top, the subject under it (#514).
function mailRow(m){
  // The numbers lead: a long role pair is what gets cut.
  let top='#'+m.id;
  if(m.request_id)top+=' · 지시 #'+m.request_id;
  top+=' · '+m.sender+' → '+m.recipient;
  const rep=alertRepeat(m);
  const hover=(m.priority==='urgent'?'[긴급] ':'')+'#'+m.id+' '+m.subject+(rep?' '+rep:'')+' — '+m.sender+' → '+(m.addressed_to&&m.addressed_to!==m.recipient?m.addressed_to+' 앞 ('+m.recipient+')':m.recipient)+' · '+(statusLabel[m.status]||m.status)+(m.task_id?' · task '+m.task_id:'')+(m.request_id?' · 지시 #'+m.request_id:'');
  return listRow('mail',String(m.id),{top:top,right:shortWhen(m.created_at),rightHover:when(m.created_at),
    title:(m.priority==='urgent'?'[긴급] ':'')+m.subject+(rep?' '+rep:''),hover:hover,cls:(m.status==='pending'?'un':'')+(m.priority==='urgent'?' urgent':'')});
}
registerDetail('mail',{
  list:'메일',
  load:async id=>(await api('messages?'+new URLSearchParams({id:id})))[0]||null,
  head:m=>({title:'#'+m.id+' '+m.subject,badge:[statusLabel[m.status]||m.status,mailTone[m.status]||'neutral'],crumb:{
    path:[m.sender,m.addressed_to&&m.addressed_to!==m.recipient?m.addressed_to+' 앞':m.recipient,m.request_id?'지시 #'+m.request_id:''],
    meta:[[shortWhen(m.created_at),'보낸 때 — '+when(m.created_at)],[m.reply_to?'#'+m.reply_to+' 의 답':'','이어진 메일']]}}),
  subs:()=>[['body','본문'],['thread','스레드']],
  draw:async(m,sub,box)=>{
    const root=m.thread_id||m.id;
    if(sub==='thread'){
      const rows=(await api('messages?'+new URLSearchParams({thread:root}))).slice().reverse();
      const ul=node('ul','','dlist');
      for(const x of rows){
        const li=node('li','',x.id===m.id?'cur':'');
        const a=node('a','#'+x.id+' '+x.subject);a.href=detailHash('mail',x.id,'body');
        if(x.id===m.id)a.setAttribute('aria-current','true');
        const text=x.body.length>400?x.body.slice(0,400)+'…':x.body;
        li.append(a,node('small',x.sender+' → '+x.recipient+' · '+(statusLabel[x.status]||x.status)+' · '+when(x.created_at)),node('p',text));
        ul.append(li);
      }
      box.append(rows.length?ul:node('p','메일 없음','empty'));
      return;
    }
    const pairs=[['보낸 쪽',m.sender],['받는 쪽',m.recipient]];
    if(m.addressed_to&&m.addressed_to!==m.recipient)pairs.push(['주소',m.addressed_to+' 앞']);
    pairs.push(['시각',when(m.created_at)],['상태',statusLabel[m.status]||m.status]);
    if(m.priority==='urgent')pairs.push(['우선','긴급']);
    if(m.alert_source)pairs.push(['출처',m.alert_source]);
    if(alertRepeat(m))pairs.push(['반복',alertRepeat(m)]);
    if(m.task_id)pairs.push(['task',m.task_id]);
    if(m.request_id){const a=node('a','지시 #'+m.request_id);a.href=detailHash('requests',String(m.request_id));a.title='이 메일이 딸린 지시를 연다';pairs.push(['지시',a])}
    box.append(dl(pairs),node('pre',m.body));
    const foot=node('p','','row muted');
    foot.append(node('span',m.thread_id?'스레드 #'+m.thread_id+(m.reply_to?' · #'+m.reply_to+'에 대한 답장':''):'스레드 시작 메일'));
    const th=node('button','스레드 보기');th.type='button';th.title='이 스레드의 메일만 목록에 남긴다';
    th.onclick=()=>{if(curTab!=='mail'){openThread(root);return}$('thread').value=root;mailID='';load()};
    foot.append(th);
    box.append(foot);
  }
});
