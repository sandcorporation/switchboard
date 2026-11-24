// Unit tests for page/tab_settings.js (#538, #544): node --test internal/dashboard/settings.test.cjs
// (tabs_test.go runs them under go test when node is installed).
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// load runs tabs.js and tab_settings.js in a fresh context with stand-ins
// for the page; hosting and ws decide the tabs 기계·묶음 and 화면's 터미널·노드.
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
  vm.runInContext(src + '\n;Object.assign(this,{setTabNow,setScrNow,setScreens,setCats,setCatOn,setAddOk,setTabHref,settingsViewOfHash,showSettingsView,setCols,setLive,setRoleRow,setRoleState});', ctx);
  return ctx;
}
const plain = v => JSON.parse(JSON.stringify(v));

test('the tabs: 역할 · 기계 · 묶음 · 로그인 · 질문 설정 · 지켜보기 · 웹훅 · 화면 · 다시 알림', () => {
  const ctx = load({ hosting: true });
  assert.deepEqual(plain(ctx.setCats.map(c => c.name)), ['역할', '기계', '묶음', '로그인', '질문 설정', '지켜보기', '웹훅', '화면', '다시 알림']);
  assert.deepEqual(plain(ctx.setCols.roles.map(c => c[0])), ['이름', '주소', '기계', '상태', '컨텍스트']);
  // Every tab but 화면 has a table.
  assert.deepEqual(plain(ctx.setCats.filter(c => !ctx.setCols[c.v]).map(c => c.v)), ['screens']);
});

test('기계·묶음 only with hosting on; 화면 has 터미널 with hosting, 노드 with the workspace', () => {
  const off = load();
  assert.deepEqual(plain(off.setCats.filter(c => !off.setCatOn(c)).map(c => c.v)), ['nodes', 'groups']);
  assert.deepEqual(plain(off.setScreens().map(s => s[1])), ['진행', '세션 표']);
  const on = load({ hosting: true, ws: { nodes() {} } });
  assert.equal(on.setCats.filter(c => !on.setCatOn(c)).length, 0);
  assert.deepEqual(plain(on.setScreens().map(s => s[1])), ['진행', '세션 표', '터미널', '노드']);
});

test('+ opens a blank form for 역할 (an invite) · 묶음 · 질문 설정 · 지켜보기 · 웹훅; 역할 and 지켜보기 need sessions', () => {
  const ctx = load({ hosting: true });
  assert.deepEqual(plain(ctx.setCats.filter(c => ctx.setAddOk(c)).map(c => c.add)), ['role:+', 'group:+', 'qs:+', 'watch:+', 'hook:+']);
  const ro = load({ hosting: true, perms: ['view'] });
  assert.deepEqual(plain(ro.setCats.filter(c => ro.setAddOk(c)).map(c => c.add)), ['group:+', 'qs:+', 'hook:+']);
});

test('tab addresses: #settings-<tab>, #settings the last tab, 화면 its first screen', () => {
  const ctx = load({ hosting: true });
  assert.equal(ctx.setTabHref('roles'), '#settings-roles');
  assert.equal(ctx.setTabHref('qsettings'), '#settings-qsettings');
  assert.equal(ctx.setTabHref('screens'), '#progress');
  assert.equal(ctx.settingsViewOfHash('settings-hooks'), 'hooks');
  assert.equal(ctx.settingsViewOfHash('settings'), 'roles');
  ctx.showSettingsView('hooks');
  assert.equal(ctx.settingsViewOfHash('settings'), 'hooks');
  assert.equal(ctx.settingsViewOfHash('settings-nope'), '');
  // 기계 without hosting falls back to 역할.
  const off = load();
  off.showSettingsView('nodes');
  assert.equal(off.settingsViewOfHash('settings'), 'roles');
});

test('the tab on screen: the open item\'s category, 화면 for its screens, else the picked tab', () => {
  const ctx = load({ hosting: true, ws: { nodes() {} } });
  ctx.curTab = 'settings';
  ctx.showSettingsView('hooks');
  assert.equal(ctx.setTabNow(), 'hooks');
  ctx.detailCur = { tab: 'settings', id: 'watch:4' };
  assert.equal(ctx.setTabNow(), 'watches');
  ctx.detailCur = { tab: 'settings', id: 'role:+' };
  assert.equal(ctx.setTabNow(), 'roles');
  ctx.detailCur = { tab: 'settings', id: 'refusals:24h' };
  assert.equal(ctx.setTabNow(), 'qsettings');
  ctx.detailCur = { tab: 'settings', id: 'view:nodes' };
  assert.equal(ctx.setTabNow(), 'screens');
  assert.equal(ctx.setScrNow(), 'view:nodes');
  ctx.detailCur = null; ctx.curTab = 'sessions';
  assert.equal(ctx.setTabNow(), 'screens');
  assert.equal(ctx.setScrNow(), 'sessions');
  ctx.detailCur = { tab: 'terminals', id: 'hs_1' }; ctx.curTab = 'terminals';
  assert.equal(ctx.setTabNow(), 'screens');
});

test('역할 줄 상태·컨텍스트: 슬롯 주소로 붙은 세션도 찾고, 여럿이면 가장 바쁜 것', () => {
  const ctx = load();
  const sess = (role, status, tok) => ({ role, online: true, status, context_tokens: tok, machine: 'm1' });
  ctx.frameData = { terms: [], questions: [], perms: [], sessions: [
    sess('team/boss-1', 'idle', 1000), sess('team/boss-2', 'busy', 2000), sess('team/boss-2-1', 'blocked:x', 3000),
    sess('team/worker', 'idle', 500), sess('team/workers-1', 'busy', 9),
  ] };
  ctx.frameRows = d => d.sessions.map(s => ({ role: s.role, st: s.online ? s.status : 'offline', ctx: s.context_tokens, machine: s.machine, label: '' }));
  ctx.sessionStatus = st => { const m = /^(blocked|error):/.exec(st); return m ? ['막힘', 'st-' + m[1]] : [{ busy: '바쁨', idle: '유휴' }[st], 'st-' + st]; };
  ctx.termRole = () => ''; ctx.kTokens = n => n + 't'; ctx.setRow = (id, cells) => ({ id, cells });
  const live = ctx.setLive();
  assert.equal(live.get('team/boss').role, 'team/boss-2');
  assert.equal(live.get('team/worker').role, 'team/worker');
  assert.equal(live.get('team/none'), null);
  const row = plain(ctx.setRoleRow({ role: 'team/boss' }, live));
  assert.deepEqual(row.cells.slice(3), [['바쁨', 'success'], '2000t']);
  // table and detail badge are the same value; ended registrations win; no session → 꺼짐
  assert.deepEqual(plain(ctx.setRoleState({ role: 'team/boss' }, live.get('team/boss'))), ['바쁨', 'success']);
  assert.deepEqual(plain(ctx.setRoleState({ role: 'team/boss', revoked_at: 'x' }, live.get('team/boss'))), ['끊김', 'danger']);
  assert.deepEqual(plain(ctx.setRoleState({ role: 'team/none' }, null)), ['꺼짐', '']);
});
