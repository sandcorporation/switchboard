# A server instance as the hosting host

The plain server (`switchboard`, container or service) is a hosting host when `SWITCHBOARD_HOSTING=on`. It keeps mail, roles and the dashboard, runs no node, and serves the terminal view; sessions run on nodes attached over a second listener. A hosting instance for one PC (`switchboard up`): [hosting-pc.md](hosting-pc.md).

Values used below:

| Value | Example |
| --- | --- |
| Dashboard origin | `https://mail.example` |
| Proxy to server | `http://127.0.0.1:8097` |
| Remote listener (published) | `100.64.0.10:8091` <!-- leak-guard:allow --> |
| Admin | `alice` |
| Roles | `team/boss`, `team/worker` |
| Node | `pc2`, directory root `/home/user/work` |

## Settings

| Variable | Values | Effect |
| --- | --- | --- |
| `SWITCHBOARD_HOSTING` | `on` | Terminal hosting. Any other value or unset: off. |
| `SWITCHBOARD_HOSTING_TERMINAL` | `loopback` (default), `on`, `off` | Browser terminal view, ticket and socket. `loopback`: a loopback peer without forwarding headers, elsewhere `404`. `on`: any listener and address, for a signed-in admin. `off`: `404` everywhere. |
| `SWITCHBOARD_HOSTING_TERMINAL_ALLOW` | CIDR list, unset | A client address outside the ranges gets `403`. Unset: no limit. |
| `SWITCHBOARD_HOSTING_REMOTE_LISTEN` | `IP:port`, `0.0.0.0:port`, `[::]:port`, unset | Second listener for nodes and the role API. Unset: none. A host name or loopback is refused. |
| `SWITCHBOARD_HOSTING_REMOTE_ALLOW` | CIDR list, unset | Peers of the remote listener (and the address a node's `--host` may name). Unset: no limit; the node and role tokens are the gate. |
| `SWITCHBOARD_TRUST_FORWARDED` | `1` | The client address is the one the nearest proxy forwarded (`X-Forwarded-For`, last entry). Set it only behind a proxy that sets that header. It decides `…_TERMINAL=loopback` and `…_TERMINAL_ALLOW`, the admin login rate limit and the 기록 address. |
| `SWITCHBOARD_ADMIN_AUTH` / `SWITCHBOARD_ADMIN_PROXY_TOKEN` | `builtin`, `oidc` / secret | Admin sign-in. `…_TERMINAL=on` does not start without one of them. |
| `SWITCHBOARD_PUBLIC_URL` | `https://mail.example` | Allowed `Origin` of the terminal socket besides the request's own host. |

`…_TERMINAL=loopback` and `off` start without admin sign-in. The remote listener answers `/api/node/*` and the role API only; the admin API, dashboard, setup, sign-in and the terminal are `404` there, and a request with `X-Forwarded-*` or `Forwarded` headers is `403`.

Start refusals:

| Message | Fix |
| --- | --- |
| `SWITCHBOARD_HOSTING=on refused: SWITCHBOARD_HOSTING_TERMINAL must be loopback, on or off (got "…")` | Fix the value. |
| `SWITCHBOARD_HOSTING=on refused: SWITCHBOARD_HOSTING_TERMINAL=on needs admin sign-in: …` | Set `SWITCHBOARD_ADMIN_AUTH` or `SWITCHBOARD_ADMIN_PROXY_TOKEN`, or use `loopback` or `off`. |
| `SWITCHBOARD_HOSTING=on refused: SWITCHBOARD_HOSTING_REMOTE_LISTEN … names a host; give an IP address` | Use an IP address. |

Start log: `terminal hosting is on`, `switchboard remote listener on 0.0.0.0:8091 (every interface) (node channel and role API; any peer)`, `switchboard listening on …`.

## Container

`compose.yaml` passes the hosting settings through and publishes the node listener port. Put the values in the deploy `.env`; empty values leave hosting off.

```sh
# .env next to the deploy
SWITCHBOARD_HOSTING=on
SWITCHBOARD_HOSTING_TERMINAL=on
SWITCHBOARD_HOSTING_REMOTE_LISTEN=0.0.0.0:8091
SWITCHBOARD_TRUST_FORWARDED=1
REMOTE_BIND_ADDRESS=100.64.0.10 # leak-guard:allow
```

```sh
docker compose up -d
docker compose logs --tail 20 switchboard | grep -E 'terminal hosting|remote listener'
```

`REMOTE_BIND_ADDRESS` (default `127.0.0.1`) is the published address of port 8091. Inside the container the address is not known, so the listener binds every interface; the published address in `ports` decides who reaches it. Add `SWITCHBOARD_HOSTING_TERMINAL_ALLOW` and `SWITCHBOARD_HOSTING_REMOTE_ALLOW` to `.env` the same way to limit peers.

## First admin

```sh
docker compose exec switchboard switchboard admin create alice
```

It prompts for the password twice (12 characters or more) and prints the `otpauth://` URI and secret once. Scan it into an authenticator, then sign in at `https://mail.example` with name, password and the 6-digit code. Other modes: README, "Protected administrator dashboard".

## Reverse proxy

The terminal socket is `GET /api/admin/terminals/<id>/socket?ticket=…` (the ticket comes from `POST /api/admin/terminals/<id>/ticket`). The proxy passes the upgrade and the host:

```nginx
server {
    listen 443 ssl;
    server_name mail.example;

    location / {
        proxy_pass http://127.0.0.1:8097;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_buffering off;
        # proxy secret mode only; the value of SWITCHBOARD_ADMIN_PROXY_TOKEN, not committed
        # proxy_set_header X-Switchboard-Admin "<secret>";
    }
}
```

- `X-Forwarded-For $remote_addr` replaces what the client sent, so `SWITCHBOARD_TRUST_FORWARDED=1` sees the real peer. Do not use `$proxy_add_x_forwarded_for`.
- `Host` must reach the server, or `SWITCHBOARD_PUBLIC_URL` must name the origin, or the socket's `Origin` check gives `403`.
- The socket closes after the idle time in `proxy_read_timeout`; the viewer reconnects.

## Checklist before `SWITCHBOARD_HOSTING_TERMINAL=on` in proxy secret mode

With `SWITCHBOARD_ADMIN_PROXY_TOKEN` as the admin gate, whoever reaches the server port without the proxy and sends the header is the admin; with the terminal on that is a shell on the node machines.

1. The server port has no route around the proxy. From a machine that is not the proxy host:

   ```sh
   curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://<server address>:8097/health
   ss -ltn | grep -E ':8097|:8091'
   docker compose port switchboard 8080
   ```

   Expected: `curl` cannot connect (exit 7 or 28), `ss` and `port` show `127.0.0.1:8097` for the dashboard port and only the intended address for `8091`. A published port on `0.0.0.0` or a firewall that admits it fails the check.
2. The proxy's header is the only source: a request that sends its own header through the proxy is overwritten.

   ```sh
   curl -sS -o /dev/null -w '%{http_code}\n' -H 'X-Switchboard-Admin: wrong' https://mail.example/api/admin/me
   ```

   Expected: the proxy's sign-in step answers (not `200`).
3. The upgrade passes the proxy. Open the terminal view in the dashboard (터미널 tab, a running session): the screen shows the session and typing in it reaches the agent. From a shell, with the admin's ticket (`POST …/ticket`; a ticket works once) and, in built-in mode, the `sb_session` cookie (proxy secret mode: the proxy adds its header):

   ```sh
   curl -sS -i -N -m 5 "https://mail.example/api/admin/terminals/<id>/socket?ticket=<ticket>" \
     -H 'Cookie: sb_session=<cookie>' \
     -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
     -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: https://mail.example' | head -1
   ```

   Expected: `HTTP/1.1 101 Switching Protocols`. `400`, `502` or a plain `200` page means the proxy dropped `Upgrade` or `Connection`; `403` is a used ticket or an `Origin` that is neither the host nor `SWITCHBOARD_PUBLIC_URL`.

## Risks and how to reduce them

| Fact |
| --- |
| Terminal input is a command run on the machine the session runs on, as that machine's user. |
| With the terminal on a public address, a broken admin sign-in is a broken node machine. |
| A role token with a host grant starts sessions in its folder roots on its nodes. |

| Reduce by | How |
| --- | --- |
| Allowed ranges | `SWITCHBOARD_HOSTING_TERMINAL_ALLOW` for the terminal, `SWITCHBOARD_HOSTING_REMOTE_ALLOW` and the published address for nodes. |
| Second factor | Built-in sign-in asks a TOTP code at every login; proxy mode takes the proxy's own second factor. |
| Record | Opening and closing a terminal socket is logged with admin, session, view or input, client address and time (dashboard 기록 tab); typed text is not stored. |

## Attach a node

On the host (the server container):

```sh
docker compose exec switchboard switchboard node invite pc2
```

On the node machine ([hosting-pc.md, Node on another machine](hosting-pc.md#node-on-another-machine) for install and start at sign-in):

```sh
SWITCHBOARD_HOSTING_AGENT="$(command -v claude)" switchboard node --host http://100.64.0.10:8091 --join 'sbj_…' # leak-guard:allow
```

The code is valid 15 minutes and usable once. With `SWITCHBOARD_HOSTING_REMOTE_ALLOW` set on the node too, `--host` must be inside it.

## A role starts sessions

```sh
docker compose exec switchboard switchboard host grant team/boss --base team/worker --node pc2 --dir-root /home/user/work
docker compose exec switchboard switchboard host grant list
```

On any machine that has the role's profile (no `DB_PATH`), against the remote listener or the dashboard origin:

```sh
SWITCHBOARD_PROFILE=boss switchboard host add --node pc2 --base team/worker --profile pc-worker --dir /home/user/work/app --label app --parent team/boss -- --model opus
SWITCHBOARD_PROFILE=boss switchboard host list
SWITCHBOARD_PROFILE=boss switchboard host stop <id>
SWITCHBOARD_PROFILE=boss switchboard host remove <id>
```

Without a grant the server answers `403`. The limits of a grant and of the role's own sessions: [hosting-pc.md, Sessions a role adds under itself](hosting-pc.md#sessions-a-role-adds-under-itself).

## Order of the move

One side starts the sessions of a repository at a time, the host or the outside terminal manager; switch only when that repository's workers have finished. Mail, roles and slots are the same on both sides, so no data moves. Where both run the same role, both get mail as different slots: address a directive by its slot name.

| Step | Move | Check | Back |
| --- | --- | --- | --- |
| 0 | Turn on hosting without a node, attach a node | The terminal view shows and takes input the way the operator set it | Remove the hosting values from `.env`, restart |
| 1 | One scheduled automation (`host auto`) in place of its outside wrapper script | A week of run history and alerts equal to the wrapper's | Pause the run (`host auto pause`), start the wrapper |
| 2 | One repository's worker sessions, started by its coordinator with a grant | Five workers finish as they did under the outside terminal manager | The coordinator's brief starts them outside again |
| 3 | That repository's coordinator session | Restart, resume, mail wake-up | Start the coordinator outside again |
| 4 | The other repositories in turn, then the lead session | Steps 2 and 3 for each | Per repository |
| 5 | A person's PC sessions | That PC's node | Stop the node |

Back out of a repository: `host stop <id>` for its records, then start its sessions outside again. Mail, roles and slots stay.
