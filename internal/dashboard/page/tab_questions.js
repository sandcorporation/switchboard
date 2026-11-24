// ---- 답이 필요함 (대기 · 결정) ----
// The list column of the list screens (#514) with two views on the same chips
// (#581): 대기 — the waiting questions, permission requests and rule
// proposals in one group, then the answered questions and the closed
// requests — and 결정, the answered questions by day. A row shows its item
// in the body: #questions/<id> a question, p:<id> a permission request, r:<id>
// a rule proposal, d:<id> a decision. 대기 holds the answer form while the
// question is pending and the admin may answer; a form is kept per question
// across live redraws so a half-filled answer survives them.
const qStatusLabel={pending:'대기',answered:'답함',withdrawn:'거둠'};
const qTone={pending:'warning',answered:'success',withdrawn:'neutral'};
// The class of an answer in the rules ledger (#409): the source of a rule,
// once, or not yet classified.
const decClassLabel={rule:'결정',once:'한 번','':'미분류'};
const decClassTone={rule:'success',once:'neutral','':'warning'};
let formSeq=0,needsData=null,needsSeq=0,questionView='questions',decData=null;
const qForms=new Map(),qNotes=new Map();
function setMsg(el,text,kind){el.textContent=text;el.className='msg'+(kind?' '+kind:'')}
function qTitle(q){return q.questions.map(x=>x.header).join(' · ')}
// itemsHead names a question or decision by its first header (외 N more).
function itemsHead(items){return (items[0]?items[0].header:'')+(items.length>1?' 외 '+(items.length-1):'')}
function clock(t){return new Date(t).toLocaleTimeString('ko-KR',{hour:'2-digit',minute:'2-digit'})}
function chosenText(it){
  let c=(it.chosen||[]).join(', ');
  if(it.other)c+=(c?' / ':'')+'기타: '+it.other;
  return c||'(선택 없음)';
}
async function refreshQuestionCount(){
  try{setTabCount('questions',(await api('questions?status=pending&limit=500')).questions.length)}catch(e){}
}
async function loadQuestions(){
  const seq=++needsSeq;
  $('questionsNote').textContent='불러오는 중…';
  try{
    const [qs,open,closed,rules,dec]=await Promise.all([api('questions?limit=200').then(x=>x.questions),
      api('permissions?status=pending').then(x=>x.permissions),api('permissions?status=closed&limit=200').then(x=>x.permissions),
      api('rules?proposed=1').then(x=>x.rules||[]).catch(()=>[]),loadDecisionData().catch(e=>({err:e.message}))]);
    if(seq!==needsSeq)return;
    needsData={qs:qs,open:open,closed:closed,rules:rules.filter(r=>r.status==='proposed')};
    decData=dec;
    const n=qs.filter(q=>q.status==='pending').length;
    setTabCount('questions',n);setTabCount('permissions',open.length||null);
    drawNeeds();
    // A pending request turns expired by time alone: reload at the next expiry.
    clearTimeout(permTimer);
    const next=Math.min(...open.map(v=>new Date(v.expires_at).getTime()));
    if(isFinite(next))permTimer=setTimeout(()=>{if(tabShown('questions'))loadQuestions()},Math.max(1000,next-Date.now()+500));
  }catch(e){if(seq===needsSeq)$('questionsNote').textContent=e.message}
}
// showQuestionView switches the chips: 대기 (questions) or 결정 (decisions).
// Both are the same list column; the chips stay where they are.
function showQuestionView(name){
  const dec=name==='decisions';questionView=name;
  $('viewQuestions').setAttribute('aria-pressed',dec?'false':'true');
  $('viewDecisions').setAttribute('aria-pressed',dec?'true':'false');
  $('questions').hidden=dec;$('decisions').hidden=!dec;$('decFilters').hidden=!dec;
  drawNeeds();
}
function drawNeeds(){
  drawWaitList();drawDecisionList();
  const d=needsData,w=d?d.qs.filter(q=>q.status==='pending').length+d.open.length+d.rules.length:null;
  $('needWaitN').textContent=w===null?'':String(w);
  $('needDecN').textContent=decData&&decData.list?String(decData.list.length):'';
}
function drawWaitList(){
  const d=needsData;if(!d)return;
  const f=$('needsFilter').value,out=[];
  const qm=q=>listMatch(f,q.asker,qTitle(q),q.id,...q.questions.map(x=>x.question));
  const pm=v=>listMatch(f,v.requester,v.tool,v.summary,v.id,'권한 요청');
  const rm=r=>listMatch(f,r.author,r.topic,r.text,'#'+r.id,'규칙 제안');
  const wait=[...d.qs.filter(q=>q.status==='pending'&&qm(q)).map(q=>[q.created_at,questionRow(q)]),
    ...d.open.filter(pm).map(v=>[v.created_at,needPermRow(v)]),...d.rules.filter(rm).map(r=>[r.created_at,ruleNeedRow(r)])]
    .sort((a,b)=>new Date(b[0])-new Date(a[0])).map(x=>x[1]);
  const qd=d.qs.filter(q=>q.status!=='pending'&&qm(q)),pd=d.closed.filter(pm);
  const g=(k,label,rows,o)=>listGroup('need:'+k,label,rows,Object.assign({redraw:drawNeeds},o));
  const head=g('w','기다림',wait,{hover:'답을 기다리는 질문·권한 요청·규칙 제안'});
  out.push(...head);
  if(!wait.length&&head.length===1&&head[0].getAttribute('aria-expanded')==='true')out.push(node('p',f?'맞는 것 없음':'기다리는 질문·권한 요청·규칙 제안 없음','empty ls-empty'));
  if(qd.length)out.push(...g('qd','지난 질문',qd.map(questionRow),{hover:'답했거나 거둔 질문'}));
  if(pd.length)out.push(...g('pd','지난 권한 요청',pd.map(needPermRow),{fold:true,hover:'허용·거절·만료된 권한 요청'}));
  listFill($('questions'),out,'');
  if(questionView==='questions'){
    $('questionsNote').textContent=String(wait.length);
    $('questionsNote').title='기다림 '+wait.length+' · 지난 질문 '+qd.length+' · 지난 권한 요청 '+pd.length;
  }
}
function questionRow(q){
  const marks=[q.answerer==='owner'?'대표':'',q.delivery==='waiting'?'전달 대기':''].filter(Boolean).join(' · ');
  const at=q.answered_at||q.withdrawn_at||q.created_at;
  return listRow('questions',q.id,{top:itemsHead(q.questions)+' · '+q.asker+(marks?' · '+marks:''),right:q.status==='pending'?ago(new Date(),q.created_at):shortWhen(at),
    rightHover:(qStatusLabel[q.status]||q.status)+' — '+when(at),title:q.questions[0]?q.questions[0].question:q.id,hover:q.questions.map(x=>x.question).join(' / '),cls:q.status==='pending'?'un':'done'});
}
function needPermRow(v){
  return listRow('questions','p:'+v.id,{top:'권한 요청 · '+v.requester,right:v.status==='pending'?ago(new Date(),v.created_at):permStatusLabel[v.status]||v.status,rightHover:when(v.created_at),
    title:v.tool+(v.summary?': '+v.summary:''),hover:v.tool+(v.summary?': '+v.summary:'')+' — '+v.requester,cls:v.status==='pending'?'un':'done'});
}
function ruleNeedRow(r){
  return listRow('questions','r:'+r.id,{top:'규칙 제안 · #'+r.id+' · '+(r.author||'-'),right:ago(new Date(),r.created_at),rightHover:when(r.created_at),
    title:(ruleKinds[r.kind]||r.kind)+' · '+r.topic+' — '+ruleFirstLine(r.text),hover:r.text,cls:'un'});
}
// ---- 결정 (#50) ----
// The answered questions as decisions, with their class from the rules
// ledger (GET rules/classes). The period picks what the server sends; 분류
// and 거르기 sort it here. The same conditions build 「JSON 내보내기」
// (format=ndjson, a same-origin GET the browser downloads).
function decisionQuery(){
  const p=new URLSearchParams(),period=$('decPeriod').value;
  if(period==='custom'){
    if($('decFrom').value)p.set('from',$('decFrom').value);
    if($('decTo').value)p.set('to',$('decTo').value);
  }else if(period!=='all'){
    p.set('from',new Date(Date.now()-Number(period)*864e5).toISOString().replace(/\.\d+Z$/,'Z'));
  }
  return p;
}
function decisionExport(){
  const p=decisionQuery(),q=$('needsFilter').value.trim();
  if(q)p.set('q',q);
  p.set('format','ndjson');
  return '/api/admin/decisions?'+p.toString();
}
async function loadDecisionData(){
  const [list,cls]=await Promise.all([api('decisions?'+decisionQuery().toString()).then(x=>x.decisions),api('rules/classes').then(x=>x.answers||[]).catch(()=>[])]);
  return {list:list,classes:new Map(cls.map(a=>[a.id,a]))};
}
async function reloadDecisions(){
  try{decData=await loadDecisionData()}catch(e){decData={err:e.message}}
  drawNeeds();
}
// decClass is a decision's class: rule, once or '' (unclassified); null for
// an answer the ledger does not count (a boss role's, or older than it).
function decClass(id){const a=decData&&decData.classes&&decData.classes.get(id);return a?a.class||'':null}
function drawDecisionList(){
  const d=decData;if(!d)return;
  const sel=$('decClass'),counts={rule:0,once:0,none:0};
  if(d.list)for(const x of d.list){const c=decClass(x.id);if(c==='rule')counts.rule++;else if(c==='once')counts.once++;else if(c==='')counts.none++}
  for(const o of sel.options)if(o.value)o.textContent=(o.value==='none'?'미분류':decClassLabel[o.value])+' '+counts[o.value];
  if(d.err){listFill($('decisions'),[],d.err);if(questionView==='decisions')$('questionsNote').textContent='';return}
  const f=$('needsFilter').value,want=sel.value;
  const shown=d.list.filter(x=>{
    const c=decClass(x.id);
    if(want&&(want==='none'?c!=='':c!==want))return false;
    return listMatch(f,x.asker,x.id,...x.items.flatMap(it=>[it.header,it.question,chosenText(it)]));
  });
  const groups=new Map();
  for(const x of shown){const k=new Date(x.answered_at).toLocaleDateString('ko-KR',{month:'numeric',day:'numeric'});if(!groups.has(k))groups.set(k,[]);groups.get(k).push(x)}
  const out=[];let i=0;
  for(const [k,rows] of groups)out.push(...listGroup('dec:'+k,k,rows.map(decisionRow),{redraw:drawNeeds,fold:i++>0,hover:k+' 결정'}));
  listFill($('decisions'),out,f||want?'맞는 결정 없음':'결정 없음');
  if(questionView==='decisions'){
    $('questionsNote').textContent=(shown.length===d.list.length?'':shown.length+' / ')+d.list.length;
    $('questionsNote').title='결정 '+d.list.length+'개'+(d.list.length>=1000?' (최대 1000개)':'');
  }
}
function decisionRow(x){
  const c=decClass(x.id);
  return listRow('questions','d:'+x.id,{top:itemsHead(x.items)+' · '+x.asker,right:clock(x.answered_at),rightHover:when(x.answered_at)+(c!==null?' · '+decClassLabel[c]:''),
    title:'→ '+(x.items[0]?chosenText(x.items[0]):''),hover:x.items.map(it=>it.header+' → '+chosenText(it)).join(' / '),cls:c===''?'un':''});
}
$('decPeriod').onchange=()=>{const c=$('decPeriod').value==='custom';$('decFrom').hidden=!c;$('decTo').hidden=!c;reloadDecisions()};
for(const id of ['decFrom','decTo'])$(id).addEventListener('change',reloadDecisions);
$('decClass').onchange=drawNeeds;
$('needsFilter').addEventListener('input',drawNeeds);
$('viewQuestions').onclick=()=>selectTab('questions');
$('viewDecisions').onclick=()=>selectTab('decisions');
// ---- the body: a question or a decision ----
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
// A decision older than the question list holds is drawn from the decision
// alone: its items carry the chosen labels but not the other options.
function questionOfDecision(x){
  return {id:x.id,asker:x.asker,created_at:x.asked_at,status:'answered',answered_at:x.answered_at,answered_by:x.answered_by,
    questions:x.items.map(it=>({header:it.header,question:it.question,options:(it.chosen||[]).map(l=>({label:l}))})),
    answers:x.items.map(it=>({selected:it.chosen||[],other:it.other||''}))};
}
// questionLoad gathers a question's body: the question, its class in the
// ledger, the rule it became and the request it was asked under.
async function questionLoad(id,dec){
  let q=await findQuestion(id);
  if(!q&&dec){
    if(!decData||!decData.list)await reloadDecisions();
    const x=decData&&decData.list&&decData.list.find(d=>d.id===id);
    if(x)q=questionOfDecision(x);
  }
  if(!q)return null;
  let cls=null,req=null,rule=null;
  const [c,r]=await Promise.all([api('rules/classes').then(x=>(x.answers||[]).find(a=>a.id===id)||null).catch(()=>null),
    q.request_id?api('requests/'+q.request_id).catch(()=>null):null]);
  cls=c;req=r;
  if(cls&&cls.rule)rule=await api('rules/'+cls.rule).catch(()=>null);
  return {q:q,dec:!!dec,cls:cls,req:req,rule:rule};
}
function questionHead(x){
  const q=x.q,c=x.cls?x.cls.class||'':null;
  const at=q.status==='answered'?[shortWhen(q.answered_at)+' 답함','답한 때 — '+when(q.answered_at)]:q.status==='withdrawn'?[shortWhen(q.withdrawn_at)+' 거둠','거둔 때 — '+when(q.withdrawn_at)]:[ago(new Date(),q.created_at),'올라온 때 — '+when(q.created_at)];
  const meta=[at];
  if(q.answered_by)meta.push([byLabel(q.answered_by),'답한 쪽']);
  if(q.request_id)meta.push(['지시 #'+q.request_id,'이 질문이 딸린 지시']);
  const title=x.dec?itemsHead(q.questions)+' → '+(q.answers&&q.answers[0]?chosenText({chosen:q.answers[0].selected,other:q.answers[0].other}):''):(q.questions[0]?q.questions[0].question:q.id)+(q.questions.length>1?' 외 '+(q.questions.length-1):'');
  const badge=x.dec?[c===null?'분류 없음':decClassLabel[c],c===null?'neutral':decClassTone[c]]:[qStatusLabel[q.status]||q.status,qTone[q.status]||'neutral'];
  return {title:title,badge:badge,crumb:{path:['답이 필요함',x.dec?'결정':'질문',q.asker],meta:meta}};
}
// questionItems draws each item: the question, then its options with the
// chosen ones marked (고름).
function questionItems(q,box){
  q.questions.forEach((item,i)=>{
    docHead(box,'질문'+(q.questions.length>1?' '+(i+1)+' · '+item.header:''));
    const p=node('p',item.question,'dc-quote');p.title=item.header;box.append(p);
    const a=q.answers&&q.answers[i],sel=new Set(a?a.selected||[]:[]);
    const rows=item.options.map(o=>({cells:[sel.has(o.label)?node('span','고름','ok'):'—',o.label,o.description||''],cls:sel.has(o.label)?'on':''}));
    if(a&&a.other)rows.push({cells:[node('span','고름','ok'),'기타',a.other]});
    docHead(box,'선택지 '+item.options.length);
    box.append(docTable(['고름','선택지','설명'],rows,'선택지 없음'));
  });
}
// questionLinks is the 이어진 것 table: the request, the question or the
// decision on the other view, and the rule.
function questionLinks(x){
  const q=x.q,rows=[],c=x.cls?x.cls.class||'':null;
  if(q.request_id){
    const r=x.req,t='#'+q.request_id+(r?' '+r.title:'');
    rows.push({cells:['지시',docLink(t,detailHash('requests',String(q.request_id))),r?requestLabel(r.status)[1]:'—'],href:detailHash('requests',String(q.request_id))});
  }else rows.push({cells:['지시','없음','—']});
  if(x.dec)rows.push({cells:['질문',docLink(q.questions[0]?q.questions[0].question:q.id,detailHash('questions',q.id)),qStatusLabel[q.status]||q.status],href:detailHash('questions',q.id)});
  else if(q.status==='answered'){
    const t=itemsHead(q.questions)+' → '+(q.answers&&q.answers[0]?chosenText({chosen:q.answers[0].selected,other:q.answers[0].other}):'');
    rows.push({cells:['결정',docLink(t,detailHash('questions','d:'+q.id)),c===null?'—':decClassLabel[c]],href:detailHash('questions','d:'+q.id)});
  }
  if(x.rule)rows.push({cells:['규칙',docLink('#'+x.rule.id+' '+ruleFirstLine(x.rule.text),detailHash('rules',String(x.rule.id))),ruleBadge(x.rule.status)[0]],href:detailHash('rules',String(x.rule.id))});
  else rows.push({cells:['규칙','없음','—']});
  return rows;
}
// questionBar is the body's foot: 규칙으로 올리기 on an answered question;
// on a decision also JSON 내보내기 and 한 번으로 바꿈.
function questionBar(x){
  const q=x.q,bar=docBar(),msg=node('span','','msg');msg.setAttribute('role','status');
  bar.append(msg,node('span','','dc-sp'));
  if(x.dec){
    const ex=node('a','JSON 내보내기','button');ex.href=decisionExport();ex.download='decisions.ndjson';ex.title='지금 기간·거르기의 결정을 NDJSON 으로 내려받는다';
    bar.append(ex);
    if(x.cls&&x.cls.class!=='once'){
      const once=node('button','한 번으로 바꿈');once.type='button';once.title='이 답을 이번 일에만 쓴 것으로 분류한다';
      once.onclick=async()=>{
        once.disabled=true;setMsg(msg,'바꾸는 중…');
        try{await apiSend('POST','rules/classify',{question:q.id,once:true});setMsg(msg,'한 번으로 바꿈','ok');reloadDecisions();drawDetail()}
        catch(e){once.disabled=false;setMsg(msg,e.message,'err')}
      };
      bar.append(once);
    }
  }
  if(q.status==='answered'&&!x.rule){
    const up=node('a','규칙으로 올리기','button'+(x.dec?' btn-p':''));up.href=detailHash('rules','new:'+q.id);up.title='이 답을 출처로 새 규칙을 올린다';
    bar.append(up);
  }
  return bar;
}
const questionSpec={
  list:'답이 필요함',kinds:['questions'],
  load:id=>questionLoad(id,false),
  head:questionHead,
  subs:()=>[['answer','답']],
  draw:async(x,sub,box)=>{
    const q=x.q;
    const marks=node('span','');questionPills(q,marks);
    if(marks.childNodes.length)box.append(marks);
    const note=qNotes.get(q.id);
    if(note)box.append(node('p',note,'msg err'));
    if(q.status==='pending'&&q.can_answer){
      let f=qForms.get(q.id);
      if(!f){f=answerForm(q);qForms.set(q.id,f)}
      box.append(f);
    }else{qForms.delete(q.id);questionItems(q,box)}
    if(q.relay_to&&q.relay_to.length){docHead(box,'중계');box.append(node('p',q.relay_to.join(', '),'dc-p'))}
    docHead(box,'이어진 것');
    box.append(docTable(['갈래','제목','상태'],questionLinks(x)));
    box.append(questionBar(x));
  }
};
const decisionSpec=Object.assign({},questionSpec,{load:id=>questionLoad(id,true),
  draw:async(x,sub,box)=>{
    questionItems(x.q,box);
    docHead(box,'이어진 것');
    box.append(docTable(['갈래','제목','상태'],questionLinks(x)));
    box.append(questionBar(x));
  }});
