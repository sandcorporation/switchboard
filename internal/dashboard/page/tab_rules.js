// ---- 규칙 (#406 ①) ----
// The rules ledger, a menu row of its own on the list screens' frame (#581):
// the chips pick 결정·요령·전체, 주제 and 상태 (유효한 것만, 대체·무효 포함,
// 제안) what the server sends, the rows group by state. A row's body has its
// text, the decisions it came from, the rows it replaced and its hit log; an
// admin fixes a row (a new row replacing it) or voids it. A 409 lists the
// rows in force on the same scope and topic: replace them or stand alongside.
const ruleKinds={decision:'결정',tip:'요령'};
const ruleStates={active:['유효','success'],replaced:['대체됨','neutral'],void:['무효','danger'],proposed:['제안','warning'],rejected:['거절됨','danger']};
const ruleChips=[['decision','결정'],['tip','요령'],['','전체']];
let rulesData=null,rulesKind='decision',rulesSeq=0,ruleEditing='';
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
  const seq=++rulesSeq,q=new URLSearchParams(),topic=$('ruleTopic').value,state=$('ruleState').value;
  if(topic)q.set('topic',topic);
  if(state==='all')q.set('all','1');
  if(state==='proposed')q.set('proposed','1');
  $('rulesNote').textContent='불러오는 중…';
  try{
    const [list,topics]=await Promise.all([api('rules?'+q.toString()),api('rules/topics')]);
    if(seq!==rulesSeq)return;
    rulesData=list.rules;
    fillRuleTopics(topics.topics);
    for(const o of $('ruleState').options)if(o.value==='proposed')o.textContent='제안 '+(list.proposed||0);
    if(!topic&&!state)setTabCount('rules',rulesData.length||null);else refreshRuleCount();
    drawRules();
  }catch(e){if(seq===rulesSeq)$('rulesNote').textContent=e.message}
}
// The menu row counts the rows in force.
async function refreshRuleCount(){
  try{setTabCount('rules',(await api('rules')).rules.length||null)}catch(e){}
}
function fillRuleTopics(topics){
  const sel=$('ruleTopic'),cur=sel.value,dl=$('ruleTopics');
  sel.replaceChildren(new Option('모든 주제',''));dl.replaceChildren();
  for(const t of topics){sel.append(new Option(t.name+' · '+t.active,t.name));dl.append(new Option(t.name,t.name))}
  if(cur&&!topics.some(t=>t.name===cur))sel.append(new Option(cur,cur));
  sel.value=cur;
}
const ruleOrder=['proposed','active','replaced','void','rejected'];
function drawRules(){
  const list=rulesData;if(!list)return;
  const chips=$('ruleChips');chips.replaceChildren();
  for(const [k,label] of ruleChips){
    const b=node('button',label+' ','ls-chip');b.type='button';b.setAttribute('aria-pressed',k===rulesKind?'true':'false');
    b.title=k?'「'+label+'」 줄만 본다':'모든 줄을 본다';
    b.append(node('span',String(list.filter(r=>!k||r.kind===k).length),'ls-chip-n'));
    b.onclick=()=>{rulesKind=k;drawRules()};
    chips.append(b);
  }
  const f=$('rulesFilter').value;
  const shown=list.filter(r=>(!rulesKind||r.kind===rulesKind)&&listMatch(f,'#'+r.id,r.topic,ruleScopeText(r.scope),r.group,r.text,r.author,ruleMatchText(r.match)));
  const groups=new Map();
  for(const r of shown){const k=r.status||'active';if(!groups.has(k))groups.set(k,[]);groups.get(k).push(r)}
  const rank=k=>{const i=ruleOrder.indexOf(k);return i<0?ruleOrder.length:i};
  const keys=[...groups.keys()].sort((a,b)=>rank(a)-rank(b)),out=[];
  for(const k of keys)out.push(...listGroup('rule:'+k,ruleBadge(k)[0],groups.get(k).map(ruleRow),{redraw:drawRules,fold:k!=='active'&&k!=='proposed'}));
  listFill($('rules'),out,f||rulesKind?'맞는 규칙 없음':'규칙 없음');
  $('rulesNote').textContent=String(list.length);
  $('rulesNote').title='규칙 '+list.length+'줄'+(shown.length===list.length?'':' 중 '+shown.length+'줄 보임');
}
function ruleBadge(st){return ruleStates[st]||[st,'neutral']}
function ruleRow(r){
  const off=r.status&&r.status!=='active';
  return listRow('rules',String(r.id),{top:'#'+r.id+' · '+r.topic+' · '+ruleScopeText(r.scope)+(r.group?' @'+r.group:''),right:'걸림 '+(r.hits||0),
    rightHover:r.last_hit_at?'마지막 걸림 — '+when(r.last_hit_at):'걸린 적 없음',title:ruleFirstLine(r.text),hover:(ruleKinds[r.kind]||r.kind)+' · '+r.text,cls:off?'done':r.status==='proposed'?'un':''});
}
$('rulesFilter').addEventListener('input',drawRules);
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
// ruleEditForm is 고쳐서 대체: a new row with r's kind, topic and scope that
// replaces r.
function ruleEditForm(r){
  const f=node('form','','ruleform');
  const text=node('textarea','');text.value=r.text;text.rows=4;text.required=true;text.setAttribute('aria-label','새 글');text.title='새 글';
  const src=node('input','');src.placeholder='출처: 질문 ID(q_…) 또는 글';src.required=true;src.setAttribute('aria-label','출처');src.title='출처';
  const [wt,wc,wp]=ruleMatchInputs(r.match),when3=node('div','','row');when3.append(wt,wc,wp);
  const save=node('button','고쳐 올리기');save.type='submit';save.title='새 글로 올리고 #'+r.id+' 를 대체한다';
  const cancel=node('button','취소');cancel.type='button';cancel.title='고치지 않는다';
  cancel.onclick=()=>{ruleEditing='';drawDetail()};
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const row=node('div','','row');row.append(save,cancel);
  f.append(text,when3,src,row,msg);
  f.onsubmit=e=>{
    e.preventDefault();
    postRule(Object.assign(ruleSourceBody(src.value),{kind:r.kind,text:text.value,topic:r.topic,scope:r.scope,group:r.group||'',device:r.device||'',match:ruleMatchBody(wt.value,wc.value,wp.value),replace:[r.id],alongside:true}),msg,n=>{ruleEditing='';loadRules();openDetail('rules',String(n.id))});
  };
  return f;
}
// ruleLinks is the 이어진 것 table: the decisions the row came from and the
// rows before and after it.
function ruleLinks(r){
  const rows=[];
  for(const a of r.answers||[]){
    const t=(a.items||[]).map(it=>it.header+' → '+[...(it.chosen||[]),...(it.other?[it.other]:[])].join(', ')).join(' / ');
    rows.push({cells:['결정',docLink(t||a.id,detailHash('questions','d:'+a.id),a.asker+' → '+a.answered_by+' · '+when(a.answered_at)),'결정'],href:detailHash('questions','d:'+a.id)});
  }
  if(!(r.answers||[]).length)rows.push({cells:['결정',r.source_text||'없음','—']});
  const hist=(r.history||[]).filter(h=>h.id!==r.id);
  for(const h of hist)rows.push({cells:['대체',docLink('#'+h.id+' '+ruleFirstLine(h.text),detailHash('rules',String(h.id))),ruleBadge(h.status)[0]],href:detailHash('rules',String(h.id))});
  if(!hist.length)rows.push({cells:['대체','없음','—']});
  return rows;
}
// ruleBar is the body's foot: 새 규칙 always; 무효로 (asked on the bar) and
// 고쳐서 대체 on a row in force; 승인 and 거절 (with a reason) on a proposal.
function ruleBar(r){
  const bar=docBar(),msg=node('span','','msg');msg.setAttribute('role','status');
  bar.append(msg,node('span','','dc-sp'));
  const add=node('a','새 규칙','button');add.href=detailHash('rules','new');add.title='새 규칙을 올린다';
  bar.append(add);
  if(r.status==='active'){
    const off=node('button','무효로');off.type='button';off.title='#'+r.id+' 를 무효로 돌린다 — 되돌릴 수 없다';
    off.onclick=async()=>{
      if(!await askOn(off,{text:'#'+r.id+' 줄을 무효로 돌릴까요? 되돌릴 수 없습니다.',ok:'무효로 확인',danger:true}))return;
      off.disabled=true;setMsg(msg,'돌리는 중…');
      try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/void');loadRules();drawDetail()}
      catch(e){off.disabled=false;setMsg(msg,e.message,'err')}
    };
    const fix=node('button','고쳐서 대체','btn-p');fix.type='button';fix.title='새 글로 고쳐 #'+r.id+' 를 대체한다';fix.setAttribute('aria-pressed',ruleEditing===String(r.id)?'true':'false');
    fix.onclick=()=>{ruleEditing=ruleEditing===String(r.id)?'':String(r.id);drawDetail()};
    bar.append(off,fix);
  }
  if(r.status==='proposed')bar.append(...ruleReview(r,msg));
  return bar;
}
// ruleReview approves a proposed row (a 409 offers to replace the rows in
// force on its scope and topic or to stand alongside them) or rejects it
// with a reason asked on the bar.
function ruleReview(r,msg){
  const done=()=>{loadRules();drawDetail();if(curTab==='questions')loadQuestions()};
  const approve=async body=>{
    setMsg(msg,'');
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/approve',body);done();return}
    catch(e){
      let c=null;
      if(e.status===409){try{c=JSON.parse(e.body)}catch(x){}}
      if(!c||!c.rules){setMsg(msg,e.message,'err');return}
      msg.className='msg';
      msg.append('같은 범위·주제에 유효한 줄: '+c.rules.map(o=>'#'+o.id+' '+ruleFirstLine(o.text)).join(', ')+' ');
      const rep=node('button','대체');rep.type='button';rep.title='그 줄들을 이 제안으로 대체한다';rep.onclick=()=>approve({replace:c.rules.map(o=>o.id)});
      const side=node('button','나란히');side.type='button';side.title='그 줄들과 나란히 둔다';side.onclick=()=>approve({alongside:true});
      msg.append(rep,' ',side);
    }
  };
  const no=node('button','거절');no.type='button';no.title='까닭과 함께 제안을 거절한다';
  no.onclick=async()=>{
    const why=await askOn(no,{text:'#'+r.id+' 제안을 거절',input:'거절 까닭',ok:'거절 확인',danger:true});if(why===null)return;
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/reject',{reason:why});done()}
    catch(e){setMsg(msg,e.message,'err')}
  };
  const ok=node('button','승인','btn-p');ok.type='button';ok.title='제안을 유효한 규칙으로 채택한다';
  ok.onclick=()=>approve({});
  return [no,ok];
}
// The 새 규칙 body (#rules/new, or new:<question id> from 규칙으로 올리기
// with the question as its source) holds the form kept in #ruleHold.
const ruleFormEl=$('ruleForm');
function ruleNewDraw(src,box){
  if(src&&!$('ruleSource').value)$('ruleSource').value=src;
  box.append(ruleFormEl);
}
registerDetail('rules',{
  list:'규칙',kinds:['rules'],
  load:async id=>{
    if(id==='new'||id.startsWith('new:'))return {neu:true,src:id.slice(4)};
    try{return await api('rules/'+encodeURIComponent(id))}
    catch(e){if(e.status===404)return null;throw e}
  },
  head:r=>r.neu?{title:'새 규칙',crumb:{path:['규칙','새 규칙'],meta:r.src?[['출처 '+r.src,'규칙으로 올리는 답']]:[]}}:
    {title:'#'+r.id+' '+ruleFirstLine(r.text),badge:ruleBadge(r.status),crumb:{path:['규칙',ruleKinds[r.kind]||r.kind,r.topic],
      meta:[[shortWhen(r.created_at)+' 정함','정한 때 — '+when(r.created_at)],[r.author||'-','적은 이'],[ruleScopeText(r.scope)+(r.group?' @'+r.group:''),'범위'],['걸림 '+(r.hits||0),'걸린 수']]}},
  subs:()=>[['body','규칙']],
  draw:async(r,sub,box)=>{
    if(r.neu){ruleNewDraw(r.src,box);return}
    docHead(box,'글');
    box.append(node('p',r.text,'dc-quote'));
    const pairs=[];
    if(r.match)pairs.push(['걸림 조건',ruleMatchText(r.match)]);
    if(r.device)pairs.push(['장치',r.device]);
    if(r.sources&&r.sources.length&&!(r.answers||[]).length)pairs.push(['출처',r.sources.join(', ')]);
    if(r.source_text&&(r.answers||[]).length)pairs.push(['출처 글',r.source_text]);
    if(r.voided_by)pairs.push(['무효로 돌린 이',r.voided_by+' · '+when(r.voided_at)]);
    if(r.reviewed_by)pairs.push([r.status==='rejected'?'거절한 이':'승인한 이',r.reviewed_by+' · '+when(r.reviewed_at)]);
    if(r.review_note)pairs.push(['거절 까닭',r.review_note]);
    if(pairs.length)box.append(dl(pairs));
    docHead(box,'이어진 것');
    box.append(docTable(['갈래','제목','상태'],ruleLinks(r)));
    docHead(box,'걸린 기록 '+(r.hits||0));
    const log=r.hit_log||[],before=r.hits_unlogged||0,gone=(r.hits||0)-before-log.length;
    const hits=log.map(h=>({cells:[shortWhen(h.at),h.role,h.what||'—'],hover:when(h.at)}));
    if(gone>0)hits.push({cells:['—','—','지난 기록 '+gone+' — 지움'],hover:'규칙마다 최근 50줄, 90일까지 남김',cls:'dc-dim'});
    if(before>0)hits.push({cells:['—','—','이전 걸림 '+before+' — 기록 없음'],hover:'걸린 기록을 남기기 전에 센 걸림',cls:'dc-dim'});
    const hitTable=docTable(['때','누가','무엇'],hits,'걸린 적 없음');
    hitTable.classList.add('rule-hits');
    box.append(hitTable);
    if(r.status==='active'&&ruleEditing===String(r.id)){docHead(box,'고쳐서 대체');box.append(ruleEditForm(r))}
    box.append(ruleBar(r));
  }
});
$('ruleNew').onclick=()=>openDetail('rules','new');
