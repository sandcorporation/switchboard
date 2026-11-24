// ---- 실시간 ----
// GET /api/admin/live (WebSocket) sends {type:'hello',boot,seq,reset,kinds}
// and {type:'change',seq,kinds}; kinds are tab names. The shown tab reloads
// (not while a field in it has focus), the others reload when next shown.
// Reconnects back off exponentially (1s to 30s) and resume with boot+after.
const liveReload={requests:loadRequests,progress:loadProgress,sessions:loadSessions,reports:loadReports,jobs:loadJobs,metrics:loadMetrics,mail:load,permissions:loadPermissions,rules:loadRules,questions:loadQuestions,settings:loadSettingsView,terminals:loadTerminals};
const liveCounts={rules:refreshRuleCount,progress:refreshProgressCount,requests:refreshRequestCount,reports:refreshReportCount,questions:refreshQuestionCount,permissions:refreshPermissionCount,metrics:refreshMetricsCount};
let liveBoot='',liveSeq=0,liveDelay=1000,liveDirty=new Set(),liveStale=new Set(),liveTimer=0,liveDetail=false;
function liveMark(kinds){
  if(ws)ws.live(kinds);
  // 답이 필요함 lists the permission requests and proposed rules too (#514).
  if(kinds.some(k=>k==='permissions'||k==='rules')&&!kinds.includes('questions'))kinds=kinds.concat('questions');
  frameLive(kinds);
  for(const k of kinds){
    if(detailCur&&(details[detailCur.tab].kinds||[detailCur.tab]).includes(k))liveDetail=true;
    if(!liveReload[k])continue;
    if(tabShown(k))liveDirty.add(k);
    else{if(tabLoaded.has(k))liveStale.add(k);if(liveCounts[k])liveCounts[k]()}
  }
  if((liveDirty.size||liveDetail)&&!liveTimer)liveTimer=setTimeout(liveFlush,300);
}
function liveFlush(){
  liveTimer=0;
  // An open askOn question (#581) holds the redraw too, or it would go.
  const a=document.activeElement,busy=a&&a.matches&&a.matches('input,textarea,select')&&($('panel-'+curTab).contains(a)||$('detail').contains(a))||!!document.querySelector('#detail .ask,#panel-'+curTab+' .ask');
  if(busy){liveTimer=setTimeout(liveFlush,2000);return}
  for(const k of liveDirty){if(tabShown(k))liveReload[k]();else if(tabLoaded.has(k))liveStale.add(k)}
  liveDirty.clear();
  if(liveDetail){liveDetail=false;drawDetail()}
}
function liveConnect(){
  const u=new URL('/api/admin/live',location.href);
  u.protocol=location.protocol==='https:'?'wss:':'ws:';
  if(liveBoot){u.searchParams.set('boot',liveBoot);u.searchParams.set('after',String(liveSeq))}
  let ws;
  try{ws=new WebSocket(u)}catch(e){liveRetry();return}
  ws.onmessage=e=>{
    let m;try{m=JSON.parse(e.data)}catch(err){return}
    if(m.type==='hello'){
      liveDelay=1000;$('live').className='live on';$('live').title='실시간';$('liveText').textContent='실시간';
      const all=m.reset;liveBoot=m.boot;liveSeq=m.seq;
      liveMark(all?Object.keys(liveReload):m.kinds||[]);
    }else if(m.type==='change'){liveSeq=m.seq;liveMark(m.kinds||[])}
  };
  ws.onclose=()=>{$('live').className='live';$('live').title='실시간 연결 끊김';$('liveText').textContent='끊김';liveRetry()};
}
function liveRetry(){
  const d=liveDelay;liveDelay=Math.min(liveDelay*2,30000);
  setTimeout(liveConnect,d/2+Math.random()*d/2);
}
let curTab='';
function showTab(name){
  if(name!=='terminals'&&$('termAdd').open)$('termAdd').close();
  curTab=name;
  // 권한 요청 sits in 답이 필요함 (#514), 세션 표·터미널 in 설정 (#515):
  // that row stays marked. 규칙 is a menu row of its own (#581).
  const mark=name==='permissions'?'questions':setTabs.includes(name)?'settings':name;
  for(const n of tabNames){
    const on=n===name,t=$('tab-'+n);
    t.setAttribute('aria-selected',n===mark?'true':'false');
    t.tabIndex=n===mark?0:-1;
    // In the workspace every section shows wherever its panel is, except a
    // list screen's (listTab), which stays in the page.
    $('panel-'+n).hidden=ws&&!pageTab(n)?false:!on;
    if(n===mark)$('crumbTab').textContent=t.querySelector('.tab-name').textContent;
  }
  frameSplit(listTab(name));
  // A menu is on screen (#519): no session screen, no right column.
  frameScreen('menu');
  frameFull(fullTabs.includes(name));
  if(setListKept(name))loadTab('settings');
  loadTab(name);
  if(ws&&!pageTab(name))ws.showMenu(name);
}
// loadTab loads a tab's section the first time it shows, and again when a
// live change marked it stale meanwhile.
function loadTab(name){
  if(!tabLoaded.has(name)){tabLoaded.add(name);if(tabLoaders[name])tabLoaders[name]()}
  else if(liveStale.has(name))liveReload[name]();
  liveStale.delete(name);
}
// tabShown: the current tab, or (workspace) any section on screen.
function tabShown(k){return k===curTab||!!(ws&&ws.shown(k))}
// Routes: #progress #sessions #metrics #mail #permissions #questions #rules #settings (the last tab),
// #settings-<tab> such as #settings-roles or #settings-qsettings (설정 tabs), #decisions (답이 필요함,
// 결정 view, #581), #q=<id> (rewritten to #questions/<id>/answer), #terminal=<id> (an older link: it becomes
// #terminals/<id>/screen), and #<tab>/<id>/<sub> for a tab with a detail panel
// (registerDetail). No hash or an unknown one shows 지시 (#501).
function route(){
  const h=location.hash.slice(1);
  const parts=h.split('/');
  // A permission request opens in 답이 필요함 (#514).
  if(parts[0]==='permissions'&&parts[1]){history.replaceState(null,'',detailHash('questions','p:'+parts[1]));route();return}
  if(parts.length>1&&details[parts[0]]&&visibleTabs().includes(parts[0])&&parts[1]){
    let id=parts[1];try{id=decodeURIComponent(id)}catch(e){}
    if(parts[0]==='questions')showQuestionView(id.startsWith('d:')?'decisions':'questions');
    // The 설정 menu marks the item's category (#538).
    if(parts[0]==='settings')showSettingsView(setKindView[id.slice(0,id.indexOf(':'))]);
    showTab(parts[0]);showDetail(parts[0],id,parts[2]||'');
    return;
  }
  hideDetail();
  // 권한 요청 has no menu row of its own (#514): its list is in 답이 필요함.
  if(h==='permissions'){history.replaceState(null,'','#questions');route();return}
  // #session=<role>: that session's screen (#519).
  if(h.startsWith('session=')){
    let role=h.slice(8);try{role=decodeURIComponent(role)}catch(e){}
    frameSession(role);
    return;
  }
  if(h.startsWith('terminal=')){
    let id=h.slice(9);try{id=decodeURIComponent(id)}catch(e){}
    if(!hostingOn)return;
    history.replaceState(null,'',detailHash('terminals',id,'screen'));route();
    return;
  }
  if(h==='decisions'){showQuestionView('decisions');showTab('questions');return}
  // 설정 itself (#settings) leaves a list already on screen where it was (#536).
  if(settingsViewOfHash(h)){showSettingsView(settingsViewOfHash(h));showTab('settings');return}
  if(h==='questions'||h.startsWith('q='))showQuestionView('questions');
  if(h.startsWith('q=')){
    // Old question links (Discord, panel) open the question's detail.
    let id=h.slice(2);try{id=decodeURIComponent(id)}catch(e){}
    history.replaceState(null,'',detailHash('questions',id,'answer'));
    route();
    return;
  }
  showTab(visibleTabs().includes(h)?h:'requests');
}
// selectTab(name) also takes 'decisions' (the 결정 view of 답이 필요함).
function selectTab(name){
  if(location.hash==='#'+name)route();else location.hash=name;
}
for(const n of tabNames)$('tab-'+n).onclick=()=>selectTab(n);
// visibleTabs leaves out 터미널 unless hosting is on.
function visibleTabs(){return tabNames.filter(n=>n!=='terminals'||hostingOn)}
for(const tl of [$('tabs'),$('tabsMore')])tl.addEventListener('keydown',e=>{
  const tabs=visibleTabs().filter(n=>!$('tab-'+n).hidden&&!setTabs.includes(n));
  const i=tabs.findIndex(n=>$('tab-'+n)===document.activeElement);
  if(i<0)return;
  let j;
  if(e.key==='ArrowRight'||e.key==='ArrowDown')j=(i+1)%tabs.length;
  else if(e.key==='ArrowLeft'||e.key==='ArrowUp')j=(i-1+tabs.length)%tabs.length;
  else if(e.key==='Home')j=0;
  else if(e.key==='End')j=tabs.length-1;
  else return;
  e.preventDefault();
  selectTab(tabs[j]);
  $('tab-'+tabs[j]).focus();
});
window.addEventListener('hashchange',route);

