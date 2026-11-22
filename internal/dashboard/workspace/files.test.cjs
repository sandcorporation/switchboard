'use strict';
const test=require('node:test');
const assert=require('node:assert');
const F=require('./files.js');
const S=require('./side.js');

test('a file panel names its session and path, colons in the path included',()=>{
  assert.strictEqual(F.pid('hs_1','a/b.txt'),'file:hs_1:a/b.txt');
  assert.deepStrictEqual(F.fileOf('file:hs_1:a/b.txt'),{session:'hs_1',path:'a/b.txt'});
  assert.deepStrictEqual(F.fileOf(F.pid('hs_1','x:y')),{session:'hs_1',path:'x:y'});
  for(const p of ['term:hs_1','file:','file:hs_1','file:hs_1:',':x',null,3])assert.strictEqual(F.fileOf(p),null,String(p));
});

test('the column follows the session of a file panel too',()=>{
  assert.strictEqual(S.sessionOf('file:hs_2:a.txt'),'hs_2');
  assert.strictEqual(S.follow('hs_1','file:hs_2:a.txt'),'hs_2');
  assert.strictEqual(S.follow('hs_1','menu:mail'),'hs_1');
});

test('the 파일 view is first of four, and the default stays 기록',()=>{
  assert.deepStrictEqual(S.SUBS.map(s=>s[0]),['files','log','autos','settings']);
  assert.strictEqual(S.initial().sub,'log');
  assert.strictEqual(S.parse('{"session":"a","sub":"files"}').sub,'files');
});

test('paths and URLs',()=>{
  assert.strictEqual(F.join('','a'),'a');
  assert.strictEqual(F.join('a/b','c'),'a/b/c');
  assert.strictEqual(F.base('a/b/c.md'),'c.md');
  assert.strictEqual(F.base('c.md'),'c.md');
  assert.strictEqual(F.url('hs_1','a b/c&d.txt'),'terminals/hs_1/files?path=a%20b%2Fc%26d.txt');
  assert.strictEqual(F.url('hs_1','x',true),'terminals/hs_1/files/raw?path=x');
});

test('a failed request reads as its code',()=>{
  assert.strictEqual(F.problem(501,'{"code":"unsupported","error":"node pc does not support files"}'),'이 노드는 파일을 지원하지 않음');
  assert.strictEqual(F.problem(403,'{"code":"refused","error":"x"}'),'세션 폴더 밖');
  assert.strictEqual(F.problem(503,'{"code":"offline"}'),'노드 꺼짐');
  assert.strictEqual(F.problem(404,'404 page not found'),'이 주소에서는 파일이 꺼져 있음');
  assert.strictEqual(F.problem(500,''),'요청 실패(500)');
});

test('text: too big, a NUL, bad UTF-8, and text',()=>{
  assert.deepStrictEqual(F.textOf(F.TEXT_MAX+1,new Uint8Array(0)),{why:'너무 큼'});
  assert.deepStrictEqual(F.textOf(3,new Uint8Array([104,0,105])),{why:'글 파일 아님'});
  assert.deepStrictEqual(F.textOf(2,new Uint8Array([0xff,0xfe])),{why:'글 파일 아님'});
  assert.deepStrictEqual(F.textOf(6,new TextEncoder().encode('한 a')),{text:'한 a'});
  assert.deepStrictEqual(F.textOf(0,new Uint8Array(0)),{text:''});
});

test('sizes',()=>{
  assert.strictEqual(F.size(5),'5 B');
  assert.strictEqual(F.size(2048),'2.0 KB');
  assert.strictEqual(F.size(3<<20),'3.0 MB');
});

test('kindOf picks the panel by extension',()=>{
  assert.strictEqual(F.kindOf('a/b/pic.PNG'),'image');
  assert.strictEqual(F.kindOf('x.svg'),'image');
  assert.strictEqual(F.kindOf('x.webp'),'image');
  assert.strictEqual(F.kindOf('s.mp3'),'audio');
  assert.strictEqual(F.kindOf('d/v.webm'),'video');
  assert.strictEqual(F.kindOf('m.mp4'),'video');
  assert.strictEqual(F.kindOf('README'),'text');
  assert.strictEqual(F.kindOf('a.png/notes'),'text');
  assert.strictEqual(F.kindOf('main.go'),'text');
});
