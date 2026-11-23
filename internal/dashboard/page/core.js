const sessionLabel={idle:'유휴',busy:'바쁨',compacting:'compact 중',unknown:'모름',offline:'오프라인'};
function sessionStatus(st){
  const m=/^(error|blocked):(.*)$/.exec(st||'');
  if(m)return [(m[1]==='error'?'오류: ':'막힘: ')+m[2],'st-'+m[1]];
  return [sessionLabel[st]||st,'st-'+st];
}
// subagentNote is the text next to a session's state while background sub-agents run.
function subagentNote(n){return n>0?'서브 에이전트 '+n+'개 작업 중':''}
const statusLabel={pending:'안 읽음',read:'읽음',acknowledged:'확인함',complete:'완료',completed:'완료',held:'보관 중'};
const $=id=>document.getElementById(id);
// adminMode is the mode from /api/admin/me. Outside the proxy-secret
// transition mode a 401 means the session ended: reload to the login page.
let adminMode='';
// hostingOn is the hosting flag from /api/admin/me (terminal hosting, #173).
let hostingOn=false;
// termAssets are the terminal view's files, listed by /api/admin/me only with hosting on.
let termAssets=[];
// editorAssets are the file editor's (#448), listed with hosting on.
let editorAssets=[];
// termRefused is why this address cannot open a terminal ('' when it can).
let termRefused='';
// adminPerms is what this login may do on the work screen (#476): view,
// type, files, sessions, discard. can(p) is whether it holds p.
let adminPerms=['view','type','files','sessions','discard'];
function can(p){return adminPerms.includes(p)}
function signedOut(){if(adminMode&&adminMode!=='legacy')location.reload()}
async function loadMe(){
  try{
    const r=await fetch('/api/admin/me',{credentials:'same-origin',headers:{'Accept':'application/json'}});
    if(!r.ok)return;
    const m=await r.json();adminMode=m.mode||'';hostingOn=m.hosting===true;termAssets=Array.isArray(m.terminal_assets)?m.terminal_assets:[];editorAssets=Array.isArray(m.editor_assets)?m.editor_assets:[];termRefused=typeof m.terminal_refused==='string'?m.terminal_refused:'';if(Array.isArray(m.perms))adminPerms=m.perms;
    $('termAddOpen').hidden=!can('sessions');
    if(hostingOn)loadWorkspace(Array.isArray(m.workspace_assets)?m.workspace_assets:[]);
    $('tab-terminals').hidden=!hostingOn;setViewButton('nodes').hidden=!hostingOn;setViewButton('groups').hidden=!hostingOn;
    if(hostingOn&&curTab==='settings')route();
    if(hostingOn&&location.hash.startsWith('#terminal'))route();
    if(hostingOn&&curTab==='sessions')loadSessions();
    if(hostingOn&&detailCur)drawDetail();
    $('whoName').textContent=m.mode==='legacy'?'프록시':m.user;
    $('whoName').title=$('whoName').textContent;
    $('whoMark').textContent=[...$('whoName').textContent][0]||'';$('who').title=$('whoName').textContent;$('who').hidden=false;
    $('logout').hidden=m.mode==='legacy';
  }catch(e){}
}
$('logout').onclick=async()=>{
  $('logout').disabled=true;
  try{await fetch('/auth/logout',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-Switchboard-Dashboard':'1'},body:'{}'})}catch(e){}
  location.replace('/');
};
async function api(path){
  let r;
  try{r=await fetch('/api/admin/'+path,{credentials:'same-origin',headers:{'Accept':'application/json'}})}
  catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
  if(r.status===401)signedOut();
  if(r.redirected||!(r.headers.get('content-type')||'').includes('application/json'))throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
  if(!r.ok)throw Error(r.status===401?'로그인 필요(401)':'요청 실패('+r.status+')');
  return r.json();
}
// apiSend makes an admin write with the write guard headers. Errors carry the
// HTTP status (e.status) and the server's message.
async function apiSend(method,path,body){
  let r;
  try{r=await fetch('/api/admin/'+path,{method:method,credentials:'same-origin',headers:{'Accept':'application/json','Content-Type':'application/json','X-Switchboard-Dashboard':'1'},body:body===undefined?undefined:JSON.stringify(body)})}
  catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
  if(r.redirected)throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
  if(r.ok){
    if(!(r.headers.get('content-type')||'').includes('application/json'))throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
    return r.json();
  }
  if(r.status===401)signedOut();
  let raw='';try{raw=(await r.text()).trim()}catch(e){}
  const text=raw.slice(0,300);
  const e=Error(r.status===401?'로그인 필요(401)':r.status===403?'쓰기 거부(403) · 같은 주소에서 열었는지 확인':'요청 실패('+r.status+')'+(text?': '+text:''));
  e.status=r.status;e.body=raw;throw e;
}
function node(tag,text,cls){const n=document.createElement(tag);n.textContent=text;if(cls)n.className=cls;return n}
async function initMail(){
  try{for(const v of await api('roles'))$('role').append(new Option(v.role,v.role));await load()}
  catch(e){$('conn').textContent=e.message}
}
function kTokens(n){if(n===null||n===undefined)return '-';return n<10000?(n/1000).toFixed(1)+'k':Math.round(n/1000)+'k'}
function when(t){return t?new Date(t).toLocaleString('ko-KR'):'-'}
function byLabel(by){return by==='panel'?'세션 패널':by}
function ago(now,t){if(!t)return '-';const s=Math.max(0,Math.round((new Date(now)-new Date(t))/1000));return s<120?s+'초 전':s<7200?Math.round(s/60)+'분 전':Math.round(s/3600)+'시간 전'}