$('go').onclick=()=>{mailID='';load()};
$('reloadMail').onclick=load;
$('reloadSessions').onclick=loadSessions;
$('reloadProgress').onclick=loadProgress;
$('reloadMetrics').onclick=loadMetrics;
$('metDays7').onclick=()=>setMetricsDays(7);
$('metDays30').onclick=()=>setMetricsDays(30);
$('reloadReports').onclick=()=>loadReports();
$('reloadRequests').onclick=()=>loadRequests();
$('reloadRules').onclick=()=>loadRules();
$('ruleTopic').onchange=()=>loadRules();
$('ruleState').onchange=()=>loadRules();
$('ruleForm').onsubmit=e=>{
  e.preventDefault();
  const body=Object.assign(ruleSourceBody($('ruleSource').value),{kind:$('ruleKind').value,text:$('ruleText').value,topic:$('ruleTopicIn').value.trim(),new_topic:$('ruleNewTopic').checked,scope:parseRuleScope($('ruleScope').value),group:$('ruleGroup').value.trim(),match:ruleMatchBody($('ruleWhenTool').value,$('ruleWhenCommand').value,$('ruleWhenPath').value)});
  postRule(body,$('ruleAddMsg'),r=>{for(const id of ['ruleText','ruleSource','ruleWhenTool','ruleWhenCommand','ruleWhenPath'])$(id).value='';loadRules();openDetail('rules',String(r.id))});
};
$('reloadJobs').onclick=()=>loadJobs();
$('jobRole').onchange=()=>loadJobs();
$('jobFind').oninput=()=>{if(jobsLast)drawJobs(...jobsLast)};
$('reloadQuestions').onclick=()=>{setMsg($('questionsMsg'),'');loadQuestions()};
$('reset').onclick=()=>{$('thread').value='';$('task').value='';mailID='';$('search').value='';$('status').value='';$('role').value='';$('box').value='all';load()};
$('search').addEventListener('keydown',e=>{if(e.key==='Enter')load()});
$('thread').addEventListener('keydown',e=>{if(e.key==='Enter')load()});
$('task').addEventListener('keydown',e=>{if(e.key==='Enter'){mailID='';load()}});
loadMe().then(frameStart);
route();
if(!tabLoaded.has('questions'))refreshQuestionCount();
if(!tabLoaded.has('rules'))refreshRuleCount();
if(!tabLoaded.has('reports'))refreshReportCount();
if(!tabLoaded.has('requests'))refreshRequestCount();
if(!tabLoaded.has('progress'))refreshProgressCount();
if(!tabLoaded.has('metrics'))refreshMetricsCount();
if(!tabLoaded.has('permissions'))refreshPermissionCount();
liveConnect();
