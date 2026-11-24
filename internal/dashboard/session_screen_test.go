package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// #519: a session card opens the session's screen (a hosted one's terminal
// panel, any other one's page), the address points at it, the right column
// shows on the session screen only and 기계
// counts the machines of the online sessions.
func TestDashboardSessionScreen(t *testing.T) {
	w := httptest.NewRecorder()
	Page(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	for _, want := range []string{
		`<section id="sessScreen" class="ss" aria-label="세션 화면" hidden></section>`,
		// The card goes through the address; the route opens the screen.
		"if(location.hash===h)frameSession(r.role);else location.hash=h.slice(1);",
		"if(h.startsWith('session=')){",
		// A page session draws the session detail's views one under another.
		"await Promise.all([details.sessions.draw(it,'status',status),",
		"sec('상태',status);\n  sec('답을 기다림',frameSessNeeds(role));\n  sec('메일',mail);\n  sec('작업',jobs);",
		"else{frameScreen('sess');frameSessRole=role;frameSessDraw();if(ws)ws.followRole(role)}",
		// A menu on screen hides the right column.
		"frameSplit(listTab(name));\n  // A menu is on screen (#519): no session screen, no right column.\n  frameScreen('menu');",
		".ws-on.fr-menu .ws-side,.fr-menu .fr-grip-r,.fr-menu .ws-sidebtn{display:none}",
		".ws-on.fr-menu .sc-app-main:not(.has-detail){grid-template-columns:minmax(0,1fr)}",
		".fr-sess .sc-app-page>:not(#sessScreen){display:none}",
		// 기계: the online sessions' machines, named on hover.
		"const machines=[...new Set(d.sessions.filter(s=>s.online&&s.machine).map(s=>s.machine))].sort();",
		"$('stCount').title=machines.length?'기계 — '+machines.join(', '):'붙은 기계 없음';",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, gone := range []string{
		"else openDetail('sessions',r.role);",
		"n.connected).length",
		".lp-main{flex:1 1 auto;min-height:0;overflow-y:auto",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("page still has %q", gone)
		}
	}
	src := workspaceSource(t)
	for _, want := range []string{
		"followRole(role){if(role===pageRole)return;pageRole=role;drawSess()},",
		"const ROLE_SUBS=['mail','needs'];",
		"if(S.sessionOf(g.active))frameToSession();",
		"if(S.sessionOf(p))frameToSession();",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("workspace missing %q", want)
		}
	}
}
