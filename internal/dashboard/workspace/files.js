// wsFiles is the session folder's files in the workspace (#447): pure
// functions, so files.test.cjs runs them under node.
//
// The right column's 파일 view lists the followed session's folder
// (GET /api/admin/terminals/{id}/files?path=); a file opens as a centre
// panel file:<session>:<path>, a read-only text view of
// GET …/files/raw?path=. The node keeps every path inside the folder; these
// only build the paths and URLs it is asked for.
(function(root){
'use strict';
const TEXT_MAX=2<<20; // wire.FileTextMax: a text file opens whole up to this
// pid is the centre panel of a file.
function pid(session,path){return 'file:'+session+':'+path}
// fileOf is {session, path} for a file panel, null for any other.
function fileOf(p){
  if(typeof p!=='string'||!p.startsWith('file:'))return null;
  const rest=p.slice(5),i=rest.indexOf(':');
  if(i<1||i===rest.length-1)return null;
  return {session:rest.slice(0,i),path:rest.slice(i+1)};
}
// join is a child's path under dir ('' is the folder itself).
function join(dir,name){return dir?dir+'/'+name:name}
// base is a path's last segment.
function base(path){const i=path.lastIndexOf('/');return i<0?path:path.slice(i+1)}
// url is the files API path (after /api/admin/) of session's path; raw for
// the content.
function url(session,path,raw){return 'terminals/'+encodeURIComponent(session)+'/files'+(raw?'/raw':'')+'?path='+encodeURIComponent(path)}
// problem is the text for a failed files request: the server's code
// ({code, error} JSON) or the status.
function problem(status,body){
  let code='';
  try{code=JSON.parse(body).code||''}catch(e){}
  switch(code){
  case 'unsupported':return '이 노드는 파일을 지원하지 않음';
  case 'offline':return '노드 꺼짐';
  case 'refused':return '세션 폴더 밖';
  case 'not-found':return '없음';
  case 'timeout':return '노드 응답 없음';
  case 'bad-request':return '잘못된 경로';
  }
  if(status===404)return '이 주소에서는 파일이 꺼져 있음';
  if(status===403)return '이 주소에서는 파일이 꺼져 있음';
  return '요청 실패('+status+')';
}
// textOf decides how a file shows as text: {text} or {why}. Too big for a
// text view, or bytes that are not text (a NUL, or not UTF-8), have a why.
function textOf(size,bytes){
  if(size>TEXT_MAX)return {why:'너무 큼'};
  const b=bytes instanceof Uint8Array?bytes:new Uint8Array(bytes);
  if(b.indexOf(0)>=0)return {why:'글 파일 아님'};
  try{return {text:new TextDecoder('utf-8',{fatal:true}).decode(b)}}
  catch(e){return {why:'글 파일 아님'}}
}
// size is a byte count for a row.
function size(n){
  if(n<1024)return n+' B';
  if(n<1<<20)return (n/1024).toFixed(n<10240?1:0)+' KB';
  if(n<1<<30)return (n/(1<<20)).toFixed(n<10<<20?1:0)+' MB';
  return (n/(1<<30)).toFixed(1)+' GB';
}
const api={TEXT_MAX:TEXT_MAX,pid:pid,fileOf:fileOf,join:join,base:base,url:url,problem:problem,textOf:textOf,size:size};
if(typeof module==='object'&&module.exports)module.exports=api;else root.wsFiles=api;
})(this);
