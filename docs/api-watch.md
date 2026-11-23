# API watches

The server GETs an address on a period and mails a role when a rule says so (`internal/watch`, #528). A rule is data: an address, headers, a period, one condition and the mail settings. No script runs and no model reads the response.

## Register a rule

On the dashboard: 설정 › 지켜보기 › `+`.

1. 이름: a name for the rule, unique (it is also the mail's source, `watch:<name>`).
2. 받는 역할: the role that gets the mail, for example `team/boss`.
3. 주소: the address to GET, `https://api.example/v1/orders`. Do not put a key in the address; put it in a header. An address with a user and password is refused.
4. 머리글 › `+ 머리글`: one row per header. For the API key, write the header name (`Authorization`, `X-Api-Key` …), the value, and tick 비밀. A secret value is sealed in the server's secret store and never shown again: the rule then shows only that it is set, (`등록됨`) and, on hover, when. If the server's secrets key changes, the field reads `다시 적어야 함` and the rule fails until the value is written again. To replace it, write a new value; leaving it empty keeps the stored one.
5. 주기(분): how often to fetch, 1 minute at least.
6. 조건: 값 조건 or 새 항목 (below).
7. 제목 틀 (optional): the mail subject, with `{path}` for a value. Empty uses `[지켜보기] <name>` (a new item: `[지켜보기] <name> 새 항목 <key>`).
8. 긴급: send the mail as urgent. Default off.
9. 묶는 시간(분): the same rule's mail with the same subject within this many minutes is counted on the first mail (`×3 · 마지막 14:05` after its subject) instead of sent again; an urgent one rings once. Default 10.
10. 실패 알림(번): how many failed fetches in a row tell the role. Default 3.
11. 시험 조회: fetches once as written and shows the HTTP status, whether the condition holds (with the value it read) or how many items and new items there are, and the subject the mail would have. It sends no mail and stores nothing.
12. 추가 (저장 for an existing rule).

The list shows each rule's role, period, last fetch and its result. A rule's detail has 끄기/켜기 and 지우기 (which also removes its stored key). Changing the address or the condition starts the rule over: the value state and the seen items are cleared.

From a session, `switchboard watch list [--json]` shows the rules that mail its role (every rule for a global boss): name, receiving role, period and last result. Registering is the dashboard's only.

## Paths

A path is dot separated; a number picks an array element; an empty path is the whole response.

```json
{"data": {"count": 7}, "items": [{"status": "open", "id": 41}]}
```

`data.count` is `7`, `items.0.status` is `open`.

## Value condition

A path, a comparison and a value: 같다 (`eq`), 다르다 (`ne`), 크다 (`gt`), 작다 (`lt`), 있다 (`exists`), 없다 (`missing`), 포함한다 (`contains`).

- `eq`/`ne` compare numbers as numbers (`10` equals `10.0`) and anything else as text.
- `gt`/`lt` need a number on both sides; otherwise the condition does not hold.
- `contains`: a substring of a text, an element of an array, a field name of an object.
- `exists`: the path is there and not `null`; `missing` the opposite.

The mail goes once when the condition starts to hold. While it keeps holding, nothing more is sent; after it stops holding and holds again, one more mail goes.

Example: tell `team/boss` when the queue of `https://api.example/v1/queue` grows past 100.

| Field | Value |
|---|---|
| 주소 | `https://api.example/v1/queue` |
| 조건 | 값 조건 |
| 경로 | `data.waiting` |
| 비교 | 크다 |
| 값 | `100` |
| 제목 틀 | `대기열 {data.waiting}건` |

## New item condition

The path of a list (an array) and the path, inside one item, of the key that tells items apart.

- The first fetch takes every item in the list as already seen and sends nothing.
- After that each key not seen before is one mail (up to 10 in one fetch; the rest in one more mail listing their keys).
- The seen keys are stored, so a restart does not tell the same item again. The newest 5,000 keys per rule are kept.

Example: tell `team/boss` of each new order at `https://api.example/v1/orders`, which answers `{"results": [{"id": 41, "title": "…"}, …]}`.

| Field | Value |
|---|---|
| 주소 | `https://api.example/v1/orders` |
| 조건 | 새 항목 |
| 목록 경로 | `results` |
| 키 경로 | `id` |
| 제목 틀 | `새 주문 {title}` |

In a new item's mail, `{path}` in the subject reads from the item. The body has the rule, the address without its query, the condition, the key and up to five of the item's plain fields; never the whole response.

## Failures

A fetch fails on a network error, a timeout (10 seconds), a status outside 2xx (a redirect too: headers go only to the written address), a response over 1 MB, a body that is not JSON, or a list path that is not an array. After 실패 알림 failures in a row the role gets one mail with the last error; the first fetch that works again sends one 「다시 됨」 mail. Errors carry neither the address's query string nor a header value.

The watcher looks for due rules every 15 seconds, apart from the server's sweep, and runs at most 4 fetches at once.
