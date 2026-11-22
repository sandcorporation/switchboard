// wsSide is the workspace's right column (#445): pure functions, so
// side.test.cjs runs them under node.
//
// The column follows the session of the focused centre panel (a terminal
// panel, term:<id>, or a file of its folder, file:<id>:<path>); any other
// panel leaves the session it showed. Its state
// is {session, sub}: the followed hosted session's id and the open view
// (파일 files (#447), 기록 log, 예약 autos, 설정 settings). Above it, 「답이 필요함」 lists every
// session's pending questions and permission requests; one opens as a centre
// panel, ask:<id> or perm:<id>.
(function(root){
'use strict';
const SUBS=[['files','파일'],['log','기록'],['autos','예약'],['settings','설정']],DEFAULT_SUB='log';
const ITEMS={ask:'questions',perm:'permissions'};
function initial(){return {session:'',sub:DEFAULT_SUB}}
// sessionOf is the hosted session a panel shows, '' for none.
function sessionOf(pid){
  if(typeof pid!=='string')return '';
  if(pid.startsWith('term:'))return pid.slice(5);
  if(!pid.startsWith('file:'))return '';
  const i=pid.indexOf(':',5);
  return i>5?pid.slice(5,i):'';
}
// follow is the session shown once focus is on pid.
function follow(cur,pid){return sessionOf(pid)||cur||''}
// itemOf is {tab, id} for an item panel: tab names the page's detail spec.
function itemOf(pid){
  if(typeof pid!=='string')return null;
  const i=pid.indexOf(':');
  if(i<1||i===pid.length-1)return null;
  const tab=ITEMS[pid.slice(0,i)];
  return tab?{tab:tab,id:pid.slice(i+1)}:null;
}
// needs is the 「답이 필요함」 rows, oldest first: questions and permission
// requests still pending, each {pid, kind, title, who, at}.
function needs(questions,perms){
  const out=[];
  for(const q of questions||[])if(q&&q.id&&(q.status===undefined||q.status==='pending'))
    out.push({pid:'ask:'+q.id,kind:'ask',title:(q.questions||[]).map(x=>x.header).join(' · ')||q.id,who:q.asker||'',at:q.created_at||''});
  for(const v of perms||[])if(v&&v.id&&(v.status===undefined||v.status==='pending'))
    out.push({pid:'perm:'+v.id,kind:'perm',title:(v.tool||'')+(v.summary?' · '+v.summary:''),who:v.requester||'',at:v.created_at||''});
  const t=x=>{const n=Date.parse(x.at);return isNaN(n)?Infinity:n};
  return out.sort((a,b)=>t(a)-t(b));
}
// parse reads a stored state back; anything else gives the initial one.
function parse(text){
  try{
    const o=JSON.parse(text);
    if(!o||typeof o!=='object'||typeof o.session!=='string')return initial();
    return {session:o.session,sub:SUBS.some(s=>s[0]===o.sub)?o.sub:DEFAULT_SUB};
  }catch(e){return initial()}
}
function serialize(s){return JSON.stringify({session:s.session,sub:s.sub})}
const api={SUBS:SUBS,initial:initial,sessionOf:sessionOf,follow:follow,itemOf:itemOf,needs:needs,parse:parse,serialize:serialize};
if(typeof module==='object'&&module.exports)module.exports=api;else root.wsSide=api;
})(this);
