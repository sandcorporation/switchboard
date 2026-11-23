// Package dashboard is the admin web page (one HTML document with its CSS and
// JS).
package dashboard

import (
	_ "embed"
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

// dashboardHTML is the page without the design system stylesheet. Colours,
// spacing and radius come from the design tokens (--color-*, --space-*,
// --radius). The status colours (--st-idle, --st-busy, --st-compacting,
// --st-error, --st-blocked, --st-offline and their -bg) are mapped onto those
// tokens in the one :root block below and shared by session text, rows,
// progress badges, question pills and the menu's waiting dot.
const dashboardHTML = `<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>` + productName + `</title>
<style>
:root{
--st-idle:var(--color-success);--st-idle-bg:var(--color-success-tint);
--st-busy:var(--color-accent);--st-busy-bg:var(--color-accent-tint);
--st-compacting:var(--color-cat-ai);--st-compacting-bg:var(--color-tint);
--st-error:var(--color-danger);--st-error-bg:var(--color-danger-tint);
--st-blocked:var(--color-warning);--st-blocked-bg:var(--color-warning-tint);
--st-offline:var(--color-muted);--st-offline-bg:var(--color-tint)}
h2{font-size:1.05rem;font-weight:650;margin:0}
h3{font-size:.98rem;font-weight:650}
h4{font-size:.85rem;font-weight:600;color:var(--color-muted)}
a{color:var(--color-accent)}
input,button,select{font:inherit;color:inherit}
input,select{padding:.4rem .55rem;margin:0;border:1px solid var(--color-line-strong);border-radius:var(--radius);background:var(--color-canvas);min-width:0}
input[type=checkbox],input[type=radio]{accent-color:var(--color-accent)}
button,a.button{padding:.4rem .75rem;margin:0;border:1px solid var(--color-line-strong);border-radius:var(--radius);background:var(--color-canvas);color:var(--color-text);cursor:pointer;line-height:1.4}
button:hover,a.button:hover{border-color:var(--color-accent);color:var(--color-accent)}
button[type=submit]{background:var(--color-accent);border-color:var(--color-accent);color:var(--color-on-accent)}
button[type=submit]:hover{color:var(--color-on-accent);filter:brightness(1.08)}
button:disabled{opacity:.55;cursor:default}
:focus-visible{outline:2px solid var(--color-focus);outline-offset:2px}
.row{display:flex;gap:var(--space-2);align-items:center;flex-wrap:wrap}
.card{background:var(--color-canvas);border:1px solid var(--color-line);border-radius:var(--radius);padding:var(--space-4);margin:var(--space-3) 0;overflow-wrap:anywhere}
.card h3{margin:0 0 var(--space-1)}
.card.urgent{border-left:4px solid var(--st-error)}
.badge{display:inline-block;background:var(--st-error);color:var(--color-on-accent);border-radius:var(--radius);padding:0 .4rem;margin-right:var(--space-2);font-size:.75em;font-weight:600;vertical-align:middle}
.body{margin-top:var(--space-3);border-top:1px solid var(--color-line);padding-top:var(--space-3)}
pre{white-space:pre-wrap;overflow-wrap:anywhere;font:13.5px/1.55 var(--font-mono);margin:0}
.muted{color:var(--color-muted)}
.empty{color:var(--color-muted);margin:var(--space-3) 0}
.reqline{margin:0 0 var(--space-2);overflow-wrap:anywhere}
.md{min-width:0;overflow-wrap:anywhere;line-height:1.55;margin:var(--space-3) 0}
.md p{white-space:pre-line;margin:var(--space-2) 0}
.md h3,.md h4,.md h5,.md h6{margin:var(--space-4) 0 var(--space-2)}
.md ul,.md ol{padding-left:1.4em;margin:var(--space-2) 0}
.md pre{overflow-x:auto;max-width:100%;padding:var(--space-2) var(--space-3);background:var(--color-surface-2,var(--color-line));border-radius:var(--radius);white-space:pre}
.md code{font-family:var(--font-mono,ui-monospace,monospace);font-size:.9em;overflow-wrap:anywhere}
.md pre code{overflow-wrap:normal}
.md .md-table{overflow-x:auto;max-width:100%;margin:var(--space-2) 0}
.md table{border-collapse:collapse;width:auto}
.md th,.md td{border:1px solid var(--color-line);padding:.2rem .5rem;text-align:left;white-space:nowrap}
.rthread{border-top:1px solid var(--color-line);padding:var(--space-2) 0}
.rthread ol{list-style:none;margin:var(--space-1) 0 0;padding-left:var(--space-4);border-left:2px solid var(--color-line)}
.rthread li{margin:var(--space-2) 0}
.rline{overflow-wrap:anywhere}
.rbody{margin:var(--space-1) 0 0;white-space:pre-line;overflow-wrap:anywhere;font-size:.9em}
#notice{margin:var(--space-2) 0;font-size:.9em}
#notice:empty{display:none}
.wide{overflow-x:auto}
#termScreen{overflow:auto;max-width:100%;padding:var(--space-2);background:var(--color-brand-ink);border-radius:var(--radius)}
#termPane.typing #termScreen{overflow:hidden;height:60vh;min-height:240px}
#termMode[aria-pressed="true"]{outline:2px solid var(--st-idle)}
.termbar{margin-bottom:var(--space-2)}
.termbar #termMode{margin-left:auto}
.detail-acts{display:flex;flex-wrap:wrap;gap:var(--space-2);margin-bottom:var(--space-3)}
.auto{margin-bottom:var(--space-4);padding-bottom:var(--space-3);border-bottom:1px solid var(--color-line)}
.auto h3 .sc-badge{margin-left:var(--space-1)}
.settings-form.termform{margin-top:0;padding-top:0;border-top:0;max-width:none}
.sc-app-main.has-detail.detail-wide{grid-template-columns:minmax(0,1fr)}
.detail-wide>.sc-topbar,.detail-wide>.sc-app-page{display:none}
.detail-wide>.detail{grid-column:1;border-left:0}
.detail-wide #termPane.typing #termScreen{height:calc(100vh - 13rem);height:calc(100dvh - 13rem)}
.toolbar{display:flex;gap:var(--space-2) var(--space-3);align-items:center;flex-wrap:wrap;margin-bottom:var(--space-3)}
.toolbar [role=status],.toolbar .muted{font-size:.85em}
.sc-sidenav-org{cursor:default}.sc-sidenav-org:hover{border-color:var(--color-line)}
.sc-sidenav-org h1{font-size:inherit;font-weight:inherit;letter-spacing:0;line-height:inherit}
.sc-sidenav .tabs{display:contents}
.sc-sidenav-item{width:100%;border:0;background:none;font:inherit;text-align:left;cursor:pointer}
.sc-sidenav-item[aria-selected=true]{background:var(--color-accent-tint);color:var(--color-ink);font-weight:600}
.sc-sidenav-item[aria-selected=true]::before{content:"";position:absolute;left:0;top:var(--space-2);bottom:var(--space-2);width:2px;background:var(--color-accent)}
.sc-sidenav-item[hidden]{display:none}
.sc-sidenav-count:empty{display:none}
.topacts{display:flex;align-items:center;gap:var(--space-3);margin-left:auto;min-width:0}
.liveind{display:inline-flex;align-items:center;gap:6px;font-size:var(--text-xs);color:var(--color-muted);white-space:nowrap}
.live{width:8px;height:8px;border-radius:50%;background:var(--st-offline);flex:none}.live.on{background:var(--st-idle)}
.sc-topbar-account{cursor:default;min-width:0}.sc-topbar-avatar{text-transform:uppercase}.sc-topbar-account[hidden]{display:none}
#whoName{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:12rem}
.sc-app-page>[role=tabpanel]{margin:0}
.sc-app-page>.card{border:0;padding:0;background:none}
[role=tabpanel][hidden]{display:none}
.sc-app-main{display:grid;grid-template-columns:minmax(0,1fr);grid-template-rows:auto 1fr}
.sc-app-main.has-detail{grid-template-columns:minmax(0,1fr) 440px}
.sc-app-main>.sc-topbar,.sc-app-main>.sc-app-page{grid-column:1}
.sc-topbar{position:sticky;top:0;z-index:5}
.detail{grid-column:2;grid-row:1/span 2;align-self:start;position:sticky;top:0;height:100vh;height:100dvh;overflow-y:auto;min-width:0;background:var(--color-canvas);border-left:1px solid var(--color-line)}
.detail[hidden]{display:none}
.detail-head{display:flex;align-items:center;gap:var(--space-2);min-height:var(--topbar-height);padding:var(--space-2) var(--space-3) var(--space-2) var(--space-4);border-bottom:1px solid var(--color-line)}
.detail-head h2{flex:1;min-width:0;overflow-wrap:anywhere;font-size:1rem}
.detail-head h2:focus{outline:0}
.detail-head .sc-badge{flex:none;white-space:normal;max-width:45%}
.detail-close{flex:none;border:0;background:none;padding:.15rem .5rem;font-size:1.25rem;line-height:1;color:var(--color-muted)}
.detail-back{display:none;flex:none;border:0;background:none;padding:.2rem .3rem;color:var(--color-accent)}
.detail .sc-tabs{padding:0 var(--space-3)}
.detail .sc-tabs:empty{display:none}
.detail-body{padding:var(--space-4);min-width:0;overflow-wrap:anywhere}
.detail-body .pdl{margin-top:0}
.detail-body pre{padding:var(--space-3);background:var(--color-paper);border-radius:var(--radius)}
.dlist{list-style:none;margin:0;padding:0}
.dlist li{padding:var(--space-2) var(--space-3);border-left:2px solid var(--color-line);margin:0 0 var(--space-2);min-width:0}
.dlist li.cur{border-left-color:var(--color-accent);background:var(--color-paper)}
.dlist small{display:block;color:var(--color-muted)}
.dlist p{margin:var(--space-1) 0 0;white-space:pre-wrap;overflow-wrap:anywhere}
.listbar{display:flex;flex-wrap:wrap;gap:var(--space-2) var(--space-3);align-items:center;margin-bottom:var(--space-3)}
.chips{display:flex;flex-wrap:wrap;gap:var(--space-2)}
.listbar input[type=search]{flex:1 1 14rem;max-width:22rem}
table.list td{white-space:normal;overflow-wrap:break-word}
table.list td small{overflow-wrap:anywhere}
table.list td.nowrap{white-space:nowrap}
table.list tr[data-detail-id]{cursor:pointer}
table.list tr[aria-current=true] td{background:var(--color-accent-tint)}
table.list tr[data-detail-id]:focus-visible{outline:2px solid var(--color-focus);outline-offset:-2px}
table.list td small{display:block;color:var(--color-muted)}
table.list tr.urgent td:first-child{box-shadow:inset 3px 0 var(--st-error)}
@media (max-width:720px){
.sc-app-main.has-detail{grid-template-columns:minmax(0,1fr)}
.detail{position:fixed;inset:0;align-self:stretch;height:auto;z-index:20;border-left:0}
.detail,.sc-drawer,.sc-drawer-body{overscroll-behavior:contain}
:root:has(.detail:not([hidden])),:root:has(dialog.sc-drawer[open]){overflow:hidden}
.detail-back{display:inline-block}
#termWide{display:none}
}
@media (max-width:1180px){
.sc-sidenav-item{width:44px}
.sc-sidenav-item[data-wait][data-nonzero] .sc-sidenav-count{display:block;position:absolute;top:5px;right:5px;width:7px;height:7px;padding:0;margin:0;border-radius:50%;background:var(--st-error);font-size:0;overflow:hidden}
}
@media (max-width:720px){
.sc-sidenav{gap:2px;padding:var(--space-2) 6px}
.sc-sidenav-org{padding:3px;margin-right:2px}
.sc-sidenav-item{width:36px}
.sc-sidenav-item[aria-selected=true]::before{top:auto;right:var(--space-2);bottom:0;left:var(--space-2);width:auto;height:2px}
.topacts{gap:var(--space-2)}
}
table{border-collapse:collapse;width:100%;font-size:.88em;font-variant-numeric:tabular-nums}
th,td{text-align:left;padding:.4rem .6rem;border-bottom:1px solid var(--color-line);white-space:nowrap;vertical-align:top}
th{font-size:.85em;font-weight:600;color:var(--color-muted);background:var(--color-paper)}
tbody tr:hover td{background:var(--color-paper)}
td.dir{max-width:14rem;overflow:hidden;text-overflow:ellipsis;direction:rtl;text-align:left}
tr.offline td{color:var(--st-offline)}
tr.row-error td:first-child{box-shadow:inset 3px 0 var(--st-error)}
tr.row-blocked td:first-child{box-shadow:inset 3px 0 var(--st-blocked)}
.st-idle{color:var(--st-idle)}.st-busy{color:var(--st-busy);font-weight:600}.st-compacting{color:var(--st-compacting);font-weight:600}
.st-error{color:var(--st-error);font-weight:600}.st-blocked{color:var(--st-blocked);font-weight:600}
.st-offline,.st-unknown{color:var(--st-offline)}
.subs{color:var(--st-busy);font-weight:400;overflow-wrap:anywhere}
.pill.allowed{color:var(--st-idle);background:var(--st-idle-bg)}.pill.denied{color:var(--st-error);background:var(--color-paper)}.pill.expired{color:var(--st-offline);background:var(--st-offline-bg)}
.pcard.allowed,.pcard.denied,.pcard.expired{background:var(--color-paper)}
.pcard summary{cursor:pointer;overflow-wrap:anywhere}
.pdl{display:grid;grid-template-columns:auto 1fr;gap:var(--space-1) var(--space-3);margin:var(--space-2) 0;font-size:.9em}
.pdl dt{color:var(--color-muted)}.pdl dd{margin:0;overflow-wrap:anywhere;white-space:pre-wrap;min-width:0}
.panswer{display:flex;gap:var(--space-2);align-items:end;flex-wrap:wrap}
.panswer label{display:flex;flex-direction:column;gap:var(--space-1);flex:1 1 14rem;min-width:0;font-size:.9em}
.panswer input{width:100%;min-width:0}
tr.focus td{background:var(--color-accent-tint)}
.panswer button.ok{background:var(--color-accent);border-color:var(--color-accent);color:var(--color-on-accent)}
.panswer button.danger{background:var(--st-error);border-color:var(--st-error);color:var(--color-on-accent)}
.sechead{margin:var(--space-4) 0 var(--space-2)}
.sechead .muted{font-weight:400;font-size:.85em}
table.list td.strong,table.list td.witem{font-weight:650}
.wsub{display:flex;flex-wrap:wrap;gap:var(--space-2);align-items:center;margin-top:var(--space-1);font-weight:400;font-size:.9em}
.wsub button{font-size:.92em}
.wsub button.linkish{background:none;border:0;padding:0;color:var(--color-accent);text-decoration:underline;cursor:pointer}
table.list td.mono{font-family:var(--font-mono);font-size:.92em}
table.list td.over{color:var(--st-error);font-weight:650}
table.list tr.done td{color:var(--color-muted)}
td.cardbr{display:none}
.dlist li.over{border-left-color:var(--st-error)}
.pill{display:inline-block;border-radius:var(--radius);padding:0 .4rem;margin-right:var(--space-2);font-size:.75em;font-weight:600;vertical-align:middle;color:var(--color-accent);background:var(--color-accent-tint)}
.pill.pending{color:var(--st-blocked);background:var(--st-blocked-bg)}.pill.answered{color:var(--st-idle);background:var(--st-idle-bg)}.pill.withdrawn{color:var(--st-offline);background:var(--st-offline-bg)}.pill.waiting{color:var(--st-blocked);background:var(--st-blocked-bg)}
.qitem{border:1px solid var(--color-line);border-radius:var(--radius);padding:var(--space-3);margin:var(--space-3) 0;min-width:0}
.qitem legend,.qhead{font-weight:600;padding:0 var(--space-1)}
.qtext{margin:var(--space-1) 0 var(--space-2);white-space:pre-wrap;overflow-wrap:anywhere}
.qopt{display:flex;gap:var(--space-2);align-items:flex-start;padding:var(--space-2);margin:var(--space-1) 0;border:1px solid var(--color-line);border-radius:var(--radius);cursor:pointer;overflow-wrap:anywhere}
.qopt:hover{border-color:var(--color-accent)}
.qopt input{margin:.2rem 0 0;flex:none;width:1.05rem;height:1.05rem}
.qopt>span{min-width:0;flex:1}
.qopt pre{margin-top:var(--space-1);padding:var(--space-2);background:var(--color-paper);border-radius:var(--radius);font-size:12.5px}
.qother{display:flex;flex-direction:column;gap:var(--space-1);margin-top:var(--space-2);font-size:.9em}
.qother input,.settings-form input:not([type=checkbox]){width:100%}
.qans{margin:var(--space-1) 0;overflow-wrap:anywhere}
.msg{margin:var(--space-2) 0;font-weight:600}.msg:empty{display:none}.msg.err{color:var(--st-error)}.msg.ok{color:var(--st-idle)}
.settings-form{display:grid;gap:var(--space-3);max-width:32rem;margin-top:var(--space-4);padding-top:var(--space-4);border-top:1px solid var(--color-line)}
.settings-form+.toolbar{margin-top:var(--space-6);padding-top:var(--space-4);border-top:1px solid var(--color-line)}
td.revoked{color:var(--st-error);font-weight:600}
.settings-form label{display:flex;flex-direction:column;gap:var(--space-1);font-size:.9em}
.settings-form label.check{flex-direction:row;align-items:center;gap:var(--space-2)}
.viewswitch{display:inline-flex;border:1px solid var(--color-line-strong);border-radius:var(--radius);overflow:hidden;margin:0 0 var(--space-3)}
.toolbar .viewswitch{margin:0}
.viewswitch button{margin:0;border:0;border-radius:0;background:var(--color-canvas);color:var(--color-muted);padding:.35rem .9rem}
.viewswitch button+button{border-left:1px solid var(--color-line-strong)}
.viewswitch button[aria-pressed=true]{background:var(--color-accent-tint);color:var(--color-accent);font-weight:650}
.decfilters{margin-bottom:var(--space-3)}
.decfilters a.button{display:inline-block;text-decoration:none}
.dec{border-bottom:1px solid var(--color-line);padding:var(--space-2) 0}
.dec p{margin:.15rem 0;overflow-wrap:anywhere}
.dec p.muted{font-size:.85em}
.metgrid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,26rem),1fr));gap:var(--space-3)}
.mcard,.pcard{border:1px solid var(--color-line);border-radius:var(--radius);padding:var(--space-3) var(--space-4);min-width:0;background:var(--color-canvas)}
.mcard.full{grid-column:1/-1}
.mcard h3{margin:0 0 var(--space-1)}.mcard h4{margin:var(--space-4) 0 var(--space-1)}
.mcard>p.muted{margin:0 0 var(--space-2);font-size:.85em}
.mcard>p.muted:empty{display:none}
.stats{display:grid;grid-template-columns:repeat(auto-fill,minmax(6rem,1fr));gap:var(--space-2);margin:var(--space-2) 0}
.stat{background:var(--color-paper);border-radius:var(--radius);padding:var(--space-2) var(--space-3)}
.stat b{display:block;font-size:1.2em;font-weight:650;font-variant-numeric:tabular-nums}.stat span{font-size:.78em;color:var(--color-muted)}
.chart svg{display:block;width:100%;max-width:26rem;height:auto;margin:var(--space-2) 0}
.chart .bar{fill:var(--color-accent)}.chart .axis{stroke:var(--color-line-strong)}.chart text{font:11px system-ui,sans-serif;fill:var(--color-muted)}
tr.over td{background:var(--st-error-bg)}tr.over td:first-child{box-shadow:inset 3px 0 var(--st-error)}
td.num,th.num{text-align:right}
table.stack td.wrap{white-space:normal;overflow-wrap:anywhere}
.mthreads{margin:var(--space-1) 0;padding-left:1.4rem}
.mthreads li{margin:var(--space-1) 0}
.mthreads li.empty{list-style:none;margin-left:-1.4rem}
.mthreads button,.plinks button{margin:0;padding:.3rem .5rem;border:1px solid var(--color-line);border-radius:var(--radius);background:var(--color-canvas);color:var(--color-accent);text-align:left;width:100%;overflow-wrap:anywhere}
.mthreads small,.plinks small{display:block;color:var(--color-muted)}
.pcard h3{display:flex;gap:var(--space-2);align-items:center;justify-content:space-between;margin:0 0 var(--space-2);min-width:0}
.pcard h3 .pname{min-width:0;overflow-wrap:anywhere}
.pcard h4{margin:var(--space-3) 0 var(--space-1)}
.pcard p{margin:var(--space-1) 0;font-size:.92em}
.pbadge{flex:none;border-radius:999px;padding:.05rem .55rem;font-size:.75em;font-weight:650;color:var(--st-offline);background:var(--st-offline-bg)}
.pbadge.b-error{color:var(--st-error);background:var(--st-error-bg)}.pbadge.b-blocked{color:var(--st-blocked);background:var(--st-blocked-bg)}.pbadge.b-busy{color:var(--st-busy);background:var(--st-busy-bg)}.pbadge.b-compacting{color:var(--st-compacting);background:var(--st-compacting-bg)}.pbadge.b-idle{color:var(--st-idle);background:var(--st-idle-bg)}
.pcard.b-error{border-top:3px solid var(--st-error)}.pcard.b-blocked{border-top:3px solid var(--st-blocked)}
.plist{list-style:none;margin:0;padding:0}
.plist li{margin:var(--space-1) 0;min-width:0;overflow-wrap:anywhere;font-size:.92em}
.pdir{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;direction:rtl;text-align:left;color:var(--color-muted);font:12px/1.4 var(--font-mono)}
.plinks li.over button{border-color:var(--st-error);background:var(--st-error-bg);box-shadow:inset 3px 0 var(--st-error)}
.plinks li.over small{color:var(--st-error);font-weight:600}
.filters{display:grid;grid-template-columns:repeat(auto-fill,minmax(9.5rem,1fr));gap:var(--space-2);align-items:center}
.filters .actions{display:flex;gap:var(--space-2);flex-wrap:wrap;align-items:center;grid-column:1/-1}
#conn{font-size:.85em;color:var(--color-muted)}
.st-online{color:var(--st-idle);font-weight:600}
@media (max-width:600px){
.card{padding:var(--space-3)}
.mcard,.pcard{padding:var(--space-3)}
.filters{grid-template-columns:1fr 1fr}
.decfilters>*{flex:1 1 100%}
.decfilters a.button,.decfilters button{text-align:center}
td.dir{max-width:11rem}
table.stack thead{display:none}
table.stack tr{display:block;border-bottom:1px solid var(--color-line);padding:var(--space-2) 0}
table.stack td{display:block;border:0;padding:.1rem .2rem;white-space:normal;overflow-wrap:anywhere;text-align:left;max-width:none;direction:ltr}
table.stack td[data-label]::before{content:attr(data-label) " ";color:var(--color-muted);font-size:.85em;margin-right:var(--space-1)}
table.stack td:first-child{font-weight:650}
table.stack tbody tr:hover td{background:none}
table.cards tr{display:flex;flex-wrap:wrap;align-items:baseline;gap:var(--space-1) var(--space-2);padding:var(--space-2) var(--space-1)}
table.cards td{padding:0;min-width:0}
table.cards td.cardbr{display:block;flex-basis:100%;height:0}
table.cards td.cardbr~td{font-size:.88em;color:var(--color-muted);font-weight:400}
table.cards td[data-label]::before{content:none}
table.cards tr.row-error td:first-child,table.cards tr.row-blocked td:first-child{padding-left:var(--space-2)}
table.cards td[data-pre]::before{content:attr(data-pre);color:var(--color-muted)}
table.cards td.cardhide{display:none}
#panel-settings table.cards td.cardbr+td~td[data-pre]::before{content:"· " attr(data-pre)}
#panel-settings table.cards td.cardbr+td~td:not([data-pre])::before{content:"· "}
#progressTbl td[data-label]+td[data-label]::before{content:"· "}
#progressTbl td.num[data-label]::after{content:" " attr(data-label)}
}
.ruleform{display:grid;gap:var(--space-2);max-width:44rem}.ruleform textarea{width:100%}.ruleform .row{display:flex;flex-wrap:wrap;gap:var(--space-2);align-items:center}
</style>
</head>
<body>
<div class="sc-app">
<aside class="sc-sidenav" aria-label="주 메뉴">
<div class="sc-sidenav-org"><span class="sc-sidenav-mark" aria-hidden="true">` + productMark + `</span><div class="sc-sidenav-org-text"><h1>` + headerText + `</h1></div></div>
<div id="tabs" class="tabs" role="tablist" aria-label="탭">
<div class="sc-sidenav-heading" aria-hidden="true">운영</div>
<button type="button" class="sc-sidenav-item" title="진행" role="tab" id="tab-progress" data-tab="progress" aria-controls="panel-progress" aria-selected="true" tabindex="0"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 8h3l2-5 2 10 2-5h3" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">진행</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="세션" role="tab" id="tab-sessions" data-tab="sessions" aria-controls="panel-sessions" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2.5 3.5h11v7h-11zM6 13.5h4" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">세션</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="메일" role="tab" id="tab-mail" data-tab="mail" aria-controls="panel-mail" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 3.5h12v9H2zM2 4l6 5 6-5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">메일</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="보고" role="tab" id="tab-reports" data-tab="reports" aria-controls="panel-reports" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 1.5h6l3 3v10h-9zM9.5 1.5v3h3M5.5 8h5M5.5 10.5h5M5.5 13h3" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">보고</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="작업" role="tab" id="tab-jobs" data-tab="jobs" aria-controls="panel-jobs" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 14.5a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13zM8 4.5V8l2.5 1.5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">작업</span><span class="tab-count sc-sidenav-count"></span></button>
<div class="sc-sidenav-heading" aria-hidden="true">응답</div>
<button type="button" class="sc-sidenav-item" title="질문" data-wait role="tab" id="tab-questions" data-tab="questions" aria-controls="panel-questions" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 14.5a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13zM6.3 6a1.8 1.8 0 1 1 2.4 1.7c-.5.2-.7.6-.7 1.1v.5M8 11.2v.1" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">질문</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="권한 요청" data-wait role="tab" id="tab-permissions" data-tab="permissions" aria-controls="panel-permissions" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.5l5.5 2v4c0 3.5-2.5 6-5.5 7-3-1-5.5-3.5-5.5-7v-4z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">권한 요청</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="규칙" role="tab" id="tab-rules" data-tab="rules" aria-controls="panel-rules" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2.5 4l1.5 1.5L6.5 3M8.5 4.5h5M2.5 9l1.5 1.5L6.5 8M8.5 9.5h5M8.5 13h5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">규칙</span><span class="tab-count sc-sidenav-count"></span></button>
<div class="sc-sidenav-heading" aria-hidden="true">관리</div>
<button type="button" class="sc-sidenav-item" title="지표" role="tab" id="tab-metrics" data-tab="metrics" aria-controls="panel-metrics" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2.5 13.5v-4M6.5 13.5v-8M10.5 13.5v-6M14 13.5v-10" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">지표</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="터미널" role="tab" id="tab-terminals" data-tab="terminals" aria-controls="panel-terminals" aria-selected="false" tabindex="-1" hidden><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 3h12v10H2zM4.5 6l2 2-2 2M8 10h3.5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">터미널</span><span class="tab-count sc-sidenav-count"></span></button>
<button type="button" class="sc-sidenav-item" title="설정" role="tab" id="tab-settings" data-tab="settings" aria-controls="panel-settings" aria-selected="false" tabindex="-1"><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 10.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span class="tab-name sc-sidenav-label">설정</span><span class="tab-count sc-sidenav-count"></span></button>
</div>
</aside>
<div class="sc-app-main" id="appMain">
<header class="sc-topbar"><nav aria-label="이동 경로"><ol class="sc-topbar-crumbs"><li>` + productName + `</li><li id="crumbTab" aria-current="page">진행</li></ol></nav>
<div class="topacts"><span class="liveind"><span id="live" class="live" title="실시간 연결 끊김"></span><span id="liveText">끊김</span></span><span class="sc-topbar-account" id="who" hidden><span id="whoMark" class="sc-topbar-avatar" aria-hidden="true"></span><span class="sc-topbar-account-text"><span id="whoName"></span></span></span><button type="button" id="logout" hidden>로그아웃</button></div></header>
<main class="sc-app-page">
<section id="panel-progress" role="tabpanel" aria-labelledby="tab-progress" tabindex="0" class="card">
<div class="toolbar"><h2>진행</h2><button id="reloadProgress">새로 고침</button><span id="progressNote" class="muted" role="status" aria-live="polite"></span></div>
<h3 class="sechead">대기 <span id="waitCount" class="muted"></span></h3>
<table id="waitTbl" class="stack list cards"><thead><tr><th>종류</th><th>항목</th><th>역할</th><th class="num">지난 시간</th></tr></thead><tbody id="wait"></tbody></table>
<h3 class="sechead">서비스</h3>
<div class="listbar"><div class="chips" id="svcChips" role="group" aria-label="서비스 고르기"></div></div>
<table id="progressTbl" class="stack list cards"><thead><tr><th>서비스</th><th>상태</th><th class="num">세션</th><th class="num">열린 지시</th><th class="num">남긴 것</th><th>마지막 소식</th></tr></thead><tbody id="progress"></tbody></table>
</section>
<section id="panel-sessions" role="tabpanel" aria-labelledby="tab-sessions" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>세션</h2><button id="reloadSessions">새로 고침</button><span id="sessionsNote" class="muted"></span></div>
<div class="listbar"><div class="chips" id="sessionChips" role="group" aria-label="상태 고르기"></div><input id="sessionsSearch" type="search" aria-label="세션 검색" placeholder="역할·기계·폴더·라벨"></div>
<table id="sessionsTbl" class="stack list"><thead><tr><th>역할</th><th>상태</th><th class="num">컨텍스트</th><th>마지막 턴</th><th>기계</th><th>판</th><th>라벨</th></tr></thead><tbody id="sessions"></tbody></table>
</section>
<section id="panel-metrics" role="tabpanel" aria-labelledby="tab-metrics" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>지표</h2>
<div class="viewswitch" role="group" aria-label="기간 고르기"><button type="button" id="metDays7" data-days="7" aria-pressed="true">7일</button><button type="button" id="metDays30" data-days="30" aria-pressed="false">30일</button></div>
<button id="reloadMetrics">새로 고침</button><span id="metricsNote" class="muted" role="status" aria-live="polite"></span></div>
<div class="metgrid">
<div class="mcard full"><h3 title="스레드를 연 메일이 첫 답을 받기까지 걸린 시간">첫 답장</h3>
<div id="mFirstStats" class="stats"></div>
<div id="mFirstChart" class="chart"></div>
<table id="mFirstTbl" class="stack"><thead><tr><th>받는 쪽</th><th class="num">메일</th><th class="num">답 받음</th><th class="num">답 없음</th><th class="num">p50</th><th class="num">p90</th><th class="num">최장</th></tr></thead><tbody id="mFirstByRecipient"></tbody></table>
</div>
<div class="mcard"><h3 title="역할별 안 읽힌 메일과 가장 오래된 것">안 읽힌 메일</h3>
<p id="mPendingNote" class="muted"></p>
<table id="mPendingTbl" class="stack"><thead><tr><th>역할</th><th class="num">안 읽음</th><th class="num">가장 오래된 것</th></tr></thead><tbody id="mPending"></tbody></table>
</div>
<div class="mcard"><h3 title="두 쪽이 두 번 넘게 주고받은 스레드">되물음</h3>
<p id="mThreadsNote" class="muted"></p>
<ol id="mThreads" class="mthreads"></ol>
</div>
<div class="mcard full"><h3 title="기간 안에 오류·막힘 상태로 있던 시간">세션 상태</h3>
<p id="mSessionsNote" class="muted"></p>
<table id="mStateTbl" class="stack"><thead><tr><th>역할</th><th class="num">오류</th><th class="num">막힘</th><th>많았던 상태</th></tr></thead><tbody id="mStates"></tbody></table>
<h4>지금 오류·막힘</h4>
<table id="mCurrentTbl" class="stack"><thead><tr><th>역할</th><th>상태</th><th>시작</th><th class="num">지난 시간</th></tr></thead><tbody id="mCurrent"></tbody></table>
</div>
</div>
<p id="mAliases" class="muted"></p>
</section>
<section id="panel-mail" role="tabpanel" aria-labelledby="tab-mail" tabindex="0" hidden>
<div class="card filters">
<select id="box" aria-label="메일함"><option value="all">모든 메일</option><option value="inbox">받은 메일</option><option value="outbox">보낸 메일</option><option value="held">보관 중(라벨)</option></select>
<select id="role" aria-label="역할"><option value="">모든 역할</option></select>
<select id="status" aria-label="상태"><option value="">모든 상태</option><option value="pending">안 읽음</option><option value="read">읽음</option><option value="acknowledged">확인함</option><option value="complete">완료</option></select>
<input id="search" aria-label="검색" placeholder="내용·역할·task 검색">
<input id="thread" type="number" min="1" aria-label="스레드 번호" placeholder="스레드 번호">
<input id="task" aria-label="task id" placeholder="task id" autocomplete="off" autocapitalize="off" spellcheck="false">
<div class="actions">
<button id="go" type="submit">검색</button>
<button id="reset">초기화</button>
<button id="reloadMail">새로 고침</button>
<span id="conn">연결 중…</span>
</div>
</div>
<div id="notice" class="muted"></div>
<table id="mailTbl" class="stack list"><thead><tr><th class="num">#</th><th>제목</th><th>보낸 쪽 → 받는 쪽</th><th>상태</th><th>시각</th></tr></thead><tbody id="list"></tbody></table>
</section>
<section id="panel-reports" role="tabpanel" aria-labelledby="tab-reports" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>보고</h2><button id="reloadReports">새로 고침</button><span id="reportsNote" class="muted" role="status" aria-live="polite"></span></div>
<div class="listbar"><div class="chips" id="reportChips" role="group" aria-label="상태 고르기"></div></div>
<table id="reportsTbl" class="stack list"><thead><tr><th>상태</th><th>제목</th><th>보낸 쪽</th><th>시각</th></tr></thead><tbody id="reports"></tbody></table>
</section>
<section id="panel-jobs" role="tabpanel" aria-labelledby="tab-jobs" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>작업</h2><button id="reloadJobs">새로 고침</button><span id="jobsNote" class="muted" role="status" aria-live="polite"></span></div>
<div class="filters">
<select id="jobRole" aria-label="역할"><option value="">모든 역할</option></select>
<select id="jobState" aria-label="상태"><option value="live">진행 중</option><option value="">모든 상태</option><option value="exited">끝남</option><option value="failed">시작 실패</option><option value="stopped">중지됨</option><option value="expired">만료</option><option value="lost">잃음</option></select>
</div>
<table id="jobsTbl" class="stack list cards"><thead><tr><th>종류</th><th>이름</th><th>역할</th><th>기계</th><th>상태</th><th>시작</th><th>만료</th><th>마지막 줄</th><th></th></tr></thead><tbody id="jobs"></tbody></table>
</section>
<section id="panel-permissions" role="tabpanel" aria-labelledby="tab-permissions" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>권한 요청</h2><button id="reloadPerms">새로 고침</button><span id="permsNote" class="muted" role="status" aria-live="polite"></span></div>
<div id="permsMsg" class="msg" role="status" aria-live="polite"></div>
<h3 class="sechead">대기</h3>
<table id="permPendingTbl" class="stack list"><thead><tr><th>상태</th><th>도구 · 요약</th><th>요청한 쪽</th><th>요청</th><th>만료</th></tr></thead><tbody id="permPending"></tbody></table>
<h3 class="sechead">지난 기록</h3>
<table id="permHistoryTbl" class="stack list"><thead><tr><th>결과</th><th>도구 · 요약</th><th>요청한 쪽</th><th>요청</th><th>답한 쪽</th></tr></thead><tbody id="permHistory"></tbody></table>
</section>
<section id="panel-rules" role="tabpanel" aria-labelledby="tab-rules" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>규칙</h2><button id="reloadRules">새로 고침</button><span id="rulesNote" class="muted" role="status" aria-live="polite"></span></div>
<div class="listbar"><div class="chips" id="ruleChips" role="group" aria-label="종류 고르기"></div>
<input id="ruleService" type="search" aria-label="서비스" placeholder="서비스" autocomplete="off" autocapitalize="off" spellcheck="false">
<select id="ruleTopic" aria-label="주제"><option value="">모든 주제</option></select>
<label><input type="checkbox" id="ruleAll"> 대체·무효 포함</label>
<button type="button" id="ruleProposed" class="sc-chip" aria-pressed="false">제안 <span id="ruleProposedN" class="sc-chip-count">0</span></button>
<span id="ruleUnclassified" class="muted"></span></div>
<table id="rulesTbl" class="stack list"><thead><tr><th class="num">#</th><th>종류</th><th>주제</th><th>범위</th><th>글</th><th class="num">걸림</th></tr></thead><tbody id="rules"></tbody></table>
<details id="ruleAdd"><summary>새 줄</summary>
<form id="ruleForm" class="ruleform">
<div class="row"><select id="ruleKind" aria-label="종류"><option value="decision">결정</option><option value="tip">요령</option></select>
<input id="ruleScope" aria-label="범위" placeholder="all · service:NAME · tag:NAME" required autocomplete="off" autocapitalize="off" spellcheck="false">
<input id="ruleTopicIn" list="ruleTopics" aria-label="주제" placeholder="주제" required autocomplete="off" autocapitalize="off" spellcheck="false"><datalist id="ruleTopics"></datalist>
<label><input type="checkbox" id="ruleNewTopic"> 새 주제</label>
<input id="ruleGroup" aria-label="그룹" placeholder="그룹 (all·tag 줄)" autocomplete="off" autocapitalize="off" spellcheck="false"></div>
<textarea id="ruleText" rows="3" aria-label="글" placeholder="첫 줄: 요약 · 둘째 줄부터: 하위 항목" required></textarea>
<div class="row"><input id="ruleWhenTool" aria-label="걸림: 도구" placeholder="도구 (Bash, mcp__*)" autocomplete="off" autocapitalize="off" spellcheck="false">
<input id="ruleWhenCommand" aria-label="걸림: 명령 정규식" placeholder="명령 정규식" autocomplete="off" autocapitalize="off" spellcheck="false">
<input id="ruleWhenPath" aria-label="걸림: 경로 glob" placeholder="경로 glob (**/x/*.sql)" autocomplete="off" autocapitalize="off" spellcheck="false"></div>
<input id="ruleSource" aria-label="출처" placeholder="출처: 질문 ID(q_…) 또는 글" required>
<div class="row"><button type="submit">올리기</button></div>
<div id="ruleAddMsg" class="msg" role="status" aria-live="polite"></div>
</form></details>
</section>
<section id="panel-questions" role="tabpanel" aria-labelledby="tab-questions" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>질문</h2>
<div class="viewswitch" role="group" aria-label="질문 보기 고르기"><button type="button" id="viewQuestions" aria-pressed="true">질문</button><button type="button" id="viewDecisions" aria-pressed="false">결정</button></div>
<button id="reloadQuestions">새로 고침</button><span id="questionsNote" class="muted"></span></div>
<div id="questionsView">
<div id="questionsMsg" class="msg" role="status" aria-live="polite"></div>
<table id="questionsTbl" class="stack list"><thead><tr><th>상태</th><th>질문</th><th>묻는 쪽</th><th>시각</th></tr></thead><tbody id="questions"></tbody></table>
</div>
<div id="decisionsView" hidden>
<form id="decForm" class="row decfilters">
<select id="decPeriod" aria-label="기간" title="날짜는 UTC 기준"><option value="7">최근 7일</option><option value="30" selected>최근 30일</option><option value="90">최근 90일</option><option value="all">전체</option><option value="custom">직접 고르기</option></select>
<input id="decFrom" type="date" aria-label="시작일" title="UTC 기준" hidden>
<input id="decTo" type="date" aria-label="끝일" title="UTC 기준" hidden>
<input id="decRole" list="decRoles" aria-label="역할" placeholder="역할 또는 svc/*" autocomplete="off" autocapitalize="off" spellcheck="false">
<datalist id="decRoles"></datalist>
<input id="decSearch" type="search" aria-label="검색" placeholder="제목·질문·고른 답 검색">
<button type="submit">검색</button>
<a id="decExport" class="button" download href="/api/admin/decisions?format=ndjson">JSON 내보내기</a>
</form>
<div id="decisionsNote" class="muted" role="status" aria-live="polite"></div>
<div id="decisions"></div>
</div>
</section>
<section id="panel-settings" role="tabpanel" aria-labelledby="tab-settings" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>설정</h2>
<div class="viewswitch" id="setViews" role="group" aria-label="설정 고르기"><button type="button" data-view="qsettings" aria-pressed="true">질문 설정</button><button type="button" data-view="roles" aria-pressed="false">역할</button><button type="button" data-view="nodes" aria-pressed="false" hidden>노드</button><button type="button" data-view="groups" aria-pressed="false" hidden>묶음</button><button type="button" data-view="reminders" aria-pressed="false">다시 알림</button></div>
<button id="reloadSettings">새로 고침</button><span id="settingsNote" class="muted"></span></div>
<div id="settingsMsg" class="msg" role="status" aria-live="polite"></div>
<div id="setView-qsettings">
<div class="listbar"><button type="button" id="setAddOpen" aria-haspopup="dialog" aria-controls="setAdd">추가</button></div>
<table id="settingsTbl" class="stack list cards"><thead><tr><th>대상</th><th>질문 가능</th><th>지켜보는 역할</th><th>디스코드</th><th>바뀐 때</th></tr></thead><tbody id="settingsTable"></tbody></table>
<h3 class="sechead">최근 거절 <span class="muted" title="can_ask 꺼짐으로 거절된 역할 (24시간)">24시간</span></h3>
<table id="refusalsTbl" class="stack list cards"><thead><tr><th>역할</th><th>맞은 대상</th><th>대신 물을 역할</th><th class="num">횟수</th><th>마지막 거절</th></tr></thead><tbody id="refusalsTable"></tbody></table>
</div>
<div id="setView-roles" hidden>
<table id="rolesTbl" class="stack list cards"><thead><tr><th>역할</th><th>상태</th><th>등록</th><th>끊은 때</th></tr></thead><tbody id="rolesTable"></tbody></table>
</div>
<div id="setView-nodes" hidden>
<table id="nodesTbl" class="stack list cards"><thead><tr><th>노드</th><th>기계</th><th>OS</th><th>버전</th><th>마지막 접속</th><th>끊은 때</th></tr></thead><tbody id="nodesTable"></tbody></table>
</div>
<div id="setView-groups" hidden>
<form id="groupForm" class="settings-form termform"><label>이름 <input id="groupName" required maxlength="64" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="fast"></label><label title="남은 한도가 이 값(%)보다 작으면 새 일을 받지 않음. 비우면 문턱 없음">문턱 % <input id="groupThreshold" type="number" min="0" max="100" step="1" inputmode="numeric"></label><div class="row"><button type="submit">추가</button></div></form>
<table id="groupsTbl" class="stack list cards"><thead><tr><th>묶음</th><th>문턱 %</th><th class="num">세션</th><th>바뀐 때</th><th></th></tr></thead><tbody id="groupsTable"></tbody></table>
</div>
<div id="setView-reminders" hidden><div id="remindBox"></div></div>
<dialog id="setAdd" class="sc-drawer" aria-labelledby="setAddTitle"><div class="sc-drawer-head"><h3 id="setAddTitle" class="sc-drawer-title">질문 설정 추가</h3><button type="button" id="setAddClose" class="detail-close" aria-label="닫기">×</button></div><div class="sc-drawer-body">
<div id="setAddMsg" class="msg" role="status" aria-live="polite"></div>
<form id="settingsForm" class="settings-form termform">
<label title="default, 역할(svc/coordinator — svc/coordinator-N 포함), 접두사(svc/*). 정확한 대상 → 가장 긴 접두사 → default 순으로 하나만 적용">대상 <input id="setTarget" required autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="svc/coordinator 또는 svc/*"></label>
<label class="check"><input type="checkbox" id="setCanAsk"> 질문할 수 있음 (can_ask)</label>
<label class="check"><input type="checkbox" id="setDiscord"> 디스코드 알림 (discord)</label>
<label>지켜보는 역할 (쉼표로 구분, 최대 20) <input id="setWatchers" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="team/boss, ops/lead"></label>
<div class="row"><button type="submit">저장</button><button type="button" id="setClear">비우기</button></div>
</form>
</div></dialog>
</section>
<section id="panel-terminals" role="tabpanel" aria-labelledby="tab-terminals" tabindex="0" class="card" hidden>
<div class="toolbar"><h2>터미널</h2><button id="reloadTerminals">새로 고침</button><span id="terminalsNote" class="muted"></span></div>
<div id="terminalsMsg" class="msg" role="status" aria-live="polite"></div>
<div id="termLimit" class="toolbar"><span>구독 한도</span><strong id="termLimitText">-</strong><button type="button" id="termLimitBtn" aria-pressed="false">한도 걸기</button></div>
<div class="listbar"><div class="chips" id="termChips" role="group" aria-label="상태 고르기"></div><button type="button" id="termAddOpen" aria-haspopup="dialog" aria-controls="termAdd">추가</button></div>
<table id="terminalsTbl" class="stack list"><thead><tr><th>이름</th><th>프로필</th><th>노드</th><th>상태</th><th>권한</th><th class="num">재시작</th><th>마지막 시작</th></tr></thead><tbody id="terminals"></tbody></table>
<div id="termHold" hidden><div id="termPane" class="termpane"><div class="toolbar termbar"><span id="termStateText" class="muted"></span><span id="termConn" class="muted" role="status" aria-live="polite"></span><button type="button" id="termMode" aria-pressed="false">입력</button><button type="button" id="termWide" aria-pressed="false" title="Esc: 원래 크기">넓히기</button></div><div id="termPaneMsg" class="msg" role="status" aria-live="polite"></div><div id="termScreen"></div></div></div>
<dialog id="termAdd" class="sc-drawer" aria-labelledby="termAddTitle"><div class="sc-drawer-head"><h3 id="termAddTitle" class="sc-drawer-title">터미널 추가</h3><button type="button" id="termAddClose" class="detail-close" aria-label="닫기">×</button></div><div class="sc-drawer-body">
<div id="termAddMsg" class="msg" role="status" aria-live="polite"></div>
<form id="termForm" class="settings-form termform">
<label>노드 <input id="termNode" required autocomplete="off" autocapitalize="off" spellcheck="false" value="local"></label>
<label>역할 <input id="termBase" required autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="team/worker"></label>
<label>프로필 <input id="termProfile" required autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="team-worker"></label>
<label>폴더 <input id="termDir" required autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="/home/me/work/app"></label>
<label>라벨 <input id="termLabel" autocomplete="off"></label>
<label>부모 <input id="termParent" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="team/boss"></label>
<label>묶음 <select id="termGroup"><option value="">없음</option></select></label>
<label>어댑터 <select id="termAdapter"><option value="">기본</option></select></label>
<label>재시작 <select id="termRestart"><option value="always">always</option><option value="on-failure">on-failure</option><option value="never">never</option></select></label>
<label>권한 <select id="termPerms"><option value="skip">건너뜀</option><option value="ask">물음</option></select></label>
<label>인자 <input id="termArgs" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="--model opus"></label>
<div class="row"><button type="submit">추가</button><button type="button" id="termClear">비우기</button></div>
</form>
</div></dialog>
</section>
</main>
<aside id="detail" class="detail" aria-labelledby="detailTitle" hidden>
<div class="detail-head"><button type="button" id="detailBack" class="detail-back"></button><h2 id="detailTitle" tabindex="-1"></h2><span id="detailBadge" class="sc-badge" hidden></span><button type="button" id="detailClose" class="detail-close" aria-label="닫기" title="닫기 (Esc)">×</button></div>
<nav id="detailTabs" class="sc-tabs" aria-label="상세 보기"></nav>
<div id="detailBody" class="detail-body"></div>
</aside>
</div>
</div>
<script>
const sessionLabel={idle:'유휴',busy:'바쁨',compacting:'compact 중',unknown:'모름',offline:'오프라인'};
function sessionStatus(st){
  const m=/^(error|blocked):(.*)$/.exec(st||'');
  if(m)return [(m[1]==='error'?'오류: ':'막힘: ')+m[2],'st-'+m[1]];
  return [sessionLabel[st]||st,'st-'+st];
}
// subagentNote is the text next to a session's state while background sub-agents run.
function subagentNote(n){return n>0?'서브 에이전트 '+n+'개 작업 중':''}
const statusLabel={pending:'안 읽음',read:'읽음',acknowledged:'확인함',complete:'완료',completed:'완료',held:'보관 중'};
const $=id=>document.getElementById(id);
// adminMode is the mode from /api/admin/me. Outside the proxy-secret
// transition mode a 401 means the session ended: reload to the login page.
let adminMode='';
// hostingOn is the hosting flag from /api/admin/me (terminal hosting, #173).
let hostingOn=false;
// termAssets are the terminal view's files, listed by /api/admin/me only with hosting on.
let termAssets=[];
// editorAssets are the file editor's (#448), listed with hosting on.
let editorAssets=[];
// termRefused is why this address cannot open a terminal ('' when it can).
let termRefused='';
function signedOut(){if(adminMode&&adminMode!=='legacy')location.reload()}
async function loadMe(){
  try{
    const r=await fetch('/api/admin/me',{credentials:'same-origin',headers:{'Accept':'application/json'}});
    if(!r.ok)return;
    const m=await r.json();adminMode=m.mode||'';hostingOn=m.hosting===true;termAssets=Array.isArray(m.terminal_assets)?m.terminal_assets:[];editorAssets=Array.isArray(m.editor_assets)?m.editor_assets:[];termRefused=typeof m.terminal_refused==='string'?m.terminal_refused:'';
    if(hostingOn)loadWorkspace(Array.isArray(m.workspace_assets)?m.workspace_assets:[]);
    $('tab-terminals').hidden=!hostingOn;setViewButton('nodes').hidden=!hostingOn;setViewButton('groups').hidden=!hostingOn;
    if(hostingOn&&curTab==='settings')route();
    if(hostingOn&&location.hash.startsWith('#terminal'))route();
    if(hostingOn&&curTab==='sessions')loadSessions();
    if(hostingOn&&detailCur)drawDetail();
    $('whoName').textContent=m.mode==='legacy'?'프록시':m.user;
    $('whoName').title=$('whoName').textContent;
    $('whoMark').textContent=[...$('whoName').textContent][0]||'';$('who').title=$('whoName').textContent;$('who').hidden=false;
    $('logout').hidden=m.mode==='legacy';
  }catch(e){}
}
$('logout').onclick=async()=>{
  $('logout').disabled=true;
  try{await fetch('/auth/logout',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-Switchboard-Dashboard':'1'},body:'{}'})}catch(e){}
  location.replace('/');
};
async function api(path){
  let r;
  try{r=await fetch('/api/admin/'+path,{credentials:'same-origin',headers:{'Accept':'application/json'}})}
  catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
  if(r.status===401)signedOut();
  if(r.redirected||!(r.headers.get('content-type')||'').includes('application/json'))throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
  if(!r.ok)throw Error(r.status===401?'로그인 필요(401)':'요청 실패('+r.status+')');
  return r.json();
}
// apiSend makes an admin write with the write guard headers. Errors carry the
// HTTP status (e.status) and the server's message.
async function apiSend(method,path,body){
  let r;
  try{r=await fetch('/api/admin/'+path,{method:method,credentials:'same-origin',headers:{'Accept':'application/json','Content-Type':'application/json','X-Switchboard-Dashboard':'1'},body:body===undefined?undefined:JSON.stringify(body)})}
  catch(e){throw Error('네트워크 오류 · 연결과 로그인 확인')}
  if(r.redirected)throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
  if(r.ok){
    if(!(r.headers.get('content-type')||'').includes('application/json'))throw Error('로그인 필요 · 다시 로그인한 뒤 새로 고침');
    return r.json();
  }
  if(r.status===401)signedOut();
  let raw='';try{raw=(await r.text()).trim()}catch(e){}
  const text=raw.slice(0,300);
  const e=Error(r.status===401?'로그인 필요(401)':r.status===403?'쓰기 거부(403) · 같은 주소에서 열었는지 확인':'요청 실패('+r.status+')'+(text?': '+text:''));
  e.status=r.status;e.body=raw;throw e;
}
function node(tag,text,cls){const n=document.createElement(tag);n.textContent=text;if(cls)n.className=cls;return n}
async function initMail(){
  try{for(const v of await api('roles'))$('role').append(new Option(v.role,v.role));await load()}
  catch(e){$('conn').textContent=e.message}
}
function kTokens(n){if(n===null||n===undefined)return '-';return n<10000?(n/1000).toFixed(1)+'k':Math.round(n/1000)+'k'}
function when(t){return t?new Date(t).toLocaleString('ko-KR'):'-'}
function byLabel(by){return by==='panel'?'세션 패널':by}
function ago(now,t){if(!t)return '-';const s=Math.max(0,Math.round((new Date(now)-new Date(t))/1000));return s<120?s+'초 전':s<7200?Math.round(s/60)+'분 전':Math.round(s/3600)+'시간 전'}
// ---- 상세 패널 ----
// A list row opens its item in the right panel (#detail, 440px; the whole
// screen below 720px). The hash is #<tab>/<id>/<sub> (id URI-encoded), so
// reload and back/forward restore the item and its sub-tab. A tab plugs in
// with registerDetail(tab,spec):
//   list   the list's name on the mobile back button (← 세션)
//   kinds  live change kinds that redraw the open item (default [tab])
//   load   async id => item, or null when it is gone
//   head   item => {title, badge:[text,tone]} (tone: success|info|warning|danger|neutral)
//   subs   item => [[key,label],...]; the first is the default sub-tab
//   draw   async (item,sub,box) => fills box with text nodes only
//   back   id => the hash closing goes to (optional; default #<tab>)
// List renderers call detailRow(tr,tab,id) on each row (Tab-reachable, Enter
// or click opens, the open row is aria-current). openDetail(tab,id,sub) and
// closeDetail() move the hash; drawDetail() redraws the open item. Esc
// closes; focus moves to the title on open and back to the row on close.
const details={};
let detailCur=null,detailSeq=0;
function registerDetail(tab,spec){details[tab]=spec}
function detailHash(tab,id,sub){return '#'+tab+'/'+encodeURIComponent(id)+(sub?'/'+sub:'')}
function openDetail(tab,id,sub){
  if(sub===undefined)sub=detailCur&&detailCur.tab===tab?detailCur.sub:'';
  const h=detailHash(tab,id,sub);
  if(location.hash===h)route();else location.hash=h;
}
function closeDetail(){if(detailCur){const b=details[detailCur.tab].back;location.hash=b?b(detailCur.id):detailCur.tab}}
function detailRow(tr,tab,id){
  id=String(id);tr.dataset.detailId=id;tr.tabIndex=0;
  if(detailCur&&detailCur.tab===tab&&detailCur.id===id)tr.setAttribute('aria-current','true');
  tr.addEventListener('click',e=>{if(!e.target.closest('a,button,input'))openDetail(tab,id)});
  tr.addEventListener('keydown',e=>{if(e.target===tr&&(e.key==='Enter'||e.key===' ')){e.preventDefault();openDetail(tab,id)}});
}
function detailRowFor(tab,id){
  for(const tr of $('panel-'+tab).querySelectorAll('tr[data-detail-id]'))if(tr.dataset.detailId===id)return tr;
  return null;
}
function markDetailRows(){
  for(const tr of document.querySelectorAll('tr[data-detail-id]')){
    const tab=tr.closest('[role=tabpanel]').id.slice(6);
    if(detailCur&&detailCur.tab===tab&&detailCur.id===tr.dataset.detailId)tr.setAttribute('aria-current','true');else tr.removeAttribute('aria-current');
  }
}
// keepRowFocus(tbody,fill) rebuilds a list with fill() and puts focus back on
// the row it was on, so a live reload does not drop the keyboard user.
function keepRowFocus(body,fill){
  const a=document.activeElement,id=a&&a.dataset&&body.contains(a)?a.dataset.detailId:undefined;
  fill();
  if(id!==undefined)for(const tr of body.querySelectorAll('tr[data-detail-id]'))if(tr.dataset.detailId===id){tr.focus();break}
}
function showDetail(tab,id,sub){
  const fresh=!detailCur||detailCur.tab!==tab||detailCur.id!==id;
  detailCur={tab:tab,id:id,sub:sub||''};
  $('appMain').classList.add('has-detail');$('detail').hidden=false;
  $('detailBack').textContent='← '+details[tab].list;
  markDetailRows();
  if(fresh){
    $('detailTitle').textContent=id;$('detailBadge').hidden=true;$('detailTabs').replaceChildren();
    $('detailBody').replaceChildren(node('p','불러오는 중…','muted'));$('detail').scrollTop=0;
    $('detailTitle').focus({preventScroll:true});
  }
  drawDetail();
}
function hideDetail(){
  const c=detailCur;if(!c)return;
  const a=document.activeElement,back=$('detail').contains(a)||a===document.body||!a;
  detailCur=null;detailSeq++;
  $('detail').hidden=true;$('appMain').classList.remove('has-detail');
  $('detailBody').replaceChildren();termGc();
  markDetailRows();
  const tr=detailRowFor(c.tab,c.id);
  if(back&&tr&&curTab===c.tab)tr.focus();
}
const toneClass={success:'sc-badge--success',info:'sc-badge--info',warning:'sc-badge--warning',danger:'sc-badge--danger',neutral:'sc-badge--neutral'};
async function drawDetail(){
  if(ws)ws.refresh();
  const c=detailCur;if(!c)return;
  const seq=++detailSeq,spec=details[c.tab],box=document.createElement('div');
  let head={title:c.id,badge:null},subs=[],sub=c.sub;
  try{
    const item=await spec.load(c.id);
    if(seq!==detailSeq)return;
    if(!item)box.append(node('p','없음 · 지워졌거나 바뀜','empty'));
    else{
      head=spec.head(item);subs=spec.subs(item);
      if(!subs.some(s=>s[0]===sub))sub=subs[0][0];
      await spec.draw(item,sub,box);
    }
  }catch(e){box.replaceChildren(node('p',e.message,'msg err'))}
  if(seq!==detailSeq)return;
  $('detailTitle').textContent=head.title;
  const b=$('detailBadge');b.hidden=!head.badge;
  if(head.badge){b.textContent=head.badge[0];b.className='sc-badge '+(toneClass[head.badge[1]]||toneClass.neutral)}
  // Redrawing replaces the sub-tab links and the body: keep focus where it was.
  const nav=$('detailTabs'),act=document.activeElement,onTab=nav.contains(act)?act.getAttribute('href'):null,inBody=$('detailBody').contains(act);
  nav.replaceChildren();
  for(const [k,label] of subs){
    const a=node('a',label);a.href=detailHash(c.tab,c.id,k);
    if(k===sub)a.setAttribute('aria-current','page');
    nav.append(a);
    if(onTab===a.getAttribute('href'))a.focus();
  }
  const top=$('detail').scrollTop;
  $('detailBody').replaceChildren(...box.childNodes);
  $('detail').scrollTop=top;
  if(inBody&&$('detailBody').contains(act))act.focus({preventScroll:true});
  else if(inBody||(onTab&&!nav.contains(document.activeElement)))$('detailTitle').focus({preventScroll:true});
  termGc();
}
$('detailClose').onclick=closeDetail;
$('detailBack').onclick=closeDetail;
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&detailCur&&!e.defaultPrevented){e.preventDefault();closeDetail()}});
function dl(pairs){
  const d=node('dl','','pdl');
  for(const [k,v] of pairs){const dd=document.createElement('dd');if(v instanceof Node)dd.append(v);else dd.textContent=v;d.append(node('dt',k),dd)}
  return d;
}
// ---- 세션 ----
const stTone={'st-idle':'success','st-busy':'info','st-compacting':'info','st-error':'danger','st-blocked':'warning'};
const sessionCols=['역할','상태','컨텍스트','마지막 턴','기계','판','라벨'];
// cliText is a session's CLI version (#308): '-' before it reported one,
// 판 모름 for no header or a development build.
function cliText(v){return !v.cli_version?'-':v.cli==='unknown'?'판 모름':v.cli_version}
const sessionChips=[['all','전체',()=>true],['busy','바쁨',v=>v.online&&/^(busy|compacting)$/.test(v.status)],['problem','오류·막힘',v=>/^(error|blocked):/.test(v.status||'')],['offline','오프라인',v=>!v.online]];
// sessionsSeq drops an answer that a later load (live update, hosting flag
// arriving from /api/admin/me) has overtaken.
let sessionsData=null,sessionsFilter='all',sessionsSeq=0;
async function loadSessions(){
  const seq=++sessionsSeq;
  $('sessionsNote').textContent='불러오는 중…';
  try{
    const p=await api('presence');
    const hosted=hostingOn?await hostedBySlot():new Map();
    if(seq!==sessionsSeq)return;
    sessionsData={p:p,hosted:hosted};
    drawSessions();
    $('sessionsNote').title='심장박동 '+p.interval_seconds+'초, '+p.missed+'번 놓치면 오프라인';
  }catch(e){if(seq===sessionsSeq)$('sessionsNote').textContent=e.message}
}
function drawSessions(){
  if(!sessionsData)return;
  const {p,hosted}=sessionsData,q=$('sessionsSearch').value.trim().toLowerCase();
  const chips=$('sessionChips');chips.replaceChildren();
  for(const [k,label,f] of sessionChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===sessionsFilter?'true':'false');
    b.append(node('span',String(p.sessions.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{sessionsFilter=k;drawSessions()};
    chips.append(b);
  }
  const keep=sessionChips.find(c=>c[0]===sessionsFilter)[2];
  const shown=p.sessions.filter(v=>keep(v)&&(!q||[v.role,v.machine,v.directory,v.label||''].some(s=>(s||'').toLowerCase().includes(q))));
  const body=$('sessions');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const v of shown){
      const [stText,stClass]=sessionStatus(v.status);
      const tr=node('tr','',!v.online?'offline':/^st-(error|blocked)$/.test(stClass)?'row'+stClass.slice(2):'');
      const cells=[v.role,stText,kTokens(v.context_tokens),ago(p.now,v.last_turn_at),v.machine,cliText(v),v.label||'-'];
      cells.forEach((text,i)=>{const td=node('td',text,i===1?stClass:i===2?'num nowrap':i===3?'nowrap':'');if(i>0)td.dataset.label=sessionCols[i];tr.append(td)});
      tr.children[3].title=when(v.last_turn_at);
      tr.children[5].classList.add('mono','nowrap');
      if(v.cli==='old'){tr.children[5].classList.add('cli-old');tr.children[5].append(' ',badge('옛 판','warning'))}
      if(v.subagents>0){const s=node('span','서브 '+v.subagents,'subs');s.title=subagentNote(v.subagents);tr.children[1].append(' · ',s)}
      const h=hosted.get(v.role);
      if(h){const a=node('a','터미널','hosted');a.href=detailHash('terminals',h.id,'screen');a.title=h.node+' · '+h.directory;tr.children[0].append(' ',a)}
      detailRow(tr,'sessions',v.role);
      body.append(tr);
    }
    if(!shown.length)emptyRow(body,sessionCols.length,'세션 없음');
  });
  $('sessionsNote').textContent=(shown.length===p.sessions.length?'':shown.length+' / ')+p.sessions.length+'개';
}
$('sessionsSearch').addEventListener('input',drawSessions);
registerDetail('sessions',{
  list:'세션',kinds:['sessions','mail','terminals'],
  load:async id=>{
    const p=await api('presence');const v=p.sessions.find(s=>s.role===id);
    if(!v)return null;
    return {v:v,now:p.now,roles:new Set(p.sessions.map(s=>s.role)),h:hostingOn?(await hostedBySlot()).get(id):null};
  },
  head:it=>{const [t,c]=sessionStatus(it.v.status);return {title:it.v.role,badge:[it.v.online?t:'오프라인',it.v.online?stTone[c]||'neutral':'neutral']}},
  subs:it=>[['status','상태'],['mail','메일']].concat(it.h?[['terminal','터미널']]:[]),
  draw:async(it,sub,box)=>{
    const v=it.v;
    if(sub==='mail'){
      const rows=(await api('messages?'+new URLSearchParams({role:v.role}))).slice(0,20);
      const ul=node('ul','','dlist');
      for(const m of rows){
        const li=node('li','');const a=node('a','#'+m.id+' '+m.subject);a.href=detailHash('mail',m.id,'body');
        li.append(a,node('small',m.sender+' → '+m.recipient+' · '+(statusLabel[m.status]||m.status)+' · '+when(m.created_at)));
        ul.append(li);
      }
      box.append(rows.length?ul:node('p','메일 없음','empty'));
      return;
    }
    if(sub==='terminal'){
      const a=node('a',termRole(it.h)+(it.h.label?' · '+it.h.label:''),'hosted');a.href=detailHash('terminals',it.h.id,'screen');a.title=it.h.node+' · '+it.h.directory;
      box.append(dl([['노드',it.h.node],['폴더',it.h.directory],['터미널',a]]),termPaneFor(it.h));
      return;
    }
    const [stText,stClass]=sessionStatus(v.status);
    const st=node('span',stText,stClass);
    let parent=v.parent||'-';
    if(v.parent&&it.roles.has(v.parent)){parent=node('a',v.parent);parent.href=detailHash('sessions',v.parent,'status')}
    const last=node('span',ago(it.now,v.last_turn_at));last.title=when(v.last_turn_at);
    box.append(dl([['상태',st],['접속',v.online?'온라인':'오프라인'],['서브 에이전트',v.subagents>0?subagentNote(v.subagents):'-'],['컨텍스트',kTokens(v.context_tokens)],['마지막 턴',last],['기계',v.machine||'-'],['판',cliText(v)+(v.cli==='old'?' · 옛 판':'')],['폴더',v.directory||'-'],['라벨',v.label||'-'],['부모',parent],['마지막 보고',ago(it.now,v.reported_at)]]));
  }
});
// ---- 메일 ----
// mailID is set by openMessage (진행 tab) to show one message; 「초기화」 or a
// new search clears it.
let mailID='';
function shortWhen(t){
  const d=new Date(t),n=new Date();
  return d.toDateString()===n.toDateString()?d.toLocaleTimeString('ko-KR',{hour:'2-digit',minute:'2-digit'}):d.toLocaleDateString('ko-KR',{month:'numeric',day:'numeric'});
}
const mailTone={pending:'warning',read:'neutral',acknowledged:'info',complete:'success',completed:'success',held:'neutral'};
const mailCols=['#','제목','보낸 쪽 → 받는 쪽','상태','시각'];
async function load(){
  $('conn').textContent='불러오는 중…';
  const thread=$('thread').value,task=$('task').value.trim(),id=mailID;
  $('notice').textContent=[id?'메일 #'+id:'',thread?'스레드 #'+thread:'',task?'task '+task:''].filter(Boolean).join(' · ')+(id||thread||task?'만 보는 중':'');
  try{
    const q=new URLSearchParams({box:$('box').value,role:$('role').value,status:$('status').value,q:$('search').value,thread:thread,task:task,id:id});
    const rows=await api('messages?'+q);
    const list=$('list');
    keepRowFocus(list,()=>{
      list.replaceChildren();
      for(const m of rows)list.append(mailRow(m));
      if(!rows.length)emptyRow(list,mailCols.length,'메일 없음');
    });
    $('conn').textContent=rows.length+'통 (최대 500통)';
  }catch(e){$('conn').textContent=e.message}
}
function mailRow(m){
  const tr=node('tr','',m.priority==='urgent'?'urgent':'');
  const subj=node('td','');
  if(m.priority==='urgent')subj.append(node('span','긴급','badge'));
  subj.append(node('strong',m.subject));
  if(m.task_id)subj.append(node('small','task '+m.task_id));
  const who=node('td',m.sender+' → '+m.recipient);
  if(m.addressed_to&&m.addressed_to!==m.recipient)who.append(node('small',m.addressed_to+' 앞'));
  const t=node('td',shortWhen(m.created_at),'nowrap');t.title=when(m.created_at);
  const cells=[node('td',String(m.id),'num nowrap'),subj,who,node('td',statusLabel[m.status]||m.status,'nowrap'),t];
  cells.forEach((td,i)=>{if(i>0)td.dataset.label=mailCols[i];tr.append(td)});
  detailRow(tr,'mail',m.id);
  return tr;
}
registerDetail('mail',{
  list:'메일',
  load:async id=>(await api('messages?'+new URLSearchParams({id:id})))[0]||null,
  head:m=>({title:'#'+m.id+' '+m.subject,badge:[statusLabel[m.status]||m.status,mailTone[m.status]||'neutral']}),
  subs:()=>[['body','본문'],['thread','스레드']],
  draw:async(m,sub,box)=>{
    const root=m.thread_id||m.id;
    if(sub==='thread'){
      const rows=(await api('messages?'+new URLSearchParams({thread:root}))).slice().reverse();
      const ul=node('ul','','dlist');
      for(const x of rows){
        const li=node('li','',x.id===m.id?'cur':'');
        const a=node('a','#'+x.id+' '+x.subject);a.href=detailHash('mail',x.id,'body');
        if(x.id===m.id)a.setAttribute('aria-current','true');
        const text=x.body.length>400?x.body.slice(0,400)+'…':x.body;
        li.append(a,node('small',x.sender+' → '+x.recipient+' · '+(statusLabel[x.status]||x.status)+' · '+when(x.created_at)),node('p',text));
        ul.append(li);
      }
      box.append(rows.length?ul:node('p','메일 없음','empty'));
      return;
    }
    const pairs=[['보낸 쪽',m.sender],['받는 쪽',m.recipient]];
    if(m.addressed_to&&m.addressed_to!==m.recipient)pairs.push(['주소',m.addressed_to+' 앞']);
    pairs.push(['시각',when(m.created_at)],['상태',statusLabel[m.status]||m.status]);
    if(m.priority==='urgent')pairs.push(['우선','긴급']);
    if(m.task_id)pairs.push(['task',m.task_id]);
    box.append(dl(pairs),node('pre',m.body));
    const foot=node('p','','row muted');
    foot.append(node('span',m.thread_id?'스레드 #'+m.thread_id+(m.reply_to?' · #'+m.reply_to+'에 대한 답장':''):'스레드 시작 메일'));
    const th=node('button','스레드 보기');
    th.onclick=()=>{if(curTab!=='mail'){openThread(root);return}$('thread').value=root;mailID='';load()};
    foot.append(th);
    box.append(foot);
  }
});
// ---- Tabs ----
// Each tab's loader runs once, the first time the tab is shown; after that the
// tab's own 「새로 고침」 button reloads it. #33 sets tabLoaders.permissions and
// #42 sets tabLoaders.questions / tabLoaders.settings (null = nothing to load).
const tabNames=['progress','sessions','mail','reports','jobs','questions','permissions','rules','metrics','terminals','settings'];
const tabLoaders={progress:null,sessions:loadSessions,reports:loadReports,jobs:loadJobs,metrics:null,mail:initMail,permissions:null,questions:null,rules:loadRules,settings:null};
const tabLoaded=new Set();
// setTabCount(name,n) shows n as a small count next to the tab name; n
// null/undefined hides it.
// #42 calls it for 'questions' (open questions), #33 for 'permissions' (pending requests),
// #51 for 'metrics' (alert thresholds crossed now; 0 hides the number).
function setTabCount(name,n){
  const t=$('tab-'+name);if(!t)return;
  t.querySelector('.tab-count').textContent=(n===null||n===undefined)?'':String(n);
  t.toggleAttribute('data-nonzero',!!n);
}
// ---- 지표 ----
// One GET /api/admin/metrics?days=N fills the four cards. metricsSeq drops a
// slow answer for a period the user has already switched away from.
let metricsDays=7,metricsSeq=0;
function dur(s){
  if(s===null||s===undefined)return '-';
  if(s<60)return s+'초';
  if(s<3600)return Math.round(s/60)+'분';
  if(s<86400){const h=s/3600;return (h<10?h.toFixed(1).replace(/\.0$/,''):Math.round(h))+'시간'}
  const d=s/86400;return (d<10?d.toFixed(1).replace(/\.0$/,''):Math.round(d))+'일';
}
const bucketLabel={'<10m':'<10분','<1h':'<1시간','<4h':'<4시간','<1d':'<1일','>=1d':'≥1일'};
function stateText(st){return sessionStatus(st)[0]}
// metricsBreaches counts what is over an alert threshold now: pending mail the
// server flagged, plus current error/blocked sessions past their limit (0 = off).
function metricsBreaches(m){
  const t=m.thresholds||{};
  let n=(m.pending||[]).filter(p=>p.over_threshold).length;
  for(const c of (m.sessions&&m.sessions.current)||[])if(sessionOver(c,t))n++;
  return n;
}
function sessionOver(c,t){
  const lim=/^error:/.test(c.state)?t.error_s:/^blocked:/.test(c.state)?t.blocked_s:0;
  return lim>0&&c.seconds>=lim;
}
function cells(tr,labels,values,classes){
  values.forEach((v,i)=>{const td=node('td',v,(classes&&classes[i])||'');if(i>0)td.dataset.label=labels[i];tr.append(td)});
  return tr;
}
function emptyRow(body,cols,text){const tr=node('tr','');const td=node('td',text,'muted');td.colSpan=cols;tr.append(td);body.append(tr)}
const SVG='http://www.w3.org/2000/svg';
function svgEl(tag,attrs,text){const e=document.createElementNS(SVG,tag);for(const k in attrs)e.setAttribute(k,attrs[k]);if(text!==undefined)e.textContent=text;return e}
// bucketChart draws buckets as bars, each with its count above and label below.
function bucketChart(buckets){
  const W=320,H=140,top=18,bottom=24,gap=10,n=Math.max(buckets.length,1);
  const bw=(W-gap*(n+1))/n,max=Math.max(1,...buckets.map(b=>b.count)),plot=H-top-bottom;
  const svg=svgEl('svg',{viewBox:'0 0 '+W+' '+H,role:'img','aria-label':'첫 답장 분포: '+buckets.map(b=>(bucketLabel[b.label]||b.label)+' '+b.count+'통').join(', ')});
  svg.append(svgEl('title',{},'첫 답장까지 걸린 시간 분포'));
  buckets.forEach((b,i)=>{
    const x=gap+i*(bw+gap),h=Math.round(plot*b.count/max),y=top+plot-h;
    const g=svgEl('g',{});
    g.append(svgEl('rect',{class:'bar',x:x,y:y,width:bw,height:Math.max(h,b.count?1:0)}));
    g.append(svgEl('text',{x:x+bw/2,y:y-4,'text-anchor':'middle'},String(b.count)));
    g.append(svgEl('text',{x:x+bw/2,y:H-8,'text-anchor':'middle'},bucketLabel[b.label]||b.label));
    svg.append(g);
  });
  svg.append(svgEl('line',{class:'axis',x1:0,x2:W,y1:top+plot+.5,y2:top+plot+.5}));
  return svg;
}
function statTile(label,value){const d=node('div','','stat');d.append(node('b',value),node('span',label));return d}
function renderFirstReply(f){
  $('mFirstStats').replaceChildren(statTile('스레드 시작',String(f.count)),statTile('답 받음',String(f.answered)),statTile('답 없음',String(f.unanswered)),statTile('p50',dur(f.p50_s)),statTile('p90',dur(f.p90_s)),statTile('최장',dur(f.max_s)));
  $('mFirstChart').replaceChildren(f.count?bucketChart(f.buckets||[]):node('p','이 기간 스레드 없음','empty'));
  const body=$('mFirstByRecipient');body.replaceChildren();
  const labels=['받는 쪽','메일','답 받음','답 없음','p50','p90','최장'],num=['','num','num','num','num','num','num'];
  for(const r of f.by_recipient||[])body.append(cells(node('tr',''),labels,[r.base,String(r.count),String(r.answered),String(r.unanswered),dur(r.p50_s),dur(r.p90_s),dur(r.max_s)],num));
  if(!body.children.length)emptyRow(body,7,'받는 쪽 없음');
}
function renderPending(list,t){
  $('mPendingNote').textContent=t.pending_s>0?'문턱 '+dur(t.pending_s):'알림 꺼짐';
  const body=$('mPending');body.replaceChildren();
  for(const p of list){
    const tr=cells(node('tr','',p.over_threshold?'over':''),['역할','안 읽음','가장 오래된 것'],[p.role,p.pending+'통',dur(p.oldest_s)+(p.over_threshold?' · 문턱 넘음':'')],['','num','num']);
    body.append(tr);
  }
  if(!list.length)emptyRow(body,3,'안 읽힌 메일 없음');
}
function renderThreads(th){
  $('mThreadsNote').textContent=th.multi_round?'스레드 '+th.multi_round+'개':'';
  const ol=$('mThreads');ol.replaceChildren();
  for(const x of th.top||[]){
    const li=node('li',''),b=node('button','');
    b.type='button';
    b.append(document.createTextNode('#'+x.thread_id+' '+(x.subject||'(제목 없음)')),node('small',(x.parties||[]).join(' ↔ ')+' · 왕복 '+x.round_trips+'번'));
    b.onclick=()=>openThread(x.thread_id);
    li.append(b);ol.append(li);
  }
  if(!ol.children.length)ol.append(node('li','되물은 스레드 없음','empty'));
}
function renderSessionStates(ss,t){
  const lim=[t.error_s>0?'오류 '+dur(t.error_s):'',t.blocked_s>0?'막힘 '+dur(t.blocked_s):''].filter(Boolean);
  $('mSessionsNote').textContent=lim.length?'문턱 '+lim.join(' · '):'알림 꺼짐';
  const body=$('mStates');body.replaceChildren();
  for(const r of ss.by_role||[]){
    const kinds=(r.top_kinds||[]).map(k=>stateText(k.state)+' '+dur(k.seconds)).join(', ')||'-';
    body.append(cells(node('tr',''),['역할','오류','막힘','많았던 상태'],[r.role,dur(r.error_s),dur(r.blocked_s),kinds],['','num','num','wrap']));
  }
  if(!body.children.length)emptyRow(body,4,'오류·막힘 없음');
  const cur=$('mCurrent');cur.replaceChildren();
  for(const c of ss.current||[]){
    const over=sessionOver(c,t),[text,cls]=sessionStatus(c.state);
    cur.append(cells(node('tr','',over?'over':''),['역할','상태','시작','지난 시간'],[c.role,text,when(c.since),dur(c.seconds)+(over?' · 문턱 넘음':'')],['',cls,'','num']));
  }
  if(!cur.children.length)emptyRow(cur,4,'지금 오류·막힘 없음');
}
// renderAliases shows old-name use since the server started (#72).
function renderAliases(a){
  a=a||{};const e=a.old_env||[];
  $('mAliases').textContent='옛 이름 환경 변수: '+(e.join(', ')||'없음');
}
async function loadMetrics(){
  const seq=++metricsSeq;
  $('metricsNote').textContent='불러오는 중…';
  try{
    const m=await api('metrics?days='+metricsDays);
    if(seq!==metricsSeq)return;
    const t=m.thresholds||{};
    renderFirstReply(m.first_reply);renderPending(m.pending||[],t);renderThreads(m.threads||{multi_round:0,top:[]});renderSessionStates(m.sessions||{},t);renderAliases(m.aliases);
    const n=metricsBreaches(m);setTabCount('metrics',n||null);
    $('metricsNote').textContent='최근 '+m.window.days+'일 · '+when(m.window.from)+' ~ '+when(m.window.to)+(n?' · 문턱 넘은 것 '+n+'개':'');
  }catch(e){if(seq===metricsSeq)$('metricsNote').textContent=e.message}
}
// refreshMetricsCount fills the tab number before the tab is first opened.
async function refreshMetricsCount(){
  try{const m=await api('metrics?days=1');if(!tabLoaded.has('metrics')){const n=metricsBreaches(m);setTabCount('metrics',n||null)}}catch(e){}
}
function setMetricsDays(d){
  metricsDays=d;
  for(const b of [$('metDays7'),$('metDays30')])b.setAttribute('aria-pressed',Number(b.dataset.days)===d?'true':'false');
  loadMetrics();
}
// openThread shows the 메일 tab filtered to one thread (the 「스레드 보기」 filter),
// clearing the other filters so the whole thread shows.
function openThread(id){openMail({thread:id})}
// openMail shows the 메일 tab with only the given filter (thread, task or id)
// set and every other filter cleared.
function openMail(f){
  $('thread').value=f.thread||'';$('task').value=f.task||'';mailID=f.id?String(f.id):'';
  $('search').value='';$('status').value='';$('role').value='';$('box').value='all';
  const fresh=!tabLoaded.has('mail');
  if(f.id)openDetail('mail',String(f.id),'body');else selectTab('mail');
  if(!fresh)load();
  window.scrollTo({top:0});
}
function openMessage(id){openMail({id:id})}
// ---- 진행 ----
// One load reads GET /api/admin/progress with the pending questions and
// permission requests. The list is 대기 (questions, permission requests,
// sessions in error or blocked, directives over the pending alert threshold,
// directives answered but not completed whose sessions sit idle (멈춤),
// what answers that ended directives left undone (남긴 것, #470: a second
// line with the directive and [이슈] [새 지시] [버림]), in that order of
// kind, oldest first within a kind) and then a row per service. Rows open in the detail panel under #progress/<kind>:<id>:
// q: 질문, p: 권한, s: 세션, d: 지시, m: 멈춤 (the directive), svc: 서비스. The tab number is the
// open directives older than the pending alert threshold (0 hides it).
const badgeLabel={error:'오류',blocked:'막힘',busy:'바쁨',compacting:'compact 중',idle:'유휴',none:'세션 없음'};
const badgeTone={error:'danger',blocked:'warning',busy:'info',compacting:'info',idle:'success',none:'neutral'};
function rel(now,t){if(!t)return '-';const s=Math.max(0,Math.round((new Date(now)-new Date(t))/1000));return s<60?'방금':dur(s)+' 전'}
function secsSince(now,t){return Math.max(0,Math.round((new Date(now)-new Date(t))/1000))}
const waitKinds=[['q','질문','warning'],['p','권한','info'],['s','오류','danger'],['d','지시','neutral'],['m','멈춤','warning'],['l','남긴 것','warning'],['v','옛 판','warning']];
const waitCols=['종류','항목','역할','지난 시간'];
const svcCols=['서비스','상태','세션','열린 지시','남긴 것','마지막 소식'];
// recipLabel is an open directive's recipient state: offline apart from idle (#269).
const recipLabel={offline:'오프라인',none:'오프라인'};
function recipWho(x){const l=recipLabel[x.recipient_state]||badgeLabel[x.recipient_state];return x.to+(l&&x.recipient_state!=='busy'?' · '+l:'')}
const svcChips=[['all','전체',()=>true],['problem','문제 있음',p=>/^(error|blocked)$/.test(p.badge)],['stale','오래된 지시',p=>p.open_directives.some(d=>d.over_threshold)]];
let progressSeq=0,progressData=null,svcFilter='all';
// waitItems builds the 대기 rows: {kind,id,item,role,age} with age in seconds.
function waitItems(d){
  const now=d.r.generated_at,by={q:[],p:[],s:[],d:[],m:[],l:[],v:[]};
  for(const x of d.r.left||[])by.l.push({id:String(x.id),item:x.kind==='unrecorded'?'적지 않음':x.text,role:x.recorded_by,age:secsSince(now,x.recorded_at),left:x});
  for(const x of d.r.cli_behind||[])by.v.push({id:x.machine,item:x.machine+' · '+(x.version==='unknown'?'판 모름':x.version),role:x.roles.join(', '),age:null});
  for(const q of d.questions)by.q.push({id:q.id,item:q.questions.map(x=>x.header).join(' · '),role:q.asker,age:secsSince(now,q.created_at)});
  for(const v of d.perms)by.p.push({id:v.id,item:v.tool+(v.summary?' · '+v.summary:''),role:v.requester,age:secsSince(now,v.created_at)});
  for(const p of d.r.services){
    for(const v of p.sessions){const m=/^(error|blocked):(.*)$/.exec(v.state||'');if(m)by.s.push({id:v.role,item:m[2]||badgeLabel[m[1]],role:v.role,age:secsSince(now,v.since),tone:m[1]==='blocked'?'warning':'danger',label:badgeLabel[m[1]]})}
    for(const x of p.open_directives)if(x.over_threshold)by.d.push({id:String(x.id),item:'#'+x.id+' '+(x.subject||'(제목 없음)'),role:recipWho(x),age:x.age_s});
    for(const x of p.stalled||[])by.m.push({id:String(x.id),item:'#'+x.id+' '+(x.subject||'(제목 없음)'),role:x.to,age:x.idle_s});
  }
  const out=[];
  for(const [k,label,tone] of waitKinds)for(const x of by[k].sort((a,b)=>(b.age||0)-(a.age||0)))out.push(Object.assign({kind:k,label:label,tone:tone},x));
  return out;
}
async function loadProgress(){
  const seq=++progressSeq;
  $('progressNote').textContent='불러오는 중…';
  try{
    const [r,q,pm]=await Promise.all([api('progress'),api('questions?status=pending&limit=500'),api('permissions?status=pending')]);
    if(seq!==progressSeq)return;
    progressData={r:r,questions:q.questions,perms:pm.permissions};
    drawProgress();
  }catch(e){if(seq===progressSeq)$('progressNote').textContent=e.message}
}
// cell(text,label,cls) is one list cell; label shows before it in the stacked layout.
function cell(text,label,cls){const td=node('td',text,cls||'');if(label)td.dataset.label=label;return td}
// cardBreak ends the first line of a two-line card row below 600px.
function cardBreak(){const td=node('td','','cardbr');td.setAttribute('aria-hidden','true');return td}
function badge(text,tone){return node('span',text,'sc-badge '+(toneClass[tone]||toneClass.neutral))}
function drawProgress(){
  const d=progressData;if(!d)return;
  const r=d.r,now=r.generated_at,items=waitItems(d);
  const wait=$('wait');
  keepRowFocus(wait,()=>{
    wait.replaceChildren();
    for(const x of items){
      const tr=node('tr','','w-'+x.kind);
      const k=cell('');k.append(badge(x.label,x.tone));
      const it=cell(x.item,'','witem');
      if(x.left)it.append(leftLine(x.left));
      tr.append(k,it,cardBreak(),cell(x.role,'','mono'),cell(x.age==null?'-':dur(x.age),'','num nowrap'));
      if(x.kind!=='v'&&x.kind!=='l')detailRow(tr,'progress',x.kind+':'+x.id);
      wait.append(tr);
    }
    if(!items.length)emptyRow(wait,waitCols.length,'대기 없음');
  });
  $('waitCount').textContent=String(items.length);
  const chips=$('svcChips');chips.replaceChildren();
  for(const [k,label,f] of svcChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===svcFilter?'true':'false');
    b.append(node('span',String(r.services.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{svcFilter=k;drawProgress()};
    chips.append(b);
  }
  const keep=svcChips.find(c=>c[0]===svcFilter)[2],shown=r.services.filter(keep);
  const body=$('progress');
  let over=0;
  for(const p of r.services)over+=p.open_directives.filter(d=>d.over_threshold).length;
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const p of shown){
      const tr=node('tr','',/^(error|blocked)$/.test(p.badge)?'row-'+p.badge:'');
      const st=cell('');st.append(badge(badgeLabel[p.badge]||p.badge,badgeTone[p.badge]||'neutral'));
      const stale=p.open_directives.filter(d=>d.over_threshold).length;
      const dirs=cell(String(p.open_directives.length),'열린 지시','num nowrap');
      if(stale){dirs.classList.add('over');dirs.title='문턱('+dur(r.pending_alert_s)+') 넘음 '+stale+'개'}
      const rep=p.last_report;
      const news=cell(rep?(rep.subject||'(제목 없음)')+' · '+rel(now,rep.at):p.last_activity_at?'보고 없음 · 마지막 활동 '+rel(now,p.last_activity_at):'소식 없음','마지막 소식','muted');
      if(rep)news.title=rep.from+' → '+rep.to+' · '+when(rep.at);
      tr.append(cell(p.service,'','strong'),st,cardBreak(),cell(String(p.sessions.length),'세션','num nowrap'),dirs,cell(String(p.left||0),'남긴 것','num nowrap'),news);
      detailRow(tr,'progress','svc:'+p.service);
      body.append(tr);
    }
    if(!shown.length)emptyRow(body,svcCols.length,'서비스 없음');
  });
  setTabCount('progress',over||null);
  $('progressNote').textContent='대기 '+items.length+' · '+r.services.length+'개 서비스'+(over?' · 오래된 지시 '+over+'개':'');
  $('progressNote').title=when(r.generated_at);
}
// leftLine is the second line of a 남긴 것 row (#470): the directive it came
// from and its three ends. 이슈 and 새 지시 do what the directive's sender
// does with left issue and left pass; 버림 is the admin's alone, with a reason.
function leftLine(v){
  const box=node('div','','wsub'),msg=node('span','','msg');msg.setAttribute('role','status');
  const dir=setButton('지시 #'+v.directive,'linkish',()=>openMessage(v.directive));
  if(v.directive_subject)dir.title=v.directive_subject;
  const id=encodeURIComponent(v.id);
  const end=(label,send,ask,body)=>setButton(label,'',async e=>{
    const a=prompt(ask,'');if(a===null)return;
    const sent=body(a.trim());if(!sent){setMsg(msg,ask,'err');return}
    e.target.disabled=true;setMsg(msg,'보내는 중…');
    try{await send(sent);loadProgress()}
    catch(err){e.target.disabled=false;setMsg(msg,err.message,'err')}
  });
  box.append(dir,
    end('이슈',b=>apiSend('POST','left/'+id+'/issue',b),'#'+v.id+' 이슈 주소',a=>/^https?:\/\/\S+$/.test(a)?{url:a}:null),
    end('새 지시',b=>apiSend('POST','left/'+id+'/pass',b),'#'+v.id+'을(를) 넘길 지시 번호 ('+v.directive_from+'이(가) 보낸 지시)',a=>/^[1-9][0-9]*$/.test(a)?{directive:Number(a)}:null),
    end('버림',b=>apiSend('POST','left/'+id+'/drop',b),'#'+v.id+' 버리는 까닭',a=>a?{reason:a}:null),
    msg);
  return box;
}
// The 진행 detail hands each kind to its own spec: q to 질문, p to 권한 요청,
// s to 세션, d (지시: the mail thread, with 닫기) and svc (서비스) are here.
const progressDirective={
  load:async id=>{
    const [m,r]=await Promise.all([details.mail.load(id),api('progress')]);
    if(!m)return null;
    let open=null,stall=null;
    for(const p of r.services){for(const d of p.open_directives)if(String(d.id)===id)open=d;for(const d of p.stalled||[])if(String(d.id)===id)stall=d}
    return {m:m,open:open,stall:stall,limit:r.pending_alert_s,now:r.generated_at};
  },
  head:x=>x.open?{title:'#'+x.m.id+' '+x.m.subject,badge:[x.open.over_threshold?'오래된 지시':'열림',x.open.over_threshold?'danger':'warning']}:x.stall?{title:'#'+x.m.id+' '+x.m.subject,badge:['멈춤','warning']}:details.mail.head(x.m),
  subs:()=>[['thread','스레드'],['body','본문']],
  draw:async(x,sub,box)=>{
    if(x.open){
      const row=node('div','','row'),msg=node('span','','msg');msg.setAttribute('role','status');
      const close=node('button','닫기');close.type='button';close.title='끝난 것으로 닫기';
      close.onclick=async()=>{
        if(!confirm('#'+x.m.id+' 지시를 닫을까요?'))return;
        close.disabled=true;setMsg(msg,'닫는 중…');
        try{await apiSend('POST','messages/'+encodeURIComponent(x.m.id)+'/close');setMsg(msg,'닫음','ok');loadProgress();drawDetail()}
        catch(e){close.disabled=false;setMsg(msg,e.message,'err')}
      };
      row.append(close,msg);
      box.append(dl([['받는 쪽',recipWho(x.open)],['상태',statusLabel[x.open.status]||x.open.status],['지난 시간',dur(x.open.age_s)+(x.open.over_threshold?' · 문턱('+dur(x.limit)+') 넘음':'')]].concat(x.open.replied_at?[['마지막 답',rel(x.now,x.open.replied_at)]]:[],x.stall?[['idle 세션',x.stall.sessions.join(', ')],['idle',dur(x.stall.idle_s)]]:[])),row);
    }
    if(x.stall&&!x.open)box.append(dl([['받는 쪽',x.stall.to]].concat(x.stall.answered_at?[['마지막 답',rel(x.now,x.stall.answered_at)]]:[],[['idle 세션',x.stall.sessions.join(', ')],['idle',dur(x.stall.idle_s)]])));
    await details.mail.draw(x.m,sub,box);
  }
};
const progressService={
  load:async id=>{const r=await api('progress');const p=r.services.find(s=>s.service===id);return p?{p:p,now:r.generated_at,limit:r.pending_alert_s}:null},
  head:x=>({title:x.p.service,badge:[badgeLabel[x.p.badge]||x.p.badge,badgeTone[x.p.badge]||'neutral']}),
  subs:x=>[['sessions','세션 '+x.p.sessions.length],['directives','열린 지시 '+x.p.open_directives.length],['news','마지막 소식']],
  draw:async(x,sub,box)=>{
    const p=x.p,now=x.now;
    if(sub==='directives'){
      const ul=node('ul','','dlist');
      for(const d of p.open_directives){
        const li=node('li','',d.over_threshold?'over':'');const a=node('a','#'+d.id+' '+(d.subject||'(제목 없음)'));a.href=detailHash('progress','d:'+d.id,'thread');
        li.append(a,node('small',recipWho(d)+' · '+(statusLabel[d.status]||d.status)+(d.replied_at?' · 답 있음':'')+' · '+dur(d.age_s)+' 지남'+(d.over_threshold?' · 문턱('+dur(x.limit)+') 넘음':'')));
        ul.append(li);
      }
      box.append(p.open_directives.length?ul:node('p','열린 지시 없음','empty'));
      return;
    }
    if(sub==='news'){
      const r=p.last_report;
      if(r){
        const a=node('a','#'+r.id+' '+(r.subject||'(제목 없음)'));a.href=detailHash('mail',r.id,'body');
        box.append(dl([['보고',a],['보낸 쪽',r.from+' → '+r.to],['시각',when(r.at)+' · '+rel(now,r.at)],['마지막 활동',p.last_activity_at?when(p.last_activity_at):'-']]));
      }else box.append(dl([['보고','보고 없음'],['마지막 활동',p.last_activity_at?when(p.last_activity_at)+' · '+rel(now,p.last_activity_at):'소식 없음']]));
      return;
    }
    const ul=node('ul','','dlist');
    for(const v of p.sessions){
      const li=node('li',''),[text,cls]=sessionStatus(v.state);
      const a=node('a',v.role);a.href=detailHash('progress','s:'+v.role,'status');
      const line=node('small','');
      line.append(node('span',text,cls),...(subagentNote(v.subagents)?[document.createTextNode(' · '),node('span',subagentNote(v.subagents),'subs')]:[]),document.createTextNode(' · '+dur(secsSince(now,v.since))+'째'));
      li.append(a,line);
      // 폴더는 #346처럼 앞쪽을 줄임표로 줄이고 전체 경로는 툴팁으로.
      if(v.directory){const d=node('span','‎'+v.directory+'‎','pdir');d.title=v.directory;li.append(d)}
      ul.append(li);
    }
    box.append(p.sessions.length?ul:node('p','세션 없음','empty'));
  }
};
const progressKinds={q:'questions',p:'permissions',s:'sessions',d:progressDirective,m:progressDirective,svc:progressService};
function progressSpec(k){const v=progressKinds[k];return typeof v==='string'?details[v]:v}
registerDetail('progress',{
  list:'진행',kinds:['progress','questions','permissions','sessions','mail'],
  load:async id=>{
    const i=id.indexOf(':'),spec=i>0?progressSpec(id.slice(0,i)):null;
    if(!spec)return null;
    const item=await spec.load(id.slice(i+1));
    return item?{spec:spec,item:item}:null;
  },
  head:x=>x.spec.head(x.item),
  subs:x=>x.spec.subs(x.item),
  draw:(x,sub,box)=>x.spec.draw(x.item,sub,box)
});
tabLoaders.progress=loadProgress;
tabLoaders.metrics=loadMetrics;
// ---- 질문 ----
// The list is one row per question; a row opens the detail (#questions/<id>/
// answer|decisions). 답하기 holds the answer form while the question is
// pending and the admin may answer, else the stored answer. A form is kept
// per question across live redraws so a half-filled answer survives them.
const qStatusLabel={pending:'대기',answered:'답함',withdrawn:'거둠'};
const qTone={pending:'warning',answered:'success',withdrawn:'neutral'};
const questionCols=['상태','질문','묻는 쪽','시각'];
let formSeq=0;
const qForms=new Map(),qNotes=new Map();
function setMsg(el,text,kind){el.textContent=text;el.className='msg'+(kind?' '+kind:'')}
function qTitle(q){return q.questions.map(x=>x.header).join(' · ')}
async function refreshQuestionCount(){
  try{setTabCount('questions',(await api('questions?status=pending&limit=500')).questions.length)}catch(e){}
}
async function loadQuestions(){
  $('questionsNote').textContent='불러오는 중…';
  try{
    const list=(await api('questions?limit=200')).questions,body=$('questions');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const q of list)body.append(questionRow(q));
      if(!list.length)emptyRow(body,questionCols.length,'질문 없음');
    });
    const open=list.filter(q=>q.status==='pending').length;
    setTabCount('questions',open);
    $('questionsNote').textContent='대기 '+open+'개 · 모두 '+list.length+'개(최대 200개)';
  }catch(e){$('questionsNote').textContent=e.message}
}
function questionRow(q){
  const tr=node('tr','',q.status==='pending'?'':'done');
  const st=cell('');st.append(badge(qStatusLabel[q.status]||q.status,qTone[q.status]||'neutral'));
  const t=cell('',questionCols[1]);
  questionPills(q,t);
  t.append(node('strong',qTitle(q)));
  const at=cell(shortWhen(q.created_at),questionCols[3],'nowrap');at.title=when(q.created_at);
  tr.append(st,t,cell(q.asker,questionCols[2]),at);
  detailRow(tr,'questions',q.id);
  return tr;
}
// questionPills adds the 대표 and 전달 대기 marks to h.
function questionPills(q,h){
  if(q.answerer==='owner')h.append(node('span','대표','pill owner'));
  if(q.delivery==='waiting')h.append(node('span','전달 대기','pill waiting'));
}
async function findQuestion(id){
  let q=(await api('questions?limit=200')).questions.find(x=>x.id===id);
  if(!q)q=(await api('questions?status=pending&limit=500')).questions.find(x=>x.id===id);
  return q||null;
}
registerDetail('questions',{
  list:'질문',kinds:['questions'],
  load:async id=>{
    const q=await findQuestion(id);
    if(!q)return null;
    let decs=[];
    try{decs=(await api('decisions?'+new URLSearchParams({role:q.asker}))).decisions.filter(d=>d.id!==q.id)}catch(e){}
    return {q:q,decs:decs};
  },
  head:x=>({title:qTitle(x.q),badge:[qStatusLabel[x.q.status]||x.q.status,qTone[x.q.status]||'neutral']}),
  subs:x=>[['answer',x.q.status==='pending'&&x.q.can_answer?'답하기':'답'],['decisions','관련 결정 '+x.decs.length]],
  draw:async(x,sub,box)=>{
    const q=x.q;
    if(sub==='decisions'){
      for(const d of x.decs.slice(0,20))box.append(decisionRow(d));
      if(!x.decs.length)box.append(node('p','관련 결정 없음','empty'));
      return;
    }
    const pairs=[['묻는 쪽',q.asker],['시각',when(q.created_at)],['ID',q.id]];
    if(q.relay_to&&q.relay_to.length)pairs.push(['중계',q.relay_to.join(', ')]);
    const marks=node('span','');questionPills(q,marks);
    if(marks.childNodes.length)pairs.push(['표시',marks]);
    box.append(dl(pairs));
    const note=qNotes.get(q.id);
    if(note)box.append(node('p',note,'msg err'));
    if(q.status==='pending'&&q.can_answer){
      let f=qForms.get(q.id);
      if(!f){f=answerForm(q);qForms.set(q.id,f)}
      box.append(f);
      return;
    }
    qForms.delete(q.id);
    box.append(...questionAnswered(q));
  }
});
// questionAnswered is the read-only view: each item with the stored answer,
// or the options while it is still pending.
function questionAnswered(q){
  const out=[];
  q.questions.forEach((item,i)=>{
    const f=node('div','','qitem');
    const head=node('div','','qhead');head.append(node('span',item.header,'pill'));
    f.append(head,node('p',item.question,'qtext'));
    const a=q.answers&&q.answers[i];
    if(a){
      const sel=(a.selected||[]).join(', ');
      f.append(node('p','답: '+(sel||'(선택 없음)'),'qans'));
      if(a.other)f.append(node('p','기타: '+a.other,'qans'));
    }else if(q.status==='pending'){
      for(const o of item.options)f.append(node('p','· '+o.label+(o.description?' — '+o.description:''),'qans muted'));
    }
    out.push(f);
  });
  if(q.status==='answered')out.push(node('p','답한 쪽: '+byLabel(q.answered_by)+' · '+when(q.answered_at),'muted'));
  if(q.status==='withdrawn')out.push(node('p','거둔 때: '+when(q.withdrawn_at),'muted'));
  return out;
}
function answerForm(q){
  const form=node('form','','qform');
  const n=++formSeq;
  const items=q.questions.map((item,i)=>{
    const f=node('fieldset','','qitem');
    const lg=node('legend','');lg.append(node('span',item.header,'pill'),document.createTextNode(item.multiSelect?' 여러 개 고름':' 하나 고름'));
    f.append(lg,node('p',item.question,'qtext'));
    const inputs=[];
    for(const o of item.options){
      const lab=node('label','','qopt');
      const inp=document.createElement('input');
      inp.type=item.multiSelect?'checkbox':'radio';inp.name='q'+n+'-'+i;inp.value=o.label;
      inputs.push(inp);
      const t=node('span','');t.append(node('strong',o.label));
      if(o.description)t.append(document.createTextNode(' — '+o.description));
      if(o.preview)t.append(node('pre',o.preview));
      lab.append(inp,t);f.append(lab);
    }
    const ol=node('label','','qother');
    const other=document.createElement('input');other.type='text';other.maxLength=1000;other.placeholder='고른 것 대신, 또는 덧붙여 쓸 말';
    ol.append(node('span','기타'),other);f.append(ol);
    form.append(f);
    return {item,inputs,other};
  });
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const send=node('button','답 보내기');send.type='submit';
  const row=node('div','','row');row.append(send);
  form.append(row,msg);
  form.onsubmit=async e=>{
    e.preventDefault();
    const answers=items.map(x=>{
      const a={selected:x.inputs.filter(i=>i.checked).map(i=>i.value)};
      const o=x.other.value.trim();if(o)a.other=o;
      return a;
    });
    const missing=items.findIndex((x,i)=>!answers[i].selected.length&&!answers[i].other);
    if(missing>=0){setMsg(msg,'「'+items[missing].item.header+'」에 고르거나 기타를 쓰세요.','err');return}
    send.disabled=true;setMsg(msg,'보내는 중…');
    try{
      await apiSend('POST','questions/'+encodeURIComponent(q.id)+'/answer',{answers:answers});
      setMsg($('questionsMsg'),'답을 보냈습니다: '+q.id,'ok');
    }catch(err){
      send.disabled=false;
      if(err.status!==409){setMsg(msg,err.message,'err');return}
      qNotes.set(q.id,'이미 답했거나 거둔 질문입니다(409)');
    }
    qForms.delete(q.id);
    // The answer form had focus: move it to the title before the redraw.
    $('detailTitle').focus({preventScroll:true});
    drawDetail();
    if(curTab==='questions')loadQuestions();
  };
  return form;
}
tabLoaders.questions=loadQuestions;
// ---- 보고 ----
// GET /api/admin/reports lists the boss roles' reports, newest first. The
// status is the server's: 진행 중 while a linked directive is open under the
// answered rule, else 완료. 진행 중 carries the count behind it, 열린 지시
// open/linked (#275). The count on the menu is the 진행 중 reports.
const reportCols=['상태','제목','보낸 쪽','시각'];
const reportStatus={open:['진행 중','info'],done:['완료','success']};
function reportBadge(r){
  const [label,tone]=reportStatus[r.status]||[r.status,'neutral'];
  return [r.status==='open'?label+' · 열린 지시 '+(r.open_directives||0)+'/'+(r.directives||0):label,tone];
}
const reportChips=[['all','전체',()=>true],['open','진행 중',r=>r.status==='open'],['done','완료',r=>r.status==='done']];
let reportsData=null,reportsFilter='all',reportsSeq=0;
async function refreshReportCount(){
  try{setTabCount('reports',(await api('reports')).reports.filter(r=>r.status==='open').length||null)}catch(e){}
}
async function loadReports(){
  const seq=++reportsSeq;
  $('reportsNote').textContent='불러오는 중…';
  try{
    const list=(await api('reports')).reports;
    if(seq!==reportsSeq)return;
    reportsData=list;
    setTabCount('reports',list.filter(r=>r.status==='open').length||null);
    drawReports();
  }catch(e){if(seq===reportsSeq)$('reportsNote').textContent=e.message}
}
function drawReports(){
  const list=reportsData;if(!list)return;
  const chips=$('reportChips');chips.replaceChildren();
  for(const [k,label,f] of reportChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===reportsFilter?'true':'false');
    b.append(node('span',String(list.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{reportsFilter=k;drawReports()};
    chips.append(b);
  }
  const keep=reportChips.find(c=>c[0]===reportsFilter)[2],shown=list.filter(keep),body=$('reports');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const r of shown)body.append(reportRow(r));
    if(!shown.length)emptyRow(body,reportCols.length,'보고 없음');
  });
  $('reportsNote').textContent=(shown.length===list.length?'':shown.length+' / ')+list.length+'개';
}
function reportRow(r){
  const [label,tone]=reportBadge(r);
  const tr=node('tr','',r.status==='open'?'':'done');
  const st=cell('');st.append(badge(label,tone));
  const t=cell('',reportCols[1],'wrap');t.append(node('strong',r.title));
  if(r.mail&&r.mail.length)t.append(' ',node('span','지시 '+r.mail.length,'muted'));
  const at=cell(shortWhen(r.created_at),reportCols[3],'nowrap');at.title=when(r.created_at);
  tr.append(st,t,cell(r.author,reportCols[2]),at);
  detailRow(tr,'reports',r.id);
  return tr;
}
// ---- 작업 (#291) ----
// Jobs, watches and schedules (#292) of every role: a live job has 중지, an
// ended job and every schedule 지우기, each behind a confirm. The list shows
// labels only; a row opens the detail (#jobs/job:<id> or sched:<id>, #404),
// which loads that one item with its command or prompt (the server records
// the read).
const jobCols=['종류','이름','역할','기계','상태','시작','만료','마지막 줄',''];
const jobKinds={command:'명령',watch:'감시',cron:'예약',once:'예약 1회'};
const jobStates={handed:['넘겨짐','neutral'],running:['실행 중','info'],exited:['끝남','success'],failed:['시작 실패','danger'],stopped:['중지됨','neutral'],expired:['만료','warning'],lost:['잃음','danger']};
const jobLive=s=>s==='handed'||s==='running'||s==='active'||s==='paused';
const schedStates={active:['예약됨','info'],paused:['멈춤','warning'],ended:['끝남','success'],expired:['만료','warning']};
// The job state filter as a schedule state; undefined: no schedule has it.
const schedFilter={live:'live','':'',exited:'ended',expired:'expired'};
let jobsSeq=0,jobRolesFilled=false;
async function loadJobs(){
  const seq=++jobsSeq;
  $('jobsNote').textContent='불러오는 중…';
  try{
    if(!jobRolesFilled){for(const v of await api('roles'))$('jobRole').append(new Option(v.role,v.role));jobRolesFilled=true}
    const q=new URLSearchParams();
    if($('jobRole').value)q.set('role',$('jobRole').value);
    if($('jobState').value)q.set('state',$('jobState').value);
    const sq=new URLSearchParams(q),sf=schedFilter[$('jobState').value];
    if(sf)sq.set('state',sf);else sq.delete('state');
    const [jobs,scheds]=await Promise.all([api('jobs'+(q.toString()?'?'+q:'')),sf===undefined?[]:api('schedules'+(sq.toString()?'?'+sq:''))]);
    if(seq!==jobsSeq)return;
    for(const sc of scheds)sc.schedule=true;
    drawJobs(jobs.concat(scheds).sort((a,b)=>String(b.created_at).localeCompare(String(a.created_at))));
  }catch(e){if(seq===jobsSeq)$('jobsNote').textContent=e.message}
}
function drawJobs(list){
  const body=$('jobs');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const j of list)body.append(jobRow(j));
    if(!list.length)emptyRow(body,jobCols.length,'작업 없음');
  });
  if(['live',''].includes($('jobState').value))setTabCount('jobs',list.filter(j=>jobLive(j.state)).length||null);
  $('jobsNote').textContent=list.length+'개';
}
function jobRow(j){
  if(j.schedule)return schedRow(j);
  const tr=node('tr','',jobLive(j.state)?'':'done');
  let [label,tone]=jobStates[j.state]||[j.state,'neutral'];
  if(j.state==='exited'&&j.exit_code!==undefined){label+=' · exit '+j.exit_code;if(j.exit_code)tone='danger'}
  if(j.stop_requested&&jobLive(j.state))label+=' · 중지 요청';
  const st=cell('',jobCols[4]);st.append(badge(label,tone));
  const name=cell('',jobCols[1],'wrap');name.append(node('strong',j.label));
  const started=cell(shortWhen(j.started_at||j.created_at),jobCols[5],'nowrap');started.title=when(j.started_at||j.created_at);
  const expires=cell(jobLive(j.state)?shortWhen(j.expires_at):'-',jobCols[6],'nowrap');expires.title=when(j.expires_at);
  const lines=(j.tail||'').split('\n'),last=lines[lines.length-1]||'-';
  const tail=cell(last,jobCols[7],'wrap mono');
  const act=cell('','','nowrap'),msg=node('span','','msg');msg.setAttribute('role','status');
  if(jobLive(j.state)){
    const b=setButton('중지','',async()=>{
      if(!confirm('#'+j.id+' '+j.label+' 작업을 멈출까요?'))return;
      b.disabled=true;setMsg(msg,'멈추는 중…');
      try{await apiSend('POST','jobs/'+encodeURIComponent(j.id)+'/stop');loadJobs()}
      catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
    });
    b.disabled=!!j.stop_requested;act.append(b);
  }else{
    const b=setButton('지우기','',async()=>{
      if(!confirm('#'+j.id+' '+j.label+' 기록을 지울까요? 되돌릴 수 없습니다.'))return;
      b.disabled=true;setMsg(msg,'지우는 중…');
      try{await apiSend('DELETE','jobs/'+encodeURIComponent(j.id));loadJobs()}
      catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
    });
    act.append(b);
  }
  act.append(msg);
  tr.append(cell(jobKinds[j.kind]||j.kind,jobCols[0],'nowrap'),name,cell(j.role,jobCols[2],'nowrap'),cell(j.machine,jobCols[3],'nowrap'),st,started,expires,tail,act);
  detailRow(tr,'jobs','job:'+j.id);
  return tr;
}
function schedRow(j){
  const tr=node('tr','',jobLive(j.state)?'':'done');
  const [label,tone]=jobBadge(j);
  const st=cell('',jobCols[4]);st.append(badge(label,tone));
  if(j.paused_by)st.append(node('div',pausedText(j),'muted'));
  const name=cell('',jobCols[1],'wrap');name.append(node('strong',j.label));
  if(j.cron){const c=node('div',j.cron,'muted mono');c.title=j.tz;name.append(c)}
  const created=cell(shortWhen(j.created_at),jobCols[5],'nowrap');created.title=when(j.created_at);
  const expires=cell(j.state==='active'?shortWhen(j.expires_at):'-',jobCols[6],'nowrap');expires.title=when(j.expires_at);
  const parts=[];
  if(j.next_fire_at&&j.state!=='paused')parts.push('다음 '+shortWhen(j.next_fire_at));
  if(j.missed)parts.push('밀린 '+j.missed+'회');
  const next=cell(parts.join(' · ')||'-',jobCols[7],'wrap');
  if(j.next_fire_at)next.title=when(j.next_fire_at);
  const act=cell('','','nowrap'),msg=node('span','','msg');msg.setAttribute('role','status');
  const b=setButton('지우기','',async()=>{
    if(!confirm('#'+j.id+' '+j.label+' 예약을 지울까요? 되돌릴 수 없습니다.'))return;
    b.disabled=true;setMsg(msg,'지우는 중…');
    try{await apiSend('DELETE','schedules/'+encodeURIComponent(j.id));loadJobs()}
    catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
  });
  act.append(...pauseButtons(j,msg),b,msg);
  tr.append(cell(jobKinds[j.kind]||j.kind,jobCols[0],'nowrap'),name,cell(j.role,jobCols[2],'nowrap'),cell(j.machine,jobCols[3],'nowrap'),st,created,expires,next,act);
  detailRow(tr,'jobs','sched:'+j.id);
  return tr;
}
// A paused schedule (#404) names who paused it, when and why; 멈춤 asks for
// an optional reason, 다시 resumes. A once schedule whose time passed while
// paused (passed) resumes only by sending that prompt now or dropping it.
function pausedText(j){
  const by=j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name;
  return by+' · '+shortWhen(j.paused_at)+(j.pause_reason?' · '+j.pause_reason:'');
}
function pauseButtons(j,msg){
  const id=encodeURIComponent(j.id),out=[];
  const act=(label,path,body,doing)=>{
    const b=setButton(label,'',async()=>{
      let send=body;
      if(path==='pause'){const r=prompt('#'+j.id+' '+j.label+' 예약을 멈춥니다. 까닭 (선택)','');if(r===null)return;send={reason:r}}
      else if(body&&body.passed==='drop'&&!confirm('#'+j.id+' '+j.label+' 지난 시각을 버리고 예약을 끝낼까요?'))return;
      b.disabled=true;setMsg(msg,doing);
      try{await(path==='pause'?apiSend('POST','schedules/'+id+'/pause',send):apiSend('POST','schedules/'+id+'/resume',send));loadJobs();drawDetail()}
      catch(e){b.disabled=false;setMsg(msg,e.message,'err')}
    });
    out.push(b);
  };
  if(j.state==='active')act('멈춤','pause',null,'멈추는 중…');
  else if(j.state==='paused'&&j.passed){act('지금 보내고 켜기','resume',{passed:'fire'},'켜는 중…');act('버리고 끝내기','resume',{passed:'drop'},'끝내는 중…')}
  else if(j.state==='paused')act('다시','resume',{},'켜는 중…');
  return out;
}
function jobBadge(j){
  if(j.schedule)return j.state==='paused'&&j.passed?['멈춤 · 지나감','warning']:schedStates[j.state]||[j.state,'neutral'];
  const [t,c]=jobStates[j.state]||[j.state,'neutral'];
  return j.state==='exited'&&j.exit_code?[t+' · exit '+j.exit_code,'danger']:[t,c];
}
registerDetail('jobs',{
  list:'작업',kinds:['jobs'],
  load:async id=>{
    const i=id.indexOf(':'),kind=id.slice(0,i),n=encodeURIComponent(id.slice(i+1));
    if(kind!=='job'&&kind!=='sched')return null;
    try{
      if(kind==='job')return await api('jobs/'+n);
      const sc=await api('schedules/'+n);sc.schedule=true;return sc;
    }catch(e){if(e.status===404)return null;throw e}
  },
  head:j=>({title:'#'+j.id+' '+j.label,badge:jobBadge(j)}),
  subs:j=>[['body',j.schedule?'예약':'명령']],
  draw:async(j,sub,box)=>{
    if(j.schedule){
      const cond=j.kind==='cron'?node('code',j.cron+' ('+j.tz+')'):when(j.next_fire_at||j.last_fire_at);
      const pairs=[['조건',cond],['다음',j.state==='active'?when(j.next_fire_at):'-'],['만료',when(j.expires_at)+(j.state==='paused'?' · 멈춘 동안 시계 멈춤':'')],['밀린',j.missed?j.missed+'회':'-'],['역할',j.role],['기계',j.machine],['ID',String(j.id)]];
      if(j.paused_by)pairs.push(['멈춘 쪽',j.paused_by.admin?'관리자 '+j.paused_by.name:'역할 '+j.paused_by.name],['멈춘 시각',when(j.paused_at)],['까닭',j.pause_reason||'-']);
      pairs.push(['프롬프트',node('pre',j.prompt)]);
      box.append(dl(pairs));
      const msg=node('span','','msg');msg.setAttribute('role','status');
      const acts=node('div','','detail-acts');acts.append(...pauseButtons(j,msg),msg);
      if(acts.childElementCount>1)box.append(acts);
      return;
    }
    box.append(dl([['종류',jobKinds[j.kind]||j.kind],['역할',j.role],['기계',j.machine],['작업 폴더',node('code',j.cwd)],['시작',when(j.started_at||j.created_at)],['만료',when(j.expires_at)],['ID',String(j.id)],['명령',node('pre',j.command)]]));
    if(j.error)box.append(node('p',j.error,'err'));
    if(j.tail)box.append(node('h3','마지막 출력','sechead'),node('pre',j.tail));
  }
});
// A linked thread: the directive with its state (열림 / 끝남, the answered
// rule's Open), then the replies in order (답 marks one the answered rule
// counts). An open directive names its sender, who closes it. Each opens the
// mail detail.
function reportThread(t){
  const wrap=node('div','','rthread');
  const b=t.open?badge('열림','info'):badge('끝남','success');
  if(!t.open)b.title=t.completed?'받은 쪽이 끝냄':'보낸 쪽이 닫음';
  const extra=t.open?[b,' ',node('span','닫을 쪽 '+t.directive.sender,'muted')]:[b];
  wrap.append(reportMail(t.directive,extra,'div'));
  if(t.replies.length){
    const ol=node('ol','');
    for(const m of t.replies)ol.append(reportMail(m,m.answers?badge('답','success'):null,'li'));
    wrap.append(ol);
  }
  return wrap;
}
function reportMail(m,b,tag){
  const box=node(tag,''),line=node('div','','rline');
  const a=node('a','#'+m.id+' '+(m.subject||'(제목 없음)'));a.href=detailHash('mail',String(m.id),'body');
  a.onclick=e=>{e.preventDefault();openMessage(m.id)};
  line.append(a);if(b)line.append(' ',...[].concat(b));
  box.append(line,node('div',m.sender+' → '+m.recipient+' · '+shortWhen(m.created_at),'muted'));
  if(m.body)box.append(node('p',m.body.length>300?m.body.slice(0,300)+'…':m.body,'rbody'));
  return box;
}
registerDetail('reports',{
  list:'보고',kinds:['reports'],
  load:async id=>{
    try{return await api('reports/'+encodeURIComponent(id))}
    catch(e){if(e.status===404)return null;throw e}
  },
  head:r=>({title:r.title,badge:reportBadge(r)}),
  subs:()=>[['body','보고']],
  draw:async(r,sub,box)=>{
    if(r.request){const q=node('p','','reqline');q.append(node('span','요청 ','muted'),document.createTextNode(r.request));box.append(q)}
    box.append(dl([['보낸 쪽',r.author],['시각',when(r.created_at)],['ID',String(r.id)]]));
    const md=node('div','','md');md.append(mdRender(r.body));box.append(md);
    box.append(node('h3','연결된 지시 '+r.threads.length,'sechead'));
    if(!r.threads.length)box.append(node('p','연결된 지시 없음','empty'));
    for(const t of r.threads)box.append(reportThread(t));
  }
});
// ---- 규칙 (#406 ①) ----
// The rules ledger: rows in force by service and topic, 결정 first. A row's
// detail has its history and the answered questions it came from; an admin
// fixes a row (a new row replacing it) or voids it. A 409 lists the rows in
// force on the same scope and topic: replace them or stand alongside.
const ruleCols=['#','종류','주제','범위','글','걸림'];
const ruleKinds={decision:'결정',tip:'요령'};
const ruleStates={active:['유효','success'],replaced:['대체됨','neutral'],void:['무효','danger'],proposed:['제안','warning'],rejected:['거절됨','danger']};
const ruleChips=[['decision','결정'],['tip','요령'],['','전체']];
let rulesData=null,rulesKind='decision',rulesSeq=0,rulesProposed=false;
function ruleScopeText(s){return !s?'':s.kind==='all'||!s.values||!s.values.length?s.kind:s.kind+':'+s.values.join(',')}
function ruleFirstLine(t){return String(t||'').split('\n')[0]}
function ruleMatchText(m){
  if(!m)return '';
  const p=[];
  if(m.tool)p.push('도구 '+m.tool);
  if(m.command)p.push('명령 /'+m.command+'/');
  if(m.path)p.push('경로 '+m.path);
  return p.join(' · ');
}
// ruleMatchBody reads the three condition fields; empty ones are left out.
function ruleMatchBody(tool,command,path){
  const m={};
  if(tool.trim())m.tool=tool.trim();
  if(command)m.command=command;
  if(path.trim())m.path=path.trim();
  return Object.keys(m).length?m:null;
}
function ruleMatchInputs(m){
  const mk=(label,ph,v)=>{const i=node('input','');i.placeholder=ph;i.value=v||'';i.setAttribute('aria-label',label);i.autocomplete='off';i.spellcheck=false;return i};
  m=m||{};
  return [mk('걸림: 도구','도구 (Bash, mcp__*)',m.tool),mk('걸림: 명령 정규식','명령 정규식',m.command),mk('걸림: 경로 glob','경로 glob (**/x/*.sql)',m.path)];
}
function parseRuleScope(v){
  const i=v.indexOf(':'),kind=(i<0?v:v.slice(0,i)).trim();
  return {kind:kind,values:i<0?[]:v.slice(i+1).split(',').map(x=>x.trim()).filter(Boolean)};
}
// ruleSourceBody reads a source field: answered question IDs (q_…, comma
// separated) or a text.
function ruleSourceBody(v){
  const ids=v.split(',').map(x=>x.trim()).filter(Boolean);
  return ids.length&&ids.every(x=>/^q_\S+$/.test(x))?{sources:ids}:{source_text:v.trim()};
}
async function loadRules(){
  const seq=++rulesSeq,q=new URLSearchParams(),svc=$('ruleService').value.trim(),topic=$('ruleTopic').value;
  if(svc)q.set('service',svc);
  if(topic)q.set('topic',topic);
  if($('ruleAll').checked)q.set('all','1');
  if(rulesProposed)q.set('proposed','1');
  $('rulesNote').textContent='불러오는 중…';
  try{
    const [list,topics,uncl]=await Promise.all([api('rules?'+q.toString()),api('rules/topics'),api('rules/unclassified')]);
    if(seq!==rulesSeq)return;
    rulesData=list.rules;
    fillRuleTopics(topics.topics);
    drawRuleCounts(list.proposed||0,uncl);
    drawRules();
  }catch(e){if(seq===rulesSeq)$('rulesNote').textContent=e.message}
}
// drawRuleCounts shows the proposals waiting for approval (a toggle that
// lists them) and the answers to panel questions still unclassified, per
// role in the title.
function drawRuleCounts(proposed,uncl){
  $('ruleProposedN').textContent=String(proposed);
  $('ruleProposed').setAttribute('aria-pressed',rulesProposed?'true':'false');
  const u=$('ruleUnclassified');
  u.textContent='미분류 '+(uncl.count||0);
  u.title=(uncl.roles||[]).map(r=>r.role+' '+r.count).join('\n');
}
function fillRuleTopics(topics){
  const sel=$('ruleTopic'),cur=sel.value,dl=$('ruleTopics');
  sel.replaceChildren(new Option('모든 주제',''));dl.replaceChildren();
  for(const t of topics){sel.append(new Option(t.name+' · '+t.active,t.name));dl.append(new Option(t.name,t.name))}
  if(cur&&!topics.some(t=>t.name===cur))sel.append(new Option(cur,cur));
  sel.value=cur;
}
function drawRules(){
  const list=rulesData;if(!list)return;
  const chips=$('ruleChips');chips.replaceChildren();
  for(const [k,label] of ruleChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===rulesKind?'true':'false');
    b.append(node('span',String(list.filter(r=>!k||r.kind===k).length),'sc-chip-count'));
    b.onclick=()=>{rulesKind=k;drawRules()};
    chips.append(b);
  }
  const shown=list.filter(r=>!rulesKind||r.kind===rulesKind),body=$('rules');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const r of shown)body.append(ruleRow(r));
    if(!shown.length)emptyRow(body,ruleCols.length,'규칙 없음');
  });
  $('rulesNote').textContent=(shown.length===list.length?'':shown.length+' / ')+list.length+'줄';
}
function ruleBadge(st){return ruleStates[st]||[st,'neutral']}
function ruleRow(r){
  const off=r.status&&r.status!=='active',tr=node('tr','',off?'done':'');
  const t=cell('',ruleCols[4],'wrap');t.append(node('span',ruleFirstLine(r.text)));
  if(off){const [l,tone]=ruleBadge(r.status);t.append(' ',badge(l,tone))}
  const mt=ruleMatchText(r.match);if(mt)t.append(' ',node('span',mt,'muted'));
  const h=cell(String(r.hits||0),ruleCols[5],'num');if(r.last_hit_at)h.title='마지막 '+when(r.last_hit_at);
  tr.append(cell('#'+r.id,ruleCols[0],'num'),cell(ruleKinds[r.kind]||r.kind,ruleCols[1],'nowrap'),cell(r.topic,ruleCols[2]),cell(ruleScopeText(r.scope)+(r.group?' @'+r.group:''),ruleCols[3]),t,h);
  detailRow(tr,'rules',r.id);
  return tr;
}
// postRule adds a row; on a 409 it shows the rows in force and offers to
// replace them or (for a new row) to stand alongside them.
async function postRule(body,msg,done){
  msg.className='msg';msg.replaceChildren();
  try{done(await apiSend('POST','rules',body));return}
  catch(e){
    let c=null;
    if(e.status===409){try{c=JSON.parse(e.body)}catch(x){}}
    if(!c||!c.rules){msg.className='msg err';msg.textContent=e.message;return}
    msg.append(node('p','같은 범위·주제에 유효한 줄이 있다'));
    const ul=node('ul','','plain');
    for(const r of c.rules)ul.append(node('li','#'+r.id+' '+(ruleKinds[r.kind]||r.kind)+' '+ruleFirstLine(r.text)));
    const row=node('div','','row'),rep=node('button','대체');rep.type='button';
    rep.onclick=()=>postRule(Object.assign({},body,{replace:[...(body.replace||[]),...c.rules.map(r=>r.id)]}),msg,done);
    row.append(rep);
    if(!(body.replace&&body.replace.length)){
      const side=node('button','나란히');side.type='button';
      side.onclick=()=>postRule(Object.assign({},body,{alongside:true}),msg,done);
      row.append(side);
    }
    msg.append(ul,row);
  }
}
function ruleEditForm(r){
  const f=node('form','','ruleform');
  const text=node('textarea','');text.value=r.text;text.rows=4;text.required=true;text.setAttribute('aria-label','새 글');
  const src=node('input','');src.placeholder='출처: 질문 ID(q_…) 또는 글';src.required=true;src.setAttribute('aria-label','출처');
  const [wt,wc,wp]=ruleMatchInputs(r.match),when3=node('div','','row');when3.append(wt,wc,wp);
  const save=node('button','고쳐 올리기');save.type='submit';
  const off=node('button','무효로');off.type='button';
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const row=node('div','','row');row.append(save,off);
  f.append(node('h3','고치기','sechead'),text,when3,src,row,msg);
  f.onsubmit=e=>{
    e.preventDefault();
    postRule(Object.assign(ruleSourceBody(src.value),{kind:r.kind,text:text.value,topic:r.topic,scope:r.scope,group:r.group||'',device:r.device||'',match:ruleMatchBody(wt.value,wc.value,wp.value),replace:[r.id],alongside:true}),msg,n=>{loadRules();openDetail('rules',String(n.id))});
  };
  off.onclick=async()=>{
    if(!confirm('#'+r.id+' 줄을 무효로 돌린다. 되돌릴 수 없다.'))return;
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/void');loadRules();drawDetail()}
    catch(e){msg.className='msg err';msg.textContent=e.message}
  };
  return f;
}
registerDetail('rules',{
  list:'규칙',kinds:['rules'],
  load:async id=>{
    try{return await api('rules/'+encodeURIComponent(id))}
    catch(e){if(e.status===404)return null;throw e}
  },
  head:r=>({title:'#'+r.id+' '+(ruleKinds[r.kind]||r.kind)+' · '+r.topic,badge:ruleBadge(r.status)}),
  subs:()=>[['body','규칙']],
  draw:async(r,sub,box)=>{
    box.append(node('p',r.text,'rbody'));
    const pairs=[['범위',ruleScopeText(r.scope)],['그룹',r.group||'(그룹 없음)']];
    if(r.sources&&r.sources.length)pairs.push(['출처',r.sources.join(', ')]);
    if(r.source_text)pairs.push(['출처 글',r.source_text]);
    if(r.device)pairs.push(['장치',r.device]);
    if(r.match)pairs.push(['걸림 조건',ruleMatchText(r.match)]);
    if(r.hits)pairs.push(['걸린 수',r.hits+(r.last_hit_at?' · 마지막 '+when(r.last_hit_at):'')]);
    pairs.push(['적은 이',r.author||'-'],['시각',when(r.created_at)]);
    if(r.voided_by)pairs.push(['무효로 돌린 이',r.voided_by+' · '+when(r.voided_at)]);
    if(r.reviewed_by)pairs.push([r.status==='rejected'?'거절한 이':'승인한 이',r.reviewed_by+' · '+when(r.reviewed_at)]);
    if(r.review_note)pairs.push(['거절 까닭',r.review_note]);
    box.append(dl(pairs));
    if(r.history&&r.history.length){
      box.append(node('h3','이력','sechead'));
      const ul=node('ul','','plain');
      for(const h of r.history){
        const li=node('li',''),a=node('a','#'+h.id+' '+ruleFirstLine(h.text));a.href=detailHash('rules',String(h.id));
        const [l,tone]=ruleBadge(h.status);li.append(a,' ',badge(l,tone));ul.append(li);
      }
      box.append(ul);
    }
    if(r.answers&&r.answers.length){
      box.append(node('h3','원문','sechead'));
      for(const a of r.answers){
        box.append(node('div',a.id+' · '+a.asker+' → '+a.answered_by+' · '+when(a.answered_at),'rline'));
        for(const it of a.items||[])box.append(node('p','['+it.header+'] '+it.question+' → '+[...(it.chosen||[]),...(it.other?[it.other]:[])].join(', '),'muted'));
      }
    }
    if(r.status==='active')box.append(ruleEditForm(r));
    if(r.status==='proposed')box.append(ruleReviewForm(r));
  }
});
// ruleReviewForm approves a proposed row (a 409 offers to replace the rows
// in force on its scope and topic or to stand alongside them) or rejects it
// with a reason.
function ruleReviewForm(r){
  const f=node('form','','ruleform');
  const reason=node('input','');reason.placeholder='거절 까닭';reason.setAttribute('aria-label','거절 까닭');reason.autocomplete='off';
  const ok=node('button','승인');ok.type='button';
  const no=node('button','거절');no.type='button';
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const row=node('div','','row');row.append(ok,reason,no);
  f.append(node('h3','제안','sechead'),row,msg);
  const done=()=>{loadRules();drawDetail()};
  const approve=async body=>{
    msg.className='msg';msg.replaceChildren();
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/approve',body);done();return}
    catch(e){
      let c=null;
      if(e.status===409){try{c=JSON.parse(e.body)}catch(x){}}
      if(!c||!c.rules){msg.className='msg err';msg.textContent=e.message;return}
      msg.append(node('p','같은 범위·주제에 유효한 줄이 있다'));
      const ul=node('ul','','plain');
      for(const o of c.rules)ul.append(node('li','#'+o.id+' '+(ruleKinds[o.kind]||o.kind)+' '+ruleFirstLine(o.text)));
      const rep=node('button','대체');rep.type='button';rep.onclick=()=>approve({replace:c.rules.map(o=>o.id)});
      const side=node('button','나란히');side.type='button';side.onclick=()=>approve({alongside:true});
      const btns=node('div','','row');btns.append(rep,side);
      msg.append(ul,btns);
    }
  };
  ok.onclick=()=>approve({});
  no.onclick=async()=>{
    msg.className='msg';msg.replaceChildren();
    try{await apiSend('POST','rules/'+encodeURIComponent(r.id)+'/reject',{reason:reason.value});done()}
    catch(e){msg.className='msg err';msg.textContent=e.message}
  };
  return f;
}
// ---- 결정 (#50) ----
// The 결정 view of the 질문 tab lists answered questions as decisions. Its
// filters build one query used both for the list and for the 「JSON 내보내기」
// link (format=ndjson, a same-origin GET the browser downloads).
let decisionsLoaded=false;
function showQuestionView(name){
  const dec=name==='decisions';
  $('viewQuestions').setAttribute('aria-pressed',dec?'false':'true');
  $('viewDecisions').setAttribute('aria-pressed',dec?'true':'false');
  $('questionsView').hidden=dec;$('decisionsView').hidden=!dec;
  if(dec&&!decisionsLoaded){decisionsLoaded=true;loadDecisionRoles();loadDecisions()}
}
function decisionQuery(){
  const p=new URLSearchParams(),period=$('decPeriod').value;
  if(period==='custom'){
    if($('decFrom').value)p.set('from',$('decFrom').value);
    if($('decTo').value)p.set('to',$('decTo').value);
  }else if(period!=='all'){
    p.set('from',new Date(Date.now()-Number(period)*864e5).toISOString().replace(/\.\d+Z$/,'Z'));
  }
  const role=$('decRole').value.trim(),q=$('decSearch').value.trim();
  if(role)p.set('role',role);
  if(q)p.set('q',q);
  return p;
}
function updateDecisionExport(){
  const p=decisionQuery();p.set('format','ndjson');
  $('decExport').href='/api/admin/decisions?'+p.toString();
}
async function loadDecisionRoles(){
  try{
    const roles=(await api('roles')).map(v=>v.role),seen=new Set(),list=$('decRoles');
    const add=v=>{if(!seen.has(v)){seen.add(v);const o=document.createElement('option');o.value=v;list.append(o)}};
    for(const r of roles){const i=r.lastIndexOf('/');if(i>0)add(r.slice(0,i)+'/*')}
    for(const r of roles)add(r);
  }catch(e){}
}
async function loadDecisions(){
  updateDecisionExport();
  $('decisionsNote').textContent='불러오는 중…';
  try{
    const list=(await api('decisions?'+decisionQuery().toString())).decisions;
    const box=$('decisions');box.replaceChildren();
    for(const d of list)box.append(decisionRow(d));
    $('decisionsNote').textContent='결정 '+list.length+'개'+(list.length>=1000?'(최대 1000개)':'');
    if(!list.length)box.append(node('p','조건에 맞는 결정 없음','empty'));
  }catch(e){$('decisionsNote').textContent=e.message}
}
function decisionRow(d){
  const row=node('div','','dec');
  row.append(node('p',d.asker+' · '+when(d.answered_at)+' · '+d.id+' · 답한 쪽 '+byLabel(d.answered_by),'muted'));
  for(const it of d.items){
    const p=node('p','');
    let chosen=(it.chosen||[]).join(', ');
    if(it.other)chosen+=(chosen?' / ':'')+'기타: '+it.other;
    p.title=it.question;
    p.append(node('span',it.header,'pill'),document.createTextNode('→ '+(chosen||'(선택 없음)')));
    row.append(p);
  }
  return row;
}
$('decPeriod').onchange=()=>{const c=$('decPeriod').value==='custom';$('decFrom').hidden=!c;$('decTo').hidden=!c;loadDecisions()};
$('decForm').onsubmit=e=>{e.preventDefault();loadDecisions()};
for(const id of ['decFrom','decTo','decRole','decSearch'])$(id).addEventListener('input',updateDecisionExport);
$('viewQuestions').onclick=()=>selectTab('questions');
$('viewDecisions').onclick=()=>selectTab('decisions');
// ---- 터미널 (hosting) ----
// The list opens a record in the detail panel (#terminals/<id>/<sub>): 화면
// (the live terminal), 기록 (runtime and the access log) and 설정. The add
// form sits in a drawer.
let termLimited=false;
async function loadTermLimit(){
  try{
    const l=await api('terminals/limit');
    termLimited=!!l.limited;
    let t=termLimited?'걸림 · '+(l.source==='operator'?'수동':'화면')+' · '+when(l.since):'없음';
    if(termLimited&&l.reset_at)t+=' · 초기화 '+when(l.reset_at);
    $('termLimitText').textContent=t;
    $('termLimitBtn').textContent=termLimited?'한도 풀기':'한도 걸기';
    $('termLimitBtn').setAttribute('aria-pressed',String(termLimited));
  }catch(e){$('termLimitText').textContent=e.message}
}
$('termLimitBtn').onclick=async()=>{
  try{await apiSend('PUT','terminals/limit',{limited:!termLimited});setMsg($('terminalsMsg'),termLimited?'구독 한도 풀림':'구독 한도 걸림','ok');loadTerminals()}
  catch(e){setMsg($('terminalsMsg'),e.message,'err')}
};
const termPresence={online:'온라인',offline:'오프라인','offline (process alive)':'오프라인 (프로세스 살아 있음)'};
// hostedBySlot maps a slot role (team/worker-1) to the hosted session holding it.
async function hostedBySlot(){
  const m=new Map();
  try{for(const v of await api('terminals'))if(v.slot&&v.slot.role)m.set(v.slot.role,v)}catch(e){}
  return m;
}
const termState={running:'실행 중',starting:'시작 중',stopped:'멈춤',waiting:'대기',crashloop:'반복 실패','backing off':'재시도 대기','resume failed':'재개 실패',relaunching:'다시 띄우는 중','backend down':'터미널 끊김',unknown:'모름','node offline':'노드 오프라인',gone:'삭제됨'};
function termStateText(st,rt){
  let t=termState[st]||st;
  if(st==='backing off'&&rt.next_attempt_at){
    const s=Math.max(0,Math.ceil((rt.next_attempt_at-Date.now())/1000));
    t+=' · '+(s<60?s+'초':Math.floor(s/60)+'분 '+(s%60)+'초')+' 뒤';
  }
  if(rt.resume_failed)t+=' · 새 대화(재개 실패)';
  return t;
}
function termSt(v){return (v.runtime||{}).state||'unknown'}
function termStClass(st){return st==='running'?'st-idle':st==='node offline'||st==='unknown'?'st-offline':st==='crashloop'||st==='resume failed'||st==='backend down'?'st-error':''}
const termTone={'st-idle':'success','st-error':'danger'};
function termRole(v){return v.slot&&v.slot.role?v.slot.role:v.base}
function termDesire(v){const st=termSt(v);return v.desired==='stopped'&&st!=='stopped'&&st!=='node offline'?' → 멈춤':''}
const termCols=['이름','프로필','노드','상태','권한','재시작','마지막 시작'];
const termChips=[['all','전체',()=>true],['running','실행 중',v=>termSt(v)==='running'],['stopped','멈춤',v=>v.desired==='stopped'||termSt(v)==='stopped'],['problem','문제',v=>/^(crashloop|resume failed|backing off|backend down)$/.test(termSt(v))],['offline','오프라인',v=>/^(node offline|unknown)$/.test(termSt(v))]];
// termSeq drops an answer a later load has overtaken.
let termData=null,termFilter='all',termSeq=0;
async function loadTerminals(){
  const seq=++termSeq;
  $('terminalsNote').textContent='불러오는 중…';
  loadTermLimit();
  try{
    const rows=await api('terminals');
    if(seq!==termSeq)return;
    termData=rows;drawTerminals();
  }catch(e){if(seq===termSeq)$('terminalsNote').textContent=e.message}
}
function drawTerminals(){
  if(!termData)return;
  const rows=termData,chips=$('termChips');chips.replaceChildren();
  for(const [k,label,f] of termChips){
    const b=node('button',label+' ','sc-chip');b.type='button';b.setAttribute('aria-pressed',k===termFilter?'true':'false');
    b.append(node('span',String(rows.filter(f).length),'sc-chip-count'));
    b.onclick=()=>{termFilter=k;drawTerminals()};
    chips.append(b);
  }
  const shown=rows.filter(termChips.find(c=>c[0]===termFilter)[2]),body=$('terminals');
  keepRowFocus(body,()=>{
    body.replaceChildren();
    for(const v of shown){
      const rt=v.runtime||{},st=termSt(v),stc=termStClass(st);
      const tr=node('tr','',st==='node offline'?'offline':stc==='st-error'?'rowerror':'');
      const name=node('td',termRole(v));if(v.slot&&v.slot.role)name.title=v.base;
      if(v.label)name.append(node('small',v.label));
      const state=node('td',termStateText(st,rt)+termDesire(v),stc);
      const pr=v.presence||'',sub=[termPresence[pr]||pr,v.accepting_work===false?'일감 받지 않음':''].filter(Boolean).join(' · ');
      if(sub)state.append(node('small',sub));
      const last=node('td',rt.started_at?ago(Date.now(),rt.started_at):'-','nowrap');last.title=when(rt.started_at);
      const cells=[name,node('td',v.profile||'-'),node('td',v.node),state,node('td',v.skip_permissions?'건너뜀':'물음','nowrap'),node('td',String(rt.restarts||0),'num'),last];
      cells.forEach((td,i)=>{if(i>0)td.dataset.label=termCols[i];tr.append(td)});
      detailRow(tr,'terminals',v.id);
      body.append(tr);
    }
    if(!shown.length)emptyRow(body,termCols.length,'터미널 없음');
  });
  $('terminalsNote').textContent=(shown.length===rows.length?'':shown.length+' / ')+rows.length+'개';
}
tabLoaders.terminals=loadTerminals;
// termNote is the last action's result, shown above the open record's sub-tabs.
let termNote=null;
async function termAct(v,method,path,body,done){
  try{
    await apiSend(method,'terminals/'+v.id+path,body);
    termNote={id:v.id,text:done,cls:'ok'};setMsg($('terminalsMsg'),done,'ok');loadTerminals();
    if(method==='DELETE'&&detailCur&&detailCur.id===v.id){closeDetail();return}
  }catch(e){termNote={id:v.id,text:e.message,cls:'err'};setMsg($('terminalsMsg'),e.message,'err')}
  drawDetail();
}
// termActs puts the record's actions (멈춤 or 시작 · 재시작 · 새 대화 · 지우기) in box.
function termActs(v,box){
  const acts=node('div','','detail-acts');
  const add=(label,fn)=>{const b=node('button',label);b.type='button';b.onclick=fn;acts.append(b)};
  if(v.desired==='stopped')add('시작',()=>termAct(v,'PUT','',{desired:'running'},v.base+' 시작'));
  else{
    add('멈춤',()=>termAct(v,'PUT','',{desired:'stopped'},v.base+' 멈춤'));
    add('재시작',()=>termAct(v,'POST','/restart',undefined,v.base+' 재시작'));
  }
  add('새 대화',()=>{if(confirm(v.node+' '+v.base+' '+v.directory+' 세션을 새 대화로 시작할까요? 지금 대화로는 돌아가지 않습니다.'))termAct(v,'POST','/new-conversation',undefined,v.base+' 새 대화')});
  add('지우기',()=>{if(confirm(v.node+' '+v.base+' '+v.directory+' 세션을 지울까요?'))termAct(v,'DELETE','',undefined,v.base+' 삭제')});
  box.append(acts);
  if(termNote&&termNote.id===v.id)box.append(node('p',termNote.text,'msg '+termNote.cls));
}
function termDir(d){const s=node('span','‎'+d+'‎','dir');s.title=d;return s}
const termLogKind={attach:'연결',detach:'끊음',input:'입력'};
const termLogMode={view:'보기',input:'입력'};
const termEventKind={start:'시작',restart:'재시작',stop:'정지',crash:'비정상 종료',exit:'종료',adopt:'이어받음','plugin-failed':'플러그인 설치 실패',add:'추가',remove:'제거'};
const termEventActor={admin:'관리자',policy:'재시작 규칙',node:'노드'};
// termTable is a stack table with head labels and one row per item (rowOf: values and classes).
function termTable(id,labels,items,rowOf){
  const tbl=node('table','','stack list'),head=node('tr','');
  for(const h of labels)head.append(node('th',h));
  const thead=document.createElement('thead');thead.append(head);
  const body=document.createElement('tbody');body.id=id;
  for(const it of items){const tr=node('tr','');const [vals,cls]=rowOf(it);cells(tr,labels,vals,cls);body.append(tr)}
  if(!items.length)emptyRow(body,labels.length,'기록 없음');
  tbl.append(thead,body);return tbl;
}
// termHistory draws 기록: the runtime now, the session's starts, restarts, stops and exits (#252), then who attached and typed.
async function termHistory(v,box){
  const rt=v.runtime||{},st=termSt(v);
  const pairs=[['상태',termStateText(st,rt)+termDesire(v)],['이후',when(rt.since)],['마지막 시작',when(rt.started_at)],['재시작',String(rt.restarts||0)+'회'],['마지막 종료',rt.last_exit===null||rt.last_exit===undefined?'-':String(rt.last_exit)]];
  if(rt.next_attempt_at)pairs.push(['다음 시도',when(rt.next_attempt_at)]);
  if(rt.resume_failed)pairs.push(['재개','실패 · 새 대화']);
  const id=encodeURIComponent(v.id);
  // acts: the admin actions on this session (#446); a view that cannot read
  // them (a boss role's) leaves the section out.
  const [events,rows,acts]=await Promise.all([api('terminals/'+id+'/events'),api('terminals/'+id+'/log'),api('admin-actions?target='+id+'&limit=20').catch(()=>null)]);
  box.append(dl(pairs),node('h3','실행'));
  box.append(termTable('termEvents',['시각','종류','종료 코드','재시작','요청'],events,e=>[[when(e.at),(termEventKind[e.kind]||e.kind)+(e.reason?' · '+e.reason:''),e.exit_code===null||e.exit_code===undefined?'-':String(e.exit_code),String(e.restarts||0),termEventActor[e.actor]||e.actor||'-'],['','','num','num','']]));
  box.append(node('h3','접속'));
  box.append(termTable('termLog',['시각','관리자','종류','방식','주소','바이트'],rows,e=>[[when(e.at),e.admin_user,termLogKind[e.kind]||e.kind,termLogMode[e.mode]||e.mode||'-',e.ip||'-',e.kind==='input'?String(e.bytes):'-'],['','','','','mono','num']]));
  if(Array.isArray(acts)){
    box.append(node('h3','관리'));
    box.append(termTable('termActs',['시각','관리자','동작','방식','주소'],acts,e=>[[when(e.at),e.admin,(window.wsNodes?wsNodes.actionName(e.action):e.action)+(e.detail?' · '+e.detail:''),e.method||'-',e.addr||'-'],['','','','','mono']]));
  }
}
// termSettings draws 설정: what the record runs and the fields a PUT changes.
function termSettings(v,box){
  const pr=v.presence||'';
  box.append(dl([['노드',v.node],['역할',v.base],['슬롯',v.slot&&v.slot.role?v.slot.role:'-'],['접속',termPresence[pr]||pr||'-'],['프로필',v.profile||'-'],['인자',(v.args||[]).join(' ')||'-'],['폴더',termDir(v.directory)],['묶음',v.group||'없음'],['일감',v.accepting_work===false?'받지 않음':'받음']]));
  const perm=node('button',v.skip_permissions?'권한 건너뜀':'권한 물음');perm.type='button';perm.id='termPermToggle';
  perm.setAttribute('aria-pressed',String(!!v.skip_permissions));
  perm.onclick=()=>termAct(v,'PUT','',{skip_permissions:!v.skip_permissions},v.base+(v.skip_permissions?' 권한 물음':' 권한 건너뜀'));
  const f=node('form','','settings-form termform');f.id='termEdit';
  const field=(label,el)=>{const l=node('label',label+' ');l.append(el);f.append(l);return el};
  const input=(val)=>{const i=document.createElement('input');i.value=val||'';i.autocomplete='off';i.spellcheck=false;return i};
  const select=(val,opts)=>{const s=document.createElement('select');for(const [k,t] of opts){const o=node('option',t);o.value=k;s.append(o)}s.value=val;return s};
  const label=field('라벨',input(v.label)),parent=field('부모',input(v.parent));
  const grp=field('묶음',select(v.group||'',[['','없음'],...(v.group?[[v.group,v.group]]:[])]));groupOptions(grp,v.group||'');
  const adp=field('어댑터',select(v.adapter||'',[['','기본'],...(v.adapter?[[v.adapter,v.adapter]]:[])]));adapterOptions(adp,v.node,v.adapter||'');
  const restart=field('재시작',select(v.restart||'always',[['always','always'],['on-failure','on-failure'],['never','never']]));
  const args=field('인자',input((v.args||[]).join(' ')));
  const row=node('div','','row'),save=node('button','저장');save.type='submit';row.append(save,perm);f.append(row);
  f.onsubmit=e=>{
    e.preventDefault();
    const c={},a=args.value.split(/\s+/).filter(Boolean);
    if(label.value.trim()!==(v.label||''))c.label=label.value.trim();
    if(parent.value.trim()!==(v.parent||''))c.parent=parent.value.trim();
    if(grp.value!==(v.group||''))c.group=grp.value;
    if(adp.value!==(v.adapter||''))c.adapter=adp.value;
    if(restart.value!==(v.restart||'always'))c.restart=restart.value;
    if(a.join(' ')!==(v.args||[]).join(' '))c.args=a;
    if(!Object.keys(c).length)return;
    termAct(v,'PUT','',c,v.base+' 설정 저장');
  };
  box.append(f,node('p','바꾼 설정은 다음 시작부터 적용','muted'));
}
// ---- 예약 (#336): the record's scheduled runs and their recent runs ----
const autoResult={fired:'실행',skipped:'건너뜀',error:'오류',timeout:'시간 초과',overrun:'시간 넘김',busy:'겹침',waiting:'대기','node-offline':'노드 꺼짐'};
const autoTone={fired:'success',skipped:'neutral',error:'danger',timeout:'danger',overrun:'danger',busy:'warning',waiting:'warning','node-offline':'warning'};
function autoBadge(r){return badge(autoResult[r]||r,autoTone[r]||'neutral')}
// autoOpen holds the first run id of each skipped stretch the viewer opened.
const autoOpen=new Set();
// autoGroups folds runs of two or more skipped in a row into one group.
function autoGroups(runs){
  const out=[];
  for(const r of runs){
    const g=out[out.length-1];
    if(r.result==='skipped'&&g&&g.skipped)g.runs.push(r);
    else out.push({skipped:r.result==='skipped',runs:[r]});
  }
  return out;
}
function autoRunRow(body,labels,r){
  const tr=node('tr','');
  const res=node('td','','nowrap');res.append(autoBadge(r.result));
  if(r.exit_code!==null&&r.exit_code!==undefined)res.append(' '+r.exit_code);
  if(r.manual)res.append(' 수동');
  const det=node('td',r.detail||(r.output?'':'-'),'wrap');
  if(r.output){const d=document.createElement('details');d.append(node('summary','출력'),node('pre',r.output));det.append(d)}
  [node('td',when(r.at),'nowrap'),res,det].forEach((td,i)=>{if(i>0)td.dataset.label=labels[i];tr.append(td)});
  body.append(tr);
}
function autoRuns(runs){
  const labels=['시각','결과','상세'];
  const tbl=node('table','','stack list autoruns'),head=node('tr','');
  for(const h of labels)head.append(node('th',h));
  const thead=document.createElement('thead');thead.append(head);
  const body=document.createElement('tbody');
  for(const g of autoGroups(runs)){
    const first=g.runs[0];
    if(!g.skipped||g.runs.length<2||autoOpen.has(first.id)){for(const r of g.runs)autoRunRow(body,labels,r);continue}
    const tr=node('tr',''),t=node('td',when(g.runs[g.runs.length-1].at)+' ~ '+when(first.at),'wrap');
    const res=node('td','');res.dataset.label=labels[1];res.colSpan=2;
    const more=node('button','건너뜀 '+g.runs.length+'회');more.type='button';
    more.onclick=()=>{autoOpen.add(first.id);drawDetail()};
    res.append(more);
    tr.append(t,res);body.append(tr);
  }
  if(!runs.length)emptyRow(body,labels.length,'기록 없음');
  tbl.append(thead,body);return tbl;
}
async function autoAct(v,a,method,path,done){
  try{
    await apiSend(method,'terminals/'+encodeURIComponent(v.id)+'/autos/'+a.id+path);
    termNote={id:v.id,text:done,cls:'ok'};
  }catch(e){termNote={id:v.id,text:e.message,cls:'err'}}
  drawDetail();
}
function autoName(a){return a.label||'예약 실행 '+a.id}
async function termAutos(v,box){
  const id=encodeURIComponent(v.id);
  const list=await api('terminals/'+id+'/autos');
  if(!list.length){box.append(node('p','예약 실행 없음','muted'));return}
  const views=await Promise.all(list.map(a=>api('terminals/'+id+'/autos/'+a.id+'?runs=20')));
  for(const a of views){
    const sec=node('section','','auto');sec.id='auto-'+a.id;
    const h=node('h3',autoName(a)+' ');
    h.append(badge(a.state==='paused'?'멈춤':'활성',a.state==='paused'?'warning':'success'));
    if(a.alert){const b=autoBadge(a.alert);b.title='알림 중';h.append(' ',b)}
    sec.append(h);
    const pairs=[['cron',a.cron+' ('+a.tz+')'],['다음',a.state==='paused'?'-':when(a.next_fire_at)],['사전 확인',a.check?node('code',a.check):'-'],['대화',a.resume?'이어서':'새 대화'],['최대 실행',Math.round(a.max_run_ms/60000)+'분'],['알림 받는 역할',(a.notify||[]).join(', ')||'-'],['프롬프트',node('pre',a.prompt)]];
    sec.append(dl(pairs));
    const acts=node('div','','detail-acts');
    const add=(label,fn)=>{const b=node('button',label);b.type='button';b.onclick=fn;acts.append(b)};
    add('지금 실행',()=>autoAct(v,a,'POST','/run',autoName(a)+' 실행'));
    if(a.state==='paused')add('다시',()=>autoAct(v,a,'POST','/resume',autoName(a)+' 다시'));
    else add('멈춤',()=>autoAct(v,a,'POST','/pause',autoName(a)+' 멈춤'));
    add('지우기',()=>{if(confirm(autoName(a)+'을(를) 지울까요? 실행 기록도 함께 지워지고 되돌릴 수 없습니다.'))autoAct(v,a,'DELETE','',autoName(a)+' 삭제')});
    sec.append(acts,autoRuns(a.runs||[]));
    box.append(sec);
  }
}
const termLoad=async id=>(await api('terminals')).find(v=>v.id===id)||null;
registerDetail('terminals',{
  list:'터미널',
  load:termLoad,
  head:v=>{const st=termSt(v);return {title:termRole(v)+(v.label?' · '+v.label:''),badge:[termState[st]||st,termTone[termStClass(st)]||'neutral']}},
  subs:()=>[['screen','화면'],['log','기록'],['autos','예약'],['settings','설정']],
  draw:async(v,sub,box)=>{
    termActs(v,box);
    if(sub==='log'){await termHistory(v,box);return}
    if(sub==='settings'){termSettings(v,box);return}
    if(sub==='autos'){await termAutos(v,box);return}
    box.append(termPaneFor(v));
  }
});
$('termAddOpen').onclick=()=>$('termAdd').showModal();
$('termAddOpen').addEventListener('click',()=>{adapterOptions($('termAdapter'),$('termNode').value.trim(),$('termAdapter').value);groupOptions($('termGroup'),$('termGroup').value)});
$('termNode').onchange=()=>adapterOptions($('termAdapter'),$('termNode').value.trim(),'');
// adapterOptions fills sel with the agent adapters the node reported (#434);
// the first option, the empty value, is the node's default.
// groupOptions fills sel with the work groups (#435); the empty value is none.
async function groupOptions(sel,cur){
  let list=[];
  try{list=(await api('terminals/work-groups')).map(g=>g.name)}catch(e){}
  if(cur&&!list.includes(cur))list=list.concat(cur);
  const first=node('option','없음');first.value='';
  sel.replaceChildren(first,...list.map(g=>{const o=node('option',g);o.value=g;return o}));
  sel.value=cur||'';
}
async function adapterOptions(sel,nodeName,cur){
  let list=[],def='';
  try{const n=(await api('nodes')).find(x=>x.name===nodeName);if(n){list=n.adapters||[];def=n.default_adapter||''}}catch(e){}
  if(cur&&!list.includes(cur))list=list.concat(cur);
  const first=node('option',def?'기본 · '+def:'기본');first.value='';
  sel.replaceChildren(first,...list.map(a=>{const o=node('option',a);o.value=a;return o}));
  sel.value=cur||'';
}
$('termAddClose').onclick=()=>$('termAdd').close();
// The drawer takes its own Esc, so an open detail panel stays open.
$('termAdd').addEventListener('keydown',e=>{if(e.key==='Escape'){e.preventDefault();$('termAdd').close()}});
// ---- 터미널 화면 ----
// With hosting on the page loads the workspace (internal/dashboard/workspace,
// files listed by /api/admin/me as workspace_assets): the three-column frame
// and the terminal panels, one socket and renderer per panel. ws is that
// workspace; null until it has loaded, and always with hosting off.
// #termHold keeps the old single screen's markup, unused.
let ws=null,wsFailed='';
// termRefusedText is why this address cannot open a terminal
// (terminal_refused from /api/admin/me, SWITCHBOARD_HOSTING_TERMINAL).
const termRefusedText={off:'터미널이 꺼져 있음',loopback:'이 주소에서는 터미널이 꺼져 있음 (이 기계에서만 열림)',allow:'이 주소는 터미널 허용 대역 밖'};
// termPaneFor(v) is what a detail body shows for the terminal: the workspace
// opens it as a centre panel.
function termPaneFor(v){
  if(termRefused)return node('p',termRefusedText[termRefused]||'이 주소에서는 터미널이 꺼져 있음','msg err');
  if(ws)return ws.termPaneFor(v);
  return wsFailed?node('p',wsFailed,'msg err'):node('p','불러오는 중…','muted');
}
function termGc(){if(ws)ws.termGc()}
// loadAssets adds stylesheets and scripts (same origin) in order.
async function loadAssets(list){
  for(const u of list){
    await new Promise((ok,fail)=>{
      let el;
      if(u.endsWith('.css')){el=document.createElement('link');el.rel='stylesheet';el.href=u}
      else{el=document.createElement('script');el.src=u}
      el.onload=ok;el.onerror=()=>fail(Error('불러오기 실패: '+u));document.head.append(el);
    });
  }
}
let wsLoading=null;
function loadWorkspace(list){
  if(ws||wsLoading||!list.length)return;
  wsLoading=loadAssets(list).then(()=>{ws=wsStart()}).catch(e=>{wsFailed=e.message}).then(()=>{if(detailCur)drawDetail()});
}
$('termForm').onsubmit=async e=>{
  e.preventDefault();
  const args=$('termArgs').value.split(/\s+/).filter(Boolean);
  const rec={node:$('termNode').value.trim(),base:$('termBase').value.trim(),profile:$('termProfile').value.trim(),directory:$('termDir').value.trim(),label:$('termLabel').value.trim(),parent:$('termParent').value.trim(),restart:$('termRestart').value,group:$('termGroup').value,adapter:$('termAdapter').value,skip_permissions:$('termPerms').value==='skip',args:args};
  try{await apiSend('POST','terminals',rec);setMsg($('terminalsMsg'),rec.base+' 추가','ok');$('termForm').reset();$('termAdd').close();loadTerminals()}
  catch(err){setMsg($('termAddMsg'),err.message,'err')}
};
$('termClear').onclick=()=>{$('termForm').reset();setMsg($('termAddMsg'),'')};
$('reloadTerminals').onclick=loadTerminals;
// targetPath keeps the slashes of a target (svc/coordinator, svc/*) as path separators.
function targetPath(t){return t.split('/').map(encodeURIComponent).join('/')}
// ---- 설정 ----
// Four views under the 설정 tab: 질문 설정 (#settings), 역할
// (#settings-roles), 노드 (#settings-nodes, hosting only) and 다시 알림
// (#settings-reminders, GET /api/admin/settings, read-only). A row opens
// #settings/<kind>:<id>/<sub> with kind qs (question setting target), role
// or node; closing goes back to that kind's view.
const setViewHash={qsettings:'settings',roles:'settings-roles',nodes:'settings-nodes',groups:'settings-groups',reminders:'settings-reminders'};
const setKindView={qs:'qsettings',role:'roles',node:'nodes'};
let setView='qsettings';
const setNotes=new Map(),setForms=new Map();
function setViewButton(v){return $('setViews').querySelector('[data-view="'+v+'"]')}
function showSettingsView(v){
  if(!setViewHash[v]||((v==='nodes'||v==='groups')&&!hostingOn))v='qsettings';
  const changed=v!==setView;setView=v;
  for(const k of Object.keys(setViewHash)){$('setView-'+k).hidden=k!==v;setViewButton(k).setAttribute('aria-pressed',k===v?'true':'false')}
  if(changed){$('settingsNote').textContent='';setMsg($('settingsMsg'),'');if(tabLoaded.has('settings'))loadSettingsView()}
}
function settingsViewOfHash(h){for(const [v,x] of Object.entries(setViewHash))if(x===h)return v;return ''}
$('setViews').addEventListener('click',e=>{const b=e.target.closest('button[data-view]');if(b)selectTab(setViewHash[b.dataset.view])});
function loadSettingsView(){return ({qsettings:loadQSettings,roles:loadRoles,nodes:loadNodes,groups:loadGroups,reminders:loadReminders})[setView]()}
tabLoaders.settings=loadSettingsView;
// setNote keeps a result line for an item's detail across the redraws of
// the next few seconds (a save also triggers a live redraw).
function setNote(id,text,kind){setNotes.set(id,{text:text,kind:kind,at:Date.now()})}
function drawSetNote(box,id){
  const n=setNotes.get(id),m=node('div','','msg');m.setAttribute('role','status');
  if(n&&Date.now()-n.at<10000)setMsg(m,n.text,n.kind);
  box.append(m);
}
function setButton(text,cls,run){const b=node('button',text,cls);b.type='button';b.onclick=run;return b}
// cardText sets the words a two-line card shows before a value (the table
// header says it on a wide screen).
function cardText(td,pre){td.dataset.pre=pre;return td}
// 질문 설정
const qsCols=['대상','질문 가능','지켜보는 역할','디스코드','바뀐 때'];
async function loadQSettings(){
  $('settingsNote').textContent='불러오는 중…';
  try{
    const [rows,refused]=await Promise.all([api('question-settings'),api('question-refusals')]);
    if(setView!=='qsettings')return;
    drawRefusals(refused);
    const body=$('settingsTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const v of rows){
        const tr=node('tr','');
        const ca=cell('');ca.append(badge(v.can_ask?'질문 가능':'질문 불가',v.can_ask?'success':'neutral'));
        const at=cell(shortWhen(v.updated_at),qsCols[4],'nowrap');at.title=when(v.updated_at);
        tr.append(cell(v.target,'','strong mono'),ca,cardBreak(),cardText(cell(v.watchers.length?v.watchers.join(', '):'-',qsCols[2]),'지켜봄 '),cardText(cell(v.discord?'켬':'끔',qsCols[3],'nowrap'),'디스코드 '),at);
        detailRow(tr,'settings','qs:'+v.target);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,qsCols.length,'설정 없음');
    });
    $('settingsNote').textContent=rows.length+'개';
  }catch(e){if(setView==='qsettings')$('settingsNote').textContent=e.message}
}
// drawRefusals lists the roles refused for can_ask in the last 24 h (#286).
const refCols=['역할','맞은 대상','대신 물을 역할','횟수','마지막 거절'];
function drawRefusals(rows){
  const body=$('refusalsTable');body.replaceChildren();
  for(const v of rows){
    const tr=node('tr','');
    const at=cell(shortWhen(v.at),refCols[4],'nowrap');at.title=when(v.at);
    tr.append(cell(v.role,'','strong mono'),cardText(cell(v.target==='none'?'없음':v.target,refCols[1],'mono'),'대상 '),cardText(cell(v.ask_via||'-',refCols[2],'mono'),'물을 곳 '),cardText(cell(String(v.count),refCols[3],'num'),'횟수 '),at);
    body.append(tr);
  }
  if(!rows.length)emptyRow(body,refCols.length,'거절 없음');
}
// qsForm is the edit form of one target. It is kept across live redraws
// while the stored setting is unchanged, so a half-made edit survives them.
function qsForm(v){
  const kept=setForms.get(v.target);
  if(kept&&kept.stamp===v.updated_at)return kept.form;
  const f=node('form','','settings-form termform');
  const check=(text,on)=>{const l=node('label','','check'),i=document.createElement('input');i.type='checkbox';i.checked=on;l.append(i,' '+text);return [l,i]};
  const [l1,canAsk]=check('질문할 수 있음 (can_ask)',v.can_ask),[l2,discord]=check('디스코드 알림 (discord)',v.discord);
  const l3=node('label','지켜보는 역할 (쉼표로 구분, 최대 20) '),w=document.createElement('input');
  w.value=v.watchers.join(', ');w.autocomplete='off';w.spellcheck=false;w.setAttribute('autocapitalize','off');w.placeholder='team/boss, ops/lead';l3.append(w);
  const save=node('button','저장');save.type='submit';
  const row=node('div','','row');row.append(save);
  f.append(l1,l2,l3,row);
  const id='qs:'+v.target;
  f.onsubmit=async e=>{
    e.preventDefault();
    const watchers=w.value.split(',').map(x=>x.trim()).filter(Boolean);
    try{
      await apiSend('PUT','question-settings/'+targetPath(v.target),{can_ask:canAsk.checked,watchers:watchers,discord:discord.checked});
      setForms.delete(v.target);setNote(id,v.target+' 설정을 저장했습니다.','ok');
    }catch(err){setNote(id,err.message,'err')}
    loadQSettings();drawDetail();
  };
  setForms.set(v.target,{stamp:v.updated_at,form:f});
  return f;
}
function drawQS(v,box){
  if(v.target!=='default'){
    const acts=node('div','','detail-acts');
    acts.append(setButton('삭제','',async()=>{
      if(!confirm(v.target+' 설정을 지울까요?'))return;
      try{await apiSend('DELETE','question-settings/'+targetPath(v.target));setForms.delete(v.target);setMsg($('settingsMsg'),v.target+' 설정을 지웠습니다.','ok');closeDetail();loadQSettings()}
      catch(e){setNote('qs:'+v.target,e.message,'err');drawDetail()}
    }));
    box.append(acts);
  }
  drawSetNote(box,'qs:'+v.target);
  box.append(dl([['대상',v.target],['바뀐 때',when(v.updated_at)]]),qsForm(v));
}
$('setAddOpen').onclick=()=>{setMsg($('setAddMsg'),'');$('setAdd').showModal();$('setTarget').focus()};
$('setAddClose').onclick=()=>$('setAdd').close();
$('setAdd').addEventListener('keydown',e=>{if(e.key==='Escape'){e.preventDefault();$('setAdd').close()}});
$('settingsForm').onsubmit=async e=>{
  e.preventDefault();
  const target=$('setTarget').value.trim();
  if(!target){setMsg($('setAddMsg'),'대상을 쓰세요.','err');return}
  const watchers=$('setWatchers').value.split(',').map(x=>x.trim()).filter(Boolean);
  try{
    await apiSend('PUT','question-settings/'+targetPath(target),{can_ask:$('setCanAsk').checked,watchers:watchers,discord:$('setDiscord').checked});
    $('settingsForm').reset();$('setAdd').close();setForms.delete(target);
    setMsg($('settingsMsg'),target+' 설정을 저장했습니다.','ok');loadQSettings();
    openDetail('settings','qs:'+target,'edit');
  }catch(err){setMsg($('setAddMsg'),err.message,'err')}
};
$('setClear').onclick=()=>{$('settingsForm').reset();setMsg($('setAddMsg'),'')};
// 역할
const roleCols=['역할','상태','등록','끊은 때'];
async function loadRoles(){
  $('settingsNote').textContent='불러오는 중…';
  try{
    const rows=await api('roles');
    if(setView!=='roles')return;
    const body=$('rolesTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const v of rows){
        const tr=node('tr','',v.revoked_at?'done':'');
        const st=cell('');st.append(badge(v.revoked_at?'끊김':'사용 중',v.revoked_at?'danger':'success'));
        const at=cardText(cell(shortWhen(v.created_at),roleCols[2],'nowrap'),'등록 ');at.title=when(v.created_at);
        const off=cardText(cell(v.revoked_at?shortWhen(v.revoked_at):'-',roleCols[3],v.revoked_at?'nowrap revoked':'nowrap cardhide'),'끊음 ');if(v.revoked_at)off.title=when(v.revoked_at);
        tr.append(cell(v.role,'','strong mono'),st,cardBreak(),at,off);
        detailRow(tr,'settings','role:'+v.role);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,roleCols.length,'역할 없음');
    });
    const off=rows.filter(v=>v.revoked_at).length;
    $('settingsNote').textContent=rows.length+'개'+(off?' · 끊김 '+off:'');
  }catch(e){if(setView==='roles')$('settingsNote').textContent=e.message}
}
function drawRole(v,box){
  const id='role:'+v.role,revoke=!v.revoked_at,acts=node('div','','detail-acts');
  acts.append(setButton(revoke?'끊기':'되살리기','',async()=>{
    const used=revoke&&v.last_used_at&&Date.now()-new Date(v.last_used_at).getTime()<864e5;
    if(!confirm(revoke?v.role+' 역할을 끊을까요? 이 토큰으로 접속한 세션이 모두 끊깁니다.'+(used?'\n최근 사용: '+when(v.last_used_at)+(v.last_machine?' · '+v.last_machine:''):''):v.role+' 역할을 되살릴까요?'))return;
    try{await apiSend('POST','roles/'+targetPath(v.role)+(revoke?'/revoke?force=1':'/restore'));setNote(id,v.role+(revoke?' 역할을 끊었습니다.':' 역할을 되살렸습니다.'),'ok')}
    catch(e){setNote(id,e.message,'err')}
    loadRoles();drawDetail();
  }));
  box.append(acts);
  drawSetNote(box,id);
  box.append(dl([['역할',v.role],['등록',when(v.created_at)],['끊은 때',v.revoked_at?when(v.revoked_at):'-']]));
}
// 노드 (hosting)
const nodeCols=['노드','기계','OS','버전','마지막 접속','끊은 때'];
async function loadNodes(){
  if(!hostingOn)return;
  $('settingsNote').textContent='불러오는 중…';
  try{
    const rows=await api('nodes');
    if(setView!=='nodes')return;
    const body=$('nodesTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const v of rows){
        const tr=node('tr','',v.revoked_at?'offline':'');
        const name=cell(v.name,'','strong mono');
        if(v.revoked_at)name.append(' ',badge('끊김','danger'));
        const seen=cardText(cell(when(v.last_seen_at),nodeCols[4],'nowrap'),'접속 ');
        const off=cell(v.revoked_at?when(v.revoked_at):'-',nodeCols[5],v.revoked_at?'revoked cardhide':'cardhide');
        tr.append(name,cardBreak(),cell(v.machine||'-',nodeCols[1]),cell(v.os||'-',nodeCols[2]),cell(v.version||'-',nodeCols[3]),seen,off);
        detailRow(tr,'settings','node:'+v.name);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,nodeCols.length,'노드 없음');
    });
    const off=rows.filter(v=>v.revoked_at).length;
    $('settingsNote').textContent=rows.length+'개'+(off?' · 끊김 '+off:'');
  }catch(e){if(setView==='nodes')$('settingsNote').textContent=e.message}
}
// 묶음 (#435): work groups and their thresholds
const groupCols=['묶음','문턱 %','세션','바뀐 때',''];
function thresholdOf(input){const t=input.value.trim();return t===''?null:Number(t)}
async function loadGroups(){
  if(!hostingOn)return;
  $('settingsNote').textContent='불러오는 중…';
  try{
    const rows=await api('terminals/work-groups');
    if(setView!=='groups')return;
    const body=$('groupsTable');
    keepRowFocus(body,()=>{
      body.replaceChildren();
      for(const g of rows){
        const tr=node('tr');
        const th=document.createElement('input');th.type='number';th.min='0';th.max='100';th.step='1';th.inputMode='numeric';
        th.value=g.threshold===null||g.threshold===undefined?'':String(g.threshold);th.setAttribute('aria-label',g.name+' 문턱 %');
        const save=setButton('저장','',async()=>{
          try{await apiSend('PUT','terminals/work-groups/'+encodeURIComponent(g.name),{threshold:thresholdOf(th)});setMsg($('settingsMsg'),g.name+' 문턱 저장','ok')}
          catch(e){setMsg($('settingsMsg'),e.message,'err')}
          loadGroups();
        });
        const rm=setButton('지우기','',async()=>{
          if(!confirm(g.name+' 묶음을 지울까요?'))return;
          try{await apiSend('DELETE','terminals/work-groups/'+encodeURIComponent(g.name));setMsg($('settingsMsg'),g.name+' 묶음을 지웠습니다.','ok')}
          catch(e){setMsg($('settingsMsg'),e.message,'err')}
          loadGroups();
        });
        rm.disabled=g.sessions>0;if(g.sessions>0)rm.title='세션이 있는 묶음은 지울 수 없음';
        const tc=cell('',groupCols[1]);tc.append(th,' ',save);
        const acts=cell('',groupCols[4]);acts.append(rm);
        tr.append(cell(g.name,'','strong mono'),cardBreak(),tc,cell(String(g.sessions),groupCols[2],'num'),cell(when(g.updated_at),groupCols[3],'nowrap'),acts);
        body.append(tr);
      }
      if(!rows.length)emptyRow(body,groupCols.length,'묶음 없음');
    });
    $('settingsNote').textContent=rows.length+'개';
  }catch(e){if(setView==='groups')$('settingsNote').textContent=e.message}
}
$('groupForm').onsubmit=async e=>{
  e.preventDefault();
  const name=$('groupName').value.trim();
  try{await apiSend('POST','terminals/work-groups',{name:name,threshold:thresholdOf($('groupThreshold'))});$('groupName').value='';$('groupThreshold').value='';setMsg($('settingsMsg'),name+' 묶음 추가','ok')}
  catch(err){setMsg($('settingsMsg'),err.message,'err')}
  loadGroups();
};
function drawNode(v,box){
  const id='node:'+v.name;
  if(!v.revoked_at){
    const acts=node('div','','detail-acts');
    acts.append(setButton('끊기','',async()=>{
      if(!confirm(v.name+' 노드를 끊을까요? 연결이 바로 끊기고 이 노드의 토큰은 다시 쓸 수 없습니다.'))return;
      try{await apiSend('DELETE','nodes/'+encodeURIComponent(v.name));setNote(id,v.name+' 노드를 끊었습니다.','ok')}
      catch(e){setNote(id,e.message,'err')}
      loadNodes();drawDetail();
    }));
    box.append(acts);
  }
  drawSetNote(box,id);
  box.append(dl([['노드',v.name],['기계',v.machine||'-'],['OS',v.os||'-'],['버전',v.version||'-'],['등록',when(v.created_at)],['마지막 접속',when(v.last_seen_at)],['끊은 때',v.revoked_at?when(v.revoked_at):'-']]));
  if(v.accounts&&v.accounts.length)box.append(accountTable(v,id));
}
// 계정 (#435): each agent account's remaining limit and the operator's value
const accountCols=['계정','남은 %','출처','세션','수동 %',''];
const accountSource={operator:'한도 켬',manual:'수동',adapter:'에이전트',screen:'화면'};
function accountTable(v,id){
  const t=node('table','','list'),head=node('tr'),body=document.createElement('tbody');
  for(const c of accountCols)head.append(node('th',c));
  const th=document.createElement('thead');th.append(head);t.append(th,body);
  for(const a of v.accounts){
    const tr=node('tr'),name=a.adapter+' / '+(a.account||'기본');
    const inp=document.createElement('input');inp.type='number';inp.min='0';inp.max='100';inp.step='1';inp.inputMode='numeric';
    inp.value=a.manual===null||a.manual===undefined?'':String(a.manual);inp.setAttribute('aria-label',name+' 수동 %');
    const send=async remaining=>{
      try{await apiSend('PUT','terminals/limit/accounts',{node:v.name,adapter:a.adapter,account:a.account||'',remaining:remaining});setNote(id,name+(remaining===null?' 수동 % 지움':' 수동 % 저장'),'ok')}
      catch(e){setNote(id,e.message,'err')}
      loadNodes();drawDetail();
    };
    const save=setButton('저장','',()=>send(thresholdOf(inp)));
    const clear=setButton('지우기','',()=>send(null));clear.disabled=a.manual===null||a.manual===undefined;
    const manual=document.createElement('td');manual.dataset.label=accountCols[4];manual.append(inp);
    const acts=document.createElement('td');acts.append(save,' ',clear);
    tr.append(cell(name,accountCols[0],'mono'),cell(a.remaining===null||a.remaining===undefined?'모름':a.remaining+'%',accountCols[1]),cell(accountSource[a.source]||'-',accountCols[2]),cell(String(a.sessions||0),accountCols[3]),manual,acts);
    body.append(tr);
  }
  return t;
}
// ruleLast: the newest answered-rule step (#274), with the closed IDs.
function ruleLast(l){
  if(!l)return '-';
  const ids=(l.ids||[]).map(i=>'#'+i).join(' ');
  return (l.from?l.from+' → '+l.to:l.to+' 저장')+' · '+l.marked+'건 닫음 · '+when(l.at)+(ids?' · '+ids:'');
}
// 다시 알림 (#223): the reminder settings and the server version, as the
// start-up line reports them.
async function loadReminders(){
  $('settingsNote').textContent='불러오는 중…';
  try{
    const s=await api('settings');
    if(setView!=='reminders')return;
    const r=s.reminders,st=document.createElement('span');
    st.append(badge(r.on?'켬':'끔',r.on?'success':'neutral'));
    $('remindBox').replaceChildren(dl([['다시 알림',st],['첫 알림',r.after],['최대 횟수',String(r.max)],['멈춤 문턱',r.idle==='off'?'끔':r.idle],['멈춤 알림',r.idle==='off'?'-':String(r.idle_max||1)+'번'],['켠 때',r.since?when(r.since):r.on?'첫 점검 전':'-'],['서버 판',s.version],['터미널 호스팅',s.hosting?'켬':'끔'],['답 규칙 판',String(s.answered_rule.version)],['마지막 이주',ruleLast(s.answered_rule.last)]]));
    $('settingsNote').textContent='';
  }catch(e){if(setView==='reminders')$('settingsNote').textContent=e.message}
}
async function findSetting(kind,key){
  if(kind==='qs')return (await api('question-settings')).find(x=>x.target===key);
  if(kind==='role')return (await api('roles')).find(x=>x.role===key);
  if(kind==='node'&&hostingOn)return (await api('nodes')).find(x=>x.name===key);
  return undefined;
}
registerDetail('settings',{
  list:'설정',kinds:['settings','terminals'],
  back:id=>setViewHash[setKindView[id.slice(0,id.indexOf(':'))]]||'settings',
  load:async id=>{
    const i=id.indexOf(':'),k=id.slice(0,i),v=i>0?await findSetting(k,id.slice(i+1)):undefined;
    return v?{k:k,v:v}:null;
  },
  head:x=>x.k==='qs'?{title:x.v.target,badge:x.v.can_ask?['질문 가능','success']:['질문 불가','neutral']}
    :x.k==='role'?{title:x.v.role,badge:x.v.revoked_at?['끊김','danger']:['사용 중','success']}
    :{title:x.v.name,badge:x.v.revoked_at?['끊김','danger']:null},
  subs:x=>x.k==='qs'?[['edit','설정']]:[['info','정보']],
  draw:(x,sub,box)=>x.k==='qs'?drawQS(x.v,box):x.k==='role'?drawRole(x.v,box):drawNode(x.v,box)
});
// ---- 권한 요청 ----
// GET /api/admin/permissions?status=pending and ?status=closed; answers go to
// POST /api/admin/permissions/{id}/answer, the same server function as
// permission answer. Every field is a text node.
const permStatusLabel={pending:'대기',allowed:'허용',denied:'거절',expired:'만료'};
function permFields(v){
  const dl=node('dl','','pdl');
  const rows=[['ID',v.id],['요청한 역할',v.requester],['기본 역할',v.requester_base],['도구',v.tool],
    ['부모',v.parent||'-'],['부모 기본 역할',v.parent_base||'-'],['보고된 부모',v.declared_parent||'-'],
    ['요청',when(v.created_at)],['만료',when(v.expires_at)],['결과',permStatusLabel[v.status]||v.status]];
  if(v.decision)rows.push(['결정',v.decision]);
  if(v.note)rows.push(['메모',v.note]);
  if(v.answered_by)rows.push(['답한 쪽',v.answered_by],['답한 때',when(v.answered_at)]);
  for(const [k,t] of rows)dl.append(node('dt',k),node('dd',t));
  return dl;
}
const permTone={pending:'warning',allowed:'success',denied:'danger',expired:'neutral'};
const permCols=['상태','도구 · 요약','요청한 쪽','요청','만료'],permHistCols=['결과','도구 · 요약','요청한 쪽','요청','답한 쪽'];
function permAnswerForm(v){
  const form=node('form','','panswer');
  const lab=node('label','메모');const note=node('input','');note.maxLength=500;note.autocomplete='off';lab.append(note);
  const allow=node('button','허용','ok');allow.type='button';
  const deny=node('button','거절','danger');deny.type='button';
  const msg=node('div','','msg');msg.setAttribute('role','status');
  const send=async decision=>{
    const word=decision==='allow'?'허용':'거절';
    if(!confirm(v.requester+'의 '+v.tool+' 요청을 '+word+'할까요? 답은 바꿀 수 없습니다.'))return;
    allow.disabled=deny.disabled=true;
    try{
      await apiSend('POST','permissions/'+encodeURIComponent(v.id)+'/answer',{decision:decision,note:note.value});
      setMsg($('permsMsg'),v.id+' '+word+'했습니다.','ok');
      $('detailTitle').focus({preventScroll:true});
      drawDetail();
      if(curTab==='permissions')loadPermissions();
    }catch(e){setMsg(msg,e.message,'err');allow.disabled=deny.disabled=false}
  };
  allow.onclick=()=>send('allow');deny.onclick=()=>send('deny');
  form.onsubmit=e=>e.preventDefault();
  const row=node('div','','row');row.append(allow,deny);
  form.append(lab,row,msg);
  return form;
}
function permRow(v,hist){
  const tr=node('tr','',v.status==='pending'?'':'done');
  const st=cell('');st.append(badge(permStatusLabel[v.status]||v.status,permTone[v.status]||'neutral'));
  const t=cell('',permCols[1]);t.append(node('strong',v.tool),node('small',v.summary));
  const at=cell(shortWhen(v.created_at),permCols[3],'nowrap');at.title=when(v.created_at);
  const last=hist?cell(v.answered_by||'-',permHistCols[4]):cell(shortWhen(v.expires_at),permCols[4],'nowrap');
  if(!hist)last.title=when(v.expires_at);
  tr.append(st,t,cell(v.requester,permCols[2]),at,last);
  detailRow(tr,'permissions',v.id);
  return tr;
}
let permTimer=0;
async function loadPermissions(){
  $('permsNote').textContent='불러오는 중…';
  try{
    const [open,closed]=await Promise.all([api('permissions?status=pending'),api('permissions?status=closed&limit=200')]);
    const p=open.permissions,h=closed.permissions;
    const pb=$('permPending'),hb=$('permHistory');
    keepRowFocus(pb,()=>{pb.replaceChildren(...p.map(v=>permRow(v,false)));if(!p.length)emptyRow(pb,permCols.length,'대기 중인 요청 없음')});
    keepRowFocus(hb,()=>{hb.replaceChildren(...h.map(v=>permRow(v,true)));if(!h.length)emptyRow(hb,permHistCols.length,'기록 없음')});
    setTabCount('permissions',p.length||null);
    $('permsNote').textContent='대기 '+p.length+' · 기록 '+h.length+'(최대 200)';
    // A pending request turns expired by time alone: reload at the next expiry.
    clearTimeout(permTimer);
    const next=Math.min(...p.map(v=>new Date(v.expires_at).getTime()));
    if(isFinite(next))permTimer=setTimeout(loadPermissions,Math.max(1000,next-Date.now()+500));
  }catch(e){$('permsNote').textContent=e.message}
}
registerDetail('permissions',{
  list:'권한 요청',kinds:['permissions'],
  load:async id=>{
    try{return await api('permissions/'+encodeURIComponent(id))}
    catch(e){if(/\(404\)/.test(e.message))return null;throw e}
  },
  head:v=>({title:v.tool+' · '+v.requester,badge:[permStatusLabel[v.status]||v.status,permTone[v.status]||'neutral']}),
  subs:()=>[['request','요청']],
  draw:async(v,sub,box)=>{
    box.append(permFields(v));
    if(v.summary)box.append(node('pre',v.summary));
    if(v.status==='pending'&&v.can_answer)box.append(permAnswerForm(v));
  }
});
async function refreshPermissionCount(){
  try{const p=(await api('permissions?status=pending')).permissions;setTabCount('permissions',p.length||null)}catch(e){}
}
$('reloadPerms').onclick=()=>{setMsg($('permsMsg'),'');loadPermissions()};
tabLoaders.permissions=loadPermissions;
// ---- 실시간 ----
// GET /api/admin/live (WebSocket) sends {type:'hello',boot,seq,reset,kinds}
// and {type:'change',seq,kinds}; kinds are tab names. The shown tab reloads
// (not while a field in it has focus), the others reload when next shown.
// Reconnects back off exponentially (1s to 30s) and resume with boot+after.
const liveReload={progress:loadProgress,sessions:loadSessions,reports:loadReports,jobs:loadJobs,metrics:loadMetrics,mail:load,permissions:loadPermissions,rules:loadRules,questions:loadQuestions,settings:loadSettingsView,terminals:loadTerminals};
const liveCounts={reports:refreshReportCount,questions:refreshQuestionCount,permissions:refreshPermissionCount,metrics:refreshMetricsCount};
let liveBoot='',liveSeq=0,liveDelay=1000,liveDirty=new Set(),liveStale=new Set(),liveTimer=0,liveDetail=false;
function liveMark(kinds){
  if(ws)ws.live(kinds);
  for(const k of kinds){
    if(detailCur&&(details[detailCur.tab].kinds||[detailCur.tab]).includes(k))liveDetail=true;
    if(!liveReload[k])continue;
    if(tabShown(k))liveDirty.add(k);
    else{if(tabLoaded.has(k))liveStale.add(k);if(liveCounts[k])liveCounts[k]()}
  }
  if((liveDirty.size||liveDetail)&&!liveTimer)liveTimer=setTimeout(liveFlush,300);
}
function liveFlush(){
  liveTimer=0;
  const a=document.activeElement,busy=a&&a.matches&&a.matches('input,textarea,select')&&($('panel-'+curTab).contains(a)||$('detail').contains(a));
  if(busy){liveTimer=setTimeout(liveFlush,2000);return}
  for(const k of liveDirty){if(tabShown(k))liveReload[k]();else if(tabLoaded.has(k))liveStale.add(k)}
  liveDirty.clear();
  if(liveDetail){liveDetail=false;drawDetail()}
}
function liveConnect(){
  const u=new URL('/api/admin/live',location.href);
  u.protocol=location.protocol==='https:'?'wss:':'ws:';
  if(liveBoot){u.searchParams.set('boot',liveBoot);u.searchParams.set('after',String(liveSeq))}
  let ws;
  try{ws=new WebSocket(u)}catch(e){liveRetry();return}
  ws.onmessage=e=>{
    let m;try{m=JSON.parse(e.data)}catch(err){return}
    if(m.type==='hello'){
      liveDelay=1000;$('live').className='live on';$('live').title='실시간';$('liveText').textContent='실시간';
      const all=m.reset;liveBoot=m.boot;liveSeq=m.seq;
      liveMark(all?Object.keys(liveReload):m.kinds||[]);
    }else if(m.type==='change'){liveSeq=m.seq;liveMark(m.kinds||[])}
  };
  ws.onclose=()=>{$('live').className='live';$('live').title='실시간 연결 끊김';$('liveText').textContent='끊김';liveRetry()};
}
function liveRetry(){
  const d=liveDelay;liveDelay=Math.min(liveDelay*2,30000);
  setTimeout(liveConnect,d/2+Math.random()*d/2);
}
let curTab='';
function showTab(name){
  if(name!=='terminals'&&$('termAdd').open)$('termAdd').close();
  if(name!=='settings'&&$('setAdd').open)$('setAdd').close();
  curTab=name;
  for(const n of tabNames){
    const on=n===name,t=$('tab-'+n);
    t.setAttribute('aria-selected',on?'true':'false');
    t.tabIndex=on?0:-1;
    // In the workspace every section shows wherever its panel is.
    $('panel-'+n).hidden=ws?false:!on;
    if(on)$('crumbTab').textContent=t.querySelector('.tab-name').textContent;
  }
  loadTab(name);
  if(ws)ws.showMenu(name);
}
// loadTab loads a tab's section the first time it shows, and again when a
// live change marked it stale meanwhile.
function loadTab(name){
  if(!tabLoaded.has(name)){tabLoaded.add(name);if(tabLoaders[name])tabLoaders[name]()}
  else if(liveStale.has(name))liveReload[name]();
  liveStale.delete(name);
}
// tabShown: the current tab, or (workspace) any section on screen.
function tabShown(k){return k===curTab||!!(ws&&ws.shown(k))}
// Routes: #progress #sessions #metrics #mail #permissions #questions #settings, #settings-roles
// #settings-nodes #settings-reminders (설정 tab views), #decisions (질문 tab,
// 결정 view), #q=<id> (rewritten to #questions/<id>/answer), #terminal=<id> (an older link: it becomes
// #terminals/<id>/screen), and #<tab>/<id>/<sub> for a tab with a detail panel
// (registerDetail). No hash or an unknown one shows 진행.
function route(){
  const h=location.hash.slice(1);
  const parts=h.split('/');
  if(parts.length>1&&details[parts[0]]&&visibleTabs().includes(parts[0])&&parts[1]){
    let id=parts[1];try{id=decodeURIComponent(id)}catch(e){}
    if(parts[0]==='questions')showQuestionView('questions');
    if(parts[0]==='settings')showSettingsView(setKindView[id.slice(0,id.indexOf(':'))]);
    showTab(parts[0]);showDetail(parts[0],id,parts[2]||'');
    return;
  }
  hideDetail();
  if(h.startsWith('terminal=')){
    let id=h.slice(9);try{id=decodeURIComponent(id)}catch(e){}
    if(!hostingOn)return;
    history.replaceState(null,'',detailHash('terminals',id,'screen'));route();
    return;
  }
  if(h==='decisions'){showTab('questions');showQuestionView('decisions');return}
  if(settingsViewOfHash(h)){showSettingsView(settingsViewOfHash(h));showTab('settings');return}
  if(h==='questions'||h.startsWith('q='))showQuestionView('questions');
  if(h.startsWith('q=')){
    // Old question links (Discord, panel) open the question's detail.
    let id=h.slice(2);try{id=decodeURIComponent(id)}catch(e){}
    history.replaceState(null,'',detailHash('questions',id,'answer'));
    route();
    return;
  }
  showTab(visibleTabs().includes(h)?h:'progress');
}
// selectTab(name) also takes 'decisions' (the 결정 view of the 질문 tab).
function selectTab(name){
  if(location.hash==='#'+name)route();else location.hash=name;
}
for(const n of tabNames)$('tab-'+n).onclick=()=>selectTab(n);
// visibleTabs leaves out 터미널 unless hosting is on.
function visibleTabs(){return tabNames.filter(n=>n!=='terminals'||hostingOn)}
$('tabs').addEventListener('keydown',e=>{
  const tabs=visibleTabs();
  const i=tabs.findIndex(n=>$('tab-'+n)===document.activeElement);
  if(i<0)return;
  let j;
  if(e.key==='ArrowRight'||e.key==='ArrowDown')j=(i+1)%tabs.length;
  else if(e.key==='ArrowLeft'||e.key==='ArrowUp')j=(i-1+tabs.length)%tabs.length;
  else if(e.key==='Home')j=0;
  else if(e.key==='End')j=tabs.length-1;
  else return;
  e.preventDefault();
  selectTab(tabs[j]);
  $('tab-'+tabs[j]).focus();
});
window.addEventListener('hashchange',route);

$('go').onclick=()=>{mailID='';load()};
$('reloadMail').onclick=load;
$('reloadSessions').onclick=loadSessions;
$('reloadProgress').onclick=loadProgress;
$('reloadMetrics').onclick=loadMetrics;
$('metDays7').onclick=()=>setMetricsDays(7);
$('metDays30').onclick=()=>setMetricsDays(30);
$('reloadReports').onclick=()=>loadReports();
$('reloadRules').onclick=()=>loadRules();
$('ruleService').onchange=()=>loadRules();
$('ruleTopic').onchange=()=>loadRules();
$('ruleAll').onchange=()=>loadRules();
$('ruleProposed').onclick=()=>{rulesProposed=!rulesProposed;rulesKind='';loadRules()};
$('ruleForm').onsubmit=e=>{
  e.preventDefault();
  const body=Object.assign(ruleSourceBody($('ruleSource').value),{kind:$('ruleKind').value,text:$('ruleText').value,topic:$('ruleTopicIn').value.trim(),new_topic:$('ruleNewTopic').checked,scope:parseRuleScope($('ruleScope').value),group:$('ruleGroup').value.trim(),match:ruleMatchBody($('ruleWhenTool').value,$('ruleWhenCommand').value,$('ruleWhenPath').value)});
  postRule(body,$('ruleAddMsg'),r=>{for(const id of ['ruleText','ruleSource','ruleWhenTool','ruleWhenCommand','ruleWhenPath'])$(id).value='';loadRules();openDetail('rules',String(r.id))});
};
$('reloadJobs').onclick=()=>loadJobs();
$('jobRole').onchange=$('jobState').onchange=()=>loadJobs();
$('reloadQuestions').onclick=()=>{setMsg($('questionsMsg'),'');loadQuestions()};
$('reloadSettings').onclick=()=>{setMsg($('settingsMsg'),'');loadSettingsView()};
$('reset').onclick=()=>{$('thread').value='';$('task').value='';mailID='';$('search').value='';$('status').value='';$('role').value='';$('box').value='all';load()};
$('search').addEventListener('keydown',e=>{if(e.key==='Enter')load()});
$('thread').addEventListener('keydown',e=>{if(e.key==='Enter')load()});
$('task').addEventListener('keydown',e=>{if(e.key==='Enter'){mailID='';load()}});
loadMe();
route();
if(!tabLoaded.has('questions'))refreshQuestionCount();
if(!tabLoaded.has('reports'))refreshReportCount();
if(!tabLoaded.has('metrics'))refreshMetricsCount();
if(!tabLoaded.has('permissions'))refreshPermissionCount();
liveConnect();
</script>
</body>
</html>
`
