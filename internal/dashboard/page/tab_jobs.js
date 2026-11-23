// ---- 작업 · 예약 (#291, #515) ----
// Jobs, watches and schedules (#292) of every role, and the hosted sessions'
// scheduled runs (#336), in one list column: 예약, 돌고 있는 작업 and 끝난
// 작업 (folded). A row shows labels only (#410); it opens the item beside
// the list (#jobs/job:<id>, sched:<id> or auto:<session>:<id>, #404), which
// loads that one item with its command or prompt (the server records the
// read) and holds its buttons: 중지 and 지우기 for a job, 멈춤 · 다시 and
// 지우기 for a schedule, 지금 실행 · 멈춤 · 다시 · 지우기 for a session's
// run, each that loses work behind a confirm.
const jobKinds={command:'명령',watch:'감시',cron:'예약',once:'예약 1회'};
const jobStates={handed:['넘겨짐','neutral'],running:['실행 중','info'],exited:['끝남','success'],failed:['시작 실패','danger'],stopped:['중지됨','neutral'],expired:['만료','warning'],lost:['잃음','danger']};
const jobLive=s=>s==='handed'||s==='running'||s==='active'||s==='paused';
const schedStates={active:['예약됨','info'],paused:['멈춤','warning'],ended:['끝남','success'],expired:['만료','warning']};
let jobsSeq=0,jobRolesFilled=false;
async function loadJobs(){
  const seq=++jobsSeq;
  $('jobsNote').textContent='불러오는 중…';
  try{
    if(!jobRolesFilled){for(const v of await api('roles'))$('jobRole').append(new Option(v.role,v.role));jobRolesFilled=true}
    const role=$('jobRole').value,q=role?'?role='+encodeURIComponent(role):'';
    const [jobs,scheds,autos]=await Promise.all([api('jobs'+q),api('schedules'+q),jobAutos(role)]);
    if(seq!==jobsSeq)return;
    for(const sc of scheds)sc.schedule=true;
    drawJobs(jobs,scheds,autos);
  }catch(e){if(seq===jobsSeq)$('jobsNote').textContent=e.message}
}
// jobAutos is every hosted session's scheduled runs, with the session.
async function jobAutos(role){
  if(!hostingOn)return [];
  const terms=(await api('terminals')).filter(v=>!role||termRole(v)===role);
  const lists=await Promise.all(terms.map(v=>api('terminals/'+encodeURIComponent(v.id)+'/autos').then(l=>l.map(a=>({a:a,v:v})),()=>[])));
  return lists.flat();
}
const byNewest=(a,b)=>String(b.created_at).localeCompare(String(a.created_at));
function drawJobs(jobs,scheds,autos){
  const box=$('jobs');
  const sched=scheds.filter(j=>jobLive(j.state)).sort(byNewest);
  const live=jobs.filter(j=>jobLive(j.state)).sort(byNewest);
  const ended=jobs.filter(j=>!jobLive(j.state)).concat(scheds.filter(j=>!jobLive(j.state))).sort(byNewest);
  keepRowFocus(box,()=>{
    box.replaceChildren();
    const g1=lvGroup(box,'jobs-sched','예약',sched.length+autos.length);
    for(const j of sched)g1.append(schedRow(j));
    for(const x of autos)g1.append(autoRow(x.v,x.a));
    if(!sched.length&&!autos.length)lvEmpty(g1,'예약 없음');
    const g2=lvGroup(box,'jobs-live','돌고 있는 작업',live.length);
    for(const j of live)g2.append(jobRow(j));
    if(!live.length)lvEmpty(g2,'돌고 있는 작업 없음');
    const g3=lvGroup(box,'jobs-ended','끝난 작업',ended.length,{folded:true});
    for(const j of ended)g3.append(j.schedule?schedRow(j):jobRow(j));
    if(!ended.length)lvEmpty(g3,'끝난 작업 없음');
  });
  lvFind($('panel-jobs'),$('jobFind').value);
  setTabCount('jobs',sched.length+live.length||null);
  $('jobsNote').textContent='';
}
function jobRow(j){
  const st=jobBadge(j),live=jobLive(j.state);
  const meta=['#'+j.id+' · '+(jobKinds[j.kind]||j.kind),j.role,j.machine,live?lvAge(j.started_at||j.created_at):shortWhen(j.ended_at||j.created_at)];
  const r=lvRow('jobs','job:'+j.id,meta,j.label,live&&st[1]==='info'?null:st);
  if(!live)r.classList.add('done');
  return r;
}
function schedRow(j){
  const st=jobBadge(j),next=j.next_fire_at&&j.state==='active'?'다음 '+shortWhen(j.next_fire_at):'';
  const r=lvRow('jobs','sched:'+j.id,['#'+j.id+' · '+(j.kind==='cron'?j.cron:'1회'),j.role,next],j.label,j.state==='active'?null:st);
  if(!jobLive(j.state))r.classList.add('done');
  return r;
}
function autoRow(v,a){
  const st=a.state==='paused'?['멈춤','warning']:a.alert?[autoResult[a.alert]||a.alert,autoTone[a.alert]||'neutral']:null;
  return lvRow('jobs','auto:'+v.id+':'+a.id,['세션 예약 · '+a.cron,termRole(v),a.state==='paused'?'':'다음 '+shortWhen(a.next_fire_at)],autoName(a),st,[v.label||'']);
}
// A paused schedule (#404) names who paused it, when and why; 멈춤 asks for
// an optional reason, 다시 resumes. A once schedule whose time passed while
// paused (passed) resumes only by sending that prompt now or dropping it.
function pausedText(j){
  const by=j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name;
  return by+' · '+shortWhen(j.paused_at)+(j.pause_reason?' · '+j.pause_reason:'');
}
// jobAct runs one action of the open item: confirm (when given) first, then
// the request, then the list and the detail again.
function jobAct(label,title,msg,doing,ask,send){
  const b=setButton(label,'',async()=>{
    let body;
    if(ask){body=ask();if(body===false)return}
    b.disabled=true;setMsg(msg,doing);
    try{await send(body);loadJobs();drawDetail()}
    catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
  });
  b.title=title;
  return b;
}
function pauseButtons(j,msg){
  const id=encodeURIComponent(j.id),out=[];
  const resume=(label,title,body,doing,ask)=>out.push(jobAct(label,title,msg,doing,ask,()=>apiSend('POST','schedules/'+id+'/resume',body)));
  if(j.state==='active')out.push(jobAct('멈춤','이 예약을 멈춘다 — 까닭을 남길 수 있다',msg,'멈추는 중…',()=>{const r=prompt('#'+j.id+' '+j.label+' 예약을 멈춥니다. 까닭 (선택)','');return r===null?false:{reason:r}},body=>apiSend('POST','schedules/'+id+'/pause',body)));
  else if(j.state==='paused'&&j.passed){
    resume('지금 보내고 켜기','지난 시각의 프롬프트를 지금 보내고 예약을 다시 켠다',{passed:'fire'},'켜는 중…');
    resume('버리고 끝내기','지난 시각을 버리고 예약을 끝낸다',{passed:'drop'},'끝내는 중…',()=>confirm('#'+j.id+' '+j.label+' 지난 시각을 버리고 예약을 끝낼까요?')?undefined:false);
  }
  else if(j.state==='paused')resume('다시','멈춘 예약을 다시 켠다',{},'켜는 중…');
  return out;
}
function jobBadge(j){
  if(j.schedule)return j.state==='paused'&&j.passed?['멈춤 · 지나감','warning']:schedStates[j.state]||[j.state,'neutral'];
  const [t,c]=jobStates[j.state]||[j.state,'neutral'];
  let label=t,tone=c;
  if(j.state==='exited'&&j.exit_code){label+=' · exit '+j.exit_code;tone='danger'}
  if(j.stop_requested&&jobLive(j.state))label+=' · 중지 요청';
  return [label,tone];
}
function jobActs(box,buttons,msg){
  const acts=node('div','','detail-acts');acts.append(...buttons,msg);
  if(buttons.length)box.append(acts);
}
function drawJob(j,box){
  const msg=node('span','','msg');msg.setAttribute('role','status');
  const id=encodeURIComponent(j.id),b=[];
  if(jobLive(j.state)){
    const stop=jobAct('중지','이 작업을 멈춘다',msg,'멈추는 중…',()=>confirm('#'+j.id+' '+j.label+' 작업을 멈출까요?')?undefined:false,()=>apiSend('POST','jobs/'+id+'/stop'));
    stop.disabled=!!j.stop_requested;b.push(stop);
  }else b.push(jobAct('지우기','이 기록을 지운다 — 되돌릴 수 없다',msg,'지우는 중…',()=>confirm('#'+j.id+' '+j.label+' 기록을 지울까요? 되돌릴 수 없습니다.')?undefined:false,()=>apiSend('DELETE','jobs/'+id).then(closeDetail)));
  jobActs(box,b,msg);
  box.append(dl([['종류',jobKinds[j.kind]||j.kind],['역할',j.role],['기계',j.machine],['작업 폴더',node('code',j.cwd)],['시작',when(j.started_at||j.created_at)],['만료',when(j.expires_at)],['ID',String(j.id)],['명령',node('pre',j.command)]]));
  if(j.error)box.append(node('p',j.error,'err'));
  if(j.tail)box.append(node('h3','마지막 출력','dsec'),node('pre',j.tail));
}
function drawSched(j,box){
  const msg=node('span','','msg');msg.setAttribute('role','status');
  const rm=jobAct('지우기','이 예약을 지운다 — 되돌릴 수 없다',msg,'지우는 중…',()=>confirm('#'+j.id+' '+j.label+' 예약을 지울까요? 되돌릴 수 없습니다.')?undefined:false,()=>apiSend('DELETE','schedules/'+encodeURIComponent(j.id)).then(closeDetail));
  jobActs(box,pauseButtons(j,msg).concat(rm),msg);
  const cond=j.kind==='cron'?node('code',j.cron+' ('+j.tz+')'):when(j.next_fire_at||j.last_fire_at);
  const pairs=[['조건',cond],['다음',j.state==='active'?when(j.next_fire_at):'-'],['지난 실행',when(j.last_fire_at)],['만료',when(j.expires_at)+(j.state==='paused'?' · 멈춘 동안 시계 멈춤':'')],['밀린',j.missed?j.missed+'회':'-'],['역할',j.role],['기계',j.machine],['ID',String(j.id)]];
  if(j.paused_by)pairs.push(['멈춘 쪽',j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name],['멈춘 시각',when(j.paused_at)],['까닭',j.pause_reason||'-']);
  pairs.push(['프롬프트',node('pre',j.prompt)]);
  box.append(dl(pairs));
}
function drawJobAuto(x,box){
  const v=x.v,a=x.a,msg=node('span','','msg');msg.setAttribute('role','status');
  const b=[];
  b.push(jobAct('지금 실행','이 예약을 지금 한 번 돌린다',msg,'실행 요청 중…',null,()=>apiSend('POST','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+'/run')));
  if(a.state==='paused')b.push(jobAct('다시','멈춘 예약을 다시 켠다',msg,'켜는 중…',null,()=>apiSend('POST','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+'/resume')));
  else b.push(jobAct('멈춤','이 예약을 멈춘다',msg,'멈추는 중…',null,()=>apiSend('POST','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+'/pause')));
  b.push(jobAct('지우기','이 예약과 실행 기록을 지운다 — 되돌릴 수 없다',msg,'지우는 중…',()=>confirm(autoName(a)+'을(를) 지울까요? 실행 기록도 함께 지워지고 되돌릴 수 없습니다.')?undefined:false,()=>apiSend('DELETE','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id).then(closeDetail)));
  jobActs(box,b,msg);
  box.append(dl([['세션',termRole(v)+(v.label?' · '+v.label:'')],['cron',a.cron+' ('+a.tz+')'],['다음',a.state==='paused'?'-':when(a.next_fire_at)],['사전 확인',a.check?node('code',a.check):'-'],['대화',a.resume?'이어서':'새 대화'],['최대 실행',Math.round(a.max_run_ms/60000)+'분'],['알림 받는 역할',(a.notify||[]).join(', ')||'-'],['ID',String(a.id)],['프롬프트',node('pre',a.prompt||'')]]));
  box.append(node('h3','지난 실행','dsec'),autoRuns(a.runs||[]));
}
registerDetail('jobs',{
  list:'작업 · 예약',kinds:['jobs','terminals'],
  load:async id=>{
    const i=id.indexOf(':'),kind=id.slice(0,i),rest=id.slice(i+1);
    try{
      if(kind==='job')return await api('jobs/'+encodeURIComponent(rest));
      if(kind==='sched'){const sc=await api('schedules/'+encodeURIComponent(rest));sc.schedule=true;return sc}
      if(kind==='auto'&&hostingOn){
        const k=rest.lastIndexOf(':'),sid=rest.slice(0,k),aid=rest.slice(k+1);
        const v=(await api('terminals')).find(t=>String(t.id)===sid);if(!v)return null;
        return {auto:true,v:v,a:await api('terminals/'+encodeURIComponent(sid)+'/autos/'+encodeURIComponent(aid)+'?runs=20')};
      }
      return null;
    }catch(e){if(e.status===404)return null;throw e}
  },
  head:j=>j.auto?{title:autoName(j.a),badge:j.a.state==='paused'?['멈춤','warning']:['활성','success']}:{title:'#'+j.id+' '+j.label,badge:jobBadge(j)},
  subs:j=>[['body',j.auto||j.schedule?'예약':'명령']],
  draw:async(j,sub,box)=>j.auto?drawJobAuto(j,box):j.schedule?drawSched(j,box):drawJob(j,box)
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
  head:r=>({title:'#'+r.id+' '+r.title,badge:reportBadge(r),crumb:{path:['보고 #'+r.id,r.author,r.request_id?'지시 '+reportRequestName(r.request_id):''],
    meta:[[shortWhen(r.created_at),'올린 때 — '+when(r.created_at)],['메일 '+r.threads.length,'이 보고에 딸린 지시 메일']]}}),
  subs:()=>[['body','보고']],
  draw:async(r,sub,box)=>{
    if(r.request){const q=node('p','','reqline');q.append(node('span','요청 ','muted'),document.createTextNode(r.request));box.append(q)}
    box.append(dl([['보낸 쪽',r.author],['시각',when(r.created_at)],['ID',String(r.id)]]));
    const md=node('div','','md');md.append(mdRender(r.body));box.append(md);
    box.append(node('h3','연결된 지시 '+r.threads.length,'sechead'));
    if(!r.threads.length)box.append(node('p','연결된 지시 없음','empty'));
    for(const t of r.threads)box.append(reportThread(t));
    box.append(reportBar(r));
  }
});
