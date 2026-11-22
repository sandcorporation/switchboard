package wire

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Rules ledger (#406 ①): the rules in force, by service and topic. A rule
// row is a decision or a tip; a new row on the same scope and topic as a
// row in force replaces it or stands alongside it, chosen by the writer.

// Rule kinds.
const (
	RuleDecision = "decision"
	RuleTip      = "tip"
)

// Rule states. A replaced or void row stays in the history.
const (
	RuleActive   = "active"
	RuleReplaced = "replaced"
	RuleVoid     = "void"
)

// Rule scopes: every role of the rule's group, the roles of one or more
// services, or the roles carrying one or more tags.
const (
	ScopeAll     = "all"
	ScopeService = "service"
	ScopeTag     = "tag"
)

// MaxRuleText is the longest rule text, in bytes.
const MaxRuleText = 4000

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// CheckRuleName checks a topic or tag name: lower case letters, digits, '.',
// '_' and '-', up to 40, starting with a letter or digit.
func CheckRuleName(what, v string) error {
	if !nameRE.MatchString(v) {
		return fmt.Errorf("%s %q: use lower-case letters, digits, '.', '_' or '-' (up to 40)", what, v)
	}
	return nil
}

// RuleScope is where a rule applies. Values are services for ScopeService
// and tags for ScopeTag, sorted, without repeats; none for ScopeAll.
type RuleScope struct {
	Kind   string   `json:"kind"`
	Values []string `json:"values,omitempty"`
}

// String is the CLI form: all, service:a,b or tag:x.
func (s RuleScope) String() string {
	if s.Kind == ScopeAll || len(s.Values) == 0 {
		return s.Kind
	}
	return s.Kind + ":" + strings.Join(s.Values, ",")
}

// ParseRuleScope reads the CLI form (all, service:a,b, tag:x,y).
func ParseRuleScope(v string) (RuleScope, error) {
	kind, vals, _ := strings.Cut(strings.TrimSpace(v), ":")
	s := RuleScope{Kind: kind}
	for _, x := range strings.Split(vals, ",") {
		if x = strings.TrimSpace(x); x != "" {
			s.Values = append(s.Values, x)
		}
	}
	return s.Normal()
}

// Normal checks the scope and returns it with its values sorted and
// deduplicated.
func (s RuleScope) Normal() (RuleScope, error) {
	vals := slices.Clone(s.Values)
	slices.Sort(vals)
	vals = slices.Compact(vals)
	switch s.Kind {
	case ScopeAll:
		if len(vals) > 0 {
			return s, errors.New("scope all takes no values")
		}
		return RuleScope{Kind: ScopeAll}, nil
	case ScopeService:
		if len(vals) == 0 {
			return s, errors.New("scope service needs one or more services (service:NAME[,NAME])")
		}
		for _, v := range vals {
			if v == "" || strings.ContainsAny(v, "/ ,") {
				return s, fmt.Errorf("service %q: the part of a role before its first /", v)
			}
		}
	case ScopeTag:
		if len(vals) == 0 {
			return s, errors.New("scope tag needs one or more tags (tag:NAME[,NAME])")
		}
		for _, v := range vals {
			if err := CheckRuleName("tag", v); err != nil {
				return s, err
			}
		}
	default:
		return s, fmt.Errorf("scope %q: all, service:NAME[,NAME] or tag:NAME[,NAME]", s.Kind)
	}
	return RuleScope{Kind: s.Kind, Values: vals}, nil
}

// RuleMatch is when a rule is hit (#406 ②; stored, not used yet): a tool
// name, a command text pattern, a file path pattern. Empty is none.
type RuleMatch struct {
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
	Path    string `json:"path,omitempty"`
}

// Empty reports whether no condition is set.
func (m RuleMatch) Empty() bool { return m == RuleMatch{} }

// Rule is one row of the ledger. A role that reads only its own scope (not
// an admin, not a global boss) gets ID, Kind, Text and Topic only; the
// other fields are left empty.
type Rule struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Topic string `json:"topic"`

	Group      string     `json:"group,omitempty"` // "" for roles in no group
	Scope      *RuleScope `json:"scope,omitempty"`
	Sources    []string   `json:"sources,omitempty"` // answered question IDs
	SourceText string     `json:"source_text,omitempty"`
	Status     string     `json:"status,omitempty"`
	Replaces   []int64    `json:"replaces,omitempty"`
	ReplacedBy int64      `json:"replaced_by,omitempty"`
	Device     string     `json:"device,omitempty"` // the mechanism that enforces it; such a rule is not put into sessions
	Match      *RuleMatch `json:"match,omitempty"`
	Author     string     `json:"author,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	VoidedBy   string     `json:"voided_by,omitempty"`
	VoidedAt   *time.Time `json:"voided_at,omitempty"`
}

// RuleList is GET /api/rules. Full is true when the reader sees every
// field (an admin or a global boss).
type RuleList struct {
	Rules []Rule `json:"rules"`
	Full  bool   `json:"full"`
}

// RuleDetail is GET /api/rules/{id}: the row, and for a full reader the
// rows it replaced or that replaced it (oldest first) and the answered
// questions it names as its source.
type RuleDetail struct {
	Rule
	History []Rule     `json:"history,omitempty"`
	Answers []Decision `json:"answers,omitempty"`
}

// RuleRequest is POST /api/rules. Exactly one of Replace and Alongside is
// needed when rows in force share the scope and topic (409 otherwise).
type RuleRequest struct {
	Kind           string     `json:"kind"`
	Text           string     `json:"text"`
	Topic          string     `json:"topic"`
	NewTopic       bool       `json:"new_topic,omitempty"`
	Scope          RuleScope  `json:"scope"`
	Group          string     `json:"group,omitempty"`
	Sources        []string   `json:"sources,omitempty"`
	SourceText     string     `json:"source_text,omitempty"`
	Replace        []int64    `json:"replace,omitempty"`
	Alongside      bool       `json:"alongside,omitempty"`
	Device         string     `json:"device,omitempty"`
	Match          *RuleMatch `json:"match,omitempty"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
}

