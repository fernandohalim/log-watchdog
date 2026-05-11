package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

func installService(configPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("Connect SCM (run as Administrator): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("Service %q already installed", serviceName)
	}

	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		StartType:   mgr.StartAutomatic,
	}, "-config", configPath)
	if err != nil {
		return fmt.Errorf("Create service: %w", err)
	}
	defer s.Close()

	_ = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	return nil
}

func removeService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("Connect SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("Open service: %w", err)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("Delete service: %w", err)
	}
	_ = eventlog.Remove(serviceName)
	return nil
}

func startService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Start()
}

func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return err
	}
	timeout := time.Now().Add(30 * time.Second)
	for status.State != svc.Stopped {
		if time.Now().After(timeout) {
			return fmt.Errorf("Timed out waiting for service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return err
		}
	}
	return nil
}

func runAsWindowsService(configPath string) {
	elog, err := eventlog.Open(serviceName)
	if err != nil {
		// Without the event log we can't surface errors anywhere reliable. Bail.
		return
	}
	defer elog.Close()
	elog.Info(1, fmt.Sprintf("%s starting (config=%s)", serviceName, configPath))
	if err := svc.Run(serviceName, &watchdogService{configPath: configPath, elog: elog}); err != nil {
		elog.Error(1, fmt.Sprintf("%s service run failed: %v", serviceName, err))
		return
	}
	elog.Info(1, serviceName+" stopped")
}

type watchdogService struct {
	configPath string
	elog       *eventlog.Log
}

func (w *watchdogService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	cfg, err := LoadConfig(w.configPath)
	if err != nil {
		w.elog.Error(1, fmt.Sprintf("Config load failed: %v", err))
		status <- svc.Status{State: svc.Stopped}
		return true, 1
	}
	setupLogger(cfg.LogFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		RunMonitor(ctx, cfg)
		close(done)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

loop:
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			break loop
		default:
			w.elog.Warning(1, fmt.Sprintf("Unexpected control: %d", c.Cmd))
		}
	}
	status <- svc.Status{State: svc.StopPending}
	cancel()
	<-done
	status <- svc.Status{State: svc.Stopped}
	return false, 0
}