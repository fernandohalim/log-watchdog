package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ServiceStatus string

const (
	StatusInitial ServiceStatus = "" // never checked yet; used only as initial PreviousStatus
	StatusUnknown ServiceStatus = "UNKNOWN"
	StatusHealthy ServiceStatus = "HEALTHY"
	StatusStuck   ServiceStatus = "STUCK"
	StatusCrashed ServiceStatus = "CRASHED"
)

// isBad returns true for any status that should trigger an alert email.
// StatusInitial is not "bad" — it means we haven't observed the service yet.
func isBad(s ServiceStatus) bool {
	return s == StatusStuck || s == StatusCrashed || s == StatusUnknown
}

type ServiceState struct {
	Config          ServiceConfig
	LastLogFile     string
	LastLogSize     int64
	LastGrowthAt    time.Time
	LastAlertAt     time.Time
	CurrentStatus  ServiceStatus
	PreviousStatus ServiceStatus
	ProcessAlive    bool
	PID             string
	DetectionDetail string
	SilentFor       time.Duration
}

func RunMonitor(ctx context.Context, cfg *Config) {
	log.Printf("Java Watchdog starting. check=%dm threshold=%dm repeat=%dm services=%d",
		cfg.CheckIntervalMin, cfg.StuckThresholdMin, cfg.AlertRepeatIntervalMin, len(cfg.Services))

	states := make(map[string]*ServiceState, len(cfg.Services))
	for _, sc := range cfg.Services {
		states[sc.Name] = &ServiceState{
			Config:         sc,
			CurrentStatus:  StatusInitial,
			PreviousStatus: StatusInitial,
		}
	}

	// Run first check immediately
	runCheck(cfg, states)

	ticker := time.NewTicker(time.Duration(cfg.CheckIntervalMin) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("Watchdog stopping.")
			return
		case <-ticker.C:
			runCheck(cfg, states)
		}
	}
}

func runCheck(cfg *Config, states map[string]*ServiceState) {
	now := time.Now()
	threshold := time.Duration(cfg.StuckThresholdMin) * time.Minute
	log.Println("--- check cycle ---")

	var problems []*ServiceState
	var recovered []*ServiceState

	for _, sc := range cfg.Services {
		st := states[sc.Name]
		st.PreviousStatus = st.CurrentStatus

		newest, mtime, size, err := newestLog(sc.LogDirectory)
		alive, pid := matchProcess(sc.ProcessMatch)
		st.ProcessAlive = alive
		st.PID = pid

		switch {
		case err != nil:
			st.CurrentStatus = StatusUnknown
			st.DetectionDetail = fmt.Sprintf("Log directory error: %v; %s",
				err, processInfo(alive, pid, sc.ProcessMatch))
		case newest == "":
			st.CurrentStatus = StatusUnknown
			st.DetectionDetail = fmt.Sprintf("No .log file in %s; %s",
				sc.LogDirectory, processInfo(alive, pid, sc.ProcessMatch))
		default:
			// Detect log rotation / first observation / truncation.
			if newest != st.LastLogFile {
				if st.LastLogFile != "" {
					log.Printf("[%s] Active log file changed: %s -> %s", sc.Name, st.LastLogFile, newest)
				}
				st.LastLogFile = newest
				st.LastLogSize = size
				st.LastGrowthAt = mtime // baseline from file's own mtime
			} else if size > st.LastLogSize {
				st.LastGrowthAt = now
				st.LastLogSize = size
			} else if size < st.LastLogSize {
				// truncated in place
				st.LastLogSize = size
				st.LastGrowthAt = mtime
			}

			st.SilentFor = now.Sub(st.LastGrowthAt)

			switch {
			case !alive:
				st.CurrentStatus = StatusCrashed
				st.DetectionDetail = fmt.Sprintf("Java process not found (cmdline substring: %q)", sc.ProcessMatch)
			case st.SilentFor >= threshold:
				st.CurrentStatus = StatusStuck
				st.DetectionDetail = fmt.Sprintf("Log idle for %s (threshold %s); pid %s is alive",
					fmtDur(st.SilentFor), fmtDur(threshold), pid)
			default:
				st.CurrentStatus = StatusHealthy
				st.DetectionDetail = fmt.Sprintf("OK; last write %s ago; pid %s",
					fmtDur(st.SilentFor), pid)
			}
		}

		log.Printf("[%-25s] %-8s | %s", sc.Name, st.CurrentStatus, st.DetectionDetail)

		if isBad(st.CurrentStatus) {
			problems = append(problems, st)
		}
		if isBad(st.PreviousStatus) && st.CurrentStatus == StatusHealthy {
			recovered = append(recovered, st)
		}
	}

	// Decide whether to send an alert email.
	if len(problems) > 0 {
		send := false

		// Newly bad: any service that wasn't bad before -> immediate alert.
		for _, st := range problems {
			if !isBad(st.PreviousStatus) {
				send = true
				break
			}
		}
		// Otherwise, re-alert if cooldown has elapsed (oldest LastAlertAt across problems).
		if !send {
			var oldest time.Time
			for _, st := range problems {
				if oldest.IsZero() || st.LastAlertAt.Before(oldest) {
					oldest = st.LastAlertAt
				}
			}
			if oldest.IsZero() ||
				now.Sub(oldest) >= time.Duration(cfg.AlertRepeatIntervalMin)*time.Minute {
				send = true
			}
		}

		if send {
			if err := SendAlertEmail(cfg, problems); err != nil {
				log.Printf("[!] Alert email failed: %v", err)
			} else {
				for _, st := range problems {
					st.LastAlertAt = now
				}
				log.Printf("Sent ALERT email for %d service(s).", len(problems))
			}
		} else {
			log.Printf("Holding alert email (%d service(s) bad, within repeat window).", len(problems))
		}
	}

	if len(recovered) > 0 {
		if err := SendRecoveryEmail(cfg, recovered); err != nil {
			log.Printf("[!] Recovery email failed: %v", err)
		} else {
			log.Printf("Sent RECOVERY email for %d service(s).", len(recovered))
		}
	}
}

// newestLog returns the .log file in dir with the most recent mtime.
// Returns empty path with nil error if no .log file present.
func newestLog(dir string) (path string, mtime time.Time, size int64, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	type cand struct {
		path  string
		mtime time.Time
		size  int64
	}
	var cs []cand
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cs = append(cs, cand{filepath.Join(dir, e.Name()), info.ModTime(), info.Size()})
	}
	if len(cs) == 0 {
		return "", time.Time{}, 0, nil
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].mtime.After(cs[j].mtime) })
	return cs[0].path, cs[0].mtime, cs[0].size, nil
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

// processInfo formats a short string describing whether the matched process is alive.
func processInfo(alive bool, pid, substr string) string {
	if alive {
		return fmt.Sprintf("Process alive (pid %s)", pid)
	}
	return fmt.Sprintf("Process NOT found (cmdline substring: %q)", substr)
}