// Package rules holds the rules ledger (#406 ①): the decisions and tips in
// force, by service and topic, with the rows they replaced; the tags a
// service's roles inherit; and who reads which rows.
//
// Everything about rules lives here: the tables rules, rule_topics,
// rule_hits, rule_proposers, rule_classes, service_tags and role_tags are
// read and written only by this package (the boundary test keeps it so). What the ledger needs from other packages is
// passed in: the caller (Caller, built by the server from identity: an
// admin or a global boss reads in full and writes; any other role reads its
// own scope), a role's group (Hooks.GroupOf, from identity's groups) and the
// answered questions a row names as its source (Hooks.Answers, from asks),
// the global bosses' notice of a proposal (Hooks.Notice, from the server),
// and each person's answer to a panel question, which asks hands to
// RecordAnswer inside its own transaction.
// This package imports store and wire only.
package rules

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Caller is who calls the ledger.
type Caller struct {
	// Role is the base role, or the admin's name ("admin:<user>"); it is
	// recorded as a row's author.
	Role string
	// Full: an admin or a global boss. It reads every row and field
	// (sources, history, other services and groups) and writes.
	Full bool
	// Group is a limited reader's group prefix, "" for a role in no
	// group. Groups are apart: a reader sees its own group's rows only.
	Group string
}

// Hooks are the ledger's inputs from other packages.
type Hooks struct {
	// GroupOf is the group prefix a role belongs to, "" for none.
	GroupOf func(role string) string
	// Answers reads answered questions by ID; IDs that are unknown or not
	// answered are left out.
	Answers func(ids []string) ([]wire.Decision, error)
	// Notice sends the global bosses a mail that asks no reply (a proposal
	// waiting for approval), once per idempotency key.
	Notice func(subject, body, idem string)
}

// Service is the ledger over one database.
type Service struct {
	db    *sql.DB
	now   func() time.Time
	hooks Hooks
}

// New builds the ledger.
func New(db *sql.DB, now func() time.Time, h Hooks) *Service {
	return &Service{db: db, now: now, hooks: h}
}

// ServiceOf is a role's service: the part of its address before the first
// "/" (team/worker → team), the whole name when it has none.
func ServiceOf(role string) string {
	svc, _, _ := strings.Cut(role, "/")
	return svc
}

// groupOfService is the group the roles of a service belong to.
func (s *Service) groupOfService(svc string) string {
	if s.hooks.GroupOf == nil {
		return ""
	}
	return s.hooks.GroupOf(svc + "/")
}

func writeJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
