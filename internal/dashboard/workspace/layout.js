// wsLayout is the host-mode workspace's panel layout (#444): pure functions
// over a plain JSON state, so layout.test.cjs runs them under node.
//
// A state is {root, focus, max, seq}. A node is either a group
// {g, tabs:[pid...], active} (a tab stack) or a split
// {dir:'row'|'col', kids:[node...], sizes:[fraction...]} whose sizes sum to 1.
// A panel id (pid) appears once in the whole tree. focus is the panel last
// used, max the panel laid over the whole centre (넓히기), seq numbers new
// groups. Every function returns a new state and leaves its argument alone.
(function(root){
'use strict';
const MIN=0.1;
const ZONES=['center','left','right','top','bottom'];
function clone(s){return JSON.parse(JSON.stringify(s))}
function isGroup(n){return !!n&&typeof n==='object'&&typeof n.g==='string'}
function initial(pid){return {root:{g:'g1',tabs:pid?[pid]:[],active:pid||null},focus:pid||null,max:null,seq:1}}
function walk(n,f,path){path=path||[];f(n,path);if(!isGroup(n))n.kids.forEach((k,i)=>walk(k,f,path.concat(i)))}
function groups(s){const out=[];walk(s.root,n=>{if(isGroup(n))out.push(n)});return out}
// panels lists every panel in tree order (the 600px view shows them as tabs).
function panels(s){const out=[];for(const g of groups(s))out.push(...g.tabs);return out}
function groupOf(s,pid){return groups(s).find(g=>g.tabs.includes(pid))||null}
function groupById(s,gid){return groups(s).find(g=>g.g===gid)||null}
function nodeAt(s,path){let n=s.root;for(const i of path)n=n.kids[i];return n}
function newGroup(s,pid){s.seq=(s.seq||1)+1;return {g:'g'+s.seq,tabs:[pid],active:pid}}
function takeOut(g,pid){
  const i=g.tabs.indexOf(pid);if(i<0)return;
  g.tabs.splice(i,1);
  if(g.active===pid)g.active=g.tabs[Math.min(i,g.tabs.length-1)]||null;
}
function norm(n,keep){
  if(isGroup(n)){
    if(!n.tabs.includes(n.active))n.active=n.tabs[0]||null;
    return n.tabs.length||keep?n:null;
  }
  const kids=[],sizes=[];
  n.kids.forEach((k,i)=>{
    const m=norm(k,false),sz=n.sizes[i];
    if(!m)return;
    if(!isGroup(m)&&m.dir===n.dir)m.kids.forEach((kk,j)=>{kids.push(kk);sizes.push(sz*m.sizes[j])});
    else{kids.push(m);sizes.push(sz)}
  });
  if(!kids.length)return null;
  if(kids.length===1)return kids[0];
  const t=sizes.reduce((a,b)=>a+b,0);
  return {dir:n.dir,kids:kids,sizes:sizes.map(x=>x/t)};
}
// normalize drops empty groups (the last one stays), lifts a split's only
// child and merges a split into a parent of the same direction.
function normalize(s){
  s.root=norm(s.root,isGroup(s.root));
  if(!s.root){s.seq=(s.seq||1)+1;s.root={g:'g'+s.seq,tabs:[],active:null}}
  const all=panels(s);
  if(!all.includes(s.focus))s.focus=(groups(s).find(g=>g.active)||{}).active||null;
  if(!all.includes(s.max))s.max=null;
}
function replaceNode(s,old,neu){
  if(s.root===old){s.root=neu;return}
  let parent=null;
  walk(s.root,n=>{if(!isGroup(n)&&n.kids.includes(old))parent=n});
  if(parent)parent.kids[parent.kids.indexOf(old)]=neu;
}
// put adds pid (already out of the tree) to group dst: as a tab (center) or
// as a new group on one edge of it.
function put(s,pid,dst,zone){
  if(zone==='center'){dst.tabs.push(pid);dst.active=pid;return}
  const ng=newGroup(s,pid),dir=zone==='left'||zone==='right'?'row':'col',before=zone==='left'||zone==='top';
  replaceNode(s,dst,{dir:dir,kids:before?[ng,dst]:[dst,ng],sizes:[0.5,0.5]});
}
// open shows pid: an open panel becomes its group's tab and the focus; a new
// one goes into group gid, else the focused panel's group, else the first.
function open(s0,pid,gid){
  const s=clone(s0);
  let g=groupOf(s,pid);
  if(!g){g=(gid&&groupById(s,gid))||(s.focus&&groupOf(s,s.focus))||groups(s)[0];g.tabs.push(pid)}
  g.active=pid;s.focus=pid;
  return s;
}
// openAt is open for a new panel placed on a zone of group gid; an open
// panel is only shown.
function openAt(s0,pid,gid,zone){
  if(groupOf(s0,pid)||!ZONES.includes(zone))return open(s0,pid,gid);
  const s=clone(s0),dst=groupById(s,gid);
  if(!dst)return open(s0,pid);
  put(s,pid,dst,zone);s.focus=pid;normalize(s);
  return s;
}
function close(s0,pid){
  const s=clone(s0),g=groupOf(s,pid);
  if(!g)return s;
  takeOut(g,pid);
  const next=g.active;
  normalize(s);
  if(s.focus===pid||!panels(s).includes(s.focus))s.focus=next||(groups(s).find(x=>x.active)||{}).active||null;
  return s;
}
// move takes an open panel to group gid: center joins its tabs, an edge
// splits that group. Moving a group's only tab to its own edge does nothing.
function move(s0,pid,gid,zone){
  if(!ZONES.includes(zone))return s0;
  const s=clone(s0),src=groupOf(s,pid),dst=groupById(s,gid);
  if(!src||!dst)return s0;
  if(src===dst&&(zone==='center'||src.tabs.length===1))return s0;
  takeOut(src,pid);
  put(s,pid,dst,zone);
  s.focus=pid;normalize(s);
  return s;
}
// resize moves the border after child i of the split at path by d (a
// fraction of the split); neither side goes under MIN.
function resize(s0,path,i,d){
  const s=clone(s0);let n;
  try{n=nodeAt(s,path)}catch(e){return s0}
  if(!n||isGroup(n)||i<0||i+1>=n.kids.length)return s0;
  const a=n.sizes[i],b=n.sizes[i+1];
  d=Math.max(MIN-a,Math.min(b-MIN,d));
  n.sizes[i]=a+d;n.sizes[i+1]=b-d;
  return s;
}
function setMax(s0,pid){const s=clone(s0);s.max=pid&&groupOf(s,pid)?pid:null;return s}
// dropZone is where a tab dropped at (x,y) in a w×h group body lands: the
// middle half joins the tabs, the outer quarter on a side splits that side.
function dropZone(w,h,x,y){
  const l=x/w,r=1-l,t=y/h,b=1-t,m=Math.min(l,r,t,b);
  if(!(m<0.25))return 'center';
  return m===l?'left':m===r?'right':m===t?'top':'bottom';
}
function serialize(s){return JSON.stringify({root:s.root,focus:s.focus,max:s.max,seq:s.seq})}
// parse reads a stored state; valid(pid) drops panels that cannot open any
// more. Anything malformed gives null (the caller opens the default).
function parse(text,valid){
  let o;
  try{o=JSON.parse(text)}catch(e){return null}
  if(!o||typeof o!=='object')return null;
  const seen=new Set();let top=1;
  function chk(n,depth){
    if(!n||typeof n!=='object'||depth>32)throw Error('bad');
    if(typeof n.g==='string'){
      if(!Array.isArray(n.tabs))throw Error('bad');
      const m=/^g(\d+)$/.exec(n.g);if(m)top=Math.max(top,+m[1]);
      const tabs=n.tabs.filter(p=>typeof p==='string'&&p&&!seen.has(p)&&(!valid||valid(p)));
      for(const p of tabs)seen.add(p);
      return {g:n.g,tabs:tabs,active:typeof n.active==='string'?n.active:null};
    }
    if(n.dir!=='row'&&n.dir!=='col')throw Error('bad');
    if(!Array.isArray(n.kids)||!Array.isArray(n.sizes)||!n.kids.length||n.kids.length!==n.sizes.length)throw Error('bad');
    if(!n.sizes.every(x=>typeof x==='number'&&x>0&&isFinite(x)))throw Error('bad');
    return {dir:n.dir,kids:n.kids.map(k=>chk(k,depth+1)),sizes:n.sizes.slice()};
  }
  let s;
  try{s={root:chk(o.root,0),focus:typeof o.focus==='string'?o.focus:null,max:typeof o.max==='string'?o.max:null,seq:0}}catch(e){return null}
  s.seq=Math.max(top,Number.isInteger(o.seq)?o.seq:0);
  normalize(s);
  return s;
}
const api={MIN:MIN,initial:initial,groups:groups,panels:panels,groupOf:groupOf,groupById:groupById,nodeAt:nodeAt,open:open,openAt:openAt,close:close,move:move,resize:resize,setMax:setMax,dropZone:dropZone,serialize:serialize,parse:parse};
if(typeof module==='object'&&module.exports)module.exports=api;else root.wsLayout=api;
})(this);
