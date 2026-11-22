#!/usr/bin/env bash
# Test for close_spec_heads.sh with a fake `gh` (no network): bash scripts/ci/close_spec_heads_test.sh
set -euo pipefail
here="$(cd "$(dirname "$0")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin"
cat >"$tmp/bin/gh" <<'GH'
#!/usr/bin/env bash
# fake gh: state in $FAKE (open.json, states/<n>), calls appended to $FAKE/calls
echo "$*" >>"$FAKE/calls"
case "$1 $2" in
"issue list") cat "$FAKE/open.json" ;;
"issue view") cat "$FAKE/states/$3" 2>/dev/null || exit 1 ;;
"issue edit") cp "$5" "$FAKE/edited-$3" ;;
"issue comment") cp "$5" "$FAKE/comment-$3" ;;
"issue close") : ;;
*) exit 9 ;;
esac
GH
chmod +x "$tmp/bin/gh"
export FAKE="$tmp" PATH="$tmp/bin:$PATH"
mkdir "$tmp/states"
fail() { echo "FAIL: $*" >&2; exit 1; }

# 10: last ticket closing -> ticked, commented, closed. 11: one ticket open -> ticked only.
# 12: no ticket lines -> untouched. 13: line already ticked (bundle) + all closed -> closed. 14: #1 vs #10 prefix.
printf CLOSED >"$tmp/states/1"; printf CLOSED >"$tmp/states/2"; printf OPEN >"$tmp/states/3"; printf CLOSED >"$tmp/states/4"
jq -n '[
 {number:10, body:"Spec\n\n- [x] #1 first\n- [ ] #2 second\r\n"},
 {number:11, body:"- [ ] #2 second\n- [ ] #3 third\n"},
 {number:12, body:"mentions #2 but has no list\n"},
 {number:13, body:"- [x] #2 done\n- [x] #4 done\n"},
 {number:14, body:"- [ ] #20 other\n- [ ] #2\n"}
]' >"$tmp/open.json"
printf CLOSED >"$tmp/states/20"

bash "$here/scripts/ci/close_spec_heads.sh" 2 >"$tmp/out"

grep -q '^- \[x\] #2 second$' "$tmp/edited-10" || fail "10 not ticked"
grep -q '^- \[x\] #1 first$' "$tmp/edited-10" || fail "10 lost #1"
grep -q 'close' <(grep '^#10' "$tmp/out") || fail "10 not closed"
grep -q '#1 #2' "$tmp/comment-10" || fail "10 comment lacks tickets"
grep -q '^- \[x\] #2 second$' "$tmp/edited-11" || fail "11 not ticked"
grep -q '^- \[ \] #3 third$' "$tmp/edited-11" || fail "11 #3 changed"
! grep -q 'issue close 11' "$tmp/calls" || fail "11 closed early"
[[ ! -e "$tmp/edited-12" ]] && ! grep -q 'issue [a-z]* 12' "$tmp/calls" || fail "12 touched"
[[ ! -e "$tmp/edited-13" ]] || fail "13 edited though already ticked"
grep -q 'issue close 13' "$tmp/calls" || fail "13 not closed"
grep -q '^- \[ \] #20 other$' "$tmp/edited-14" || fail "14 #20 ticked by #2"
grep -q '^- \[x\] #2$' "$tmp/edited-14" || fail "14 #2 not ticked"
! grep -q 'issue close 14' "$tmp/calls" || fail "14 closed with #20 unchecked"

# dry run: no writes
: >"$tmp/calls"; rm -f "$tmp"/edited-* "$tmp"/comment-*
DRY_RUN=1 bash "$here/scripts/ci/close_spec_heads.sh" 2 >/dev/null
! grep -Eq 'issue (edit|comment|close)' "$tmp/calls" || fail "dry run wrote"
echo "ok"
