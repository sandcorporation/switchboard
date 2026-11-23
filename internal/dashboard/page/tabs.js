// ---- Tabs ----
// Each tab's loader runs once, the first time the tab is shown; after that the
// tab's own 「새로 고침」 button reloads it. #33 sets tabLoaders.permissions and
// #42 sets tabLoaders.questions / tabLoaders.settings (null = nothing to load).
const tabNames=['requests','progress','sessions','mail','reports','jobs','questions','permissions','rules','metrics','terminals','settings'];
const tabLoaders={requests:loadRequests,progress:null,sessions:loadSessions,reports:loadReports,jobs:loadJobs,metrics:null,mail:initMail,permissions:null,questions:null,rules:loadRules,settings:null};
const tabLoaded=new Set();
// setTabCount(name,n) shows n as a small count next to the tab name; n
// null/undefined hides it.
// #42 calls it for 'questions' (open questions), #33 for 'permissions' (pending requests),
// #51 for 'metrics' (alert thresholds crossed now; 0 hides the number).
function setTabCount(name,n){
  const t=$('tab-'+name);if(!t)return;
  t.querySelector('.tab-count').textContent=(n===null||n===undefined)?'':String(n);
  t.toggleAttribute('data-nonzero',!!n);
}
