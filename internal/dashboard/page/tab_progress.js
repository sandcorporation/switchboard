// ---- 진행 ----
// One load reads GET /api/admin/progress with the pending questions and
// permission requests. The list is 대기 (questions, permission requests,
// sessions in error or blocked, directives over the pending alert threshold,
// directives answered but not completed whose sessions sit idle (멈춤),
// what answers that ended directives left undone (남긴 것, #470: a second
// line with the owner's request (지시 N, #564), the directive mail and
// [이슈] [새 지시] [끝남] [버림]; the ones under no request in their own
// 지시 없음 group with [지시에 붙이기]), in that order of
// kind, oldest first within a kind) and then a row per service. Rows open in the detail panel under #progress/<kind>:<id>:
// q: 질문, p: 권한, s: 세션, d: 지시, m: 멈춤 (the directive), svc: 서비스. The tab number is the
// open directives older than the pending alert threshold (0 hides it).
const badgeLabel={error:'오류',blocked:'막힘',busy:'바쁨',compacting:'compact 중',idle:'유휴',none:'세션 없음'};
const badgeTone={error:'danger',blocked:'warning',busy:'info',compacting:'info',idle:'success',none:'neutral'};
function rel(now,t){if(!t)return '-';const s=Math.max(0,Math.round((new Date(now)-new Date(t))/1000));return s<60?'방금':dur(s)+' 전'}
function secsSince(now,t){return Math.max(0,Math.round((new Date(now)-new Date(t))/1000))}
const waitKinds=[['q','질문','warning'],['p','권한','info'],['s','오류','danger'],['d','지시','neutral'],['m','멈춤','warning'],['l','남긴 것','warning'],['n','지시 없음','neutral'],['v','옛 판','warning']];
const waitCols=['종류','항목','역할','지난 시간'];
const svcCols=['서비스','상태','세션','열린 지시','남긴 것','마지막 소식'];
// recipLabel is an open directive's recipient state: offline apart from idle (#269).
const recipLabel={offline:'오프라인',none:'오프라인'};
function recipWho(x){const l=recipLabel[x.recipient_state]||badgeLabel[x.recipient_state];return x.to+(l&&x.recipient_state!=='busy'?' · '+l:'')}
const svcChips=[['all','전체',()=>true],['problem','문제 있음',p=>/^(error|blocked)$/.test(p.badge)],['stale','오래된 지시',p=>p.open_directives.some(d=>d.over_threshold)]];
let progressSeq=0,progressData=null,svcFilter='all';
// waitItems builds the 대기 rows: {kind,id,item,role,age} with age in seconds.
function waitItems(d){
  const now=d.r.generated_at,by={q:[],p:[],s:[],d:[],m:[],l:[],n:[],v:[]};
  for(const x of d.r.left||[])by[x.request?'l':'n'].push({id:String(x.id),item:x.kind==='unrecorded'?'적지 않음':x.text,role:x.recorded_by,age:secsSince(now,x.recorded_at),left:x});
  for(const x of d.r.cli_behind||[])by.v.push({id:x.machine,item:x.machine+' · '+(x.version==='unknown'?'판 모름':x.version),role:x.roles.join(', '),age:null});
  for(const q of d.questions)by.q.push({id:q.id,item:q.questions.map(x=>x.header).join(' · '),role:q.asker,age:secsSince(now,q.created_at)});
  for(const v of d.perms)by.p.push({id:v.id,item:v.tool+(v.summary?' · '+v.summary:''),role:v.requester,age:secsSince(now,v.created_at)});
  for(const p of d.r.services){
    for(const v of p.sessions){const m=/^(error|blocked):(.*)$/.exec(v.state||'');if(m)by.s.push({id:v.role,item:m[2]||badgeLabel[m[1]],role:v.role,age:secsSince(now,v.since),tone:m[1]==='blocked'?'warning':'danger',label:badgeLabel[m[1]]})}
    for(const x of p.open_directives)if(x.over_threshold)by.d.push({id:String(x.id),item:'#'+x.id+' '+(x.subject||'(제목 없음)'),role:recipWho(x),age:x.age_s});
    for(const x of p.stalled||[])by.m.push({id:String(x.id),item:'#'+x.id+' '+(x.subject||'(제목 없음)'),role:x.to,age:x.idle_s});
  }
  const out=[];
  for(const [k,label,tone] of waitKinds)for(const x of by[k].sort((a,b)=>(b.age||0)-(a.age||0)))out.push(Object.assign({kind:k,label:label,tone:tone},x));
  return out;
}
// progressFetch reads what 진행 shows: the progress view with the pending
// questions and permission requests.
async function progressFetch(){
  const [r,q,pm]=await Promise.all([api('progress'),api('questions?status=pending&limit=500'),api('permissions?status=pending')]);
  return {r:r,questions:q.questions,perms:pm.permissions};
}
// refreshProgressCount puts the 대기 count on the 진행 menu row (#576)
// while the tab has not been opened.
async function refreshProgressCount(){
  try{setTabCount('progress',waitItems(await progressFetch()).length||null)}catch(e){}
}
async function loadProgress(){
  const seq=++progressSeq;
  $('progressNote').textContent='불러오는 중…';
  try{
    const d=await progressFetch();
    if(seq!==progressSeq)return;
    progressData=d;
    drawProgress();
  }catch(e){if(seq===progressSeq)$('progressNote').textContent=e.message}
}
// cell(text,label,cls) is one list cell; label shows before it in the stacked layout.
function cell(text,label,cls){const td=node('td',text,cls||'');if(label)td.dataset.label=label;return td}
// cardBreak ends the first line of a two-line card row below 600px.
function cardBreak(){const td=node('td','','cardbr');td.setAttribute('aria-hidden','true');return td}
function badge(text,tone){return node('span',text,'sc-badge '+(toneClass[tone]||toneClass.neutral))}
function drawProgress(){
  const d=progressData;if(!d)return;
  const r=d.r,now=r.generated_at,items=waitItems(d);
  const wait=$('wait');
  keepRowFocus(wait,()=>{
    wait.replaceChildren();
    for(const x of items){
      const tr=node('tr','','w-'+x.kind);
      const k=cell('');k.append(badge(x.label,x.tone));
      const it=cell(x.item,'','witem');
      if(x.left)it.append(leftLine(x.left));
      tr.append(k,it,cardBreak(),cell(x.role,'','mono'),cell(x.age==null?'-':dur(x.age),'','num nowrap'));
      if(x.kind!=='v'&&x.kind!=='l'&&x.kind!=='n')detailRow(tr,'progress',x.kind+':'+x.id);
      wait.append(tr);
    }
    if(!items.length)emptyRow(wait,waitCols.length,'대기 없음');
  });
  $('waitCount').textContent=String(items.length);
  const chips=$('svcChips');chips.replaceChildren();
  for(const [k,label,f] of svcChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===svcFilter?'true':'false');
    b.append(node('span',String(r.services.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{svcFilter=k;drawProgress()};
    chips.append(b);
  }
  const keep=svcChips.find(c=>c[0]===svcFilter)[2],shown=r.services.filter(keep);
  const body=$('progress');
  let over=0;
  for(const p of r.services)over+=p.open_directives.filter(d=>d.over_threshold).length;
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const p of shown){
      const tr=node('tr','',/^(error|blocked)$/.test(p.badge)?'row-'+p.badge:'');
      const st=cell('');st.append(badge(badgeLabel[p.badge]||p.badge,badgeTone[p.badge]||'neutral'));
      const stale=p.open_directives.filter(d=>d.over_threshold).length;
      const dirs=cell(String(p.open_directives.length),'열린 지시','num nowrap');
      if(stale){dirs.classList.add('over');dirs.title='문턱('+dur(r.pending_alert_s)+') 넘음 '+stale+'개'}
      const rep=p.last_report;
      const news=cell(rep?(rep.subject||'(제목 없음)')+' · '+rel(now,rep.at):p.last_activity_at?'보고 없음 · 마지막 활동 '+rel(now,p.last_activity_at):'소식 없음','마지막 소식','muted');
      if(rep)news.title=rep.from+' → '+rep.to+' · '+when(rep.at);
      tr.append(cell(p.service,'','strong'),st,cardBreak(),cell(String(p.sessions.length),'세션','num nowrap'),dirs,cell(String(p.left||0),'남긴 것','num nowrap'),news);
      detailRow(tr,'progress','svc:'+p.service);
      body.append(tr);
    }
    if(!shown.length)emptyRow(body,svcCols.length,'서비스 없음');
  });
  setTabCount('progress',items.length||null);
  $('progressNote').textContent='대기 '+items.length+' · '+r.services.length+'개 서비스'+(over?' · 오래된 지시 '+over+'개':'');
  $('progressNote').title=when(r.generated_at);
}
// leftLine is the second line of a 남긴 것 row (#470): the directive it came
// from and its ends. 이슈, 새 지시 and 끝남 do what the directive's sender
// does with left issue, left pass and left done (#553); 버림, with a reason,
// needs a login allowed to discard.
function leftLine(v){
  const box=node('div','','wsub'),msg=node('span','','msg');msg.setAttribute('role','status');
  const dir=setButton('메일 #'+v.directive,'linkish',()=>openMessage(v.directive));
  if(v.directive_subject)dir.title=v.directive_subject;
  let req;
  if(v.request){req=setButton('지시 '+v.request,'linkish',()=>openRequest(v.request));if(v.request_title)req.title=v.request_title}
  const id=encodeURIComponent(v.id);
  // Each end asks for its value on the row (#581), not in a browser prompt.
  const end=(label,send,ask,body)=>setButton(label,'',async e=>{
    const b=e.currentTarget;
    const a=await askOn(b,{text:label,input:ask,ok:label+' 확인',check:x=>!!body(x),bad:ask});if(a===null)return;
    b.disabled=true;setMsg(msg,'보내는 중…');
    try{await send(body(a));loadProgress()}
    catch(err){b.disabled=false;setMsg(msg,err.message,'err')}
  });
  if(!v.request)req=end('지시에 붙이기',b=>apiSend('POST','left/'+id+'/request',b),'#'+v.id+'을(를) 붙일 지시 번호',a=>/^[1-9][0-9]*$/.test(a)?{request:Number(a)}:null);
  box.append(req,dir,
    end('이슈',b=>apiSend('POST','left/'+id+'/issue',b),'#'+v.id+' 이슈 주소',a=>/^https?:\/\/\S+$/.test(a)?{url:a}:null),
    end('새 지시',b=>apiSend('POST','left/'+id+'/pass',b),'#'+v.id+'을(를) 넘길 지시 번호 ('+v.directive_from+'이(가) 보낸 지시)',a=>/^[1-9][0-9]*$/.test(a)?{directive:Number(a)}:null),
    doneButton(v,msg),
    ...(can('discard')?[end('버림',b=>apiSend('POST','left/'+id+'/drop',b),'#'+v.id+' 버리는 까닭',a=>a?{reason:a}:null)]:[]),
    msg);
  return box;
}
// doneButton is 남긴 것's [끝남]: it asks on the row, not in a browser
// confirm (#576) — [끝남] turns into [끝남 확인] and [취소].
function doneButton(v,msg){
  const id=encodeURIComponent(v.id);
  const done=setButton('끝남','',()=>{
    const ok=setButton('끝남 확인','',async()=>{
      ok.disabled=no.disabled=true;setMsg(msg,'보내는 중…');
      try{await apiSend('POST','left/'+id+'/done',{});loadProgress()}
      catch(err){ok.disabled=no.disabled=false;setMsg(msg,err.message,'err')}
    },'#'+v.id+' 끝남으로 처리 — 다시 알리지 않음');
    const no=setButton('취소','',()=>{no.remove();ok.replaceWith(done);setMsg(msg,'');done.focus()},'끝남으로 처리하지 않는다');
    done.replaceWith(ok);ok.after(no);setMsg(msg,'#'+v.id+' 끝남으로 처리할까요?');ok.focus();
  },'#'+v.id+' 끝남 — 다시 알리지 않음');
  return done;
}
// The 진행 detail hands each kind to its own spec: q to 질문, p to 권한 요청,
// s to 세션, d (지시: the mail thread, with 닫기) and svc (서비스) are here.
const progressDirective={
  load:async id=>{
    const [m,r]=await Promise.all([details.mail.load(id),api('progress')]);
    if(!m)return null;
    let open=null,stall=null;
    for(const p of r.services){for(const d of p.open_directives)if(String(d.id)===id)open=d;for(const d of p.stalled||[])if(String(d.id)===id)stall=d}
    return {m:m,open:open,stall:stall,limit:r.pending_alert_s,now:r.generated_at};
  },
  head:x=>x.open?{title:'#'+x.m.id+' '+x.m.subject,badge:[x.open.over_threshold?'오래된 지시':'열림',x.open.over_threshold?'danger':'warning']}:x.stall?{title:'#'+x.m.id+' '+x.m.subject,badge:['멈춤','warning']}:details.mail.head(x.m),
  subs:()=>[['thread','스레드'],['body','본문']],
  draw:async(x,sub,box)=>{
    if(x.open){
      const row=node('div','','row'),msg=node('span','','msg');msg.setAttribute('role','status');
      const close=node('button','닫기');close.type='button';close.title='끝난 것으로 닫기';
      close.onclick=async()=>{
        if(!await askOn(close,{text:'#'+x.m.id+' 지시를 닫을까요?',ok:'닫기 확인'}))return;
        close.disabled=true;setMsg(msg,'닫는 중…');
        try{await apiSend('POST','messages/'+encodeURIComponent(x.m.id)+'/close');setMsg(msg,'닫음','ok');loadProgress();drawDetail()}
        catch(e){close.disabled=false;setMsg(msg,e.message,'err')}
      };
      row.append(close,msg);
      box.append(dl([['받는 쪽',recipWho(x.open)],['상태',statusLabel[x.open.status]||x.open.status],['지난 시간',dur(x.open.age_s)+(x.open.over_threshold?' · 문턱('+dur(x.limit)+') 넘음':'')]].concat(x.open.replied_at?[['마지막 답',rel(x.now,x.open.replied_at)]]:[],x.stall?[['idle 세션',x.stall.sessions.join(', ')],['idle',dur(x.stall.idle_s)]]:[])),row);
    }
    if(x.stall&&!x.open)box.append(dl([['받는 쪽',x.stall.to]].concat(x.stall.answered_at?[['마지막 답',rel(x.now,x.stall.answered_at)]]:[],[['idle 세션',x.stall.sessions.join(', ')],['idle',dur(x.stall.idle_s)]])));
    await details.mail.draw(x.m,sub,box);
  }
};
const progressService={
  load:async id=>{const r=await api('progress');const p=r.services.find(s=>s.service===id);return p?{p:p,now:r.generated_at,limit:r.pending_alert_s}:null},
  head:x=>({title:x.p.service,badge:[badgeLabel[x.p.badge]||x.p.badge,badgeTone[x.p.badge]||'neutral']}),
  subs:x=>[['sessions','세션 '+x.p.sessions.length],['directives','열린 지시 '+x.p.open_directives.length],['news','마지막 소식']],
  draw:async(x,sub,box)=>{
    const p=x.p,now=x.now;
    if(sub==='directives'){
      const ul=node('ul','','dlist');
      for(const d of p.open_directives){
        const li=node('li','',d.over_threshold?'over':'');const a=node('a','#'+d.id+' '+(d.subject||'(제목 없음)'));a.href=detailHash('progress','d:'+d.id,'thread');
        li.append(a,node('small',recipWho(d)+' · '+(statusLabel[d.status]||d.status)+(d.replied_at?' · 답 있음':'')+' · '+dur(d.age_s)+' 지남'+(d.over_threshold?' · 문턱('+dur(x.limit)+') 넘음':'')));
        ul.append(li);
      }
      box.append(p.open_directives.length?ul:node('p','열린 지시 없음','empty'));
      return;
    }
    if(sub==='news'){
      const r=p.last_report;
      if(r){
        const a=node('a','#'+r.id+' '+(r.subject||'(제목 없음)'));a.href=detailHash('mail',r.id,'body');
        box.append(dl([['보고',a],['보낸 쪽',r.from+' → '+r.to],['시각',when(r.at)+' · '+rel(now,r.at)],['마지막 활동',p.last_activity_at?when(p.last_activity_at):'-']]));
      }else box.append(dl([['보고','보고 없음'],['마지막 활동',p.last_activity_at?when(p.last_activity_at)+' · '+rel(now,p.last_activity_at):'소식 없음']]));
      return;
    }
    const ul=node('ul','','dlist');
    for(const v of p.sessions){
      const li=node('li',''),[text,cls]=sessionStatus(v.state);
      const a=node('a',v.role);a.href=detailHash('progress','s:'+v.role,'status');
      const line=node('small','');
      line.append(node('span',text,cls),...(subagentNote(v.subagents)?[document.createTextNode(' · '),node('span',subagentNote(v.subagents),'subs')]:[]),document.createTextNode(' · '+dur(secsSince(now,v.since))+'째'));
      li.append(a,line);
      // 폴더는 #346처럼 앞쪽을 줄임표로 줄이고 전체 경로는 툴팁으로.
      if(v.directory){const d=node('span','‎'+v.directory+'‎','pdir');d.title=v.directory;li.append(d)}
      ul.append(li);
    }
    box.append(p.sessions.length?ul:node('p','세션 없음','empty'));
  }
};
const progressKinds={q:'questions',p:'permissions',s:'sessions',d:progressDirective,m:progressDirective,svc:progressService};
function progressSpec(k){const v=progressKinds[k];return typeof v==='string'?details[v]:v}
registerDetail('progress',{
  list:'진행',kinds:['progress','questions','permissions','sessions','mail'],
  load:async id=>{
    const i=id.indexOf(':'),spec=i>0?progressSpec(id.slice(0,i)):null;
    if(!spec)return null;
    const item=await spec.load(id.slice(i+1));
    return item?{spec:spec,item:item}:null;
  },
  head:x=>x.spec.head(x.item),
  subs:x=>x.spec.subs(x.item),
  draw:(x,sub,box)=>x.spec.draw(x.item,sub,box)
});
tabLoaders.progress=loadProgress;
tabLoaders.metrics=loadMetrics;
