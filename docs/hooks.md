# Incoming webhooks

A tool that sends Discord- or Slack-style webhook notices (monitoring, error tracking, builds and deploys, business alerts) can send them to switchboard too. Each notice becomes alert mail to the role the webhook names, read as text; in forward mode switchboard also sends the notice on to the original destination (#527).

## Pick a mode

| Mode | Who sends to the original destination | Use it for |
|---|---|---|
| 메일만 (`mail`) | the tool itself; switchboard is a second destination | notices that must not be lost when switchboard is down, such as monitoring |
| 다시 보내기 (`forward`) | switchboard, after it has mailed the role | tools that take only one destination, and less urgent notices |

In `mail` mode the original destination is optional. When it is set, switchboard only uses it to keep that address out of mail.

## Register a webhook

1. Dashboard › 설정 › 웹훅 › `+` (a login with the `sessions` permission).
2. Fill in:
   - 이름: shown in the list and in mail (`웹훅 <name>`).
   - 받는 역할: the role that gets the mail, such as `team/boss`.
   - 꼴: 메일만 or 다시 보내기 (above).
   - 긴급: send as urgent mail. Off by default.
   - 묶는 시간(분): notices with the same subject inside this time become one mail with a count (×N). 10 by default.
   - 원래 목적지: the tool's original webhook address, such as `https://hooks.example/services/T0/B0/xyz`. Required for 다시 보내기.
3. 추가. The detail shows the receiving address once, such as `https://mail.example/hook/3q2…`. Copy it now: leaving the page hides it for good. 주소 다시 만들기 makes a new one and ends the old one.
4. In the tool:
   - 메일만: add the receiving address as a second webhook beside the original one.
   - 다시 보내기: replace the original webhook address with the receiving address.

After registering, the original destination is never shown again (dashboard, CLI or API): the field reads 있음 or 없음, with 바꾸기 and 없애기. 다시 등록 필요 means the server's secrets key changed (README, "Docker deployment"); enter the destination again.

## What arrives

- Body: Discord (`content`, `embeds` with title, description, fields, footer), Slack (`text`, `blocks`, `attachments`), or any other JSON object (its first eight top-level keys as `key: value` lines). Mail carries text only, never the raw JSON. The original destination, and any address that looks like a webhook, is hidden in mail.
- Subject: Discord's content or first embed title, Slack's header block, text or first attachment title; otherwise `웹훅 <name>`.
- Limits: 256 KiB per body, 60 notices per webhook per minute (then 429 with `Retry-After: 60`). An unknown or removed address answers 404. A body that is not JSON answers 400 or 415.
- Forwarding: the same body and content type go to the original destination. A 5xx, 429 or connection failure is tried again after 30 s, 2 min, 10 min and 30 min (kept in the database across restarts). When the last try fails, or the destination answers another 4xx, the role gets a mail `원래 목적지 전송 실패: <subject>` with the status code; the address is never in it.
- 설정 › 웹훅 › a webhook shows its recent notices: time, subject and forwarding result.

## Commands

`switchboard hooks list [--json]` lists each webhook's name, receiving role, mode and last notice, for any role. Adding and changing webhooks is the dashboard's only.
