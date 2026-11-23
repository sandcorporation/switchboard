// Unit tests for layout.js (the workspace's panel layout). Run: node --test layout.test.cjs
const test=require('node:test');
const assert=require('node:assert');
const L=require('./layout.js');

const sum=a=>a.reduce((x,y)=>x+y,0);
const close=(a,b)=>Math.abs(a-b)<1e-9;

test('initial holds one group with the panel',()=>{
  const s=L.initial('menu:progress');
  assert.deepStrictEqual(L.panels(s),['menu:progress']);
  assert.strictEqual(s.focus,'menu:progress');
  assert.deepStrictEqual(L.panels(L.initial()),[]);
});

test('open adds to the focused group and an open panel is only shown',()=>{
  let s=L.initial('menu:progress');
  s=L.open(s,'menu:mail');
  assert.deepStrictEqual(L.groups(s)[0].tabs,['menu:progress','menu:mail']);
  assert.strictEqual(L.groups(s)[0].active,'menu:mail');
  const again=L.open(s,'menu:progress');
  assert.deepStrictEqual(L.panels(again),['menu:progress','menu:mail']);
  assert.strictEqual(again.focus,'menu:progress');
  assert.strictEqual(L.groups(again)[0].active,'menu:progress');
  assert.deepStrictEqual(L.panels(s),['menu:progress','menu:mail'],'the argument is left alone');
  assert.strictEqual(L.groups(s)[0].active,'menu:mail');
});

test('openAt splits the group for a new panel',()=>{
  let s=L.initial('menu:terminals');
  s=L.openAt(s,'term:a','g1','right');
  assert.strictEqual(s.root.dir,'row');
  assert.deepStrictEqual(s.root.kids.map(k=>k.tabs),[['menu:terminals'],['term:a']]);
  assert.strictEqual(s.focus,'term:a');
  const same=L.openAt(s,'term:a','g1','left');
  assert.deepStrictEqual(same.root.kids.map(k=>k.tabs),[['menu:terminals'],['term:a']]);
});

test('move to an edge splits, to the centre joins, and empty groups go',()=>{
  let s=L.initial('a');s=L.open(s,'b');s=L.open(s,'c');
  s=L.move(s,'c','g1','bottom');
  assert.strictEqual(s.root.dir,'col');
  assert.deepStrictEqual(s.root.kids.map(k=>k.tabs),[['a','b'],['c']]);
  const gc=L.groupOf(s,'c').g;
  s=L.move(s,'b',gc,'center');
  assert.deepStrictEqual(s.root.kids.map(k=>k.tabs),[['a'],['c','b']]);
  assert.strictEqual(L.groupOf(s,'b').active,'b');
  s=L.move(s,'a',gc,'center');
  assert.ok(s.root.g,'the split folds into its only group');
  assert.deepStrictEqual(s.root.tabs,['c','b','a']);
});

test('a group alone cannot move into itself',()=>{
  let s=L.initial('a');s=L.openAt(s,'b','g1','right');
  const gb=L.groupOf(s,'b').g;
  assert.strictEqual(L.move(s,'b',gb,'left'),s);
  assert.strictEqual(L.move(s,'b',gb,'center'),s);
  assert.strictEqual(L.move(s,'b',gb,'middle'),s,'unknown zone');
  assert.strictEqual(L.move(s,'zz','g1','left'),s,'unknown panel');
});

test('splits in the same direction merge and keep their sizes',()=>{
  let s=L.initial('a');
  s=L.openAt(s,'b','g1','right');
  const gb=L.groupOf(s,'b').g;
  s=L.openAt(s,'c',gb,'right');
  assert.strictEqual(s.root.dir,'row');
  assert.strictEqual(s.root.kids.length,3);
  assert.ok(close(sum(s.root.sizes),1));
  assert.ok(close(s.root.sizes[0],0.5)&&close(s.root.sizes[1],0.25)&&close(s.root.sizes[2],0.25));
  s=L.openAt(s,'d',gb,'bottom');
  assert.strictEqual(s.root.kids[1].dir,'col');
  s=L.close(s,'d');
  assert.strictEqual(s.root.kids.length,3);
  assert.ok(close(sum(s.root.sizes),1));
});

