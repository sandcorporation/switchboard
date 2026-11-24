// Unit tests for page/tabs.js: node --test internal/dashboard/tabs.test.cjs
// (tabs_test.go runs them under go test when node is installed).
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// load runs tabs.js in a fresh context with a fake <html> and page body;
// curTab is the tab on screen (live.js owns it in the page).
function load(curTab, ws) {
  const cls = new Set();
  const page = { hidden: true };
  const ctx = {
    curTab, ws: ws || null,
    document: {
      documentElement: { classList: { toggle: (c, on) => { if (on) cls.add(c); else cls.delete(c); }, contains: c => cls.has(c) } },
      querySelector: () => page,
    },
    $: () => ({ hidden: true }),
    setChrome() {}, loadRequests() {}, loadSessions() {}, loadReports() {}, loadJobs() {}, initMail() {}, loadRules() {},
  };
  vm.createContext(ctx);
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'page', 'tabs.js'), 'utf8') + '\n;this.frameFull=frameFull;this.setListKept=setListKept;this.pageTab=pageTab;', ctx);
  return { ctx, cls, page };
}

test('the 설정 screen is 설정 and its 화면 tabs only (#536, #538); 진행 is a menu row (#576)', () => {
  const { ctx } = load('');
  for (const n of ['settings', 'sessions', 'terminals']) assert.equal(ctx.setListKept(n), true, n);
  for (const n of ['requests', 'progress', 'questions', 'mail', 'reports', 'jobs', 'metrics', 'rules']) assert.equal(ctx.setListKept(n), false, n);
});

test('설정 and 세션 표·터미널 are the 설정 screen, 지표 and 진행 stay full (#538, #576)', () => {
  for (const n of ['settings', 'sessions', 'terminals']) {
    const { ctx, cls } = load(n);
    ctx.frameFull(true);
    assert.ok(cls.has('fr-full') && cls.has('fr-set'), n);
    ctx.frameFull(false);
    assert.ok(!cls.has('fr-full') && !cls.has('fr-set'), n + ' off');
  }
  for (const n of ['metrics', 'progress']) {
    const { ctx, cls } = load(n);
    ctx.frameFull(true);
    assert.ok(cls.has('fr-full') && !cls.has('fr-set'), n);
  }
});

test('설정 is no list screen: no list column (#538)', () => {
  const { ctx } = load('');
  assert.equal(vm.runInContext("listTab('settings')", ctx), false);
  assert.equal(vm.runInContext("fullTabs.includes('settings')", ctx), true);
});

test('the 화면 tabs stay page tabs, so the workspace never takes them (#523)', () => {
  const { ctx } = load('');
  for (const n of ['progress', 'sessions', 'terminals', 'settings']) assert.equal(ctx.pageTab(n), true, n);
});
