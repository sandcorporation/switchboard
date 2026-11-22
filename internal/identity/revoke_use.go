package identity

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// RecentUse is how far back a role's last request or heartbeat makes a
// revoke ask for a confirm.
const RecentUse = 24 * time.Hour

// LastUse is the latest register or heartbeat of any slot of a role.
type LastUse struct {
	At      time.Time `json:"at"`
	Machine string    `json:"machine"`
}

// LastUseOf reads the latest slot report of role from the slots table. ok is
// false when no slot of the role ever reported.
func LastUseOf(q Queryer, role string) (u LastUse, ok bool) {
	var at int64
	err := q.QueryRow(`SELECT reported_at,machine FROM slots WHERE base=? AND reported_at>0 ORDER BY reported_at DESC LIMIT 1`, role).Scan(&at, &u.Machine)
	if err != nil {
		return LastUse{}, false
	}
	u.At = time.UnixMilli(at).UTC()
	return u, true
}

// recentUse is the last use of role when it falls within RecentUse of now.
func (s *Service) recentUse(role string) (LastUse, bool) {
	u, ok := LastUseOf(s.db, role)
	if !ok || s.now().Sub(u.At) > RecentUse {
		return LastUse{}, false
	}
	return u, true
}

// inUseRefusal is the 409 body of a revoke that needs force=1.
type inUseRefusal struct {
	Error      string `json:"error"`
	LastUsedAt string `json:"last_used_at"`
	Machine    string `json:"machine"`
}

// refuseInUse answers a revoke of a role used within RecentUse, unless
// force=1 or the role is already revoked. It reports whether it answered.
func (s *Service) refuseInUse(w http.ResponseWriter, r *http.Request, role string) bool {
	if r.URL.Query().Get("force") == "1" {
		return false
	}
	if _, revoked := RevokedAt(s.db, role); revoked {
		return false
	}
	u, ok := s.recentUse(role)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(inUseRefusal{Error: "role used in the last 24h; repeat with force=1 to revoke", LastUsedAt: u.At.Format(time.RFC3339), Machine: u.Machine})
	return true
}

// RevokedBody is the 401 body for a request that carries a valid token of a
// revoked role: the revoke time and nothing else. A wrong token gets ok=false.
func (s *Service) RevokedBody(t, h string) (string, bool) {
	if t == "" || h == "" {
		return "", false
	}
	h = Resolve(s.db, h)
	roles := []string{h}
	if base, _, ok := wire.SplitSlot(h); ok {
		roles = append(roles, base)
	}
	for _, role := range roles {
		hash, revoked, ok := roleRecord(s.db, role)
		if !ok || !revoked {
			continue
		}
		if !tokenMatches(hash, t) && !credentialEverMatched(s.db, role, t) {
			continue
		}
		if at, ok := RevokedAt(s.db, role); ok {
			return "role revoked at " + at.UTC().Format(time.RFC3339), true
		}
	}
	return "", false
}

func credentialEverMatched(q Queryer, role, t string) bool {
	var one int
	return q.QueryRow(`SELECT 1 FROM role_credentials WHERE token_hash=? AND role=?`, store.TokenHash(t), role).Scan(&one) == nil
}
