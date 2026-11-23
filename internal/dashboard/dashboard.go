// Package dashboard is the admin web page (one HTML document with its CSS and
// JS, put together from the files under page/).
package dashboard

import (
	"embed"
	"io"
	"net/http"
	"strings"

	"github.com/sandcorporation/switchboard/internal/config"
)

// Page serves the admin overview. It only calls same-origin /api/admin/*
// endpoints and sends no Authorization header. It stores nothing in the
// browser, except that with hosting on the workspace keeps its panel layout
// in localStorage.
// Its only writes are question answers, permission answers, question
// settings, role revokes and restores, the Terminals tab's record writes and
// terminal socket tickets, sent with the admin write guard headers. Message
// and question fields are rendered as text nodes, never as HTML; terminal
// output reaches the page only through the vendored terminal renderer, whose
// files the page loads only when the admin bootstrap lists them (hosting on).
func Page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, pageHTML)
}

// headerText is the only place the dashboard header lives; the product name
// is config.ProductName.
const (
	productName = config.ProductName
	headerText  = productName
	// productMark is the letter in the side menu's square mark.
	productMark = "S"
)

// pageHTML is what Page serves: dashboardHTML with the design system
// stylesheet (assets/sandcorp.css, see DesignCSS) inlined ahead of the page's
// own <style>, so the page loads nothing besides itself.
var pageHTML = strings.Replace(strings.Replace(dashboardHTML, "<style>", "<style>\n"+DesignCSS()+"</style>\n<style>", 1), "<script>\n", "<script>\n"+markdownJS, 1)

// markdownJS is the report body renderer (mdRender), inlined ahead of the
// page's script; markdown.test.cjs holds its unit tests.
//
//go:embed markdown.js
var markdownJS string

// dashboardHTML is the page, put together from page/ (see buildPage), without the design system stylesheet. Colours,
// spacing and radius come from the design tokens (--color-*, --space-*,
// --radius). The status colours (--st-idle, --st-busy, --st-compacting,
// --st-error, --st-blocked, --st-offline and their -bg) are mapped onto those
// tokens in the one :root block below and shared by session text, rows,
// progress badges, question pills and the menu's waiting dot.
var dashboardHTML = buildPage()

// pageFS holds the page's parts (#412): head.html, page.css and body.html
// (the markup), then the script as one file per tab (tab_<id>.js) and the
// shared ones (core.js: api() and the helpers; detail.js: the detail panel;
// tabs.js: the tab registry and switching; search.js; live.js: the change
// bus and the first load).
//
//go:embed page
var pageFS embed.FS

// scriptParts is the script's order. The parts share one scope, so a
// top-level name a later part uses must be declared by an earlier one or sit
// in a function that runs after the whole script has loaded. A new tab is a
// new file added here.
var scriptParts = []string{
	"core.js", "detail.js", "tab_sessions.js", "tab_mail.js", "tabs.js", "tab_requests.js",
	"tab_metrics.js", "search.js", "tab_progress.js", "tab_questions.js",
	"tab_reports.js", "tab_jobs.js", "tab_rules.js", "tab_terminals.js",
	"tab_settings.js", "tab_permissions.js", "live.js",
}

// buildPage joins the parts: the markup, then the script parts of scriptParts
// between "<script>" and the closing tags.
func pagePart(name string) string {
	b, err := pageFS.ReadFile("page/" + name)
	if err != nil {
		panic("dashboard: " + err.Error())
	}
	// A Windows checkout may turn line ends into CRLF; Go drops them from a
	// raw string, so the parts must too.
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func buildPage() string {
	var sb strings.Builder
	sb.WriteString(pagePart("head.html"))
	sb.WriteString(pagePart("page.css"))
	sb.WriteString(pagePart("body.html"))
	sb.WriteString("<script>\n")
	for _, n := range scriptParts {
		sb.WriteString(pagePart(n))
	}
	sb.WriteString("</script>\n</body>\n</html>\n")
	return strings.NewReplacer("{{name}}", productName, "{{mark}}", productMark, "{{header}}", headerText).Replace(sb.String())
}
