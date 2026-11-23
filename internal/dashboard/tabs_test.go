package dashboard

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPageTabs runs page/tabs.js's unit tests (tabs.test.cjs) under node,
// skipped without node unless SWITCHBOARD_TEST_NODE=require.
func TestPageTabs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("SWITCHBOARD_TEST_NODE") == "require" {
			t.Fatal("node not found and SWITCHBOARD_TEST_NODE=require")
		}
		t.Skip("node not installed")
	}
	if out, err := exec.Command(node, "--test", "tabs.test.cjs").CombinedOutput(); err != nil {
		t.Fatalf("node --test tabs.test.cjs: %v\n%s", err, out)
	}
}

// #536: only the session list scrolls in the left pane (검색, the menu and
// the 설정 row stay put, in the drawer too), and the 설정 list stays on
// screen with its scroll and its marked row while 진행·세션 표·터미널 open
// beside it and an item is picked.
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
		// 진행·세션 표·터미널 beside the list; below 720px the body alone.
		"html.fr-setv.fr-full .sc-app-main:not(.detail-wide)>.sc-app-page:not([hidden]){display:grid;grid-template-columns:var(--ls-w,300px) minmax(0,1fr);",
		"html.fr-setv .sc-app-page>:is(#panel-progress,#panel-sessions,#panel-terminals){grid-column:2;grid-row:1;min-width:0;min-height:0;overflow:auto;",
		// The list keeps its scroll under the body (not display:none).
		"html.fr-setv .sc-app-page>#panel-settings{grid-column:1;visibility:hidden}",
		"html.classList.toggle('fr-setv',on&&setTabs.includes(curTab));",
		// The list panel stays shown for 설정 and its 화면 tabs.
		"$('panel-'+n).hidden=ws&&!pageTab(n)?false:!(on||n==='settings'&&setListKept(name));",
		"if(setListKept(name)){loadTab('settings');markSetViews()}",
		"if(setListKept(curTab))$('panel-settings').hidden=true",
		// A pick keeps the scroll: only an out-of-sight row comes into view.
		"showSettingsView(setKindView[id.slice(0,id.indexOf(':'))],id);",
		"box.scrollTop=top;markSetViews();",
		"if(h!=='settings'||!setListKept(curTab))showSettingsView(settingsViewOfHash(h));showTab('settings');return}",
		"if(row){if(row.scrollIntoView)row.scrollIntoView({block:'nearest'})}",
		"r.dataset.setView=x[0];",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, gone := range []string{
		"overflow-x:hidden;overflow-y:auto}\n.lp-find",
		".lp-main{flex:1 0 auto;",
		"showSettingsView(setKindView[id.slice(0,id.indexOf(':'))]);",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("page still has %q", gone)
		}
	}
}
