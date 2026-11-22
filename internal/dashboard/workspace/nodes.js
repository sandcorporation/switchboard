// wsNodes is the 노드 index's data side (#446): pure functions over the
// rows of /api/admin/nodes, /api/admin/terminals and /api/admin/host-grants,
// so nodes.test.cjs runs them under node.
(function(root){
'use strict';
// parseVersion reads "v1.2.3" or "1.2.3" (a suffix after the numbers is
// ignored); anything else is null.
function parseVersion(v){
  const m=/^v?(\d+)\.(\d+)(?:\.(\d+))?/.exec(String(v||'').trim());
  return m?[+m[1],+m[2],+(m[3]||0)]:null;
}
// older says whether node version v is below server version s. A version
// either side cannot read is never older.
function older(v,s){
  const a=parseVersion(v),b=parseVersion(s);
  if(!a||!b)return false;
  for(let i=0;i<3;i++)if(a[i]!==b[i])return a[i]<b[i];
  return false;
}
// state is a node's row state: revoked (끊음), on (붙음) or off (끊김).
function state(n){return n.revoked_at?'revoked':n.connected?'on':'off'}
const stateText={revoked:'끊음',on:'붙음',off:'끊김'};
// sessionsOn lists the hosted sessions of node name.
function sessionsOn(recs,name){return (recs||[]).filter(r=>r.node===name)}
// grantsOn lists the grants that name node name.
function grantsOn(grants,name){return (grants||[]).filter(g=>(g.nodes||[]).includes(name))}
// list splits a field on commas and whitespace, drops empties and repeats.
function list(s){
  const out=[];
  for(const p of String(s||'').split(/[,\s]+/))if(p&&!out.includes(p))out.push(p);
  return out;
}
// grantWith is the body that gives role node name: the role's grant
// (existing, or none) with name added. Bases and roots given replace the
// grant's; left empty they keep them.
function grantWith(existing,name,role,bases,roots){
  const g=existing||{nodes:[],bases:[],dir_roots:[]};
  const b=list(bases),d=list(roots),nodes=(g.nodes||[]).slice();
  if(!nodes.includes(name))nodes.push(name);
  const out={role:role,bases:b.length?b:(g.bases||[]),nodes:nodes,dir_roots:d.length?d:(g.dir_roots||[])};
  if(g.adapters&&g.adapters.length)out.adapters=g.adapters.slice();
  return out;
}
// grantWithout is how to take node name out of grant g: a DELETE of the
// whole grant when name is its only node, else a POST of the rest.
function grantWithout(g,name){
  const nodes=(g.nodes||[]).filter(n=>n!==name);
  if(!nodes.length)return {method:'DELETE',path:'host-grants?role='+encodeURIComponent(g.role)};
  const body={role:g.role,bases:g.bases||[],nodes:nodes,dir_roots:g.dir_roots||[]};
  if(g.adapters&&g.adapters.length)body.adapters=g.adapters.slice();
  return {method:'POST',path:'host-grants',body:body};
}
// usageText is one agent account's remaining usage line.
function usageText(u){
  const who=u.adapter+(u.account?' · '+u.account:'');
  return who+' '+(u.remaining==null?'-':u.remaining+'%');
}
const actionText={'node.invite':'초대','node.revoke':'끊기','grant.set':'허가','grant.revoke':'허가 거둠','session.add':'세션 넣기','session.change':'세션 고침','session.remove':'세션 지움','file.save':'파일 저장'};
function actionName(a){return actionText[a]||a}
const api={parseVersion:parseVersion,older:older,state:state,stateText:stateText,sessionsOn:sessionsOn,grantsOn:grantsOn,list:list,grantWith:grantWith,grantWithout:grantWithout,usageText:usageText,actionName:actionName};
if(typeof module==='object'&&module.exports)module.exports=api;else root.wsNodes=api;
})(this);
