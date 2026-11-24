package dashboard

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPageTabs runs page/tabs.js's and page/tab_settings.js's unit tests
// (tabs.test.cjs, settings.test.cjs) under node,
// skipped without node unless SWITCHBOARD_TEST_NODE=require.
func TestPageTabs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("SWITCHBOARD_TEST_NODE") == "require" {
			t.Fatal("node not found and SWITCHBOARD_TEST_NODE=require")
		}
		t.Skip("node not installed")
	}
	if out, err := exec.Command(node, "--test", "tabs.test.cjs", "settings.test.cjs").CombinedOutput(); err != nil {
		t.Fatalf("node --test tabs.test.cjs settings.test.cjs: %v\n%s", err, out)
	}
}

// #536: only the session list scrolls in the left pane (검색, the menu and
// the 설정 row stay put, in the drawer too). #544: 설정 sits beside the
// session list as tabs over a table; an item opens on the right and the
// table keeps its scroll and its marked row.
func TestDashboardSettingsListKept(t *testing.T) {
	w := httptest.NewRecorder()
	Page(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	for _, want := range []string{
		// The left pane: one scroller, the session list.
		".lp{flex:none;width:var(--lp-w,248px);min-width:0;display:flex;flex-direction:column;background:var(--color-paper);overflow:hidden}",
		".lp-main{flex:1 1 auto;min-height:0;display:flex;flex-direction:column}",
		".lp .tabs{flex:none;",
		".lp-sess{flex:1 1 auto;min-height:0;overflow-x:hidden;overflow-y:auto;",
		".lp-foot{flex:none;",
		// #544: 설정 sits beside the session list — the top (title, 찾기, +
		// 추가, tabs) over the picked tab's table, an item in the panel on the
		// right with the table beside it.
		"html.classList.toggle('fr-set',on&&setListKept(curTab));",
		".fr-set #appMain,.fr-set #appMain.has-detail{display:grid;grid-template-columns:minmax(0,1fr);grid-template-rows:auto minmax(0,1fr)}",
		".fr-set #appMain.has-detail:not(.st-wide){grid-template-columns:minmax(0,1fr) 380px}",
		".fr-set #appMain.st-wide>.sc-app-page{display:none}",
		".st-tabs{height:34px;display:flex;align-items:stretch;gap:2px;padding:0 10px;border-bottom:1px solid var(--color-line);overflow-x:auto;",
		".st-tbl .st-n{display:none}",
		".sd-foot{position:sticky;bottom:0;",
		`<h1 class="st-t">설정</h1>`, `id="setFind" class="st-find" type="search"`, `placeholder="설정 전체에서 찾기"`, `id="setAddBtn" class="st-add"`,
		`<nav id="setTabsRow" class="st-tabs" aria-label="설정 분류"></nav>`, `<nav id="setScr" class="st-scr" aria-label="화면" hidden></nav>`,
		`<span id="detailSub" class="detail-sub mono"></span>`,
		// Only the open panel shows; the table keeps its scroll on a redraw.
		"$('panel-'+n).hidden=ws&&!pageTab(n)?false:!on;",
		"if(setListKept(name))loadTab('settings');",
		"if(page)page.scrollTop=same?top:0;",
		"if(settingsViewOfHash(h)){showSettingsView(settingsViewOfHash(h));showTab('settings');return}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, gone := range []string{
		"overflow-x:hidden;overflow-y:auto}\n.lp-find",
		".lp-main{flex:1 0 auto;",
		// #538's settings menu and path row are gone (#544).
		`id="setNav"`, `id="setHead"`, `id="setMenu"`, `id="setBack"`, "setExit", "fr-setm", ".sn-i{",
		"fr-setv", `id="settingsList"`, "function markSetViews(", `id="setAdd"`, "listTabs=['requests','questions','mail','reports','jobs','settings']",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("page still has %q", gone)
		}
	}
}
