// ---- 질문 ----
// The list is one row per question; a row opens the detail (#questions/<id>/
// answer|decisions). 답하기 holds the answer form while the question is
// pending and the admin may answer, else the stored answer. A form is kept
// per question across live redraws so a half-filled answer survives them.
const qStatusLabel={pending:'대기',answered:'답함',withdrawn:'거둠'};
const qTone={pending:'warning',answered:'success',withdrawn:'neutral'};
const questionCols=['상태','질문','묻는 쪽','시각'];
let formSeq=0;
const qForms=new Map(),qNotes=new Map();
function setMsg(el,text,kind){el.textContent=text;el.className='msg'+(kind?' '+kind:'')}
function qTitle(q){return q.questions.map(x=>x.header).join(' · ')}
async function refreshQuestionCount(){
  try{setTabCount('questions',(await api('questions?status=pending&limit=500')).questions.length)}catch(e){}
}
async function loadQuestions(){
  $('questionsNote').textContent='불러오는 중…';
  try{
    const list=(await api('questions?limit=200')).questions,body=$('questions');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const q of list)body.append(questionRow(q));
      if(!list.length)emptyRow(body,questionCols.length,'질문 없음');
    });
    const open=list.filter(q=>q.status==='pending').length;
    setTabCount('questions',open);
    $('questionsNote').textContent='대기 '+open+'개 · 모두 '+list.length+'개(최대 200개)';
  }catch(e){$('questionsNote').textContent=e.message}
}
function questionRow(q){
  const tr=node('tr','',q.status==='pending'?'':'done');
  const st=cell('');st.append(badge(qStatusLabel[q.status]||q.status,qTone[q.status]||'neutral'));
  const t=cell('',questionCols[1]);
  questionPills(q,t);
  t.append(node('strong',qTitle(q)));
  const at=cell(shortWhen(q.created_at),questionCols[3],'nowrap');at.title=when(q.created_at);
  tr.append(st,t,cell(q.asker,questionCols[2]),at);
  detailRow(tr,'questions',q.id);
  return tr;
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
registerDetail('questions',{
  list:'질문',kinds:['questions'],
  load:async id=>{
    const q=await findQuestion(id);
    if(!q)return null;
    let decs=[];
    try{decs=(await api('decisions?'+new URLSearchParams({role:q.asker}))).decisions.filter(d=>d.id!==q.id)}catch(e){}
    return {q:q,decs:decs};
  },
  head:x=>({title:qTitle(x.q),badge:[qStatusLabel[x.q.status]||x.q.status,qTone[x.q.status]||'neutral']}),
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
    const other=document.createElement('input');other.type='text';other.maxLength=1000;other.placeholder='고른 것 대신, 또는 덧붙여 쓸 말';
    ol.append(node('span','기타'),other);f.append(ol);
    form.append(f);
    return {item,inputs,other};
  });
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const send=node('button','답 보내기');send.type='submit';
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
