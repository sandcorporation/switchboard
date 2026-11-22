// Unit tests for nodes.js (the 노드 index's data side). Run: node --test nodes.test.cjs
const test=require('node:test');
const assert=require('node:assert');
const N=require('./nodes.js');

test('older compares versions and never flags one it cannot read',()=>{
  assert.strictEqual(N.older('v0.30.1','v0.31.0'),true);
  assert.strictEqual(N.older('0.31.0','v0.31.0'),false);
  assert.strictEqual(N.older('v0.31.2','v0.31.10'),true);
  assert.strictEqual(N.older('v1.0.0','v0.99.9'),false);
  assert.strictEqual(N.older('v0.31','v0.31.1'),true);
  assert.strictEqual(N.older('v0.31.0-3-gabc','v0.31.0'),false);
  for(const [a,b] of [['dev','v1.0.0'],['v1.0.0','dev'],['','v1.0.0'],[undefined,'v1.0.0']])
    assert.strictEqual(N.older(a,b),false,a+' vs '+b);
});

test('state reads revoked before connected',()=>{
  assert.strictEqual(N.state({connected:true}),'on');
  assert.strictEqual(N.state({connected:false}),'off');
  assert.strictEqual(N.state({connected:true,revoked_at:5}),'revoked');
  assert.strictEqual(N.stateText[N.state({})],'끊김');
});

test('sessionsOn and grantsOn pick the node',()=>{
  const recs=[{id:'1',node:'pc'},{id:'2',node:'pc2'},{id:'3',node:'pc'}];
  assert.deepStrictEqual(N.sessionsOn(recs,'pc').map(r=>r.id),['1','3']);
  assert.deepStrictEqual(N.sessionsOn(null,'pc'),[]);
  const gs=[{role:'team/a',nodes:['pc','pc2']},{role:'team/b',nodes:['pc2']},{role:'team/c'}];
  assert.deepStrictEqual(N.grantsOn(gs,'pc').map(g=>g.role),['team/a']);
});

test('grantWith adds the node to the role grant and keeps what is left empty',()=>{
  assert.deepStrictEqual(N.grantWith(null,'pc','team/a','team/worker','/work, /srv'),
    {role:'team/a',bases:['team/worker'],nodes:['pc'],dir_roots:['/work','/srv']});
  const g={role:'team/a',bases:['team/worker'],nodes:['pc2'],dir_roots:['/work'],adapters:['claude']};
  assert.deepStrictEqual(N.grantWith(g,'pc','team/a','',''),
    {role:'team/a',bases:['team/worker'],nodes:['pc2','pc'],dir_roots:['/work'],adapters:['claude']});
  assert.deepStrictEqual(N.grantWith(g,'pc2','team/a','team/boss','/x').nodes,['pc2']);
  assert.deepStrictEqual(g.nodes,['pc2'],'the grant given is left alone');
});

test('grantWithout deletes a one-node grant and narrows a wider one',()=>{
  assert.deepStrictEqual(N.grantWithout({role:'team/a',nodes:['pc']},'pc'),{method:'DELETE',path:'host-grants?role=team%2Fa'});
  const w=N.grantWithout({role:'team/a',bases:['b'],nodes:['pc','pc2'],dir_roots:['/w'],adapters:['x']},'pc');
  assert.deepStrictEqual(w,{method:'POST',path:'host-grants',body:{role:'team/a',bases:['b'],nodes:['pc2'],dir_roots:['/w'],adapters:['x']}});
});

test('list splits on commas and spaces without repeats',()=>{
  assert.deepStrictEqual(N.list(' a, b  a,,c '),['a','b','c']);
  assert.deepStrictEqual(N.list(''),[]);
});

test('usageText and actionName',()=>{
  assert.strictEqual(N.usageText({adapter:'claude',remaining:42}),'claude 42%');
  assert.strictEqual(N.usageText({adapter:'codex',account:'work'}),'codex · work -');
  assert.strictEqual(N.usageText({adapter:'claude',remaining:0}),'claude 0%');
  assert.strictEqual(N.actionName('node.invite'),'초대');
  assert.strictEqual(N.actionName('other'),'other');
});
