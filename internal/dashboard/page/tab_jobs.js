// ---- 작업 (#291) ----
// Jobs, watches and schedules (#292) of every role: a live job has 중지, an
// ended job and every schedule 지우기, each behind a confirm. The list shows
// labels only; a row opens the detail (#jobs/job:<id> or sched:<id>, #404),
// which loads that one item with its command or prompt (the server records
// the read).
const jobCols=['종류','이름','역할','기계','상태','시작','만료','마지막 줄',''];
const jobKinds={command:'명령',watch:'감시',cron:'예약',once:'예약 1회'};
const jobStates={handed:['넘겨짐','neutral'],running:['실행 중','info'],exited:['끝남','success'],failed:['시작 실패','danger'],stopped:['중지됨','neutral'],expired:['만료','warning'],lost:['잃음','danger']};
const jobLive=s=>s==='handed'||s==='running'||s==='active'||s==='paused';
const schedStates={active:['예약됨','info'],paused:['멈춤','warning'],ended:['끝남','success'],expired:['만료','warning']};
// The job state filter as a schedule state; undefined: no schedule has it.
const schedFilter={live:'live','':'',exited:'ended',expired:'expired'};
let jobsSeq=0,jobRolesFilled=false;
async function loadJobs(){
  const seq=++jobsSeq;
  $('jobsNote').textContent='불러오는 중…';
  try{
    if(!jobRolesFilled){for(const v of await api('roles'))$('jobRole').append(new Option(v.role,v.role));jobRolesFilled=true}
    const q=new URLSearchParams();
    if($('jobRole').value)q.set('role',$('jobRole').value);
    if($('jobState').value)q.set('state',$('jobState').value);
    const sq=new URLSearchParams(q),sf=schedFilter[$('jobState').value];
    if(sf)sq.set('state',sf);else sq.delete('state');
    const [jobs,scheds]=await Promise.all([api('jobs'+(q.toString()?'?'+q:'')),sf===undefined?[]:api('schedules'+(sq.toString()?'?'+sq:''))]);
    if(seq!==jobsSeq)return;
    for(const sc of scheds)sc.schedule=true;
    drawJobs(jobs.concat(scheds).sort((a,b)=>String(b.created_at).localeCompare(String(a.created_at))));
  }catch(e){if(seq===jobsSeq)$('jobsNote').textContent=e.message}
}
function drawJobs(list){
  const body=$('jobs');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const j of list)body.append(jobRow(j));
    if(!list.length)emptyRow(body,jobCols.length,'작업 없음');
  });
  if(['live',''].includes($('jobState').value))setTabCount('jobs',list.filter(j=>jobLive(j.state)).length||null);
  $('jobsNote').textContent=list.length+'개';
}
function jobRow(j){
  if(j.schedule)return schedRow(j);
  const tr=node('tr','',jobLive(j.state)?'':'done');
  let [label,tone]=jobStates[j.state]||[j.state,'neutral'];
  if(j.state==='exited'&&j.exit_code!==undefined){label+=' · exit '+j.exit_code;if(j.exit_code)tone='danger'}
  if(j.stop_requested&&jobLive(j.state))label+=' · 중지 요청';
  const st=cell('',jobCols[4]);st.append(badge(label,tone));
  const name=cell('',jobCols[1],'wrap');name.append(node('strong',j.label));
  const started=cell(shortWhen(j.started_at||j.created_at),jobCols[5],'nowrap');started.title=when(j.started_at||j.created_at);
  const expires=cell(jobLive(j.state)?shortWhen(j.expires_at):'-',jobCols[6],'nowrap');expires.title=when(j.expires_at);
  const lines=(j.tail||'').split('\n'),last=lines[lines.length-1]||'-';
  const tail=cell(last,jobCols[7],'wrap mono');
  const act=cell('','','nowrap'),msg=node('span','','msg');msg.setAttribute('role','status');
  if(jobLive(j.state)){
    const b=setButton('중지','',async()=>{
      if(!confirm('#'+j.id+' '+j.label+' 작업을 멈출까요?'))return;
      b.disabled=true;setMsg(msg,'멈추는 중…');
      try{await apiSend('POST','jobs/'+encodeURIComponent(j.id)+'/stop');loadJobs()}
      catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
    });
    b.disabled=!!j.stop_requested;act.append(b);
  }else{
    const b=setButton('지우기','',async()=>{
      if(!confirm('#'+j.id+' '+j.label+' 기록을 지울까요? 되돌릴 수 없습니다.'))return;
      b.disabled=true;setMsg(msg,'지우는 중…');
      try{await apiSend('DELETE','jobs/'+encodeURIComponent(j.id));loadJobs()}
      catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
    });
    act.append(b);
  }
  act.append(msg);
  tr.append(cell(jobKinds[j.kind]||j.kind,jobCols[0],'nowrap'),name,cell(j.role,jobCols[2],'nowrap'),cell(j.machine,jobCols[3],'nowrap'),st,started,expires,tail,act);
  detailRow(tr,'jobs','job:'+j.id);
  return tr;
}
function schedRow(j){
  const tr=node('tr','',jobLive(j.state)?'':'done');
  const [label,tone]=jobBadge(j);
  const st=cell('',jobCols[4]);st.append(badge(label,tone));
  if(j.paused_by)st.append(node('div',pausedText(j),'muted'));
  const name=cell('',jobCols[1],'wrap');name.append(node('strong',j.label));
  if(j.cron){const c=node('div',j.cron,'muted mono');c.title=j.tz;name.append(c)}
  const created=cell(shortWhen(j.created_at),jobCols[5],'nowrap');created.title=when(j.created_at);
  const expires=cell(j.state==='active'?shortWhen(j.expires_at):'-',jobCols[6],'nowrap');expires.title=when(j.expires_at);
  const parts=[];
  if(j.next_fire_at&&j.state!=='paused')parts.push('다음 '+shortWhen(j.next_fire_at));
  if(j.missed)parts.push('밀린 '+j.missed+'회');
  const next=cell(parts.join(' · ')||'-',jobCols[7],'wrap');
  if(j.next_fire_at)next.title=when(j.next_fire_at);
  const act=cell('','','nowrap'),msg=node('span','','msg');msg.setAttribute('role','status');
  const b=setButton('지우기','',async()=>{
    if(!confirm('#'+j.id+' '+j.label+' 예약을 지울까요? 되돌릴 수 없습니다.'))return;
    b.disabled=true;setMsg(msg,'지우는 중…');
    try{await apiSend('DELETE','schedules/'+encodeURIComponent(j.id));loadJobs()}
    catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
  });
  act.append(...pauseButtons(j,msg),b,msg);
  tr.append(cell(jobKinds[j.kind]||j.kind,jobCols[0],'nowrap'),name,cell(j.role,jobCols[2],'nowrap'),cell(j.machine,jobCols[3],'nowrap'),st,created,expires,next,act);
  detailRow(tr,'jobs','sched:'+j.id);
  return tr;
}
// A paused schedule (#404) names who paused it, when and why; 멈춤 asks for
// an optional reason, 다시 resumes. A once schedule whose time passed while
// paused (passed) resumes only by sending that prompt now or dropping it.
function pausedText(j){
  const by=j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name;
  return by+' · '+shortWhen(j.paused_at)+(j.pause_reason?' · '+j.pause_reason:'');
}
function pauseButtons(j,msg){
  const id=encodeURIComponent(j.id),out=[];
  const act=(label,path,body,doing)=>{
    const b=setButton(label,'',async()=>{
      let send=body;
      if(path==='pause'){const r=prompt('#'+j.id+' '+j.label+' 예약을 멈춥니다. 까닭 (선택)','');if(r===null)return;send={reason:r}}
      else if(body&&body.passed==='drop'&&!confirm('#'+j.id+' '+j.label+' 지난 시각을 버리고 예약을 끝낼까요?'))return;
      b.disabled=true;setMsg(msg,doing);
      try{await(path==='pause'?apiSend('POST','schedules/'+id+'/pause',send):apiSend('POST','schedules/'+id+'/resume',send));loadJobs();drawDetail()}
      catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
    });
    out.push(b);
  };
  if(j.state==='active')act('멈춤','pause',null,'멈추는 중…');
  else if(j.state==='paused'&&j.passed){act('지금 보내고 켜기','resume',{passed:'fire'},'켜는 중…');act('버리고 끝내기','resume',{passed:'drop'},'끝내는 중…')}
  else if(j.state==='paused')act('다시','resume',{},'켜는 중…');
  return out;
}
function jobBadge(j){
  if(j.schedule)return j.state==='paused'&&j.passed?['멈춤 · 지나감','warning']:schedStates[j.state]||[j.state,'neutral'];
  const [t,c]=jobStates[j.state]||[j.state,'neutral'];
  return j.state==='exited'&&j.exit_code?[t+' · exit '+j.exit_code,'danger']:[t,c];
}
registerDetail('jobs',{
  list:'작업',kinds:['jobs'],
  load:async id=>{
    const i=id.indexOf(':'),kind=id.slice(0,i),n=encodeURIComponent(id.slice(i+1));
    if(kind!=='job'&&kind!=='sched')return null;
    try{
      if(kind==='job')return await api('jobs/'+n);
      const sc=await api('schedules/'+n);sc.schedule=true;return sc;
    }catch(e){if(e.status===404)return null;throw e}
  },
  head:j=>({title:'#'+j.id+' '+j.label,badge:jobBadge(j)}),
  subs:j=>[['body',j.schedule?'예약':'명령']],
  draw:async(j,sub,box)=>{
    if(j.schedule){
      const cond=j.kind==='cron'?node('code',j.cron+' ('+j.tz+')'):when(j.next_fire_at||j.last_fire_at);
      const pairs=[['조건',cond],['다음',j.state==='active'?when(j.next_fire_at):'-'],['만료',when(j.expires_at)+(j.state==='paused'?' · 멈춘 동안 시계 멈춤':'')],['밀린',j.missed?j.missed+'회':'-'],['역할',j.role],['기계',j.machine],['ID',String(j.id)]];
      if(j.paused_by)pairs.push(['멈춘 쪽',j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name],['멈춘 시각',when(j.paused_at)],['까닭',j.pause_reason||'-']);
      pairs.push(['프롬프트',node('pre',j.prompt)]);
      box.append(dl(pairs));
      const msg=node('span','','msg');msg.setAttribute('role','status');
      const acts=node('div','','detail-acts');acts.append(...pauseButtons(j,msg),msg);
      if(acts.childElementCount>1)box.append(acts);
      return;
    }
    box.append(dl([['종류',jobKinds[j.kind]||j.kind],['역할',j.role],['기계',j.machine],['작업 폴더',node('code',j.cwd)],['시작',when(j.started_at||j.created_at)],['만료',when(j.expires_at)],['ID',String(j.id)],['명령',node('pre',j.command)]]));
    if(j.error)box.append(node('p',j.error,'err'));
    if(j.tail)box.append(node('h3','마지막 출력','sechead'),node('pre',j.tail));
  }
});
// A linked thread: the directive with its state (열림 / 끝남, the answered
// rule's Open), then the replies in order (답 marks one the answered rule
// counts). An open directive names its sender, who closes it. Each opens the
// mail detail.
function reportThread(t){
  const wrap=node('div','','rthread');
  const b=t.open?badge('열림','info'):badge('끝남','success');
  if(!t.open)b.title=t.completed?'받은 쪽이 끝냄':'보낸 쪽이 닫음';
  const extra=t.open?[b,' ',node('span','닫을 쪽 '+t.directive.sender,'muted')]:[b];
  wrap.append(reportMail(t.directive,extra,'div'));
  if(t.replies.length){
    const ol=node('ol','');
    for(const m of t.replies)ol.append(reportMail(m,m.answers?badge('답','success'):null,'li'));
    wrap.append(ol);
  }
  return wrap;
}
function reportMail(m,b,tag){
  const box=node(tag,''),line=node('div','','rline');
  const a=node('a','#'+m.id+' '+(m.subject||'(제목 없음)'));a.href=detailHash('mail',String(m.id),'body');
  a.onclick=e=>{e.preventDefault();openMessage(m.id)};
  line.append(a);if(b)line.append(' ',...[].concat(b));
  box.append(line,node('div',m.sender+' → '+m.recipient+' · '+shortWhen(m.created_at),'muted'));
  if(m.body)box.append(node('p',m.body.length>300?m.body.slice(0,300)+'…':m.body,'rbody'));
  return box;
}
registerDetail('reports',{
  list:'보고',kinds:['reports'],
  load:async id=>{
    try{return await api('reports/'+encodeURIComponent(id))}
    catch(e){if(e.status===404)return null;throw e}
  },
  head:r=>({title:r.title,badge:reportBadge(r)}),
  subs:()=>[['body','보고']],
  draw:async(r,sub,box)=>{
    if(r.request){const q=node('p','','reqline');q.append(node('span','요청 ','muted'),document.createTextNode(r.request));box.append(q)}
    box.append(dl([['보낸 쪽',r.author],['시각',when(r.created_at)],['ID',String(r.id)]]));
    const md=node('div','','md');md.append(mdRender(r.body));box.append(md);
    box.append(node('h3','연결된 지시 '+r.threads.length,'sechead'));
    if(!r.threads.length)box.append(node('p','연결된 지시 없음','empty'));
    for(const t of r.threads)box.append(reportThread(t));
  }
});
