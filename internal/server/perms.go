package server

import (
	"net/http"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// workPerm is the permission (#476) a signed-in admin needs for an
// /api/admin/ hosting path (prefix removed) with method; "" when the path is
// not one of the work screen's. The terminal list, the nodes and the
// settings of hosting stay with every admin.
//
//   - terminals/{id}/ticket, socket, log, events, and GET of files and
//     files/raw: view (an input ticket also needs type, checked by the hub,
//     which checks type again on every frame of an open input socket)
//   - any other method on terminals/{id}/files…: files
//   - POST terminals, PUT and DELETE terminals/{id}, POST
//     terminals/{id}/restart and new-conversation: sessions
func workPerm(path, method string) string {
	rest, ok := strings.CutPrefix(path, "terminals")
	if !ok {
		return ""
	}
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		if method == http.MethodPost {
			return wire.PermSessions
		}
		return ""
	}
	id, sub, _ := strings.Cut(rest, "/")
	switch id {
	case "limit", "work-groups":
		return ""
	}
	switch {
	case sub == "" && (method == http.MethodPut || method == http.MethodDelete):
		return wire.PermSessions
	case sub == "restart" || sub == "new-conversation":
		return wire.PermSessions
	case sub == "ticket" || sub == "socket" || sub == "log" || sub == "events":
		return wire.PermView
	case sub == "files" || strings.HasPrefix(sub, "files/"):
		if method == http.MethodGet || method == http.MethodHead {
			return wire.PermView
		}
		return wire.PermFiles
	}
	return ""
}

// refusePerm answers 403 for a missing permission.
func refusePerm(w http.ResponseWriter, perm string) {
	http.Error(w, "forbidden: this login may not "+permVerb(perm), http.StatusForbidden)
}

func permVerb(perm string) string {
	switch perm {
	case wire.PermView:
		return "view the work screen (" + perm + ")"
	case wire.PermType:
		return "type into a terminal (" + perm + ")"
	case wire.PermFiles:
		return "change files (" + perm + ")"
	case wire.PermSessions:
		return "operate sessions (" + perm + ")"
	case wire.PermDiscard:
		return "discard left items (" + perm + ")"
	}
	return perm
}
