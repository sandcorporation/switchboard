package watch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// JSON is a decoded response body: map[string]JSON, []JSON, string,
// json.Number, bool or nil.
type JSON = any

// decode reads a body as JSON with numbers kept as json.Number.
func decode(b []byte) (JSON, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v JSON
	if err := d.Decode(&v); err != nil {
		return nil, errNotJSON
	}
	if d.More() {
		return nil, errNotJSON
	}
	return v, nil
}

var errNotJSON = errors.New("JSON 아님")

// lookup follows a dot path: a name picks an object's field, a number an
// array's element. The empty path is v itself.
func lookup(v JSON, path string) (JSON, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return v, true
	}
	for _, part := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]JSON:
			n, ok := x[part]
			if !ok {
				return nil, false
			}
			v = n
		case []JSON:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// text is a value's form in comparisons, mail and templates: a string as
// it is, a number as written, true/false, null, and an object or array as
// compact JSON cut at 200 characters.
func text(v JSON) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return cut(string(b), 200)
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// number reads v (a JSON number, or a string holding one) as an exact
// rational, so 10 and 10.0 compare equal and big values keep their digits.
func number(v JSON) (*big.Rat, bool) {
	var s string
	switch x := v.(type) {
	case json.Number:
		s = x.String()
	case string:
		s = strings.TrimSpace(x)
	default:
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok && s != ""
}

// holds evaluates a value condition on the body: whether it holds and the
// value it looked at ("" when the path is absent).
func holds(c wire.WatchCond, body JSON) (bool, string) {
	v, found := lookup(body, c.Path)
	shown := ""
	if found {
		shown = text(v)
	}
	switch c.Op {
	case "exists":
		return found && v != nil, shown
	case "missing":
		return !found || v == nil, shown
	}
	if !found {
		return false, shown
	}
	switch c.Op {
	case "eq", "ne":
		eq := text(v) == c.Value
		if a, ok := number(v); ok {
			if b, ok := number(c.Value); ok {
				eq = a.Cmp(b) == 0
			}
		}
		return eq == (c.Op == "eq"), shown
	case "gt", "lt":
		a, ok1 := number(v)
		b, ok2 := number(c.Value)
		if !ok1 || !ok2 {
			return false, shown
		}
		if c.Op == "gt" {
			return a.Cmp(b) > 0, shown
		}
		return a.Cmp(b) < 0, shown
	case "contains":
		switch x := v.(type) {
		case string:
			return strings.Contains(x, c.Value), shown
		case []JSON:
			for _, e := range x {
				if text(e) == c.Value {
					return true, shown
				}
			}
			return false, shown
		case map[string]JSON:
			_, ok := x[c.Value]
			return ok, shown
		}
		return strings.Contains(text(v), c.Value), shown
	}
	return false, shown
}

// opWords are the operators as a mail and the list say them.
var opWords = map[string]string{"eq": "=", "ne": "≠", "gt": ">", "lt": "<", "exists": "있음", "missing": "없음", "contains": "포함"}

// describe is the condition in one line.
func describe(c wire.WatchCond) string {
	if c.Kind == wire.WatchNew {
		return fmt.Sprintf("새 항목: %s (키 %s)", orRoot(c.List), c.Key)
	}
	switch c.Op {
	case "exists", "missing":
		return orRoot(c.Path) + " " + opWords[c.Op]
	}
	return fmt.Sprintf("%s %s %s", orRoot(c.Path), opWords[c.Op], c.Value)
}

func orRoot(p string) string {
	if strings.TrimSpace(p) == "" {
		return "(본문)"
	}
	return p
}

// items is a new-item rule's list and each item's key; items without a key
// are skipped.
type item struct {
	key string
	v   JSON
}

func listItems(c wire.WatchCond, body JSON) ([]item, error) {
	v, ok := lookup(body, c.List)
	if !ok {
		return nil, fmt.Errorf("목록 경로 %s 없음", orRoot(c.List))
	}
	arr, ok := v.([]JSON)
	if !ok {
		return nil, fmt.Errorf("목록 경로 %s가 배열이 아님", orRoot(c.List))
	}
	out := make([]item, 0, len(arr))
	seen := map[string]bool{}
	for _, e := range arr {
		k, ok := lookup(e, c.Key)
		if !ok || k == nil {
			continue
		}
		key := cut(text(k), 200)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item{key: key, v: e})
	}
	return out, nil
}

// render fills a subject template: {path} takes the text of that path in
// v, "?" when absent. A template without braces is used as it is.
func render(tpl string, v JSON) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(tpl, '{')
		if i < 0 {
			b.WriteString(tpl)
			break
		}
		j := strings.IndexByte(tpl[i:], '}')
		if j < 0 {
			b.WriteString(tpl)
			break
		}
		b.WriteString(tpl[:i])
		if x, ok := lookup(v, tpl[i+1:i+j]); ok {
			b.WriteString(cut(text(x), 100))
		} else {
			b.WriteString("?")
		}
		tpl = tpl[i+j+1:]
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// fields are up to five of an item's scalar fields for a mail body (never
// the whole item).
func fields(v JSON) []string {
	m, ok := v.(map[string]JSON)
	if !ok {
		return []string{cut(text(v), 200)}
	}
	keys := make([]string, 0, len(m))
	for k, x := range m {
		switch x.(type) {
		case map[string]JSON, []JSON:
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+": "+cut(text(m[k]), 200))
	}
	return out
}
