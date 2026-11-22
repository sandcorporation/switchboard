# Hosting instance on a PC

A second switchboard installation that runs agent sessions on this PC: `switchboard up` (server with terminal hosting on, plus this machine's node) on `127.0.0.1`, with its own port, data directory and profile folder. The operations instance, its plugin and its profiles stay as they are.

Values used below:

| Value | Windows | Linux |
| --- | --- | --- |
| Data directory | `%LOCALAPPDATA%\switchboard-pc` | `~/.local/share/switchboard-pc` |
| Binary | `<data>\bin\switchboard.exe` | `<data>/bin/switchboard` |
| Profile folder (`SWITCHBOARD_CONFIG_DIR`) | `<data>\config` | `<data>/config` |
| Port | `8090` | `8090` |
| Node | `local` | `local` |
| Roles / profiles | `team/boss` / `pc-boss`, `team/worker` / `pc-worker` | same |
| Admins | the person's own (setup page, W4/L4), `pcadmin` (checks) | same |
| Session directory | `%USERPROFILE%\work\app` | `~/work/app` |
| Start at sign-in | Task Scheduler task `switchboard-pc` | systemd user unit `switchboard-pc.service` |

Rules for every step:

- Profiles for this instance live only in `<data>\config`. Every command that names a profile goes through `sbpc`, which sets `SWITCHBOARD_CONFIG_DIR` and `DB_PATH` for that one command, so the operations profiles (in the user config folder) are never read or written.
- Do not set `SWITCHBOARD_PROFILE`, `SWITCHBOARD_CONFIG_DIR`, `SWITCHBOARD_ENDPOINT` or `SWITCHBOARD_TOKEN` in the user's permanent environment.
- The node may be started from inside an agent session: it drops that session's markers (`CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, and the other per-session `CLAUDE_CODE_*` names) from hosted sessions' environment and logs the names it dropped.
- A check that fails stops the procedure: do the step's failure action, then run the step again.
- Messages from `switchboard` that refuse to start are listed under [Refusals](#refusals).

## Windows

Run every block in one Windows PowerShell (5.1 or 7) window opened for these steps, as the signed-in user, not as administrator. In a new window, run `. "$env:LOCALAPPDATA\switchboard-pc\env.ps1"` first (step 3 writes it).

### W1. Prerequisites

```powershell
[Environment]::OSVersion.Version
where.exe claude
```

Check: `Build` is `17763` or higher (Windows 10 1809). `where.exe` prints at least one path; the first one ending in `.exe` is the agent.

If it fails: Build lower than 17763 → update Windows; this PC cannot host. `where.exe` finds nothing → install Claude Code for this user and open a new window. In PowerShell, `where` alone is `Where-Object`; use `where.exe`.

### W2. Download and verify

```powershell
$version = (Invoke-RestMethod https://api.github.com/repos/sandcorporation/switchboard/releases/latest).tag_name
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "switchboard_${version}_windows_$arch.zip"
$dl = Join-Path $env:TEMP "switchboard-$version"
New-Item -ItemType Directory -Force $dl | Out-Null
Invoke-WebRequest "https://github.com/sandcorporation/switchboard/releases/download/$version/$asset" -OutFile "$dl\$asset" -UseBasicParsing
Invoke-WebRequest "https://github.com/sandcorporation/switchboard/releases/download/$version/checksums.txt" -OutFile "$dl\checksums.txt" -UseBasicParsing
$want = ((Select-String -Path "$dl\checksums.txt" -SimpleMatch $asset).Line -split '\s+')[0]
$got = (Get-FileHash "$dl\$asset" -Algorithm SHA256).Hash.ToLower()
if ($got -ne $want) { throw "checksum mismatch for ${asset}: $got, want $want" } else { "checksum OK $asset" }
```

Check: the last line is `checksum OK switchboard_<version>_windows_<arch>.zip`.

If it fails: `checksum mismatch` → delete `$dl` and run the step again; a second mismatch stops the install (report the version and both hashes). A download error → check the network and that the release page lists the asset.

### W3. Data directory, binary and port

```powershell
$data = Join-Path $env:LOCALAPPDATA 'switchboard-pc'
$port = 8090
New-Item -ItemType Directory -Force "$data\bin", "$data\config" | Out-Null
Expand-Archive "$dl\$asset" -DestinationPath "$data\bin" -Force
$sb = "$data\bin\switchboard.exe"
$agent = (Get-Command claude -CommandType Application | Where-Object Source -like '*.exe' | Select-Object -First 1).Source
& $sb version
& $sb up --help
$probe = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $port); $probe.Start(); $probe.Stop()
```

Check: `version` prints the tag from W2. `up --help` lists `--data DIR` and `--port PORT`. The `$probe` line prints nothing (`up` can bind the port). `$agent` is a path ending in `.exe`.

If it fails: `up --help` prints the general help instead → the release predates terminal hosting; use a newer release. The `$probe` line prints `Only one usage of each socket address` or `forbidden by its access permissions` → another program holds the port, or Windows reserves it, even when no listener shows for it; set `$port` to another port (`8091`, …), run the block again, and use that port everywhere below. `$agent` is empty → back to W1.

Save the values for later windows:

```powershell
@"
`$data = '$data'
`$port = $port
`$sb = '$sb'
`$agent = '$agent'
"@ | Set-Content "$data\env.ps1"
```

The helpers block writes the helpers to `$data\sb.ps1` and makes `env.ps1` load them. It touches no value, so it can run again at any time; W9 runs it before an update, and an `env.ps1` from docs before v0.17.0 gets `sbstop` and `sbstarted` from it:

```powershell
@'
function sbpc {
  $env:SWITCHBOARD_CONFIG_DIR = "$data\config"; $env:DB_PATH = "$data\switchboard.db"
  try { & $sb @args } finally { Remove-Item Env:SWITCHBOARD_CONFIG_DIR, Env:DB_PATH -ErrorAction SilentlyContinue }
}
function New-SbToken { $b = New-Object byte[] 32; [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b); [Convert]::ToBase64String($b) }
function Test-SbHealth { try { (Invoke-WebRequest "http://127.0.0.1:$port/health" -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200 } catch { $false } }
function sbwait([int]$seconds, [scriptblock]$ok) {
  for ($i = 0; $i -lt $seconds; $i++) { if (& $ok) { return }; Start-Sleep 1 }
  throw "timed out after ${seconds}s: $ok"
}
function sbstop {
  $ups = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue | ForEach-Object { $_.OwningProcess } | Select-Object -Unique)
  if ($ups.Count -gt 0) {
    Set-Content "$data\up.stop" ''
    Wait-Process -Id $ups -Timeout 40 -ErrorAction SilentlyContinue
    $ups | Where-Object { Get-Process -Id $_ -ErrorAction SilentlyContinue } | ForEach-Object { Write-Warning "up ($_) still runs 40 s after up.stop; ending it"; Stop-Process -Id $_ -Force }
  }
  $upcmd = [regex]::Escape("$data\up.cmd")
  for ($i = 0; $i -lt 60; $i++) {
    $task = Get-ScheduledTask -TaskName 'switchboard-pc' -ErrorAction SilentlyContinue
    $cmds = @(Get-CimInstance Win32_Process -Filter "Name = 'cmd.exe'" | Where-Object { $_.CommandLine -match $upcmd })
    if ((-not $task -or $task.State -eq 'Ready') -and $cmds.Count -eq 0) { return }
    Start-Sleep 1
  }
  throw "up.cmd still runs 60 s after up stopped (task switchboard-pc: $($task.State), cmd.exe: $($cmds.ProcessId)); leave up.cmd as it is"
}
function sbstarted {
  (Select-String -Path "$data\up.log" -Pattern 'admin auth: .*' | Select-Object -Last 1).Matches[0].Value
  'version ' + (Invoke-RestMethod "http://127.0.0.1:$port/health" -TimeoutSec 5).version
}
function Get-SbTotp([string]$secret) {
  $alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  $bits = -join ($secret.ToUpper().TrimEnd('=').ToCharArray() | ForEach-Object { [Convert]::ToString($alphabet.IndexOf($_), 2).PadLeft(5, '0') })
  $key = [byte[]]@(for ($i = 0; $i + 8 -le $bits.Length; $i += 8) { [Convert]::ToByte($bits.Substring($i, 8), 2) })
  $counter = [BitConverter]::GetBytes([int64][Math]::Floor([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() / 30))
  if ([BitConverter]::IsLittleEndian) { [Array]::Reverse($counter) }
  $h = (New-Object Security.Cryptography.HMACSHA1 (, $key)).ComputeHash($counter)
  $o = $h[19] -band 15
  $n = (([int]($h[$o] -band 127) -shl 24) -bor ([int]$h[$o + 1] -shl 16) -bor ([int]$h[$o + 2] -shl 8) -bor [int]$h[$o + 3]) % 1000000
  '{0:D6}' -f $n
}
function sblogin {
  $admin = Get-Content "$data\admin.json" -Raw | ConvertFrom-Json
  $body = @{ name = 'pcadmin'; password = (Get-Content "$data\admin.password" -Raw).Trim(); code = (Get-SbTotp $admin.secret) } | ConvertTo-Json
  Invoke-RestMethod -Method Post "http://127.0.0.1:$port/auth/login" -ContentType 'application/json' -Headers @{ 'X-Switchboard-Dashboard' = '1' } -Body $body -SessionVariable web | Out-Null
  $global:sbweb = $web
}
function sblist { $r = Invoke-RestMethod "http://127.0.0.1:$port/api/admin/terminals" -WebSession $global:sbweb; $r }
function sbget([string]$id) { sblist | Where-Object id -eq $id }
function sbterm { sblist | Select-Object id, @{ n = 'state'; e = { $_.runtime.state } }, @{ n = 'pid'; e = { $_.runtime.pid } }, @{ n = 'run'; e = { $_.runtime.run } }, agent_session_id, @{ n = 'slot'; e = { $_.slot.role } }, presence | Format-Table -AutoSize }
'@ | Set-Content "$data\sb.ps1"
if (-not (Select-String -Path "$data\env.ps1" -Pattern 'sb\.ps1' -Quiet)) { Add-Content "$data\env.ps1" '. "$data\sb.ps1"' }
. "$data\env.ps1"
sbpc version
```

Check: `sbpc version` prints the same tag as `& $sb version`.

If it fails: a parse error in `env.ps1` or `sb.ps1` → delete `$data\env.ps1`, run both blocks again unchanged.

### W4. Admin

The first admin is made on the instance's setup page. It answers only on `127.0.0.1`, only while the instance has no admin, and needs the one-time code in `$data\setup-code`.

```powershell
$env:SWITCHBOARD_ADMIN_AUTH = 'builtin'; $env:SWITCHBOARD_INSECURE_COOKIE = '1'; $env:SWITCHBOARD_HOSTING_AGENT = $agent
$setup = Start-Process $sb -ArgumentList 'up', '--data', "`"$data`"", '--port', $port -RedirectStandardOutput "$data\admin-setup.out" -RedirectStandardError "$data\admin-setup.log" -WindowStyle Hidden -PassThru
Remove-Item Env:SWITCHBOARD_ADMIN_AUTH, Env:SWITCHBOARD_INSECURE_COOKIE, Env:SWITCHBOARD_HOSTING_AGENT
$setup.Id | Set-Content "$data\admin-setup.pid"
sbwait 60 { if ($setup.HasExited) { throw "up exited: $(Get-Content "$data\admin-setup.log" -Tail 3)" }; Test-SbHealth }
(Invoke-WebRequest "http://127.0.0.1:$port/setup" -UseBasicParsing).StatusCode
"http://127.0.0.1:$port/setup"
Get-Content "$data\setup-code"
```

Check: prints `200`, then the setup address and a code like `ABCD-EFGH-JKLM-NPQR`.

If it fails: `up exited: …` or `timed out` → look the message up under [Refusals](#refusals). `404` or no `setup-code` → this data directory already has an admin; continue with the next block.

The person at this PC opens the setup address in a browser on this PC and enters the code, a name and a password (12 or more characters) of their own, scans the QR code with an authenticator app and enters the 6-digit code the app shows. The admin exists only after that code is accepted; the browser is then signed in to the dashboard. Five wrong setup codes in a row replace the code (read `setup-code` again).

Then the admin for the checks in this procedure (`pcadmin`; also the way to make the first admin with nobody at the screen):

```powershell
New-SbToken | Set-Content "$data\admin.password"
$env:DB_PATH = "$data\switchboard.db"
Get-Content "$data\admin.password" | & $sb admin create pcadmin --json | Set-Content "$data\admin.json"
Remove-Item Env:DB_PATH
(Get-Content "$data\admin.json" -Raw | ConvertFrom-Json).action
try { (Invoke-WebRequest "http://127.0.0.1:$port/setup" -UseBasicParsing).StatusCode } catch { [int]$_.Exception.Response.StatusCode }
Test-Path "$data\setup-code"
Stop-Process -Id (Get-Content "$data\admin-setup.pid")
sbwait 30 { -not (Test-SbHealth) }
Remove-Item "$data\admin-setup.pid"
```

Check: prints `create`, `404` (the setup page is closed for good) and `False` (the code file is gone). `$data\admin.json` holds `secret` and `otpauth_uri` (shown only this once); `$data\admin.password` holds the password. Both stay inside the data directory, which only this user can read.

If it fails: `admin "pcadmin" already exists` → this data directory was set up before; stop the instance (`Stop-Process -Id (Get-Content "$data\admin-setup.pid")`) and continue with W5 if `admin.json` and `admin.password` are both there, otherwise run W9 uninstall and start again at W3. `DB_PATH` errors → the `$env:DB_PATH` line was skipped; run the block again.

### W5. Roles and profiles

Clear the variables an agent session (or an earlier `sbpc`) leaves in this window; `Start-Process` below passes them on to `up`.

```powershell
Remove-Item Env:SWITCHBOARD_PROFILE, Env:SWITCHBOARD_ROLE, Env:SWITCHBOARD_TOKEN, Env:SWITCHBOARD_ENDPOINT, Env:SWITCHBOARD_CONFIG_DIR, Env:SWITCHBOARD_MACHINE, Env:SWITCHBOARD_LABEL, Env:SWITCHBOARD_PARENT, Env:DB_PATH, Env:LISTEN_ADDR -ErrorAction SilentlyContinue
Get-ChildItem Env: | Where-Object Name -match '^(SWITCHBOARD_(PROFILE|ROLE|TOKEN|ENDPOINT|CONFIG_DIR|MACHINE|LABEL|PARENT)|DB_PATH|LISTEN_ADDR)$'
```

Check: `Get-ChildItem` prints nothing.

`register` creates a role only on an instance started with `SWITCHBOARD_ENROLLMENT_TOKEN`; the instance runs with it only for these commands.

```powershell
New-SbToken | Set-Content "$data\enroll.token"
foreach ($r in 'boss', 'worker') { New-SbToken | Set-Content "$data\$r.token" }
$env:SWITCHBOARD_ADMIN_AUTH = 'builtin'; $env:SWITCHBOARD_HOSTING_AGENT = $agent; $env:SWITCHBOARD_ENROLLMENT_TOKEN = Get-Content "$data\enroll.token"
$setup = Start-Process $sb -ArgumentList 'up', '--data', "`"$data`"", '--port', $port -RedirectStandardOutput "$data\setup.out" -RedirectStandardError "$data\setup.log" -WindowStyle Hidden -PassThru
Remove-Item Env:SWITCHBOARD_ADMIN_AUTH, Env:SWITCHBOARD_HOSTING_AGENT, Env:SWITCHBOARD_ENROLLMENT_TOKEN
sbwait 60 { if ($setup.HasExited) { throw "up exited: $(Get-Content "$data\setup.log" -Tail 3)" }; Test-SbHealth }
foreach ($r in 'boss', 'worker') {
  sbpc register --endpoint "http://127.0.0.1:$port" --role "team/$r" --token-file "$data\$r.token" --enrollment-token-file "$data\enroll.token"
  sbpc --profile "pc-$r" config set --endpoint "http://127.0.0.1:$port" --role "team/$r" --token-file "$data\$r.token"
}
sbpc --profile pc-worker inbox
Stop-Process -Id $setup.Id
Remove-Item "$data\enroll.token", "$data\boss.token", "$data\worker.token"
Get-ChildItem "$data\config"
```

Check: each `register` prints `{"role":"team/<name>","status":"registered"}`, each `config set` prints `Saved <data>\config\pc-<name>.json`, `inbox` prints no error, and `$data\config` lists `pc-boss.json` and `pc-worker.json`. A profile holds the role token; `host add` names only the profile.

If it fails: `up exited: …` or `timed out` waiting for health → look the message (the rest of it is in `$data\setup.log`) up under [Refusals](#refusals). `role already registered` → the role exists from an earlier run whose profile is in `$data\config`; skip it. `enrollment authorization required` → the instance was not started from this block; stop it (`Stop-Process -Id $setup.Id`) and run the block again.

### W6. Start, and start at sign-in

Every block that writes `up.cmd` starts with `sbstop` (its first line stops the block when `env.ps1` predates `sbstop`; run the helpers block of W3 then): it stops `up` and waits until the task `switchboard-pc` is `Ready` and no `cmd.exe` runs `up.cmd`. cmd.exe reads a batch file by position while it runs, so an `up.cmd` rewritten under a running one can start the old `up` again with the old settings, and `Start-ScheduledTask` does nothing while the task still runs.

```powershell
if (-not (Get-Command sbstop -ErrorAction SilentlyContinue)) { throw 'sbstop is missing: this env.ps1 predates it; run the helpers block of W3, then this block again' }
sbstop
@"
@echo off
set SWITCHBOARD_ADMIN_AUTH=builtin
set SWITCHBOARD_INSECURE_COOKIE=1
set "SWITCHBOARD_CONFIG_DIR=%~dp0config"
set "SWITCHBOARD_HOSTING_AGENT=$agent"
"%~dp0bin\switchboard.exe" up --data "%~dp0." --port $port >> "%~dp0up.log" 2>&1
"@ | Set-Content "$data\up.cmd" -Encoding Ascii
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -WindowStyle Hidden -Command `"& '$data\up.cmd'`""
$trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName 'switchboard-pc' -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName 'switchboard-pc'
sbwait 60 { Test-SbHealth }
Select-String -Path "$data\up.log" -Pattern 'terminal hosting is on', 'conpty backend', 'switchboard listening on 127.0.0.1:'
sbstarted
```

Check: health answers within 60 s and `Select-String` shows the three lines (`terminal hosting is on`, `node local: conpty backend, socket …`, `switchboard listening on 127.0.0.1:8090`). `sbstarted` prints `admin auth: builtin` and `version` with the tag from W2. The task runs at every sign-in of this user, with no time limit and without administrator rights.

If it fails: `sbstop` says `up.cmd still runs` → leave `up.cmd` as it is, end the task (`Stop-ScheduledTask -TaskName 'switchboard-pc'`; stop the `cmd.exe` it names) and run the block again. `sbstarted` prints another mode or an older version → an old `up` is running; run the block again. `Register-ScheduledTask` says `Access is denied` → use the Startup folder instead and start it now:

```powershell
Set-Content "$([Environment]::GetFolderPath('Startup'))\switchboard-pc.cmd" "@start `"`" /min `"$data\up.cmd`"" -Encoding Ascii
Start-Process -WindowStyle Minimized "$data\up.cmd"
```

`up` stopped later on its own (health does not answer, the task is `Ready`) → read the end of `$data\up.log`. Every way `up` ends by itself leaves an `exit N: …` line there, after a `signal …`, `up.stop: …`, `console event …` or `panic in …` line when that was the cause. No `exit` line after the last start means a program outside ended `switchboard.exe` (as `Stop-Process` does). The task does not start it again: Task Scheduler restarts a task only when it fails to start, not when its program exits with an error, so W6 sets no restart. Start it with `Start-ScheduledTask -TaskName 'switchboard-pc'`; it adopts the keeper and the sessions keep running (`node: keeper … (adopted)` in `up.log`).

(With the Startup folder, "Start-ScheduledTask" in W8 and W9 becomes `Start-Process -WindowStyle Minimized "$data\up.cmd"`.) Health times out → read the end of `$data\up.log` (`Get-Content "$data\up.log" -Tail 20`) and look the message up under [Refusals](#refusals).

### W7. Add a session

```powershell
$work = Join-Path $env:USERPROFILE 'work\app'
New-Item -ItemType Directory -Force $work | Out-Null
$id = sbpc host add --node local --base team/worker --profile pc-worker --dir $work --label app --parent team/boss --restart always '--' --model opus
$id
sbpc host list
sblogin
sbwait 120 { (sbget $id).runtime.state -eq 'running' }
sbterm
```

Check: `host add` prints the new session ID; `host list` shows it with node `local`, base `team/worker`, `DESIRED` `running`, restart `always`. `sbterm` (`GET /api/admin/terminals`) shows it with state `running` and a pid. `--group NAME` puts it in a work group (`host group add NAME --threshold N`); `--restart` is `always`, `on-failure` or `never`; words after `'--'` go to the agent (the quotes keep PowerShell from eating `--`). `host list` shows `PERMS` `skip`: see [Permission mode](#permission-mode).

If it fails: `base role ... must exist` → W5 did not register `team/worker`. `sblogin` gets `401` → the code was used already in this 30-second step; wait 30 s and run `sblogin` again. State stays `waiting` → `SWITCHBOARD_HOSTING_MAX` (default 4) sessions are running already. State `crashloop` or `exited` → open the session in the dashboard (`http://127.0.0.1:8090`, tab Terminals) to read the agent's screen; a first start in a new folder can stop at the agent's folder-trust question, which someone at the PC answers there once.

### W8. Verify

Slot with its number:

```powershell
sbwait 120 { $s = sbget $id; $s.slot.role -eq 'team/worker-1' -and $s.presence -eq 'online' }
sbterm
```

Check: `slot` is `team/worker-1` (the base with its number) and `presence` is `online`.

If it fails: `slot` stays empty → the agent did not register its slot; check that the switchboard plugin is installed for Claude Code on this PC and that `$data\config\pc-worker.json` exists.

The session survives a restart of `up`:

```powershell
$before = sbget $id
if (-not (Get-Command sbstop -ErrorAction SilentlyContinue)) { throw 'sbstop is missing: this env.ps1 predates it; run the helpers block of W3, then this block again' }
sbstop
Start-ScheduledTask -TaskName 'switchboard-pc'
sbwait 60 { Test-SbHealth }
sbwait 60 { (sbget $id).runtime.state -eq 'running' }
$after = sbget $id
if ($after.runtime.pid -ne $before.runtime.pid -or $after.runtime.run -ne $before.runtime.run) { throw 'the session was relaunched' } else { 'same process' }
```

Check: prints `same process`.

If it fails: `the session was relaunched` → the keeper was stopped with `up`; read `$data\up.log` for `node: keeper` lines and report them.

A killed session comes back with resume:

```powershell
Stop-Process -Id $after.runtime.pid -Force
sbwait 120 { $s = sbget $id; $s.runtime.state -eq 'running' -and $s.runtime.run -gt $after.runtime.run }
if ((sbget $id).agent_session_id -ne $after.agent_session_id) { throw 'a new conversation instead of resume' } else { 'resumed' }
```

Check: prints `resumed` (same `agent_session_id`, higher `run`).

If it fails: state `crashloop` → the agent exits right after start; open its terminal in the dashboard and read why.

### W9. Stop, update, uninstall

Stop `up` only (sessions keep running in the keeper; the next start adopts them). `sbstop` writes `up.stop` in the data directory and waits for `up` to exit: `up` stops the way Ctrl-C stops it (`up.stop: shutting down` and `exit 0: stopped by up.stop` in `up.log`), and the session of a record removed just before stops with it. No console event reaches `up` started by the task, so a signal cannot do this. An `up` still running 40 s later (one that predates `up.stop`) is ended at once with a warning; the session of a record removed just before then runs until the next start stops it:

```powershell
if (-not (Get-Command sbstop -ErrorAction SilentlyContinue)) { throw 'sbstop is missing: this env.ps1 predates it; run the helpers block of W3, then this block again' }
sbstop
```

The keeper ends by itself after five minutes with no sessions and no node attached (`keeper: no sessions and no node attached for 5m0s; exiting` in `$data\keeper-<instance>.log`). A session of any state, even one that has ended, or an attached node keeps it. A running `up` whose keeper went away (it ended this way, or something ended it) starts a new one on its next pass (`node: keeper: none answered; started a new one` in `up.log`) and relaunches every session with `--resume`; the next `up` does the same. When the new keeper cannot start, every session shows `backend down` (터미널 끊김) until one answers. A connect to the keeper's pipe that fails is logged (`keeper: accept: …; retrying`) and does not end the keeper; a keeper that ends on a pipe error logs `keeper: serve: …; shutting down` first. The keeper is started outside the job of the shell that ran `up` when that job allows it; when it does not, the keeper ends with that job.

Stop or start one session: `sbpc host stop $id`, `sbpc host start $id` (while `up` runs).

Update: run W2 for the new version, run the helpers block of W3 again (it brings `sbstop` and the other helpers up to date; an `env.ps1` from docs before v0.17.0 has no `sbstop`), then:

```powershell
if (-not (Get-Command sbstop -ErrorAction SilentlyContinue)) { throw 'sbstop is missing: this env.ps1 predates it; run the helpers block of W3, then this block again' }
sbstop
Move-Item -Force $sb "$data\bin\switchboard.old.exe"
Expand-Archive "$dl\$asset" -DestinationPath "$data\bin" -Force
& $sb version
Start-ScheduledTask -TaskName 'switchboard-pc'
sbwait 60 { Test-SbHealth }
sbstarted
sbwait 180 { (sbget $id).runtime.state -eq 'running' }
Select-String -Path "$data\up.log" -Pattern 'node: keeper' | Select-Object -Last 1
Remove-Item "$data\bin\switchboard.old.exe" -ErrorAction SilentlyContinue
```

Check: `version` prints the new tag, and `sbstarted` prints the admin auth line of `up.cmd` and `version` with the new tag; the session is `running` again. `sbstop` waits for the task to be `Ready`: a start while the old `up.cmd` is still exiting is dropped (`MultipleInstances IgnoreNew`). The new `up` replaces the keeper of the old version and relaunches every session with `--resume`, so each conversation continues. A running `switchboard.exe` can be renamed but not overwritten, hence the `Move-Item`; when the last `Remove-Item` cannot delete the old file, run it again later.

If it fails: `sbstop is missing` → the helpers block of W3 did not run in this window; run it, then this block again (nothing was changed yet). Health times out → `Get-Content "$data\up.log" -Tail 20`. `sbstarted` prints the old version → the task did not start the new `up`; run `sbstop`, then `Start-ScheduledTask -TaskName 'switchboard-pc'`. To go back, run `sbstop`, move `switchboard.old.exe` back to `switchboard.exe` and start the task.

Uninstall (removes the sessions, the task, the keeper and the data directory; conversations of the agent stay in its own folders):

```powershell
Start-ScheduledTask -TaskName 'switchboard-pc'
sbwait 60 { Test-SbHealth }
(sbpc host list --json | ConvertFrom-Json) | ForEach-Object { sbpc host remove $_.id }
sbwait 60 { @(sblist).Count -eq 0 }
Unregister-ScheduledTask -TaskName 'switchboard-pc' -Confirm:$false
Get-Process switchboard -ErrorAction SilentlyContinue | Where-Object Path -like "$data\bin\*" | Stop-Process -Force
Remove-Item -Recurse -Force $data
```

Check: `Get-ScheduledTask switchboard-pc` reports no task and `Test-Path $data` is `False`. Only processes started from `$data\bin` are stopped; any other `switchboard.exe` (the operations CLI) is left alone.

If it fails: `Remove-Item` reports a file in use → wait 5 s (Windows releases files after the process ends) and run it again. With the Startup folder (W6), also delete `switchboard-pc.cmd` there.

Reinstall: a removed node (revoked in the dashboard, Settings) cannot come back in the same data directory; `up` stops with `node local: node is revoked`. Uninstall as above and start again at W3.

## Linux

Requires bash, `curl`, `python3` (for the checks), and tmux 3.2 or newer. Run every block in one bash shell as the user who will own the sessions, not as root. In a new shell, run `. ~/.local/share/switchboard-pc/env.sh` first (step 3 writes it).

### L1. Prerequisites

```sh
sbtmux_ok() { command -v tmux >/dev/null && printf '3.2\n%s\n' "$(tmux -V | sed 's/^tmux //; s/[^0-9.].*$//')" | sort -V -C; }
if ! sbtmux_ok; then
  . /etc/os-release
  case " ${ID-} ${ID_LIKE-} " in
    *" debian "*|*" ubuntu "*) sudo apt-get install -y tmux ;;
    *" fedora "*|*" rhel "*) sudo dnf install -y tmux ;;
    *" arch "*) sudo pacman -S --needed --noconfirm tmux ;;
    *" suse "*|*" opensuse "*) sudo zypper install -y tmux ;;
    *" alpine "*) sudo apk add tmux ;;
  esac
fi
sbtmux_ok && tmux -V
command -v python3 curl
command -v claude
```

Check: `tmux -V` prints 3.2 or newer; `command -v` prints a path for `python3`, `curl` and `claude`.

If it fails: tmux still missing or older than 3.2 → the distribution's package is too old; install tmux 3.2+ from the distribution's backports or build it, then run the step again. `claude` not found → install Claude Code for this user and open a new shell.

### L2. Download and verify

```sh
SBVER="$(curl -fsSL https://api.github.com/repos/sandcorporation/switchboard/releases/latest | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])')"
case "$(uname -m)" in x86_64|amd64) SBARCH=amd64 ;; aarch64|arm64) SBARCH=arm64 ;; esac
SBASSET="switchboard_${SBVER}_linux_${SBARCH}.tar.gz"
SBDL="$(mktemp -d)"
curl -fsSL -o "$SBDL/$SBASSET" "https://github.com/sandcorporation/switchboard/releases/download/$SBVER/$SBASSET"
curl -fsSL -o "$SBDL/checksums.txt" "https://github.com/sandcorporation/switchboard/releases/download/$SBVER/checksums.txt"
(cd "$SBDL" && sha256sum --ignore-missing -c checksums.txt)
```

Check: the last line is `switchboard_<version>_linux_<arch>.tar.gz: OK`.

If it fails: `FAILED` → download again into a new directory; a second mismatch stops the install (report the version and `sha256sum` output). `SBARCH` empty → this machine is neither amd64 nor arm64; no release asset fits.

### L3. Data directory, binary and port

```sh
umask 077
mkdir -p "$HOME/.local/share/switchboard-pc/bin" "$HOME/.local/share/switchboard-pc/config"
cat > "$HOME/.local/share/switchboard-pc/env.sh" <<'EOF'
SBDATA="$HOME/.local/share/switchboard-pc"
SBPORT=8090
SB="$SBDATA/bin/switchboard"
sbpc() { SWITCHBOARD_CONFIG_DIR="$SBDATA/config" DB_PATH="$SBDATA/switchboard.db" "$SB" "$@"; }
sbwait() { local n="$1"; shift; until "$@"; do n=$((n - 1)); if [ "$n" -le 0 ]; then echo "timed out: $*" >&2; return 1; fi; sleep 1; done; }
sbhealth() { curl -fsS -o /dev/null "http://127.0.0.1:$SBPORT/health"; }
sbtotp() {
  python3 -c 'import base64,hashlib,hmac,json,struct,sys,time
s=json.load(open(sys.argv[1]))["secret"]; k=base64.b32decode(s+"="*(-len(s)%8))
h=hmac.new(k,struct.pack(">Q",int(time.time())//30),hashlib.sha1).digest(); o=h[19]&15
print("%06d"%((struct.unpack(">I",h[o:o+4])[0]&0x7fffffff)%1000000))' "$SBDATA/admin.json"
}
sblogin() {
  curl -fsS -o /dev/null -c "$SBDATA/admin.cookies" -H 'Content-Type: application/json' -H 'X-Switchboard-Dashboard: 1' \
    -d "{\"name\":\"pcadmin\",\"password\":\"$(cat "$SBDATA/admin.password")\",\"code\":\"$(sbtotp)\"}" "http://127.0.0.1:$SBPORT/auth/login"
}
sblist() { curl -fsS -b "$SBDATA/admin.cookies" "http://127.0.0.1:$SBPORT/api/admin/terminals"; }
sbget() {
  sblist | python3 -c 'import json,sys
for h in json.load(sys.stdin):
    if h["id"] == sys.argv[1]:
        r, s = h["runtime"], h.get("slot") or {}
        print({"state": r["state"], "pid": r["pid"], "run": r["run"], "session": h["agent_session_id"], "slot": s.get("role", ""), "presence": h.get("presence", "")}[sys.argv[2]])' "$1" "$2"
}
sbis() { [ "$(sbget "$1" "$2")" = "$3" ]; }
sbterm() {
  sblist | python3 -c 'import json,sys
for h in json.load(sys.stdin):
    r, s = h["runtime"], h.get("slot") or {}
    print(h["id"], r["state"], "pid=%s" % r["pid"], "run=%s" % r["run"], h["agent_session_id"], s.get("role", "-"), h.get("presence") or "-")'
}
EOF
echo "AGENT=$(command -v claude)" >> "$HOME/.local/share/switchboard-pc/env.sh"
. "$HOME/.local/share/switchboard-pc/env.sh"
chmod 700 "$SBDATA" "$SBDATA/config"
tar -xzf "$SBDL/$SBASSET" -C "$SBDATA/bin" switchboard
"$SB" version
"$SB" up --help | grep -- '--data DIR'
python3 -c 'import socket,sys; s=socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind(("127.0.0.1", int(sys.argv[1]))); s.listen()' "$SBPORT" 2>/dev/null || { echo "port $SBPORT is in use" >&2; false; }
stat -c '%a %n' "$SBDATA" "$SBDATA/config"
```

Check: `version` prints the tag from L2; `grep` prints the `--data DIR` line; nothing says `port 8090 is in use`; `stat` prints `700` for both directories.

If it fails: `grep` prints nothing → the release predates terminal hosting; use a newer release. `port 8090 is in use` → another program holds the port (listening or only bound); change `SBPORT=` in `env.sh` to another free port, run `. "$HOME/.local/share/switchboard-pc/env.sh"` and continue.

### L4. Admin

The first admin is made on the instance's setup page. It answers only on `127.0.0.1`, only while the instance has no admin, and needs the one-time code in `$SBDATA/setup-code`.

```sh
SWITCHBOARD_ADMIN_AUTH=builtin SWITCHBOARD_INSECURE_COOKIE=1 SWITCHBOARD_HOSTING_AGENT="$AGENT" \
  "$SB" up --data "$SBDATA" --port "$SBPORT" >> "$SBDATA/admin-setup.log" 2>&1 &
echo $! > "$SBDATA/admin-setup.pid"
sbwait 60 sbhealth
curl -sS -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:$SBPORT/setup"
echo "http://127.0.0.1:$SBPORT/setup"
cat "$SBDATA/setup-code"
```

Check: prints `200`, then the setup address and a code like `ABCD-EFGH-JKLM-NPQR`.

If it fails: `timed out` → `tail -n 20 "$SBDATA/admin-setup.log"` and look the message up under [Refusals](#refusals). `404` or no `setup-code` → this data directory already has an admin; continue with the next block.

The person at this PC opens the setup address in a browser on this PC and enters the code, a name and a password (12 or more characters) of their own, scans the QR code with an authenticator app and enters the 6-digit code the app shows. The admin exists only after that code is accepted; the browser is then signed in to the dashboard. Five wrong setup codes in a row replace the code (read `setup-code` again).

Then the admin for the checks in this procedure (`pcadmin`; also the way to make the first admin with nobody at the screen):

```sh
head -c 24 /dev/urandom | base64 > "$SBDATA/admin.password"
sbpc admin create pcadmin --json < "$SBDATA/admin.password" > "$SBDATA/admin.json"
python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["action"])' "$SBDATA/admin.json"
stat -c '%a %n' "$SBDATA/admin.password" "$SBDATA/admin.json"
curl -sS -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:$SBPORT/setup"
ls "$SBDATA/setup-code" 2>/dev/null || echo 'no setup-code'
kill "$(cat "$SBDATA/admin-setup.pid")"
sbwait 30 eval '! sbhealth 2>/dev/null'
rm -f "$SBDATA/admin-setup.pid"
```

Check: prints `create`, `600` for both files, `404` (the setup page is closed for good) and `no setup-code`. `admin.json` holds `secret` and `otpauth_uri` (shown only this once).

If it fails: `admin "pcadmin" already exists` → stop the instance (`kill "$(cat "$SBDATA/admin-setup.pid")"`) and continue with L5 if both files are there, otherwise uninstall (L9) and start again at L3.

### L5. Roles and profiles

Clear the variables an agent session leaves in this shell; the `up` below inherits them.

```sh
unset SWITCHBOARD_PROFILE SWITCHBOARD_ROLE SWITCHBOARD_TOKEN SWITCHBOARD_ENDPOINT SWITCHBOARD_CONFIG_DIR SWITCHBOARD_MACHINE SWITCHBOARD_LABEL SWITCHBOARD_PARENT DB_PATH LISTEN_ADDR
env | grep -E '^(SWITCHBOARD_(PROFILE|ROLE|TOKEN|ENDPOINT|CONFIG_DIR|MACHINE|LABEL|PARENT)|DB_PATH|LISTEN_ADDR)=' || echo 'none set'
```

Check: prints `none set`.

`register` creates a role only on an instance started with `SWITCHBOARD_ENROLLMENT_TOKEN`; the instance runs with it only for these commands.

```sh
for f in enroll boss worker; do head -c 32 /dev/urandom | base64 | tr -d '\n' > "$SBDATA/$f.token"; done
SWITCHBOARD_ADMIN_AUTH=builtin SWITCHBOARD_HOSTING_AGENT="$AGENT" SWITCHBOARD_ENROLLMENT_TOKEN="$(cat "$SBDATA/enroll.token")" \
  "$SB" up --data "$SBDATA" --port "$SBPORT" >> "$SBDATA/setup.log" 2>&1 &
SBSETUP=$!
sbwait 60 sbhealth
for r in boss worker; do
  sbpc register --endpoint "http://127.0.0.1:$SBPORT" --role "team/$r" --token-file "$SBDATA/$r.token" --enrollment-token-file "$SBDATA/enroll.token"
  echo
  sbpc --profile "pc-$r" config set --endpoint "http://127.0.0.1:$SBPORT" --role "team/$r" --token-file "$SBDATA/$r.token"
done
sbpc --profile pc-worker inbox
kill "$SBSETUP"
wait "$SBSETUP" || true
rm -f "$SBDATA/enroll.token" "$SBDATA/boss.token" "$SBDATA/worker.token"
ls -l "$SBDATA/config"
```

Check: each `register` prints `{"role":"team/<name>","status":"registered"}`, each `config set` prints `Saved <data>/config/pc-<name>.json`, `inbox` prints no error, and `ls` shows `pc-boss.json` and `pc-worker.json` with `-rw-------`. A profile holds the role token; `host add` names only the profile.

If it fails: `timed out` → `tail -n 20 "$SBDATA/setup.log"` and look the message up under [Refusals](#refusals). `role already registered` → the role exists from an earlier run whose profile is in `$SBDATA/config`; skip it. `enrollment authorization required` → another process holds the port; stop it and run the block again.

### L6. Start, and start at sign-in

```sh
mkdir -p "$HOME/.config/systemd/user"
cat > "$HOME/.config/systemd/user/switchboard-pc.service" <<EOF
[Unit]
Description=switchboard hosting instance on this PC

[Service]
Environment=SWITCHBOARD_ADMIN_AUTH=builtin
Environment=SWITCHBOARD_INSECURE_COOKIE=1
Environment=SWITCHBOARD_CONFIG_DIR=$SBDATA/config
Environment=SWITCHBOARD_HOSTING_AGENT=$AGENT
Environment=PATH=$PATH
ExecStart=$SB up --data $SBDATA --port $SBPORT
StandardOutput=append:$SBDATA/up.log
StandardError=append:$SBDATA/up.log
KillMode=process
Restart=on-failure

[Install]
WantedBy=default.target
EOF
: > "$SBDATA/up.log"
systemctl --user daemon-reload
systemctl --user enable switchboard-pc.service
systemctl --user restart switchboard-pc.service
sbwait 60 sbhealth
grep -E 'terminal hosting is on|tmux backend|switchboard listening on 127.0.0.1:' "$SBDATA/up.log"
```

Check: health answers within 60 s and `grep` shows `terminal hosting is on`, `node local: tmux backend, socket …` and `switchboard listening on 127.0.0.1:8090`. The unit starts at every sign-in of this user. `KillMode=process` stops only `up`; the tmux server with the sessions keeps running.

If it fails: `systemctl --user` reports `Failed to connect to bus` → the shell has no user session (for example over `su`); sign in as the user directly. Health times out → `tail -n 20 "$SBDATA/up.log"` and look the message up under [Refusals](#refusals).

### L7. Add a session

```sh
mkdir -p "$HOME/work/app"
SBID="$(sbpc host add --node local --base team/worker --profile pc-worker --dir "$HOME/work/app" --label app --parent team/boss --restart always -- --model opus)"
echo "$SBID"
sbpc host list
sblogin
sbwait 120 sbis "$SBID" state running
sbterm
```

Check: `host add` prints the new session ID; `host list` shows it with node `local`, base `team/worker`, `DESIRED` `running`, restart `always`. `sbterm` (`GET /api/admin/terminals`) shows it as `running` with a pid. `--group NAME` puts it in a work group (`host group add NAME --threshold N`); `--restart` is `always`, `on-failure` or `never`; words after `--` go to the agent. `host list` shows `PERMS` `skip`: see [Permission mode](#permission-mode).

If it fails: `base role ... must exist` → L5 did not register `team/worker`. `sblogin` fails with `401` → the code was used already in this 30-second step; wait 30 s and run `sblogin` again. State stays `waiting` → `SWITCHBOARD_HOSTING_MAX` (default 4) sessions are running already. State `crashloop` or `exited` → open the session in the dashboard (`http://127.0.0.1:8090`, tab Terminals) to read the agent's screen; a first start in a new folder can stop at the agent's folder-trust question, which someone at the PC answers there once.

### L8. Verify

Slot with its number:

```sh
sbwait 120 sbis "$SBID" slot team/worker-1
sbwait 30 sbis "$SBID" presence online
sbterm
```

Check: the slot is `team/worker-1` and presence `online`.

If it fails: the slot stays empty → check that the switchboard plugin is installed for Claude Code for this user and that `$SBDATA/config/pc-worker.json` exists.

The session survives a restart of `up`:

```sh
SBPID="$(sbget "$SBID" pid)"; SBRUN="$(sbget "$SBID" run)"; SBSESSION="$(sbget "$SBID" session)"
systemctl --user restart switchboard-pc.service
sbwait 60 sbhealth
sbwait 60 sbis "$SBID" state running
[ "$(sbget "$SBID" pid)" = "$SBPID" ] && [ "$(sbget "$SBID" run)" = "$SBRUN" ] && echo "same process"
```

Check: prints `same process`.

If it fails: the unit lacks `KillMode=process` (systemd stopped the tmux server with `up`); fix the unit (L6) and run this check again.

A killed session comes back with resume:

```sh
kill "$SBPID"
sbnewrun() { sbis "$SBID" state running && [ "$(sbget "$SBID" run)" -gt "$SBRUN" ]; }
sbwait 120 sbnewrun
[ "$(sbget "$SBID" session)" = "$SBSESSION" ] && echo "resumed"
```

Check: prints `resumed` (same conversation ID, higher run).

If it fails: state `crashloop` → the agent exits right after start; open its terminal in the dashboard and read why.

### L9. Stop, update, uninstall

Stop `up` only (sessions keep running in tmux; the next start adopts them). The session of a record removed just before is stopped first: `up` waits for it to exit, at most 15 s:

```sh
systemctl --user stop switchboard-pc.service
```

Stop or start one session: `sbpc host stop "$SBID"`, `sbpc host start "$SBID"` (while `up` runs).

Update: run L2 for the new version, then:

```sh
systemctl --user stop switchboard-pc.service
tar -xzf "$SBDL/$SBASSET" -C "$SBDATA/bin" switchboard
"$SB" version
systemctl --user start switchboard-pc.service
sbwait 60 sbhealth
sbwait 60 sbis "$SBID" state running
```

Check: `version` prints the new tag and the session is `running`; it keeps its process across the update.

If it fails: `tail -n 20 "$SBDATA/up.log"`; to go back, run L2 with `SBVER` set to the old tag instead of the first line and repeat this block.

Uninstall (removes the sessions, the unit, the tmux server and the data directory; conversations of the agent stay in its own folders):

```sh
systemctl --user start switchboard-pc.service
sbwait 60 sbhealth
for id in $(sbpc host list --json | python3 -c 'import json,sys; print(" ".join(h["id"] for h in json.load(sys.stdin)))'); do sbpc host remove "$id"; done
sbempty() { [ "$(sblist)" = "[]" ]; }
sbwait 60 sbempty
systemctl --user disable --now switchboard-pc.service
rm -f "$HOME/.config/systemd/user/switchboard-pc.service"
systemctl --user daemon-reload
SBSOCK="$(sed -n 's/.*tmux backend, socket \([^ ]*\).*/\1/p' "$SBDATA/up.log" | tail -n 1)"
[ -z "$SBSOCK" ] || tmux -L "$SBSOCK" kill-server 2>/dev/null || true
rm -r "$SBDATA"
```

Check: `systemctl --user status switchboard-pc.service` reports the unit as not found and `$SBDATA` no longer exists.

If it fails: `sbwait` times out → the sessions did not stop; `sbterm` shows which, and the `kill-server` line ends them anyway.

Reinstall: a removed node (revoked in the dashboard, Settings) cannot come back in the same data directory; `up` stops with `node local: node is revoked`. Uninstall as above and start again at L3.

## Node on another machine

A node on a second machine runs hosted sessions there for the instance above (the host). The node machine needs only the binary, the agent and a network path to the host; it runs no server and no database. The host listens for it on one of its addresses, `100.64.0.10:8091` below. Other values: <!-- leak-guard:allow -->

| Value | Windows | Linux |
| --- | --- | --- |
| Node name | `pc2` | `pc2` |
| Node directory | `%LOCALAPPDATA%\switchboard-node` | `~/.local/share/switchboard-node` |
| Profile folder (`SWITCHBOARD_CONFIG_DIR`) | `<node>\config` | `<node>/config` |
| Start at sign-in | Task Scheduler task `switchboard-node` | systemd user unit `switchboard-node.service` |

The host gives each session on the node its own credential of the session's base role over the node channel. The node writes it into its profile folder as `hosted-<record id>.json` (user-only: mode 0600, a protected DACL on Windows) and starts the session with `SWITCHBOARD_PROFILE=hosted-<record id>`, which a profile named in the folder's own agent settings does not replace (for `claude` the node also puts its identity variables in the env of the session's `--settings`, which Claude Code applies over the folder's `.claude/settings*.json`); the role's own token never leaves the host machine and no profile is copied by hand. `--profile` of `host add` is still required but the node does not read it. The credential stops working (401) when the record is removed, the node is revoked or the role is revoked; a node that lost its profile folder gets new ones when it attaches. The host's own node (`local`) keeps using the profiles named in its records.

### H1. Open the host to the node machine

On the host machine, add the remote listener to the start file and restart: on Windows add `set SWITCHBOARD_HOSTING_REMOTE_LISTEN=100.64.0.10:8091` after the `SWITCHBOARD_HOSTING_AGENT` line of the W6 block and run W6 again; on Linux add `Environment=SWITCHBOARD_HOSTING_REMOTE_LISTEN=100.64.0.10:8091` after the `SWITCHBOARD_HOSTING_AGENT` line of the L6 block and run L6 again. Use an address of the host that the node machine reaches. <!-- leak-guard:allow -->

Check: `up.log` shows `switchboard remote listener on 100.64.0.10:8091 (node channel and role API; …)` besides `switchboard listening on 127.0.0.1:8090`. <!-- leak-guard:allow -->

If it fails: `up` refuses the address (see [Refusals](#refusals)) → it is a wildcard, a host name or loopback, or outside `SWITCHBOARD_HOSTING_REMOTE_ALLOW` when that is set; use one of the machine's own IP addresses. To limit the listener and its peers to some ranges, also set `SWITCHBOARD_HOSTING_REMOTE_ALLOW` (CIDR list) on the host and the node; unset, any address is accepted and the node token is the gate.

### H2. Invite the node

On the host machine, in a window with the host's environment (`env.ps1` / `env.sh`):

```sh
sbpc node invite pc2
```

Check: one line with a code `sbj_…`, valid 15 minutes and usable once.

The dashboard makes the same code: 노드 (left index) → 노드 붙이기, the name, 초대 코드 만들기. The code shows once, with the `--join` command.

If it fails: `already exists` → the name is taken (a removed node's name stays taken); pick another name.

### NW1. Windows node: download and install

On the node machine run W1 and W2, then:

```powershell
$node = Join-Path $env:LOCALAPPDATA 'switchboard-node'
New-Item -ItemType Directory -Force "$node\bin", "$node\config" | Out-Null
Expand-Archive "$dl\$asset" -DestinationPath "$node\bin" -Force
$sbn = "$node\bin\switchboard.exe"
$agent = (Get-Command claude -CommandType Application | Where-Object Source -like '*.exe' | Select-Object -First 1).Source
$hostUrl = 'http://100.64.0.10:8091' # leak-guard:allow
& $sbn version
```

Check: `version` prints the tag from W2 (the host's tag or newer). `$agent` is a path ending in `.exe`.

### NW2. Windows node: join

```powershell
$env:SWITCHBOARD_CONFIG_DIR = "$node\config"
$env:SWITCHBOARD_HOSTING_AGENT = $agent
& $sbn node --host $hostUrl --join 'sbj_…'
```

Check: the node prints `node pc2: joined; token saved to …\config\node.token`, then `node: attached to http://100.64.0.10:8091`. `pc2` is listed in the host's dashboard (Settings, nodes) with this machine's name. Stop it with Ctrl+C; NW3 runs it from now on. <!-- leak-guard:allow -->

If it fails: `join refused (401)` → the code expired or was used; run H2 again. A refusal naming `SWITCHBOARD_HOSTING_REMOTE_ALLOW` → the host URL resolves outside the private ranges. `… already exists; run without --join` → this machine already joined; skip to NW3.

### NW3. Windows node: start, and start at sign-in

```powershell
Stop-ScheduledTask -TaskName 'switchboard-node' -ErrorAction SilentlyContinue
@"
@echo off
set "SWITCHBOARD_CONFIG_DIR=%~dp0config"
set "SWITCHBOARD_HOSTING_AGENT=$agent"
"%~dp0bin\switchboard.exe" node --host $hostUrl >> "%~dp0node.log" 2>&1
"@ | Set-Content "$node\node.cmd" -Encoding Ascii
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -WindowStyle Hidden -Command `"& '$node\node.cmd'`""
$trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName 'switchboard-node' -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName 'switchboard-node'
Start-Sleep 5
Select-String -Path "$node\node.log" -Pattern 'conpty backend', 'attached to'
```

Check: `Select-String` shows `node: conpty backend, socket …` and `node: attached to http://100.64.0.10:8091`. The task runs at every sign-in of this user, without administrator rights. <!-- leak-guard:allow -->

If it fails: no `attached to` line → read `Get-Content "$node\node.log" -Tail 20`; `host channel down (…)` means the host or the private network is not reachable (the node keeps retrying). `Register-ScheduledTask` says `Access is denied` → use the Startup folder as in W6, with `node.cmd`.

### NL1. Linux node: download and install

On the node machine run L1 and L2, then:

```sh
umask 077
SBNODE="$HOME/.local/share/switchboard-node"
mkdir -p "$SBNODE/bin" "$SBNODE/config"
chmod 700 "$SBNODE" "$SBNODE/config"
tar -xzf "$SBDL/$SBASSET" -C "$SBNODE/bin" switchboard
SBHOST=http://100.64.0.10:8091 # leak-guard:allow
AGENT="$(command -v claude)"
"$SBNODE/bin/switchboard" version
```

Check: `version` prints the tag from L2 (the host's tag or newer). `AGENT` is a path.

### NL2. Linux node: join

```sh
SWITCHBOARD_CONFIG_DIR="$SBNODE/config" SWITCHBOARD_HOSTING_AGENT="$AGENT" "$SBNODE/bin/switchboard" node --host "$SBHOST" --join 'sbj_…'
```

Check and failures: as in NW2 (the token is saved to `$SBNODE/config/node.token`). Stop it with Ctrl+C.

### NL3. Linux node: start, and start at sign-in

```sh
mkdir -p "$HOME/.config/systemd/user"
cat > "$HOME/.config/systemd/user/switchboard-node.service" <<EOF
[Unit]
Description=switchboard node for a hosting instance on another machine

[Service]
Environment=SWITCHBOARD_CONFIG_DIR=$SBNODE/config
Environment=SWITCHBOARD_HOSTING_AGENT=$AGENT
Environment=PATH=$PATH
ExecStart=$SBNODE/bin/switchboard node --host $SBHOST
StandardOutput=append:$SBNODE/node.log
StandardError=append:$SBNODE/node.log
KillMode=process
Restart=on-failure

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable switchboard-node.service
systemctl --user restart switchboard-node.service
sleep 5
grep -E 'tmux backend|attached to' "$SBNODE/node.log"
```

Check: `grep` shows `node: tmux backend, socket …` and `node: attached to http://100.64.0.10:8091`. `KillMode=process` stops only the node; the tmux server with the sessions keeps running. <!-- leak-guard:allow -->

If it fails: as in NW3, reading `tail -n 20 "$SBNODE/node.log"`.

### N4. Add a session on the node

On the host machine, as in W7/L7 with `--node pc2` and a directory on the node machine (`--profile` is still required; the node runs the session with its own profile):

```sh
sbpc host add --node pc2 --base team/worker --profile pc-worker --dir /home/user/work/app --label app --parent team/boss -- --model opus
```

Check: the dashboard's 터미널 tab shows the session on `pc2` as `running` and, after the agent's plugin registers, holding `team/worker-1`. On the node machine the profile folder has `hosted-<id>.json` and nothing else names the token.

### N5. Disconnect and reconnect

While the host is stopped, restarted or unreachable, the node logs `host channel down (…); retrying in …` and keeps its sessions running as last assigned; it reconnects (1 s, doubling to 30 s) and logs `attached to …` again, keeping the credentials it holds. The host shows the node's sessions as `node offline` meanwhile. A stopped node (task or unit stopped, machine off) leaves its sessions to the keeper or tmux; the next start adopts them.

A revoked node stops with exit code 4 and `the host refused this node's token (HTTP 401): the node was revoked`; its sessions' credentials are refused from then on. It cannot come back under the same name: remove it as below and start again at H2 with a new name.

### N6. Remove the node

On the host machine remove the node's sessions (`sbpc host remove <id>` for each record on `pc2`), then revoke the node in the dashboard (Settings, nodes, 끊기). Both revoke the credentials on the node. Then on the node machine:

Windows:

```powershell
Unregister-ScheduledTask -TaskName 'switchboard-node' -Confirm:$false
Get-Process switchboard -ErrorAction SilentlyContinue | Where-Object Path -like "$node\bin\*" | Stop-Process -Force
Remove-Item -Recurse -Force $node
```

Linux:

```sh
systemctl --user disable --now switchboard-node.service
rm -f "$HOME/.config/systemd/user/switchboard-node.service"
systemctl --user daemon-reload
SBSOCK="$(sed -n 's/.*tmux backend, socket \([^ ]*\).*/\1/p' "$SBNODE/node.log" | tail -n 1)"
[ -z "$SBSOCK" ] || tmux -L "$SBSOCK" kill-server 2>/dev/null || true
rm -r "$SBNODE"
```

Check: the task or unit is gone and the node directory no longer exists; with it go `node.token` and every `hosted-*.json`.

## Scheduled runs

A scheduled run launches a hosted session at cron times and mails a prompt to the session's base role. It belongs to one session record on the host; the session's node does the launching and, with a precheck, decides whether to run. Values below, used with the session from W7 / L7 (`team/worker` in `work\app` / `~/work/app`):

| Value | Setting |
| --- | --- |
| `--cron` | five fields, read in the host's time zone: `30 2 * * 1-5` is 02:30 on weekdays |
| `--prompt` | the mail the session's base role gets at each run |
| `--label` | the mail subject (default `scheduled run <id>`) |
| `--check` | precheck: a shell command run on the node in the session's folder before each run (`sh -c`; `cmd /c` on Windows) |
| `--check-timeout` | limit of the precheck, `1s` to `10m` (default `60s`) |
| `--max-run` | stop a session the run launched after this long, `1m` to `168h` (default `2h`); it also stops when the run's mail is completed |
| `--resume` | continue the session's last conversation (default: a new one each run) |
| `--notify` | roles mailed about runs that need attention, comma separated |

Precheck exit codes (its standard output, the last 4 KiB, follows the prompt in the mail):

| Precheck | Run result | What happens |
| --- | --- | --- |
| exit `0` | `fired` | the session is launched (when stopped) and gets the mail |
| exit `1` | `skipped` | nothing is launched or mailed |
| any other exit, or no exit code | `error` | nothing is launched; alert |
| no exit within `--check-timeout` | `timeout` | the command is killed with everything it started; alert |
| node started without `--allow-checks` | `error` | `this node does not run prechecks (start it with --allow-checks or SWITCHBOARD_HOSTING_ALLOW_CHECKS=on)`; alert |

Other results: `overrun` (the launched session was still running at `--max-run`; it is stopped; alert), `busy` (an earlier run's session still holds the session, or its precheck still runs; nothing is launched), `waiting` (`SWITCHBOARD_HOSTING_MAX` sessions already run), `node-offline` (the session's node is not attached). `error`, `timeout` and `overrun` alert once (Discord, and a system mail to each `--notify` role); the same result again is not alerted; the next `fired` or `skipped` sends `resolved`. `busy`, `waiting` and `node-offline` do not alert. Each scheduled run keeps its newest 500 runs.

The session must be stopped for the run to launch it (`host stop`); a session that already runs only gets the mail and is not stopped afterwards. The host picks up changes and due times on its sweep (every 30 s, `SWITCHBOARD_SWEEP_INTERVAL`); a time missed while the host was down fires once, not once per missed time.

### S1. Allow prechecks on the node

A node runs a precheck only when started with `--allow-checks`; a scheduled run without `--check` needs nothing. On the host machine, for the node `local` that `up` runs: on Windows add `set SWITCHBOARD_HOSTING_ALLOW_CHECKS=on` after the `SWITCHBOARD_HOSTING_AGENT` line of the W6 block and run W6 again; on Linux add `Environment=SWITCHBOARD_HOSTING_ALLOW_CHECKS=on` after the `SWITCHBOARD_HOSTING_AGENT` line of the L6 block and run L6 again. For a node on another machine add `--allow-checks` to its `node --host …` line (NW3: in `node.cmd`; NL3: after `ExecStart=$SBNODE/bin/switchboard node --host $SBHOST`) and run that step again. A precheck gets the session's environment without its role profile, so it cannot read the role's token.

Check: `sbpc host auto add` with a `--check` (below) and `host auto run` ends `skipped` or `fired`, not `error` with `this node does not run prechecks`.

### SW2. Windows: schedule a run

```powershell
New-Item -ItemType Directory -Force $work | Out-Null
Set-Content "$work\queue.txt" -Value '' -NoNewline
sbpc host stop $id
$auto = sbpc host auto add $id --cron '30 2 * * 1-5' --label nightly --prompt 'Work through queue.txt and report to team/boss.' --check 'findstr /r /c:. queue.txt' --notify team/boss --max-run 30m
$auto
sbpc host auto list
sbpc host auto run $auto
sbwait 90 { (sbpc host auto show $auto) -match 'precheck: exit 1' }
Set-Content "$work\queue.txt" -Value '- rotate logs'
sbpc host auto run $auto
sbwait 90 { (sbpc host auto show $auto) -match ' fired ' }
sbpc host auto show $auto
sbpc --profile pc-worker inbox
$mail = (sbpc host auto show $auto --json | ConvertFrom-Json).last_run.mail_id
sbpc --profile pc-worker complete $mail
sbwait 90 { (sbpc host list) -match 'stopped' }
sbpc host auto show $auto
```

Check: `host auto add` prints the scheduled run's number; `host auto list` shows it `active` with the next time and `LAST` `-`. The first run ends `skipped` (`precheck: exit 1`: the queue is empty). The second ends `fired` with a `MAIL` number and the agent's conversation ID under `LAUNCHED`; `inbox` shows the mail `nightly` from `switchboard/system` with the prompt and, below it, `- rotate logs`. After `complete` the session is stopped within one sweep and the run's `DETAIL` reads `mail completed, session stopped`. `host auto run` works for any state and time; its lines read `(run now)`.

If it fails: `no hosted session …` → `$id` is not set; take it from `sbpc host list`. `notify: "team/boss" is not a registered role` → W5 did not register it. The run is `error` with `this node does not run prechecks` → S1. `error` with `precheck: exit 2` → `findstr` could not read `queue.txt`; the check runs in the session's folder, so run the first line again. `busy` → the session is still running from an earlier run; `sbpc host stop $id`. `waiting` → `SWITCHBOARD_HOSTING_MAX` sessions already run.

### SL2. Linux: schedule a run

```sh
mkdir -p "$HOME/work/app"
: > "$HOME/work/app/queue.txt"
sbpc host stop "$SBID"
SBAUTO="$(sbpc host auto add "$SBID" --cron '30 2 * * 1-5' --label nightly --prompt 'Work through queue.txt and report to team/boss.' --check 'grep . queue.txt' --notify team/boss --max-run 30m)"
echo "$SBAUTO"
sbpc host auto list
sbpc host auto run "$SBAUTO"
sbwait 90 eval 'sbpc host auto show "$SBAUTO" | grep -q "precheck: exit 1"'
echo '- rotate logs' > "$HOME/work/app/queue.txt"
sbpc host auto run "$SBAUTO"
sbwait 90 eval 'sbpc host auto show "$SBAUTO" | grep -q " fired "'
sbpc host auto show "$SBAUTO"
sbpc --profile pc-worker inbox
SBMAIL="$(sbpc host auto show "$SBAUTO" --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["last_run"]["mail_id"])')"
sbpc --profile pc-worker complete "$SBMAIL"
sbwait 90 eval 'sbpc host list | grep -q stopped'
sbpc host auto show "$SBAUTO"
```

Check and failures: as in SW2 (`grep . queue.txt` exits `1` on an empty file and prints the non-empty lines otherwise).

### S3. Read the history, pause, remove

```sh
sbpc host auto list
sbpc host auto show <auto-id> --runs 50
sbpc host auto pause <auto-id>
sbpc host auto resume <auto-id>
sbpc host auto rm <auto-id>
```

`list` has one line per scheduled run: `STATE` `active` or `paused`, `NEXT`, and `LAST` (the newest run's result and time). `show` prints the settings, the prompt and the newest `--runs` runs (default 20, at most 500) with `AT`, `RESULT`, `MAIL`, `LAUNCHED` and `DETAIL` (a precheck's exit code, the reason of an `error`); `--json` has each run's precheck exit code, duration and output. The dashboard shows the same under the session in the 터미널 tab, sub-tab 예약; a stretch of `skipped` runs is folded into one line. `pause` stops the times from firing (times passed meanwhile are not made up); `resume` takes the next time after now; `rm` deletes the scheduled run with its history; removing the session record removes its scheduled runs. `host auto` reads and writes the host's database directly, so run it in the same window as the other `sbpc host` commands.

### S4. Move an outside automation to a scheduled run

An automation outside the host (a crontab line or a Task Scheduler task that runs a script, which checks whether there is work, opens a new terminal session in the project folder and types the prompt) maps one to one:

| Outside automation | Scheduled run |
| --- | --- |
| crontab line / Task Scheduler trigger | `--cron` (host's time zone) |
| the script's "is there work?" test and its exit code | `--check` (exit `0` run, `1` skip, other: error) |
| text the script prints into the prompt | the precheck's output after the prompt |
| a new terminal session in the folder | the session record, launched with a new conversation (or `--resume`) |
| typing the prompt | `--prompt`, mailed to the session's base role |
| closing the terminal when done, or after a timeout | completing the mail, or `--max-run` |
| mailing yourself when the script fails | `--notify team/boss`: once, then `resolved` |

Before, with the base role `team/relay`:

```
*/30 * * * * cd ~/work/relay && ./has-work.sh && tmux new-session -d -s relay-run 'claude "Process the relay queue."'
```

After. Register `team/relay` and its profile `pc-relay` as in L5 / W5, then:

Linux:

```sh
mkdir -p "$HOME/work/relay"
SBRELAY="$(sbpc host add --node local --base team/relay --profile pc-relay --dir "$HOME/work/relay" --label relay --parent team/boss -- --model opus)"
sbpc host stop "$SBRELAY"
SBAUTO="$(sbpc host auto add "$SBRELAY" --cron '*/30 * * * *' --label relay-queue --prompt 'Process the relay queue.' --check './has-work.sh' --check-timeout 30s --notify team/boss --max-run 1h)"
sbpc host auto list
```

Windows:

```powershell
$relay = Join-Path $env:USERPROFILE 'work\relay'
New-Item -ItemType Directory -Force $relay | Out-Null
$rid = sbpc host add --node local --base team/relay --profile pc-relay --dir $relay --label relay --parent team/boss '--' --model opus
sbpc host stop $rid
$auto = sbpc host auto add $rid --cron '*/30 * * * *' --label relay-queue --prompt 'Process the relay queue.' --check 'has-work.cmd' --check-timeout 30s --notify team/boss --max-run 1h
sbpc host auto list
```

Check: `host auto list` shows `relay-queue` `active` with the next time. Then remove the outside entry (Linux: delete the crontab line; Windows: `Unregister-ScheduledTask -TaskName 'relay-queue' -Confirm:$false`) so that nothing starts a second session. The script keeps its exit codes (`0` work, `1` none). Run the script once by hand in the session's folder first (`./has-work.sh; echo $?`), and `host auto run` once before waiting for the first time.

If it fails: a run is `busy` while the old automation's session still runs → stop it before the first run. `error` with `precheck: exit 127` (or `'has-work.cmd' is not recognized`) → the script is not in the session's folder or not executable; the precheck runs there with a shell, not the script's own folder.

## OIDC sign-in

Instead of names, passwords and TOTP, the instance can sign admins in with an OpenID Connect provider of the installer's choice. Do this after W8 / L8: the checks there sign in as `pcadmin`, which the oidc mode no longer accepts (`sblogin` gets `401`).

At the provider, register the instance as a public client (native or desktop application, no client secret). Many providers accept a loopback `http` redirect URI only for public clients (RFC 8252), and a program on a PC cannot keep a secret anyway. Register a confidential client (web application, with a secret) only at a provider that has no public clients or refuses loopback redirects for them.

| Setting | Public client (default) | Confidential client |
| --- | --- | --- |
| Redirect URI | `http://127.0.0.1:8090/auth/callback` (the instance's port) | same |
| Grant | authorization code | authorization code |
| PKCE | `S256` | `S256` |
| Token endpoint auth method | `none` | `client_secret_basic` or `client_secret_post` |
| Scopes | `openid email profile` (`openid` is required; `email` is what an email entry below needs) | same |
| ID token signing | an algorithm the provider's discovery lists in `id_token_signing_alg_values_supported` (RS256, ES256, EdDSA, …) | same |
| Settings | no `SWITCHBOARD_OIDC_CLIENT_SECRET*` | `SWITCHBOARD_OIDC_CLIENT_SECRET_FILE` |

Then note the issuer and the client ID (and, for a confidential client, copy the client secret). `up.log` says which kind is in force at start: `admin auth: oidc (public client, PKCE)` or `admin auth: oidc (confidential client)`.

| Provider | `SWITCHBOARD_OIDC_ISSUER` |
| --- | --- |
| Google (confidential client) | `https://accounts.google.com` |
| Microsoft Entra ID | `https://login.microsoftonline.com/<tenant-id>/v2.0` |
| Self-hosted (Keycloak, Authentik, …) | its issuer, e.g. `https://id.example/realms/<realm>` or `https://id.example/application/o/<app>/` |

`SWITCHBOARD_OIDC_ADMINS` is the allow list, comma separated. An entry is an email, matched against the ID token's `email` claim only when the token also says `email_verified: true`, or `sub:<subject>`, matched against the `sub` claim. Providers that send no `email_verified` (Entra ID among them) need `sub:` entries. A refused sign-in logs its subject in `up.log` (`oidc: login refused: "not on the allowlist (sub:…)"`); add that entry and restart `up` to let that account in. Removing an entry and restarting ends that admin's sessions.

Open the dashboard at `http://127.0.0.1:8090/` exactly (not `localhost`): the redirect URI and the sign-in cookies are tied to that address.

Windows (fill in the first three lines):

```powershell
$issuer = 'https://id.example/realms/home'
$clientId = '<client-id>'
$admins = 'you@example.com'
if (-not (Get-Command sbstop -ErrorAction SilentlyContinue)) { throw 'sbstop is missing: this env.ps1 predates it; run the helpers block of W3, then this block again' }
sbstop
@"
@echo off
set SWITCHBOARD_ADMIN_AUTH=oidc
set SWITCHBOARD_INSECURE_COOKIE=1
set "SWITCHBOARD_PUBLIC_URL=http://127.0.0.1:$port"
set "SWITCHBOARD_OIDC_ISSUER=$issuer"
set "SWITCHBOARD_OIDC_CLIENT_ID=$clientId"
set "SWITCHBOARD_OIDC_ADMINS=$admins"
set "SWITCHBOARD_CONFIG_DIR=%~dp0config"
set "SWITCHBOARD_HOSTING_AGENT=$agent"
"%~dp0bin\switchboard.exe" up --data "%~dp0." --port $port >> "%~dp0up.log" 2>&1
"@ | Set-Content "$data\up.cmd" -Encoding Ascii
Start-ScheduledTask -TaskName 'switchboard-pc'
sbwait 60 { Test-SbHealth }
sbstarted
curl.exe -sS -o NUL -w '%{redirect_url}\n' "http://127.0.0.1:$port/auth/login"
```

Linux (fill in the first three lines):

```sh
SBISSUER='https://id.example/realms/home'
SBCLIENTID='<client-id>'
SBADMINS='you@example.com'
cat > "$HOME/.config/systemd/user/switchboard-pc.service" <<EOF
[Unit]
Description=switchboard hosting instance on this PC

[Service]
Environment=SWITCHBOARD_ADMIN_AUTH=oidc
Environment=SWITCHBOARD_INSECURE_COOKIE=1
Environment=SWITCHBOARD_PUBLIC_URL=http://127.0.0.1:$SBPORT
Environment=SWITCHBOARD_OIDC_ISSUER=$SBISSUER
Environment=SWITCHBOARD_OIDC_CLIENT_ID=$SBCLIENTID
Environment=SWITCHBOARD_OIDC_ADMINS=$SBADMINS
Environment=SWITCHBOARD_CONFIG_DIR=$SBDATA/config
Environment=SWITCHBOARD_HOSTING_AGENT=$AGENT
Environment=PATH=$PATH
ExecStart=$SB up --data $SBDATA --port $SBPORT
StandardOutput=append:$SBDATA/up.log
StandardError=append:$SBDATA/up.log
KillMode=process
Restart=on-failure

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user restart switchboard-pc.service
sbwait 60 sbhealth
grep -o 'admin auth: .*' "$SBDATA/up.log" | tail -n 1
curl -fsS "http://127.0.0.1:$SBPORT/health"; echo
curl -sS -o /dev/null -w '%{redirect_url}\n' "http://127.0.0.1:$SBPORT/auth/login"
```

For a confidential client, keep the secret in a file in the data directory, not in `up.cmd` or the unit, and add one setting to the block before running it. Windows: run `[Net.NetworkCredential]::new('', (Read-Host 'Client secret' -AsSecureString)).Password | Set-Content "$data\oidc-client-secret"` and add the line `set "SWITCHBOARD_OIDC_CLIENT_SECRET_FILE=%~dp0oidc-client-secret"` to `up.cmd`. Linux: run `(umask 077; read -rsp 'Client secret: ' s; echo; printf '%s\n' "$s" > "$SBDATA/oidc-client-secret")` and add the line `Environment=SWITCHBOARD_OIDC_CLIENT_SECRET_FILE=$SBDATA/oidc-client-secret` to the unit.

Check: the mode line is `admin auth: oidc (public client, PKCE)` (`admin auth: oidc (confidential client)` with a secret), the version (`sbstarted` on Windows, `/health` on Linux) is the tag from W2 / L2, and the last line prints the provider's authorization address with `redirect_uri=http%3A%2F%2F127.0.0.1%3A8090%2Fauth%2Fcallback`. Opening `http://127.0.0.1:8090/` then shows one 로그인 button, which goes to the provider and back signed in.

If it fails: the mode line still says `admin auth: builtin` → the old `up` is running; on Windows run the block again (it starts with `sbstop`). Health times out → `up.log` names the missing or invalid `SWITCHBOARD_OIDC_*` setting (never its value). The provider says the redirect URI does not match → register exactly the URI above, with `127.0.0.1` and the port. `403 login failed` with `code exchange failed` in `up.log` → the registration and the settings disagree on the client kind: a public client needs auth method `none` and no secret setting, a confidential client the secret file. `id token rejected` → the provider signs ID tokens with an algorithm its discovery does not list. `403 login failed` after the provider → `up.log` has the reason (`oidc: login refused: …`): `not on the allowlist (sub:…)` or `email not verified (sub:…)` → add that `sub:` entry to the admins line and run the block again; `state mismatch` or `no flow cookie` → the dashboard was opened at another address than `http://127.0.0.1:<port>/`. To go back to names and passwords, run W6 / L6 again; `sbstarted` then prints `admin auth: builtin`.

Providers that only speak OAuth 2.0 (GitHub and similar) are not supported: they issue no ID token and have no issuer discovery.

## Refusals

| Message (in `up.log`, `admin-setup.log`, `setup.log` or on the console) | Fix |
| --- | --- |
| `SWITCHBOARD_HOSTING=on refused: SWITCHBOARD_HOSTING_TERMINAL must be loopback, on or off (got …)` | Fix the value, or remove it for the default `loopback`. |
| `SWITCHBOARD_HOSTING=on refused: SWITCHBOARD_HOSTING_TERMINAL=on needs admin sign-in: …` | Set `SWITCHBOARD_ADMIN_AUTH` (builtin or oidc) or `SWITCHBOARD_ADMIN_PROXY_TOKEN`, or use `loopback` or `off`. |
| 「이 주소에서는 터미널이 꺼져 있음」 on the terminal screen | `SWITCHBOARD_HOSTING_TERMINAL` is `loopback` (the default) and the page is not opened from this machine directly, or it is `off`. |
| `host: DB_PATH is unset; …` | `host` ran without `DB_PATH`; run it as `sbpc host …`. |
| `profile "pc-worker" not found (…)` | The command ran without `SWITCHBOARD_CONFIG_DIR`; run it through `sbpc`. |
| `node: agent CLI not found: SWITCHBOARD_HOSTING_AGENT="…"` | Set `SWITCHBOARD_HOSTING_AGENT` to the agent's full path (W3 `$agent` / L3 `AGENT`) and rewrite `up.cmd` (W6) or the unit (L6). |
| `tmux 3.2 or newer is required for terminal hosting on Linux: …` | L1. |
| `node: terminal hosting needs Windows 10 1809 (build 17763) or later; …` | This PC cannot host; see W1. |
| `node local: node is revoked` | The node was removed in Settings. Uninstall (W9 / L9) and install again from W3 / L3. |
| `node: the host refused this node's token (HTTP 401): the node was revoked; …` (exit code 4) | The node was removed in Settings while it ran; its terminals keep running. Uninstall (W9 / L9) and install again from W3 / L3. |
| `up: listen tcp 127.0.0.1:8090: bind: …` | Another program holds the port (it may show no listener), or Windows reserves it. `up` exits at once and starts no keeper or tmux server. Pick a free port and change it in `env.ps1` and `up.cmd` (run `sbstop` before changing `up.cmd`, then `Start-ScheduledTask`; when `sbstop` is not recognized, run the helpers block of W3 first), or in `env.sh` and the unit. |
| `401 login failed` from `sblogin` | Wrong password or a TOTP code already used; wait 30 s and run `sblogin` again. |

## Permission mode

Warning: with `PERMS` `skip` the agent runs every tool without asking anyone.

- Default: `host add` launches the agent with its permission-skip arguments. For `claude` (checked against Claude Code 2.1.328) that is `--dangerously-skip-permissions --settings {"skipDangerousModePermissionPrompt":true}`; the settings key skips the agent's one-time "Bypass Permissions mode" screen without touching the user's own settings. An agent without an entry (`internal/node/launch.go`, `permissionSkip`) gets nothing.
- Off for one session: `host add … --ask-permissions`, or `sbpc host set <id> --skip-permissions off` for an existing one.
- Words after `--` that choose a permission mode (`--permission-mode …`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`) win: nothing is added. A `--settings` of your own replaces the one above, so add `"skipDangerousModePermissionPrompt": true` to it, or the session stops at that screen until someone answers it in the dashboard. The node still joins its identity variables (`SWITCHBOARD_PROFILE`, `SWITCHBOARD_ENDPOINT`, …) into that `--settings`' `env`: inline JSON is changed in place; a file is read from the session folder and passed inline in its place, and one that cannot be read as a JSON object is passed unchanged.
- Sessions added before this release show `PERMS` `ask` and keep running as before. Turn one on with `sbpc host set <id> --skip-permissions on`; like every change but start and stop, it applies at the next launch, so follow it with `sbpc host restart <id>` (the conversation resumes).
- The dashboard's 터미널 tab shows the mode in 권한 (건너뜀 / 물음); `GET /api/admin/terminals` carries `skip_permissions`, and `PUT /api/admin/terminals/{id}` with `{"skip_permissions":true}` changes it.

## Agent adapters

- `host add … --adapter NAME` (or the dashboard's 어댑터) launches a session with that adapter; empty is the node's default. `host grant … --adapter NAME[,NAME]` limits what a role may choose.
- A node lists the adapters whose executable it finds; `GET /api/admin/nodes` shows them in `adapters` and `default_adapter`. A session naming one the node lacks is refused.
- Remaining usage: a node started by `up` or `node` sets the default adapter's status line to `switchboard node statusline` through `--settings` (joined with the permission skip's; a `--settings` of your own after `--` turns it off). It stores the account's remaining percentage (100 less the larger of the five-hour and seven-day use) and reset time, then runs the status line command of your settings files with the same input (5 s limit) and shows its output. `GET /api/admin/nodes` lists `usage` per adapter and account (the `CLAUDE_CONFIG_DIR` name, empty for the default); `remaining` is missing until the first response, for accounts without limit values, and after a window fills again, and 0 while a session shows a usage-limit message.
- Another agent CLI without code: set `SWITCHBOARD_HOSTING_ADAPTERS` on the node to a JSON file and restart the node. A bad file stops the node at start.

```json
{"adapters": [{
  "name": "acme",
  "executable": "acme-agent",
  "new_args": ["--conversation", "{id}"],
  "resume_args": ["--reopen", "{id}"],
  "permission_skip_args": ["--no-confirm"],
  "drop_env": ["ACME_SESSION"],
  "limit_patterns": ["(?i)quota exhausted"]
}]}
```

| Field | |
|---|---|
| `name` | `[a-z0-9][a-z0-9_-]*`, up to 32; not a built-in adapter's |
| `executable` | looked up on `PATH`, or a full path |
| `new_args` | arguments of a new conversation; `{id}` is the session's conversation id |
| `resume_args` | arguments that resume `{id}`; without them every launch starts a new conversation |
| `permission_skip_args` | added when `PERMS` is `skip` |
| `drop_env` | environment names removed from the session |
| `limit_patterns` | Go regular expressions of the usage-limit message on screen |

## Sessions a role adds under itself

The admin lets a role (a boss, say) add, start, stop and remove worker sessions under itself with its own token, within a grant:

```sh
sbpc host grant team/boss --base team/worker --node local,pc2 --dir-root /home/user/work
sbpc host grant list
sbpc host revoke-grant team/boss
```

`--base`, `--node` and `--dir-root` take comma lists or repeat; a new `host grant` for the same role replaces the old one. Revoking leaves the role's sessions in place; only the admin can change them then.

On any machine with the role's profile and no `DB_PATH`, `host add|list|start|stop|remove` run as that role through the server (`SWITCHBOARD_PROFILE=<profile> switchboard host list`; `--profile` of `host add` still names the session's profile on the node). The server refuses (403, naming the condition) a session whose parent is not the role (an empty `--parent` becomes the role), whose base or node is not in the grant, whose class is not `execute` or `review`, that sets env, whose args are anything but `--model`, `--agent`, `--name` or `--effort` with a value (letters, digits and `. _ -`; `--add-dir`, `--settings`, `--permission-mode` and the rest are for the admin), or whose directory, after `.` and `..` are resolved (Windows: either separator, any case), is not under a grant root; a start, stop or remove of a session whose parent is another role; and every other `host` action, including `host auto`. `host list` shows only the role's own sessions. The node checks once more at each launch, or refuses it: the directory, with symbolic links resolved, must still be under its grant root, and on `up`'s own node (which uses the profiles its records name) the named profile's role must be the session's base role or one of its numbered slots; a stored role session's args are checked again against the same list, and a refusal leaves a `refused` event on the record. The session's 기록 keeps `add` and `remove` with the role's name, and the removal is kept after the session is gone.

## Operations deployment

The operations deployment (`compose.yaml`, the Jenkins deploy) may pass `SWITCHBOARD_HOSTING*` through, but only from its deploy `.env`: `internal/config/opsguard_test.go` fails the build when an operations file writes an address into a hosting variable, or when a compose file sets one other than as `${NAME:-}` (empty by default). With hosting on, the plain server is a host only and runs no node; sessions run on nodes attached over the remote listener, or in an instance started with `switchboard up` on the PC that runs them.