test('close picks the next tab and moves the focus',()=>{
  let s=L.initial('a');s=L.open(s,'b');s=L.open(s,'c');
  s=L.open(s,'b');
  s=L.close(s,'b');
  assert.deepStrictEqual(L.panels(s),['a','c']);
  assert.strictEqual(L.groups(s)[0].active,'c');
  assert.strictEqual(s.focus,'c');
  s=L.openAt(s,'d','g1','right');
  s=L.close(s,'d');
  assert.ok(s.root.g);
  assert.ok(['a','c'].includes(s.focus));
  s=L.close(L.close(s,'a'),'c');
  assert.deepStrictEqual(L.panels(s),[]);
  assert.ok(s.root.g,'the last group stays, empty');
  assert.strictEqual(s.focus,null);
});

test('resize keeps both sides at least MIN',()=>{
  let s=L.initial('a');s=L.openAt(s,'b','g1','right');
  const r=L.resize(s,[],0,0.2);
  assert.ok(close(r.root.sizes[0],0.7)&&close(r.root.sizes[1],0.3));
  const far=L.resize(s,[],0,5);
  assert.ok(close(far.root.sizes[1],L.MIN));
  assert.strictEqual(L.resize(s,[],1,0.1),s,'no border after the last child');
  assert.strictEqual(L.resize(s,[7],0,0.1),s,'no such split');
});

test('max follows the panel and goes with it',()=>{
  let s=L.initial('a');s=L.open(s,'b');
  s=L.setMax(s,'b');assert.strictEqual(s.max,'b');
  assert.strictEqual(L.setMax(s,'zz').max,null);
  s=L.close(s,'b');assert.strictEqual(s.max,null);
});

test('dropZone: middle joins, outer quarter splits that side',()=>{
  assert.strictEqual(L.dropZone(400,200,200,100),'center');
  assert.strictEqual(L.dropZone(400,200,10,100),'left');
  assert.strictEqual(L.dropZone(400,200,395,100),'right');
  assert.strictEqual(L.dropZone(400,200,200,10),'top');
  assert.strictEqual(L.dropZone(400,200,200,195),'bottom');
  assert.strictEqual(L.dropZone(0,0,0,0),'center');
});

test('serialize and parse round-trip',()=>{
  let s=L.initial('a');s=L.openAt(s,'b','g1','right');s=L.open(s,'c');s=L.setMax(s,'c');
  const back=L.parse(L.serialize(s));
  assert.deepStrictEqual(back,JSON.parse(L.serialize(s)));
  const t=L.openAt(back,'d','g1','top');
  assert.strictEqual(new Set(L.groups(t).map(g=>g.g)).size,L.groups(t).length,'new group ids stay unique');
});

test('parse: a layout saved with menu tabs keeps the session panels (#523)',()=>{
  const text=JSON.stringify({root:{dir:'row',kids:[{g:'g1',tabs:['menu:progress','term:a','menu:sessions'],active:'menu:progress'},{g:'g2',tabs:['menu:terminals','term:b']}],sizes:[1,1]},focus:'menu:terminals',max:null,seq:2});
  const s=L.parse(text,p=>!p.startsWith('menu:'));
  assert.deepStrictEqual(L.panels(s),['term:a','term:b']);
  assert.ok(!JSON.stringify(s).includes('menu:'));
});

test('parse: malformed gives null, gone or repeated panels drop out',()=>{
  for(const bad of ['', 'x', 'null', '[]', '{"root":{"dir":"row","kids":[],"sizes":[]}}',
    '{"root":{"dir":"diag","kids":[{"g":"g1","tabs":[]}],"sizes":[1]}}',
    '{"root":{"dir":"row","kids":[{"g":"g1","tabs":[]}],"sizes":[-1]}}',
    '{"root":{"g":"g1","tabs":"a"}}'])
    assert.strictEqual(L.parse(bad),null,bad);
  const text=JSON.stringify({root:{dir:'row',kids:[{g:'g1',tabs:['menu:mail','term:x','menu:gone'],active:'menu:gone'},{g:'g4',tabs:['term:x']}],sizes:[3,1]},focus:'menu:gone',max:'term:x',seq:2});
  const s=L.parse(text,p=>p!=='menu:gone');
  assert.ok(s.root.g,'the emptied group goes and the split folds');
  assert.deepStrictEqual(s.root.tabs,['menu:mail','term:x']);
  assert.strictEqual(s.root.active,'menu:mail');
  assert.strictEqual(s.focus,'menu:mail');
  assert.strictEqual(s.max,'term:x');
  assert.ok(s.seq>=4);
});
