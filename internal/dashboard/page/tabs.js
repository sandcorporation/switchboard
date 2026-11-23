// ---- Tabs ----
// Each tab's loader runs once, the first time the tab is shown; after that the
// tab's own 「새로 고침」 button reloads it. #33 sets tabLoaders.permissions and
// #42 sets tabLoaders.questions / tabLoaders.settings (null = nothing to load).
// The order is the left pane's (#510): the menu rows, then the foot.
const tabNames=['requests','questions','permissions','rules','mail','reports','jobs','metrics','settings','progress','sessions','terminals'];
const tabLoaders={requests:loadRequests,progress:null,sessions:loadSessions,reports:loadReports,jobs:loadJobs,metrics:null,mail:initMail,permissions:null,questions:null,rules:loadRules,settings:null};
const tabLoaded=new Set();
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
