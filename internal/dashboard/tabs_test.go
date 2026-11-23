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
// the 설정 row stay put, in the drawer too). #538: 설정 takes the whole
// screen — the settings menu keeps its scroll and its marked row while an
// item or 진행·세션 표·터미널 opens on the right.
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
		// #538: 설정 is the whole screen — the left pane the settings menu,
		// the centre the path row over the body; no list column.
		"html.classList.toggle('fr-set',on&&setListKept(curTab));",
		".fr-set .lp>:not(#setNav),.fr-set #gripL,.fr-set .fr-lp-open{display:none}",
		".fr-set #appMain,.fr-set #appMain.has-detail{display:grid;grid-template-columns:minmax(0,1fr);grid-template-rows:48px minmax(0,1fr)}",
		".sn-top{flex:none;height:48px;",
		".sn-h{height:28px;display:flex;align-items:center;justify-content:space-between;margin-top:12px;padding:0 8px 0 16px;font-size:11px;font-weight:600;letter-spacing:.4px;text-transform:uppercase;",
		".fr-set .detail.sd-form>*{max-width:720px}",
		// Below 720px the menu and the body take turns.
		"html.fr-set:not(.fr-setm) .lp{display:none}",
		"html.fr-set.fr-setm #appMain{display:none}",
		".fr-set .detail{position:static;inset:auto;z-index:auto}",
		// Only the open panel shows; the menu keeps its scroll and its marked row.
		"$('panel-'+n).hidden=ws&&!pageTab(n)?false:!on;",
		"if(setListKept(name))loadTab('settings');",
		"const box=$('setMenu'),top=box.scrollTop,",
		"box.scrollTop=top;\n  setMark();",
		"if(on&&setJump&&r.scrollIntoView){setJump=false;r.scrollIntoView({block:'nearest'})}",
		"if(settingsViewOfHash(h)){showSettingsView(settingsViewOfHash(h));showTab('settings');return}",
		// 돌아가기 · ✕ · Esc leave for the screen before 설정.
		`<button type="button" class="sn-back" id="setBack"`, `id="setFind" type="search"`, `placeholder="설정 찾기"`,
		`<div class="sh" id="setHead">`, `id="setMenuBack"`, `<nav id="setPath" class="sh-path" aria-label="경로"></nav>`, `id="setClose"`,
		"$('setBack').onclick=setExit;", "$('setClose').onclick=setExit;",
		"location.hash=r&&r!=='#'&&!setModeHash(r)?r.slice(1):'requests';",
		"if(setModeHash(location.hash)&&!setModeHash(old)){setReturn=old;",
		"if(matchMedia('(max-width:720px)').matches&&setPane==='body'){setPane='menu';",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, gone := range []string{
		"overflow-x:hidden;overflow-y:auto}\n.lp-find",
		".lp-main{flex:1 0 auto;",
		"fr-setv", `id="settingsList"`, "function markSetViews(", `id="setAdd"`, "listTabs=['requests','questions','mail','reports','jobs','settings']",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("page still has %q", gone)
		}
	}
}
