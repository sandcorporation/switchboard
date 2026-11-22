// wsFiles is the session folder's files in the workspace (#447): pure
// functions, so files.test.cjs runs them under node.
//
// The right column's 파일 view lists the followed session's folder
// (GET /api/admin/terminals/{id}/files?path=); a file opens as a centre
// panel file:<session>:<path> (an image, audio or video player, else the editor) over
// GET …/files/raw?path=. The node keeps every path inside the folder; these
// only build the paths and URLs it is asked for.
// A text file opens in the editor (#448, CodeMirror from assets/codemirror)
// and saves with PUT …/files/raw?path=, sending back the fingerprint the
// read gave (FP_HEADER); a file changed since is 409, and the admin picks
// 다시 읽기 or 그래도 저장 (fingerprint *).
(function(root){
'use strict';
const TEXT_MAX=2<<20; // wire.FileTextMax: a text file opens whole up to this, and saves
// FP_HEADER is config.HeaderPrefix + config.FileFingerprintHeaderName.
const FP_HEADER='X-Switchboard-File-Fingerprint';
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
// the content; v makes a re-read a new URL (a media element caches by it).
function url(session,path,raw,v){return 'terminals/'+encodeURIComponent(session)+'/files'+(raw?'/raw':'')+'?path='+encodeURIComponent(path)+(v?'&v='+v:'')}
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
  case 'conflict':return '다른 곳에서 바뀜';
  case 'too-big':return '너무 큼';
  case 'read-only':return '이 노드는 저장을 지원하지 않음';
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
// kindOf is how a file's panel shows it, by extension: image, audio, video
// or text (anything else is tried as text). Mirrors the server's content-type
// allow list (hosting.FileContentType).
const KINDS={png:'image',jpg:'image',jpeg:'image',gif:'image',webp:'image',svg:'image',
  mp3:'audio',m4a:'audio',wav:'audio',ogg:'audio',oga:'audio',flac:'audio',
  mp4:'video',m4v:'video',webm:'video',mov:'video',ogv:'video'};
function kindOf(path){
  const b=base(path),i=b.lastIndexOf('.');
  if(i<0)return 'text';
  return KINDS[b.slice(i+1).toLowerCase()]||'text';
}
// modes is the editor's syntax colouring by extension (CodeMirror modes
// loaded from assets/codemirror); any other file is plain text.
const modes={go:'go',mod:'go',js:'javascript',mjs:'javascript',cjs:'javascript',jsx:'javascript',ts:'text/typescript',tsx:'text/typescript',
  json:'application/json',jsonl:'application/json',py:'python',md:'markdown',markdown:'markdown',yaml:'yaml',yml:'yaml',
  sh:'shell',bash:'shell',zsh:'shell'};
// modeOf is the editor mode of path, null for plain text. A dot file
// (.bashrc) is shell; Dockerfile-like names stay plain.
function modeOf(path){
  const b=base(path).toLowerCase();
  if(/^\.(bash|zsh)(rc|_profile|_aliases)$|^\.profile$/.test(b))return 'shell';
  const i=b.lastIndexOf('.');
  if(i<=0)return null;
  return modes[b.slice(i+1)]||null;
}
// size is a byte count for a row.
function size(n){
  if(n<1024)return n+' B';
  if(n<1<<20)return (n/1024).toFixed(n<10240?1:0)+' KB';
  if(n<1<<30)return (n/(1<<20)).toFixed(n<10<<20?1:0)+' MB';
  return (n/(1<<30)).toFixed(1)+' GB';
}
const api={TEXT_MAX:TEXT_MAX,FP_HEADER:FP_HEADER,modeOf:modeOf,pid:pid,fileOf:fileOf,join:join,base:base,url:url,problem:problem,textOf:textOf,kindOf:kindOf,size:size};
if(typeof module==='object'&&module.exports)module.exports=api;else root.wsFiles=api;
})(this);
