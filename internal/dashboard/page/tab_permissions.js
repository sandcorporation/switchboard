// ---- 권한 요청 ----
// GET /api/admin/permissions?status=pending and ?status=closed; answers go to
// POST /api/admin/permissions/{id}/answer, the same server function as
// permission answer. Every field is a text node.
const permStatusLabel={pending:'대기',allowed:'허용',denied:'거절',expired:'만료'};
function permFields(v){
  const dl=node('dl','','pdl');
  const rows=[['ID',v.id],['요청한 역할',v.requester],['기본 역할',v.requester_base],['도구',v.tool],
    ['부모',v.parent||'-'],['부모 기본 역할',v.parent_base||'-'],['보고된 부모',v.declared_parent||'-'],
    ['요청',when(v.created_at)],['만료',when(v.expires_at)],['결과',permStatusLabel[v.status]||v.status]];
  if(v.decision)rows.push(['결정',v.decision]);
  if(v.note)rows.push(['메모',v.note]);
  if(v.answered_by)rows.push(['답한 쪽',v.answered_by],['답한 때',when(v.answered_at)]);
  for(const [k,t] of rows)dl.append(node('dt',k),node('dd',t));
  return dl;
}
const permTone={pending:'warning',allowed:'success',denied:'danger',expired:'neutral'};
const permCols=['상태','도구 · 요약','요청한 쪽','요청','만료'],permHistCols=['결과','도구 · 요약','요청한 쪽','요청','답한 쪽'];
function permAnswerForm(v){
  const form=node('form','','panswer');
  const lab=node('label','메모');const note=node('input','');note.maxLength=500;note.autocomplete='off';note.title='답에 붙여 보낼 메모';lab.append(note);
  const allow=node('button','허용','ok');allow.type='button';allow.title='이 도구 사용을 허용한다 — 답은 바꿀 수 없다';
  const deny=node('button','거절','danger');deny.type='button';deny.title='이 도구 사용을 거절한다 — 답은 바꿀 수 없다';
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const send=async decision=>{
    const word=decision==='allow'?'허용':'거절';
    if(!await askOn(decision==='allow'?allow:deny,{text:v.requester+'의 '+v.tool+' 요청을 '+word+'할까요? 답은 바꿀 수 없습니다.',ok:word+' 확인',danger:decision!=='allow'}))return;
    allow.disabled=deny.disabled=true;
    try{
      await apiSend('POST','permissions/'+encodeURIComponent(v.id)+'/answer',{decision:decision,note:note.value});
      setMsg($('permsMsg'),v.id+' '+word+'했습니다.','ok');
      $('detailTitle').focus({preventScroll:true});
      drawDetail();
      if(curTab==='permissions')loadPermissions();
      if(curTab==='questions')loadQuestions();
    }catch(e){setMsg(msg,e.message,'err');allow.disabled=deny.disabled=false}
  };
  allow.onclick=()=>send('allow');deny.onclick=()=>send('deny');
  form.onsubmit=e=>e.preventDefault();
  const row=node('div','','row');row.append(allow,deny);
  form.append(lab,row,msg);
  return form;
}
function permRow(v,hist){
  const tr=node('tr','',v.status==='pending'?'':'done');
  const st=cell('');st.append(badge(permStatusLabel[v.status]||v.status,permTone[v.status]||'neutral'));
  const t=cell('',permCols[1]);t.append(node('strong',v.tool),node('small',v.summary));
  const at=cell(shortWhen(v.created_at),permCols[3],'nowrap');at.title=when(v.created_at);
  const last=hist?cell(v.answered_by||'-',permHistCols[4]):cell(shortWhen(v.expires_at),permCols[4],'nowrap');
  if(!hist)last.title=when(v.expires_at);
  tr.append(st,t,cell(v.requester,permCols[2]),at,last);
  detailRow(tr,'permissions',v.id);
  return tr;
}
let permTimer=0;
async function loadPermissions(){
  $('permsNote').textContent='불러오는 중…';
  try{
    const [open,closed]=await Promise.all([api('permissions?status=pending'),api('permissions?status=closed&limit=200')]);
    const p=open.permissions,h=closed.permissions;
    const pb=$('permPending'),hb=$('permHistory');
    keepRowFocus(pb,()=>{pb.replaceChildren(...p.map(v=>permRow(v,false)));if(!p.length)emptyRow(pb,permCols.length,'대기 중인 요청 없음')});
    keepRowFocus(hb,()=>{hb.replaceChildren(...h.map(v=>permRow(v,true)));if(!h.length)emptyRow(hb,permHistCols.length,'기록 없음')});
    setTabCount('permissions',p.length||null);
    $('permsNote').textContent='대기 '+p.length+' · 기록 '+h.length+'(최대 200)';
    // A pending request turns expired by time alone: reload at the next expiry.
    clearTimeout(permTimer);
    const next=Math.min(...p.map(v=>new Date(v.expires_at).getTime()));
    if(isFinite(next))permTimer=setTimeout(loadPermissions,Math.max(1000,next-Date.now()+500));
  }catch(e){$('permsNote').textContent=e.message}
}
registerDetail('permissions',{
  list:'권한 요청',kinds:['permissions'],
  load:async id=>{
    try{return await api('permissions/'+encodeURIComponent(id))}
    catch(e){if(/\(404\)/.test(e.message))return null;throw e}
  },
  head:v=>({title:v.tool+' · '+v.requester,badge:[permStatusLabel[v.status]||v.status,permTone[v.status]||'neutral']}),
  subs:()=>[['request','요청']],
  draw:async(v,sub,box)=>{
    box.append(permFields(v));
    if(v.summary)box.append(node('pre',v.summary));
    if(v.status==='pending'&&v.can_answer)box.append(permAnswerForm(v));
  }
});
async function refreshPermissionCount(){
  try{const p=(await api('permissions?status=pending')).permissions;setTabCount('permissions',p.length||null)}catch(e){}
}
$('reloadPerms').onclick=()=>{setMsg($('permsMsg'),'');loadPermissions()};
tabLoaders.permissions=loadPermissions;
