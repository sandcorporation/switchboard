// ---- Tabs ----
// Each tab's loader runs once, the first time the tab is shown; after that the
// tab's own 「새로 고침」 button reloads it. #33 sets tabLoaders.permissions and
// #42 sets tabLoaders.questions / tabLoaders.settings (null = nothing to load).
// The order is the left pane's (#510): the menu rows, then the foot.
// setTabs open from 설정's 화면 rows, inside the 설정 screen (#515, #538);
// 진행 is a menu row under 지시 since #576.
const setTabs=['sessions','terminals'];
const tabNames=['requests','progress','questions','permissions','rules','mail','reports','jobs','metrics','settings','sessions','terminals'];
const tabLoaders={requests:loadRequests,progress:null,sessions:loadSessions,reports:loadReports,jobs:loadJobs,metrics:null,mail:initMail,permissions:null,questions:null,rules:loadRules,settings:null};
const tabLoaded=new Set();
// listTabs are the list screens (#514, #509 계약 4): no panels or splits, a
// list column and the open item's body beside it (html.fr-split). A row
// shows its item at once; with hosting on the workspace steps aside while
// one is on screen.
const listTabs=['requests','questions','mail','reports','jobs'];
function listTab(name){return listTabs.includes(name)}
// fullTabs (#515) are one body with no list column (지표, 진행, 설정 and its
// 화면 rows, #523, #538, #576): like a list screen they take the centre, no panels or
// splits (html.fr-full).
const fullTabs=['metrics','settings','progress','sessions','terminals'];
function pageTab(name){return listTab(name)||fullTabs.includes(name)}
// 설정 and 세션 표·터미널 are the 설정 screen (#538, html.fr-set): the
// left pane is the settings menu, which keeps its scroll and its marked row.
function setListKept(name){return name==='settings'||setTabs.includes(name)}
function frameFull(on){
  const html=document.documentElement;
  html.classList.toggle('fr-full',on);
  html.classList.toggle('fr-set',on&&setListKept(curTab));
  if(ws&&on)document.querySelector('.sc-app-page').hidden=false;
  setChrome();
}
// frameSplit turns the list screen layout on or off.
function frameSplit(on){
  document.documentElement.classList.toggle('fr-split',on);
  const main=document.querySelector('.sc-app-page');if(ws)main.hidden=!on&&!document.documentElement.classList.contains('fr-sess');
  splitEmptyShow();
}
// splitEmptyShow shows the body column's empty state while a list screen
// has nothing open.
function splitEmptyShow(){$('splitEmpty').hidden=!document.documentElement.classList.contains('fr-split')||!!detailCur}
// setTabCount(name,n) shows n as a small count next to the tab name; n
// null/undefined hides it.
// #42 calls it for 'questions' (open questions), #33 for 'permissions' (pending requests),
// #51 for 'metrics' (alert thresholds crossed now; 0 hides the number).
// 답이 필요함 (the questions row, #510) counts open questions and pending
// permission requests together.
const tabCounts={};
function setTabCount(name,n){
  tabCounts[name]=n;
  if(name==='questions'||name==='permissions')tabCountShow('questions',(tabCounts.questions||0)+(tabCounts.permissions||0)||null);
  if(name!=='questions')tabCountShow(name,n);
}
function tabCountShow(name,n){
  const t=$('tab-'+name);if(!t)return;
  t.querySelector('.tab-count').textContent=(n===null||n===undefined)?'':String(n);
  t.toggleAttribute('data-nonzero',!!n);
}
