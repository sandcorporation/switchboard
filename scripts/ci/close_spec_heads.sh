#!/usr/bin/env bash
# Ticks the checklist line of a just-closed issue in every open issue body, and closes an issue once every
# ticket line in its body is ticked and each of those tickets is closed (#419).
#   scripts/ci/close_spec_heads.sh <closed issue number>      (GH_REPO selects the repository; DRY_RUN=1 only prints)
# A ticket line is `- [ ] #N` or `- [x] #N` (any list marker, any indent). Bodies without such a line are never touched.
set -euo pipefail

n=${1:?usage: close_spec_heads.sh <issue number>}
[[ $n =~ ^[0-9]+$ ]] || { echo "not an issue number: $n" >&2; exit 2; }
dry=${DRY_RUN:-}
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# ticket lines: list marker, box, then an issue reference
line_re='^[[:space:]]*[-*+][[:space:]]+\[[ xX]\][[:space:]]+#[0-9]+'
todo_re="^([[:space:]]*[-*+][[:space:]]+)\\[ \\]([[:space:]]+#$n)([^0-9]|\$)"

gh issue list --state open --limit 1000 --json number,body >"$tmp/open.json"

jq -r --argjson n "$n" '.[] | select(.number != $n) | .number' "$tmp/open.json" | while read -r head; do
	jq -r --argjson h "$head" '.[] | select(.number == $h) | .body // ""' "$tmp/open.json" | tr -d '\r' >"$tmp/body"
	grep -Eq "^[[:space:]]*[-*+][[:space:]]+\\[[ xX]\\][[:space:]]+#$n([^0-9]|\$)" "$tmp/body" || continue

	# tick the line(s) for this ticket
	changed=
	: >"$tmp/new"
	while IFS= read -r l || [[ -n $l ]]; do
		if [[ $l =~ $todo_re ]]; then
			l="${BASH_REMATCH[1]}[x]${BASH_REMATCH[2]}${l:${#BASH_REMATCH[0]}-${#BASH_REMATCH[3]}}"
			changed=1
		fi
		printf '%s\n' "$l" >>"$tmp/new"
	done <"$tmp/body"

	if [[ -n $changed ]]; then
		echo "#$head: tick #$n"
		[[ -n $dry ]] || gh issue edit "$head" --body-file "$tmp/new" >/dev/null
	fi

	# close when every ticket line is ticked and every ticket is closed
	grep -E "$line_re" "$tmp/new" >"$tmp/lines" || true
	grep -Eq '\[ \]' "$tmp/lines" && { echo "#$head: unchecked lines remain"; continue; }
	tickets=$(grep -Eo '#[0-9]+' "$tmp/lines" | tr -d '#' | awk '!seen[$0]++')
	all_closed=1
	for t in $tickets; do
		[[ $t == "$n" ]] && continue
		st=$(gh issue view "$t" --json state -q .state 2>/dev/null || echo UNKNOWN)
		[[ $st == CLOSED ]] || { all_closed=; echo "#$head: ticket #$t is $st"; break; }
	done
	[[ -n $all_closed ]] || continue

	refs=$(printf '#%s ' $tickets)
	echo "#$head: close (${refs% })"
	[[ -n $dry ]] && continue
	printf 'All tickets are closed (%s); closing the spec head.\n' "${refs% }" >"$tmp/comment"
	gh issue comment "$head" --body-file "$tmp/comment" >/dev/null
	gh issue close "$head" --reason completed >/dev/null
done
