package hosting

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/sandcorporation/switchboard/internal/agent"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Remaining limit per agent account (#435). An account is a node, an
// adapter and the adapter's account name ("" its default account). Its
// remaining percentage is, in this order: 0 while the operator's switch is
// on (host limit on); the operator's value for the account; the value the
// adapter measured, until the window it is for refills (resets_at); 0 while
// the node's screens show the usage-limit message and the node reports no
// usage of its own (a node before #434); else not known. Not known takes
// work: a group's threshold holds back only a known value below it.

// Account limit sources, after LimitOperator.
const (
	AccountManual  = "manual"
	AccountAdapter = "adapter"
)

type accountKey struct{ node, adapter, account string }

func (k accountKey) less(o accountKey) int {
	return strings.Compare(k.node+"\x00"+k.adapter+"\x00"+k.account, o.node+"\x00"+o.adapter+"\x00"+o.account)
}

// accountState is every account the host knows of, sorted, and the
// adapter a record without one launches with, by node.
func (s *Service) accountState() ([]wire.AccountLimit, map[string]string, error) {
	now := s.clock().UnixMilli()
	var op, cleared int64
	err := s.db.QueryRow(`SELECT operator,cleared_at FROM hosting_limit WHERE id=1`).Scan(&op, &cleared)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	byKey := map[accountKey]*wire.AccountLimit{}
	get := func(k accountKey) *wire.AccountLimit {
		a := byKey[k]
		if a == nil {
			a = &wire.AccountLimit{Node: k.node, Adapter: k.adapter, Account: k.account}
			byKey[k] = a
		}
		return a
	}
	defaults := map[string]string{}
	screen := map[string]*int64{}
	screenOn := map[string]bool{}
	rows, err := s.db.Query(`SELECT name,default_adapter,agent_usage,limit_since,limit_reset FROM nodes WHERE revoked_at IS NULL`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var name, def, usage string
		var since, reset sql.NullInt64
		if err := rows.Scan(&name, &def, &usage, &since, &reset); err != nil {
			rows.Close()
			return nil, nil, err
		}
		defaults[name] = def
		var list []wire.AgentUsage
		if usage != "" {
			_ = json.Unmarshal([]byte(usage), &list)
		}
		for _, u := range list {
			a := get(accountKey{name, u.Adapter, u.Account})
			if u.Remaining != nil && (u.ResetsAt == nil || *u.ResetsAt > now) {
				a.Remaining, a.Source, a.ResetsAt = u.Remaining, AccountAdapter, u.ResetsAt
			}
		}
		if len(list) == 0 && since.Valid && since.Int64 > cleared && (!reset.Valid || reset.Int64 > now) {
			screenOn[name] = true
			if reset.Valid {
				screen[name] = &reset.Int64
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = s.db.Query(`SELECT node,adapter,agent_account FROM hosted_sessions`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var k accountKey
		if err := rows.Scan(&k.node, &k.adapter, &k.account); err != nil {
			rows.Close()
			return nil, nil, err
		}
		k.adapter = effectiveAdapter(k.node, k.adapter, defaults)
		get(k).Sessions++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = s.db.Query(`SELECT node,adapter,account,manual,cleared_at FROM account_limits WHERE manual IS NOT NULL OR cleared_at>0`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var k accountKey
		var manual sql.NullInt64
		var at int64
		if err := rows.Scan(&k.node, &k.adapter, &k.account, &manual, &at); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if manual.Valid {
			v := int(manual.Int64)
			get(k).Manual = &v
		} else if byKey[k] == nil {
			continue
		}
		if at > 0 {
			get(k).ClearedAt = &at
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	keys := make([]accountKey, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, accountKey.less)
	out := make([]wire.AccountLimit, 0, len(keys))
	for _, k := range keys {
		a := byKey[k]
		switch {
		case op != 0:
			zero := 0
			a.Remaining, a.Source, a.ResetsAt = &zero, LimitOperator, nil
		case a.Manual != nil:
			v := *a.Manual
			a.Remaining, a.Source, a.ResetsAt = &v, AccountManual, nil
		case a.Source == AccountAdapter:
		case screenOn[k.node]:
			zero := 0
			a.Remaining, a.Source, a.ResetsAt = &zero, LimitScreen, screen[k.node]
		}
		out = append(out, *a)
	}
	return out, defaults, nil
}

// Accounts is every agent account the host knows of with its remaining
// limit, by node, adapter and account.
func (s *Service) Accounts() ([]wire.AccountLimit, error) {
	out, _, err := s.accountState()
	return out, err
}

// SetAccountLimit stores the operator's remaining value for one account;
// Remaining nil clears it.
func (s *Service) SetAccountLimit(c wire.AccountLimitChange) (wire.HostingLimit, error) {
	if c.Node == "" || c.Adapter == "" {
		return wire.HostingLimit{}, invalidf("node and adapter are required")
	}
	if !adapterPattern.MatchString(c.Adapter) {
		return wire.HostingLimit{}, invalidf("adapter: invalid name %q", c.Adapter)
	}
	if c.Remaining != nil && (*c.Remaining < 0 || *c.Remaining > 100) {
		return wire.HostingLimit{}, invalidf("remaining: want 0 to 100, not %d", *c.Remaining)
	}
	_, err := s.db.Exec(`INSERT INTO account_limits(node,adapter,account,manual,set_at) VALUES(?,?,?,?,?)
		ON CONFLICT(node,adapter,account) DO UPDATE SET manual=excluded.manual,set_at=excluded.set_at`,
		c.Node, c.Adapter, c.Account, nullInt(c.Remaining), s.clock().UnixMilli())
	if err != nil {
		return wire.HostingLimit{}, err
	}
	return s.Limit()
}

// markAccountCleared records when an account's limit last cleared.
func (s *Service) markAccountCleared(k accountKey, at int64) error {
	_, err := s.db.Exec(`INSERT INTO account_limits(node,adapter,account,cleared_at) VALUES(?,?,?,?)
		ON CONFLICT(node,adapter,account) DO UPDATE SET cleared_at=excluded.cleared_at`, k.node, k.adapter, k.account, at)
	return err
}

// setSessionAccount stores the agent account the node reported a record's
// session uses.
func (s *Service) setSessionAccount(id, account string) error {
	_, err := s.db.Exec(`UPDATE hosted_sessions SET agent_account=? WHERE id=? AND agent_account<>?`, account, id, account)
	return err
}

// effectiveAdapter is the adapter a record launches with: its own, else
// the node's default (defaults, by node), else the host's. It is
// nodeDefaultAdapter without a query, since callers hold rows open and
// the database may have one connection.
func effectiveAdapter(node, adapter string, defaults map[string]string) string {
	switch {
	case adapter != "":
		return adapter
	case defaults[node] != "":
		return defaults[node]
	}
	return agent.Default().Name()
}

// accountOf is the account of a record.
func accountOf(h wire.HostedSession, defaults map[string]string) accountKey {
	return accountKey{h.Node, effectiveAdapter(h.Node, h.Adapter, defaults), h.Account}
}
