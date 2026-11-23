// ---- 답이 필요함 (질문 · 권한 요청 · 규칙 제안) ----
// One list column (#514): the pending questions, the pending permission
// requests and the proposed rules, then the answered questions and the closed
// requests, folded. A row shows its item in the body (#questions/<id>,
// #questions/p:<id> for a request, #questions/r:<id> for a rule), drawn by
// the question spec below or the permissions / rules detail spec. 답하기
// holds the answer form while the question is pending and the admin may
// answer, else the stored answer. A form is kept per question across live
// redraws so a half-filled answer survives them.
const qStatusLabel={pending:'대기',answered:'답함',withdrawn:'거둠'};
const qTone={pending:'warning',answered:'success',withdrawn:'neutral'};
let formSeq=0,needsData=null,needsSeq=0;
const qForms=new Map(),qNotes=new Map();
function setMsg(el,text,kind){el.textContent=text;el.className='msg'+(kind?' '+kind:'')}
function qTitle(q){return q.questions.map(x=>x.header).join(' · ')}
async function refreshQuestionCount(){
  try{setTabCount('questions',(await api('questions?status=pending&limit=500')).questions.length)}catch(e){}
}
async function loadQuestions(){
  const seq=++needsSeq;
  $('questionsNote').textContent='불러오는 중…';
  try{
    const [qs,open,closed,rules]=await Promise.all([api('questions?limit=200').then(x=>x.questions),
      api('permissions?status=pending').then(x=>x.permissions),api('permissions?status=closed&limit=200').then(x=>x.permissions),
      api('rules?proposed=1').then(x=>x.rules||[]).catch(()=>[])]);
    if(seq!==needsSeq)return;
    needsData={qs:qs,open:open,closed:closed,rules:rules.filter(r=>r.status==='proposed')};
    const n=qs.filter(q=>q.status==='pending').length;
    setTabCount('questions',n);setTabCount('permissions',open.length||null);
    drawNeeds();
    // A pending request turns expired by time alone: reload at the next expiry.
    clearTimeout(permTimer);
    const next=Math.min(...open.map(v=>new Date(v.expires_at).getTime()));
    if(isFinite(next))permTimer=setTimeout(()=>{if(tabShown('questions'))loadQuestions()},Math.max(1000,next-Date.now()+500));
  }catch(e){if(seq===needsSeq)$('questionsNote').textContent=e.message}
}
function drawNeeds(){
  const d=needsData;if(!d)return;
  const f=$('needsFilter').value,out=[];
  const qm=q=>listMatch(f,q.asker,qTitle(q),q.id,...q.questions.map(x=>x.question));
  const pm=v=>listMatch(f,v.requester,v.tool,v.summary,v.id);
  const rm=r=>listMatch(f,r.author,r.topic,r.text,'#'+r.id);
  const qp=d.qs.filter(q=>q.status==='pending'&&qm(q)),pp=d.open.filter(pm),rp=d.rules.filter(rm);
  const qd=d.qs.filter(q=>q.status!=='pending'&&qm(q)),pd=d.closed.filter(pm);
  const g=(k,label,rows,o)=>{if(rows.length)out.push(...listGroup('need:'+k,label,rows,Object.assign({redraw:drawNeeds},o)))};
  g('q','질문',qp.map(questionRow),{hover:'답을 기다리는 질문'});
  g('p','권한 요청',pp.map(needPermRow),{hover:'답을 기다리는 권한 요청'});
  g('r','규칙 제안',rp.map(ruleNeedRow),{hover:'채택이나 거절을 기다리는 규칙'});
  g('qd','지난 질문',qd.map(questionRow),{fold:true,hover:'답했거나 거둔 질문'});
  g('pd','지난 권한 요청',pd.map(needPermRow),{fold:true,hover:'허용·거절·만료된 권한 요청'});
  listFill($('questions'),out,f?'맞는 것 없음':'답이 필요한 것 없음');
  $('questionsNote').textContent=String(qp.length+pp.length+rp.length);
  $('questionsNote').title='질문 '+qp.length+' · 권한 요청 '+pp.length+' · 규칙 제안 '+rp.length;
}
function questionRow(q){
  const marks=[q.answerer==='owner'?'대표':'',q.delivery==='waiting'?'전달 대기':''].filter(Boolean).join(' · ');
  return listRow('questions',q.id,{top:q.asker+(marks?' · '+marks:''),right:q.status==='pending'?ago(new Date(),q.created_at):qStatusLabel[q.status]||q.status,rightHover:when(q.created_at),
    title:qTitle(q),hover:q.questions.map(x=>x.question).join(' / '),cls:q.status==='pending'?'un':'done'});
}
function needPermRow(v){
  return listRow('questions','p:'+v.id,{top:v.requester,right:v.status==='pending'?ago(new Date(),v.created_at):permStatusLabel[v.status]||v.status,rightHover:when(v.created_at),
    title:v.tool+(v.summary?': '+v.summary:''),hover:v.tool+(v.summary?': '+v.summary:'')+' — '+v.requester,cls:v.status==='pending'?'un':'done'});
}
function ruleNeedRow(r){
  return listRow('questions','r:'+r.id,{top:'#'+r.id+' · '+(r.author||'-'),right:shortWhen(r.created_at),rightHover:when(r.created_at),
    title:(ruleKinds[r.kind]||r.kind)+' · '+r.topic+' — '+ruleFirstLine(r.text),hover:r.text});
}
// questionPills adds the 대표 and 전달 대기 marks to h.
function questionPills(q,h){
  if(q.answerer==='owner')h.append(node('span','대표','pill owner'));
  if(q.delivery==='waiting')h.append(node('span','전달 대기','pill waiting'));
}
async function findQuestion(id){
  let q=(await api('questions?limit=200')).questions.find(x=>x.id===id);
  if(!q)q=(await api('questions?status=pending&limit=500')).questions.find(x=>x.id===id);
  return q||null;
}
const questionSpec={
  list:'답이 필요함',kinds:['questions'],
  load:async id=>{
    const q=await findQuestion(id);
    if(!q)return null;
    let decs=[];
    try{decs=(await api('decisions?'+new URLSearchParams({role:q.asker}))).decisions.filter(d=>d.id!==q.id)}catch(e){}
    return {q:q,decs:decs};
  },
  head:x=>({title:qTitle(x.q),badge:[qStatusLabel[x.q.status]||x.q.status,qTone[x.q.status]||'neutral'],crumb:{path:['질문',x.q.asker],
    meta:[[ago(new Date(),x.q.created_at),'올라온 때 — '+when(x.q.created_at)],[x.q.questions.length>1?'물음 '+x.q.questions.length:x.q.questions[0]&&x.q.questions[0].multiSelect?'여러 개 고르기':'한 개 고르기','답하는 방식']]}}),
  subs:x=>[['answer',x.q.status==='pending'&&x.q.can_answer?'답하기':'답'],['decisions','관련 결정 '+x.decs.length]],
  draw:async(x,sub,box)=>{
    const q=x.q;
    if(sub==='decisions'){
      for(const d of x.decs.slice(0,20))box.append(decisionRow(d));
      if(!x.decs.length)box.append(node('p','관련 결정 없음','empty'));
      return;
    }
    const pairs=[['묻는 쪽',q.asker],['시각',when(q.created_at)],['ID',q.id]];
    if(q.relay_to&&q.relay_to.length)pairs.push(['중계',q.relay_to.join(', ')]);
    const marks=node('span','');questionPills(q,marks);
    if(marks.childNodes.length)pairs.push(['표시',marks]);
    box.append(dl(pairs));
    const note=qNotes.get(q.id);
    if(note)box.append(node('p',note,'msg err'));
    if(q.status==='pending'&&q.can_answer){
      let f=qForms.get(q.id);
      if(!f){f=answerForm(q);qForms.set(q.id,f)}
      box.append(f);
      return;
    }
    qForms.delete(q.id);
    box.append(...questionAnswered(q));
  }
};
// needItem splits a 답이 필요함 id: p:<id> a permission request, r:<id> a
// rule, else a question.
function needItem(id){
  if(id.startsWith('p:'))return ['permissions',id.slice(2)];
  if(id.startsWith('r:'))return ['rules',id.slice(2)];
  return ['',id];
}
function needSpec(kind){return kind?details[kind]:questionSpec}
registerDetail('questions',{
  list:'답이 필요함',kinds:['questions','permissions','rules'],
  load:async id=>{
    const [k,raw]=needItem(id),x=await needSpec(k).load(raw);
    return x?{k:k,x:x}:null;
  },
  head:w=>{
    const h=needSpec(w.k).head(w.x);
    if(w.k==='permissions')h.crumb={path:['권한 요청',w.x.requester],meta:[[ago(new Date(),w.x.created_at),'요청한 때 — '+when(w.x.created_at)],['만료 '+shortWhen(w.x.expires_at),'답이 없으면 이때 만료 — '+when(w.x.expires_at)]]};
    if(w.k==='rules')h.crumb={path:['규칙 #'+w.x.id,w.x.author],meta:[[shortWhen(w.x.created_at),'올라온 때 — '+when(w.x.created_at)]]};
    return h;
  },
  subs:w=>needSpec(w.k).subs(w.x),
  draw:(w,sub,box)=>needSpec(w.k).draw(w.x,sub,box)
});
// questionAnswered is the read-only view: each item with the stored answer,
// or the options while it is still pending.
function questionAnswered(q){
  const out=[];
  q.questions.forEach((item,i)=>{
    const f=node('div','','qitem');
    const head=node('div','','qhead');head.append(node('span',item.header,'pill'));
    f.append(head,node('p',item.question,'qtext'));
    const a=q.answers&&q.answers[i];
    if(a){
      const sel=(a.selected||[]).join(', ');
      f.append(node('p','답: '+(sel||'(선택 없음)'),'qans'));
      if(a.other)f.append(node('p','기타: '+a.other,'qans'));
    }else if(q.status==='pending'){
      for(const o of item.options)f.append(node('p','· '+o.label+(o.description?' — '+o.description:''),'qans muted'));
    }
    out.push(f);
  });
  if(q.status==='answered')out.push(node('p','답한 쪽: '+byLabel(q.answered_by)+' · '+when(q.answered_at),'muted'));
  if(q.status==='withdrawn')out.push(node('p','거둔 때: '+when(q.withdrawn_at),'muted'));
  return out;
}
function answerForm(q){
  const form=node('form','','qform');
  const n=++formSeq;
  const items=q.questions.map((item,i)=>{
    const f=node('fieldset','','qitem');
    const lg=node('legend','');lg.append(node('span',item.header,'pill'),document.createTextNode(item.multiSelect?' 여러 개 고름':' 하나 고름'));
    f.append(lg,node('p',item.question,'qtext'));
    const inputs=[];
    for(const o of item.options){
      const lab=node('label','','qopt');
      const inp=document.createElement('input');
      inp.type=item.multiSelect?'checkbox':'radio';inp.name='q'+n+'-'+i;inp.value=o.label;
      inputs.push(inp);
      const t=node('span','');t.append(node('strong',o.label));
      if(o.description)t.append(document.createTextNode(' — '+o.description));
      if(o.preview)t.append(node('pre',o.preview));
      lab.append(inp,t);f.append(lab);
    }
    const ol=node('label','','qother');
    const other=document.createElement('input');other.type='text';other.maxLength=1000;other.placeholder='고른 것 대신, 또는 덧붙여 쓸 말';other.title='목록에 없는 답을 글로 적는다';
    ol.append(node('span','기타'),other);f.append(ol);
    form.append(f);
    return {item,inputs,other};
  });
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const send=node('button','답 보내기');send.type='submit';send.title='고른 답을 묻는 쪽에 보낸다';
  const row=node('div','','row');row.append(send);
  form.append(row,msg);
  form.onsubmit=async e=>{
    e.preventDefault();
    const answers=items.map(x=>{
      const a={selected:x.inputs.filter(i=>i.checked).map(i=>i.value)};
      const o=x.other.value.trim();if(o)a.other=o;
      return a;
    });
    const missing=items.findIndex((x,i)=>!answers[i].selected.length&&!answers[i].other);
    if(missing>=0){setMsg(msg,'「'+items[missing].item.header+'」에 고르거나 기타를 쓰세요.','err');return}
    send.disabled=true;setMsg(msg,'보내는 중…');
    try{
      await apiSend('POST','questions/'+encodeURIComponent(q.id)+'/answer',{answers:answers});
      setMsg($('questionsMsg'),'답을 보냈습니다: '+q.id,'ok');
    }catch(err){
      send.disabled=false;
      if(err.status!==409){setMsg(msg,err.message,'err');return}
      qNotes.set(q.id,'이미 답했거나 거둔 질문입니다(409)');
    }
    qForms.delete(q.id);
    // The answer form had focus: move it to the title before the redraw.
    $('detailTitle').focus({preventScroll:true});
    drawDetail();
    if(curTab==='questions')loadQuestions();
  };
  return form;
}
tabLoaders.questions=loadQuestions;