// needItem splits a 답이 필요함 id: p:<id> a permission request, r:<id> a
// rule, d:<id> a decision, else a question.
function needItem(id){
  if(id.startsWith('p:'))return ['permissions',id.slice(2)];
  if(id.startsWith('r:'))return ['rules',id.slice(2)];
  if(id.startsWith('d:'))return ['d',id.slice(2)];
  return ['',id];
}
function needSpec(kind){return kind==='d'?decisionSpec:kind?details[kind]:questionSpec}
registerDetail('questions',{
  list:'답이 필요함',kinds:['questions','permissions','rules'],
  load:async id=>{
    const [k,raw]=needItem(id),x=await needSpec(k).load(raw);
    return x?{k:k,x:x}:null;
  },
  head:w=>{
    const h=needSpec(w.k).head(w.x);
    if(w.k==='permissions')h.crumb={path:['답이 필요함','권한 요청',w.x.requester],meta:[[ago(new Date(),w.x.created_at),'요청한 때 — '+when(w.x.created_at)],['만료 '+shortWhen(w.x.expires_at),'답이 없으면 이때 만료 — '+when(w.x.expires_at)]]};
    if(w.k==='rules')h.crumb={path:['답이 필요함','규칙 제안',w.x.topic],meta:[[shortWhen(w.x.created_at)+' 올라옴','올라온 때 — '+when(w.x.created_at)],[w.x.author||'-','적은 이']]};
    return h;
  },
  subs:w=>needSpec(w.k).subs(w.x),
  draw:(w,sub,box)=>needSpec(w.k).draw(w.x,sub,box)
});
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
