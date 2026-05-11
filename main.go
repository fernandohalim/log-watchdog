package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/svc"
)

const (
	serviceName        = "JavaWatchdog"
	serviceDisplayName = "Java Services Watchdog"
	serviceDescription = "Monitors Java services for stuck/crashed states and sends email alerts."
)

func main() {
	configPath := flag.String("config", "config.json", "path to config.json")
	install := flag.Bool("install", false, "install as Windows service")
	remove := flag.Bool("uninstall", false, "uninstall Windows service")
	start := flag.Bool("start", false, "start the Windows service")
	stop := flag.Bool("stop", false, "stop the Windows service")
	debug := flag.Bool("debug", false, "run in foreground (for debugging or no-admin deployments)")
	foreground := flag.Bool("foreground", false, "run in foreground (alias of -debug, for no-admin deployments)")
	flag.Parse()
	if *foreground {
		*debug = true
	}

	absConfig, err := filepath.Abs(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config path error:", err)
		os.Exit(1)
	}

	switch {
	case *install:
		die(installService(absConfig), "install")
		fmt.Println("Service installed. Run -start (or `sc start " + serviceName + "`) to launch.")
	case *remove:
		die(removeService(), "uninstall")
		fmt.Println("Service uninstalled.")
	case *start:
		die(startService(), "start")
		fmt.Println("Service started.")
	case *stop:
		die(stopService(), "stop")
		fmt.Println("Service stopped.")
	case *debug:
		cfg, err := LoadConfig(absConfig)
		die(err, "load config")
		setupLogger(cfg.LogFile)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-ch
			cancel()
		}()
		RunMonitor(ctx, cfg)
	default:
		isService, err := svc.IsWindowsService()
		die(err, "detect service mode")
		if isService {
			runAsWindowsService(absConfig)
			return
		}
		fmt.Println("Java Watchdog — usage:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Typical setup (run as Administrator):")
		fmt.Println("  watchdog.exe -install -config D:\\rs-fhalim\\watchdog\\config.json")
		fmt.Println("  watchdog.exe -start")
		fmt.Println()
		fmt.Println("No-admin setup (run in foreground, like your other .bat services):")
		fmt.Println("  watchdog.exe -foreground -config D:\\rs-fhalim\\watchdog\\config.json")
		fmt.Println()
		fmt.Println("For testing without installing as service:")
		fmt.Println("  watchdog.exe -debug -config D:\\rs-fhalim\\watchdog\\config.json")
	}
}

func die(err error, what string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", what, err)
	os.Exit(1)
}