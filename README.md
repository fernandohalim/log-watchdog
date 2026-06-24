# log watchdog

a lightweight go microservice that watches java services on windows for "stuck"
or "crashed" states and emails you when something is wrong.

each run is a single pass — no background loop, no installed service. you point
windows task scheduler at the `.exe` and it does one check and exits. timing
(how often to check) belongs to the scheduler, so the only knob in config is how
long a log can sit idle before it counts as stuck.

## how it works

on every run, for each configured service:

1. **find the newest `.log` file** (by modification time) in the service's log directory.
2. **check if it's gone idle.** if the newest log hasn't been written to for `stuck_threshold_minutes`, the service is flagged **STUCK**.
3. **check if the java process is alive** by scanning running processes for one whose command line contains the configured substring. if none is found, the service is **CRASHED**.
4. **if the log directory is unreadable or has no `.log` file**, the service is flagged **UNKNOWN** (drive unmounted, folder deleted, permissions changed, etc.).
5. if any service is STUCK, CRASHED, or UNKNOWN, send **one combined HTML email** to all recipients.
6. if a service that was bad on the previous run is now HEALTHY, send a **recovery email**.

statuses are color-coded in the email: **STUCK** = amber, **CRASHED** = red, **UNKNOWN** = purple.

## features

* **stateless checks:** stuck detection compares the newest log's mtime to now — no in-memory history needed.
* **recovery emails:** a tiny `state.json` next to the exe remembers the last run's status so it can tell you when things come back.
* **re-alerts on its own:** still bad next run? you get another alert. the repeat cadence is just your scheduler interval.
* **json configured:** update services, recipients, and the stuck threshold without recompiling.
* **single exe:** statically linked, no DLLs beyond windows itself — perfect for windows task scheduler.

## installation & build

1. clone the repository.
2. compile the executable (go 1.21+):
```bash
go build -o watchdog.exe .
```
to cross-compile from a non-windows machine:
```bash
GOOS=windows GOARCH=amd64 go build -o watchdog.exe .
```

## configuration

copy `config.example.json` to `config.json` (the exe reads `config.json` from
its own folder) and edit:

| field | meaning |
|--|--|
| `mail.host`, `mail.port`, `mail.from`, `mail.subject` | SMTP relay (no auth, no STARTTLS) and the email envelope |
| `recipients` | who gets the emails |
| `stuck_threshold_minutes` | how long a log can be idle before the service is STUCK (default 15) |
| `log_file` | where the watchdog appends its own run log |
| `services[].name` | label shown in emails and the log |
| `services[].log_directory` | folder holding the service's `.log` files |
| `services[].process_match` | case-insensitive substring matched against process command lines |

## deploy with task scheduler

1. create a folder on the server, e.g. `D:\log-watchdog\`.
2. copy `watchdog.exe` and your edited `config.json` into it.
3. open task scheduler (`taskschd.msc`) → **create task**:
   - **general:** run whether user is logged on or not; run with highest privileges (so it can read every process's command line).
   - **triggers:** new → repeat task every e.g. 15 minutes, indefinitely.
   - **actions:** start a program → program = the full path to `watchdog.exe`. set **start in** to the folder so it finds `config.json`.
4. save. that's the whole deployment.

to verify before scheduling, just run it once from a terminal in the folder:
```bash
watchdog.exe
```
it prints each service's status to the console (and the log file) and exits.

## services that don't write log files

if a service only prints to its cmd window, redirect its output to a file the
watchdog can see, then point `log_directory` at that folder:
```bat
java -jar myservice.jar >> "D:\path\to\log\console.log" 2>&1
```

## log rotation

the watchdog appends to `log_file` and does **not** rotate it. if you already
run `log-janitor`, add the watchdog's log folder to its `directories` list and
it'll handle retention.

## troubleshooting

- **no alert email:** run `watchdog.exe` by hand and watch the console. confirm the SMTP relay accepts mail from your `from` address and this server's IP.
- **false CRASHED alerts:** your `process_match` substring isn't matching the real java command line. check the actual cmdline with `Get-CimInstance Win32_Process | select ProcessId,CommandLine` and adjust.
- **false STUCK alerts:** a genuinely idle service writes no logs. raise `stuck_threshold_minutes`, or have the app emit a heartbeat log line.
- **no recovery email after a fix:** recovery is only sent if the *previous* run saw the service as bad. delete `state.json` to reset history.

## project layout

```
log-watchdog/
  main.go              config, check logic, process matching, state, logging
  email.go             SMTP (no auth, no STARTTLS) + HTML templates
  go.mod
  config.example.json  copy to config.json and edit before deploying
```
