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
    loadRequests() {}, loadSessions() {}, loadReports() {}, loadJobs() {}, initMail() {}, loadRules() {},
  };
  vm.createContext(ctx);
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'page', 'tabs.js'), 'utf8') + '\n;this.frameFull=frameFull;this.setListKept=setListKept;this.pageTab=pageTab;', ctx);
  return { ctx, cls, page };
}

test('the 설정 list stays for 설정 and its 화면 tabs only (#536)', () => {
  const { ctx } = load('');
  for (const n of ['settings', 'progress', 'sessions', 'terminals']) assert.equal(ctx.setListKept(n), true, n);
  for (const n of ['requests', 'questions', 'mail', 'reports', 'jobs', 'metrics', 'rules']) assert.equal(ctx.setListKept(n), false, n);
});

test('진행·세션 표·터미널 open beside the list, 지표 stays full (#536)', () => {
  for (const n of ['progress', 'sessions', 'terminals']) {
    const { ctx, cls } = load(n);
    ctx.frameFull(true);
    assert.ok(cls.has('fr-full') && cls.has('fr-setv'), n);
    ctx.frameFull(false);
    assert.ok(!cls.has('fr-full') && !cls.has('fr-setv'), n + ' off');
  }
  const { ctx, cls } = load('metrics');
  ctx.frameFull(true);
  assert.ok(cls.has('fr-full') && !cls.has('fr-setv'));
});

test('the 화면 tabs stay page tabs, so the workspace never takes them (#523)', () => {
  const { ctx } = load('');
  for (const n of ['progress', 'sessions', 'terminals', 'settings']) assert.equal(ctx.pageTab(n), true, n);
});