// Check checks the request's shape and normalises its scope and text.
func (q *RuleRequest) Check() error {
	if q.Kind != RuleDecision && q.Kind != RuleTip {
		return errors.New("kind: decision or tip")
	}
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" || len(q.Text) > MaxRuleText {
		return fmt.Errorf("text: 1-%d bytes", MaxRuleText)
	}
	if err := CheckRuleName("topic", q.Topic); err != nil {
		return err
	}
	s, err := q.Scope.Normal()
	if err != nil {
		return err
	}
	q.Scope = s
	q.SourceText = strings.TrimSpace(q.SourceText)
	if len(q.Sources) == 0 && q.SourceText == "" {
		return errors.New("source: one or more answered question IDs, or a text")
	}
	if len(q.SourceText) > MaxRuleText {
		return fmt.Errorf("source text: up to %d bytes", MaxRuleText)
	}
	if len(q.Replace) > 0 && q.Alongside {
		return errors.New("replace and alongside: choose one")
	}
	if len(q.Device) > 200 {
		return errors.New("device: up to 200 bytes")
	}
	return nil
}

// RuleConflict is the 409 of POST /api/rules: the rows in force on the
// same scope and topic. Post again with Replace (some or all of them) or
// Alongside.
type RuleConflict struct {
	Error string `json:"error"`
	Rules []Rule `json:"rules"`
}

// RuleTopic is one topic of GET /api/rules/topics with its rows in force
// that the reader sees.
type RuleTopic struct {
	Name   string `json:"name"`
	Active int    `json:"active"`
}

// RuleTopics is GET /api/rules/topics.
type RuleTopics struct {
	Topics []RuleTopic `json:"topics"`
}

// ServiceTags is a service's tags, which its roles inherit.
type ServiceTags struct {
	Service string   `json:"service"`
	Tags    []string `json:"tags"`
}

// RoleTags is a role's changes to its service's tags: Add more, Remove some.
type RoleTags struct {
	Role   string   `json:"role"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// RuleTags is GET /api/rules/tags (admins and global bosses).
type RuleTags struct {
	Services []ServiceTags `json:"services"`
	Roles    []RoleTags    `json:"roles"`
}

// RoleTagChange is POST /api/rules/tags/roles: tags to add to the role, to
// remove from it, and to drop from either list (inherit the service's).
type RoleTagChange struct {
	Role    string   `json:"role"`
	Add     []string `json:"add,omitempty"`
	Remove  []string `json:"remove,omitempty"`
	Inherit []string `json:"inherit,omitempty"`
}
