// ---- 세션 ----
const stTone={'st-idle':'success','st-busy':'info','st-compacting':'info','st-error':'danger','st-blocked':'warning','st-stalled':'danger'};
const sessionCols=['역할','상태','컨텍스트','마지막 턴','기계','판','라벨'];
// cliText is a session's CLI version (#308): '-' before it reported one,
// 판 모름 for no header or a development build.
function cliText(v){return !v.cli_version?'-':v.cli==='unknown'?'판 모름':v.cli_version}
const sessionChips=[['all','전체',()=>true],['busy','바쁨',v=>v.online&&/^(busy|compacting)$/.test(v.status)],['problem','오류·막힘·멈춤',v=>/^(error|blocked|stalled):/.test(v.status||'')],['offline','오프라인',v=>!v.online]];
// sessionsSeq drops an answer that a later load (live update, hosting flag
// arriving from /api/admin/me) has overtaken.
let sessionsData=null,sessionsFilter='all',sessionsSeq=0;
async function loadSessions(){
  const seq=++sessionsSeq;
  $('sessionsNote').textContent='불러오는 중…';
  try{
    const p=await api('presence');
    const hosted=hostingOn?await hostedBySlot():new Map();
    if(seq!==sessionsSeq)return;
    sessionsData={p:p,hosted:hosted};
    drawSessions();
    $('sessionsNote').title='심장박동 '+p.interval_seconds+'초, '+p.missed+'번 놓치면 오프라인';
  }catch(e){if(seq===sessionsSeq)$('sessionsNote').textContent=e.message}
}
function drawSessions(){
  if(!sessionsData)return;
  const {p,hosted}=sessionsData,q=$('sessionsSearch').value.trim().toLowerCase();
  const chips=$('sessionChips');chips.replaceChildren();
  for(const [k,label,f] of sessionChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===sessionsFilter?'true':'false');
    b.append(node('span',String(p.sessions.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{sessionsFilter=k;drawSessions()};
    chips.append(b);
  }
  const keep=sessionChips.find(c=>c[0]===sessionsFilter)[2];
  const shown=p.sessions.filter(v=>keep(v)&&(!q||[v.role,v.machine,v.directory,v.label||''].some(s=>(s||'').toLowerCase().includes(q))));
  const body=$('sessions');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const v of shown){
      const [stText,stClass]=sessionStatus(v.status);
      const tr=node('tr','',!v.online?'offline':/^st-(error|blocked|stalled)$/.test(stClass)?'row'+stClass.slice(2):'');
      const cells=[v.role,stText,kTokens(v.context_tokens),ago(p.now,v.last_turn_at),v.machine,cliText(v),v.label||'-'];
      cells.forEach((text,i)=>{const td=node('td',text,i===1?stClass:i===2?'num nowrap':i===3?'nowrap':'');if(i>0)td.dataset.label=sessionCols[i];tr.append(td)});
      tr.children[3].title=when(v.last_turn_at);
      tr.children[5].classList.add('mono','nowrap');
      if(v.cli==='old'){tr.children[5].classList.add('cli-old');tr.children[5].append(' ',badge('옛 판','warning'))}
      if(v.subagents>0){const s=node('span','서브 '+v.subagents,'subs');s.title=subagentNote(v.subagents);tr.children[1].append(' · ',s)}
      const h=hosted.get(v.role);
      if(h){const a=node('a','터미널','hosted');a.href=detailHash('terminals',h.id,'screen');a.title=h.node+' · '+h.directory;tr.children[0].append(' ',a)}
      detailRow(tr,'sessions',v.role);
      body.append(tr);
    }
    if(!shown.length)emptyRow(body,sessionCols.length,'세션 없음');
  });
  $('sessionsNote').textContent=(shown.length===p.sessions.length?'':shown.length+' / ')+p.sessions.length+'개';
}
$('sessionsSearch').addEventListener('input',drawSessions);
registerDetail('sessions',{
  list:'세션',kinds:['sessions','mail','terminals'],
  load:async id=>{
    const p=await api('presence');const v=p.sessions.find(s=>s.role===id);
    if(!v)return null;
    return {v:v,now:p.now,roles:new Set(p.sessions.map(s=>s.role)),h:hostingOn?(await hostedBySlot()).get(id):null};
  },
  head:it=>{const [t,c]=sessionStatus(it.v.status);return {title:it.v.role,badge:[it.v.online?t:'오프라인',it.v.online?stTone[c]||'neutral':'neutral']}},
  subs:it=>[['status','상태'],['mail','메일']].concat(it.h?[['terminal','터미널']]:[]),
  draw:async(it,sub,box)=>{
    const v=it.v;
    if(sub==='mail'){
      const rows=(await api('messages?'+new URLSearchParams({role:v.role}))).slice(0,20);
      const ul=node('ul','','dlist');
      for(const m of rows){
        const li=node('li','');const a=node('a','#'+m.id+' '+m.subject);a.href=detailHash('mail',m.id,'body');
        li.append(a,node('small',m.sender+' → '+m.recipient+' · '+(statusLabel[m.status]||m.status)+' · '+when(m.created_at)));
        ul.append(li);
      }
      box.append(rows.length?ul:node('p','메일 없음','empty'));
      return;
    }
    if(sub==='terminal'){
      const a=node('a',termRole(it.h)+(it.h.label?' · '+it.h.label:''),'hosted');a.href=detailHash('terminals',it.h.id,'screen');a.title=it.h.node+' · '+it.h.directory;
      box.append(dl([['노드',it.h.node],['폴더',it.h.directory],['터미널',a]]),termPaneFor(it.h));
      return;
    }
    const [stText,stClass]=sessionStatus(v.status);
    const st=node('span',stText,stClass);
    let parent=v.parent||'-';
    if(v.parent&&it.roles.has(v.parent)){parent=node('a',v.parent);parent.href=detailHash('sessions',v.parent,'status')}
    const last=node('span',ago(it.now,v.last_turn_at));last.title=when(v.last_turn_at);
    box.append(dl([['상태',st],['접속',v.online?'온라인':'오프라인'],['서브 에이전트',v.subagents>0?subagentNote(v.subagents):'-'],['컨텍스트',kTokens(v.context_tokens)],['마지막 턴',last],['기계',v.machine||'-'],['판',cliText(v)+(v.cli==='old'?' · 옛 판':'')],['폴더',v.directory||'-'],['라벨',v.label||'-'],['부모',parent],['마지막 보고',ago(it.now,v.reported_at)]]));
  }
});
