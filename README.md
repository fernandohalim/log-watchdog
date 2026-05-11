# Java Services Watchdog

A small Go-based Windows service that monitors Java apps for "stuck" or "crashed" states and emails you when something goes wrong.

## How it works

Every `check_interval_minutes`, for each configured service:

1. **Find the newest `.log` file** (by modification time) in the service's log directory.
2. **Check if the file is growing.** If its size hasn't increased for `stuck_threshold_minutes`, the service is flagged **STUCK**.
3. **Check if the Java process is alive** by scanning all running processes for one whose command line contains the configured substring. If none found, the service is **CRASHED**.
4. **If the log directory is unreadable or contains no `.log` file**, the service is flagged **UNKNOWN**. This usually means the directory was deleted, permissions changed, the drive is unmounted, or the service was reconfigured. UNKNOWN is treated as an alert-worthy condition (the process info is included in the detail so you can tell whether the underlying service is also down).
5. If any service is STUCK, CRASHED, or UNKNOWN, send **one combined HTML email** to all recipients. The alert is **re-sent every `alert_repeat_interval_minutes`** until the service recovers.
6. When a previously bad service goes back to HEALTHY, send a **recovery email**.

In the email, statuses are color-coded: **STUCK** = amber, **CRASHED** = red, **UNKNOWN** = purple.

The watchdog logs everything to its own log file (`log_file` in config) — useful for troubleshooting.

## Project layout

```
watchdog/
  main.go              entry point, flag parsing
  config.go            config schema + loader
  monitor.go           check loop, state machine, log file discovery
  process.go           process matching via command-line substring
  email.go             SMTP (no auth, no STARTTLS) + HTML templates
  service.go           Windows service install/run/stop
  logger.go            log file setup
  go.mod
  config.json          example config (edit before deploying)
  install.bat          installs the Windows service
  uninstall.bat        removes the Windows service
```

## Build

On a machine with Go installed (1.21+):

```bat
go mod tidy
go build -o watchdog.exe
```

If you build on a non-Windows machine, cross-compile:

```bash
GOOS=windows GOARCH=amd64 go build -o watchdog.exe
```

The resulting `watchdog.exe` is statically linked and has no DLL dependencies beyond Windows itself.

## Deploy

You have two paths depending on whether you have admin rights:

### A) With admin (recommended — service mode)

1. Create a folder on the server, e.g. `D:\rs-fhalim\watchdog\`.
2. Copy `watchdog.exe`, `config.example.json`, `install.bat`, `uninstall.bat` into it.
3. **Rename `config.example.json` to `config.json`** and edit it:
   - `recipients` — who gets emails
   - `services` — your services (name, log directory, and the cmdline substring for matching the Java process)
   - `mail.host`, `mail.from` — your SMTP relay and from-address
   - `log_file` — where the watchdog logs its own activity
4. **Right-click `install.bat` → Run as administrator.** This installs and starts the service.

To remove later: **right-click `uninstall.bat` → Run as administrator.**

### B) Without admin (foreground mode)

If you can't install Windows services, run the watchdog as a regular cmd window — same pattern as your existing Java services and log-janitor.

1. Same folder setup as above (`watchdog.exe`, `config.example.json`, plus `run.bat`).
2. **Rename `config.example.json` to `config.json`** and edit as above.
3. **Double-click `run.bat`.** A minimized "JavaWatchdog" cmd window appears in your taskbar. That's it.

To stop it: bring the minimized window to focus and press `Ctrl+C`, or right-click its taskbar entry and close it.

**Auto-start on logon (still no admin needed):** Use the included `scheduled-task.xml`:

1. Open Task Scheduler (`taskschd.msc`).
2. Edit `scheduled-task.xml` and change the `<Command>` path to wherever you put `run.bat`.
3. In Task Scheduler: **Action → Import Task...**, pick `scheduled-task.xml`.
4. Confirm. The task runs as your user, at every logon, automatically.

**Tradeoffs vs. service mode:**

| | Service (admin) | Foreground (.bat / Task Scheduler) |
|--|--|--|
| Admin required | Yes | No |
| Survives logoff | Yes | No — dies when user session ends |
| Auto-starts on boot | Yes (before login) | Only after a user logs in |
| Runs as | LocalSystem | Your user account |
| Sees all java.exe cmdlines | Always | Only processes in same/lower session |

Since your Java services already run as cmd windows (which means **someone has to stay logged in** to keep them alive), the foreground mode is functionally equivalent for your setup — the watchdog dies under the same conditions your monitored services would die anyway.

## Operational commands

```bat
sc query JavaWatchdog                  REM check service state
sc stop JavaWatchdog
sc start JavaWatchdog
watchdog.exe -debug -config config.json   REM run in foreground for testing
```

In `-debug` mode the watchdog runs in your console, prints everything to stdout, and stops on Ctrl+C. Great for verifying email delivery and process matching before installing the service.

## Services that don't write log files

If a service only prints to its cmd window (no file logging), edit its `.bat` to redirect stdout/stderr to a file the watchdog can see. See `sample-service.bat`. One-liner pattern:

```bat
java -jar myservice.jar >> "D:\path\to\log\console.log" 2>&1
```

After this, the watchdog treats it like any other service.

## Log rotation

The watchdog itself appends to `watchdog.log` and does **not** rotate it. If you already use your `log-janitor` Go app to clean logs, add the watchdog's log directory to its `directories` list and it'll handle retention automatically.

## Troubleshooting

- **Service won't start:** Open **Event Viewer → Windows Logs → Application**, filter source `JavaWatchdog`. Common causes: bad path in `config.json`, missing log directory, malformed JSON.
- **No alert email:** Run `watchdog.exe -debug -config config.json` and watch the console. Check that the SMTP relay accepts mail from `noreply@rintis.co.id` from this server's IP.
- **False CRASHED alerts:** Your `process_match` substring may not be unique enough. The watchdog ignores its own PID but will only find one match. If the Java cmdline doesn't actually contain the substring you configured, the process won't be detected. Use the PowerShell command above to verify.
- **False STUCK alerts:** A genuinely idle service (no traffic) won't write logs. Either raise `stuck_threshold_minutes`, or have the app emit a heartbeat log line on a timer.

## Limitations / known gotchas

- The watchdog reads command lines via the standard Windows process API. Running as **LocalSystem** (the default for installed services) gives it access to all processes' command lines.
- If two services happen to share a command-line substring, the watchdog only finds the first match. Make `process_match` specific.
- File-size based detection means a service that writes the *same number of bytes* but rewrites them in place (truncate + write) will look stuck. None of your Java logging frameworks do this, so it shouldn't matter, but worth knowing.