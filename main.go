package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// Config mirrors config.json. Timing (how often the watchdog runs) is owned by
// Windows Task Scheduler, so the only time knob here is stuck_threshold_minutes.
type Config struct {
	Mail              MailConfig      `json:"mail"`
	Recipients        []string        `json:"recipients"`
	StuckThresholdMin int             `json:"stuck_threshold_minutes"`
	LogFile           string          `json:"log_file"`
	Services          []ServiceConfig `json:"services"`
}

type MailConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	From    string `json:"from"`
	Subject string `json:"subject"`
}

type ServiceConfig struct {
	Name         string `json:"name"`
	LogDirectory string `json:"log_directory"`
	ProcessMatch string `json:"process_match"`
}

type Status string

const (
	StatusHealthy Status = "HEALTHY"
	StatusStuck   Status = "STUCK"
	StatusCrashed Status = "CRASHED"
	StatusUnknown Status = "UNKNOWN"
)

// isBad reports whether a status is alert-worthy.
func isBad(s Status) bool { return s == StatusStuck || s == StatusCrashed || s == StatusUnknown }

// Result is one service's outcome for a single run — used for logging and email rendering.
type Result struct {
	Name    string
	Status  Status
	Detail  string
	LogFile string
	PID     string
	Alive   bool
}

func main() {
	exeDir := filepath.Dir(mustExe())

	cfg, err := loadConfig(filepath.Join(exeDir, "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	closeLog := setupLogger(cfg.LogFile)
	defer closeLog()

	statePath := filepath.Join(exeDir, "state.json")
	prev := loadState(statePath)

	threshold := time.Duration(cfg.StuckThresholdMin) * time.Minute
	log.Printf("watchdog run: threshold=%dm services=%d", cfg.StuckThresholdMin, len(cfg.Services))

	var problems, recovered []Result
	next := make(map[string]Status, len(cfg.Services))

	for _, sc := range cfg.Services {
		r := checkService(sc, threshold)
		log.Printf("[%-25s] %-8s | %s", r.Name, r.Status, r.Detail)

		next[sc.Name] = r.Status
		if isBad(r.Status) {
			problems = append(problems, r)
		} else if isBad(prev[sc.Name]) {
			recovered = append(recovered, r)
		}
	}

	if len(problems) > 0 {
		if err := SendAlertEmail(cfg, problems); err != nil {
			log.Printf("[!] alert email failed: %v", err)
		} else {
			log.Printf("sent ALERT email for %d service(s)", len(problems))
		}
	}
	if len(recovered) > 0 {
		if err := SendRecoveryEmail(cfg, recovered); err != nil {
			log.Printf("[!] recovery email failed: %v", err)
		} else {
			log.Printf("sent RECOVERY email for %d service(s)", len(recovered))
		}
	}

	saveState(statePath, next)
	log.Println("run finished")
}

// checkService inspects one service's newest log file and process, statelessly.
func checkService(sc ServiceConfig, threshold time.Duration) Result {
	r := Result{Name: sc.Name}
	alive, pid := matchProcess(sc.ProcessMatch)
	r.Alive, r.PID = alive, pid

	newest, mtime, err := newestLog(sc.LogDirectory)
	switch {
	case err != nil:
		r.Status = StatusUnknown
		r.Detail = fmt.Sprintf("log directory error: %v; %s", err, procInfo(alive, pid, sc.ProcessMatch))
	case newest == "":
		r.Status = StatusUnknown
		r.Detail = fmt.Sprintf("no .log file in %s; %s", sc.LogDirectory, procInfo(alive, pid, sc.ProcessMatch))
	case !alive:
		r.Status = StatusCrashed
		r.LogFile = newest
		r.Detail = fmt.Sprintf("process not found (cmdline substring: %q)", sc.ProcessMatch)
	default:
		r.LogFile = newest
		silent := time.Since(mtime)
		if silent >= threshold {
			r.Status = StatusStuck
			r.Detail = fmt.Sprintf("log idle for %s (threshold %s); pid %s is alive", fmtDur(silent), fmtDur(threshold), pid)
		} else {
			r.Status = StatusHealthy
			r.Detail = fmt.Sprintf("ok; last write %s ago; pid %s", fmtDur(silent), pid)
		}
	}
	return r
}

// newestLog returns the .log file in dir with the most recent mtime.
// Returns an empty path with nil error if dir has no .log file.
func newestLog(dir string) (path string, mtime time.Time, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, err
	}
	type cand struct {
		path  string
		mtime time.Time
	}
	var cs []cand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cs = append(cs, cand{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	if len(cs) == 0 {
		return "", time.Time{}, nil
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].mtime.After(cs[j].mtime) })
	return cs[0].path, cs[0].mtime, nil
}

// matchProcess returns the first running process whose command line contains
// substr (case-insensitive). The watchdog's own PID is excluded.
func matchProcess(substr string) (alive bool, pid string) {
	if substr == "" {
		return false, ""
	}
	self := int32(os.Getpid())
	procs, err := process.Processes()
	if err != nil {
		return false, ""
	}
	needle := strings.ToLower(substr)
	for _, p := range procs {
		if p.Pid == self {
			continue
		}
		cmd, err := p.Cmdline()
		if err != nil || cmd == "" {
			continue
		}
		if strings.Contains(strings.ToLower(cmd), needle) {
			return true, fmt.Sprintf("%d", p.Pid)
		}
	}
	return false, ""
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if cfg.StuckThresholdMin <= 0 {
		cfg.StuckThresholdMin = 15
	}
	if cfg.Mail.Subject == "" {
		cfg.Mail.Subject = "[ watchdog ] java service alert"
	}
	if cfg.Mail.Port == 0 {
		cfg.Mail.Port = 25
	}

	if len(cfg.Services) == 0 {
		return nil, fmt.Errorf("config has no services")
	}
	if len(cfg.Recipients) == 0 {
		return nil, fmt.Errorf("config has no recipients")
	}
	if cfg.Mail.Host == "" {
		return nil, fmt.Errorf("config mail.host is required")
	}
	if cfg.Mail.From == "" {
		return nil, fmt.Errorf("config mail.from is required")
	}
	for i, s := range cfg.Services {
		switch {
		case s.Name == "":
			return nil, fmt.Errorf("service[%d]: name is required", i)
		case s.LogDirectory == "":
			return nil, fmt.Errorf("service[%d] %s: log_directory is required", i, s.Name)
		case s.ProcessMatch == "":
			return nil, fmt.Errorf("service[%d] %s: process_match is required", i, s.Name)
		}
	}
	return &cfg, nil
}

// loadState reads the previous run's per-service statuses. A missing or unreadable
// file is treated as "no history" so recovery emails simply don't fire on first run.
func loadState(path string) map[string]Status {
	m := map[string]Status{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func saveState(path string, m map[string]Status) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("warning: cannot write state %s: %v", path, err)
	}
}

func setupLogger(path string) func() {
	log.SetFlags(log.LstdFlags)
	if path == "" {
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("warning: cannot create log dir for %s: %v", path, err)
		return func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("warning: cannot open log file %s: %v", path, err)
		return func() {}
	}
	log.SetOutput(io.MultiWriter(os.Stdout, f))
	return func() { f.Close() }
}

func mustExe() string {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate executable:", err)
		os.Exit(1)
	}
	return exe
}

func procInfo(alive bool, pid, substr string) string {
	if alive {
		return fmt.Sprintf("process alive (pid %s)", pid)
	}
	return fmt.Sprintf("process NOT found (cmdline substring: %q)", substr)
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}
