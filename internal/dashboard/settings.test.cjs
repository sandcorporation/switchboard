// Unit tests for page/tab_settings.js (#538): node --test internal/dashboard/settings.test.cjs
// (tabs_test.go runs them under go test when node is installed).
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// load runs tabs.js and tab_settings.js in a fresh context with stand-ins
// for the page; hosting and ws decide the menu's 기계·묶음·터미널·노드.
function load(o) {
  o = o || {};
  const el = () => ({ addEventListener() {}, classList: { toggle() {}, add() {} }, setAttribute() {}, append() {}, replaceChildren() {}, focus() {}, value: '', textContent: '' });
  const ctx = {
    hostingOn: !!o.hosting, ws: o.ws || null, detailCur: null, curTab: '', location: { hash: o.hash || '' },
    perms: o.perms || ['view', 'sessions'],
    $: el, node: el, addEventListener() {}, registerDetail() {},
    document: { addEventListener() {}, documentElement: { classList: { toggle() {}, contains: () => false } } },
    detailHash: (tab, id, sub) => '#' + tab + '/' + encodeURIComponent(id) + (sub ? '/' + sub : ''),
    loadRequests() {}, loadSessions() {}, loadReports() {}, loadJobs() {}, initMail() {}, loadRules() {},
  };
  ctx.can = p => ctx.perms.includes(p);
  vm.createContext(ctx);
  const src = ['tabs.js', 'tab_settings.js'].map(f => fs.readFileSync(path.join(__dirname, 'page', f), 'utf8')).join('\n');
  vm.runInContext(src + '\n;Object.assign(this,{setPathOf,setModeHash,setSel,setScreens,setCats,setCatOn,setAddOk,setPaneNow:()=>setPane});', ctx);
  return ctx;
}
const plain = v => JSON.parse(JSON.stringify(v));
const texts = segs => segs.map(s => s[0]).join(' › ');

test('the menu groups are 일반 · 연결 · 역할 with the categories in order', () => {
  const ctx = load({ hosting: true });
  const by = {};
  for (const c of ctx.setCats) (by[c.grp] = by[c.grp] || []).push(c.name);
  assert.deepEqual(JSON.parse(JSON.stringify(by)), { 일반: ['로그인', '질문 설정', '다시 알림'], 연결: ['기계', '묶음', '웹훅', '지켜보기'], 역할: ['역할'] });
});

test('기계·묶음·터미널 only with hosting on, 노드 only with the workspace', () => {
  const off = load();
  assert.deepEqual(plain(off.setCats.filter(c => !off.setCatOn(c)).map(c => c.v)), ['nodes', 'groups']);
  assert.deepEqual(plain(off.setScreens().map(s => s[1])), ['진행', '세션 표']);
  const on = load({ hosting: true, ws: { nodes() {} } });
  assert.equal(on.setCats.filter(c => !on.setCatOn(c)).length, 0);
  assert.deepEqual(plain(on.setScreens().map(s => s[1])), ['진행', '세션 표', '터미널', '노드']);
});

test('+ opens a blank form for 질문 설정 · 묶음 · 웹훅 · 지켜보기; 지켜보기 needs sessions; no + for 역할 (#140)', () => {
  const ctx = load({ hosting: true });
  assert.deepEqual(plain(ctx.setCats.filter(c => ctx.setAddOk(c)).map(c => c.add)), ['qs:+', 'group:+', 'hook:+', 'watch:+']);
  const ro = load({ hosting: true, perms: ['view'] });
  assert.deepEqual(plain(ro.setCats.filter(c => ro.setAddOk(c)).map(c => c.add)), ['qs:+', 'group:+', 'hook:+']);
});

test('the path row: 설정 › 분류 › 항목, the category a link to its list', () => {
  const ctx = load({ hosting: true });
  assert.deepEqual(JSON.parse(JSON.stringify(ctx.setPathOf({ tab: 'settings', id: 'hook:3' }, 'settings', 'hooks', 'build-alert'))), [['설정', ''], ['웹훅', '#settings-hooks'], ['build-alert', '']]);
  assert.equal(texts(ctx.setPathOf({ tab: 'settings', id: 'role:team/boss' }, 'settings', 'roles', 'team/boss')), '설정 › 역할 › team/boss');
  assert.equal(texts(ctx.setPathOf({ tab: 'settings', id: 'qs:+' }, 'settings', 'qsettings', '새 질문 설정')), '설정 › 질문 설정 › 새 질문 설정');
  assert.equal(texts(ctx.setPathOf({ tab: 'settings', id: 'server:now' }, 'settings', 'reminders', '다시 알림 · 서버')), '설정 › 다시 알림');
  assert.equal(texts(ctx.setPathOf(null, 'settings', 'nodes', '')), '설정 › 기계');
  assert.equal(texts(ctx.setPathOf(null, 'progress', 'qsettings', '')), '설정 › 진행');
  assert.equal(texts(ctx.setPathOf({ tab: 'terminals', id: 'hs_1' }, 'terminals', 'qsettings', 'team/worker')), '설정 › 터미널 › team/worker');
});

test('the menu marks the category of the open item, a role itself, a 화면 row', () => {
  const ctx = load({ hosting: true });
  ctx.curTab = 'settings';
  vm.runInContext("setView='hooks'", ctx);
  assert.equal(ctx.setSel(), 'cat:hooks');
  ctx.detailCur = { tab: 'settings', id: 'watch:4' };
  assert.equal(ctx.setSel(), 'cat:watches');
  ctx.detailCur = { tab: 'settings', id: 'role:team/boss' };
  assert.equal(ctx.setSel(), 'role:team/boss');
  ctx.detailCur = { tab: 'settings', id: 'view:nodes' };
  assert.equal(ctx.setSel(), 'scr:view:nodes');
  ctx.detailCur = null; ctx.curTab = 'sessions';
  assert.equal(ctx.setSel(), 'scr:sessions');
});

test('설정 addresses: #settings… and 진행·세션 표·터미널; the menu first on #settings', () => {
  const ctx = load();
  for (const h of ['#settings', '#settings-roles', '#settings/hook%3A1/edit', '#progress', '#sessions', '#terminals/hs_1/screen']) assert.equal(ctx.setModeHash(h), true, h);
  for (const h of ['', '#requests', '#mail', '#session=team/boss', '#metrics']) assert.equal(ctx.setModeHash(h), false, h);
  assert.equal(load({ hash: '#settings' }).setPaneNow(), 'menu');
  assert.equal(load({ hash: '#settings-hooks' }).setPaneNow(), 'body');
});
