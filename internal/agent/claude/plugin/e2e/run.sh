#!/usr/bin/env bash
# 질문 경로 e2e(#157): 같은 커밋의 switchboard 서버를 빌드해(#477) 플러그인 코어를 그 서버에 붙여 돌린다.
# 시험이 MIN 개보다 적게 통과하면(이름을 바꿔 조용히 빠짐 등) 실패다.
#   bash internal/agent/claude/plugin/e2e/run.sh
# 빌드에는 PATH 의 go 를 쓴다. 이미 빌드한 바이너리를 쓰려면 SWITCHBOARD_E2E_BIN.
set -euo pipefail

TSX=tsx@4.23.15
MIN=16

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../../../.." && pwd)

bin=${SWITCHBOARD_E2E_BIN:-}
if [ -z "$bin" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "question source of truth: go 가 없다. 이 커밋에서 빌드한 서버를 SWITCHBOARD_E2E_BIN 으로 준다" >&2
    exit 1
  fi
  out=$(mktemp -d)
  bin=$out/switchboard
  (cd "$root" && go build -o "$bin" ./cmd/switchboard)
fi
"$bin" version

tap=$(mktemp)
status=0
SWITCHBOARD_E2E_BIN=$bin npx -y "$TSX" --test --test-force-exit \
  --test-reporter=spec --test-reporter-destination=stdout \
  --test-reporter=tap --test-reporter-destination="$tap" \
  "$here"/*.e2e.ts || status=$?

pass=$(sed -n 's/^# pass \([0-9]*\)$/\1/p' "$tap" | tail -n 1)
skipped=$(sed -n 's/^# skipped \([0-9]*\)$/\1/p' "$tap" | tail -n 1)
echo "e2e: pass ${pass:-0}, skipped ${skipped:-0} (최소 $MIN)"
if [ "$status" -ne 0 ]; then exit "$status"; fi
if [ "${pass:-0}" -lt "$MIN" ]; then
  echo "question source of truth: e2e 가 ${pass:-0}개만 통과했다(최소 $MIN개)" >&2
  exit 1
fi
