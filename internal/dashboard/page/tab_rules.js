// ---- 규칙 (#406 ①) ----
// The rules ledger: rows in force by service and topic, 결정 first. A row's
// detail has its history and the answered questions it came from; an admin
// fixes a row (a new row replacing it) or voids it. A 409 lists the rows in
// force on the same scope and topic: replace them or stand alongside.
const ruleCols=['#','종류','주제','범위','글','걸림'];
const ruleKinds={decision:'결정',tip:'요령'};
const ruleStates={active:['유효','success'],replaced:['대체됨','neutral'],void:['무효','danger'],proposed:['제안','warning'],rejected:['거절됨','danger']};
const ruleChips=[['decision','결정'],['tip','요령'],['','전체']];
let rulesData=null,rulesKind='decision',rulesSeq=0,rulesProposed=false;
function ruleScopeText(s){return !s?'':s.kind==='all'||!s.values||!s.values.length?s.kind:s.kind+':'+s.values.join(',')}
function ruleFirstLine(t){return String(t||'').split('\n')[0]}
function ruleMatchText(m){
  if(!m)return '';
  const p=[];
  if(m.tool)p.push('도구 '+m.tool);
  if(m.command)p.push('명령 /'+m.command+'/');
  if(m.path)p.push('경로 '+m.path);
  return p.join(' · ');
}
// ruleMatchBody reads the three condition fields; empty ones are left out.
function ruleMatchBody(tool,command,path){
  const m={};
  if(tool.trim())m.tool=tool.trim();
  if(command)m.command=command;
  if(path.trim())m.path=path.trim();
  return Object.keys(m).length?m:null;
}
function ruleMatchInputs(m){
  const mk=(label,ph,v)=>{const i=node('input','');i.placeholder=ph;i.value=v||'';i.setAttribute('aria-label',label);i.autocomplete='off';i.spellcheck=false;return i};
  m=m||{};
  return [mk('걸림: 도구','도구 (Bash, mcp__*)',m.tool),mk('걸림: 명령 정규식','명령 정규식',m.command),mk('걸림: 경로 glob','경로 glob (**/x/*.sql)',m.path)];
}
function parseRuleScope(v){
  const i=v.indexOf(':'),kind=(i<0?v:v.slice(0,i)).trim();
  return {kind:kind,values:i<0?[]:v.slice(i+1).split(',').map(x=>x.trim()).filter(Boolean)};
}
// ruleSourceBody reads a source field: answered question IDs (q_…, comma
// separated) or a text.
function ruleSourceBody(v){
  const ids=v.split(',').map(x=>x.trim()).filter(Boolean);
  return ids.length&&ids.every(x=>/^q_\S+$/.test(x))?{sources:ids}:{source_text:v.trim()};
}
async function loadRules(){
  const seq=++rulesSeq,q=new URLSearchParams(),svc=$('ruleService').value.trim(),topic=$('ruleTopic').value;
  if(svc)q.set('service',svc);
  if(topic)q.set('topic',topic);
  if($('ruleAll').checked)q.set('all','1');
  if(rulesProposed)q.set('proposed','1');
  $('rulesNote').textContent='불러오는 중…';
  try{
    const [list,topics,uncl]=await Promise.all([api('rules?'+q.toString()),api('rules/topics'),api('rules/unclassified')]);
    if(seq!==rulesSeq)return;
    rulesData=list.rules;
    fillRuleTopics(topics.topics);
    drawRuleCounts(list.proposed||0,uncl);
    drawRules();
  }catch(e){if(seq===rulesSeq)$('rulesNote').textContent=e.message}
}
// drawRuleCounts shows the proposals waiting for approval (a toggle that
// lists them) and the answers to panel questions still unclassified, per
// role in the title.
function drawRuleCounts(proposed,uncl){
  $('ruleProposedN').textContent=String(proposed);
  $('ruleProposed').setAttribute('aria-pressed',rulesProposed?'true':'false');
  const u=$('ruleUnclassified');
  u.textContent='미분류 '+(uncl.count||0);
  u.title=(uncl.roles||[]).map(r=>r.role+' '+r.count).join('\n');
}
function fillRuleTopics(topics){
  const sel=$('ruleTopic'),cur=sel.value,dl=$('ruleTopics');
  sel.replaceChildren(new Option('모든 주제',''));dl.replaceChildren();
  for(const t of topics){sel.append(new Option(t.name+' · '+t.active,t.name));dl.append(new Option(t.name,t.name))}
  if(cur&&!topics.some(t=>t.name===cur))sel.append(new Option(cur,cur));
  sel.value=cur;
}
function drawRules(){
  const list=rulesData;if(!list)return;
  const chips=$('ruleChips');chips.replaceChildren();
  for(const [k,label] of ruleChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===rulesKind?'true':'false');
    b.append(node('span',String(list.filter(r=>!k||r.kind===k).length),'sc-chip-count'));
    b.onclick=()=>{rulesKind=k;drawRules()};
    chips.append(b);
  }
  const shown=list.filter(r=>!rulesKind||r.kind===rulesKind),body=$('rules');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const r of shown)body.append(ruleRow(r));
    if(!shown.length)emptyRow(body,ruleCols.length,'규칙 없음');
  });
  $('rulesNote').textContent=(shown.length===list.length?'':shown.length+' / ')+list.length+'줄';
}
function ruleBadge(st){return ruleStates[st]||[st,'neutral']}
function ruleRow(r){
  const off=r.status&&r.status!=='active',tr=node('tr','',off?'done':'');
  const t=cell('',ruleCols[4],'wrap');t.append(node('span',ruleFirstLine(r.text)));
  if(off){const [l,tone]=ruleBadge(r.status);t.append(' ',badge(l,tone))}
  const mt=ruleMatchText(r.match);if(mt)t.append(' ',node('span',mt,'muted'));
  const h=cell(String(r.hits||0),ruleCols[5],'num');if(r.last_hit_at)h.title='마지막 '+when(r.last_hit_at);
  tr.append(cell('#'+r.id,ruleCols[0],'num'),cell(ruleKinds[r.kind]||r.kind,ruleCols[1],'nowrap'),cell(r.topic,ruleCols[2]),cell(ruleScopeText(r.scope)+(r.group?' @'+r.group:''),ruleCols[3]),t,h);
  detailRow(tr,'rules',r.id);
  return tr;
}
// postRule adds a row; on a 409 it shows the rows in force and offers to
// replace them or (for a new row) to stand alongside them.
async function postRule(body,msg,done){
  msg.className='msg';msg.replaceChildren();
  try{done(await apiSend('POST','rules',body));return}
  catch(e){
    let c=null;
    if(e.status===409){try{c=JSON.parse(e.body)}catch(x){}}
    if(!c||!c.rules){msg.className='msg err';msg.textContent=e.message;return}
    msg.append(node('p','같은 범위·주제에 유효한 줄이 있다'));
    const ul=node('ul','','plain');
    for(const r of c.rules)ul.append(node('li','#'+r.id+' '+(ruleKinds[r.kind]||r.kind)+' '+ruleFirstLine(r.text)));
    const row=node('div','','row'),rep=node('button','대체');rep.type='button';
    rep.onclick=()=>postRule(Object.assign({},body,{replace:[...(body.replace||[]),...c.rules.map(r=>r.id)]}),msg,done);
    row.append(rep);
    if(!(body.replace&&body.replace.length)){
      const side=node('button','나란히');side.type='button';
      side.onclick=()=>postRule(Object.assign({},body,{alongside:true}),msg,done);
      row.append(side);
    }
    msg.append(ul,row);
  }
}
function ruleEditForm(r){
  const f=node('form','','ruleform');
  const text=node('textarea','');text.value=r.text;text.rows=4;text.required=true;text.setAttribute('aria-label','새 글');
  const src=node('input','');src.placeholder='출처: 질문 ID(q_…) 또는 글';src.required=true;src.setAttribute('aria-label','출처');
  const [wt,wc,wp]=ruleMatchInputs(r.match),when3=node('div','','row');when3.append(wt,wc,wp);
  const save=node('button','고쳐 올리기');save.type='submit';
  const off=node('button','무효로');off.type='button';
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const row=node('div','','row');row.append(save,off);
  f.append(node('h3','고치기','sechead'),text,when3,src,row,msg);
  f.onsubmit=e=>{
    e.preventDefault();
    postRule(Object.assign(ruleSourceBody(src.value),{kind:r.kind,text:text.value,topic:r.topic,scope:r.scope,group:r.group||'',device:r.device||'',match:ruleMatchBody(wt.value,wc.value,wp.value),replace:[r.id],alongside:true}),msg,n=>{loadRules();openDetail('rules',String(n.id))});
  };
  off.onclick=async()=>{
    if(!confirm('#'+r.id+' 줄을 무효로 돌린다. 되돌릴 수 없다.'))return;
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/void');loadRules();drawDetail()}
    catch(e){msg.className='msg err';msg.textContent=e.message}
  };
  return f;
}
registerDetail('rules',{
  list:'규칙',kinds:['rules'],
  load:async id=>{
    try{return await api('rules/'+encodeURIComponent(id))}
    catch(e){if(e.status===404)return null;throw e}
  },
  head:r=>({title:'#'+r.id+' '+(ruleKinds[r.kind]||r.kind)+' · '+r.topic,badge:ruleBadge(r.status)}),
  subs:()=>[['body','규칙']],
  draw:async(r,sub,box)=>{
    box.append(node('p',r.text,'rbody'));
    const pairs=[['범위',ruleScopeText(r.scope)],['그룹',r.group||'(그룹 없음)']];
    if(r.sources&&r.sources.length)pairs.push(['출처',r.sources.join(', ')]);
    if(r.source_text)pairs.push(['출처 글',r.source_text]);
    if(r.device)pairs.push(['장치',r.device]);
    if(r.match)pairs.push(['걸림 조건',ruleMatchText(r.match)]);
    if(r.hits)pairs.push(['걸린 수',r.hits+(r.last_hit_at?' · 마지막 '+when(r.last_hit_at):'')]);
    pairs.push(['적은 이',r.author||'-'],['시각',when(r.created_at)]);
    if(r.voided_by)pairs.push(['무효로 돌린 이',r.voided_by+' · '+when(r.voided_at)]);
    if(r.reviewed_by)pairs.push([r.status==='rejected'?'거절한 이':'승인한 이',r.reviewed_by+' · '+when(r.reviewed_at)]);
    if(r.review_note)pairs.push(['거절 까닭',r.review_note]);
    box.append(dl(pairs));
    if(r.history&&r.history.length){
      box.append(node('h3','이력','sechead'));
      const ul=node('ul','','plain');
      for(const h of r.history){
        const li=node('li',''),a=node('a','#'+h.id+' '+ruleFirstLine(h.text));a.href=detailHash('rules',String(h.id));
        const [l,tone]=ruleBadge(h.status);li.append(a,' ',badge(l,tone));ul.append(li);
      }
      box.append(ul);
    }
    if(r.answers&&r.answers.length){
      box.append(node('h3','원문','sechead'));
      for(const a of r.answers){
        box.append(node('div',a.id+' · '+a.asker+' → '+a.answered_by+' · '+when(a.answered_at),'rline'));
        for(const it of a.items||[])box.append(node('p','['+it.header+'] '+it.question+' → '+[...(it.chosen||[]),...(it.other?[it.other]:[])].join(', '),'muted'));
      }
    }
    if(r.status==='active')box.append(ruleEditForm(r));
    if(r.status==='proposed')box.append(ruleReviewForm(r));
  }
});
// ruleReviewForm approves a proposed row (a 409 offers to replace the rows
// in force on its scope and topic or to stand alongside them) or rejects it
// with a reason.
function ruleReviewForm(r){
  const f=node('form','','ruleform');
  const reason=node('input','');reason.placeholder='거절 까닭';reason.setAttribute('aria-label','거절 까닭');reason.autocomplete='off';
  const ok=node('button','승인');ok.type='button';ok.title='제안을 유효한 규칙으로 채택한다';
  const no=node('button','거절');no.type='button';no.title='적은 까닭과 함께 제안을 거절한다';
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const row=node('div','','row');row.append(ok,reason,no);
  f.append(node('h3','제안','sechead'),row,msg);
  const done=()=>{loadRules();drawDetail();if(curTab==='questions')loadQuestions()};
  const approve=async body=>{
    msg.className='msg';msg.replaceChildren();
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/approve',body);done();return}
    catch(e){
      let c=null;
      if(e.status===409){try{c=JSON.parse(e.body)}catch(x){}}
      if(!c||!c.rules){msg.className='msg err';msg.textContent=e.message;return}
      msg.append(node('p','같은 범위·주제에 유효한 줄이 있다'));
      const ul=node('ul','','plain');
      for(const o of c.rules)ul.append(node('li','#'+o.id+' '+(ruleKinds[o.kind]||o.kind)+' '+ruleFirstLine(o.text)));
      const rep=node('button','대체');rep.type='button';rep.onclick=()=>approve({replace:c.rules.map(o=>o.id)});
      const side=node('button','나란히');side.type='button';side.onclick=()=>approve({alongside:true});
      const btns=node('div','','row');btns.append(rep,side);
      msg.append(ul,btns);
    }
  };
  ok.onclick=()=>approve({});
  no.onclick=async()=>{
    msg.className='msg';msg.replaceChildren();
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/reject',{reason:reason.value});done()}
    catch(e){msg.className='msg err';msg.textContent=e.message}
  };
  return f;
}
// ---- 결정 (#50) ----
// The 결정 view of the 질문 tab lists answered questions as decisions. Its
// filters build one query used both for the list and for the 「JSON 내보내기」
// link (format=ndjson, a same-origin GET the browser downloads).
let decisionsLoaded=false,questionView='questions';
function showQuestionView(name){
  const dec=name==='decisions';questionView=name;
  if(curTab==='questions')frameSplit(!dec);
  $('viewQuestions').setAttribute('aria-pressed',dec?'false':'true');
  $('viewDecisions').setAttribute('aria-pressed',dec?'true':'false');
  $('questionsView').hidden=dec;$('decisionsView').hidden=!dec;
  if(dec&&!decisionsLoaded){decisionsLoaded=true;loadDecisionRoles();loadDecisions()}
}
function decisionQuery(){
  const p=new URLSearchParams(),period=$('decPeriod').value;
  if(period==='custom'){
    if($('decFrom').value)p.set('from',$('decFrom').value);
    if($('decTo').value)p.set('to',$('decTo').value);
  }else if(period!=='all'){
    p.set('from',new Date(Date.now()-Number(period)*864e5).toISOString().replace(/\.\d+Z$/,'Z'));
  }
  const role=$('decRole').value.trim(),q=$('decSearch').value.trim();
  if(role)p.set('role',role);
  if(q)p.set('q',q);
  return p;
}
function updateDecisionExport(){
  const p=decisionQuery();p.set('format','ndjson');
  $('decExport').href='/api/admin/decisions?'+p.toString();
}
async function loadDecisionRoles(){
  try{
    const roles=(await api('roles')).map(v=>v.role),seen=new Set(),list=$('decRoles');
    const add=v=>{if(!seen.has(v)){seen.add(v);const o=document.createElement('option');o.value=v;list.append(o)}};
    for(const r of roles){const i=r.lastIndexOf('/');if(i>0)add(r.slice(0,i)+'/*')}
    for(const r of roles)add(r);
  }catch(e){}
}
async function loadDecisions(){
  updateDecisionExport();
  $('decisionsNote').textContent='불러오는 중…';
  try{
    const list=(await api('decisions?'+decisionQuery().toString())).decisions;
    const box=$('decisions');box.replaceChildren();
    for(const d of list)box.append(decisionRow(d));
    $('decisionsNote').textContent='결정 '+list.length+'개'+(list.length>=1000?'(최대 1000개)':'');
    if(!list.length)box.append(node('p','조건에 맞는 결정 없음','empty'));
  }catch(e){$('decisionsNote').textContent=e.message}
}
function decisionRow(d){
  const row=node('div','','dec');
  row.append(node('p',d.asker+' · '+when(d.answered_at)+' · '+d.id+' · 답한 쪽 '+byLabel(d.answered_by),'muted'));
  for(const it of d.items){
    const p=node('p','');
    let chosen=(it.chosen||[]).join(', ');
    if(it.other)chosen+=(chosen?' / ':'')+'기타: '+it.other;
    p.title=it.question;
    p.append(node('span',it.header,'pill'),document.createTextNode('→ '+(chosen||'(선택 없음)')));
    row.append(p);
  }
  return row;
}
$('decPeriod').onchange=()=>{const c=$('decPeriod').value==='custom';$('decFrom').hidden=!c;$('decTo').hidden=!c;loadDecisions()};
$('decForm').onsubmit=e=>{e.preventDefault();loadDecisions()};
for(const id of ['decFrom','decTo','decRole','decSearch'])$(id).addEventListener('input',updateDecisionExport);
$('viewQuestions').onclick=()=>selectTab('questions');
$('viewDecisions').onclick=()=>selectTab('decisions');
