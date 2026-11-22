package hosting

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"text/tabwriter"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Work groups (#435): the operator names groups and gives each a
// remaining-limit threshold; a hosted session belongs to one group or none.
// They replaced the four session classes of #190.

var (
	// ErrNoGroup means no work group has the name.
	ErrNoGroup = errors.New("no such work group")
	// ErrGroupExists refuses a second group of a name.
	ErrGroupExists = errors.New("a work group of this name already exists")
	// ErrGroupInUse refuses removing a group a hosted session is in.
	ErrGroupInUse = errors.New("hosted sessions are in this work group; move them out first")
)

var groupPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func checkThreshold(t *int) error {
	if t != nil && (*t < 0 || *t > 100) {
		return invalidf("threshold: want 0 to 100, not %d", *t)
	}
	return nil
}

// checkGroup refuses a group name that names no work group; "" is none.
func (s *Service) checkGroup(name string) error {
	if name == "" {
		return nil
	}
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM work_groups WHERE name=?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidf("group: no work group %q", name)
	}
	return err
}

// thresholds maps each work group to its threshold (nil: none). A record's
// group missing from it (none, "") has no threshold.
func (s *Service) thresholds() (map[string]*int, error) {
	rows, err := s.db.Query(`SELECT name,threshold FROM work_groups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*int{}
	for rows.Next() {
		var name string
		var t sql.NullInt64
		if err := rows.Scan(&name, &t); err != nil {
			return nil, err
		}
		if t.Valid {
			v := int(t.Int64)
			out[name] = &v
		} else {
			out[name] = nil
		}
	}
	return out, rows.Err()
}

func scanGroup(r rowScanner) (wire.WorkGroup, error) {
	var g wire.WorkGroup
	var t sql.NullInt64
	if err := r.Scan(&g.Name, &t, &g.CreatedAt, &g.UpdatedAt, &g.Sessions); err != nil {
		return g, err
	}
	if t.Valid {
		v := int(t.Int64)
		g.Threshold = &v
	}
	return g, nil
}

const groupSelect = `SELECT g.name,g.threshold,g.created_at,g.updated_at,(SELECT COUNT(*) FROM hosted_sessions h WHERE h.work_group=g.name) FROM work_groups g`

// Groups lists the work groups by name.
func (s *Service) Groups() ([]wire.WorkGroup, error) {
	rows, err := s.db.Query(groupSelect + ` ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.WorkGroup{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Group is one work group, or ErrNoGroup.
func (s *Service) Group(name string) (wire.WorkGroup, error) {
	g, err := scanGroup(s.db.QueryRow(groupSelect+` WHERE g.name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNoGroup
	}
	return g, err
}

// AddGroup stores a new work group.
func (s *Service) AddGroup(g wire.WorkGroup) (wire.WorkGroup, error) {
	if !groupPattern.MatchString(g.Name) {
		return g, invalidf("name: invalid work group name %q (1-64 of A-Z a-z 0-9 _ . -, not starting with _ . -)", g.Name)
	}
	if err := checkThreshold(g.Threshold); err != nil {
		return g, err
	}
	now := s.clock().UnixMilli()
	res, err := s.db.Exec(`INSERT INTO work_groups(name,threshold,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(name) DO NOTHING`, g.Name, nullInt(g.Threshold), now, now)
	if err != nil {
		return g, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return g, ErrGroupExists
	}
	return s.Group(g.Name)
}

// SetGroupThreshold changes a group's threshold; every session of the group
// follows it.
func (s *Service) SetGroupThreshold(name string, t *int) (wire.WorkGroup, error) {
	if err := checkThreshold(t); err != nil {
		return wire.WorkGroup{}, err
	}
	res, err := s.db.Exec(`UPDATE work_groups SET threshold=?,updated_at=? WHERE name=?`, nullInt(t), s.clock().UnixMilli(), name)
	if err != nil {
		return wire.WorkGroup{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return wire.WorkGroup{}, ErrNoGroup
	}
	return s.Group(name)
}

// RemoveGroup removes a group no session is in, and drops it from the
// grants that list it.
func (s *Service) RemoveGroup(name string) (wire.WorkGroup, error) {
	g, err := s.Group(name)
	if err != nil {
		return g, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return g, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM work_groups WHERE name=? AND NOT EXISTS (SELECT 1 FROM hosted_sessions WHERE work_group=?)`, name, name)
	if err != nil {
		return g, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return g, ErrGroupInUse
	}
	rows, err := tx.Query(`SELECT role,groups FROM host_grants WHERE groups<>''`)
	if err != nil {
		return g, err
	}
	type grantGroups struct{ role, groups string }
	var changes []grantGroups
	for rows.Next() {
		var role, raw string
		if err := rows.Scan(&role, &raw); err != nil {
			rows.Close()
			return g, err
		}
		list := decodeList(raw)
		kept := list[:0:0]
		for _, n := range list {
			if n != name {
				kept = append(kept, n)
			}
		}
		if len(kept) != len(list) {
			changes = append(changes, grantGroups{role, encodeList(kept)})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}
	for _, c := range changes {
		if _, err := tx.Exec(`UPDATE host_grants SET groups=? WHERE role=?`, c.groups, c.role); err != nil {
			return g, err
		}
	}
	return g, tx.Commit()
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// encodeList is the JSON of a list column, "" for an empty list.
func encodeList(list []string) string {
	if len(list) == 0 {
		return ""
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func dash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// groupsAPI serves /api/admin/terminals/work-groups (rest "") and
// /api/admin/terminals/work-groups/{name}: GET the list, POST a group, PUT its
// threshold, DELETE it (409 while a session is in it). A change of the
// threshold is published, since every session of the group may change
// whether it accepts work.
func (h *Hub) groupsAPI(w http.ResponseWriter, r *http.Request, name string) {
	var g wire.WorkGroup
	var err error
	code := http.StatusOK
	switch {
	case name == "" && r.Method == http.MethodGet:
		list, err := h.svc.Groups()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	case name == "" && r.Method == http.MethodPost:
		in, ok := decode[wire.WorkGroup](w, r)
		if !ok {
			return
		}
		g, err = h.svc.AddGroup(wire.WorkGroup{Name: in.Name, Threshold: in.Threshold})
		code = http.StatusCreated
	case name != "" && r.Method == http.MethodGet:
		g, err = h.svc.Group(name)
	case name != "" && r.Method == http.MethodPut:
		in, ok := decode[wire.WorkGroupChange](w, r)
		if !ok {
			return
		}
		g, err = h.svc.SetGroupThreshold(name, in.Threshold)
	case name != "" && r.Method == http.MethodDelete:
		g, err = h.svc.RemoveGroup(name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var invalid InvalidError
	switch {
	case err == nil:
		if r.Method != http.MethodGet {
			h.publish("terminals")
		}
		writeJSON(w, code, g)
	case errors.As(err, &invalid):
		http.Error(w, invalid.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNoGroup):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrGroupExists), errors.Is(err, ErrGroupInUse):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, "update failed", http.StatusInternalServerError)
	}
}

// RunGroupCLI serves `host group add|set|rm NAME` and `host group list`
// against the service's database. clear drops the threshold (set
// --threshold none).
func (s *Service) RunGroupCLI(action, name string, threshold *int, jsonOut bool, stdout io.Writer) error {
	var g wire.WorkGroup
	var err error
	switch action {
	case "list":
		list, err := s.Groups()
		if err != nil {
			return err
		}
		if jsonOut {
			return json.NewEncoder(stdout).Encode(list)
		}
		printGroups(stdout, list)
		return nil
	case "add":
		g, err = s.AddGroup(wire.WorkGroup{Name: name, Threshold: threshold})
	case "set":
		g, err = s.SetGroupThreshold(name, threshold)
	case "rm":
		g, err = s.RemoveGroup(name)
	default:
		return fmt.Errorf("host group: unknown action %q", action)
	}
	switch {
	case err != nil:
		return err
	case jsonOut:
		return json.NewEncoder(stdout).Encode(g)
	case action == "rm":
		_, err = fmt.Fprintf(stdout, "removed work group %s\n", g.Name)
		return err
	}
	printGroups(stdout, []wire.WorkGroup{g})
	return nil
}

// ThresholdText is a group's threshold as the CLI shows it.
func ThresholdText(t *int) string {
	if t == nil {
		return "-"
	}
	return fmt.Sprintf("%d%%", *t)
}

func printGroups(w io.Writer, list []wire.WorkGroup) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no work groups")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTHRESHOLD\tSESSIONS")
	for _, g := range list {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", g.Name, ThresholdText(g.Threshold), g.Sessions)
	}
	tw.Flush()
}
