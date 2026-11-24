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
// jobsLast keeps the last read, so 거르기 redraws without reading again.
let jobsLast=null;
function drawJobs(jobs,scheds,autos){
  jobsLast=[jobs,scheds,autos];
  const q=$('jobFind').value,redraw=()=>drawJobs(jobs,scheds,autos);
  const sched=scheds.filter(j=>jobLive(j.state)).sort(byNewest);
  const live=jobs.filter(j=>jobLive(j.state)).sort(byNewest);
  const ended=jobs.filter(j=>!jobLive(j.state)).concat(scheds.filter(j=>!jobLive(j.state))).sort(byNewest);
  const hit=j=>listMatch(q,j.label,j.role,j.machine,'#'+j.id);
  const rows=(list,each,empty)=>list.length?list.map(each):[node('p',empty,'empty ls-empty')];
  const sRows=sched.filter(hit).map(schedRow).concat(autos.filter(x=>listMatch(q,autoName(x.a),termRole(x.v),x.v.label)).map(x=>autoRow(x.v,x.a)));
  listFill($('jobs'),[
    ...listGroup('jobs-sched','예약',sRows.length?sRows:[node('p','예약 없음','empty ls-empty')],{count:sRows.length,redraw:redraw,add:jobAdds()}),
    ...listGroup('jobs-live','돌고 있는 작업',rows(live.filter(hit),jobRow,'돌고 있는 작업 없음'),{count:live.filter(hit).length,redraw:redraw}),
    ...listGroup('jobs-ended','끝난 작업',rows(ended.filter(hit),j=>j.schedule?schedRow(j):jobRow(j),'끝난 작업 없음'),{count:ended.filter(hit).length,fold:true,redraw:redraw}),
  ],'');
  setTabCount('jobs',sched.length+live.length||null);
  $('jobsNote').textContent='';
}
// jobAdds are the 예약 head's buttons (#551): a new role schedule and, with
// hosting, a new session's scheduled run; each opens an empty schedForm in
// the detail (#jobs/new:sched, new:auto).
function jobAdds(){
  const box=node('span','','grp-adds');
  const add=(text,title,id)=>{const b=node('button',text,'fr-ib grp-add');b.type='button';b.title=title;b.onclick=()=>openDetail('jobs',id,'body');return b};
  box.append(add('+ 역할 예약','역할 예약을 만든다','new:sched'));
  if(hostingOn)box.append(add('+ 세션 예약','세션 예약을 만든다','new:auto'));
  return box;
}
// jobRowOf is one list row: top is the small line's parts, st an optional
// [text,tone] at its right.
function jobRowOf(id,top,title,st){
  return listRow('jobs',id,{top:top.filter(Boolean).join(' · '),title:title,right:st?st[0]:'',tone:st?st[1]:''});
}
// jobAge: how long ago t was, as 「n분째」 for a thing still going.
function jobAge(t){
  if(!t)return '';
  const m=Math.max(0,Math.floor((Date.now()-new Date(t).getTime())/60000));
  return m<60?m+'분째':m<1440?Math.floor(m/60)+'시간째':Math.floor(m/1440)+'일째';
}
function jobRow(j){
  const st=jobBadge(j),live=jobLive(j.state);
  const r=jobRowOf('job:'+j.id,['#'+j.id,jobKinds[j.kind]||j.kind,j.role,j.machine,live?jobAge(j.started_at||j.created_at):shortWhen(j.ended_at||j.created_at)],j.label,live&&st[1]==='info'?null:st);
  if(!live)r.classList.add('done');
  return r;
}
// runAt is a run now's time as HH:MM (#588), with the date when not today.
function runAt(t){
  const d=new Date(t),hm=d.toLocaleTimeString('ko-KR',{hour:'2-digit',minute:'2-digit',hourCycle:'h23'});
  return d.toDateString()===new Date().toDateString()?hm:d.toLocaleDateString('ko-KR',{month:'numeric',day:'numeric'})+' '+hm;
}
function schedRow(j){
  const st=jobBadge(j),next=j.next_fire_at&&j.state==='active'?'다음 '+shortWhen(j.next_fire_at):'';
  const r=jobRowOf('sched:'+j.id,['#'+j.id,j.kind==='cron'?j.cron:'1회',j.last_run_at&&'지금 실행 · '+runAt(j.last_run_at),j.role,next],j.label,j.state==='active'?null:st);
  if(!jobLive(j.state))r.classList.add('done');
  return r;
}
function autoRow(v,a){
  const st=a.state==='paused'?['멈춤','warning']:a.alert?[autoResult[a.alert]||a.alert,autoTone[a.alert]||'neutral']:null;
  return jobRowOf('auto:'+v.id+':'+a.id,['세션 예약',a.cron,termRole(v),a.state==='paused'?'':'다음 '+shortWhen(a.next_fire_at)],autoName(a),st);
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
    if(ask){body=await ask(b);if(body===false)return}
    b.disabled=true;setMsg(msg,doing);
    try{await send(body);loadJobs();drawDetail()}
    catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
  });
  b.title=title;
  return b;
}
// runButton mails the prompt now (#588), after a confirm in the page; the
// next fire stays. A run again within the server's window is the same run.
// runFlash keeps the sent line for a minute across the detail's redraws.
let runFlash=null;
function runButton(j,msg){
  if(runFlash&&runFlash.id===j.id&&Date.now()-runFlash.at<60000)setMsg(msg,runFlash.text);
  if(j.state!=='active'&&j.state!=='paused')return [];
  const b=setButton('지금 실행','',async()=>{
    if(!await askOn(b,{text:'#'+j.id+' '+j.label+' 프롬프트를 지금 한 통 보냅니다. 다음 회차는 그대로입니다.',ok:'보내기 확인'}))return;
    b.disabled=true;setMsg(msg,'보내는 중…');
    try{
      const r=await apiSend('POST','schedules/'+encodeURIComponent(j.id)+'/run');
      runFlash={id:j.id,at:Date.now(),text:(r.repeated?'이미 보냄 · ':'지금 실행 · ')+runAt(r.schedule.last_run_at||new Date())+' · 메일 #'+r.mail_id};
      await loadJobs();drawDetail();
    }catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
  });
  b.title='프롬프트를 지금 한 번 보낸다 — 다음 회차는 그대로';
  return [b];
}
function pauseButtons(j,msg){
  const id=encodeURIComponent(j.id),out=[];
  const resume=(label,title,body,doing,ask)=>out.push(jobAct(label,title,msg,doing,ask,()=>apiSend('POST','schedules/'+id+'/resume',body)));
  if(j.state==='active')out.push(jobAct('멈춤','이 예약을 멈춘다 — 까닭을 남길 수 있다',msg,'멈추는 중…',at=>askOn(at,{text:'#'+j.id+' '+j.label+' 예약을 멈춥니다.',input:'까닭 (선택)',ok:'멈춤 확인'}).then(r=>r===null?false:{reason:r}),body=>apiSend('POST','schedules/'+id+'/pause',body)));
  else if(j.state==='paused'&&j.passed){
    resume('지금 보내고 켜기','지난 시각의 프롬프트를 지금 보내고 예약을 다시 켠다',{passed:'fire'},'켜는 중…');
    resume('버리고 끝내기','지난 시각을 버리고 예약을 끝낸다',{passed:'drop'},'끝내는 중…',at=>askOn(at,{text:'#'+j.id+' '+j.label+' 지난 시각을 버리고 예약을 끝낼까요?',ok:'끝내기 확인'}).then(r=>r?undefined:false));
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
    const stop=jobAct('중지','이 작업을 멈춘다',msg,'멈추는 중…',at=>askOn(at,{text:'#'+j.id+' '+j.label+' 작업을 멈출까요?',ok:'중지 확인'}).then(r=>r?undefined:false),()=>apiSend('POST','jobs/'+id+'/stop'));
    stop.disabled=!!j.stop_requested;b.push(stop);
  }else b.push(jobAct('지우기','이 기록을 지운다 — 되돌릴 수 없다',msg,'지우는 중…',at=>askOn(at,{text:'#'+j.id+' '+j.label+' 기록을 지울까요? 되돌릴 수 없습니다.',ok:'지우기 확인',danger:true}).then(r=>r?undefined:false),()=>apiSend('DELETE','jobs/'+id).then(closeDetail)));
  jobActs(box,b,msg);
  box.append(dl([['종류',jobKinds[j.kind]||j.kind],['역할',j.role],['기계',j.machine],['작업 폴더',node('code',j.cwd)],['시작',when(j.started_at||j.created_at)],['만료',when(j.expires_at)],['ID',String(j.id)],['명령',node('pre',j.command)]]));
  if(j.error)box.append(node('p',j.error,'err'));
  if(j.tail)box.append(node('h3','마지막 출력','dsec'),node('pre',j.tail));
}
function drawSched(j,box){
  const msg=node('span','','msg');msg.setAttribute('role','status');
  const rm=jobAct('지우기','이 예약을 지운다 — 되돌릴 수 없다',msg,'지우는 중…',at=>askOn(at,{text:'#'+j.id+' '+j.label+' 예약을 지울까요? 되돌릴 수 없습니다.',ok:'지우기 확인',danger:true}).then(r=>r?undefined:false),()=>apiSend('DELETE','schedules/'+encodeURIComponent(j.id)).then(closeDetail));
  const edit=j.state==='active'||j.state==='paused'?[editButton(j,false,body=>apiSend('PUT','schedules/'+encodeURIComponent(j.id),body),'시각·프롬프트·라벨을 고친다 — 번호와 기록은 그대로')]:[];
  jobActs(box,runButton(j,msg).concat(pauseButtons(j,msg),edit,rm),msg);
  const cond=j.kind==='cron'?node('code',j.cron+' ('+j.tz+')'):when(j.next_fire_at||j.last_fire_at);
  const pairs=[['조건',cond],['다음',j.state==='active'?when(j.next_fire_at):'-'],['지난 실행',when(j.last_fire_at)],['지금 실행',j.last_run_at?when(j.last_run_at):'-'],['만료',when(j.expires_at)+(j.state==='paused'?' · 멈춘 동안 시계 멈춤':'')],['밀린',j.missed?j.missed+'회':'-'],['역할',j.role],['기계',j.machine],['ID',String(j.id)]];
  if(j.paused_by)pairs.push(['멈춘 쪽',j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name],['멈춘 시각',when(j.paused_at)],['까닭',j.pause_reason||'-']);
  pairs.push(['프롬프트',node('pre',j.prompt)]);
  box.append(dl(pairs));
}
function drawJobAuto(x,box){
  const v=x.v,a=x.a,msg=node('span','','msg');msg.setAttribute('role','status');
  const b=[];
  b.push(editButton(a,true,body=>apiSend('PUT','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id,body),'cron·프롬프트·라벨을 고친다 — 번호와 실행 기록은 그대로'));
  b.push(jobAct('지금 실행','이 예약을 지금 한 번 돌린다',msg,'실행 요청 중…',null,()=>apiSend('POST','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+'/run')));
  if(a.state==='paused')b.push(jobAct('다시','멈춘 예약을 다시 켠다',msg,'켜는 중…',null,()=>apiSend('POST','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+'/resume')));
  else b.push(jobAct('멈춤','이 예약을 멈춘다',msg,'멈추는 중…',null,()=>apiSend('POST','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+'/pause')));
  b.push(jobAct('지우기','이 예약과 실행 기록을 지운다 — 되돌릴 수 없다',msg,'지우는 중…',at=>askOn(at,{text:autoName(a)+'을(를) 지울까요? 실행 기록도 함께 지워지고 되돌릴 수 없습니다.',ok:'지우기 확인',danger:true}).then(r=>r?undefined:false),()=>apiSend('DELETE','terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id).then(closeDetail)));
  jobActs(box,b,msg);
  box.append(dl([['세션',termRole(v)+(v.label?' · '+v.label:'')],['cron',a.cron+' ('+a.tz+')'],['다음',a.state==='paused'?'-':when(a.next_fire_at)],['사전 확인',a.check?node('code',a.check):'-'],['대화',a.resume?'이어서':'새 대화'],['최대 실행',Math.round(a.max_run_ms/60000)+'분'],['알림 받는 역할',(a.notify||[]).join(', ')||'-'],['ID',String(a.id)],['프롬프트',node('pre',a.prompt||'')]]));
  box.append(node('h3','지난 실행','dsec'),autoRuns(a.runs||[]));
}
// schedForm is the schedule form (#516): a role schedule (auto false: 종류,
// cron or 시각, 시간대) or a session's scheduled run (auto true: cron, read
// in the zone it was added in), with 프롬프트 and 라벨. v holds the values
// it opens with; v.fresh opens it empty for a new one. save gets the body:
// for an edit only what changed (a role schedule's time as kind with cron or
// at and tz), for a new one every field.
function schedForm(v,auto,save,cancel){
  const f=node('form','','fld sched-form'),fresh=!!v.fresh;
  const inp=(val,title,attrs)=>{const i=document.createElement('input');i.value=val==null?'':val;i.title=title;i.setAttribute('aria-label',title);i.autocomplete='off';i.spellcheck=false;for(const k in attrs||{})i.setAttribute(k,attrs[k]);return i};
  const lab=(t,el)=>{const l=node('label',t);l.title=el.title;f.append(l,el);return el};
  const local=t=>{if(!t)return '';const d=new Date(t);d.setMinutes(d.getMinutes()-d.getTimezoneOffset());return d.toISOString().slice(0,16)};
  let kind=null,at=null,tz=null;
  if(!auto){
    kind=document.createElement('select');kind.title='예약 종류';kind.setAttribute('aria-label','예약 종류');
    for(const [k,t] of [['cron','주기 (cron)'],['once','1회']]){const o=node('option',t);o.value=k;kind.append(o)}
    kind.value=v.kind||'cron';lab('종류',kind);
  }
  const cron=lab('cron',inp(v.kind==='once'?'':v.cron,'분 시 일 월 요일 (5칸)',{placeholder:'0 9 * * 1-5'}));
  if(!auto){
    at=lab('시각',inp(v.kind==='once'?local(v.next_fire_at):'','한 번 보낼 시각 (이 브라우저의 시간대)',{type:'datetime-local'}));
    tz=lab('시간대',inp(v.tz||Intl.DateTimeFormat().resolvedOptions().timeZone,'cron 을 읽는 시간대',{placeholder:'Asia/Seoul'}));
  }
  const prompt=lab('프롬프트',(()=>{const t=document.createElement('textarea');t.value=v.prompt||'';t.title='실행마다 보낼 글';t.setAttribute('aria-label','프롬프트');t.rows=4;t.required=true;return t})());
  const label=lab('라벨',inp(v.label,'비우면 예약 번호',{maxlength:'200'}));
  if(auto&&!fresh)f.append(node('label',''),node('span','시간대 '+(v.tz||'-'),'muted'));
  const show=()=>{if(!kind)return;const once=kind.value==='once';for(const [el,hide] of [[cron,once],[at,!once],[tz,once]]){el.hidden=hide;el.previousSibling.hidden=hide}};
  if(kind){kind.onchange=show;show()}
  const msg=node('span','','msg');msg.setAttribute('role','status');
  const ok=node('button',fresh?'만들기':'저장');ok.type='submit';
  const no=node('button','취소');no.type='button';no.onclick=()=>cancel&&cancel();
  const acts=node('span','','watch-acts');acts.append(ok,no,msg);f.append(node('label',''),acts);
  f.onsubmit=async e=>{
    e.preventDefault();
    const b={};
    if(auto){if(fresh||cron.value.trim()!==v.cron)b.cron=cron.value.trim()}
    else{
      const k=kind.value,timeChanged=fresh||k!==v.kind||(k==='cron'?cron.value.trim()!==v.cron||tz.value.trim()!==v.tz:at.value!==local(v.next_fire_at));
      if(timeChanged){
        b.kind=k;b.tz=tz.value.trim();
        if(k==='cron')b.cron=cron.value.trim();
        else{if(!at.value){setMsg(msg,'시각을 넣으세요','err');return}b.at=new Date(at.value).toISOString()}
      }
    }
    if(fresh||prompt.value!==(v.prompt||''))b.prompt=prompt.value;
    if(fresh||label.value.trim()!==(v.label||''))b.label=label.value.trim();
    if(!Object.keys(b).length){setMsg(msg,'바뀐 것 없음');return}
    ok.disabled=true;setMsg(msg,'저장하는 중…');
    try{await save(b)}catch(err){ok.disabled=false;setMsg(msg,err.message,'err')}
  };
  return f;
}
// drawNewSched is a new schedule's detail (#551): the role (or session) to
// make it for, in the caller's view, then schedForm empty. A save opens the
// new schedule.
async function drawNewSched(j,box){
  const pick=document.createElement('select');pick.required=true;
  pick.title=j.session?'예약을 돌릴 세션':'예약 메일을 받을 역할';pick.setAttribute('aria-label',j.session?'세션':'역할');
  if(j.session){for(const v of await api('terminals'))pick.append(new Option(termRole(v)+(v.label?' · '+v.label:''),v.id))}
  else{
    for(const v of await api('roles'))if(!v.retired_at)pick.append(new Option(v.role,v.role));
    if($('jobRole').value)pick.value=$('jobRole').value;
  }
  if(!pick.options.length){box.append(node('p',j.session?'세션 없음':'역할 없음','empty'));return}
  const f=schedForm({fresh:true},j.session,async b=>{
    const at=pick.value;
    const r=j.session?await apiSend('POST','terminals/'+encodeURIComponent(at)+'/autos',b):await apiSend('POST','schedules',Object.assign({role:at},b));
    loadJobs();
    openDetail('jobs',j.session?'auto:'+at+':'+r.id:'sched:'+r.id,'body');
  },closeDetail);
  const l=node('label',j.session?'세션':'역할');l.title=pick.title;
  f.prepend(l,pick);f.id='schedNew';
  box.append(f);
  pick.focus();
}
// editButton opens schedForm with v's values below the detail's buttons;
// a save sends the body (send) and draws the list and the detail again.
function editButton(v,auto,send,title){
  let f=null;
  const b=setButton('고치기','',()=>{
    if(f){f.remove();f=null;b.setAttribute('aria-expanded','false');return}
    f=schedForm(v,auto,async body=>{await send(body);loadJobs();drawDetail()},()=>{f.remove();f=null;b.setAttribute('aria-expanded','false')});
    f.id='schedEdit';b.setAttribute('aria-expanded','true');
    b.closest('.detail-acts').after(f);
    f.querySelector('textarea,input,select').focus();
  });
  b.title=title;b.setAttribute('aria-expanded','false');
  return b;
}
registerDetail('jobs',{
  list:'작업 · 예약',kinds:['jobs','terminals'],
  load:async id=>{
    const i=id.indexOf(':'),kind=id.slice(0,i),rest=id.slice(i+1);
    try{
      if(kind==='new')return rest==='sched'||rest==='auto'&&hostingOn?{fresh:true,session:rest==='auto'}:null;
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
  head:j=>j.fresh?{title:j.session?'새 세션 예약':'새 역할 예약',badge:null}:j.auto?{title:autoName(j.a),badge:j.a.state==='paused'?['멈춤','warning']:['활성','success']}:{title:'#'+j.id+' '+j.label,badge:jobBadge(j)},
  subs:j=>[['body',j.fresh||j.auto||j.schedule?'예약':'명령']],
  draw:async(j,sub,box)=>j.fresh?drawNewSched(j,box):j.auto?drawJobAuto(j,box):j.schedule?drawSched(j,box):drawJob(j,box)
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
