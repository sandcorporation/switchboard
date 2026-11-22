// Unit tests for side.js (the workspace's right column). Run: node --test side.test.cjs
const test=require('node:test');
const assert=require('node:assert');
const S=require('./side.js');

test('the column follows a terminal panel and keeps its session on any other',()=>{
  assert.strictEqual(S.follow('','term:a1'),'a1');
  assert.strictEqual(S.follow('a1','term:b2'),'b2');
  assert.strictEqual(S.follow('b2','menu:mail'),'b2');
  assert.strictEqual(S.follow('b2','ask:q1'),'b2');
  assert.strictEqual(S.follow('b2',null),'b2');
  assert.strictEqual(S.follow('','menu:progress'),'');
  assert.strictEqual(S.follow('a1','term:'),'a1');
});

test('itemOf names the detail spec of an item panel',()=>{
  assert.deepStrictEqual(S.itemOf('ask:q-1'),{tab:'questions',id:'q-1'});
  assert.deepStrictEqual(S.itemOf('perm:p:2'),{tab:'permissions',id:'p:2'});
  for(const p of ['term:a','menu:questions','ask:','perm','x:1','',null])assert.strictEqual(S.itemOf(p),null,String(p));
});

test('needs lists pending questions and permission requests, oldest first',()=>{
  const qs=[
    {id:'q2',status:'pending',asker:'team/worker',created_at:'2026-01-02T00:00:03Z',questions:[{header:'판'},{header:'이름'}]},
    {id:'q3',status:'answered',asker:'team/worker',created_at:'2026-01-02T00:00:00Z',questions:[{header:'끝남'}]},
  ];
  const ps=[{id:'p1',status:'pending',tool:'Bash',summary:'make build',requester:'team/boss',created_at:'2026-01-02T00:00:01Z'}];
  assert.deepStrictEqual(S.needs(qs,ps),[
    {pid:'perm:p1',kind:'perm',title:'Bash · make build',who:'team/boss',at:'2026-01-02T00:00:01Z'},
    {pid:'ask:q2',kind:'ask',title:'판 · 이름',who:'team/worker',at:'2026-01-02T00:00:03Z'},
  ]);
  assert.deepStrictEqual(S.needs(null,undefined),[]);
});

test('a stored state that does not read back gives the initial one',()=>{
  const s={session:'a1',sub:'autos'};
  assert.deepStrictEqual(S.parse(S.serialize(s)),s);
  assert.deepStrictEqual(S.parse('{"session":"a1","sub":"screen"}'),{session:'a1',sub:'log'});
  for(const t of ['','{','null','[]','{"session":3}'])assert.deepStrictEqual(S.parse(t),S.initial(),t);
});
