package wire

import (
	"strings"
	"testing"
)

// Topics take letters of any script (#408 T1); tags stay lower-case ASCII.
func TestTopicAndTagNames(t *testing.T) {
	for _, ok := range []string{"release", "배포", "ci-요령", "v1.2_x", strings.Repeat("가", 40)} {
		if err := CheckTopicName(ok); err != nil {
			t.Errorf("topic %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", "배 포", "a/b", strings.Repeat("가", 41)} {
		if CheckTopicName(bad) == nil {
			t.Errorf("topic %q accepted", bad)
		}
	}
	if CheckRuleName("tag", "배포") == nil || CheckRuleName("tag", "ci-tips") != nil {
		t.Error("tags: lower-case ASCII only")
	}
}

// A rule text keeps its lines; the first is the summary.
func TestRuleRequestMultiLine(t *testing.T) {
	q := RuleRequest{Kind: RuleTip, Text: "  rerun once\n- then ask\n", Topic: "ci", Scope: RuleScope{Kind: ScopeAll}, SourceText: "x"}
	if err := q.Check(); err != nil || q.Text != "rerun once\n- then ask" {
		t.Fatalf("%v %q", err, q.Text)
	}
	q.Text = strings.Repeat("x", MaxRuleText+1)
	if q.Check() == nil {
		t.Fatal("too long accepted")
	}
}

// Conditions: checked when a row goes in, matched on a tool call.
func TestRuleMatch(t *testing.T) {
	if (RuleMatch{Command: "("}).Check() == nil || (RuleMatch{Tool: "a b"}).Check() == nil || (RuleMatch{Path: "a/***"}).Check() == nil {
		t.Fatal("bad conditions accepted")
	}
	for _, c := range []struct {
		m    RuleMatch
		call ToolCall
		want bool
	}{
		{RuleMatch{Tool: "mcp__*"}, ToolCall{Tool: "mcp__x__y"}, true},
		{RuleMatch{Tool: "Bash"}, ToolCall{Tool: "BashOutput"}, false},
		{RuleMatch{Path: "**"}, ToolCall{Path: "/a/b"}, true},
		{RuleMatch{Path: "*.go"}, ToolCall{Path: "a/b.go"}, false},
		{RuleMatch{Path: "a/**/*.go"}, ToolCall{Path: "a/b.go"}, true},
		{RuleMatch{Path: "a/?.go"}, ToolCall{Path: "a/bc.go"}, false},
		{RuleMatch{Command: "rm -rf"}, ToolCall{Tool: "Edit"}, false},
		{RuleMatch{}, ToolCall{Tool: "Bash"}, false},
	} {
		if got := c.m.Hits(c.call); got != c.want {
			t.Errorf("%+v on %+v: %v", c.m, c.call, got)
		}
	}
}
