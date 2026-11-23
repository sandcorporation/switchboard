// ---- 메일 ----
// mailID is set by openMessage (진행 tab) to show one message; 「초기화」 or a
// new search clears it.
let mailID='';
function shortWhen(t){
  const d=new Date(t),n=new Date();
  return d.toDateString()===n.toDateString()?d.toLocaleTimeString('ko-KR',{hour:'2-digit',minute:'2-digit'}):d.toLocaleDateString('ko-KR',{month:'numeric',day:'numeric'});
}
const mailTone={pending:'warning',read:'neutral',acknowledged:'info',complete:'success',completed:'success',held:'neutral'};
const mailCols=['#','제목','보낸 쪽 → 받는 쪽','상태','시각'];
// mailRows are the rows shown; a page is mailPage rows and 「더 오래된 메일」
// asks for the next one before the last row's ID (#8).
let mailRows=[];
const mailPage=100;
function mailQuery(extra){
  const thread=$('thread').value,task=$('task').value.trim();
  return new URLSearchParams(Object.assign({box:$('box').value,role:$('role').value,status:$('status').value,q:$('search').value,thread:thread,task:task,id:mailID},extra));
}
function drawMail(more){
  const list=$('list');
  keepRowFocus(list,()=>{
    list.replaceChildren();
    for(const m of mailRows)list.append(mailRow(m));
    if(!mailRows.length)emptyRow(list,mailCols.length,'메일 없음');
  });
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
function mailRow(m){
  const tr=node('tr','',m.priority==='urgent'?'urgent':'');
  const subj=node('td','');
  if(m.priority==='urgent')subj.append(node('span','긴급','badge'));
  subj.append(node('strong',m.subject));
  if(m.task_id)subj.append(node('small','task '+m.task_id));
  const who=node('td',m.sender+' → '+m.recipient);
  if(m.addressed_to&&m.addressed_to!==m.recipient)who.append(node('small',m.addressed_to+' 앞'));
  const t=node('td',shortWhen(m.created_at),'nowrap');t.title=when(m.created_at);
  const cells=[node('td',String(m.id),'num nowrap'),subj,who,node('td',statusLabel[m.status]||m.status,'nowrap'),t];
  cells.forEach((td,i)=>{if(i>0)td.dataset.label=mailCols[i];tr.append(td)});
  detailRow(tr,'mail',m.id);
  return tr;
}
registerDetail('mail',{
  list:'메일',
  load:async id=>(await api('messages?'+new URLSearchParams({id:id})))[0]||null,
  head:m=>({title:'#'+m.id+' '+m.subject,badge:[statusLabel[m.status]||m.status,mailTone[m.status]||'neutral']}),
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
    if(m.task_id)pairs.push(['task',m.task_id]);
    box.append(dl(pairs),node('pre',m.body));
    const foot=node('p','','row muted');
    foot.append(node('span',m.thread_id?'스레드 #'+m.thread_id+(m.reply_to?' · #'+m.reply_to+'에 대한 답장':''):'스레드 시작 메일'));
    const th=node('button','스레드 보기');
    th.onclick=()=>{if(curTab!=='mail'){openThread(root);return}$('thread').value=root;mailID='';load()};
    foot.append(th);
    box.append(foot);
  }
});
